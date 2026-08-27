package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const testParentLeaseSourceEnv = "WAKE_TEST_PARENT_LEASE_FD"

var testParentLeaseRead *os.File

// TestMain lets this binary be the daemon, and be an agent.
//
// daemon.EnsureRunning forks os.Executable() with a single "daemon" argument,
// which under `go test` is this test binary. Without this intercept the fork
// would re-run the test suite; with it, the detached path is exercised exactly
// as a user gets it - its own session, no controlling terminal, /dev/null
// stdio - by the code that ships.
//
// The order of the two branches is load-bearing and cost internal/daemon a
// debugging round before this package copied it. A forked daemon must inherit
// the agent marker, because its own children are the fake agents - so the
// daemon is checked first, on an argument that cannot be inherited. Checked the
// other way round, a daemon starts life as an agent, reads EOF from the
// /dev/null a detached process gets for stdin, and exits without a word, which
// surfaces as "the daemon never started listening" with no explanation
// anywhere.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == cmdDaemon {
		if err := run(os.Args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "wake:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv(fakeAgentEnv) == "1" {
		os.Exit(runFakeAgent())
	}
	// And be a Bubble Tea program that has stopped answering, which is what
	// killswitch.go's header claims about SIGINT and SIGTERM. Same reason as
	// the two branches above: the claim is about a real process taking a real
	// signal, and nothing in-process can observe it.
	if os.Getenv(wedgeProbeEnv) == "1" {
		os.Exit(runWedgedProbe())
	}
	// Every daemon this suite serves - the in-process one, the forked `wake
	// daemon`, and the real binary the pty tests run - starts agents on the direct
	// path. The supervisor re-exec of a race-instrumented test binary is a flaky,
	// unrepresentative test artifact (production re-execs a small optimized
	// binary); the supervised path has its own daemon tests plus make live. Set as
	// an inherited env so every fathered process gets it through os.Environ().
	if err := os.Setenv(daemon.DirectAgentLauncherEnv, "1"); err != nil {
		fmt.Fprintln(os.Stderr, "set direct launcher:", err)
		os.Exit(1)
	}
	leaseR, leaseW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "test parent lease:", err)
		os.Exit(1)
	}
	testParentLeaseRead = leaseR
	if err := os.Setenv(testParentLeaseSourceEnv, fmt.Sprintf("%d", leaseR.Fd())); err != nil {
		fmt.Fprintln(os.Stderr, "test parent lease:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = leaseW.Close()
	_ = leaseR.Close()
	removeWakeBinary()
	os.Exit(code)
}

// testTimeout bounds every wait in this package, so a wiring bug fails a test
// rather than hanging until the whole package times out. Two of the defects
// this project has found failed by hanging rather than by failing, which
// reports nothing at all.
const testTimeout = 15 * time.Second

// neverStops is the quitDelay of a daemon that takes a quit frame and then
// does nothing at all - the case `wake stop` must refuse to report as success.
const neverStops = -1

// shortCeiling compresses how long stopFleet waits for one test. Two minutes
// is the right production bound and an impossible test.
func shortCeiling(t *testing.T, d time.Duration) {
	t.Helper()
	prev := stopCeiling
	stopCeiling = d
	t.Cleanup(func() { stopCeiling = prev })
}

// shortActDeadline compresses how long an MCP acting call waits for the daemon
// to confirm it. Three seconds is the right production bound and a slow test.
func shortActDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	prev := actTimeout
	actTimeout = d
	t.Cleanup(func() { actTimeout = prev })
}

// tempSocket returns a short path for a unix socket. Short on purpose:
// sun_path is capped near 104 bytes on darwin, and t.TempDir() plus a test
// name pushes a socket past it - which fails as a bind error and looks exactly
// like a daemon that never started.
func tempSocket(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "waket")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("RemoveAll: %v", err)
		}
	})
	return filepath.Join(dir, "s")
}

// listenSilently binds a socket and never accepts on it.
//
// This is the shape of a daemon in graceful shutdown, and it is the condition
// worth building without a daemon: the kernel completes connections into the
// listen backlog throughout, so a dial succeeds and then hears nothing at all.
func listenSilently(t *testing.T, socket string) net.Listener {
	t.Helper()

	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on %s: %v", socket, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// fakeDaemon speaks just enough of the protocol to be waited on: a hello per
// connection, an answer to a status request, and a shutdown that takes a while
// and then closes every client - which is the ordering `wake stop` depends on.
type fakeDaemon struct {
	t      *testing.T
	socket string
	ln     net.Listener

	// quitDelay is how long shutdown takes. It stands in for the daemon's
	// grace for in-flight turns, and it is what makes "did stop wait" an
	// observable question rather than a race. neverStops means the daemon
	// takes the quit and does nothing about it.
	quitDelay time.Duration

	// status is what a status request is answered with.
	status rpc.Status

	// forkRefusal is what a fork frame is answered with, addressed to the id
	// *on the frame*. That is the daemon's own rule and the thing worth being
	// faithful to here: every one of daemon.fork's refusals carries the fork's
	// id rather than the parent's, because the client is waiting on that id
	// and the wait has no deadline. Empty leaves a fork frame unanswered,
	// which is what every other test in this package wants.
	forkRefusal string

	// sendRefusal is what a send or an interrupt is answered with, and
	// refusalAddress is the session it is addressed to - empty meaning the one
	// on the frame.
	//
	// Two fields rather than one because the property they exist for is which
	// id a refusal names. Nothing acknowledges a send, so `wake mcp` writes a
	// status request behind the verb and reads the daemon's refusal off the same
	// connection - and it must ignore an error frame naming a session it did not
	// address. The real daemon's error frames are all unicast, so the frame this
	// produces is one it cannot currently send; what it stands for is the two
	// that carry another id or none - `client.go`'s dropped-frames notice, and
	// anything a later daemon broadcasts. One field could only produce this
	// call's own refusal, which is the half that was never in doubt.
	sendRefusal    string
	refusalAddress string

	// spawnRefusal is the same thing for a spawn, and it exists so a test can
	// drive a client all the way through the spawn handshake and get an *error*
	// back. openSession's wait has no deadline either, so a fake that took the
	// frame and said nothing would leave `wake` blocked forever - and the branch
	// under test is what it did before the wait, which cannot be asserted from a
	// goroutine that never returns.
	spawnRefusal string

	// statusesBeforeHangUp is how many status requests this daemon answers
	// before it starts hanging up on them instead. Zero means it always answers.
	//
	// It exists for one shape a test cannot otherwise reach: bare `wake` against
	// a fleet ends in a Bubble Tea program, which needs the terminal `go test`
	// does not have and would write an alt screen over the test's own output. A
	// daemon that answers daemon.Status - the question the branch is taken on -
	// and then hangs up on the room's own request turns that path into an error
	// this package can assert on, without moving the decision anywhere it is not.
	statusesBeforeHangUp int

	// keepSocketFile makes shutdown leave the socket behind, which is what a
	// SIGKILLed daemon leaves: the kernel closes the listening fd, and nothing
	// unlinks the file because nothing ran ln.Close. A daemon that panicked
	// leaves the opposite - it unwinds through the deferred Close - and
	// telling those two apart is the only thing awaitSocketRelease's answer is
	// good for.
	keepSocketFile bool

	// closed is closed at the moment every client connection is hung up on.
	closed chan struct{}

	mu    sync.Mutex
	conns []net.Conn
	done  bool

	// received is every frame this daemon was sent, in arrival order and across
	// every connection, so a test can assert on what a client actually put on
	// the wire rather than on what it was asked to put there. It is one list
	// rather than a field per kind because the sharpest assertion bare `wake`
	// needs is about a frame that must **not** be here.
	received []rpc.Frame

	// answered is how many status requests have been replied to, for
	// statusesBeforeHangUp.
	answered int
}

func startFakeDaemon(t *testing.T, quitDelay time.Duration, status rpc.Status) *fakeDaemon {
	t.Helper()
	return listenAs(t, &fakeDaemon{quitDelay: quitDelay, status: status})
}

// startForkRefusingDaemon answers every fork frame with one refusal.
//
// A second constructor rather than a field written after the daemon is up: the
// serve goroutine reads it, and a test that assigned it after go d.accept()
// would be writing what another goroutine reads with nothing ordering the two.
func startForkRefusingDaemon(t *testing.T, status rpc.Status, why string) *fakeDaemon {
	t.Helper()
	return listenAs(t, &fakeDaemon{status: status, forkRefusal: why})
}

// listenAs binds the socket and starts serving one configured fake daemon.
// Everything it fills in is either the test's or the listener's, so a
// constructor above sets only what it means to change.
func listenAs(t *testing.T, d *fakeDaemon) *fakeDaemon {
	t.Helper()

	socket := tempSocket(t)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on %s: %v", socket, err)
	}
	d.t, d.socket, d.ln, d.closed = t, socket, ln, make(chan struct{})
	go d.accept()
	t.Cleanup(d.shutdown)
	return d
}

// frames is every frame this daemon was sent, copied so a caller cannot race
// the serve goroutines that are still appending to it.
func (d *fakeDaemon) frames() []rpc.Frame {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]rpc.Frame(nil), d.received...)
}

// lastOfKind is the newest frame of one kind, or a zero frame.
func (d *fakeDaemon) lastOfKind(kind string) rpc.Frame {
	got := d.frames()
	for i := len(got) - 1; i >= 0; i-- {
		if got[i].Kind == kind {
			return got[i]
		}
	}
	return rpc.Frame{}
}

// lastFork is the fork frame this daemon was sent, or a zero frame.
func (d *fakeDaemon) lastFork() rpc.Frame { return d.lastOfKind(rpc.FrameFork) }

// lastSpawn is the same for a spawn: the id a client minted, read off the wire
// rather than off the call site.
func (d *fakeDaemon) lastSpawn() rpc.Frame { return d.lastOfKind(rpc.FrameSpawn) }

func (d *fakeDaemon) accept() {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			return
		}
		d.mu.Lock()
		d.conns = append(d.conns, conn)
		d.mu.Unlock()
		go d.serve(conn)
	}
}

func (d *fakeDaemon) serve(conn net.Conn) {
	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameHello}); err != nil {
		return
	}
	frames, errs := rpc.ReadFrames(conn)
	defer func() { <-errs }()

	for f := range frames {
		d.record(f)
		switch f.Kind {
		case rpc.FrameStatus:
			if !d.answerStatus(conn) {
				// Out of budget, so this connection gets the other thing a
				// daemon can do with a question: nothing, and then EOF.
				_ = conn.Close()
			}
		case rpc.FrameSpawn:
			if d.spawnRefusal != "" {
				// Addressed to the id on the frame, for the reason a fork
				// refusal is: the client is waiting on that id.
				_ = rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameError, SessionID: f.SessionID, Text: d.spawnRefusal})
			}
		case rpc.FrameSend, rpc.FrameInterrupt:
			if d.sendRefusal != "" {
				// Addressed to refusalAddress rather than to f.SessionID, which
				// is the whole reason that field exists: the daemon fans every
				// client's failures out to every client, so a test needs to be
				// able to produce one that is *somebody else's*.
				id := d.refusalAddress
				if id == "" {
					id = f.SessionID
				}
				_ = rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameError, SessionID: id, Text: d.sendRefusal})
			}
		case rpc.FrameFork:
			if d.forkRefusal != "" {
				// Addressed to f.SessionID, which is the fork's own id and not
				// the parent's - see the field's comment. Echoing the id the
				// client sent is what makes the wait on the other side either
				// match or hang forever.
				_ = rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameError, SessionID: f.SessionID, Text: d.forkRefusal})
			}
		case rpc.FrameQuit:
			if d.quitDelay == neverStops {
				continue
			}
			// The real daemon stops every session first and closes its
			// clients only afterwards, so the end of a client's connection is
			// evidence the fleet is down. The delay is that ordering.
			go func() {
				time.Sleep(d.quitDelay)
				d.shutdown()
			}()
		}
	}
}

// record remembers one frame this daemon was sent.
func (d *fakeDaemon) record(f rpc.Frame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.received = append(d.received, f)
}

// answerStatus replies with the fleet, reporting whether it still had a reply
// left to give. The count and the read of the status are under one lock so two
// connections asking at once cannot both spend the last one.
func (d *fakeDaemon) answerStatus(conn net.Conn) bool {
	d.mu.Lock()
	if d.statusesBeforeHangUp > 0 && d.answered >= d.statusesBeforeHangUp {
		d.mu.Unlock()
		return false
	}
	d.answered++
	st := d.status
	d.mu.Unlock()

	_ = rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st})
	return true
}

// shutdown closes the listener and every client. Idempotent: a test's cleanup
// and a quit frame both reach it.
func (d *fakeDaemon) shutdown() {
	d.mu.Lock()
	if d.done {
		d.mu.Unlock()
		return
	}
	d.done = true
	conns := d.conns
	d.conns = nil
	d.mu.Unlock()

	// Closed before the connections, which is the real daemon's order and the
	// whole reason a client's EOF is evidence: shutdown ends every session
	// first and closes its clients afterwards. Closing this last would let a
	// client see EOF before the signal it is being compared against, which is
	// a flake in the test rather than in the thing under test.
	close(d.closed)
	if d.keepSocketFile {
		if ul, ok := d.ln.(*net.UnixListener); ok {
			ul.SetUnlinkOnClose(false)
		}
	}
	_ = d.ln.Close()
	for _, c := range conns {
		_ = c.Close()
	}
}

// shortRelease compresses the wait for the socket to be unlinked, for the tests
// that make sure it never is.
func shortRelease(t *testing.T, d time.Duration) {
	t.Helper()
	prev := releaseCeiling
	releaseCeiling = d
	t.Cleanup(func() { releaseCeiling = prev })
}

func (d *fakeDaemon) hasClosed() bool {
	select {
	case <-d.closed:
		return true
	default:
		return false
	}
}

// errTest is the stand-in for a failure whose text does not matter.
var errTest = errors.New("something went wrong")

// realDaemon is internal/daemon's own Serve, running in this process on its
// own socket. The fake above is for asserting on ordering this package
// controls; this is for asserting that the ordering the real daemon has is the
// one this package was written against.
type realDaemon struct {
	socket string
	err    <-chan error
	cancel context.CancelFunc
	waited bool
}

func startRealDaemon(t *testing.T) *realDaemon {
	t.Helper()

	socket := tempSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- daemon.Serve(ctx, socket) }()

	d := &realDaemon{socket: socket, err: errc, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		d.waitForExit(t)
	})

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			return d
		}
		select {
		case err := <-d.err:
			t.Fatalf("Serve returned before it was listening: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the daemon never started listening")
	return nil
}

// waitForExit insists Serve actually returns. Idempotent, so a test that shuts
// the daemon down itself and the cleanup can both call it.
func (d *realDaemon) waitForExit(t *testing.T) {
	t.Helper()
	if d.waited {
		return
	}
	d.waited = true

	select {
	case err := <-d.err:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(testTimeout):
		t.Errorf("Serve did not return within %v", testTimeout)
	}
}

// exited reports whether Serve has already returned, without waiting for it.
func (d *realDaemon) exited(t *testing.T, within time.Duration) bool {
	t.Helper()
	select {
	case err := <-d.err:
		d.waited = true
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
		return true
	case <-time.After(within):
		return false
	}
}

// syncBuffer is a bytes.Buffer a test can read while the code under test is
// still writing to it. waitForHello prints its explanation from whichever
// goroutine is waiting, and the test that asserts on that explanation is
// deliberately still running.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

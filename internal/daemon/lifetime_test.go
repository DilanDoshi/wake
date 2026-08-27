package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Removing the empty-exit check from dropClient must leave this daemon alive.
func TestLastClientLeavingAnEmptyDaemonEndsIt(t *testing.T) {
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.close()
	if !d.waitForExitWithin(t, time.Second) {
		t.Fatal("the last client left an empty daemon, but Serve stayed alive with nothing to supervise")
	}
}

// Removing the retirement-side empty-exit check must leave this daemon alive.
// Moving that check to disconnect instead must kill it before the process exits.
func TestFinalLiveSessionRetiringAfterTheLastClientEndsTheDaemon(t *testing.T) {
	fakeClaudeOnPath(t, "delayedexit")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.close()
	if d.waitForExitWithin(t, 100*time.Millisecond) {
		t.Fatal("the daemon exited on the last disconnect while a live agent still needed supervision; detach was broken")
	}
	if !d.waitForExitWithin(t, 3*time.Second) {
		t.Fatal("the final live session retired with no clients, but Serve stayed alive")
	}
}

// Removing the completePark-side empty-exit check must leave the first daemon
// alive. Clearing the book on that exit must make the second report empty.
func TestParkedOnlyDaemonEndsAndItsBookSurvivesTheRestart(t *testing.T) {
	fakeClaudeOnPath(t, "")
	socket := tempSocket(t)
	first := startDaemonOn(t, socket)
	c := attach(t, socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.close()

	if !first.waitForExitWithin(t, 3*time.Second) {
		t.Fatal("the only live session completed a park with no clients, but the parked-only daemon stayed alive")
	}

	seedParkTranscript(t, idAlpha)
	second := startDaemonOn(t, socket)
	again := attach(t, socket)
	row := sessionRow(again.status(), idAlpha)
	if row.State != rpc.StateParked || row.Name != "sydney" {
		t.Fatalf("after the parked-only daemon restarted, status row = %+v, want the parked session still available to /resume", row)
	}
	second.stop(t)
}

// Removing either post-commit refusal lets a connection or a process enter a
// daemon after shutdown has become irrevocable.
func TestNoClientOrAgentEntersAfterQuitIsCommitted(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "s"))
	s.beginQuit(quitNone)

	serverConn, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	c := newClient(serverConn)
	s.addClient(c)
	s.mu.Lock()
	_, attached := s.clients[c]
	s.mu.Unlock()
	if attached || !c.closed() {
		t.Error("a client attached after quit was committed")
	}

	cfg := core.Config{SessionID: idAlpha, Name: "sydney", Dir: t.TempDir()}
	a := newAgent(cfg.SessionID, cfg.Name, "task", cfg.Dir, "", core.NewSession(cfg), func() {})
	if why := s.admitLive(a, nil, false); !strings.Contains(why, "shutting down") {
		t.Fatalf("agent admission after quit commitment returned %q, want a shutdown refusal", why)
	}
}

// Removing admitMu lets the wake register while liveCount is blocked on the
// parked row, after that count has already taken its snapshot. Empty exit then
// commits from a stale zero with a newly admitted process row behind it.
func TestAdmissionCannotCrossAnEmptyExitCommitment(t *testing.T) {
	wasProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(wasProcs) })

	s := newServer(filepath.Join(t.TempDir(), "s"))
	parked := parkedAgentRow(parkedRecord{ID: idAlpha, Dir: t.TempDir()}, "sydney")
	if !s.register(parked) {
		t.Fatal("the parked barrier row was not registered")
	}
	parked.mu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			parked.mu.Unlock()
		}
	})

	reconsiderStarted := make(chan struct{})
	reconsidered := make(chan struct{})
	go func() {
		close(reconsiderStarted)
		s.reconsiderEmptyExit()
		close(reconsidered)
	}()
	<-reconsiderStarted
	// One P plus the held agent lock is the barrier: reconsider runs until it
	// blocks in the snapshot while still holding admitMu, then yields here.
	runtime.Gosched()

	cfg := core.Config{SessionID: idBeta, Name: "alex", Dir: t.TempDir()}
	late := newAgent(cfg.SessionID, cfg.Name, "task", cfg.Dir, "", core.NewSession(cfg), func() {})
	admitted := make(chan string, 1)
	go func() { admitted <- s.admitLive(late, nil, true) }()
	runtime.Gosched()

	parked.mu.Unlock()
	locked = false
	why := <-admitted
	<-reconsidered
	if !strings.Contains(why, "shutting down") {
		t.Fatalf("admission racing empty exit returned %q, want the shutdown refusal: a stale zero count admitted a process row behind the commitment", why)
	}
	if _, held := s.agent(idBeta); held {
		t.Fatal("the late process row remains in the fleet after empty exit committed")
	}
}

// Moving addClient back into serveClient lets the accept loop begin its next
// iteration first. The listener's second Accept commits empty exit before that
// goroutine can run, so an already accepted connection is hung up without its
// hello.
func TestAcceptedClientIsRegisteredBeforeEmptyExitCanCommit(t *testing.T) {
	wasProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(wasProcs) })

	s := newServer(filepath.Join(t.TempDir(), "s"))
	serverConn, peer := net.Pipe()
	listener := &emptyExitBarrierListener{s: s, conn: serverConn}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() { errCh <- s.run(ctx, listener) }()

	c := attachConn(t, peer)
	if err := <-errCh; err != nil {
		t.Fatalf("run: %v", err)
	}
	s.mu.Lock()
	quitting := s.quitting
	s.mu.Unlock()
	if quitting {
		t.Fatal("empty exit committed while an already accepted client was attached")
	}
	c.close()
	if !waitFor(&s.wg, testTimeout) {
		t.Fatal("server goroutines did not retire after the accepted client closed")
	}
}

// Moving Hello back into serveClient lets this broadcast enter the published
// client's queue first. A client that discards pre-Hello frames then loses a
// real fleet event at the exact moment it attaches.
func TestHelloIsQueuedBeforePublishedClientCanReceiveLiveFrames(t *testing.T) {
	wasProcs := runtime.GOMAXPROCS(1)
	t.Cleanup(func() { runtime.GOMAXPROCS(wasProcs) })

	s := newServer(filepath.Join(t.TempDir(), "s"))
	serverConn, peer := net.Pipe()
	live := rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, PID: 4242}}
	listener := &helloBarrierListener{s: s, conn: serverConn, live: live}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- s.run(ctx, listener) }()

	frames, errs := rpc.ReadFrames(peer)
	t.Cleanup(func() {
		cancel()
		_ = peer.Close()
		for range frames {
		}
		<-errs
		if !waitFor(&s.wg, testTimeout) {
			t.Error("server goroutines did not retire after the Hello ordering test")
		}
	})
	first := nextOrderedFrame(t, frames, errs, "first frame")
	second := nextOrderedFrame(t, frames, errs, "live frame after Hello")
	if first.Kind != rpc.FrameHello {
		t.Fatalf("first frame = %s, want %s before any live frame", first.Kind, rpc.FrameHello)
	}
	if second.Kind != live.Kind || second.Status == nil || second.Status.PID != 4242 {
		t.Fatalf("frame after Hello = %+v, want the live status frame", second)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("run: %v", err)
	}
}

func nextOrderedFrame(t *testing.T, frames <-chan rpc.Frame, errs <-chan error, what string) rpc.Frame {
	t.Helper()
	select {
	case frame, open := <-frames:
		if !open {
			t.Fatalf("connection closed before %s", what)
		}
		return frame
	case err := <-errs:
		t.Fatalf("read before %s: %v", what, err)
	case <-time.After(testTimeout):
		t.Fatalf("waited %v for %s", testTimeout, what)
	}
	return rpc.Frame{}
}

type emptyExitBarrierListener struct {
	s    *server
	conn net.Conn
}

func (l *emptyExitBarrierListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		conn := l.conn
		l.conn = nil
		return conn, nil
	}
	l.s.reconsiderEmptyExit()
	return nil, net.ErrClosed
}

func (l *emptyExitBarrierListener) Close() error   { return nil }
func (l *emptyExitBarrierListener) Addr() net.Addr { return barrierAddr("empty-exit") }
func (l *emptyExitBarrierListener) SetDeadline(time.Time) error {
	return nil
}

type barrierAddr string

func (a barrierAddr) Network() string { return "barrier" }
func (a barrierAddr) String() string  { return string(a) }

type helloBarrierListener struct {
	s    *server
	conn net.Conn
	live rpc.Frame
}

func (l *helloBarrierListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		conn := l.conn
		l.conn = nil
		return conn, nil
	}
	l.s.broadcast(l.live)
	return nil, net.ErrClosed
}

func (l *helloBarrierListener) Close() error   { return nil }
func (l *helloBarrierListener) Addr() net.Addr { return barrierAddr("hello") }
func (l *helloBarrierListener) SetDeadline(time.Time) error {
	return nil
}

// Removing lostProcess's endProcess leaves the reclaimed row live forever;
// removing retire's empty reconsideration retires the row but not the daemon.
func TestLastDetachedSilentAgentIsReclaimedRetiredAndEndsTheDaemon(t *testing.T) {
	shortSilence(t, 200*time.Millisecond)
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idAlpha, "held") // the leader is OS-confirmably gone

	c.close()
	if !d.waitForExitWithin(t, 3*time.Second) {
		t.Fatal("the last detached OS-confirmed-gone agent did not complete reclaim, retire, and empty daemon exit")
	}
	if records := loadRoster(rosterPath(d.socket)); len(records) != 0 {
		t.Fatalf("roster after reclaim and empty exit = %+v, want no live process record", records)
	}
}

// Restoring quietAndDue's unreachable exclusion, or making lostProcess return
// when the write already reported the agent, leaves this row live forever.
func TestWriteReportedAgentIsLaterOSReclaimedAndLetsEmptyDaemonExit(t *testing.T) {
	shortSilence(t, time.Second)
	fakeClaudeOnPath(t, "hold")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idAlpha, "held") // the leader is gone before the write

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "report first"})
	c.await("the failed write reporting the agent before an OS probe", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha
	})
	if row := c.pollState(idAlpha, rpc.StateSilent); row.Error == "" {
		t.Fatal("failed write reported a silent agent without its error")
	}

	c.close()
	if !d.waitForExitWithin(t, 4*time.Second) {
		t.Fatal("write-reported agent was never OS-reclaimed, so retire and empty exit never followed")
	}
}

// Returning unreachable instead of reclaiming from unreachableNow turns an
// EPIPE report into OS proof, clears the park label, and hard-kills this live
// process instead of letting it finish inside the normal grace.
func TestWriteReportAloneDoesNotBypassParkAllGrace(t *testing.T) {
	fakeClaudeOnPath(t, "closedstdin")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "stdin closed; still working")
	row := sessionRow(c.status(), idAlpha)
	if row.PID <= 0 || !processAlive(row.PID) {
		t.Fatalf("agent process %d is not alive before the write report", row.PID)
	}

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "report only"})
	c.await("the EPIPE report before ParkAll", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == idAlpha
	})
	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)

	records := loadParkBook(parkBookPath(d.socket))
	if len(records) != 1 || records[0].ID != idAlpha {
		t.Fatalf("park book after write report and ParkAll = %+v, want the live process gracefully parked", records)
	}
}

// Returning early when the daemon-side marker is absent must still remove the
// source marker: in-process Serve shares its environment with the test runner,
// and core.Session copies that real environment into the child.
func TestServeStripsPrivateLeaseMarkersFromARealAgentChild(t *testing.T) {
	fakeClaudeOnPath(t, "leaseenv")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	frame := c.awaitEvent(idAlpha, "test lease environment")
	if frame.Event.Text != "test lease environment clean" {
		t.Fatalf("fake agent inherited %q, want neither private lease marker", frame.Event.Text)
	}
}

// Removing the inherited-FD watcher leaves both the daemon and this live fake
// agent behind after the test runner's lease closes.
func TestClosingTheTestLeaseReclaimsAForkedDaemonAndItsAgent(t *testing.T) {
	fakeClaudeOnPath(t, "fdcheck")
	t.Setenv("WAKE_FAKE_FORBIDDEN_FD", "3")
	socket := tempSocket(t)
	t.Setenv(SocketEnv, socket)
	t.Setenv(fakeDaemonEnv, "1")

	leaseR, leaseW, err := os.Pipe()
	if err != nil {
		t.Fatalf("parent-death pipe: %v", err)
	}
	defer func() { _ = leaseR.Close() }()
	t.Setenv("WAKE_TEST_PARENT_LEASE_FD", fileDescriptor(leaseR))

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := EnsureRunning(ctx, socket)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	c := attachConn(t, conn)
	c.spawn(idAlpha, "sydney")
	leaseState := c.await("the fake agent's lease descriptor state", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil &&
			strings.Contains(f.Event.Text, "test lease descriptor")
	})
	if !strings.Contains(leaseState.Event.Text, "closed") {
		c.send(rpc.Frame{Kind: rpc.FrameQuit})
		waitForCleanExit(t, socket)
		t.Fatalf("fake agent reported %q: the daemon did not mark its lease descriptor close-on-exec", leaseState.Event.Text)
	}
	agentPID := sessionRow(c.status(), idAlpha).PID

	if err := leaseW.Close(); err != nil {
		t.Fatalf("close parent-death lease: %v", err)
	}
	if !waitForScratchDaemonExit(socket, 3*time.Second) {
		// RED cleanup: current production has no lease, so stop the daemon this
		// test started before reporting the missing behavior.
		c.send(rpc.Frame{Kind: rpc.FrameQuit})
		waitForCleanExit(t, socket)
		t.Fatal("closing the test runner's lease did not shut down the scratch daemon")
	}
	waitForProcessGone(t, agentPID)
}

func fileDescriptor(f *os.File) string {
	return strconv.Itoa(int(f.Fd()))
}

func waitForScratchDaemonExit(socket string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		_, socketErr := os.Stat(socket)
		_, rosterErr := os.Stat(rosterPath(socket))
		if os.IsNotExist(socketErr) && os.IsNotExist(rosterErr) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

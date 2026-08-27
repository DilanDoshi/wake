// The socket itself: listening, dialling, and the file a crashed daemon
// leaves behind. What travels over the socket is lifecycle_test.go.

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestServeAcceptsAClientAndSendsHello(t *testing.T) {
	d := startDaemon(t)

	conn, err := Dial(d.socket)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	frames, errs := rpc.ReadFrames(conn)
	defer func() {
		_ = conn.Close()
		for range frames {
		}
		<-errs
	}()

	select {
	case f := <-frames:
		if f.Kind != rpc.FrameHello {
			t.Fatalf("first frame kind = %q, want %q", f.Kind, rpc.FrameHello)
		}
	case err := <-errs:
		t.Fatalf("read: %v", err)
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for hello")
	}
}

func TestDialFailsWhenNoDaemonIsListening(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "absent.sock")
	if _, err := Dial(sock); err == nil {
		t.Fatal("want error dialing an absent socket, got nil")
	}
}

// A crashed daemon leaves its socket file behind; the next start must not
// fail with "address already in use".
func TestServeRemovesStaleSocketFile(t *testing.T) {
	sock := tempSocket(t)
	f, err := os.Create(sock)
	if err != nil {
		t.Fatalf("seed stale socket: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close stale socket: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, sock) }()

	conn := dialUntilListening(t, sock, errc)
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	cancel()
	if err := <-errc; err != nil {
		t.Errorf("Serve: %v", err)
	}
}

// The other half of the same decision, and the one that costs something if it
// is wrong. Unlinking a socket does not disturb the process bound to it, so a
// second daemon that removed the file and bound its own would leave the first
// one running, invisible, holding every agent - a fleet split across two
// daemons with one of them unreachable.
func TestServeWillNotEvictALiveDaemon(t *testing.T) {
	d := startDaemon(t)
	c := attach(t, d.socket)

	err := Serve(context.Background(), d.socket)
	if err == nil {
		t.Fatal("a second Serve on a live socket returned nil: it evicted the running daemon")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("Serve = %v, want it to say a daemon is already running", err)
	}

	// listen is asked directly, because Serve no longer reaches it in this
	// case: the daemon lock refuses first and by design. The stale-socket
	// probe is still the thing standing between "a file is in the way" and
	// "unlink whatever is there", and a test that only went through Serve
	// would stop exercising it the moment the lock landed in front.
	if ln, err := listen(d.socket); err == nil {
		_ = ln.Close()
		t.Error("listen bound over a live daemon: it removed a socket file with a process still behind it, and the fleet is now split across two daemons with one unreachable")
	}
	if _, err := os.Stat(d.socket); err != nil {
		t.Errorf("the live daemon's socket file is gone after a second start was refused: %v", err)
	}

	// The first daemon is still reachable, which is the thing that matters.
	if st := c.status(); !st.Running {
		t.Error("the original daemon stopped answering after a second one tried to start")
	}
}

// Cancelling the context has to end everything, not just the listener. A
// daemon that returned while its goroutines ran would be a process that
// cannot be restarted cleanly and a fleet nobody is watching.
func TestServeLeavesNothingRunningWhenItReturns(t *testing.T) {
	fakeClaudeOnPath(t, "")
	base := settledGoroutines()

	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.spawn(idBeta, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.awaitEvent(idBeta, "ready")

	d.cancel()
	d.waitForExit(t)
	c.close()

	waitForGoroutines(t, base)
}

// dialUntilListening polls, failing with Serve's own error if it has already
// given up - "daemon never started listening" hides a bind error, and a bind
// error is what a too-long socket path looks like.
func dialUntilListening(t *testing.T, sock string, errc <-chan error) *closer {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if c, err := Dial(sock); err == nil {
			return &closer{c}
		}
		select {
		case err := <-errc:
			t.Fatalf("Serve returned before it was listening: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon never started listening")
	return nil
}

type closer struct{ c interface{ Close() error } }

func (c *closer) Close() error { return c.c.Close() }

// settledGoroutines is the count once the runtime has stopped moving: the
// minimum over a short window, so a goroutine unwinding from an earlier test
// drags the baseline toward the truth rather than inflating it.
func settledGoroutines() int {
	lowest := runtime.NumGoroutine()
	for range 10 {
		time.Sleep(10 * time.Millisecond)
		if n := runtime.NumGoroutine(); n < lowest {
			lowest = n
		}
	}
	return lowest
}

func waitForGoroutines(t *testing.T, base int) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for {
		n := runtime.NumGoroutine()
		if n <= base {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%d goroutines still running, want at most %d - something the daemon started never returned\n%s", n, base, allStacks())
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func allStacks() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

// An over-long socket path fails at bind with EINVAL - a bare "invalid
// argument" - and the bind happens in the forked daemon, whose stderr is
// /dev/null. The client is then told only that no daemon started listening.
// Caught here, where the person who set $WAKE_SOCKET can read it.
func TestAnOverLongSocketPathIsRefusedWithAReason(t *testing.T) {
	// Under a directory this test owns. An earlier draft built the path
	// straight under os.TempDir(), which made "was it created?" an assertion
	// about a fixed global path - so the one run that did create it (a
	// mutation with the check disabled) left the test failing for every run
	// afterwards.
	dir := filepath.Join(filepath.Dir(tempSocket(t)), strings.Repeat("d", maxSocketPath))
	t.Setenv(SocketEnv, filepath.Join(dir, "daemon.sock"))

	_, err := SocketPath()
	if err == nil {
		t.Fatal("an unbindable socket path was accepted")
	}
	for _, want := range []string{"limit", SocketEnv} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("SocketPath error = %q, want it to mention %q", err, want)
		}
	}
	// And nothing was created for a path that cannot work.
	if _, serr := os.Stat(dir); !os.IsNotExist(serr) {
		t.Errorf("a directory was created for an unusable socket path: %v", serr)
	}
}

// tempSocket rather than t.TempDir: on darwin the latter is already 100 bytes
// before a filename is added, which is the same limit under test.
func TestAnOrdinarySocketPathIsAccepted(t *testing.T) {
	want := tempSocket(t)
	t.Setenv(SocketEnv, want)

	// Asserted against the path that was set. `sock == ""` was unreachable:
	// SocketPath returns ("", err) or (sock, nil), so after err == nil there is
	// nothing an empty string could come from.
	got, err := SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if got != want {
		t.Errorf("SocketPath = %q, want %q", got, want)
	}
}

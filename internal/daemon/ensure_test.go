// Start-or-attach: the first thing `wake` does, and the thing that has to be
// idempotent. Two terminals starting Wake at the same moment must end up
// talking to one daemon holding one fleet.

package daemon

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The fork is a real second process here, because that is the only way to
// find out whether the daemon survives the client that started it.
func TestEnsureRunningForksADaemonAndTheSecondCallAttachesToIt(t *testing.T) {
	fakeClaudeOnPath(t, "")
	sock := tempSocket(t)
	t.Setenv(SocketEnv, sock)
	t.Setenv(fakeDaemonEnv, "1")

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	first, err := EnsureRunning(ctx, sock)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	a := attachConn(t, first)
	a.spawn(idAlpha, "sydney")
	a.awaitEvent(idAlpha, "ready")
	one := a.status()

	// A second client, and it must not start a second daemon: two daemons on
	// one socket is a fleet split in half with one half unreachable.
	second, err := EnsureRunning(ctx, sock)
	if err != nil {
		t.Fatalf("second EnsureRunning: %v", err)
	}
	b := attachConn(t, second)
	two := b.status()

	if one.PID != two.PID {
		t.Errorf("two daemons: first %d, second %d", one.PID, two.PID)
	}
	if got := live(two); len(got) != 1 || got[0].ID != idAlpha {
		t.Errorf("the second client sees %+v, want the session the first one started", two.Sessions)
	}

	// It is a working connection, not just an open one.
	b.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "from the second client"})
	b.awaitEvent(idAlpha, "from the second client")

	b.send(rpc.Frame{Kind: rpc.FrameQuit})
	waitForCleanExit(t, sock)
}

// A daemon that is already listening must be attached to, not forked over.
func TestEnsureRunningAttachesToADaemonThatIsAlreadyThere(t *testing.T) {
	d := startDaemon(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := EnsureRunning(ctx, d.socket)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	c := attachConn(t, conn)

	if st := c.status(); st.PID != os.Getpid() {
		t.Errorf("attached to pid %d, want this process %d - it forked a second daemon over a live one", st.PID, os.Getpid())
	}
}

// The forked daemon and the client that forked it must agree on the socket
// even if they would not derive the same path. A disagreement is a hang with
// no explanation, which is the worst shape a startup bug can take.
func TestSocketPathIsWhatTheEnvironmentSays(t *testing.T) {
	// A helper rather than t.TempDir, because SocketPath now refuses a path no
	// unix socket could be bound to and t.TempDir's is already past the limit
	// on darwin. This one nests a level deeper than tempSocket to prove
	// SocketPath creates the directory, which made it one of the two longest
	// paths in the package - see TestEverySocketPathThisSuiteBuildsFitsInSunPath.
	want := tempSocketUnderMissingDir(t)
	t.Setenv(SocketEnv, want)

	got, err := SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if got != want {
		t.Errorf("SocketPath = %q, want %q", got, want)
	}
	// The directory has to exist, or the daemon fails to bind for a reason
	// that has nothing to do with the daemon.
	if _, err := os.Stat(filepath.Dir(want)); err != nil {
		t.Errorf("SocketPath did not create the directory: %v", err)
	}
}

// A machine with no daemon is an answer, not a failure. `wake status` is the
// command someone runs because something is wrong; it must not be the second
// thing that goes wrong.
func TestStatusWithNoDaemonIsNotAnError(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "absent.sock")

	st, err := Status(sock)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Running {
		t.Error("Status says a daemon is running where there is no socket at all")
	}
	if len(st.Sessions) != 0 {
		t.Errorf("Status = %+v, want no sessions", st.Sessions)
	}
}

// A status *push* is not an answer, and this is why FrameStatusPush is its own
// kind. The daemon announces a state change to every attached client the moment
// it happens, so one can already be in the socket when Status asks - and while
// replies and announcements shared a kind, Status returned that announcement: a
// report assembled before the question, stale by up to a liveness tick.
//
// The fake daemon here writes the announcement *before* the request even
// arrives, which is the case exactly: no timing, no race, the wrong frame is
// simply there first.
func TestStatusIgnoresAnAnnouncementThatPredatesTheQuestion(t *testing.T) {
	const (
		announced = 111 // the fleet as it was before the question
		answered  = 222 // the fleet as it is now
	)

	sock := tempSocket(t)
	ln := listenRaw(t, sock)

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for _, f := range []rpc.Frame{
			{Kind: rpc.FrameHello},
			{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, PID: announced}},
			{Kind: rpc.FrameStatusReply, Status: &rpc.Status{Running: true, PID: answered}},
		} {
			if err := rpc.WriteFrame(conn, f); err != nil {
				return
			}
		}
		// Read until the client hangs up, so nothing above is lost to a close
		// race and this goroutine retires with the test.
		_, _ = io.Copy(io.Discard, conn)
	}()

	st, err := Status(sock)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.PID == announced {
		t.Fatal("Status returned the announcement that was already in the socket before it asked: `wake status` would report the fleet as it was up to a liveness tick ago")
	}
	if st.PID != answered {
		t.Fatalf("Status = %+v, want the reply to its own request", st)
	}
	<-done
}

// And it must not hang. A daemon that accepted the connection and then said
// nothing would otherwise stall the one command that exists to diagnose it.
func TestStatusGivesUpOnADaemonThatDoesNotAnswer(t *testing.T) {
	sock := tempSocket(t)
	ln := listenRaw(t, sock)
	defer func() { _ = ln.Close() }()

	// Accept and say nothing at all.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		time.Sleep(2 * statusTimeout)
		_ = conn.Close()
	}()

	start := time.Now()
	if _, err := Status(sock); err == nil {
		t.Error("Status returned nil for a daemon that never answered")
	}
	if elapsed := time.Since(start); elapsed > 2*statusTimeout {
		t.Errorf("Status took %v, want it bounded by %v", elapsed, statusTimeout)
	}
}

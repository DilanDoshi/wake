// The on-disk record, and the refusals around it. Everything here is about a
// file written by a process that has since died: it has to survive being
// re-read, and every way it can be wrong has to end in "do nothing" rather
// than in a signal.

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestTheRosterSurvivesBeingWrittenAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), rosterFileName)
	r := newRosterFile(path)

	started := time.Now().Truncate(time.Second)
	if err := r.add(record{ID: idAlpha, Name: "sydney", PID: 4242, Started: started}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.add(record{ID: idBeta, Name: "alex", PID: 4243, Started: started}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.remove(idAlpha); err != nil {
		t.Fatalf("remove: %v", err)
	}

	recs := loadRoster(path)
	if len(recs) != 1 || recs[0].ID != idBeta || recs[0].PID != 4243 {
		t.Fatalf("roster = %+v, want just %s with its process group", recs, idBeta)
	}
	if !recs[0].Started.Equal(started) {
		t.Errorf("Started = %v, want %v", recs[0].Started, started)
	}

	// It names process groups, so it must not be readable by anyone else.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != rosterPerm {
		t.Errorf("roster mode = %v, want %v", perm, os.FileMode(rosterPerm))
	}

	if err := r.clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the roster is still there after clear: %v", err)
	}
	// Clearing what is already gone is what a second shutdown looks like.
	if err := r.clear(); err != nil {
		t.Errorf("second clear: %v", err)
	}
}

// retain is what the reaper writes back instead of clearing regardless: exactly
// the records it could not finish, so a later daemon can retry them. It keeps
// the named records and drops the rest, and an empty set removes the file the
// same way clear does - "nothing left to hunt" and "a roster of nothing" must
// not be two states.
func TestRetainKeepsOnlyTheNamedRecordsAndEmptyRemovesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), rosterFileName)
	r := newRosterFile(path)

	kept := record{ID: idAlpha, Name: "sydney", PID: 4242, Started: time.Now().Truncate(time.Second)}
	if err := r.retain([]record{kept, {ID: idBeta, Name: "alex", PID: 4243}}); err != nil {
		t.Fatalf("retain two: %v", err)
	}
	if err := r.retain([]record{kept}); err != nil {
		t.Fatalf("retain one: %v", err)
	}

	recs := loadRoster(path)
	if len(recs) != 1 || recs[0].ID != idAlpha || recs[0].PID != 4242 {
		t.Fatalf("roster = %+v, want just %s with its process group", recs, idAlpha)
	}

	if err := r.retain(nil); err != nil {
		t.Fatalf("retain nothing: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("retaining nothing left the roster file behind: %v", err)
	}
}

// A daemon killed mid-write leaves a half-written file. Refusing to start
// would be the wrong answer - the machine still has agents on it and the way
// to reach them is to start - so it is reported and treated as empty.
func TestAnUnreadableRosterIsReportedAndTreatedAsEmpty(t *testing.T) {
	sock := tempSocket(t)
	// Truncated mid-value, which is what a daemon SIGKILLed during a write
	// leaves. The id is interpolated rather than spelled inside the raw
	// string: a find/replace once landed the bare identifier `idAlpha` in
	// here, which still failed to parse, but for the wrong reason.
	truncated := `[{"id":"` + idAlpha + `","pid":`
	if err := os.WriteFile(rosterPath(sock), []byte(truncated), rosterPerm); err != nil {
		t.Fatalf("seed a truncated roster: %v", err)
	}

	if recs := loadRoster(rosterPath(sock)); recs != nil {
		t.Errorf("loadRoster = %+v, want nothing usable out of a truncated file", recs)
	}

	// And the daemon starts anyway.
	d := startDaemonOn(t, sock)
	c := attach(t, d.socket)
	if st := c.status(); !st.Running {
		t.Error("the daemon did not start with a corrupt roster on disk")
	}
}

// An agent that stopped reading its stdin must not be able to take the
// client's read goroutine down with it - that goroutine is carrying the kill
// frame. A full queue is an answer, not a wait.
func TestSubmittingToAnAgentThatIsNotReadingIsAnErrorRatherThanAWait(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	c := newClient(nil)

	// No serveInput goroutine, so nothing is ever taken off the queue.
	for i := range agentQueue {
		if err := a.submit(c, rpc.Frame{Kind: rpc.FrameSend, Text: "queued"}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	done := make(chan error, 1)
	go func() { done <- a.submit(c, rpc.Frame{Kind: rpc.FrameSend, Text: "one too many"}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("submit to a full queue returned nil, so the frame went nowhere and nobody was told")
		}
	case <-time.After(testTimeout):
		t.Fatal("submit blocked on a full queue: the connection carrying this client's kill frame is now stuck behind the agent it would kill")
	}

	// And once the session is over, submitting says so rather than queueing
	// for a process that no longer exists.
	a.finish(nil)
	if err := a.submit(c, rpc.Frame{Kind: rpc.FrameSend}); err == nil {
		t.Error("submit to an ended session returned nil")
	}
}

// The default path, which is what every real run uses. $HOME is redirected
// rather than trusted: a test must never create or touch the operator's own
// ~/.wake.
func TestSocketPathDefaultsUnderTheHomeDirectory(t *testing.T) {
	// A short home, for the same reason tempSocket exists: on darwin
	// t.TempDir() is already past 100 bytes before a filename is added, and
	// SocketPath now refuses a path no unix socket could be bound to. A real
	// home directory is nowhere near the limit; this one would be.
	//
	// It is the tightest path in the package - the /.wake/daemon.sock under it
	// is production's, so only the root is a test's to shorten. That is what
	// TestEverySocketPathThisSuiteBuildsFitsInSunPath measures against.
	home := tempHome(t)
	t.Setenv("HOME", home)
	t.Setenv(SocketEnv, "")

	got, err := SocketPath()
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	want := filepath.Join(home, ".wake", "daemon.sock")
	if got != want {
		t.Fatalf("SocketPath = %q, want %q", got, want)
	}

	info, err := os.Stat(filepath.Dir(want))
	if err != nil {
		t.Fatalf("the state directory was not created: %v", err)
	}
	// It holds the control channel for every agent on the machine.
	if perm := info.Mode().Perm(); perm != stateDirPerm {
		t.Errorf("~/.wake mode = %v, want %v", perm, os.FileMode(stateDirPerm))
	}
}

// A daemon asked to spawn while it is shutting down must refuse rather than
// start a process nothing will be left to supervise.
//
// Three routes out, and all three have to refuse. The last is why this is a
// table: run closes s.done when the accept loop ends and Serve then calls
// shutdown, which empties the agent map - so on the one path where neither the
// context nor a client's quit is set (an accept error that is not a clean
// stop) a spawn dispatched afterwards would start a process shutdown had
// already walked past, with nothing left to stop it.
func TestSpawnIsRefusedOnceTheDaemonIsQuitting(t *testing.T) {
	fakeClaudeOnPath(t, "")

	for _, tc := range []struct {
		name string
		stop func(*server) context.Context
	}{
		{"a client quit", func(s *server) context.Context {
			s.beginQuit(quitStop)
			return context.Background()
		}},
		{"the context was cancelled", func(*server) context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}},
		{"the accept loop already finished", func(s *server) context.Context {
			close(s.done)
			return context.Background()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(tempSocket(t))
			ctx := tc.stop(s)

			c := newClient(nil)
			s.spawn(ctx, c, rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney"})

			select {
			case f := <-c.out:
				if f.Kind != rpc.FrameError {
					t.Fatalf("frame = %+v, want an error", f)
				}
			default:
				t.Fatal("a spawn during shutdown was answered with silence")
			}
			if _, held := s.agent(idAlpha); held {
				t.Error("a session was started by a daemon that is shutting down")
			}
		})
	}
}

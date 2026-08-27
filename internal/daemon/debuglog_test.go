package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Equal names are one path, so only the session that claimed it may stay
// live. Every ending reaches retire; a park takes its branch only afterwards,
// and both have to free the name because debug config does not survive one.
func TestADebugLogNameIsHeldOnlyWhileItsSessionIsLive(t *testing.T) {
	for _, tc := range []struct {
		name string
		park bool
	}{
		{"ends", false},
		{"parks", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
			first, err := s.configFor(rpc.Frame{SessionID: idAlpha, DebugFile: "api"})
			if err != nil {
				t.Fatalf("the first session could not claim the debug log: %v", err)
			}
			first.SessionID, first.Name, first.Dir = idAlpha, "alex", t.TempDir()
			if why := s.launchRefusal(first); why != "" {
				t.Fatalf("the session that claimed the debug log was refused at launch: %s", why)
			}

			secondFrame := rpc.Frame{SessionID: idBeta, DebugFile: "api"}
			if why := s.configRefusal(secondFrame); !strings.Contains(why, `"api"`) {
				t.Fatalf("configRefusal accepted a second session or did not name its debug log: %q", why)
			}
			secondConfig := first
			secondConfig.SessionID = idBeta
			if why := s.launchRefusal(secondConfig); !strings.Contains(why, `"api"`) {
				t.Fatalf("launchRefusal accepted a second session or did not name its debug log: %q", why)
			}

			if second, err := s.configFor(secondFrame); err == nil {
				t.Fatalf("a second live session claimed %q; want a refusal naming %q", second.DebugFile, "api")
			} else if !strings.Contains(err.Error(), `"api"`) {
				t.Errorf("the refusal %q does not name the debug log", err)
			}

			_, cancel := context.WithCancel(context.Background())
			a := newAgent(first.SessionID, first.Name, "", first.Dir, "", core.NewSession(first), cancel)
			if tc.park {
				a.beginPark()
			}
			s.retire(a)

			if _, err := s.configFor(secondFrame); err != nil {
				t.Errorf("the debug log name stayed held after the session %s: %v", tc.name, err)
			}
		})
	}
}

func TestTwoRacingSessionsCannotClaimTheSameDebugLog(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{idAlpha, idBeta} {
		go func() {
			<-start
			_, err := s.configFor(rpc.Frame{SessionID: id, DebugFile: "api"})
			results <- err
		}()
	}
	close(start)

	started, refused := 0, 0
	for range 2 {
		if err := <-results; err != nil {
			refused++
			if !strings.Contains(err.Error(), `"api"`) {
				t.Errorf("the refusal %q does not name the debug log", err)
			}
			continue
		}
		started++
	}
	if started != 1 || refused != 1 {
		t.Errorf("racing claims started %d sessions and refused %d, want one of each", started, refused)
	}
}

// A session claims once. A second spawn frame carrying a live session's id is
// refused later, at admit - and if it were allowed to claim the same log here,
// its own cleanup would delete the entry and free the name under the session
// that is writing to it. That is BUG-22 again, reached through the refusal.
func TestASecondSpawnOnALiveIDCannotTakeOverItsDebugLogName(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	first, err := s.configFor(rpc.Frame{SessionID: idAlpha, DebugFile: "api"})
	if err != nil {
		t.Fatalf("the first spawn could not claim the debug log: %v", err)
	}
	if _, err := s.configFor(rpc.Frame{SessionID: idAlpha, DebugFile: "api"}); err == nil {
		t.Fatal("a second spawn under the same session id claimed the log too, so its own refusal frees the name the first one is writing to")
	}
	if !s.ownsDebugFile(idAlpha, first.DebugFile) {
		t.Error("the duplicate spawn took the claim from the session that holds it")
	}
}

func TestDebugLogClaimsTreatCaseVariantsAsOneName(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	if _, err := s.configFor(rpc.Frame{SessionID: idAlpha, DebugFile: "api"}); err != nil {
		t.Fatalf("the first session could not claim the debug log: %v", err)
	}
	if second, err := s.configFor(rpc.Frame{SessionID: idBeta, DebugFile: "API"}); err == nil {
		t.Fatalf("a case variant claimed %q beside api; the two names can be one file on macOS", second.DebugFile)
	}
}

func TestALaunchRefusalGivesTheDebugLogNameBack(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	cfg, err := s.configFor(rpc.Frame{SessionID: idAlpha, DebugFile: "api"})
	if err != nil {
		t.Fatalf("configFor: %v", err)
	}
	cfg.SessionID, cfg.Name, cfg.Dir = idAlpha, "alex", "relative"
	c := newClient(nil)
	s.launch(c, cfg, "", nil, nil)
	if got := <-c.out; got.Kind != rpc.FrameError {
		t.Fatalf("a launch with a relative directory answered with %q, want an error", got.Kind)
	}
	if _, err := s.configFor(rpc.Frame{SessionID: idBeta, DebugFile: "api"}); err != nil {
		t.Errorf("the refused launch kept its debug log name: %v", err)
	}
}

func TestARefusalBeforeLaunchGivesTheDebugLogNameBack(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	c := newClient(nil)
	s.spawn(context.Background(), c, rpc.Frame{
		Kind: rpc.FrameSpawn, SessionID: idAlpha, Dir: t.TempDir(),
		Worktree: "../escape", DebugFile: "api",
	})
	if got := <-c.out; got.Kind != rpc.FrameError {
		t.Fatalf("an invalid worktree answered with %q, want an error", got.Kind)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.socket), debugDirName)); !os.IsNotExist(err) {
		t.Errorf("the pre-launch refusal touched the debug directory: %v", err)
	}
	if _, err := s.configFor(rpc.Frame{SessionID: idBeta, DebugFile: "api"}); err != nil {
		t.Errorf("the pre-launch refusal kept its debug log name: %v", err)
	}
}

// The client chooses a name; the daemon chooses the directory. That is the
// whole fence, and it lands beside the socket where every other per-fleet file
// already is.
func TestADebugLogLandsBesideTheSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wake.sock")

	got, err := debugFilePath(socket, "alex")
	if err != nil {
		t.Fatalf("debugFilePath: %v", err)
	}
	want := filepath.Join(filepath.Dir(socket), debugDirName, "alex"+debugFileExt)
	if got != want {
		t.Errorf("the debug log is at %q, want %q", got, want)
	}
	info, err := os.Stat(filepath.Dir(got))
	if err != nil {
		t.Fatalf("the directory was not created: %v", err)
	}
	// A debug log carries this session's prompts and tool arguments, and Wake
	// creates only the directory - the file's own mode is claude's - so this is
	// the whole of the protection.
	if perm := info.Mode().Perm(); perm != debugDirPerm {
		t.Errorf("the debug directory is %o, want %o: it holds every agent's prompts and tool arguments", perm, debugDirPerm)
	}
}

// A log that cannot be placed refuses the spawn rather than starting a session
// that logs nowhere - the same ruling addWorktree makes one file over.
func TestADebugLogThatCannotBePlacedIsAnError(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "wake.sock")
	// A regular file where the directory has to go, which is what a stale fleet
	// directory or a hostile touch looks like from here.
	if err := os.WriteFile(filepath.Join(dir, debugDirName), nil, 0o600); err != nil {
		t.Fatalf("plant a file in the way: %v", err)
	}
	if got, err := debugFilePath(socket, "alex"); err == nil {
		t.Errorf("debugFilePath returned %q with a file where its directory has to be", got)
	}
}

// And the spawn that could not place it releases the name it had already
// claimed, so the next spawn can have it. Nothing else asserts that branch -
// every other refusal on this path happens before a name is claimed at all.
func TestASpawnWhoseLogCannotBePlacedGivesTheNameBack(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)

	if err := os.WriteFile(filepath.Join(filepath.Dir(d.socket), debugDirName), nil, 0o600); err != nil {
		t.Fatalf("plant a file in the way: %v", err)
	}
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(), DebugFile: "alex"})
	c.await("a refusal about the log", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "debug log")
	})

	if err := os.Remove(filepath.Join(filepath.Dir(d.socket), debugDirName)); err != nil {
		t.Fatalf("clear the way: %v", err)
	}
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idBeta, Text: "alex", Dir: t.TempDir()})
	c.await("the name going to the next spawn", func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameStatusReply || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == idBeta && s.Name == "alex" {
				return true
			}
		}
		return false
	})
}

// No name is no log, and no directory either: a fleet that logs nothing should
// leave nothing behind.
func TestNoDebugNameMakesNoPathAndNoDirectory(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wake.sock")

	got, err := debugFilePath(socket, "")
	if err != nil {
		t.Fatalf("debugFilePath: %v", err)
	}
	if got != "" {
		t.Errorf("an unnamed debug log resolved to %q", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(socket), debugDirName)); !os.IsNotExist(err) {
		t.Errorf("a fleet that logs nothing has a debug directory: %v", err)
	}
}

// The name is fenced here as well as at the client, and that is the check that
// makes the wire field safe against a client that never ran the client's code.
func TestADebugNameThatIsAPathIsRefusedByTheDaemon(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "wake.sock")
	for _, name := range []string{"../../.zshrc", "/etc/passwd", "a/b"} {
		if got, err := debugFilePath(socket, name); err == nil {
			t.Errorf("%q resolved to %q instead of being refused", name, got)
		}
	}
}

// configRefusal is the door in front of the name, the directory and the
// process: a value this build does not accept must cost none of them.
func TestConfigRefusalRefusesTheSpawnFlagsItCannotAccept(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	for _, tc := range []struct {
		name  string
		frame rpc.Frame
		names string
	}{
		{"a relative added directory", rpc.Frame{AddDir: []string{"lib"}}, "lib"},
		{"an added directory that is a flag", rpc.Frame{AddDir: []string{"-rf"}}, "-rf"},
		{"a debug filter that is not one", rpc.Frame{Debug: "api;rm", DebugFile: "alex"}, "api;rm"},
		{"a debug name that is a path", rpc.Frame{DebugFile: "../../.zshrc"}, ".zshrc"},
		{"a filter with no file to write to", rpc.Frame{Debug: "api"}, "no log anywhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			why := s.configRefusal(tc.frame)
			if why == "" {
				t.Fatalf("%+v was accepted", tc.frame)
			}
			if !strings.Contains(why, tc.names) {
				t.Errorf("the refusal %q does not name %q", why, tc.names)
			}
		})
	}
}

func TestConfigRefusalAcceptsWhatTheseFlagsAreFor(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	for _, f := range []rpc.Frame{
		{},
		{AddDir: []string{"/repo/lib", "/repo/docs"}},
		{DebugFile: "alex"},
		{Debug: "api,hooks", DebugFile: "alex"},
	} {
		if why := s.configRefusal(f); why != "" {
			t.Errorf("%+v was refused: %s", f, why)
		}
	}
}

// launchRefusal is the last door before the argv, and it holds the shapes the
// frame check cannot: a Config carrying a debug path this daemon did not
// resolve, and a filter whose file went missing between the two.
func TestLaunchRefusalHoldsTheResolvedShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  core.Config
	}{
		{"a relative directory", core.Config{Dir: "repo"}},
		{"a debug path this daemon did not resolve", core.Config{SessionID: idAlpha, DebugFile: "/fleet/debug/alex.log"}},
		{"a filter with no file behind it", core.Config{Debug: "api"}},
		{"an added directory that is a flag", core.Config{AddDir: []string{"--force"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
			if why := s.launchRefusal(tc.cfg); why == "" {
				t.Errorf("%+v reached the argv", tc.cfg)
			}
		})
	}
}

// The whole hop, over a real socket: what a client asks for is what the process
// is started with, and the log lands where the daemon put it rather than where
// the client said.
//
// This asserts the **transfer** and deliberately not the word-splitting: the
// fake reports its argv already joined by spaces, so nothing read here could
// tell one word from two. That property belongs to
// TestEveryAddedDirectoryIsItsOwnArgvWord in internal/core, which reads the
// slice by index.
func TestASpawnCarriesTheAddedDirectoriesAndTheDebugLogToTheArgv(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "alex", Dir: t.TempDir(),
		AddDir: []string{"/repo/lib", "/repo/docs"}, Debug: "api,hooks", DebugFile: "alex"})

	got := c.await("the session reporting its command line", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil &&
			strings.Contains(f.Event.Text, "argv: ")
	})
	argv := got.Event.Text
	want := []string{
		"--add-dir /repo/lib --add-dir /repo/docs",
		"--debug api,hooks",
		"--debug-file " + filepath.Join(filepath.Dir(d.socket), debugDirName, "alex"+debugFileExt),
	}
	for _, w := range want {
		if !strings.Contains(argv, w) {
			t.Errorf("the session was started as\n  %s\nand it is missing %q", argv, w)
		}
	}
}

// A value this build does not accept costs no name and no process, and it is
// refused **before the filesystem is touched at all** - which is what the
// directory assertion below is for: all three inputs are refused in
// configRefusal, and configRefusal runs before configFor, the only thing here
// that creates anything.
//
// It does not claim that no refusal ever leaves a directory. A spawn refused
// *after* configFor - losing the race for an id, or a claude that will not exec
// - leaves the fleet's empty debug directory behind, which is harmless and is
// not what this asserts.
func TestASpawnWithAPathItCannotAcceptStartsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame rpc.Frame
		says  string
	}{
		{"a relative added directory", rpc.Frame{AddDir: []string{"../lib"}}, "../lib"},
		{"a debug name that is a path", rpc.Frame{DebugFile: "../../.zshrc"}, ".zshrc"},
		{"a filter with nowhere to write", rpc.Frame{Debug: "api"}, "no log anywhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClaudeOnPath(t, "")
			d := startDaemon(t)
			c := attach(t, d.socket)

			f := tc.frame
			f.Kind, f.SessionID, f.Text, f.Dir = rpc.FrameSpawn, idAlpha, "alex", t.TempDir()
			c.send(f)
			c.await("a refusal naming what was wrong", func(r rpc.Frame) bool {
				return r.Kind == rpc.FrameError && strings.Contains(r.Text, tc.says)
			})
			if _, err := os.Stat(filepath.Join(filepath.Dir(d.socket), debugDirName)); !os.IsNotExist(err) {
				t.Errorf("a refused spawn left a debug directory behind: %v", err)
			}
		})
	}
}

func TestLaunchRefusalAcceptsAResolvedConfig(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "wake.sock"))
	cfg, err := s.configFor(rpc.Frame{
		SessionID: idAlpha,
		AddDir:    []string{"/repo/lib"},
		Debug:     "api",
		DebugFile: "alex",
	})
	if err != nil {
		t.Fatalf("configFor: %v", err)
	}
	cfg.SessionID = idAlpha
	cfg.Dir = "/repo"
	if why := s.launchRefusal(cfg); why != "" {
		t.Errorf("a resolved config was refused: %s", why)
	}
}

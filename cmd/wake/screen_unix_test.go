//go:build unix

// A real pty, the real binary, a terminal emulator behind it - so a test can
// assert on what is actually on screen.
//
// Every other test in this tree asserts on strings a test handed the model.
// None had ever looked at a rendered frame, which is why the first person to
// run the build hit three layout bugs in a minute that 2,342 tests were green
// over.

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"

	"github.com/DilanDoshi/wake/internal/core"
)

// screenTimeout bounds every wait for something to appear on screen. Generous,
// because a first run forks a daemon and spawns an agent behind it.
const screenTimeout = 20 * time.Second

var (
	buildOnce sync.Once
	buildDir  string
	buildPath string
	buildErr  error
)

// wakeBinary compiles cmd/wake once per test binary run. The real binary is the
// point: a test that re-entered this test binary would be asserting about a
// program nobody ships.
func wakeBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "wake-screen")
		if buildErr != nil {
			return
		}
		buildPath = filepath.Join(buildDir, "wake")
		out, err := exec.Command("go", "build", "-o", buildPath, ".").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatalf("build wake: %v", buildErr)
	}
	return buildPath
}

// removeWakeBinary is called by TestMain after the suite, since the build is
// shared by every screen test and outlives all of them.
func removeWakeBinary() {
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
}

type screen struct {
	t    *testing.T
	cmd  *exec.Cmd
	ptmx *os.File
	term vt10x.Terminal
	cols int
	rows int
}

// startWake runs the real binary in a pty of exactly this size.
//
// The caller must have called withScriptedAgent: the child inherits this
// process's environment, which is where the fake `claude` and the scratch
// WAKE_SOCKET live.
func startWake(t *testing.T, cols, rows int, args ...string) *screen {
	t.Helper()
	return startWakeIn(t, "", cols, rows, args...)
}

// startWakeIn is startWake from a chosen working directory, and the whole of
// what the directory changes is what the operator sees: the status bar's path,
// the branch beside it, and where a spawned agent starts. An empty dir keeps
// exec's own default, which is this package's directory - a short path on
// whatever branch the checkout is on, and the environment every fixture here
// used before realistic_unix_test.go existed.
func startWakeIn(t *testing.T, dir string, cols, rows int, args ...string) *screen {
	t.Helper()

	socket := os.Getenv("WAKE_SOCKET")
	if socket == "" || !strings.HasPrefix(socket, os.TempDir()) {
		t.Fatalf("WAKE_SOCKET must be a scratch path, got %q - the default is the owner's real fleet", socket)
	}

	bin := wakeBinary(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if testParentLeaseRead != nil {
		childFD := 3 + len(cmd.ExtraFiles)
		cmd.ExtraFiles = append(cmd.ExtraFiles, testParentLeaseRead)
		cmd.Env = testLeaseEnv(cmd.Env, childFD)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		t.Fatalf("start wake in a pty: %v", err)
	}

	s := &screen{
		t:    t,
		cmd:  cmd,
		ptmx: ptmx,
		term: vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(ptmx)),
		cols: cols,
		rows: rows,
	}

	go func() {
		br := bufio.NewReader(ptmx)
		for {
			if err := s.term.Parse(br); err != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = ptmx.Close()
		// The daemon this forked outlives the client. It holds the scratch
		// socket asserted above, so nothing here can reach a real fleet.
		stop := exec.Command(bin, "stop")
		stop.Env = cmd.Env
		stop.Dir = dir
		_ = stop.Run()
	})
	return s
}

func testLeaseEnv(env []string, fd int) []string {
	prefix := testParentLeaseSourceEnv + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, fmt.Sprintf("%s=%d", testParentLeaseSourceEnv, fd))
}

// startWakeInAConversation is `wake new`: an agent, and its conversation with
// the keys - which is what bare `wake` did on first run until openroom.go
// stopped opening a pane nobody had asked for.
//
// Nearly every test in this suite is about a conversation rather than about the
// front door, and each one used to reach one by being the first `wake` on a
// scratch socket. This is the verb that still means "an agent, and put me in
// it", so those tests kept exactly the state they were written against instead
// of gaining setup apiece. The ones that *are* about the front door call
// startWake directly - openroomscreen_unix_test.go, and the second window in
// the two reopen tests.
func startWakeInAConversation(t *testing.T, cols, rows int) *screen {
	t.Helper()
	return startWake(t, cols, rows, cmdNew)
}

// send writes raw key bytes, which is what a keyboard sends.
func (s *screen) send(keys string) {
	s.t.Helper()
	if _, err := s.ptmx.WriteString(keys); err != nil {
		s.t.Fatalf("write %q to the pty: %v", keys, err)
	}
}

// lines is the screen as text, one string per row, right-trimmed.
func (s *screen) lines() []string {
	s.term.Lock()
	defer s.term.Unlock()

	out := make([]string, s.rows)
	for y := range s.rows {
		var b strings.Builder
		for x := range s.cols {
			b.WriteRune(s.term.Cell(x, y).Char)
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	return out
}

func (s *screen) text() string { return strings.Join(s.lines(), "\n") }

// await blocks until want is on screen, and fails with the screen if it is not.
func (s *screen) await(want string) {
	s.t.Helper()
	deadline := time.Now().Add(screenTimeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("%q never appeared on screen.\n%s", want, s.dump())
}

// awaitGone blocks until want is off screen, and fails with the screen if it
// is still there.
//
// The negative of await, and it exists because settle answers a different
// question. settle waits for the frame to stop moving, which is not the same
// as waiting for a verb to have finished: a notice line is the last thing the
// client knows, so the frame can be still while the roster it is drawn from is
// one report behind. An assertion about a string a *pending* verb will remove
// has to wait for the removal, not for quiet.
//
// Use settle for a string that should never appear at all - there is nothing
// to wait for there, and no amount of waiting would decide it.
//
// **Only meaningful once want is known to be on screen**, because this answers
// "is it gone now" rather than "did it appear and then leave": called before the
// string has ever been drawn it returns on the first poll and asserts nothing.
// Its one caller awaits "parked" before parking is asked for. Not the
// awaitGone(pid, within) in detach_unix_test.go, which waits on a process.
func (s *screen) awaitGone(want string) {
	s.t.Helper()
	deadline := time.Now().Add(screenTimeout)
	for time.Now().Before(deadline) {
		if !strings.Contains(s.text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("%q never left the screen.\n%s", want, s.dump())
}

// awaitCount blocks until want is on screen exactly n times.
//
// The third member of the family, and the one whose absence has cost this
// package two flakes. await answers "has it appeared" and awaitGone "has it
// left"; neither can express what a *fan-out* needs. N agents are asked one
// thing, so the reply belongs on screen N times, and the first one arriving
// says nothing about the rest. Reaching for settle there asserts the count the
// moment the frame stops moving, which is after the first reply and not after
// the last - docs/notes/bugs.md BUG-7.
//
// **It waits for equality rather than for "at least n".** A count that
// overshoots is a real failure - a broadcast echoed twice, one agent answering
// into two panes - and a >= test passes over exactly that. So a deadline
// reached with too *many* on screen fails as loudly as too few, and the message
// says which it was.
//
// awaitGone's precondition, one shape over: this asks "is the count n now", so
// a caller that has not yet caused any of them and passes n=0 returns on the
// first poll having asserted nothing. Every caller today is waiting to reach a
// count it has already asked for.
func (s *screen) awaitCount(want string, n int) {
	s.t.Helper()
	deadline := time.Now().Add(screenTimeout)
	got := -1
	for time.Now().Before(deadline) {
		if got = strings.Count(s.text(), want); got == n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("%q is on screen %d times, want %d.\n%s", want, got, n, s.dump())
}

// settle waits for the frame to stop changing, for assertions about what is
// *not* there - which no amount of waiting for a string can decide.
func (s *screen) settle() {
	s.t.Helper()
	prev := ""
	for deadline := time.Now().Add(screenTimeout); time.Now().Before(deadline); {
		time.Sleep(80 * time.Millisecond)
		if now := s.text(); now == prev {
			return
		} else {
			prev = now
		}
	}
	s.t.Fatalf("the frame never settled.\n%s", s.dump())
}

// click presses the left button at a cell, in the SGR encoding, whose
// coordinates are 1-based.
func (s *screen) click(x, y int) {
	s.t.Helper()
	s.send(fmt.Sprintf("\x1b[<0;%d;%dM", x+1, y+1))
	s.send(fmt.Sprintf("\x1b[<0;%d;%dm", x+1, y+1))
}

// rosterNames reads the activity sidebar off the screen, in the order it is
// drawn - which is attention order, and is what ↑↓ walks.
func (s *screen) rosterNames() []string {
	var out []string
	for _, line := range s.lines() {
		if m := rosterRow.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// Anchored on the sidebar's own left border (or the start of the dump) rather
// than on the glyph alone: `·` is also StateEnded's own state glyph, but it is
// bare in the room's own target line too - "→ @robin · direct" - which a
// fresh `/new` now leaves drawn (see starts.go's draftMention), and an
// unanchored match read "direct" off that line as a roster row. headLine
// starts a row at column 0 of the sidebar, which is always immediately after
// the divider - so requiring that adjacency is exact rather than heuristic.
var rosterRow = regexp.MustCompile(`(?:^|│)[●◐○▪·] ([a-z][a-z0-9-]*)`)

// agentsOnRoster is every roster row that is not the manager, in screen order.
//
// Every room starts a manager now, so a test that made two agents sees three
// rows. The service is filtered out rather than counted, because what these
// tests set up is agents and what they assert about is agents; asserting on
// three would be asserting on a number rather than on a fleet.
func agentsOnRoster(s *screen) []string {
	var out []string
	for _, name := range s.rosterNames() {
		if name != core.ManagerName {
			out = append(out, name)
		}
	}
	return out
}

// pickRoster walks the roster cursor from one named row onto another with ↓.
//
// The count is read off the screen rather than assumed to be one. Two things
// made "↓ to the other agent" stop naming a row a test can predict: every room
// holds a manager as well as the agents a test made, and it sits among them in
// arrival order - and Roster.Move **wraps**, so ↑ from the top row, a no-op
// while a roster had a single row, now lands on the service instead.
func (s *screen) pickRoster(from, to string) {
	s.t.Helper()
	rows := s.rosterNames()
	at, want := slices.Index(rows, from), slices.Index(rows, to)
	if at < 0 || want < 0 {
		s.t.Fatalf("the roster is %v and has no row for %q or %q.\n%s", rows, from, to, s.dump())
	}
	for range ((want-at)%len(rows) + len(rows)) % len(rows) {
		s.send("\x1b[B")
	}
	s.settle()
}

// openAgent opens one agent's conversation by clicking its roster row.
//
// By name rather than ⌃D on whoever ranks first: a fleet of equally idle agents
// is ranked stably, so the top row is whatever order the daemon's own map
// happened to iterate in when it assembled the report - and the manager is in
// that draw now. ⌃D on "the first one" was a coin flip the moment the room
// started seating a service.
func (s *screen) openAgent(name string) {
	s.t.Helper()
	row := s.rowOf("○ " + name)
	if row < 0 {
		s.t.Fatalf("no roster row for %q.\n%s", name, s.dump())
	}
	s.click(s.cols-3, row)
	s.await("@" + name)
}

// rowOf is the first screen row containing text, or -1.
func (s *screen) rowOf(text string) int {
	for y, line := range s.lines() {
		if strings.Contains(line, text) {
			return y
		}
	}
	return -1
}

// resize is a window drag: the kernel signals the child and the emulator has to
// be told too, or the assertion is against a grid nobody is drawing on.
func (s *screen) resize(cols, rows int) {
	s.t.Helper()
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		s.t.Fatalf("resize the pty: %v", err)
	}
	s.term.Resize(cols, rows)
	s.cols, s.rows = cols, rows
}

// agentName reads the handle out of the conversation's own composer border,
// which is where the pane's name is drawn. The pool assigns it, so a test
// cannot know it in advance.
func (s *screen) agentName() string {
	s.t.Helper()
	s.await("╭")
	for _, line := range s.lines() {
		if !strings.Contains(line, "╭") {
			continue
		}
		if m := paneTitle.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	}
	s.t.Fatalf("no conversation name on a composer border.\n%s", s.dump())
	return ""
}

var paneTitle = regexp.MustCompile(`@([a-z][a-z0-9-]*) `)

// dump renders the screen with a column ruler, for failure messages. A layout
// bug is unreadable without one.
func (s *screen) dump() string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %dx%d ---\n", s.cols, s.rows)
	for y, line := range s.lines() {
		fmt.Fprintf(&b, "%2d|%s\n", y, line)
	}
	return b.String()
}

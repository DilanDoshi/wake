package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

// --- the feature ---------------------------------------------------------

// The whole path in one test: a draft that starts with a bang runs a real
// command, in a real directory, and what it printed reaches the conversation it
// was typed into.
//
// The file is read by absolute path because a bang dispatched through the App
// runs where this process runs - see App.bangDir - and a test may not chdir.
// bangRun's own use of that directory is asserted separately below, where the
// path can be relative and the mutation is visible.
func TestABangRunsLocallyAndItsOutputLandsInTheConversation(t *testing.T) {
	fresh(t)
	note := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(note, []byte("hello from a real file\n"), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}

	a := bangApp(t)
	a = dispatch(t, a, "!cat "+note)

	if !strings.Contains(shown(a), "hello from a real file") {
		t.Errorf("the command ran and its output is not in the conversation:\n%s", shown(a))
	}
}

// The directory is the session's, not Wake's, and a relative path is the only
// thing that can tell the difference. Deleting cmd.Dir leaves `cat note.txt`
// looking in the package directory, where there is no such file.
func TestABangRunsWhereTheSessionRunsRatherThanWhereWakeDoes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("read relatively\n"), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}

	msg := runBangSync(t, dir, "cat note.txt")
	if !strings.Contains(msg.Text, "read relatively") {
		t.Errorf("output = %q, want the file the session's own directory holds", msg.Text)
	}
}

func TestACommandThatNeverReturnsIsBoundedRatherThanHangingTheApp(t *testing.T) {
	start := time.Now()
	msg := runBangSyncWithTimeout(t, t.TempDir(), "sleep 600", 200*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("a `!sleep 600` held for %v. Bubble Tea runs a tea.Cmd on its own goroutine so the UI survives - but an unbounded one is a goroutine per bang for the life of the process, in an app whose whole claim is that it is cheap to leave open", elapsed)
	}
	if !strings.Contains(msg.Text, "timed out") {
		t.Errorf("a timed-out command reported %q, want something naming the timeout: silence would read as a command that produced nothing", msg.Text)
	}
}

// The bound has to reach what the command started, not only the shell. `sleep
// 600 | cat` is two processes and the shell is neither of them; killing one pid
// leaves the rest running, holding the pipe Wake reads and holding a slot in a
// laptop that is meant to be able to run thirty agents.
//
// The background job says it survived by writing a file a second after the
// deadline. Nothing else in this package can create it - the directory is this
// test's own, made fresh per call, which is what keeps a previous run of the
// mutation battery from poisoning the next clean one.
func TestATimedOutBangKillsWhatTheCommandLeftBehind(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")

	msg := runBangSyncWithTimeout(t, dir, "(sleep 1; touch survived) & sleep 600", 150*time.Millisecond)
	if !strings.Contains(msg.Text, "timed out") {
		t.Fatalf("the fixture did not reach the timeout at all, so this test asserted nothing about the kill: %q", msg.Text)
	}

	time.Sleep(1300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("a background job outlived the bang that started it: the deadline killed one pid instead of the process group, so `!make test &` leaves the build running with nobody holding a handle to it")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat the marker: %v", err)
	}
}

// A command can finish and still leave its output pipe open: `(sleep 5 &)`
// exits at once and hands the descriptor to something that does not. Waiting
// for EOF there waits for the background job, which is unbounded by
// construction - so the wait is bounded too, and the transcript says why rather
// than presenting a possibly-partial capture as the whole answer.
//
// The delay is passed in for the reason the timeout is above: what is under
// test is that the wait ends and is explained, not how many seconds the
// constant holds.
func TestABangDoesNotWaitForSomethingTheCommandLeftRunning(t *testing.T) {
	start := time.Now()
	text := bangRunWithin(t, 3*time.Second, t.TempDir(), "(sleep 5 &) ; echo started", 5*time.Second, 200*time.Millisecond)

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("a command that had already exited held the bang for %v, waiting on a pipe a background job still owns", elapsed)
	}
	if !strings.Contains(text, "started") {
		t.Errorf("output = %q, want what the command actually printed before it left", text)
	}
	if !strings.Contains(text, "held its output open") {
		t.Errorf("output = %q: the capture may be partial and nothing says so, which is a wrong transcript rather than a short one", text)
	}
}

// The wait bound can return after the shell exits while a background job from
// that successful command is still alive. The group kill must run on this path
// too, not only when the command's deadline fires.
func TestABangReclaimsWhatACommandLeftBehindAfterItExits(t *testing.T) {
	dir := t.TempDir()
	text := bangRunWithin(t, 3*time.Second, dir, "(sh -c 'echo $$ > background.pid; exec sleep 45' &) ; sleep 0.3; echo started", 5*time.Second, 200*time.Millisecond)
	if !strings.Contains(text, "held its output open") {
		t.Fatalf("the fixture did not leave a pipe open after the shell exited, so this test asserted nothing about the success-path kill: %q", text)
	}

	rawPID, err := os.ReadFile(filepath.Join(dir, "background.pid"))
	if err != nil {
		t.Fatalf("read the backgrounded fixture's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatalf("parse the backgrounded fixture's pid: %v", err)
	}
	if pid <= 1 {
		t.Fatalf("backgrounded fixture recorded unsafe pid %d", pid)
	}
	fixtureGone := false
	t.Cleanup(func() {
		if fixtureGone {
			return
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("clean up backgrounded fixture %d: %v", pid, err)
		}
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			fixtureGone = true
			break
		}
		if err != nil {
			t.Fatalf("check whether backgrounded child %d survived: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("backgrounded child %d is still alive after bangRun returned", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A background job whose output is redirected does not hold the pipe open, so
// nothing reports the abandonment - and before this line the reclaim under it
// was silent too, leaving a transcript identical to a bang that started
// nothing. A bound that cut something says so: this file's own rule.
func TestABangSaysWhenItReclaimedSomethingTheCommandLeftRunning(t *testing.T) {
	dir := t.TempDir()
	text := bangRunWithin(t, 5*time.Second, dir, "(sh -c 'echo $$ > background.pid; exec sleep 45' >/dev/null 2>&1 &) ; sleep 0.3; echo started", 5*time.Second, 200*time.Millisecond)
	if strings.Contains(text, "held its output open") {
		t.Fatalf("the fixture held its output pipe open, so this is not the silent shape the test was written for: %q", text)
	}

	rawPID, err := os.ReadFile(filepath.Join(dir, "background.pid"))
	if err != nil {
		t.Fatalf("read the backgrounded fixture's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil || pid <= 1 {
		t.Fatalf("parse the backgrounded fixture's pid %q: %v", rawPID, err)
	}
	t.Cleanup(func() {
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("clean up backgrounded fixture %d: %v", pid, err)
		}
	})

	if !strings.Contains(text, bangReclaimed) {
		t.Errorf("output = %q: a process the command left running was killed and the transcript does not say so", text)
	}
}

// And a command that leaves an empty group says nothing, or the line is on
// every bang there is. `/bin/echo` rather than the builtin on purpose: it forks
// a real child the shell then reaps, so this exercises the empty-group answer
// instead of a shell that never forked at all.
func TestABangThatLeavesAnEmptyGroupSaysNothingAboutIt(t *testing.T) {
	for _, line := range []string{"echo hi", "/bin/echo hi", "/bin/echo a | /usr/bin/wc -l"} {
		text := bangRunWithin(t, 3*time.Second, t.TempDir(), line, 5*time.Second, 200*time.Millisecond)
		if strings.Contains(text, bangReclaimed) {
			t.Errorf("`!%s` produced %q: its group was empty and it was told something was killed", line, text)
		}
	}
}

// The second assertion is against the fixture's own volume rather than against
// bangMaxBytes, and that is the whole difference between a guard and a
// restatement: written as `len > bangMaxBytes+1024` alone, raising the cap to
// 1<<30 moves the assertion with it and only the "truncated" line fires -
// measured, that is exactly what the first version of this test did.
func TestACommandThatWritesMegabytesIsCutAndSaysSo(t *testing.T) {
	const flood = 200000

	msg := runBangSync(t, t.TempDir(), fmt.Sprintf("yes hello | head -c %d", flood))
	if len(msg.Text) > bangMaxBytes+1024 {
		t.Errorf("output was %d bytes against a cap of %d. The DM's transcript is unbounded for the life of the session, so an unbounded bang is a permanent pin on that memory", len(msg.Text), bangMaxBytes)
	}
	if len(msg.Text) >= flood {
		t.Errorf("output was %d bytes of the %d the command wrote: nothing was cut at all", len(msg.Text), flood)
	}
	if !strings.Contains(msg.Text, "truncated") {
		t.Error("output was cut with nothing saying so, which is a wrong transcript rather than a short one")
	}
}

func TestAFailingCommandReportsItsExitAndItsStderr(t *testing.T) {
	msg := runBangSync(t, t.TempDir(), "ls /definitely/not/here")

	// The path rather than the message: every ls names the operand it could
	// not reach, and only some of them do it in English.
	if !strings.Contains(msg.Text, "/definitely/not/here") {
		t.Errorf("stderr was swallowed: %q", msg.Text)
	}
	// The status without its number, because ls answers 1 on BSD and 2 on GNU
	// for the same missing operand. Zero is the one answer that would be wrong.
	if !strings.Contains(msg.Text, "exited") || strings.Contains(msg.Text, "exited 0") {
		t.Errorf("a command that failed reported %q, want its exit status: a bang that failed and a bang that printed a warning look identical without it", msg.Text)
	}
}

// A command that says nothing and fails says something. Without this, `!false`
// is a blank turn in the conversation, which reads as a command that worked.
func TestASilentFailureIsStillReported(t *testing.T) {
	msg := runBangSync(t, t.TempDir(), "exit 3")
	if !strings.Contains(msg.Text, "exited 3") {
		t.Errorf("a silent failure reported %q, want its status", msg.Text)
	}
}

// A command a signal ended has no exit status to name, and "exited -1" is a
// number nobody can look up. What Go said is used instead.
func TestACommandASignalEndedSaysThatRatherThanAStatus(t *testing.T) {
	msg := runBangSync(t, t.TempDir(), "kill -9 $$")
	if strings.Contains(msg.Text, "-1") {
		t.Errorf("a signalled command reported %q, and no exit status reads as -1", msg.Text)
	}
	if !strings.Contains(msg.Text, "signal") {
		t.Errorf("a signalled command reported %q, want what ended it", msg.Text)
	}
}

// The failure that is not an exit at all: nothing started. It is reported in
// Go's own words because there is no status to report instead, and a bang that
// could not run must not read as one that ran and said nothing.
func TestACommandThatCouldNotStartIsReportedInFull(t *testing.T) {
	if got := bangFailure(errors.New("no shell to run it with")); !strings.Contains(got, "no shell to run it with") {
		t.Errorf("bangFailure = %q, want the reason it could not run", got)
	}
}

func TestOnlyALeadingBangIsACommand(t *testing.T) {
	cases := map[string]struct {
		cmdline string
		want    bool
	}{
		"!ls":            {"ls", true},
		"  !ls":          {"ls", true},
		"\t!ls -la | wc": {"ls -la | wc", true},
		"!  spaced  ":    {"spaced", true},
		"tell me !ls":    {"", false},
		"!":              {"", false},
		"! ":             {"", false},
		"what does ! do": {"", false},
		"":               {"", false},
	}
	for in, want := range cases {
		cmdline, got := isBang(in)
		if got != want.want {
			t.Errorf("isBang(%q) = %v, want %v", in, got, want.want)
		}
		if cmdline != want.cmdline {
			t.Errorf("isBang(%q) took %q as the command, want %q", in, cmdline, want.cmdline)
		}
	}
}

// --- the conversation ----------------------------------------------------

// The composer is cleared on dispatch and what is typed next survives the
// result landing. Both halves matter and the second is the one that is easy to
// get wrong: Composers share a text area by pointer and a DM is a value, so a
// result folded into a copy captured when the command started would take the
// next draft with it.
func TestTheDraftSurvivesACommandThatIsStillRunning(t *testing.T) {
	fresh(t)
	a := bangApp(t)
	a = typeText(a, "!sleep 5").(App)

	a, cmd, ok := a.bang(a.dm().Composer().Value())
	if !ok || cmd == nil {
		t.Fatalf("a bang was not taken as a local command: took=%v cmd=%v", ok, cmd != nil)
	}
	if got := a.dm().Composer().Value(); got != "" {
		t.Errorf("draft = %q after dispatching a bang, want cleared", got)
	}

	a = typeText(a, "the next thing").(App)
	a = a.bangResult(bangResultMsg{ID: a.sessionID, Cmd: "sleep 5", Text: "done"})

	if got := a.dm().Composer().Value(); got != "the next thing" {
		t.Errorf("draft = %q once the command finished: the result was folded into a stale copy and took what was typed meanwhile with it", got)
	}
	if !strings.Contains(shown(a), "done") {
		t.Errorf("the result never reached the conversation it was typed into:\n%s", shown(a))
	}
}

// An ordinary message is not a command, and the bang path must hand it back
// untouched - draft included. Swallowing one here would lose a message a person
// meant to send, which is worse than any of the failures the rest of this file
// is about.
func TestAnOrdinaryDraftIsNotACommand(t *testing.T) {
	fresh(t)
	a := bangApp(t)
	a = typeText(a, "ask the agent about ! marks").(App)

	a, cmd, ok := a.bang(a.dm().Composer().Value())
	if ok || cmd != nil {
		t.Errorf("an ordinary message was taken as a local command: took=%v cmd=%v", ok, cmd != nil)
	}
	if got := a.dm().Composer().Value(); got != "ask the agent about ! marks" {
		t.Errorf("draft = %q, want the message left exactly where it was", got)
	}
}

// A bang goes nowhere near the socket. Without the interception it is an
// ordinary message and the model answers as if it had been asked about the
// command, which is what it does today.
//
// Asserted on what the daemon's side of the connection received, not on the
// absence of a call. What this cannot yet reach is the Enter key itself:
// App.submit is where the interception has to sit and it belongs to another
// change - see the task report.
func TestABangIsNotSentToTheAgent(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex")
	m, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	a = m.(App)

	a, cmd, ok := a.bang("!echo not for the agent")
	if !ok {
		t.Fatal("a bang was not taken as a local command")
	}
	a = a.bangResult(runBangMsg(t, cmd))

	select {
	case f := <-sent:
		t.Errorf("a bang reached the daemon as %v %q. That is what happens today without an interception, and the model answers as if it had been asked about the command", f.Kind, f.Text)
	case <-time.After(250 * time.Millisecond):
	}
	if !strings.Contains(shown(a), "not for the agent") {
		t.Errorf("the output went neither to the agent nor to the conversation:\n%s", shown(a))
	}
}

// The result is addressed, and a DM shows only its own. Today the id can only
// be this App's; the guard is what keeps that true when a window holds thirty
// conversations rather than one.
func TestAResultForAnotherConversationDoesNotLandInThisOne(t *testing.T) {
	fresh(t)
	a := bangApp(t)
	a = a.bangResult(bangResultMsg{ID: "somebody-else", Cmd: "ls", Text: "another agent's directory"})

	if strings.Contains(shown(a), "another agent's directory") {
		t.Errorf("output typed into one conversation appeared in another:\n%s", shown(a))
	}
}

// It enters as an echoed turn rather than as something the operator said: the
// muted label is the DM's own way of saying "this is a record, not a message",
// and it is the shape the CLI itself uses for a local command's output.
func TestABangEntersTheConversationAsAnEchoedTurn(t *testing.T) {
	ev := bangEvent(bangResultMsg{ID: "s1", Cmd: "ls", Text: "note.txt"})
	if ev.Kind != core.KindUserText || !ev.Echoed || ev.SessionID != "s1" {
		t.Errorf("event = %+v, want an echoed user turn addressed to s1", ev)
	}

	d := NewDM("s1", "alex").SetSize(80, 24).Append(ev)
	if !strings.Contains(visible(d, 80, 24), echoedLabel) {
		t.Errorf("a bang was drawn as the operator's own message:\n%s", visible(d, 80, 24))
	}
}

// What the command printed is what the conversation shows. An echoed user turn
// is rendered as markdown, and markdown joins consecutive lines into a
// paragraph - so three files from `!ls` arrive as one line of three words
// unless the block says it is preformatted.
func TestTheLinesACommandPrintedStayLines(t *testing.T) {
	fresh(t)
	a := bangApp(t)
	a = dispatch(t, a, `!printf 'alpha\nbeta\n'`)

	first, second := lineOf(shown(a), "alpha"), lineOf(shown(a), "beta")
	if first < 0 || second < 0 {
		t.Fatalf("the output is not in the conversation at all:\n%s", shown(a))
	}
	if first == second {
		t.Errorf("two lines of output were reflowed onto one:\n%s", shown(a))
	}
}

// A command's output is not markdown and a fence inside it must not end the
// block early - `!cat README.md` is an ordinary thing to type.
func TestOutputCarryingItsOwnFenceStaysInsideTheBlock(t *testing.T) {
	body := "```\nfenced\n```"
	block := bangBlock(body)

	if !strings.HasPrefix(block, "````") {
		t.Errorf("block opened with %q, want a fence longer than the one in the output", strings.SplitN(block, "\n", 2)[0])
	}
	if !strings.Contains(block, body) {
		t.Errorf("the output was altered to fit the block:\n%s", block)
	}
}

// docs/notes/bugs.md BUG-9. A command's output is drawn, and it was drawn with
// its escape sequences intact: `!cat` on a file somebody else wrote - or any
// command that prints back what it was given - reached the operator's terminal
// directly. OSC 52 sets their clipboard with no keystroke, and CSI clears the
// alt screen Wake is drawing on and homes the cursor, which is enough to erase
// the frame and forge one in its place.
//
// Asserted from a real command rather than from a literal, because the fence is
// in bangRun - where a child's bytes first become a Wake string - and a fence
// moved off that path has to fail here.
func TestACommandsOutputCannotDriveTheTerminal(t *testing.T) {
	fresh(t)
	dir := t.TempDir()
	const hostile = "before\x1b]52;c;cHduZWQ=\amiddle\x1b[2J\x1b[H\u009b2Jafter"
	hostileFile := filepath.Join(dir, "hostile.txt")
	if err := os.WriteFile(hostileFile, []byte(hostile), 0o600); err != nil {
		t.Fatalf("write the fixture: %v", err)
	}

	msg := runBangSync(t, dir, "cat hostile.txt")
	if i := strings.IndexFunc(msg.Text, actsOnATerminal); i >= 0 {
		t.Errorf("a command's output keeps a character a terminal acts on, at %d: %q", i, msg.Text)
	}
	if block := bangEvent(msg).Text; strings.IndexFunc(block, actsOnATerminal) >= 0 {
		t.Errorf("the block that enters the transcript keeps one: %q", block)
	}
	// Substituted rather than deleted: what the command printed either side of
	// the escapes is still there to read.
	for _, word := range []string{"before", "middle", "after"} {
		if !strings.Contains(msg.Text, word) {
			t.Errorf("containment ate the output around the escapes, losing %q: %q", word, msg.Text)
		}
	}

	// And the drawn frame carries none of it either. The frame cannot be
	// asserted escape-free - Wake's own colour is escapes - so this names two
	// bytes nothing in Wake emits and only a child could have put there. It is
	// the weaker half deliberately: glamour colours a fenced block token by
	// token, which happens to break the OSC apart, so the contiguous sequence
	// is absent for a reason that has nothing to do with a fence.
	a := dispatch(t, bangApp(t), "!cat "+hostileFile)
	for _, b := range []rune{'\a', '\u009b'} {
		if strings.ContainsRune(a.View(), b) {
			t.Errorf("%q reached the terminal from a command's output", b)
		}
	}
}

// actsOnATerminal is these tests' own predicate, written out rather than
// reached through core's fence: a class narrowed by mistake would narrow the
// assertion with it. core's contain_test.go states that rule and is why.
// mcppanel_test.go's own BUG-9 test reads it from here.
func actsOnATerminal(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\u2028' || r == '\u2029'
}

// --- the bounds themselves -----------------------------------------------

// Past the cap the writer keeps claiming whole writes. Refusing them makes
// os/exec stop reading, which fills the pipe and blocks the command until the
// deadline kills it - so a `!yes | head -c 200000` that finished in
// milliseconds would report a thirty-second timeout.
func TestOutputPastTheCapIsDroppedRatherThanRefused(t *testing.T) {
	o := &bangOutput{limit: 4}
	for _, chunk := range []string{"ab", "cdef", "gh"} {
		n, err := o.Write([]byte(chunk))
		if n != len(chunk) || err != nil {
			t.Fatalf("Write(%q) = (%d, %v), want the whole slice claimed and no error", chunk, n, err)
		}
	}
	if o.String() != "abcd" {
		t.Errorf("kept %q, want the first 4 bytes", o.String())
	}
	if !o.truncated {
		t.Error("bytes were dropped and nothing recorded it")
	}
}

// --- helpers -------------------------------------------------------------

// bangApp is a sized App with one conversation and no daemon behind it.
func bangApp(t *testing.T) App {
	t.Helper()
	m, _ := dmApp(nil, Stream{}, "s1", "alex").Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m.(App)
}

// dispatch types a draft, runs the bang it is, and folds the result - the three
// steps Update makes, in the order it makes them.
func dispatch(t *testing.T, a App, draft string) App {
	t.Helper()
	a = typeText(a, draft).(App)
	a, cmd, ok := a.bang(a.dm().Composer().Value())
	if !ok {
		t.Fatalf("%q was not taken as a local command", draft)
	}
	return a.bangResult(runBangMsg(t, cmd))
}

// runBangMsg runs one dispatched command and returns its message, bounded so a
// command that never answers fails this test rather than the whole package.
func runBangMsg(t *testing.T, cmd tea.Cmd) bangResultMsg {
	t.Helper()
	msg, ok := within(t, bangTestLimit, cmd)
	if !ok {
		t.Fatalf("a bang produced no result within %v", bangTestLimit)
	}
	return msg
}

// runBangSync runs one command the way Update would and returns the message.
func runBangSync(t *testing.T, dir, cmdline string) bangResultMsg {
	t.Helper()
	return runBangSyncWithTimeout(t, dir, cmdline, bangTimeout)
}

// runBangSyncWithTimeout is the same with the deadline the caller wants, which
// is how a test reaches the timeout without waiting thirty seconds for it.
func runBangSyncWithTimeout(t *testing.T, dir, cmdline string, timeout time.Duration) bangResultMsg {
	t.Helper()
	msg, ok := within(t, bangTestLimit, runBang("s1", dir, cmdline, timeout))
	if !ok {
		t.Fatalf("`!%s` produced no result within %v: the bound did not hold", cmdline, bangTestLimit)
	}
	if msg.Cmd != cmdline {
		t.Errorf("the result names %q, want the command that produced it", msg.Cmd)
	}
	return msg
}

// bangRunWithin runs the body directly, so a test can drive the wait delay as
// well as the deadline, and fails rather than hangs if it does not return.
func bangRunWithin(t *testing.T, limit time.Duration, dir, cmdline string, timeout, waitDelay time.Duration) string {
	t.Helper()
	out := make(chan string, 1)
	go func() { out <- bangRun(dir, cmdline, timeout, waitDelay) }()
	select {
	case text := <-out:
		return text
	case <-time.After(limit):
		t.Fatalf("`!%s` produced nothing within %v: it is waiting on something with no bound on it", cmdline, limit)
		return ""
	}
}

// within runs a tea.Cmd off the test goroutine and gives up rather than
// hanging. A mutation that removes a bound must fail this suite in a line
// somebody reads, not park it until the package timeout prints a stack dump -
// those two look identical in a summary.
func within(t *testing.T, limit time.Duration, cmd tea.Cmd) (bangResultMsg, bool) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run: nothing was dispatched")
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		res, ok := msg.(bangResultMsg)
		if !ok {
			t.Fatalf("a bang produced %T, want a result addressed to a conversation", msg)
		}
		return res, true
	case <-time.After(limit):
		return bangResultMsg{}, false
	}
}

// bangTestLimit is how long any of these may take before the test says so. Far
// longer than every fixture here and far shorter than the package timeout.
const bangTestLimit = 10 * time.Second

// lineOf is the index of the first line whose visible text is want, or -1.
func lineOf(frame, want string) int {
	for i, line := range strings.Split(frame, "\n") {
		if strings.TrimSpace(line) == want {
			return i
		}
	}
	return -1
}

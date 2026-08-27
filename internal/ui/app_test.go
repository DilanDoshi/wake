package ui

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// --- the brief's four ---------------------------------------------------

// Asserted through shown rather than on the raw frame. glamour breaks even a
// four-word sentence into several colour runs - "from the" and " agent" arrive
// with an SGR reset between them - so a Contains over the raw view is testing
// escape codes, and it answers differently depending on whether the process
// that ran the test had a terminal. dm_test.go strips for the same reason.
func TestAppRendersIncomingEvents(t *testing.T) {
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(eventMsg{Event: core.Event{Kind: core.KindAssistantText, Text: "from the agent"}})

	if !strings.Contains(shown(m), "from the agent") {
		t.Errorf("event not rendered:\n%s", shown(m))
	}
}

// ⌃O is the product's central key: it detaches, the TUI exits, and the fleet
// keeps working. That is the property the whole daemon exists to provide.
//
// It was ⌃C for the whole of Phase 1, because stopping was irreversible and
// detach was the safe thing to put under the key everyone reaches for. Park
// removed that reason, so ⌃C parks and leaving has a key of its own - see
// park.go. This test kept its subject and changed its key.
//
// Asserted on the *message the command produces*, not on the command being
// non-nil, and the difference is the whole test. Under the old assertion,
// replacing tea.Quit with textarea.Blink - a key that blinks the cursor and
// never lets go of the terminal - left this package and cmd/wake green.
//
// Two keys since the arm - ⌃O then ↵, see detach.go - and the subject is
// unchanged: the gesture still leaves, and detach_test.go is where the arm
// itself is the subject.
func TestCtrlOQuits(t *testing.T) {
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("ctrl+o then ↵ produced no command; expected tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+o produced %T, want tea.QuitMsg: the one key that has to leave does not", cmd())
	}
}

// And it quits whatever else is going on. A ⌃O that only worked from a resting
// model would be a ⌃O that fails exactly when somebody needs it.
func TestCtrlOQuitsFromAnyState(t *testing.T) {
	frames, errs := openStream(t, eventFrame("s1", "mid conversation"))
	var m tea.Model = sizedApp(t, frames, errs, "s1")
	m, _ = step(t, m, m.(App).listen())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 24}) // a drag in flight
	m = typeText(m, "an unsent draft")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	// ↵ confirms even with a draft in the box, which is what the armed legend
	// says it will do: `↵ detach` replaces `↵ send` for as long as the arm is
	// live. See detach.go.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("ctrl+o then ↵ produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+o produced %T, want tea.QuitMsg", cmd())
	}
}

func TestEnterClearsTheComposer(t *testing.T) {
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "hello" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if got := m.(App).dm().Composer().Value(); got != "" {
		t.Errorf("composer still holds %q after enter", got)
	}
}

// The brief's body asserted `m.View() != ""`, which no mutation can reach:
// View is always a DM plus the reserved notice row. Asserting the property the
// name claims instead - that the size reached the model before it drew.
func TestWindowSizeIsAppliedBeforeFirstRender(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Asserted on the DM's own stored size, not on the frame's shape. DM.View
	// re-lays for a size it was never given, so an early frame is never wrong -
	// which means the geometry is identical whether or not the size was ever
	// applied, and measuring it discriminates nothing. What "applied" buys is
	// the memoized fast path and the scroll position that survives it.
	// 100 columns less the right sidebar, which `wake` opens with and which a
	// conversation no longer takes away.
	dm := m.(App).dm()
	if dm.width != 100-rosterWidth || dm.height != 30-noticeHeight-stripHeight {
		t.Errorf("the DM was sized %dx%d, want %dx%d", dm.width, dm.height, 100-rosterWidth, 30-noticeHeight-stripHeight)
	}

	out := m.View()
	if got := strings.Count(out, "\n") + 1; got != 30 {
		t.Errorf("the first frame is %d rows, want the 30 it was told about", got)
	}
	if got := widest(out); got != 100 {
		t.Errorf("the first frame is %d columns wide, want 100", got)
	}
}

// widest is the display width of the longest line of a frame.
func widest(frame string) int {
	w := 0
	for _, line := range strings.Split(stripANSI(frame), "\n") {
		w = max(w, ansi.StringWidth(line))
	}
	return w
}

// --- helpers ------------------------------------------------------------

// shown is the App's frame reduced to what a reader sees.
func shown(m tea.Model) string { return stripANSI(m.View()) }

// runCmd runs one tea.Cmd off the Update goroutine and returns its message.
//
// Bounded on purpose. A read that never produces a message is one of the two
// ways this wiring can fail, and a test that hangs instead of failing reports
// nothing at all.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run: the model stopped reading the daemon")
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		return msg
	case <-time.After(cmdTimeout):
		t.Fatalf("a command produced no message within %v", cmdTimeout)
		return nil
	}
}

const cmdTimeout = 2 * time.Second

// step runs the model's pending read and applies whatever it produced,
// returning the model and its next read.
func step(t *testing.T, m tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	t.Helper()
	return m.Update(runCmd(t, cmd))
}

// sized is an App wired to a stream, already told how big it is.
func sizedApp(t *testing.T, frames chan rpc.Frame, errs chan error, sessionID string) App {
	t.Helper()
	fresh(t)
	a := dmApp(nil, Stream{Frames: frames, Errs: errs}, sessionID, "alex")
	m, _ := a.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m.(App)
}

// fresh empties the process-wide notice sink, which View reads.
func fresh(t testing.TB) {
	t.Helper()
	notice.Reset()
	t.Cleanup(notice.Reset)
}

// closedStream returns channels carrying frames and then closing, in the same
// order rpc.ReadFrames closes them. It is a connection that has *ended*; use
// openStream for one that is merely quiet.
func closedStream(fs ...rpc.Frame) (chan rpc.Frame, chan error) {
	frames := make(chan rpc.Frame, len(fs))
	errs := make(chan error, 1)
	for _, f := range fs {
		frames <- f
	}
	close(errs)
	close(frames)
	return frames, errs
}

// openStream is a live connection: the frames have arrived and the daemon is
// still there.
//
// It is the shape almost every test wants, and closedStream was standing in for
// it while a read delivered one frame at a time - the model now folds a whole
// batch, so a stream that ends in the same breath reports the hang-up in the
// same message and its notice lands on top of whatever the test was asserting.
// A daemon that goes away the instant it speaks is not the case under test.
func openStream(t *testing.T, fs ...rpc.Frame) (chan rpc.Frame, chan error) {
	t.Helper()
	frames := make(chan rpc.Frame, max(len(fs)+8, 16))
	errs := make(chan error, 1)
	for _, f := range fs {
		frames <- f
	}
	// Closed in the order rpc.ReadFrames closes them, so the pump goroutine
	// retires with the test rather than parking for the life of the package.
	t.Cleanup(func() { close(errs); close(frames) })
	return frames, errs
}

// eventFrame is one assistant turn from one session, which is what most of this
// package's tests need to see arrive.
func eventFrame(sessionID, text string) rpc.Frame {
	return kindFrame(sessionID, core.KindAssistantText, text)
}

// kindFrame is the same for any event kind - a thinking block, which only a DM
// draws, or a permission request, which only the room's cards do.
func kindFrame(sessionID string, kind core.EventKind, text string) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event:     &core.Event{Kind: kind, SessionID: sessionID, Text: text},
	}
}

// --- the stream ---------------------------------------------------------

// The sketch this task started from called rpc.ReadFrames inside the tea.Cmd,
// which is a fresh reader goroutine and a fresh 64KB scanner per received
// event - an unbounded goroutine leak, plus silent frame loss from bytes left
// in the abandoned buffers. The reader is created once by whoever opened the
// connection; this is what proves the model reads from the one it was given
// rather than opening its own.
func TestEveryFrameOnOneStreamIsRendered(t *testing.T) {
	frames, errs := openStream(t,
		eventFrame("s1", "first message"),
		eventFrame("s1", "second message"),
		eventFrame("s1", "third message"),
	)
	a := sizedApp(t, frames, errs, "s1")
	settled(t, a, 3, false)

	var m tea.Model = a
	m, cmd := step(t, m, a.listen())
	for _, want := range []string{"first message", "second message", "third message"} {
		if !strings.Contains(shown(m), want) {
			t.Errorf("the transcript lost %q:\n%s", want, shown(m))
		}
	}

	// And the read is still armed afterwards. A batch that folded three frames
	// and then stopped listening would pass every assertion above and leave the
	// conversation dead from the fourth event on.
	frames <- eventFrame("s1", "fourth message")
	m, _ = step(t, m, cmd)
	if !strings.Contains(shown(m), "fourth message") {
		t.Errorf("the read did not re-arm after a batch:\n%s", shown(m))
	}
}

// A frame the view does not model must still re-arm the read. The sketch
// returned a nil tea.Msg for those, and a nil message schedules nothing - so
// the first status reply the daemon pushed would have stopped the stream dead
// with no error anywhere.
func TestAFrameTheViewIgnoresStillKeepsReading(t *testing.T) {
	frames, errs := openStream(t, rpc.Frame{Kind: rpc.FrameStatusReply, Status: &rpc.Status{Running: true}})
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	// Folded on its own, so the read that follows it is the one under test
	// rather than one that came with an event in the same batch.
	m, cmd := step(t, m, m.(App).listen())
	frames <- eventFrame("s1", "after the status")
	m, _ = step(t, m, cmd)

	if !strings.Contains(shown(m), "after the status") {
		t.Errorf("the read stopped at a frame the view ignores:\n%s", shown(m))
	}
}

// The daemon broadcasts every session's events to every attached client, so a
// second `wake` in another repo puts its agent's output on this connection. A
// DM is 1:1.
func TestAnotherSessionsEventsAreNotRendered(t *testing.T) {
	frames, errs := openStream(t,
		eventFrame("s2", "somebody else's agent"),
		eventFrame("s1", "this agent"),
	)
	a := sizedApp(t, frames, errs, "s1")
	settled(t, a, 2, false)

	var m tea.Model = a
	m, _ = step(t, m, a.listen())

	if strings.Contains(shown(m), "somebody else's agent") {
		t.Errorf("another session's event was rendered into this DM:\n%s", shown(m))
	}
	if !strings.Contains(shown(m), "this agent") {
		t.Errorf("this session's event went missing:\n%s", shown(m))
	}
}

// A refused spawn comes back as a FrameError, not as a dropped connection. A
// client that ignores it shows an empty conversation for a session that never
// started, forever.
func TestADaemonErrorIsVisible(t *testing.T) {
	frames, errs := openStream(t, rpc.Frame{
		Kind:      rpc.FrameError,
		SessionID: "s1",
		Text:      "a session id must be a UUID",
	})
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if !strings.Contains(shown(m), "a session id must be a UUID") {
		t.Errorf("the daemon's refusal was swallowed:\n%s", shown(m))
	}
}

// Every agent's failures are reported now, and that is a deliberate reversal.
// A DM filtered these because it was 1:1 and another agent's crash was
// somebody else's window; the room is every agent, so an error about any of
// them is about something on this screen - and an agent that died silently
// because its error was addressed elsewhere is the state the notice row exists
// to prevent.
//
// What each one has to carry is *which* agent. "exit status 1" was answerable
// while a window held one conversation; at fifteen it is a sentence about
// nobody.
//
// Asserted on the sink rather than on the frame. Latest() only ever shows the
// newest notice, so an error that was reported and immediately overwritten by
// the gap report would leave a Contains assertion still passing.
func TestEveryAgentsErrorsAreReportedAndEachOneSaysWhose(t *testing.T) {
	frames, errs := openStream(t,
		rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Sessions: []rpc.SessionStatus{
			{ID: "s2", Name: "john", State: rpc.StateWorking},
		}}},
		rpc.Frame{Kind: rpc.FrameError, SessionID: "s2", Text: "exit status 1"},
		rpc.Frame{Kind: rpc.FrameError, Text: "dropped 12 frames"},
	)
	a := sizedApp(t, frames, errs, "s1")
	settled(t, a, 3, false)

	var m tea.Model = a
	m, _ = step(t, m, a.listen())

	if n := notice.Count(agentPrefix + "john: exit status 1"); n != 1 {
		t.Errorf("another agent's failure was reported %d times under its own name; the room shows that agent, so its errors are this window's business:\n%s", n, shown(m))
	}
	if notice.Count("dropped 12 frames") != 1 {
		t.Errorf("the daemon's own gap report was swallowed:\n%s", shown(m))
	}
}

// The daemon hanging up is the one failure a TUI cannot afford to render as
// silence: every later keystroke goes nowhere.
func TestTheStreamEndingIsVisible(t *testing.T) {
	frames, errs := closedStream()
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if !strings.Contains(shown(m), "hung up") {
		t.Errorf("the daemon hanging up left no trace in the view:\n%s", shown(m))
	}
}

func TestAReadFailureIsVisible(t *testing.T) {
	frames := make(chan rpc.Frame)
	errs := make(chan error, 1)
	errs <- errors.New("decode frame: unexpected end of input")
	close(errs)
	close(frames)

	var m tea.Model = sizedApp(t, frames, errs, "s1")
	m, _ = step(t, m, m.(App).listen())

	if !strings.Contains(shown(m), "unexpected end of input") {
		t.Errorf("the read error was swallowed:\n%s", shown(m))
	}
}

// --- sending ------------------------------------------------------------

// pipeClient gives the App a connection with somebody reading the far end, and
// returns the frames that arrive there.
func pipeClient(t *testing.T) (net.Conn, <-chan rpc.Frame) {
	t.Helper()
	mine, theirs := net.Pipe()
	frames, errs := rpc.ReadFrames(theirs)
	t.Cleanup(func() {
		_ = mine.Close()
		_ = theirs.Close()
		for range frames {
		}
		<-errs
	})
	return mine, frames
}

func awaitFrame(t *testing.T, frames <-chan rpc.Frame) rpc.Frame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("the connection closed before a frame arrived")
		}
		return f
	case <-time.After(cmdTimeout):
		t.Fatalf("no frame reached the daemon within %v", cmdTimeout)
		return rpc.Frame{}
	}
}

func typeText(m tea.Model, text string) tea.Model {
	for _, r := range text {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestEnterSendsTheMessageToTheDaemon(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m = typeText(m, "run the tests")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	go func() { _ = runCmdQuietly(cmd) }()

	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameSend {
		t.Errorf("frame kind = %q, want %q", f.Kind, rpc.FrameSend)
	}
	if f.SessionID != "s1" {
		t.Errorf("frame session = %q, want s1", f.SessionID)
	}
	if f.Text != "run the tests" {
		t.Errorf("frame text = %q", f.Text)
	}
}

// runCmdQuietly runs a command with no test to fail, for the sending side of a
// net.Pipe, which cannot complete until somebody reads it.
func runCmdQuietly(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// Wake owns the single-source rule: a sent message appears either as a frame
// the transcript replayed or as a local echo, never both. Nothing passes
// --replay-user-messages today, so the local echo is the only source there is -
// without it the user's own turn is invisible in their own conversation.
func TestASentMessageIsEchoedExactlyOnce(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m = typeText(m, "echo me")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	go func() { _ = runCmdQuietly(cmd) }()
	awaitFrame(t, sent)

	if n := strings.Count(shown(m), "echo me"); n != 1 {
		t.Errorf("the sent message appears %d times in the transcript, want exactly 1:\n%s", n, shown(m))
	}
}

func TestAnEmptyComposerSendsNothing(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m = typeText(m, "   ")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		if msg := runCmdQuietly(cmd); msg != nil {
			t.Errorf("an empty message produced %v", msg)
		}
	}

	select {
	case f := <-sent:
		t.Errorf("whitespace was sent to the agent as %+v", f)
	case <-time.After(50 * time.Millisecond):
	}
	_ = m
}

func TestAFailedSendIsVisible(t *testing.T) {
	fresh(t)
	mine, theirs := net.Pipe()
	_ = theirs.Close()
	_ = mine.Close()

	var m tea.Model = dmApp(mine, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = typeText(m, "this cannot land")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(runCmd(t, cmd))

	// Asserted on the failure's own words rather than on "send", which is
	// already in the composer's key hint and would make this pass with the
	// whole error path deleted.
	if !strings.Contains(shown(m), "closed pipe") {
		t.Errorf("a write that failed left no trace in the view:\n%s", shown(m))
	}
}

// --- layout and keys ----------------------------------------------------

// View has a value receiver and cannot memoize, so a DM that was never told
// its size re-renders the whole transcript through the process-global render
// mutex on every frame - and SetSize's return to the newest line means
// scrollback does not merely get slow, it stops working.
func TestTheWindowSizeReachesTheDM(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(eventMsg{Event: core.Event{Kind: core.KindAssistantText, Text: wrappingSentence}})

	if n := countRenders(t, func() {
		for range 3 {
			_ = m.View()
		}
	}); n != 0 {
		t.Errorf("View re-rendered the transcript %d times at a size the model was told about", n)
	}
}

// The view measures exactly what it was given, notice or no notice. A frame
// one row too tall scrolls the alt screen on every draw.
func TestTheViewIsExactlyTheHeightItWasGiven(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	m, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if got := strings.Count(m.View(), "\n") + 1; got != 24 {
		t.Errorf("the view is %d rows with nothing to report, want 24", got)
	}
	m, _ = m.Update(errMsg{Err: errors.New("something went wrong")})
	if got := strings.Count(m.View(), "\n") + 1; got != 24 {
		t.Errorf("the view is %d rows with a notice on it, want 24", got)
	}
}

// And the row is reserved whether or not there is anything on it. Taking it
// only when there is something to say would change the DM's height at an
// arbitrary moment, and DM.SetSize returns the reader to the newest line - so
// a failure arriving would silently end somebody's scrollback.
//
// The total height cannot catch that: reserving late keeps the frame the right
// size and moves the reader anyway. This is the assertion that discriminates.
func TestANoticeArrivingDoesNotMoveAScrolledReader(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	var m tea.Model
	m, _ = app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := range 60 {
		m, _ = m.Update(eventMsg{Event: core.Event{
			Kind: core.KindAssistantText,
			Text: fmt.Sprintf("message number %d", i),
		}})
	}
	for range 3 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}

	const newest = "message number 59"
	if strings.Contains(shown(m), newest) {
		t.Fatalf("paging up left the newest message on screen, so this test proves nothing:\n%s", shown(m))
	}

	m, _ = m.Update(errMsg{Err: errors.New("something went wrong")})

	if strings.Contains(shown(m), newest) {
		t.Errorf("a notice arriving returned the reader to the newest message:\n%s", shown(m))
	}
}

// Ruled after round 1: the hint line describes only what this build does. A
// permissions control that appears to tighten permissions and changes nothing
// is worse than no control, because a missing feature is not trusted and a
// lying one is - and this one failed silently in the unsafe direction.
func TestShiftTabDoesNotMoveTheModeIndicator(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	var m tea.Model
	m, _ = app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	for range 4 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
		if got := m.(App).dm().Composer().Mode(); got != spawnedMode {
			t.Fatalf("shift+tab moved the indicator to %v; every session in this build runs %v", got, spawnedMode)
		}
	}
	if strings.Contains(shown(m), "⌃⇧A") {
		t.Errorf("the hint still advertises ⌃⇧A, which is bound to nothing:\n%s", shown(m))
	}
}

// transcript already tracks a scroll position and Append already samples it,
// so a reader who has scrolled back is not yanked to the newest line. Nothing
// could reach any of that until there were keys bound to it.
func TestPageKeysMoveTheReaderBackThroughTheConversation(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	var m tea.Model
	m, _ = app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := range 40 {
		m, _ = m.Update(eventMsg{Event: core.Event{
			Kind: core.KindAssistantText,
			Text: "message number " + string(rune('a'+i%26)) + strings.Repeat("!", i),
		}})
	}

	bottom := shown(m)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if scrolled := shown(m); scrolled == bottom {
		t.Errorf("page up did not move the reader:\n%s", scrolled)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if returned := shown(m); returned != bottom {
		t.Errorf("page down did not return the reader to where they started:\n%s", returned)
	}
}

func TestTheWheelMovesTheReader(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	var m tea.Model
	m, _ = app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := range 40 {
		m, _ = m.Update(eventMsg{Event: core.Event{Kind: core.KindAssistantText, Text: "line " + strings.Repeat("x", i)}})
	}

	bottom := shown(m)
	m, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if scrolled := shown(m); scrolled == bottom {
		t.Error("the wheel did not move the reader")
	}
}

// The ask arrives as an ordinary event and the DM already draws it. What must
// not happen is Update dropping it before it gets there - a blocked agent that
// says nothing is the worst state in the product.
func TestAPermissionRequestReachesTheView(t *testing.T) {
	frames, errs := openStream(t, rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: "s1",
		Event: &core.Event{
			Kind:      core.KindPermissionRequest,
			SessionID: "s1",
			RequestID: "req-1",
			Tool:      &core.ToolCall{Name: "Bash", Display: "rm -rf /"},
		},
	})
	var m tea.Model = sizedApp(t, frames, errs, "s1")

	m, _ = step(t, m, m.(App).listen())

	if out := shown(m); !strings.Contains(out, "permission") {
		t.Errorf("a permission request left no trace in the view:\n%s", out)
	}
}

// Update returns a model, and every branch must return the modified copy. A
// branch that returns the receiver silently discards whatever it just did.
func TestUpdateIsImmutable(t *testing.T) {
	fresh(t)
	app := dmApp(nil, Stream{}, "s1", "alex")
	sized, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	after, _ := sized.Update(eventMsg{Event: core.Event{Kind: core.KindAssistantText, Text: "later message"}})

	if strings.Contains(shown(sized), "later message") {
		t.Errorf("appending an event changed a model somebody else was holding:\n%s", shown(sized))
	}
	if !strings.Contains(shown(after), "later message") {
		t.Errorf("the returned model is missing the event:\n%s", shown(after))
	}
}

// --- the notice row is one row ---------------------------------------------

// lipgloss's MaxWidth truncates each line but preserves the line count, so a
// notice carrying a newline makes the frame one row too tall and the alt
// screen scrolls on every draw. The text comes from the daemon's error frames,
// which carry an agent's stderr.
func TestAMultiLineNoticeStaysOneRow(t *testing.T) {
	fresh(t)
	var m tea.Model = dmApp(nil, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(frameMsg{Frame: rpc.Frame{
		Kind: rpc.FrameError,
		Text: "spawn failed:\nError: Session ID is already in use.\n  at startup",
	}})

	if got := strings.Count(m.View(), "\n") + 1; got != 24 {
		t.Errorf("a multi-line notice made the frame %d rows, want 24", got)
	}
	if !strings.Contains(shown(m), "already in use") {
		t.Errorf("flattening the notice lost its content:\n%s", shown(m))
	}
}

// An escape sequence in an agent's stderr must not reach the terminal as a
// command. \x1b[2J clears the screen, and U+009B is CSI in 8-bit mode - an
// escape sequence introducer with no ESC in front of it, so stripping only the
// C0 escape would leave the same capability behind under a different byte.
func TestAControlSequenceInANoticeIsNeutralised(t *testing.T) {
	fresh(t)
	var m tea.Model = dmApp(nil, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameError, Text: "boom \x1b[2J\r\u009b2Jgone"}})

	row := m.(App).noticeLine()
	for _, forbidden := range []string{"\x1b[2J", "\r", "\u009b"} {
		if strings.Contains(row, forbidden) {
			t.Errorf("the notice row carries %q verbatim: %q", forbidden, row)
		}
	}
	// The text itself survives; only its ability to move a cursor does not.
	if !strings.Contains(stripANSI(row), "gone") {
		t.Errorf("flattening lost the message: %q", row)
	}
}

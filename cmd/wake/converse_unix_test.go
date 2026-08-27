//go:build unix

// The two loops a person completes at a keyboard, driven from the keyboard.
//
// # Why here, and why a whole program
//
// internal/ui proves a keystroke becomes a frame, over a pipe. internal/daemon
// proves a frame reaches an agent and its answer comes back, over a socket.
// Neither can prove the two together, because neither may import the other -
// the UI is not allowed to know the daemon exists. cmd/wake is where they meet,
// which makes it the only place these loops are reachable at all.
//
// So nothing below is faked except the model: a real daemon on a real socket, a
// real forked `claude` process, the real attach path (connect, requestSpawn,
// awaitSpawn, resume, conversation), a real tea.Program, and real bytes
// arriving on its terminal input - "zebrafish\r" and "\x1b" are what a keyboard
// sends, parsed by Bubble Tea's own reader rather than injected past it.
//
// Every assertion is on what the far side did or on what a reader can see, and
// none is on a frame Wake produced. That distinction is the whole reason this
// file exists: a recording spike found Wake emitting bytes that were valid,
// correctly addressed and completely ineffective, and every test asserting the
// shape of those bytes passed.

package main

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
)

// The window the conversation is drawn in. Wide enough that a short sentence is
// not wrapped, because these tests assert on rendered prose and glamour breaks
// lines to fit.
const (
	viewWidth  = 100
	viewHeight = 30
)

// viewPoll is how often the rendered frame is re-read while waiting for
// something to appear in it.
const viewPoll = 20 * time.Millisecond

// The loop the product is: type a message, the agent answers, and the answer is
// in the conversation.
//
// It is the most basic promise Wake makes and nothing automated has ever
// checked it end to end. The nearest thing, TestAStalledDrawLoopDoesNotCostThe
// Conversation, gets agent output onto the screen but never sends anything -
// its agent floods unprompted - and internal/ui's send tests stop at the far
// end of a pipe with no agent behind it.
//
// The assertion is deliberately not "the message I typed is on the screen".
// App.submit echoes a sent message into the transcript locally, so that text is
// there whether or not anything received it; heardPrefix is the part only the
// agent can produce.
//
// Mutation check: deleting the rpc.WriteFrameTo call from ui.App.submit - the
// UI still echoing locally, so the conversation looks entirely normal - leaves
// this failing at "the agent never answered". Deleting the a.sess.Send call
// from daemon agent.apply's rpc.FrameSend case fails at the same assertion.
// Dropping the FrameEvent case from ui.App.apply fails at "the agent's opening
// turn", before a key is pressed.
func TestTypingAMessageGetsAnAnswerBackIntoTheConversation(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	withScriptedAgent(t, "")
	socket := serveInProcess(t)
	d := startConversation(t, socket)

	// The conversation is live before anything is typed. Without this the test
	// could fail at the send while the real fault was a spawn that never
	// produced a process.
	d.awaitView("the agent's opening turn", "ready")

	const typed = "zebrafish"
	d.typeKeys(typed + "\r")

	view := d.awaitView("the agent's answer", heardPrefix+typed)

	// The person's own turn is in the transcript too. A conversation showing
	// only the answers is half a conversation, and the local echo is the only
	// source there is - nothing passes --replay-user-messages, so the user's
	// turn never comes back from claude.
	if !strings.Contains(view, typed) {
		t.Errorf("the message that was typed is not in the conversation:\n%s", view)
	}
	// Nothing went wrong quietly on the way. The notice row is where a failed
	// write, a dropped frame or a hang-up would be reported, and a test that
	// ignored it could pass while the reader was being told the conversation
	// was broken.
	if n, reported := notice.Latest(); reported {
		t.Errorf("the reader was told something went wrong: %q\n%s", n.Text, view)
	}

	// Asserted again on the model Bubble Tea hands back at exit, so the result
	// does not rest on the filter this harness observes through.
	if final := stripEscapes(d.stop().View()); !strings.Contains(final, heardPrefix+typed) {
		t.Errorf("the agent's answer is absent from the final frame:\n%s", final)
	}
}

// The loop an operator completes when an agent is doing the wrong thing: stop
// this turn, and carry on.
//
// ⎋ is wired through four layers - App.key, rpc.FrameInterrupt, agent.apply,
// core.Session.Interrupt - and each has its own test. What none of them covers
// is the half that makes the key worth binding: the session is still there
// afterwards and takes the next thing it is told. internal/daemon proves that
// with a raw client; this proves it from a keyboard, with the abort visible to
// a reader.
//
// The turn is provably in flight before the key is pressed. An interrupt with
// no turn under it is a harmless no-op that still produces a receipt, so a test
// that raced the agent could pass having stopped nothing.
//
// Mutation check, with the assertion each one actually fired on. Returning nil
// from ui.App.interrupt, and routing rpc.FrameInterrupt to a.stop() in daemon
// agent.apply - the confusion the four-verbs rule exists to prevent - both fail
// at "the abort never reached the transcript at all": neither produces a marker,
// because stop closes stdin rather than aborting anything. Following the
// interrupt with a stop, which is the tidying-up version of the same mistake,
// gets the abort drawn and then fails at "the daemon reports 0 running sessions
// after an interrupt". Deleting core.interruptNotice's resolution fails at
// "Claude's abort wording was drawn as the user's turn" - not at the barrier,
// which is the whole reason the barrier accepts either rendering.
func TestEscapeStopsTheTurnAndTheSessionTakesTheNextOne(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	withScriptedAgent(t, scriptInterruptible)
	socket := serveInProcess(t)
	d := startConversation(t, socket)

	d.awaitView("the agent's opening turn", "ready")

	d.typeKeys("firstword\r")
	// The turn is running and has not finished. This is the barrier that makes
	// the keystroke below an interrupt rather than a no-op.
	d.awaitView("the turn to be running", workingMarker+": firstword")

	d.typeKeys("\x1b")

	// The barrier accepts either rendering of the abort, deliberately.
	//
	// Claude reports the abort itself, as an ordinary user frame carrying
	// nothing but English - no subtype, no isSynthetic - so the text is the
	// only discriminator and the airlock is what resolves it into one of Wake's
	// own notices. Waiting only for the resolved form would make an unresolved
	// one time out here, and "the abort never happened" is a different fault
	// from "the abort was drawn as something the operator typed". Waiting for
	// either separates them: the timeout means the first, and the checks below
	// name the second.
	view := d.awaitViewAny("the abort never reached the transcript at all",
		interruptedNotice, rawAbortMarker)
	if strings.Contains(view, rawAbortMarker) {
		t.Errorf("Claude's abort wording was drawn as the user's turn - it reads as somebody typing a bracketed sentence they never wrote:\n%s", view)
	}
	if !strings.Contains(view, interruptedNotice) {
		t.Errorf("the abort reached the transcript and was not drawn as %q:\n%s", interruptedNotice, view)
	}

	// The session survives, asserted on the fleet before anything else is
	// typed. A transcript can show an answer from a session the daemon has
	// since given up on, so "it is still there" is a question for the daemon -
	// and asking it here rather than after the next turn is what makes an
	// interrupt that also ended the session fail on that fact rather than on a
	// message that had nowhere to go.
	st, err := daemon.Status(socket)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := runningCount(st); got != 1 {
		t.Fatalf("the daemon reports %d running sessions after an interrupt, want 1: ⎋ ended the session instead of the turn", got)
	}

	// And it still takes work, which is the half that makes the key worth
	// binding at all.
	d.typeKeys("secondword\r")
	d.awaitView("the agent never took the next turn after being interrupted", workingMarker+": secondword")
}

// interruptedNotice is how a stopped turn reads, and rawAbortMarker is the
// wording it replaces.
//
// Both are named here because a test asserting on a rendered frame has to name
// what is rendered. The pair is what makes them more than spellings: one must
// be present and the other absent, so a build that stopped resolving the
// wording fails on the marker it let through rather than on the label it
// stopped drawing.
const (
	interruptedNotice = "⊘ turn interrupted"
	rawAbortMarker    = "[Request interrupted by user]"
)

// --- driving a real program -------------------------------------------------

// drivenApp is a conversation with a keyboard attached.
//
// The rendered frame is captured through tea.WithFilter, which runs on the
// event loop with the current model in hand. That is the only way to read what
// a reader can see while the program is still running - p.Run() hands back a
// model only at exit, and every assertion here is about an ordering that
// happens before then.
type drivenApp struct {
	t    *testing.T
	p    *tea.Program
	keys *io.PipeWriter
	done chan tea.Model

	mu     sync.Mutex
	view   string
	final  tea.Model
	closed bool
}

// nudge is a message with no meaning, sent to make the loop turn.
//
// The filter sees the model as it was *before* the message it is filtering, so
// the newest update is one message behind. Something has to arrive for it to
// catch up, and a real conversation goes quiet - so a poll that only looked
// would wait out its deadline on a frame that had already been drawn. App.Update
// hands anything it does not recognise to the composer, which ignores it.
type nudge struct{}

// startConversation runs the whole client path short of the terminal: dial,
// spawn, wait for the daemon to name the session, and hand the frames read past
// on the way to the model that will draw them.
//
// It goes through connect/requestSpawn/awaitSpawn/conversation rather than
// writing frames by hand, so what is under test is the path `wake` takes. The
// only thing converse() adds is tea.WithAltScreen, which needs a TTY.
func startConversation(t *testing.T, socket string) *drivenApp {
	t.Helper()

	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	held := &connection{}
	held.replace(conn)
	t.Cleanup(held.close)

	sessionID := uuid.NewString()
	if err := requestSpawn(conn, sessionID, "", "", spawnOpts{}); err != nil {
		t.Fatalf("requestSpawn: %v", err)
	}
	sess, fleet, readPast, err := awaitSpawn(stream, sessionID)
	if err != nil {
		t.Fatalf("awaitSpawn: %v", err)
	}

	app := conversation(socket, sess, fleet, conn, resume(stream, readPast), held)
	sized, _ := app.Update(tea.WindowSizeMsg{Width: viewWidth, Height: viewHeight})

	d := &drivenApp{t: t, done: make(chan tea.Model, 1)}
	reader, writer := io.Pipe()
	d.keys = writer
	d.p = tea.NewProgram(sized,
		tea.WithInput(reader),
		tea.WithOutput(io.Discard),
		tea.WithFilter(func(m tea.Model, msg tea.Msg) tea.Msg {
			d.record(m)
			return msg
		}))

	go func() {
		final, err := d.p.Run()
		if err != nil {
			t.Errorf("the program failed: %v", err)
		}
		d.done <- final
	}()
	t.Cleanup(func() { d.stop() })
	return d
}

func (d *drivenApp) record(m tea.Model) {
	view := stripEscapes(m.View())
	d.mu.Lock()
	defer d.mu.Unlock()
	d.view = view
}

func (d *drivenApp) snapshot() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.view
}

// typeKeys writes bytes to the program's terminal input, exactly as a keyboard
// would.
//
// Bounded, and that is not caution for its own sake: an io.Pipe write blocks
// until somebody reads it, so a program that has stopped reading its input
// turns every later keystroke into a hang. A hang and a failure look identical
// in a summary line, and this project has lost a debugging round to a mutation
// that produced a package timeout instead of a message.
func (d *drivenApp) typeKeys(keys string) {
	d.t.Helper()

	written := make(chan error, 1)
	go func() {
		_, err := d.keys.Write([]byte(keys))
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			d.t.Fatalf("typing %q: %v", keys, err)
		}
	case <-time.After(testTimeout):
		d.t.Fatalf("the program never read %q from its terminal within %v:\n%s", keys, testTimeout, d.snapshot())
	}
}

// awaitView waits for something to appear in the frame a reader sees, and fails
// with the whole frame.
//
// The frame is the useful half of the failure: a conversation that rendered the
// wrong thing is far more common than one that rendered nothing, and the two
// are indistinguishable from a message that only says what was missing.
func (d *drivenApp) awaitView(what, want string) string {
	d.t.Helper()
	return d.awaitViewAny(what, want)
}

// awaitViewAny waits for any one of several things to appear.
//
// It exists so a barrier can be stated as "the frame arrived" while the
// assertion about *how* it was drawn stays separate. A wait for one spelling
// answers both questions at once and answers the second one by timing out,
// which is the shape of a guard whose failure mode no test reaches.
func (d *drivenApp) awaitViewAny(what string, wants ...string) string {
	d.t.Helper()

	if len(wants) == 0 {
		d.t.Fatal("awaitViewAny was given nothing to wait for, so it would return the first frame it saw")
	}
	deadline := time.Now().Add(testTimeout)
	for {
		d.p.Send(nudge{})
		view := d.snapshot()
		for _, want := range wants {
			if strings.Contains(view, want) {
				return view
			}
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("waited %v for %s (any of %q) and none appeared. The conversation reads:\n%s",
				testTimeout, what, wants, view)
		}
		time.Sleep(viewPoll)
	}
}

// stop quits the program and returns the model it ended with. Idempotent: a
// test that asserts on the final frame calls it, and so does the cleanup.
func (d *drivenApp) stop() tea.Model {
	d.t.Helper()

	d.mu.Lock()
	if d.closed {
		final := d.final
		d.mu.Unlock()
		return final
	}
	d.closed = true
	d.mu.Unlock()

	d.p.Quit()
	// After the quit, so a keystroke still parked on the pipe is released
	// rather than holding this goroutine and the program's reader against each
	// other.
	_ = d.keys.Close()

	select {
	case final := <-d.done:
		d.mu.Lock()
		d.final = final
		d.mu.Unlock()
		return final
	case <-time.After(testTimeout):
		d.t.Fatalf("the program never exited within %v:\n%s", testTimeout, d.snapshot())
		return nil
	}
}

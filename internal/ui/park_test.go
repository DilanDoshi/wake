// Parking from inside the room: the keys, and what the room says about a
// session that has been parked.
//
// The routing half came first, before there was a key. The keys are here now -
// ⌃C parks the focused agent, ⌃Q parks the fleet and closes Wake, ⌃O detaches -
// and the rebinding is the reason this file has a subject rather than a
// paragraph.

package ui

import (
	"errors"
	"go/ast"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// ⌃Q writes a question behind the verb, and the answer is what lets the window
// close.
//
// **This is the half of outstanding bug 3 that ⌃C's fix could not take.** ⌃C
// says the ask on the press and lets the *report* make the promise, because the
// window stays open for the report to arrive in. ⌃Q's window is closing: the
// keypress is followed by tea.Quit, so a refused, dropped or lost write has
// nowhere at all to land, and `Parking 3 agents.` is printed over it either way.
//
// The instrument is Task 14's and it needs no new frame kind. serveClient
// dispatches one connection's frames **synchronously and in order**, so a
// FrameStatus written behind FrameParkAll cannot be answered until the ParkAll
// has been dispatched - which is beginQuit(quitPark), the point at which this
// daemon is provably ending as a park rather than as anything else. Reaching the
// reply means the daemon has it.
//
// The frames are asserted in order, because the order is the whole mechanism: a
// status written *ahead* of the verb is answered before the verb is dispatched
// and proves nothing about it.
func TestCtrlQAsksWhetherTheDaemonTookTheParkBeforeTheWindowCloses(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(160, 30)

	_, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	go runLikeTheLoop(cmd)

	if f := awaitFrame(t, sent); f.Kind != rpc.FrameParkAll {
		t.Fatalf("the first frame ⌃Q wrote is %q, want a FrameParkAll", f.Kind)
	}
	select {
	case f, ok := <-sent:
		if !ok {
			t.Fatal("the connection closed before ⌃Q asked anything behind the park-all")
		}
		if f.Kind != rpc.FrameStatus {
			t.Fatalf("⌃Q wrote %q behind the FrameParkAll, want a FrameStatus", f.Kind)
		}
	case <-time.After(cmdTimeout):
		t.Fatalf("⌃Q wrote the FrameParkAll and asked nothing behind it within %v. Nothing on this "+
			"socket acknowledges a park-all, so a write the daemon never took - a connection that "+
			"went away, a deadline that expired, a daemon already shutting down under `wake stop` - "+
			"is indistinguishable here from one it did, and the exit line promises a parked fleet "+
			"either way. A FrameStatus behind the verb is the acknowledgement: one connection's "+
			"frames are dispatched in order, so its reply cannot come back before the verb was "+
			"taken", cmdTimeout)
	}
}

// A parked agent is not an addressee, so `@all` does not reach it and its name
// resolves to nobody.
//
// The same rule ended already has, for the same reason and not by analogy: the
// process is gone, so a message to it is a frame the daemon refuses. The cost
// of getting this wrong scales with the fleet - `@all` at thirty agents means
// one refusal per parked session, in the room, for one keystroke.
func TestAParkedAgentIsNotAnAddressee(t *testing.T) {
	fleet := NewFleet().WithStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "sydney", State: rpc.StateParked},
		{ID: "s3", Name: "peter", State: rpc.StateEnded},
	}})
	a := App{fleet: fleet}

	var got []string
	for _, who := range a.live() {
		got = append(got, who.Name)
	}
	if len(got) != 1 || got[0] != "alex" {
		t.Errorf("the addressee list is %v, want [alex] alone: a parked agent has no process to read a "+
			"message, so @all reaching it is one refused frame per parked session", got)
	}
}

// --- the keys -------------------------------------------------------------

// ⌃C parks the conversation that has the keys. It no longer quits.
//
// This is the rebinding the whole task exists for, and the assertion is in two
// halves because either alone passes against a defect: the frame has to go
// out, and the program must not be told to quit. A ⌃C that wrote the frame and
// also quit would park the agent and close the window, which is ⌃Q's job.
func TestCtrlCParksTheFocusedAgentAndDoesNotQuit(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !handled {
		t.Fatal("⌃C was not taken by App.key")
	}
	_ = m
	msg := runCmdQuietly(cmd)
	if _, quit := msg.(tea.QuitMsg); quit {
		t.Error("⌃C quit the program. Detach is ⌃O now; ⌃C parks, and a key that sometimes closes " +
			"the workspace costs somebody an hour once and is never trusted again")
	}
	f := awaitFrame(t, sent)
	if f.Kind != rpc.FramePark || f.SessionID != "s1" {
		t.Errorf("⌃C wrote %+v, want a FramePark for s1", f)
	}
}

// ⌃O then ↵ detaches: the program quits and nothing is stopped. Two keys since
// the arm, and two *different* keys since the repeat finding - see detach.go.
func TestCtrlODetachesAndStopsNothing(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	armed, _, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !handled {
		t.Fatal("⌃O was not taken by App.key")
	}
	m, cmd, handled := armed.(App).key(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("↵ after ⌃O was not taken by App.key")
	}
	_ = m
	if _, quit := runCmdQuietly(cmd).(tea.QuitMsg); !quit {
		t.Error("⌃O then ↵ did not quit the program. Detach is the property the daemon exists to provide, " +
			"and losing it would make the background daemon pointless")
	}
	select {
	case f := <-sent:
		t.Errorf("the detach wrote %+v. It reaches the daemon as nothing at all - the client just "+
			"disconnects - and a frame here is an agent being stopped by the key that means leave", f)
	default:
	}
}

// ⌃Q parks every live agent and then quits.
//
// The frame is what this asserts. That it goes out *before* the quit is the
// other half and it is not observable from here - see
// TestCtrlQSequencesTheWriteAheadOfTheWaitAndQuitsFromNeither for why, and for
// where it is held instead.
func TestCtrlQParksEveryAgentBeforeItQuits(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(160, 30)

	m, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	if n, quit, _ := m.(App).ParkedFleet(); !quit || n != 2 {
		t.Errorf("after ⌃Q the model reports (%d, %v), want (2, true): cmd/wake reads this to say what "+
			"happened on the way out instead of asking a daemon that is already shutting down", n, quit)
	}
	go func() { runLikeTheLoop(cmd) }()
	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameParkAll {
		t.Errorf("⌃Q wrote %+v, want a FrameParkAll: one frame for the fleet, because the daemon is "+
			"the only process that can see all of it", f)
	}
}

// ⌃Q counts what it carries forward, which is neither the whole roster nor the
// list of agents a message could be sent to.
//
// **Both wrong answers are one word away and each passes a fleet that happens
// to be all live**, which is every other test in this file. `len(fleet.Agents())`
// over-counts by the ended sessions, which are not bookable at all -
// `agent.bookable` is `ended && (parking || parked)`, and a session that simply
// finished is neither. `len(a.live())` under-counts by the parked ones, and
// that is the one that shipped: `live()` is send.go's **broadcast-addressee
// list**, whose subject is "who can I send a message to", and it excludes
// StateParked for a reason that is about addressing rather than about counting.
//
// The daemon's own arithmetic is the authority and it disagrees with `live()`:
// `bookParked` counts a session already in the book as `booked` (`case
// held[a.id]: booked++`), so a fleet of one live and one already-parked logs
// *"parked 2 sessions on the way out"* and offers both back, while the line the
// operator actually read said one. The degenerate case is the one that reads as
// a bug rather than as a rounding error - park the only session with ⌃C, press
// ⌃Q, and the last sentence before the window closes is **"parking 0 agents"**
// about a shutdown carrying one forward.
//
// It is the last line an operator sees, with nothing left on screen to check it
// against, at the exact moment they are deciding whether to trust the key.
func TestCtrlQCountsWhatItCarriesForwardAndNotWhatItCouldSendTo(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateWorking},
		rpc.SessionStatus{ID: "s3", Name: "marco", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s4", Name: "peter", State: rpc.StateEnded},
	)

	m, _, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	if n, _, _ := m.(App).ParkedFleet(); n != 3 {
		t.Errorf("⌃Q over a fleet of one idle, one working, one parked and one ended reports %d, want 3. "+
			"The number is what this shutdown hands to the next `wake` - the two it parks now plus the "+
			"one ⌃C already parked, which is in the book and will be offered back - and an ended session "+
			"is not bookable at all", n)
	}
	if !strings.Contains(shown(m), "3 agents") {
		t.Errorf("the notice row says something other than 3 agents:\n%s", shown(m))
	}
}

// The degenerate case, which is the one that reads as a bug: everything was
// parked by hand first, so `live()` is empty and the shutdown is still carrying
// the whole fleet.
//
// Its own test rather than a row in the one above, because "wrong by the number
// of parked sessions" and "says zero about a fleet it is carrying" are the same
// defect with different consequences - the first is a number somebody might not
// check, and the second tells an operator who has just parked three
// conversations that their ⌃Q is closing over nothing.
func TestCtrlQOnAFleetParkedByHandDoesNotReportZero(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(160, 30).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateParked},
	)

	m, _, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	if n, _, _ := m.(App).ParkedFleet(); n != 2 {
		t.Errorf("⌃Q over a fleet somebody parked with ⌃C first reports %d, want 2: the daemon writes "+
			"both into the book and the next `wake` offers both back, so a zero here is the client "+
			"disowning a fleet it is carrying", n)
	}
}

// runLikeTheLoop runs one command the way Bubble Tea's loop does.
//
// tea.Sequence and tea.Batch both **do nothing**: each hands back a message
// carrying the commands, and the loop is what runs them. A test that called
// the command and stopped there would write no frame at all, which is what
// makes this a helper rather than a line.
//
// tea.sequenceMsg is unexported, so its members are reached by reflection -
// there is no type to assert to. tea.BatchMsg is exported and is handled the
// same way for the same reason.
//
// **It is deliberately not an ordering assertion.** A batch carries the same
// commands in the same order and running them here in that order looks
// identical, while the real loop runs a batch concurrently. Which of the two
// ⌃Q uses is a fact about the source, and it is asserted there.
func runLikeTheLoop(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return
	}
	for i := range v.Len() {
		inner, ok := v.Index(i).Interface().(tea.Cmd)
		if !ok {
			continue
		}
		runLikeTheLoop(inner)
	}
}

// ⌃Q's write is sequenced ahead of the quit, and this is where that is held.
//
// **The behavioural test cannot see it and does not claim to.** tea.Sequence
// hands the loop a list of commands; tea.Batch hands it the same list. The
// difference is what the *loop* does with each - Sequence runs them to
// completion in order, Batch runs them concurrently - so any test that expands
// the message itself and runs the members in order fabricates exactly the
// property it is trying to observe, and passes against tea.Batch. On a busy
// machine the real Batch loses the race the other way: tea.Quit ends the
// program before the write goroutine has reached the socket, and twenty agents
// are left running by the key whose whole promise is that they were parked.
//
// So the seam is what is asserted - the same move
// TestADragCostsOneReWrapWhenTheLoopRunsEveryCommand makes for the settle
// timer, and the honest one when the loop is the thing under test.
//
// # What this guard used to say, and why the last clause is now inverted
//
// It required ⌃Q's sequence to end in **tea.Quit**, and that was the defect
// stated as a property: the program ended on the keypress, so every refusal the
// write could produce arrived at a window that had already closed. Requiring it
// meant writing the fix was a build failure here first - park_test.go's own
// `resume` clause did the same thing to `wake attach`'s refusal, which is the
// instance rung 7 is recorded on.
//
// It is narrowed rather than deleted, and the narrowing is in both directions.
// Sequence-not-Batch is **kept**, for a reason that moved one command over: the
// second member is now the deadline on the daemon's answer, and Batch would
// start those three seconds before the write goroutine had reached the socket -
// so a slow write would be reported as a daemon that did not answer. And
// tea.Quit is now **forbidden** here, which is the property the behavioural test
// cannot see: it observes that the reply quits, and a parkFleet that also quit
// on the press would satisfy it while quitting twice.
func TestCtrlQSequencesTheWriteAheadOfTheWaitAndQuitsFromNeither(t *testing.T) {
	fn := funcDeclIn(t, "park.go", "parkFleet")

	var calls []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch exprName(call.Fun) {
		case "tea.Sequence", "tea.Batch":
			calls = append(calls, call)
		}
		if exprName(call.Fun) == "tea.Quit" {
			t.Error("parkFleet calls tea.Quit. That is outstanding bug 3 itself: the window closes on " +
				"the keypress, so a write the daemon refused, dropped or never received has nowhere " +
				"to land, and cmd/wake prints `Parking N agents.` over it. The daemon's answer is " +
				"what ends this program - see parkAllSettled")
		}
		return true
	})
	// tea.Quit as a bare identifier handed to something else is the same lie by
	// another route, and the walk above only sees it called.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && exprName(sel) == "tea.Quit" {
			t.Error("parkFleet names tea.Quit. See above: nothing on the keypress may end this program")
		}
		return true
	})

	if len(calls) != 1 {
		t.Fatalf("parkFleet calls tea.Sequence or tea.Batch %d times, and this guard is written for the "+
			"one that carries the wait: with none, nothing orders the write against the deadline at all",
			len(calls))
	}
	if got := exprName(calls[0].Fun); got != "tea.Sequence" {
		t.Fatalf("parkFleet builds its command with %s. tea.Batch runs both concurrently, so the "+
			"deadline on the daemon's answer would start before the write goroutine had reached the "+
			"socket - and a write that was merely slow would be reported on the way out as a daemon "+
			"that never answered", got)
	}
	if len(calls[0].Args) != 2 {
		t.Fatalf("tea.Sequence is given %d commands and this guard reads two: the ask, then the wait",
			len(calls[0].Args))
	}
	first, isCall := calls[0].Args[0].(*ast.CallExpr)
	if !isCall || exprName(first.Fun) != "a.askParkAll" {
		t.Errorf("the first command in ⌃Q's sequence is %T, and the ask is what has to finish first: "+
			"a sequence that waits first bounds nothing it can name", calls[0].Args[0])
	}
	second, isCall := calls[0].Args[1].(*ast.CallExpr)
	if !isCall || exprName(second.Fun) != "a.parkAllDeadline" {
		t.Errorf("the last command in ⌃Q's sequence is %v rather than a.parkAllDeadline. Without the "+
			"deadline a daemon that took the frame and then went quiet - which is exactly what a "+
			"graceful shutdown looks like from a listen backlog - leaves ⌃Q as a window that never "+
			"closes", calls[0].Args[1])
	}
}

// ⌃Q's ask goes through a.write, and its deadline goes through the seam.
//
// Two separate things one function could quietly stop doing, and neither is
// visible to any behavioural test in this file. A second write path here would
// bypass the bound rpc.WriteFrameTo puts on a client write - "every client write
// in this package is one command", and this one wraps that command rather than
// replacing it. And a tea.Tick called directly is invisible to parkAckTimer, so
// every test that holds the deadline would go on passing while asserting nothing
// about the delay, which is the failure TestADragCostsOneReWrapWhenTheLoopRunsEveryCommand
// exists for one file over.
func TestCtrlQsAskWritesThroughTheOneWriterAndWaitsThroughTheSeam(t *testing.T) {
	ask := funcDeclIn(t, "park.go", "askParkAll")
	var writes int
	ast.Inspect(ask.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && exprName(call.Fun) == "a.write" {
			writes++
		}
		return true
	})
	if writes != 1 {
		t.Errorf("askParkAll calls a.write %d times, want exactly 1: it is a wrapper that turns that "+
			"one command's failure into ⌃Q's answer, and a second frame path here is the parallel "+
			"implementation the rules forbid", writes)
	}

	deadline := funcDeclIn(t, "park.go", "parkAllDeadline")
	var seam, direct int
	ast.Inspect(deadline.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch exprName(call.Fun) {
		case "parkAckTimer":
			seam++
		case "tea.Tick", "time.After", "time.AfterFunc", "time.NewTimer":
			direct++
		}
		return true
	})
	if seam != 1 || direct != 0 {
		t.Errorf("parkAllDeadline reaches the seam %d times and a clock directly %d times, want 1 and 0. "+
			"A direct tick is invisible to parkAckTimer, so a delay of zero - which turns ⌃Q into a key "+
			"that reports `the daemon did not answer` before the write has landed - leaves every test "+
			"here green", seam, direct)
	}
}

// loopQuiet is how long loopMessages gives one command to produce a message
// before reading it as one that is waiting rather than one that is working.
//
// Every command it is pointed at writes to a recorder or returns a constant, so
// the real figure is microseconds; this is slack for a loaded machine, and the
// only thing that costs it is a command that is deliberately waiting - which is
// the case the helper exists to distinguish.
const loopQuiet = 250 * time.Millisecond

// loopMessages runs a command tree the way Bubble Tea's loop does and returns
// the messages it produced promptly.
//
// runLikeTheLoop's shape with the messages kept rather than dropped, and with a
// bound per leaf. The bound is what makes it usable on ⌃Q: that command tree now
// contains a command that is **waiting on the daemon's answer**, and a helper
// that blocked on it would turn "this does not quit yet" into a test that takes
// as long as the wait it is asserting about. A command still waiting has
// produced nothing, which is exactly what a caller asking "did this quit"
// needs.
//
// It may only be pointed at commands whose writes do not block - a recorder
// rather than a net.Pipe with nobody reading - because a leaf that never
// returns leaves its goroutine behind.
func loopMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	produced := make(chan tea.Msg, 1)
	go func() { produced <- cmd() }()

	var msg tea.Msg
	select {
	case msg = <-produced:
	case <-time.After(loopQuiet):
		return nil
	}
	// tea.sequenceMsg is unexported and tea.BatchMsg is a slice of the same
	// shape, so both are reached by reflection - runLikeTheLoop's own reason.
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for i := range v.Len() {
		inner, ok := v.Index(i).Interface().(tea.Cmd)
		if !ok {
			continue
		}
		out = append(out, loopMessages(inner)...)
	}
	return out
}

// quits reports whether a command tree ends the program.
func quits(cmd tea.Cmd) bool {
	for _, msg := range loopMessages(cmd) {
		if _, quit := msg.(tea.QuitMsg); quit {
			return true
		}
	}
	return false
}

// The window closes when the daemon has answered, and not on the keypress.
//
// **This is the property the exit line rests on.** cmd/wake prints `Parking 3
// agents.` off the final model rather than asking a daemon that is already
// shutting down - which is right, and was unconditional: the sentence was
// decided by the keypress, so a write that never reached the daemon printed it
// too. Park exists so that stopping an agent is recoverable, and a park the
// operator was told happened and did not is the exact failure that feature was
// built to prevent.
//
// So ⌃Q's own command may not carry tea.Quit at all. What ends the program is
// the reply to the question written behind the verb - or, when there is no
// reply, one of the three settlements below, each of which says on the way out
// that it could not confirm.
func TestCtrlQDoesNotCloseTheWindowUntilTheDaemonHasAnswered(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(160, 30).withAgents("alex", "sydney")

	m, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	if quits(cmd) {
		t.Fatal("⌃Q's own command ends the program. Nothing has confirmed anything at that point: " +
			"the frame may have gone nowhere, and the exit line promises a parked fleet regardless. " +
			"The daemon's answer is what may close this window")
	}

	// A **batch off the ring**, which is the only path a real daemon's reply
	// takes: the pump reads the socket and Update folds what it collected. A
	// test that handed apply the frame on its own would not see App.stream
	// re-arming the read instead of ending the program, which is where the
	// realistic mutation lives - the answer arrives, is folded correctly, and
	// the window stays open because that path did not ask whether it should.
	st := rpc.Status{Running: true}
	reply := rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}
	_, settled := m.Update(streamMsg{batch: batch{frames: []rpc.Frame{reply}}})
	if !quits(settled) {
		t.Fatal("the reply to ⌃Q's own question did not end the program, so the key parks the fleet " +
			"and leaves the window open - which is ⌃C's job done N times")
	}
}

// The answer and the hang-up arrive in one batch, and the answer still wins.
//
// **This is the ordinary shape of a successful ⌃Q, not an edge case.** The
// daemon enqueues the status reply, parks the fleet, writes the book and then
// closes every client - and one Update folds whatever the ring holds by then, so
// the batch that carries the reply routinely carries `done` behind it. App.stream
// folds the frames and goes straight to hungUp, so a settle that only the
// re-arm path consulted was a settle nothing ever turned into a quit: the window
// stayed open, reattaching to a daemon that had just parked the fleet and
// exited, and the operator was left in a dead room by the key whose job is to
// close it.
//
// It cost fifteen seconds of an end-to-end lifecycle test to find and is one
// Update to state, which is the whole argument for it being here as well.
func TestTheAnswerIsNotLostWhenTheDaemonHangsUpInTheSameBatch(t *testing.T) {
	fresh(t)
	holdParkAck(t)
	a := newRoomApp(t).withSize(160, 30).withAgents("alex", "sydney")

	m, _, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	st := rpc.Status{Running: true}
	reply := rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}
	m, end := m.Update(streamMsg{batch: batch{
		frames: []rpc.Frame{reply},
		done:   true,
		err:    errDaemonHungUp,
	}})

	if !quits(end) {
		t.Fatal("the batch carrying ⌃Q's answer and the daemon's hang-up left the window open. That is " +
			"what a working ⌃Q looks like on the wire - reply, then the clients closed - so this is " +
			"every successful quit, not an edge")
	}
	if _, _, err := m.(App).ParkedFleet(); err != nil {
		t.Errorf("the ask settled as %v, want no error: the reply was in the batch and it is the "+
			"acknowledgement. Reading the hang-up over it reports a failure that did not happen", err)
	}
	if strings.Contains(shown(m), "reattach") {
		t.Error("the window tried to reattach on the way out of ⌃Q. connect forks a daemon when nothing " +
			"is listening, so that is a fresh daemon holding no sessions, started by the key whose " +
			"last act was asking the old one to park them")
	}
}

// The first settlement wins, and the ones behind it change nothing.
//
// Four routes settle this ask and three of them are timers or transport events
// that cannot be cancelled once armed - so the deadline **always** fires,
// including three seconds after a park was confirmed. Without this the ordinary
// successful ⌃Q would be re-settled as "the daemon did not answer", and the
// exit line would be a lie in the other direction.
func TestAnAnswerThatArrivedIsNotOverwrittenByTheDeadlineBehindIt(t *testing.T) {
	fresh(t)
	held := holdParkAck(t)
	a := newRoomApp(t).withSize(160, 30).withAgents("alex", "sydney")

	m, _, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	st := rpc.Status{Running: true}
	reply := rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}
	m, _ = m.Update(streamMsg{batch: batch{frames: []rpc.Frame{reply}}})

	m, _ = held.expire(t, m)
	if _, _, err := m.(App).ParkedFleet(); err != nil {
		t.Errorf("the deadline behind a confirmed park settled it as %v. Every ⌃Q that works ends this "+
			"way - the timer is armed and nothing cancels it - so an overwrite here makes the honest "+
			"exit line unreachable", err)
	}
}

// heldParkAck is ⌃Q's deadline, held rather than run.
//
// holdSettles' shape, for holdSettles' reason: the delay it asked for is
// recorded too, because a deadline scheduled for zero is one that does not wait
// and would otherwise look identical here to one that does - and a zero here
// reports "the daemon did not answer" over a write that is still in flight.
type heldParkAck struct {
	delay time.Duration
	msg   tea.Msg
}

// closedConn is a connection whose every write fails, which is what a daemon
// that went away leaves behind. Both ends are closed, so nothing blocks and
// nothing has to be drained.
func closedConn(t *testing.T) net.Conn {
	t.Helper()
	mine, theirs := net.Pipe()
	_ = mine.Close()
	_ = theirs.Close()
	return mine
}

func holdParkAck(t testing.TB) *heldParkAck {
	t.Helper()
	held := &heldParkAck{}
	original := parkAckTimer
	parkAckTimer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		held.delay, held.msg = d, fn(time.Time{})
		return nil // not fired; nothing is delivered until the test says so
	}
	t.Cleanup(func() { parkAckTimer = original })
	return held
}

// expire delivers what the held deadline was holding.
func (h *heldParkAck) expire(t *testing.T, m tea.Model) (tea.Model, tea.Cmd) {
	t.Helper()
	if h.msg == nil {
		t.Fatal("⌃Q scheduled no deadline at all, so a daemon that goes quiet leaves the window open " +
			"forever with a fleet nobody can see")
	}
	if h.delay <= 0 {
		t.Fatalf("⌃Q's deadline was scheduled for %v. A wait of zero settles the ask before the write "+
			"has reached the socket, so every ⌃Q reports a daemon that did not answer", h.delay)
	}
	return m.Update(h.msg)
}

// A park that cannot be confirmed ends the program anyway, and says why.
//
// **Both halves are the decision, and they were made once already: a park that
// fails must not hang the quit forever and must not be silently dropped.** A
// window that stays open because a daemon went quiet is worse than the bug -
// ⌃Q's whole promise is that it closes - and a window that closes claiming a
// parked fleet it never confirmed is the bug. So every route out settles, and
// what it settles with is what cmd/wake prints after the alt screen is gone.
//
// Three routes, one table, because the failure is one class with three
// producers and a check pointed at one of them would leave the other two
// reporting a parked fleet.
// Every route is driven the way it actually arrives. **That is not a
// formality**: the write row hands nothing in, it writes to a connection that is
// really broken and asks what came back, because the first version of this
// delivered a parkAllMsg by hand - and a mutation that made askParkAll discard
// the write's error entirely left the whole suite green. A test that observes
// the end state proves the end state and not what produced it.
func TestEveryWayCtrlQGoesUnansweredStillClosesTheWindowAndSaysSo(t *testing.T) {
	cases := []struct {
		name string
		// conn is what ⌃Q writes to. Nil is a connection that works.
		conn func(t *testing.T) net.Conn
		// settle drives the model from just-after-⌃Q to whatever ends it, and
		// nil means the ask settles itself out of the command ⌃Q returned.
		settle func(t *testing.T, m tea.Model, held *heldParkAck) (tea.Model, tea.Cmd)
		want   string
	}{{
		name: "the write to the daemon failed",
		conn: closedConn,
		want: parkAllFailed,
	}, {
		name: "there was nothing to write to",
		conn: func(*testing.T) net.Conn { return nil },
		want: errParkAllUnsent.Error(),
	}, {
		name: "the daemon never answered",
		settle: func(t *testing.T, m tea.Model, held *heldParkAck) (tea.Model, tea.Cmd) {
			return held.expire(t, m)
		},
		want: errParkAllUnknown.Error(),
	}, {
		name: "the daemon hung up without saying",
		settle: func(t *testing.T, m tea.Model, _ *heldParkAck) (tea.Model, tea.Cmd) {
			return m.(App).hungUp(io.EOF)
		},
		want: errParkAllHungUp.Error(),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			held := holdParkAck(t)
			conn := net.Conn(newRecorder(t))
			if tc.conn != nil {
				conn = tc.conn(t)
			}
			a := NewRoomApp(conn, Stream{}, nil).withSize(160, 30).withAgents("alex", "sydney")

			m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
			if quits(cmd) {
				t.Fatal("⌃Q ended the program on the keypress")
			}
			settle := tc.settle
			if settle == nil {
				// The transport answered for itself. What came back off ⌃Q's
				// own command is the message, and folding it is the whole of
				// what the loop would do with it.
				settle = func(t *testing.T, m tea.Model, _ *heldParkAck) (tea.Model, tea.Cmd) {
					t.Helper()
					for _, msg := range loopMessages(cmd) {
						if answer, ok := msg.(parkAllMsg); ok {
							return m.Update(answer)
						}
					}
					t.Fatal("the write reported nothing at all. It is the only thing that can say " +
						"this ask failed before the daemon ever heard it, and a discarded error " +
						"there leaves the deadline to report a silent daemon three seconds later " +
						"about a frame that never left this process")
					return m, nil
				}
			}
			m, end := settle(t, m, held)
			if !quits(end) {
				t.Fatal("the ask went unanswered and the window stayed open. ⌃Q's promise is that it " +
					"closes, and a wait with no bound on it is a fleet nobody can see behind a " +
					"terminal nobody can leave")
			}
			n, pressed, err := m.(App).ParkedFleet()
			if !pressed || n != 2 {
				t.Errorf("the final model reports (%d, %v), want (2, true): the count is what this "+
					"client knows and it does not stop knowing it because the ask failed", n, pressed)
			}
			if err == nil {
				t.Fatal("the final model reports no error, so cmd/wake prints `Parking 2 agents.` " +
					"about a fleet nothing confirmed. That sentence is the whole of outstanding bug 3")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the reason reads %q, and %q is what an operator needs to act on it", err, tc.want)
			}
		})
	}
}

// ⌃Q with nothing to write to says nothing was asked, rather than waiting out a
// deadline for the answer to a question nobody was asked.
//
// **The failure it names is a nil command**, which is how a.write reports having
// no connection - a success from every angle a tea.Sequence can see, and the
// silent total failure this whole change is about: the frame went nowhere, the
// program ended, and the exit line promised a parked fleet. It is also the one
// outcome that knows the fleet is untouched, which is why it has a sentence of
// its own rather than the general one.
func TestCtrlQWithNoConnectionSaysNothingWasAskedRatherThanWaiting(t *testing.T) {
	fresh(t)
	held := holdParkAck(t)
	a := NewRoomApp(nil, Stream{}, nil).withSize(160, 30).withAgents("alex")

	m, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !handled {
		t.Fatal("⌃Q was not taken by App.key")
	}
	var answer tea.Msg
	for _, msg := range loopMessages(cmd) {
		if settle, ok := msg.(parkAllMsg); ok {
			answer = settle
		}
	}
	if answer == nil {
		t.Fatal("⌃Q over no connection settled nothing. a.write reports having no connection by " +
			"handing back a nil command, so a sequence runs it as a success and the deadline is all " +
			"that is left - three seconds of a frozen window, then a vaguer sentence than this " +
			"knows how to say")
	}
	m, end := m.Update(answer)
	if !quits(end) {
		t.Fatal("the settlement did not end the program")
	}
	if _, _, err := m.(App).ParkedFleet(); !errors.Is(err, errParkAllUnsent) {
		t.Errorf("the final model reports %v, want %v: this is the one failure that knows the fleet is "+
			"untouched, and the sentence an operator reads should say so", err, errParkAllUnsent)
	}
	// The deadline is still armed, because parkFleet is uniform and does not
	// special-case this. It costs a timer that finds an ask already settled -
	// first-wins - and buys one decision in one place.
	if held.msg == nil {
		t.Error("no deadline was armed at all, which is the property every other route out depends on")
	}
}

// The answer is a reply, and never a push.
//
// **A push can already have been in the socket when ⌃Q was pressed.** The daemon
// broadcasts one whenever any session changes state, so at thirty agents there
// is nearly always one in flight - and reading one as this ask's answer confirms
// a park the daemon has not been told about yet, which is the bug arriving
// through its own fix. That is daemon.Status's own rule, on the one connection
// this package now writes a question on.
func TestCtrlQIsAnsweredByAReplyAndNeverByAPush(t *testing.T) {
	fresh(t)
	holdParkAck(t)
	a := newRoomApp(t).withSize(160, 30).withAgents("alex", "sydney")

	m, _, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})

	st := rpc.Status{Running: true}
	m, cmd := m.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}})
	if quits(cmd) {
		t.Fatal("a status push closed the window. A push is the daemon announcing somebody else's " +
			"state change and answers no question: one already in flight when ⌃Q was pressed would " +
			"confirm a park-all the daemon had not been handed yet")
	}
	if _, _, err := m.(App).ParkedFleet(); err != nil {
		t.Errorf("the push settled the ask as %v rather than leaving it open", err)
	}

	m, cmd = m.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}})
	if !quits(cmd) {
		t.Fatal("the reply did not close the window")
	}
	if _, _, err := m.(App).ParkedFleet(); err != nil {
		t.Errorf("the reply settled the ask as %v, want no error: reaching it means the daemon "+
			"dispatched the FrameParkAll written in front of it", err)
	}
}

// A second ⌃Q while the first is still waiting changes nothing.
//
// Two waits would be two deadlines racing one answer, and the loser settles an
// ask that was already settled - so a confirmed park could be overwritten by
// "the daemon did not answer" three seconds later. It is also what an impatient
// operator does when a window does not close instantly, which is precisely the
// case this change introduced.
func TestASecondCtrlQWhileTheFirstIsWaitingIsANoOp(t *testing.T) {
	fresh(t)
	held := holdParkAck(t)
	a := newRoomApp(t).withSize(160, 30).withAgents("alex", "sydney")

	m, _, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	first := held.msg
	m2, cmd, _ := m.(App).key(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if quits(cmd) {
		t.Fatal("the second ⌃Q ended the program on its own keypress, which is the first press's bug " +
			"reintroduced by an operator pressing the key twice")
	}
	if held.msg != first {
		t.Error("the second ⌃Q scheduled a second deadline. Two deadlines race one answer, and the " +
			"loser overwrites a confirmed park with `the daemon did not answer`")
	}

	st := rpc.Status{Running: true}
	m3, end := m2.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}})
	if !quits(end) {
		t.Fatal("the reply did not close a window two ⌃Q presses had asked to close")
	}
	if _, _, err := m3.(App).ParkedFleet(); err != nil {
		t.Errorf("after two presses the ask settled as %v, want no error", err)
	}
}

// ⌃C with no agent in front of it says so rather than doing nothing.
//
// A control that does nothing and says nothing is the failure the legend rule
// exists for, arriving at runtime instead of in the legend - and this is the
// one key where the silent alternative is also wrong in the other direction,
// because "⌃C did nothing" is indistinguishable from "⌃C parked something".
func TestCtrlCInAnEmptyRoomNamesWhatWouldParkSomething(t *testing.T) {
	a := newRoomApp(t).withSize(160, 30)

	m, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !handled {
		t.Fatal("⌃C was not taken by App.key")
	}
	if cmd != nil {
		t.Fatalf("⌃C parked something with no agent selected: %+v", sentFrames(t, m.(App), cmd))
	}
	if got := shown(m); !strings.Contains(got, noParkTarget) {
		t.Errorf("⌃C with nothing selected said nothing:\n%s", got)
	}
}

// ⌃C on a session that is already parked is silent, and so is one on a session
// that has ended.
//
// Nothing is lost either way, which is the same trade ⎋ makes for an ended
// session: a park that is not sent costs nothing, and a sentence about it is a
// notice row spent on a keystroke that was already true.
func TestCtrlCOnAConversationWithNoProcessWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		what  string
		state string
	}{
		{"parked", rpc.StateParked},
		{"ended", rpc.StateEnded},
	} {
		t.Run(tc.what, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(160, 30).
				withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: tc.state})

			_, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlC})
			if !handled {
				t.Fatal("⌃C was not taken by App.key")
			}
			if cmd != nil {
				t.Errorf("⌃C wrote a park for a %s session, which has no process to stop: the daemon "+
					"refuses it and the refusal lands on the notice row of somebody who pressed a key "+
					"twice", tc.what)
			}
		})
	}
}

// A parked conversation says what to do; an ended one says there is nothing to
// do. They are different states and the difference is a verb.
func TestTypingIntoAParkedConversationNamesTheCommandThatBringsItBack(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(160, 30).
		withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateParked}).
		withDraft("carry on")

	m, cmd := a.sendDM("carry on", nil)
	if cmd != nil {
		t.Fatalf("a message went out to a parked session: %+v", sentFrames(t, m.(App), cmd))
	}
	got := shown(m)
	if !strings.Contains(got, resumeVerb) {
		t.Errorf("typing into a parked conversation does not name %s, which is the one thing to do "+
			"next and is one command away:\n%s", resumeVerb, got)
	}
	if strings.Contains(got, endedAdvice) {
		t.Errorf("a parked conversation is answered with the *ended* sentence, which says there is "+
			"nothing to be done about a session that is one command from coming back:\n%s", got)
	}
	if a.composer().Value() == "" {
		t.Error("the draft was destroyed as well as undelivered, and here it is worth more than usual: " +
			"the thing to do next is one command and then send it")
	}
}

// --- what ⌃C does in each state a report can carry ------------------------

// parkVerdict is what ⌃C does to a session in one state: whether a FramePark
// goes out, and what the notice row must say when one does not.
//
// says is "" for the cases that are deliberately silent. A park that is not
// sent costs nothing when there is nothing to park, and a sentence about it is
// a notice row spent on a keystroke that was already true — the same trade ⎋
// makes for an ended session.
type parkVerdict struct {
	writes bool
	says   string

	// carried is ⌃Q's half: whether a session in this state is one the next
	// `wake` is handed. It lives in ⌃C's table rather than in one of its own
	// because the two keys read the same state space and disagree on exactly
	// one member, and a second table would let that disagreement be made twice
	// without anybody noticing it was a disagreement.
	//
	// **Blocked is where they part, and the asymmetry is deliberate rather than
	// an oversight.** ⌃C refuses a blocked agent because parking closes stdin
	// under an outstanding ask and the agent is told that is a denial. ⌃Q has
	// no such option: the window is closing either way, and FrameParkAll is one
	// frame the daemon applies to the fleet - so a blocked session is parked,
	// booked, and carried. Counting it is honest about that; leaving it out
	// would under-report the fleet in the one state where something was already
	// lost.
	carried bool
}

// parkStates is what ⌃C does in each state a running daemon can report.
//
// **Blocked is the one that is not obvious, and it is the reason this table
// exists.** Park closes stdin, and CLAUDE.md records what that does to an
// outstanding permission ask in its own words: *"A question that dies because
// Wake closed stdin is indistinguishable from an operator deny —
// non_execution_kind: "permission-rule", listed in permission_denials, on a
// success turn."* It is not lost work — the process exits cleanly, the park
// completes, /resume brings it back — it is worse in one specific way: a
// decision the operator never made is written into the transcript, and it
// **survives the wake**. The agent comes back believing it was told no about a
// question nobody saw.
//
// So blocked is a refusal that speaks, and it names ⎋, which handles exactly
// this state cleanly — the ask is withdrawn on a control_cancel_request and the
// session stays usable. ⌃C and ⎋ share interruptTarget and are one legend row
// apart with opposite safety on this one state.
//
// Working and silent are parks, and that is the design rather than an
// oversight: park is FramePark → stop → close stdin, which lets an in-flight
// turn finish and never signals, so nothing is half-written. Nothing is waiting
// on the operator in either state.
//
// **Ended is the only state ⌃Q does not carry**, and it is the one the daemon's
// own `bookable` refuses: `ended && (parking || parked)` is false for a session
// that simply finished, so there is nothing to write down and nothing to offer
// back. Every other state has a session the next `wake` can be handed - which
// includes one that is *already* parked, because `bookParked` counts an entry
// it finds already in the book as booked and the next daemon restores it.
//
// A hand-written decision checked against a derived set rather than the derived
// set itself, so a seventh reachable state is a build failure until somebody
// says what ⌃C does with it - and, since 2026-08-11, what ⌃Q's count does with
// it too.
var parkStates = map[string]parkVerdict{
	rpc.StateIdle:    {writes: true, carried: true},
	rpc.StateWorking: {writes: true, carried: true},
	rpc.StateSilent:  {writes: true, carried: true},
	rpc.StateBlocked: {writes: false, says: parkWouldDeny, carried: true},
	rpc.StateEnded:   {writes: false, carried: false},
	rpc.StateParked:  {writes: false, carried: true},
}

// ⌃Q's count is the table's `carried` column, asserted per member.
//
// Driven one state at a time so a count that is right in aggregate for the
// wrong reasons cannot pass: a fleet with one of each would let an
// over-count of ended cancel an under-count of parked, which is very nearly the
// arithmetic the defect had.
func TestCtrlQCarriesForwardExactlyWhatTheTableSays(t *testing.T) {
	for state, want := range parkStates {
		t.Run(state, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(160, 30).
				withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: state})

			m, _, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlQ})
			if !handled {
				t.Fatal("⌃Q was not taken by App.key")
			}
			n, quit, _ := m.(App).ParkedFleet()
			if !quit {
				t.Fatal("⌃Q did not record that it is why this program is ending")
			}
			if got := n == 1; got != want.carried {
				t.Errorf("⌃Q over a fleet of one %q agent counts %d, want %d: the number is what this "+
					"shutdown hands to the next `wake`, and it is the last sentence the operator reads",
					state, n, map[bool]int{true: 1, false: 0}[want.carried])
			}
		})
	}
}

// The reachable domain, derived from agent.stateLocked rather than from rpc's
// constant block, and asserted in both directions.
//
// Rung 4, and the same scan forkguard_test.go runs for ⌃F — reused rather than
// re-spelled, because the two keys read the same producer. StateOrphaned is
// excused there with the producer that makes it unreachable, and the excuse is
// the same one here, so this reads that map rather than restating it.
func TestParkHasAVerdictForEveryStateARunningDaemonCanReport(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := parkStates[state]
		why, excused := unreachableInAForkReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says what ⌃C "+
				"does to an agent in it. Blocked is why this table exists: parking closes stdin, and an "+
				"ask that dies that way is recorded as a denial the operator never made", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it - so the cell "+
				"asserts something about an input no daemon can produce, which reads as coverage", name, state)
		}
	}
	for state := range reachable {
		if _, decided := parkStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says what ⌃C does with it", state)
		}
	}
}

// The behaviour asserted per member, from a DM — where a *successful* park is
// silent, so a refusal that speaks is separable from one that does not.
//
// The narrowing this kills and an example cannot: a guard keyed on ended alone,
// which is what park shipped with, and which parks a blocked agent into a
// permanent recorded denial.
func TestCtrlCDoesWhatTheTableSaysInEveryStateAReportCanCarry(t *testing.T) {
	for state, want := range parkStates {
		t.Run(state, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(160, 30).
				withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: state})
			// Emptied *after* the fleet report, because noteEnding announces
			// this client's own session dying and that is the report's
			// sentence rather than ⌃C's. What is under test is what the key
			// said.
			notice.Reset()

			m, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlC})
			if !handled {
				t.Fatal("⌃C was not taken by App.key")
			}
			switch {
			case want.writes && cmd == nil:
				t.Fatalf("⌃C on a %q agent wrote nothing: that agent has a process, and the key an "+
					"operator reaches for did nothing at all", state)
			case !want.writes && cmd != nil:
				t.Fatalf("⌃C on a %q agent wrote %+v", state, sentFrames(t, m.(App), cmd))
			}
			if want.writes {
				if f := sentFrame(t, m.(App), cmd); f.Kind != rpc.FramePark || f.SessionID != "s1" {
					t.Errorf("⌃C on a %q agent wrote %+v, want a FramePark for s1", state, f)
				}
				return
			}
			// The notice sink rather than the rendered frame: the legend
			// draws ⎋ on every frame, so a Contains over the view is
			// satisfied by the composer and says nothing about what ⌃C
			// reported.
			got, said := notice.Latest()
			if want.says == "" {
				if said {
					t.Errorf("⌃C on a %q agent said %q. There is nothing to park and nothing lost, so "+
						"this is silent - the same trade ⎋ makes for an ended session", state, got.String())
				}
				return
			}
			if !said {
				t.Fatalf("⌃C on a %q agent did nothing and said nothing, which is the one outcome this "+
					"project treats as worse than a refusal", state)
			}
			if !strings.Contains(got.String(), "permission") {
				t.Errorf("⌃C on a %q agent refused and did not say why. Parking now closes stdin under "+
					"the ask, which the agent is told is a denial - and that survives the wake, so the "+
					"session comes back believing it was refused something nobody saw:\n%s", state, got.String())
			}
			if !strings.Contains(got.String(), "esc") {
				t.Errorf("⌃C on a %q agent refused without naming a key that works. ⎋ withdraws the ask "+
					"cleanly and is one legend row away, and a refusal that says only \"no\" leaves an "+
					"operator with a key that does nothing and no idea when it would:\n%s", state, got.String())
			}
		})
	}
}

// ⌃C says what it asked for; the report says what happened.
//
// It used to say "parking @alex - /resume alex brings it back" on the keypress,
// which is a promise about an outcome this client has not seen yet. The daemon
// refuses a park for real reasons - a session that has already ended, one it
// does not hold - and the refusal arrives *afterwards*, so the operator reads a
// confident sentence and then a contradiction of it.
//
// ⌃F settled this shape already and CLAUDE.md states it: **the report is the
// confirmation and the keypress is not.** This is that rule applied one key
// over. The keypress names the ask; the fleet report naming the session as
// parked is what earns the sentence with `/resume` in it.
func TestParkingSaysWhatItAskedForAndTheReportSaysWhatHappened(t *testing.T) {
	fresh(t)
	// From the room, because that is where the sentence is said: a DM names its
	// agent in its own header, so parkedFormat is room-only by design.
	a := newRoomApp(t).withSize(200, 40).
		withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle})
	// interruptTarget reads the roster's selection when no DM has the keys, and
	// a status push populates the rows without moving the cursor.
	a.roster.Selected = "s1"

	m, _, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !handled {
		t.Fatal("⌃C was not handled, so nothing here is about parking")
	}
	a = m.(App)

	asked := latestNotice(t)
	if strings.Contains(asked, resumeVerb) {
		t.Errorf("the keypress says %q, which promises %s before the daemon has answered. A park the "+
			"daemon refuses makes that a sentence the next frame contradicts", asked, resumeVerb)
	}
	if !strings.Contains(asked, "alex") {
		t.Errorf("the keypress says %q and does not name the agent it is parking: in the room the "+
			"target is wherever the roster cursor is, which is not where the eye is", asked)
	}

	// And the report is what earns the promise.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true,
		Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateParked}}}})

	if got := latestNotice(t); !strings.Contains(got, resumeVerb) {
		t.Errorf("the report says a session is parked and the notice is %q. Nothing tells the operator "+
			"how to get it back, and %s is the whole reason ⌃C is safe to press", got, resumeVerb)
	}
}

// latestNotice is the newest line on the notice row, or a fatal if there is
// none - because "" would make every Contains assertion above pass or fail for
// the wrong reason.
func latestNotice(t *testing.T) string {
	t.Helper()
	n, ok := notice.Latest()
	if !ok {
		t.Fatal("nothing reached the notice row, so there is no sentence to read")
	}
	return n.String()
}

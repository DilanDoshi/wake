package main

// Names and labels as the command line sees them: creating one on purpose,
// reaching one by name, and what a row of `wake status` reads as.

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// --- `wake status` and the listings ------------------------------------------

// The spec's rendering: a session is a name and what it is working on, not a
// bare name and not a hex string.
func TestASessionReadsAsItsNameAndItsLabel(t *testing.T) {
	got := sessionTitle(rpc.SessionStatus{ID: idAlpha, Name: "sydney", Label: "dev-5748"})
	if got != "sydney <> dev-5748" {
		t.Errorf("sessionTitle = %q, want %q", got, "sydney <> dev-5748")
	}
}

// A session with nothing to say about what it is doing says nothing, rather
// than showing a separator with a hole after it - which reads as a label that
// failed to load.
func TestASessionWithNoLabelIsJustItsName(t *testing.T) {
	got := sessionTitle(rpc.SessionStatus{ID: idAlpha, Name: "sydney"})
	if got != "sydney" {
		t.Errorf("sessionTitle = %q, want a bare name", got)
	}
	if strings.Contains(got, strings.TrimSpace(nameLabelSeparator)) {
		t.Errorf("sessionTitle = %q, want no separator at all", got)
	}
}

// The label reaches the row somebody actually reads.
//
// Mutation check: reverting sessionLine to displayName leaves this failing at
// "the status row does not say what the session is working on".
func TestAStatusRowCarriesTheLabel(t *testing.T) {
	// nil, because this row is nobody's fork: forkedFrom returns "" for a row
	// with no ParentID whatever index it is handed.
	got := sessionLine(rpc.SessionStatus{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle}, nil)
	if !strings.Contains(got, "sydney <> dev-5748") {
		t.Errorf("the status row does not say what the session is working on: %q", got)
	}
	// The id column is load-bearing - `wake attach` resolves against it - so a
	// wider first column must not have pushed it off the row.
	if !strings.Contains(got, shortID(idAlpha)) {
		t.Errorf("the status row lost its id column: %q", got)
	}
}

// The handle and the title are different things and stay different. §7 routes
// on `@name`; a handle with a branch glued to it is not one.
func TestTheHandleStaysBareWhileTheTitleIsLabelled(t *testing.T) {
	s := rpc.SessionStatus{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle}
	if got := displayName(s); got != "sydney" {
		t.Errorf("displayName = %q, want the bare handle", got)
	}
	if got := sessionTitle(s); got == displayName(s) {
		t.Errorf("sessionTitle and displayName are the same string %q; one of the two has stopped doing its job", got)
	}
}

// --- the command line ---------------------------------------------------------

// `wake new` with a name is the way to create one on purpose, and `wake new`
// without one is bare `wake`. Both are the same verb, which is why the name is
// optional rather than a second command.
func TestNewTakesAtMostOneName(t *testing.T) {
	err := run([]string{cmdNew, "sydney", "marco"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("`wake new` took two names")
	}
	if !strings.Contains(err.Error(), "at most one name") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

func TestTheNameNewWasGiven(t *testing.T) {
	if got := chosenName([]string{cmdNew, "sydney"}); got != "sydney" {
		t.Errorf("chosenName = %q, want sydney", got)
	}
	if got := chosenName([]string{cmdNew}); got != "" {
		t.Errorf("chosenName with no name = %q, want the daemon to draw one", got)
	}
}

// The usage says the verb exists. A command nobody can discover is a command
// nobody has.
func TestTheUsageNamesTheVerbThatCreatesAnAgent(t *testing.T) {
	err := run([]string{"nonsense"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("an unknown verb was accepted")
	}
	// Every verb that starts an agent, because a command nobody can discover is
	// a command nobody has - and `wake fork` is the one somebody has to be told
	// about, since nothing else on the command line hints that a conversation
	// can be branched at all.
	for _, verb := range []string{"wake new", "wake fork"} {
		if !strings.Contains(err.Error(), verb) {
			t.Errorf("the usage does not mention `%s`: %v", verb, err)
		}
	}
}

// A name the daemon will not accept is reported on the terminal, before
// anything takes over the screen.
//
// This is the whole reason attach waits for its spawn to be confirmed. Without
// the wait, the refusal arrives after tea.NewProgram has the alt screen and
// lands on a notice row nobody is looking at yet, in a conversation with no
// agent behind it.
//
// It runs the real daemon - TestMain serves it when this binary is exec'd with
// `daemon`, which is what EnsureRunning forks - and never reaches a terminal,
// because the refusal comes back before the TUI is built.
func TestANameTheDaemonRefusesIsReportedBeforeTheTUIOpens(t *testing.T) {
	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)
	t.Cleanup(func() { _ = stopFleet(socket, io.Discard) })

	done := make(chan error, 1)
	go func() { done <- run([]string{cmdNew, "two words"}, io.Discard) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("`wake new \"two words\"` opened a conversation")
		}
		if !strings.Contains(err.Error(), "letters") {
			t.Errorf("the refusal does not say what a name may hold: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("`wake new` with a name the daemon refuses never returned; it is waiting for a confirmation that is not coming, or it opened a terminal")
	}
}

// The conversation is drawn under the name the *daemon* assigned. Bare `wake`
// asks for no name at all, so a build that carried the request through to the
// view would draw a header with nothing after the @ - which is exactly the "one
// agent reads as two" defect displayName was written for.
//
// Driven through the model rather than through attach, which needs a terminal:
// conversation is the half of converse that is wiring, split out for this.
//
// Mutation check: handing conversation anything other than displayName(sess)
// leaves this failing at "the DM header does not name the agent".
func TestTheConversationIsDrawnUnderTheNameTheDaemonAssigned(t *testing.T) {
	held := &connection{}
	t.Cleanup(held.close)

	app := conversation(tempSocket(t),
		rpc.SessionStatus{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle},
		nil, nil, ui.Stream{}, held)

	sized, _ := app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := sized.View()
	if !strings.Contains(view, "sydney") {
		t.Errorf("the DM header does not name the agent:\n%s", view)
	}
	// The handle, not the title: the label belongs in listings, and `@sydney`
	// is what §7 routes on.
	if strings.Contains(view, nameLabelSeparator) {
		t.Errorf("the DM header carries the label as well as the handle:\n%s", view)
	}
}

// --- the spawn handshake -------------------------------------------------------

// streamOf builds the stream awaitSpawn reads, without a socket. The frames it
// is given are all there is; the channels are closed, which is a daemon that
// hung up.
func streamOf(frames ...rpc.Frame) ui.Stream {
	fc := make(chan rpc.Frame, len(frames))
	for _, f := range frames {
		fc <- f
	}
	close(fc)
	ec := make(chan error, 1)
	close(ec)
	return ui.Stream{Frames: fc, Errs: ec}
}

func statusReply(sessions ...rpc.SessionStatus) rpc.Frame {
	st := rpc.Status{Running: true, Sessions: sessions}
	return rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}
}

// The name is assigned over there, so this is how the client learns it.
func TestTheSpawnConfirmationIsWhereTheNameComesFrom(t *testing.T) {
	got, _, _, err := awaitSpawn(streamOf(
		statusReply(rpc.SessionStatus{ID: idAlpha, Name: "sydney", Label: "dev-5748", State: rpc.StateIdle}),
	), idAlpha)
	if err != nil {
		t.Fatalf("awaitSpawn: %v", err)
	}
	if got.Name != "sydney" || got.Label != "dev-5748" {
		t.Errorf("awaitSpawn returned %+v, want the name and label the daemon assigned", got)
	}
}

// A refusal addressed to this session is this spawn's answer.
func TestARefusedSpawnIsAnError(t *testing.T) {
	_, _, _, err := awaitSpawn(streamOf(
		rpc.Frame{Kind: rpc.FrameError, SessionID: idAlpha, Text: "a live session is already called \"sydney\""},
	), idAlpha)
	if err == nil {
		t.Fatal("a refused spawn was reported as a session")
	}
	if !strings.Contains(err.Error(), "sydney") {
		t.Errorf("the error lost what the daemon said: %v", err)
	}
}

// Another client's failure is not this one's. One daemon serves every terminal
// on the machine, and its error frames are fanned out.
//
// Mutation check: matching a FrameError on any session leaves this failing at
// "another client's error was taken as this spawn's answer".
func TestAnotherSessionsErrorIsNotThisSpawnsAnswer(t *testing.T) {
	got, _, _, err := awaitSpawn(streamOf(
		rpc.Frame{Kind: rpc.FrameError, SessionID: idBeta, Text: "somebody else's problem"},
		statusReply(rpc.SessionStatus{ID: idAlpha, Name: "sydney", State: rpc.StateIdle}),
	), idAlpha)
	if err != nil {
		t.Fatalf("another client's error was taken as this spawn's answer: %v", err)
	}
	if got.Name != "sydney" {
		t.Errorf("awaitSpawn returned %+v", got)
	}
}

// A push is not a reply. The daemon announces state changes to every attached
// client, so one can be sitting in the socket already - and reading it as this
// spawn's confirmation would name a session that has not started.
//
// Mutation check: accepting FrameStatusPush leaves this failing at "a status
// push was read as this spawn's confirmation".
func TestAStatusPushIsNotASpawnConfirmation(t *testing.T) {
	push := statusReply(rpc.SessionStatus{ID: idAlpha, Name: "stale", State: rpc.StateIdle})
	push.Kind = rpc.FrameStatusPush

	got, _, read, err := awaitSpawn(streamOf(
		push,
		statusReply(rpc.SessionStatus{ID: idAlpha, Name: "sydney", State: rpc.StateIdle}),
	), idAlpha)
	if err != nil {
		t.Fatalf("awaitSpawn: %v", err)
	}
	if got.Name == "stale" {
		t.Fatal("a status push was read as this spawn's confirmation")
	}
	if len(read) != 1 || read[0].Kind != rpc.FrameStatusPush {
		t.Errorf("the push was dropped rather than handed on: %+v", read)
	}
}

// A daemon that hangs up before answering is an error with a reason, not a
// conversation with nothing in it.
func TestASpawnThatIsNeverAnsweredIsAnError(t *testing.T) {
	_, _, _, err := awaitSpawn(streamOf(), idAlpha)
	if err == nil {
		t.Fatal("a daemon that said nothing at all was treated as a successful spawn")
	}
	if !strings.Contains(err.Error(), "hung up") {
		t.Errorf("the error does not say what happened: %v", err)
	}
}

// A read failure is reported as one rather than as a silent hang-up.
func TestAReadFailureDuringASpawnIsReported(t *testing.T) {
	fc := make(chan rpc.Frame)
	close(fc)
	ec := make(chan error, 1)
	ec <- errors.New("decode frame: unexpected end of JSON input")
	close(ec)

	_, _, _, err := awaitSpawn(ui.Stream{Frames: fc, Errs: ec}, idAlpha)
	if err == nil {
		t.Fatal("a stream that failed was treated as a successful spawn")
	}
	if !strings.Contains(err.Error(), "decode frame") {
		t.Errorf("the error lost the reason: %v", err)
	}
}

// The transcript must not be the price of waiting for a name.
//
// The daemon starts fanning a session's events out on its own goroutine and
// *then* enqueues the spawn's confirmation, so an agent that is quick can put
// its first event ahead of the reply. A wait that dropped what it read past
// would lose the opening of the conversation - silently, and only sometimes.
//
// Mutation check: returning nil instead of read leaves this failing at "the
// event that arrived before the confirmation was dropped".
func TestAnEventThatArrivesBeforeTheConfirmationIsNotLost(t *testing.T) {
	early := rpc.Frame{Kind: rpc.FrameEvent, SessionID: idAlpha}

	_, _, read, err := awaitSpawn(streamOf(
		early,
		statusReply(rpc.SessionStatus{ID: idAlpha, Name: "sydney", State: rpc.StateIdle}),
	), idAlpha)
	if err != nil {
		t.Fatalf("awaitSpawn: %v", err)
	}
	if len(read) != 1 || read[0].Kind != rpc.FrameEvent {
		t.Fatalf("the event that arrived before the confirmation was dropped: %+v", read)
	}
}

// …and it goes back in front of the stream, in order, before anything the
// daemon says next.
func TestFramesReadPastGoBackInFrontOfTheStream(t *testing.T) {
	first := rpc.Frame{Kind: rpc.FrameEvent, SessionID: idAlpha, Text: "first"}
	second := rpc.Frame{Kind: rpc.FrameEvent, SessionID: idAlpha, Text: "second"}
	rest := rpc.Frame{Kind: rpc.FrameEvent, SessionID: idAlpha, Text: "third"}

	resumed := resume(streamOf(rest), []rpc.Frame{first, second})

	var got []string
	for f := range resumed.Frames {
		got = append(got, f.Text)
	}
	want := []string{"first", "second", "third"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the resumed stream delivered %v, want %v", got, want)
	}
}

// Nothing read past means no goroutine and no wrapper: the common case pays
// nothing for the rare one.
func TestAStreamWithNothingReadPastIsHandedBackUntouched(t *testing.T) {
	stream := streamOf()
	if got := resume(stream, nil); got.Frames != stream.Frames {
		t.Error("resume wrapped a stream with nothing to replay")
	}
}

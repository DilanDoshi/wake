package ui

// The room, end to end: an agent this client never spawned saying something,
// a blocked agent promoted into a card and answered where it appears, and a
// broadcast costing one command and N frames.
//
// Split from app_test.go, which was already at two thirds of this project's
// 800-line file maximum before any of this arrived.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// --- the room ------------------------------------------------------------

// The one line this whole task is. daemon.fanOut has always broadcast every
// session's events to every attached client, and App.apply threw away the ones
// whose SessionID was not its own; the room is that discard turned into a fold.
func TestAnotherAgentsWordsReachTheRoomInsteadOfBeingThrownAway(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s2", Name: "john", Label: "api-v2", Dir: "/repos/api", State: rpc.StateWorking},
	}}})
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "Fixed the retry header, tests pass",
	}})

	if out := shown(a); !strings.Contains(out, "john") || !strings.Contains(out, "retry header") {
		t.Errorf("an agent this client did not spawn said something and the room did not show it. daemon.fanOut already broadcasts every session's events to every client - App.apply was throwing them away, and the room is that discard turned into a fold:\n%s", out)
	}
}

// A frame gap can eat the KindTurnEnd that fold clears inDM on, leaving a
// DM-sent turn's flag stuck true - which would then hold this agent's *next*
// turn out of the room, even though the operator is no longer in a DM with it.
// The report is the gap-robust second observable of that turn-end: an agent
// reported working→idle has inDM reconciled in Fleet.WithStatus, so its next
// prose reaches the room. This is BUG-30's room-suppression half - notedGap
// (the mode/tool/counts half) never touches inDM.
//
// Mutation check: drop `a.inDM = false` from WithStatus's working→idle edge and
// the follow-up line never reaches the room.
func TestAReportedTurnEndClearsAStaleInDMSoTheRoomGetsTheNextTurn(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s2", Name: "john", Label: "api-v2", Dir: "/repos/api", State: rpc.StateWorking},
	}}})
	// The operator sent this turn from john's DM, so its prose stays private.
	a.fleet = a.fleet.sending("s2", true)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the private DM answer",
	}})
	if strings.Contains(shown(a), "private DM answer") {
		t.Fatalf("a DM-sent turn's prose reached the room:\n%s", shown(a))
	}
	// The turn's KindTurnEnd is dropped in a gap, so fold never clears inDM. The
	// next report says john is idle - the working→idle edge must reconcile it.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s2", Name: "john", Label: "api-v2", Dir: "/repos/api", State: rpc.StateIdle},
	}}})
	if a.fleet.inDM("s2") {
		t.Fatal("a reported working→idle left inDM stale; the next turn's prose will be held out of the room")
	}
	// A new turn's words now belong in the room.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the public follow-up",
	}})
	if !strings.Contains(shown(a), "public follow-up") {
		t.Errorf("after a reported turn-end the agent's next prose was still suppressed from the room:\n%s", shown(a))
	}
}

// A blocked agent is the highest-priority thing in a fleet and the worst thing
// to be invisible: it is stopped dead until somebody answers, with no timeout
// and no heartbeat. Its conversation is where it is put and where it is
// answered - the room draws no card, so this is the whole of the surface.
func TestABlockedAgentIsPromotedIntoItsConversationAndIsAnswerableThere(t *testing.T) {
	a := blockedPane(t)
	// The card's own headline, not the tool's argument: the pane's transcript
	// draws the tool call too, so `rm -rf build/` on screen says nothing about
	// whether the card is up.
	if !strings.Contains(shown(a), headlineFor("Bash")) {
		t.Fatalf("a blocked agent is invisible in its own conversation:\n%s", shown(a))
	}

	a, cmd := settleKey(a, cardAllowKey)
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameAllow || f.RequestID != "r1" || f.SessionID != "s2" {
		t.Errorf("answering the card sent %+v, want an allow for r1 on s2", f)
	}
	// The card, not the words. The room *announces* a blocked agent in its own
	// transcript, and that line is a record which stays - so "wants Bash" is
	// still on screen after the answer, by design, and the string can no longer
	// tell an offered card from a report that one happened. What has to come
	// down is the card.
	if _, up := a.cardOf(a.focus); up {
		t.Error("the card stayed up after being answered")
	}
}

// `a` is a letter people type. If it could grant a tool call while somebody
// writes the word "analyse", that is the unsafe direction of exactly the
// failure the hint line's rule exists for.
//
// Mutation check: deleting a.composerEmpty() from App.key fails this at "a
// keystroke in a draft answered a permission request".
func TestTypingIntoAPaneDoesNotAnswerItsCard(t *testing.T) {
	// The pane holding the card, because that is the only pane whose keys are
	// ever read against one: a room-focused App would pass this with the gate
	// deleted, since the room draws no card to answer.
	a := blockedPane(t).withDraft("all agents please stand by")

	a2, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardAllowKey}})

	// Asserted on the card rather than on the command, because a keystroke
	// that reaches the composer produces one either way - the cursor's blink.
	// The card coming down is what settling it looks like, and it is what
	// deleting a.composerEmpty() from App.key does here.
	if a2.cards.Len() != 1 {
		t.Fatal("a keystroke in a draft answered a permission request. `a` is a letter people type; it may only mean allow when the composer is empty, or somebody grants a tool call while writing the word 'analyse'")
	}
	if !strings.HasSuffix(a2.composer().Value(), "a") {
		t.Errorf("the keystroke was swallowed instead of reaching the draft: %q", a2.composer().Value())
	}
}

// A question is refused beneath the daemon if it is short one choice, and a
// refusal the operator has to read is worse than a key that was not offered.
// What the key must never do instead is fall back to a bare allow: that runs
// the tool while telling the model nobody replied, on a turn that still ends
// successfully with nothing anywhere reporting the loss.
func TestAnUnansweredQuestionDoesNotOfferTheAnswerKeyAndNeverFallsBackToABareAllow(t *testing.T) {
	a := paneAsking(t)

	before, _ := a.cards.Top()
	if before.Answered() {
		t.Fatal("the recorded question arrives already answered, so this test proves nothing")
	}
	if strings.Contains(shown(a), cardAnswerKeys) {
		t.Errorf("a question nobody has chosen an option for advertises %q:\n%s", cardAnswerKeys, shown(a))
	}
	if !strings.Contains(shown(a), chooseKeys(otherIndex(before.Detail.Questions[0])+1)) {
		t.Errorf("a question offers no way to choose, so the answer key could never become reachable:\n%s", shown(a))
	}

	a2, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardAllowKey}})
	if a2.cards.Len() != 1 {
		t.Error("an unanswered question was settled anyway: the card came down with nothing chosen")
	}
	// And the rune was not swallowed either. A key the card has no meaning for
	// is the first character of what somebody is about to type.
	if a2.composer().Value() != string(cardAllowKey) {
		t.Errorf("the draft is %q: a rune the card declined should have reached the composer", a2.composer().Value())
	}
}

// The other half, and the one that has to be a different frame kind: a bare
// allow on a question runs the tool while telling the model nobody replied, on
// a turn that still ends successfully with nothing anywhere reporting the loss.
//
// A fresh App rather than the one above, deliberately. Composers share a text
// area by pointer, so the 'a' that fell through to the draft there is in every
// copy of that model - and a non-empty draft is exactly the state in which no
// card key is read at all.
func TestAnsweringEveryQuestionSendsTheAnswerKindAndNotABareAllow(t *testing.T) {
	a := paneAsking(t)

	// Every question, because core.EncodeAnswer requires a choice for each one
	// it was asked - a missing one is the same lost answer arriving one
	// question at a time.
	asked := questionCount(a)
	if asked < 2 {
		t.Fatalf("the recorded ask puts %d questions: this test is meant to walk more than one", asked)
	}
	for range asked {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption}})
	}
	// The review step, which is what a fully answered question offers instead
	// of a settle key: it names every answer before the one press that sends
	// them, where the arm named only a verb.
	if !strings.Contains(shown(a), reviewSubmitLabel) {
		t.Fatalf("choosing an option for every question did not reach the review:\n%s", shown(a))
	}

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameAnswer {
		t.Errorf("answering a question sent a %q frame, want %q: a bare allow runs the tool and tells the model nobody replied", f.Kind, rpc.FrameAnswer)
	}
	if len(f.Answers) != asked {
		t.Errorf("the answer frame carries %d choices for %d questions, which is the shape core.EncodeAnswer refuses", len(f.Answers), asked)
	}
	if len(f.UpdatedInput) == 0 {
		t.Error("the answer frame carries no input to fold the choices into, and core.EncodeAnswer refuses one that arrives without it")
	}
}

// Deny is the other half of a card, and it is a different frame kind because
// the payloads are different things: an allow carries the input a tool will
// receive, a deny carries prose the model reads verbatim.
func TestDenyingACardSendsARefusalWithSomethingInIt(t *testing.T) {
	a, cmd := settleKey(blockedPane(t), cardDenyKey)
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameDeny || f.RequestID != "r1" || f.SessionID != "s2" {
		t.Errorf("denying the card sent %+v, want a deny for r1 on s2", f)
	}
	if strings.TrimSpace(f.Reason) == "" {
		t.Error("the deny carries no reason. It reaches the model verbatim as the tool result, and it is the one channel for saying what to do instead of retrying the identical call")
	}
}

// A card is arbitrary text an agent wrote - a plan keeps eight lines of
// markdown - and a short terminal is one where that is more rows than the pane
// has. A frame one row too tall wraps and scrolls the alt screen on every draw,
// which is the same failure the notice row's one-row rule exists for.
//
// Mutation check: dropping the MaxHeight in roomPane fails this at 6 and 10
// rows.
func TestATallCardDoesNotMakeTheFrameTallerThanTheTerminal(t *testing.T) {
	plan := recordedAsks(t, planFixture)
	if len(plan) == 0 {
		t.Fatal("the plan fixture holds no asks: this test would assert nothing")
	}
	// From the room's own floor upwards. Below it the room stops shrinking
	// rather than drawing a broken box, which is the trade minDMHeight already
	// makes for the DM - and the card is dropped entirely before that, because a
	// card with no conversation under it is a modal.
	// The floor is the room's own plus the two rows below the panes: the
	// awareness strip and the notice row.
	floor := newRoomApp(t).withSize(80, 24).room.minHeight() + noticeHeight + stripHeight
	for _, height := range []int{floor, floor + 1, floor + 2, 14, 24, 40} {
		a := newRoomApp(t).withSize(80, height)
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &plan[0]})
		if got := lipgloss.Height(a.View()); got != height {
			t.Errorf("a plan card in a %d-row terminal drew %d rows:\n%s", height, got, shown(a))
		}
	}
}

// --- sending -------------------------------------------------------------

// bubbletea runs every tea.Cmd on its own goroutine and rpc's write lock is
// process-wide and held across the socket write, so thirty agents built as
// thirty commands would be thirty goroutines queueing on one lock for one
// keystroke.
func TestABroadcastSendsOneFramePerAgentFromOneGoroutine(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john").withDraft("@all status please")
	_, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	frames := sentFrames(t, a, cmd)
	if len(frames) != 3 {
		t.Fatalf("a broadcast sent %d frames, want 3", len(frames))
	}
	if goroutines := commandCount(cmd); goroutines != 1 {
		t.Errorf("a broadcast used %d commands. bubbletea runs every tea.Cmd on its own goroutine and rpc's write lock is process-wide and held across the socket write, so thirty agents would be thirty goroutines queueing on one lock for one keystroke", goroutines)
	}
	for _, f := range frames {
		if f.Text != "status please" {
			t.Errorf("frame text = %q: the mention is stripped before sending, not echoed at the agent", f.Text)
		}
		if f.Kind != rpc.FrameSend {
			t.Errorf("frame kind = %q, want %q", f.Kind, rpc.FrameSend)
		}
	}
}

func TestTheRoomEchoesYourMessageOnceHoweverManyAgentsItWentTo(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex").withDraft("@all hello")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if n := strings.Count(shown(a), "hello"); n != 1 {
		t.Errorf("your own message appears %d times, want 1. Nothing passes --replay-user-messages, so the local echo is the single source - and one broadcast is one thing you said, not N:\n%s", n, shown(a))
	}
}

// §7 makes the cost visible *before* it fires, because a broadcast is N full
// turns and the number is the whole difference between @all at three agents
// and @all at thirty.
func TestTheComposerSaysWhereEnterWillSendAndWhatItCosts(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")

	if got := a.room.Composer().Target(); !strings.Contains(got, noTarget) {
		t.Errorf("an unaddressed draft reads %q, and says nothing about how to address it", got)
	}
	// `→ @john · direct` rather than `→ @john`: the mention mode is what
	// decides whether this costs one turn or thirty, so the line that exists
	// to say what ↵ costs has to name it. Still an exact match, because the
	// whole point of the line is that it is not approximately right - see
	// TestTheComposerNamesTheMentionModeUnderBothReadings for the other half.
	if got := a.withDraft("@john ship it").room.Composer().Target(); got != "→ @john · direct" {
		t.Errorf("a resolved mention reads %q, want → @john · direct", got)
	}
	if got := a.withDraft("@all ship it").room.Composer().Target(); !strings.Contains(got, "3 turns") {
		t.Errorf("a broadcast to three agents reads %q and does not say it is three turns", got)
	}
	// A mention that resolved to nothing is a path, or an agent that has ended.
	// It must not read as an address, because that is the misroute
	// core.Route.Resolved exists to make visible.
	if got := a.withDraft("@nobody ship it").room.Composer().Target(); strings.Contains(got, "nobody ship") {
		t.Errorf("an unresolved mention reads as an address: %q", got)
	}
}

// The room does not guess. With thirty agents listening, inventing a recipient
// is the misroute Route.Resolved exists to prevent - and a message that went to
// the wrong agent is one they have already started acting on.
func TestAnUnaddressedRoomMessageIsRefusedRatherThanSentSomewhere(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex").withDraft("who is free")
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Fatalf("an unaddressed message was sent anyway: %+v", sentFrames(t, a, cmd))
	}
	// Asserted on the sink, not on the frame. "@all" is already on screen
	// before ↵ is ever pressed - App.retarget puts `→ @name or @all` on the
	// composer for exactly this draft - so a Contains over the view is
	// satisfied by the composer's own advice and stays green with the refusal
	// deleted, leaving the room to swallow the keystroke in silence. That is
	// the outcome this test's own name is about.
	if n := notice.Count(NoAddressee); n != 1 {
		t.Errorf("the refusal was reported %d times, want 1: the room swallowed the keystroke without saying how to address it", n)
	}
	if a.room.Composer().Value() != "who is free" {
		t.Errorf("the draft was destroyed as well as undelivered: %q", a.room.Composer().Value())
	}
}

// --- helpers -------------------------------------------------------------

// newRoomApp is the room with nothing open beside it and a connection that
// records rather than blocks.
func newRoomApp(t *testing.T) App {
	t.Helper()
	fresh(t)
	return NewRoomApp(newRecorder(t), Stream{}, nil)
}

// dmApp is the model `wake` builds: the room, with one agent's conversation
// open beside it. It is what NewApp was before the room existed, so every test
// that predates the room is asserting about this shape.
func dmApp(conn net.Conn, stream Stream, sessionID, name string) App {
	return NewRoomApp(conn, stream, nil).WithOpenDM(sessionID, name)
}

// withSize tells the model how big the terminal is, the way Bubble Tea does.
func (a App) withSize(w, h int) App {
	m, _ := a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(App)
}

// withDraft types into the focused composer one rune at a time, through Update,
// because the path a character takes to the draft is part of what is under
// test: a card key is read before the composer and only when it is empty.
func (a App) withDraft(text string) App {
	var m tea.Model = a
	for _, r := range text {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m.(App).scanned()
}

// scanned answers the directory read the menu is waiting for, the way Bubble
// Tea runs the command Update handed back. The `@` half of a menu is off the
// draw goroutine - see completionpath.go - so a test that reads its offers has
// to let the read land.
//
// Twice at most: a fold either fills the directory the menu wants or asks for
// the one the draft moved to while the first read was out.
func (a App) scanned() App {
	for range 2 {
		out := a.completion.paths.out
		if out == "" {
			break
		}
		m, _ := a.Update(scanPaths(out)())
		a = m.(App)
	}
	return a
}

// withAgents puts named agents in the fleet, the way the daemon does: a status
// push carrying the whole roster.
func (a App) withAgents(names ...string) App {
	st := rpc.Status{Running: true}
	for i, name := range names {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID:    fmt.Sprintf("s%d", i+1),
			Name:  name,
			State: rpc.StateIdle,
		})
	}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
}

// applyFrame folds one frame the way Update does.
func (a App) applyFrame(f rpc.Frame) App { return a.apply(f) }

// pressKey delivers one key the way Bubble Tea does - through Update, so a key
// the App does not take still reaches the composer.
func pressKey(a App, k tea.KeyMsg) (App, tea.Cmd) {
	m, cmd := a.Update(k)
	return m.(App), cmd
}

// questionCount is how many questions the top card is putting.
func questionCount(a App) int {
	top, ok := a.cards.Top()
	if !ok || top.Detail == nil {
		return 0
	}
	return len(top.Detail.Questions)
}

// questionAsk is a recorded AskChoice, so the options a digit picks are the
// ones a real tool offered rather than ones invented here.
func questionAsk(t *testing.T) *core.Event {
	t.Helper()
	asks := recordedAsks(t, choiceFixture)
	if len(asks) == 0 {
		t.Fatal("the choice fixture holds no asks: this test would assert nothing")
	}
	ev := asks[0]
	return &ev
}

// --- what reached the daemon ---------------------------------------------

// sentFrame runs a command and returns the one frame it wrote.
func sentFrame(t *testing.T, a App, cmd tea.Cmd) rpc.Frame {
	t.Helper()
	frames := sentFrames(t, a, cmd)
	if len(frames) != 1 {
		t.Fatalf("%d frames reached the daemon, want exactly 1: %+v", len(frames), frames)
	}
	return frames[0]
}

// sentFrames runs a command and returns every frame it wrote.
//
// It takes the App as well as the command because the frames are read back off
// the connection the App is holding. The alternative - a package-level recorder
// - would be one shared buffer across a parallel package.
func sentFrames(t *testing.T, a App, cmd tea.Cmd) []rpc.Frame {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command: nothing was sent")
	}
	if msg, ok := cmd().(errMsg); ok {
		t.Fatalf("the write failed: %v", msg.Err)
	}
	return recorderOf(t, a).taken(t)
}

// commandCount is how many goroutines Bubble Tea would spend on one command:
// one, unless it is a batch, in which case one per member.
//
// It runs the command a second time, which is why the connection under it has
// to be one that never blocks and never fills - the frames of that second run
// go nowhere anybody is asserting on.
func commandCount(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		return len(batch)
	}
	return 1
}

func recorderOf(t *testing.T, a App) *recorder {
	t.Helper()
	rec, ok := a.conn.(*recorder)
	if !ok {
		t.Fatalf("this App's connection is a %T rather than a recorder", a.conn)
	}
	return rec
}

// recorder is a connection with nobody at the far end.
//
// A net.Pipe would need a reader goroutine and would park a second run of the
// same command; this keeps what was written and can be read at leisure. It
// answers every deadline call, because rpc.WriteFrameTo sets one - that bound
// is the whole reason clients write through it.
type recorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	return &recorder{}
}

// taken decodes everything written since the last call and forgets it.
func (r *recorder) taken(t *testing.T) []rpc.Frame {
	t.Helper()
	r.mu.Lock()
	raw := r.buf.String()
	r.buf.Reset()
	r.mu.Unlock()

	var out []rpc.Frame
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		if line == "" {
			continue
		}
		var f rpc.Frame
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("decode a frame the App wrote (%q): %v", line, err)
		}
		out = append(out, f)
	}
	return out
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recorder) Read([]byte) (int, error)         { return 0, io.EOF }
func (r *recorder) Close() error                     { return nil }
func (r *recorder) LocalAddr() net.Addr              { return recorderAddr{} }
func (r *recorder) RemoteAddr() net.Addr             { return recorderAddr{} }
func (r *recorder) SetDeadline(time.Time) error      { return nil }
func (r *recorder) SetReadDeadline(time.Time) error  { return nil }
func (r *recorder) SetWriteDeadline(time.Time) error { return nil }

type recorderAddr struct{}

func (recorderAddr) Network() string { return "recorder" }
func (recorderAddr) String() string  { return "recorder" }

// Every conversation in the grid has a transcript behind it.
//
// This used to be a reconciliation: ShowDM was a public bool on the pure Layout,
// so "there is a second pane" and "there is a conversation in it" were two facts
// in two places, and setting the first without the second was a nil dereference
// inside the draw loop - a zero DM has a zero text area inside it and View
// dereferences one. The grid holds session ids, so the two facts are one, and
// what is worth holding now is that App.show is the only way into it.
//
// Asserted over a sequence of every placement rather than at one state, because
// the hole a future ⌃⇧-key would open is a grid write that skips show - and that
// is exactly what this catches.
func TestEveryConversationInTheGridHasATranscript(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john", "marcus")

	for _, step := range []struct {
		what string
		do   func(App) App
	}{
		{"⌃D", func(a App) App { return a.openDMWith("s1", "sydney") }},
		{"⌃Y", func(a App) App { return a.openRight("s2", "john") }},
		{"⌃B", func(a App) App { return a.openBelow("s3", "marcus") }},
		{"⌃W", func(a App) App { return a.closeDM() }},
		{"⇥", func(a App) App { return a.nextChat() }},
	} {
		a = step.do(a)
		for _, id := range a.grid.Panes() {
			if id == "" {
				continue
			}
			if _, ok := a.dms[id]; !ok {
				t.Fatalf("after %s the grid holds %q with no transcript behind it: View dereferences a zero DM's text area", step.what, id)
			}
		}
		frame := a.View()
		if got := lipgloss.Height(frame); got != 40 {
			t.Errorf("after %s the frame is %d rows, want 40", step.what, got)
		}
		if got := widest(frame); got != 200 {
			t.Errorf("after %s the frame is %d columns, want the 200 the terminal reported", step.what, got)
		}
	}
}

// roomWithBacklog is a room holding more conversation than a pane can show, so
// there is something to scroll back through.
func roomWithBacklog(t *testing.T, lines int) App {
	t.Helper()
	a := newRoomApp(t).withSize(200, 40).withAgents("john")
	for i := range lines {
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
			Kind: core.KindAssistantText, Text: fmt.Sprintf("message number %d", i),
		}})
	}
	return a
}

// Scrolling the room has to keep working while a card is pinned - which is
// exactly when it is needed, because a card is up when an agent is stopped
// waiting on the operator, and working out what it is asking about is what
// reading back is for.
//
// The failure this guards is silent and is not in the model: App.roomPane draws
// the room at height minus the card, which is not the height resizePanes gave
// it, so Room.View re-enters SetSize - and a SetSize that returns to the bottom
// on a height change throws the offset away on every drawn frame. PgUp moves
// r.tr.scroll and the frame does not move at all.
//
// The card-less half is the control: without it, an assertion that the frame
// moved could pass on a build where PgUp does nothing anywhere.
func TestTheRoomScrollsWhileACardIsPinned(t *testing.T) {
	for _, tc := range []struct {
		what string
		card bool
	}{
		{"with no card", false},
		{"with a card pinned", true},
	} {
		a := roomWithBacklog(t, 200)
		if tc.card {
			a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
				Kind: core.KindPermissionRequest, RequestID: "r1",
				Tool: &core.ToolCall{Name: "Bash", Display: "rm -rf build/"},
			}})
			if a.cards.Len() != 1 {
				t.Fatalf("%s: no card is up, so this case is the other one", tc.what)
			}
		}

		bottom := shown(a)
		up, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyPgUp})
		if shown(up) == bottom {
			t.Errorf("%s: ⇞ moved nothing on screen. The reader's offset is thrown away on every drawn frame, so the key looks broken:\n%s", tc.what, shown(up))
			continue
		}
		back, _ := pressKey(up, tea.KeyMsg{Type: tea.KeyPgDown})
		if shown(back) != bottom {
			t.Errorf("%s: ⇟ did not return the reader to where they started", tc.what)
		}
	}
}

// A broadcast with nobody live is a different refusal from a draft addressed to
// nobody, and it used to be the same one - so somebody who typed @all was
// advised to address it with @all.
func TestABroadcastWithNobodyListeningSaysThatRatherThanAskingForAnAddress(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withDraft("@all anyone there")
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Fatalf("a broadcast to nobody was sent anyway: %+v", sentFrames(t, a, cmd))
	}
	if n := notice.Count(noneListening); n != 1 {
		t.Errorf("a broadcast with nobody live was reported %d times as nobody listening", n)
	}
	if n := notice.Count(NoAddressee); n != 0 {
		t.Errorf("it was reported %d times as needing an address, about a draft that already carries one", n)
	}
}

// A broadcast that fails part-way has already been echoed as sent, and the echo
// is the single source of what you said. At thirty targets a deadline expiring
// on the fifth leaves twenty-five agents unmessaged, so the report has to say
// how far it got rather than only that it failed.
func TestABroadcastThatFailsPartWayThroughSaysHowMuchLanded(t *testing.T) {
	fresh(t)
	mine, theirs := net.Pipe()
	_ = theirs.Close()
	_ = mine.Close()

	a := NewRoomApp(mine, Stream{}, nil).withSize(200, 40).
		withAgents("sydney", "alex", "john").withDraft("@all status please")
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the broadcast produced no command")
	}
	m, _ := a.Update(cmd())

	if got := shown(m); !strings.Contains(got, "0 of 3 sent") {
		t.Errorf("a broadcast that reached nobody reports:\n%s\nwant it to say how many of the three landed", got)
	}
}

// ⎋ from the room stops whoever the roster cursor is on - and closeDM does not
// clear that cursor, so it can be an agent selected some time ago. A key that
// stops work somewhere the eye is not resting has to say where.
func TestInterruptingFromTheRoomSaysWhoseTurnItStopped(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlX}) // selects and opens marco
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW})

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if f := sentFrame(t, a, cmd); f.Kind != rpc.FrameInterrupt || f.SessionID != "s3" {
		t.Fatalf("⎋ from the room sent %+v, want an interrupt for the selected agent", f)
	}
	if n := notice.Count(fmt.Sprintf(interruptedFormat, agentPrefix, "marco")); n != 1 {
		t.Errorf("⎋ stopped marco's turn and said so %d times", n)
	}
}

// And from a DM it stays silent, which is the older rule: the pane names its
// agent in its own header, so there is nothing a notice would add.
func TestInterruptingFromADMStaysSilent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlX})

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if f := sentFrame(t, a, cmd); f.SessionID != "s3" {
		t.Fatalf("⎋ in a DM sent %+v, want an interrupt for the agent it is with", f)
	}
	if n, said := notice.Latest(); said {
		t.Errorf("⎋ in a DM reported %q", n.Text)
	}
}

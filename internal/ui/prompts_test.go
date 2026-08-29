package ui

// Walking back through what you typed, on ⌥↑↓ - because bare ↑↓ move the roster
// cursor or the query cursor here (keys.go), so the history needs the modifier.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// alt is one arrow with the modifier a prompt walk is on.
func alt(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k, Alt: true} }

// spokenApp is a conversation this client has open, holding turns the operator
// typed into it - the shape App.observe leaves behind.
func spokenApp(t *testing.T, texts ...string) App {
	t.Helper()
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	for _, text := range texts {
		a = a.applyFrame(kindFrame("s1", core.KindUserText, text))
	}
	return a
}

// ⌥↑ brings back the last thing you typed, and keeps going back.
func TestAltUpWalksBackThroughWhatWasTyped(t *testing.T) {
	a := spokenApp(t, "the first thing", "the second thing")

	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "the second thing" {
		t.Fatalf("⌥↑ put %q in the draft, want the newest prompt back", got)
	}
	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "the first thing" {
		t.Fatalf("a second ⌥↑ put %q in the draft, want the one before it", got)
	}
	// And the oldest is the end of the walk rather than an empty box: a key that
	// silently clears a recalled prompt is a key that loses it.
	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "the first thing" {
		t.Errorf("⌥↑ past the oldest prompt left %q, want the oldest to hold", got)
	}
	a, _ = pressKey(a, alt(tea.KeyDown))
	if got := a.composer().Value(); got != "the second thing" {
		t.Errorf("⌥↓ put %q in the draft, want the walk to come back the way it went", got)
	}
}

// The draft the walk started from comes back at the end of it.
//
// Without this an accidental ⌥↑ destroys a half-written message, which is the
// same loss the card's arm exists to prevent one surface over.
func TestTheWalkGivesBackTheDraftItStartedFrom(t *testing.T) {
	a := spokenApp(t, "something said earlier").withDraft("half written and not sent")

	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "something said earlier" {
		t.Fatalf("⌥↑ over a draft put %q in the box", got)
	}
	a, _ = pressKey(a, alt(tea.KeyDown))
	if got := a.composer().Value(); got != "half written and not sent" {
		t.Errorf("⌥↓ left %q in the box, want the draft the walk started from", got)
	}
}

// The bare arrows are still the roster's, which is the whole constraint this
// key was chosen under: nothing of Wake's moves.
func TestABareArrowStillMovesTheRosterAndLeavesTheDraft(t *testing.T) {
	a := spokenApp(t, "something said earlier").withAgents("alex", "john")
	a.layout.ShowRoster = true

	before := a.roster.Selected
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})

	if a.roster.Selected == before {
		t.Errorf("↓ left the roster cursor on %q: the roster keys are Wake's and do not move for this", before)
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("↓ put %q in the draft, so the bare arrow walked the prompt history", got)
	}
}

// Claude's abort marker is a user frame and is not a prompt.
//
// It arrives as KindUserText carrying Claude's own English about Wake's own
// interrupt - "[Request interrupted by user]" - resolved in the airlock to a
// notice. Recalling it would put a sentence nobody typed into the box.
func TestTheAbortMarkerIsNotAPrompt(t *testing.T) {
	a := spokenApp(t, "a real prompt")
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind:      core.KindUserText,
		SessionID: "s1",
		Notice:    core.NoticeTurnInterrupted,
		Text:      "[Request interrupted by user]",
	}})

	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "a real prompt" {
		t.Errorf("⌥↑ recalled %q, want the newest thing the operator actually typed", got)
	}
}

// A conversation filled from claude's own transcript has a history, which is
// what makes this work on a reattach and on one this client has never opened.
func TestAConversationFilledFromTheTranscriptHasAHistory(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.withDM("s1", a.dms["s1"].Before([]core.Event{
		{Kind: core.KindUserText, SessionID: "s1", Text: "what did we decide about the cap"},
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "thirty"},
	}))

	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "what did we decide about the cap" {
		t.Errorf("⌥↑ over a conversation read back off disk put %q in the draft. Wake owns no prompt "+
			"history of its own, so the transcript is the only thing a fresh window can walk", got)
	}
}

// The room's history is what was typed into the room, which is the one record
// of it: the room is not a conversation and has no transcript on disk.
func TestTheRoomWalksWhatWasTypedIntoTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")
	a = a.withDraft("@alex look at the tests")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if got := a.composer().Value(); got != "" {
		t.Fatalf("the draft is still %q, so nothing was sent and there is no echo to walk", got)
	}
	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "@alex look at the tests" {
		t.Errorf("⌥↑ in the room put %q in the draft, want the message back with its mention - "+
			"which is what was typed, and what a resend has to address", got)
	}
}

// A pane with nothing behind it says so rather than doing nothing.
func TestAWalkWithNoHistorySaysSo(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")

	a, _ = pressKey(a, alt(tea.KeyUp))

	n, said := notice.Latest()
	if !said || !strings.Contains(n.String(), "⌥↑") {
		t.Errorf("⌥↑ with nothing typed said %q; it has to name itself, the way every other key that "+
			"declines does", n)
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("⌥↑ with no history put %q in the draft", got)
	}
}

// A question card keeps the bare arrows and hands back the ⌥ ones.
//
// The card advertises `↑↓ move` on itself. Reading ⌥↑ as that would make the
// legend's prompt-history entry silently mean something else for as long as any
// agent in the focused pane is asking - which is most of the time on a fleet.
func TestAQuestionCardKeepsItsBareArrowsAndHandsBackTheAltOnes(t *testing.T) {
	a := paneAsking(t)
	a = a.applyFrame(kindFrame("s1", core.KindUserText, "something typed earlier"))

	a, _ = pressKey(a, alt(tea.KeyDown))
	if got := topCard(t, a).Option; got != 0 {
		t.Errorf("⌥↓ moved the card's option cursor to %d: the card's key is the bare arrow", got)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := topCard(t, a).Option; got != 1 {
		t.Errorf("↓ left the card's cursor on %d, want the card to keep its own key", got)
	}
}

// The filter, at the level it is derived rather than through the keys.
func TestWhatCountsAsAPrompt(t *testing.T) {
	got := promptsIn([]core.Event{
		{Kind: core.KindUserText, Text: "typed"},
		{Kind: core.KindAssistantText, Text: "answered"},
		{Kind: core.KindUserText, Notice: core.NoticeTurnInterrupted, Text: "[Request interrupted by user]"},
		{Kind: core.KindUserText, Subagent: &core.Subagent{Agent: "explorer"}, Text: "a prompt an agent wrote"},
		{Kind: core.KindUserText, Echoed: true, Text: "output the tooling generated"},
		{Kind: core.KindUserText, Text: "   "},
		{Kind: core.KindUserText, Text: "typed again"},
	})
	if want := []string{"typed", "typed again"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the prompts are %q, want %q: a notice is Claude's English about Wake's own action, "+
			"a subagent prompt is an agent's, and an echo is generated - none of which anybody typed here",
			got, want)
	}
}

// compactionFixture is the one recording that carries echoed user frames: a
// compaction summary and a <local-command-stdout> line, both generated. See
// core.Event.Echoed, which counts them over the whole corpus.
const compactionFixture = "compaction.jsonl"

// recordedUserText is every KindUserText event in one recording, echoed or not.
func recordedUserText(t *testing.T, fixture string) []core.Event {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "stream", fixture))
	if err != nil {
		t.Fatalf("reading %s: %v", fixture, err)
	}
	var out []core.Event
	for _, line := range splitLines(string(blob)) {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		for _, ev := range evs {
			if ev.Kind == core.KindUserText {
				out = append(out, ev)
			}
		}
	}
	return out
}

// **Generated text is not a prompt**, however much it looks like one on the
// wire: a bang line's output, an /mcp panel and a compaction summary are all
// echoed KindUserText with no Notice and no Subagent, so the two conditions
// that were there let every one of them into the walk - where ⌥↑ puts it in the
// draft and ↵ sends it back to the model as though somebody had typed it.
func TestGeneratedTextIsNotAPrompt(t *testing.T) {
	summaries := 0
	for _, ev := range recordedUserText(t, compactionFixture) {
		if ev.Echoed {
			summaries++
		}
	}
	if summaries == 0 {
		t.Fatalf("%s carries no echoed user frame, so the compaction case below asserts nothing - "+
			"either the fixture changed or the airlock stopped folding isSynthetic onto Echoed", compactionFixture)
	}

	for _, tc := range []struct {
		what string
		ev   core.Event
	}{
		{"a bang line's output", bangEvent(bangResultMsg{ID: "s1", Cmd: "ls", Text: "one\ntwo"})},
		{"an /mcp panel", mcpEvent("s1", []mcpRow{{Name: "wake", Status: "connected"}}, "/tmp", 80)},
		{"a compaction summary, from the recording", recordedEcho(t, compactionFixture)},
	} {
		if got := promptsIn([]core.Event{tc.ev}); len(got) != 0 {
			t.Errorf("%s is in the prompt history as %q. ⌥↑ puts it in the draft and ↵ sends it, so "+
				"generated text goes back to the model as something the operator said", tc.what, got)
		}
	}
}

// recordedEcho is the first echoed user frame in a recording.
func recordedEcho(t *testing.T, fixture string) core.Event {
	t.Helper()
	for _, ev := range recordedUserText(t, fixture) {
		if ev.Echoed {
			return ev
		}
	}
	t.Fatalf("%s carries no echoed user frame", fixture)
	return core.Event{}
}

// And the room does not draw one either, because it is the same predicate: a
// compaction summary in the group chat is generated text under the operator's
// own name.
func TestTheRoomDoesNotDrawGeneratedUserText(t *testing.T) {
	_, out := fold(Agent{}, recordedEcho(t, compactionFixture), "s1")
	if len(out) != 0 {
		t.Errorf("the room draws a compaction summary as %d event(s). fold and the prompt walk ask the "+
			"same question of the same frames, so they cannot come to different answers", len(out))
	}
}

// Through the keys, because the predicate being right is not the same as the
// walk skipping it: an echoed frame reaches the transcript the same way a typed
// one does, and it is the walk that has to pass over it.
func TestAltUpSkipsABangLinesOutput(t *testing.T) {
	a := spokenApp(t, "the thing I typed")
	a = a.withDM("s1", a.dms["s1"].Append(bangEvent(bangResultMsg{ID: "s1", Cmd: "ls", Text: "one\ntwo"})))

	a, _ = pressKey(a, alt(tea.KeyUp))
	if got := a.composer().Value(); got != "the thing I typed" {
		t.Errorf("⌥↑ recalled %q, want the newest thing the operator actually typed", got)
	}
}

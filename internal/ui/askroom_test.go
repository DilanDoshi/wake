package ui

// The room says an agent is blocked, and the conversation says it once.
//
// Two halves of one report. The room was silent while an agent waited - the
// fold routed every KindPermissionRequest into Cards and never into
// Room.Append, and Cards.Undrawn excludes an agent whose conversation is on
// screen, so with a DM open the group chat had no card *and* no line. And the
// conversation drew the same ask three times: the tool call, the ask's label,
// and the ask's tool call again, with the card above all of it.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// The room announces; it does not offer. The card is still the one surface
// that answers - see Cards.Undrawn - so this line carries no keys and is a
// record of a thing that happened, which is what a transcript is.
func TestTheRoomSaysWhenAnAgentIsBlocked(t *testing.T) {
	a := paneAsking(t)
	out := ansi.Strip(a.room.View(roomWidth, 40))
	if !strings.Contains(out, cardHasQuestion) {
		t.Errorf("the group chat says nothing about an agent that has stopped and is waiting on somebody:\n%s", out)
	}
}

// And it says it even when the ask's own conversation is drawing the card,
// which is the state that was reported: the card went to the DM and the room
// went quiet.
func TestTheRoomStillSaysItWhenAConversationDrawsTheCard(t *testing.T) {
	a, _ := asking(t, narrowColumns)
	if _, ok := a.cards.For("s1"); !ok {
		t.Fatal("the conversation is putting no ask, so this test cannot see the room's half")
	}
	out := ansi.Strip(a.room.View(roomWidth, 40))
	if !strings.Contains(out, cardHasQuestion) {
		t.Errorf("the conversation has the card and the group chat says nothing at all, so a fleet's supervisor cannot see that this agent stopped:\n%s", out)
	}
}

// A permission ask keeps its command in the room, because "wants Bash" is not
// what is being asked about.
func TestTheRoomNamesWhatAPermissionAskWants(t *testing.T) {
	ev := core.Event{
		Kind: core.KindPermissionRequest, RequestID: "r1",
		Tool: &core.ToolCall{Name: "Bash", Display: "rm -rf build/"},
	}
	got := ansi.Strip(roomBlock(ev, Agent{Name: "sydney"}, roomWidth, false).text)
	if !strings.Contains(got, "Bash") {
		t.Errorf("a permission ask in the room does not name the tool: %q", got)
	}
}

// --- the conversation draws it once ---------------------------------------

// An interactive ask is presented by its card - the questions, the options,
// each one's consequence. Repeating its tool call underneath is a third
// account of one thing, and the one that says least.
func TestAnInteractiveAskDrawsNoToolCallOfItsOwn(t *testing.T) {
	ev := recordedAsks(t, choiceFixture)[0]
	if ev.Tool == nil || ev.Ask != core.AskChoice {
		t.Fatalf("the recorded ask is %q/%v, not the interactive one this test is about", ev.Ask, ev.Tool != nil)
	}
	got := ansi.Strip(permissionBlock(ev, roomWidth))
	if strings.Contains(got, ev.Tool.Name) {
		t.Errorf("the conversation draws %q's tool call beneath the card that already presents it:\n%s", ev.Tool.Name, got)
	}
	if !strings.Contains(got, ansi.Strip(warnLine(permissionLabel, roomWidth))) {
		t.Errorf("the ask left no mark in the transcript at all:\n%s", got)
	}
}

// The other half, and the reason this is a rule about the *ask kind* rather
// than a blanket one: what a permission ask puts up for approval is the
// command, so heading it with the tool alone would put a description in front
// of an operator being asked to approve `rm -rf`.
func TestAPermissionAskStillDrawsWhatWouldRun(t *testing.T) {
	ev := core.Event{
		Kind: core.KindPermissionRequest, RequestID: "r1", Ask: core.AskPermission,
		Tool: &core.ToolCall{Name: "Bash", Display: "rm -rf build/", Command: "rm -rf build/"},
	}
	got := ansi.Strip(permissionBlock(ev, roomWidth))
	for _, want := range []string{"Bash", "rm -rf build/"} {
		if !strings.Contains(got, want) {
			t.Errorf("a permission ask does not show %q, which is what is being approved:\n%s", want, got)
		}
	}
}

// --- the frame ------------------------------------------------------------

// An ask is a prompt, and it has to read as one. Unframed, its rows sat in the
// same column as the transcript above them and read as one more block of
// conversation - which is what "rendering funny" turned out to mean.
func TestAnAskIsDrawnAsAFramedPrompt(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	out := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	rows := strings.Split(out, "\n")

	b := lipgloss.RoundedBorder()
	if !strings.HasPrefix(rows[0], b.TopLeft) || !strings.HasSuffix(rows[0], b.TopRight) {
		t.Errorf("the card's first row is not the top of a box: %q", rows[0])
	}
	last := rows[len(rows)-1]
	if !strings.HasPrefix(last, b.BottomLeft) || !strings.HasSuffix(last, b.BottomRight) {
		t.Errorf("the card's last row is not the bottom of a box: %q", last)
	}
	// The headline rides the top edge and the keys the bottom, which is what
	// keeps a framed card exactly as tall as the unframed one was.
	if !strings.Contains(rows[0], cardHasQuestion) {
		t.Errorf("the top edge does not say what the agent wants: %q", rows[0])
	}
	if !strings.Contains(last, cardDenyLabel) {
		t.Errorf("the bottom edge does not carry the card's keys: %q", last)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != ansi.StringWidth(rows[0]) {
			t.Errorf("row %d is %d columns against the box's %d, so the frame is ragged: %q", i, got, ansi.StringWidth(rows[0]), r)
		}
	}
}

// Bounded, for roomBlock's reason: the room is one column of a three-region
// layout and lipgloss sizes a joined row on its widest line, so one over-wide
// row shoves both sidebars out of place.
func TestAFramedAskFitsTheWidthItIsGiven(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	for _, w := range []int{minBlockWidth, 40, 52, 92, 160} {
		out := ansi.Strip(oneCard(card).topView(w, Agent{Name: "sydney"}))
		for _, r := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(r); got > max(w, minBlockWidth) {
				t.Errorf("at width %d a card row is %d columns: %q", w, got, r)
			}
		}
	}
}

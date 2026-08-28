package ui

// Expanding a tool result: what the key acts on, what it refuses, and the two
// properties a toggle over a memoized transcript has to keep.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// longResult is a tool result with more lines than the collapse keeps, each
// numbered so an assertion can name the one that is out of sight rather than
// counting rows.
func longResult(n int) core.Event {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	return core.Event{Kind: core.KindToolResult, Text: strings.Join(lines, "\n")}
}

// The gap this closes: ten lines and an ellipsis is not a fold, it is a loss.
// render.ToolResult has taken a `collapsed` argument since it was written and
// nothing has ever passed false, so the operator could not see what the agent
// saw.
func TestAToolResultIsCollapsedUntilItIsExpanded(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).Append(longResult(30))

	assertShows(t, d, 60, 20, "lines (⌃E to expand)")
	assertHides(t, d, 60, 20, "line-30")

	d = d.toggleExpanded()

	assertShows(t, d, 60, 20, "line-30")
	assertHides(t, d, 60, 20, "lines (⌃E to expand)")
}

// And back, because a toggle that only opens is a one-way door on the pane an
// operator reads most.
func TestExpandingTogglesBothWays(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).Append(longResult(30))

	d = d.toggleExpanded().toggleExpanded()

	assertShows(t, d, 60, 20, "lines (⌃E to expand)")
	assertHides(t, d, 60, 20, "line-30")
}

// The transcript is memoized lines rather than events, so a toggle that
// forgot to re-render would change the flag and nothing else - which is the
// failure that looks exactly like a dead key.
func TestExpandingIsImmutable(t *testing.T) {
	base := NewDM("s1", "alex").SetSize(60, 20).Append(longResult(30))

	next := base.toggleExpanded()

	assertHides(t, base, 60, 20, "line-30")
	assertShows(t, next, 60, 20, "line-30")
}

// Expanding grows the transcript, which renumbers every line a scroll offset
// points at - the same thing a width change does, and it gets the same answer
// for the same reason. See DM.SetSize.
func TestExpandingReturnsTheReaderToTheNewestLine(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	for range 5 {
		d = d.Append(longResult(30))
	}
	d = d.ScrollUp(40)
	if d.tr.atBottom() {
		t.Fatal("the reader is still at the bottom, so this test proves nothing")
	}

	d = d.toggleExpanded()

	if !d.tr.atBottom() {
		t.Error("expanding left the reader at a stale offset, pointing at lines that have renumbered")
	}
}

// ⌃E acts on the pane with the keys, not on the roster's pick: expanding is
// about what is being read, and the roster cursor names an agent that may have
// no pane on screen at all.
func TestCtrlEExpandsTheConversationWithTheKeys(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a.roster.Selected = "s1"
	// The conversation is opened first: App.observe folds an event only into a
	// DM it already holds, so a result that arrived before the pane did would
	// be read back off claude's transcript rather than appended here.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if a.focus != "s1" {
		t.Fatalf("focus is %q, want the conversation this test is about", a.focus)
	}
	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameEvent, SessionID: "s1",
		Event: &core.Event{Kind: core.KindToolResult, SessionID: "s1", Text: longResult(30).Text},
	})
	if !strings.Contains(shown(a), "lines (⌃E to expand)") {
		t.Fatalf("the result is not collapsed to begin with:\n%s", shown(a))
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})

	if !strings.Contains(shown(a), "line-30") {
		t.Errorf("⌃E did not expand the focused conversation:\n%s", shown(a))
	}
}

// One conversation at a time. Thirty open DMs re-rendered on one keypress is
// thirty transcripts back through glamour, which renders behind one
// process-global mutex - so a fleet-wide toggle stalls every pane on screen.
func TestExpandingOneConversationLeavesTheOthersCollapsed(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a.roster.Selected = "s2"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	a.roster.Selected = "s1"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if a.focus != "s1" {
		t.Fatalf("focus is %q, want sydney's conversation", a.focus)
	}
	if _, ok := a.dms["s2"]; !ok {
		t.Fatal("john's conversation is not open, so this test cannot see it stay collapsed")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})

	if !a.dms["s1"].expanded {
		t.Error("⌃E did not expand the conversation it was pressed in")
	}
	if a.dms["s2"].expanded {
		t.Error("⌃E expanded a conversation that was not on the keys")
	}
}

// The room folds a long reply into a pointer rather than a tool result, so ⌃E
// here expands what the room folded - every collapsed response at once, the
// room learning the DM's ⌃E. It used to refuse by name; that was before the
// room could show a full response without opening the agent's DM.
func TestCtrlEInTheRoomExpandsEveryCollapsedResponse(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	if a.focus != "" {
		t.Fatalf("focus is %q, want the room", a.focus)
	}
	a = a.applyFrame(rpc.Frame{
		Kind: rpc.FrameEvent, SessionID: "s1",
		Event: &core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: longRoomReply("TAIL_OF_THE_REPLY")},
	})
	if !strings.Contains(shown(a), openDMHint) {
		t.Fatalf("the reply is not collapsed to begin with:\n%s", shown(a))
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})

	if !strings.Contains(shown(a), "TAIL_OF_THE_REPLY") {
		t.Errorf("⌃E did not expand the collapsed response in the room:\n%s", shown(a))
	}
	if strings.Contains(shown(a), openDMHint) {
		t.Errorf("⌃E left the pointer up beside the expanded body:\n%s", shown(a))
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})

	if strings.Contains(shown(a), "TAIL_OF_THE_REPLY") {
		t.Errorf("a second ⌃E did not re-collapse the room:\n%s", shown(a))
	}
}

// longRoomReply is an agent reply whose rendered height is well past the room's
// inline cap, so it collapses to a pointer. The tail marker rides at the end,
// out of the collapsed preview's opening lines, so a test can name the row that
// is hidden until the reply is expanded.
func longRoomReply(tail string) string {
	return "OPENING of the reply.\n\n" +
		strings.Repeat("more of the reply's reasoning.\n\n", roomInlineRows+5) + tail
}

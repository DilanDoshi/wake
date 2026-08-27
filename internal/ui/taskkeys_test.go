package ui

// Walking the dispatch list and opening what the cursor is on.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func taskFrame(sessionID string, ev core.Event) rpc.Frame {
	ev.SessionID = sessionID
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &ev}
}

// dispatching is an open conversation with two subagents under it, one of
// which has forwarded a line.
func dispatching(t *testing.T) App {
	t.Helper()
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 40)
	for _, ev := range []core.Event{
		started("a1", "toolu_1", "Count lines in alpha.txt", "general-purpose", core.TaskAgent),
		started("a2", "toolu_2", "Grep the tree", "general-purpose", core.TaskAgent),
		spoke("toolu_1", subSaid),
	} {
		a = a.applyFrame(taskFrame("s1", ev))
	}
	return a
}

func viewedBy(t *testing.T, a App, sessionID string) string {
	t.Helper()
	d, ok := a.dms[sessionID]
	if !ok {
		t.Fatalf("no conversation for %s", sessionID)
	}
	return d.Viewed()
}

func cursorOf(t *testing.T, a App, sessionID string) int {
	t.Helper()
	d, ok := a.dms[sessionID]
	if !ok {
		t.Fatalf("no conversation for %s", sessionID)
	}
	return d.taskCursor
}

// ⌃N and ⌃P walk the list. They are the keys Claude Code binds for the same
// job beside its own arrows, which Wake cannot use here: ↑↓ are the roster's,
// and taking them would move the fleet cursor's meaning depending on which
// pane had the focus.
func TestTheDispatchListIsWalkedWithCtrlNAndCtrlP(t *testing.T) {
	a := dispatching(t)
	if got := cursorOf(t, a, "s1"); got != 0 {
		t.Fatalf("cursor starts at %d, want 0 - nothing is selected until somebody walks the list", got)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := cursorOf(t, a, "s1"); got != 1 {
		t.Errorf("cursor = %d after one ⌃N, want 1 - the conversation's own row, which is the way back", got)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := cursorOf(t, a, "s1"); got != 3 {
		t.Errorf("cursor = %d after three ⌃N, want 3", got)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	if got := cursorOf(t, a, "s1"); got != 2 {
		t.Errorf("cursor = %d after ⌃P, want 2", got)
	}
}

// The list does not wrap. A cursor that jumps from the last dispatch back to
// the conversation is one that opens the wrong thing on a key held down.
func TestTheCursorStopsAtBothEndsOfTheList(t *testing.T) {
	a := dispatching(t)
	for range 8 {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	if got := cursorOf(t, a, "s1"); got != 3 {
		t.Errorf("cursor = %d at the bottom, want 3 - the conversation plus two dispatches", got)
	}
	for range 8 {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	}
	if got := cursorOf(t, a, "s1"); got != 1 {
		t.Errorf("cursor = %d at the top, want 1 - the conversation's row, not off the list", got)
	}
}

// ↵ on a dispatch opens it, which is the whole point of the surface.
func TestEnterOpensTheDispatchTheCursorIsOn(t *testing.T) {
	a := dispatching(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if got := viewedBy(t, a, "s1"); got != "toolu_1" {
		t.Errorf("viewing %q, want toolu_1", got)
	}
	if out := strings.Join(dmLines(t, a, "s1"), "\n"); !strings.Contains(out, subSaid) {
		t.Errorf("the subagent's transcript is not on screen:\n%s", out)
	}
}

// And ↵ on the conversation's own row is the way back, which is what Claude
// Code's `main` row does.
func TestEnterOnTheConversationRowLeavesTheDispatch(t *testing.T) {
	a := dispatching(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlP})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if got := viewedBy(t, a, "s1"); got != "" {
		t.Errorf("viewing %q, want the conversation", got)
	}
}

// ↵ still sends when there is a draft. The cursor is a way to move around a
// pane, and it must never eat a message somebody typed.
func TestADraftStillSendsWhileTheCursorIsOnADispatch(t *testing.T) {
	a := dispatching(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a = a.withDraft("hello")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if got := viewedBy(t, a, "s1"); got != "" {
		t.Errorf("the draft opened a dispatch instead of sending: viewing %q", got)
	}
}

// esc clears the cursor and still interrupts, which is this tree's rule for
// every transient thing on screen: a keystroke that stops a runaway agent must
// never be swallowed by something decorative. Asserted against the frame that
// reaches the daemon, because "it cleared the cursor" is the half that would
// pass on its own.
func TestEscClearsTheCursorAndStillInterrupts(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withSize(120, 40)
	a = a.applyFrame(taskFrame("s1", started("a1", "toolu_1", "Count lines", "general-purpose", core.TaskAgent)))

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})

	if got := cursorOf(t, a, "s1"); got != 0 {
		t.Errorf("cursor = %d after esc, want 0", got)
	}
	go func() { _ = runCmdQuietly(cmd) }()
	if f := awaitFrame(t, sent); f.Kind != rpc.FrameInterrupt {
		t.Errorf("frame kind = %q, want %q - esc cleared the cursor and stopped there", f.Kind, rpc.FrameInterrupt)
	}
}

// A conversation with nothing dispatched hands the keys back, so ⌃N and ⌃P
// reach the composer the way any unclaimed key does rather than being dead.
func TestTheKeysAreHandedBackWhenThereIsNothingToWalk(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 40)

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	if got := cursorOf(t, a, "s1"); got != 0 {
		t.Errorf("cursor = %d in a conversation with no dispatches, want 0", got)
	}
}

// The cursor belongs to the pane it was set in. Two conversations each have
// their own list, and walking one must not move the other's.
func TestEachConversationKeepsItsOwnCursor(t *testing.T) {
	a := dispatching(t)
	a = a.applyFrame(taskFrame("s2", started("b1", "toolu_9", "other work", "general-purpose", core.TaskAgent)))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})

	if got := cursorOf(t, a, "s1"); got != 1 {
		t.Errorf("the focused conversation's cursor = %d, want 1", got)
	}
	if _, ok := a.dms["s2"]; ok {
		if got := cursorOf(t, a, "s2"); got != 0 {
			t.Errorf("an unfocused conversation's cursor moved to %d", got)
		}
	}
}

// The cursor is drawn, or walking the list is invisible.
func TestTheCursorIsVisibleInTheList(t *testing.T) {
	a := dispatching(t)
	before := strings.Join(dmLines(t, a, "s1"), "\n")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlN})
	after := strings.Join(dmLines(t, a, "s1"), "\n")

	if before == after {
		t.Errorf("the pane is identical before and after ⌃N: the cursor is not drawn\n%s", after)
	}
}

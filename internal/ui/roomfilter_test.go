package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// frameText is one agent's assistant line, delivered the way the daemon does.
func frameText(id, text string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: id, Text: text,
	}}
}

// A narrowed room widens on ToggleNarrow and re-narrows on a second toggle,
// keeping the resolved target either way - the override is a view choice, not a
// change of who the composer is addressing.
func TestToggleNarrowWidensAndReNarrows(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john green"}, Agent{ID: john, Name: "john"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris building"}, Agent{ID: iris, Name: "iris"})
	r = r.WithFocus(john, "john", mgr)

	if got := r.effectiveFocus(); got != john {
		t.Fatalf("default-on WithFocus did not narrow: effectiveFocus = %q, want %q", got, john)
	}
	if v := r.View(80, 24); strings.Contains(v, "iris building") {
		t.Fatalf("narrowed room leaked iris:\n%s", v)
	}

	r = r.ToggleNarrow()
	if got := r.effectiveFocus(); got != "" {
		t.Fatalf("ToggleNarrow did not widen: effectiveFocus = %q, want empty", got)
	}
	if r.focus != john {
		t.Fatalf("ToggleNarrow dropped the resolved target: focus = %q, want %q", r.focus, john)
	}
	if v := r.View(80, 24); !strings.Contains(v, "iris building") || !strings.Contains(v, "john green") {
		t.Fatalf("widened room hid a line:\n%s", v)
	}

	r = r.ToggleNarrow()
	if got := r.effectiveFocus(); got != john {
		t.Fatalf("second ToggleNarrow did not re-narrow: effectiveFocus = %q, want %q", got, john)
	}
	if v := r.View(80, 24); strings.Contains(v, "iris building") {
		t.Fatalf("re-narrowed room leaked iris:\n%s", v)
	}
}

// With the default off, resolving a lone @name records the target but does not
// narrow; ⌃A narrows on demand. The key is symmetric under both defaults.
func TestNarrowDefaultOffDoesNotNarrowUntilToggled(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	r := NewRoom().SetSize(80, 24).WithNarrowDefault(false)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john green"}, Agent{ID: john, Name: "john"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris building"}, Agent{ID: iris, Name: "iris"})

	r = r.WithFocus(john, "john", mgr)
	if got := r.effectiveFocus(); got != "" {
		t.Fatalf("default-off WithFocus narrowed anyway: effectiveFocus = %q, want empty", got)
	}
	if r.focus != john {
		t.Fatalf("default-off WithFocus did not record the target: focus = %q, want %q", r.focus, john)
	}
	if v := r.View(80, 24); !strings.Contains(v, "iris building") {
		t.Fatalf("default-off room hid iris:\n%s", v)
	}

	r = r.ToggleNarrow()
	if got := r.effectiveFocus(); got != john {
		t.Fatalf("⌃A did not narrow on demand under default-off: effectiveFocus = %q, want %q", got, john)
	}
	if v := r.View(80, 24); strings.Contains(v, "iris building") {
		t.Fatalf("on-demand narrow leaked iris:\n%s", v)
	}
}

// ⌃A widens a room narrowed by @john and re-narrows it, driven through the App
// the way a keypress arrives.
func TestCtrlAWidensTheNarrowedRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john", "iris")
	john := idOfAgentNamed(t, a, "john")
	iris := idOfAgentNamed(t, a, "iris")
	a = a.applyFrame(frameText(john, "john tests green"))
	a = a.applyFrame(frameText(iris, "iris still building"))

	a = a.withDraft("@john ")
	if a.room.effectiveFocus() != john {
		t.Fatalf("precondition: @john did not narrow (effectiveFocus=%q)", a.room.effectiveFocus())
	}
	if out := shown(a); strings.Contains(out, "iris still building") {
		t.Fatalf("narrowed room leaked iris:\n%s", out)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	if a.room.effectiveFocus() != "" {
		t.Fatalf("⌃A did not widen: effectiveFocus=%q", a.room.effectiveFocus())
	}
	if a.room.focus != john {
		t.Fatalf("⌃A dropped the target: room.focus=%q, want %q", a.room.focus, john)
	}
	out := shown(a)
	if !strings.Contains(out, "iris still building") {
		t.Fatalf("⌃A did not reveal iris:\n%s", out)
	}
	if strings.Contains(out, "› @john") {
		t.Fatalf("⌃A left the narrowing affordance in the header:\n%s", out)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	if a.room.effectiveFocus() != john {
		t.Fatalf("second ⌃A did not re-narrow: effectiveFocus=%q", a.room.effectiveFocus())
	}
	if out := shown(a); strings.Contains(out, "iris still building") {
		t.Fatalf("second ⌃A did not re-hide iris:\n%s", out)
	}
}

// A ⌃A override survives typing more into the same draft, and clearing the
// draft widens the room - driven through the App the way keys arrive.
func TestCtrlAOverrideSurvivesTypingAndClears(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john")

	a = a.withDraft("@john ")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA}) // widen while @john is the target
	if a.room.effectiveFocus() != "" {
		t.Fatalf("precondition: ⌃A did not widen (effectiveFocus=%q)", a.room.effectiveFocus())
	}

	a = a.withDraft("do the thing") // same target, more text appended to the draft
	if a.room.effectiveFocus() != "" {
		t.Fatalf("the override did not survive typing into the same draft: effectiveFocus=%q", a.room.effectiveFocus())
	}

	cleared, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc}) // clearing the draft resets to the default
	if cleared.room.effectiveFocus() != "" {
		t.Fatalf("clearing the draft left the room narrowed: effectiveFocus=%q", cleared.room.effectiveFocus())
	}
}

// A new target resets the override to the default - asserted at the Room level,
// where WithFocus is driven directly rather than through the composer's
// append-only draft.
func TestOverrideResetsOnNewTarget(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	r := NewRoom().SetSize(80, 24).WithFocus(john, "john", mgr) // narrowed (default on)
	r = r.ToggleNarrow()                                        // ⌃A widened; override live
	if r.effectiveFocus() != "" {
		t.Fatalf("precondition: ToggleNarrow did not widen (effectiveFocus=%q)", r.effectiveFocus())
	}
	r = r.WithFocus(john, "john", mgr) // same target: override survives
	if r.effectiveFocus() != "" {
		t.Fatalf("override did not survive the same target: effectiveFocus=%q", r.effectiveFocus())
	}
	r = r.WithFocus(iris, "iris", mgr) // new target: reset to the default (on)
	if r.effectiveFocus() != iris {
		t.Fatalf("a new target did not reset the override: effectiveFocus=%q, want %q", r.effectiveFocus(), iris)
	}
}

// /groupchat-filter off makes a lone @name stop narrowing; ⌃A still narrows on
// demand; /groupchat-filter on restores the default.
func TestGroupchatFilterCommandTogglesTheDefault(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john")
	john := idOfAgentNamed(t, a, "john")

	off, _ := pressKey(a.withDraft(SlashPrefix+groupchatFilterCommand+" off"), tea.KeyMsg{Type: tea.KeyEnter})
	if off.room.narrowDefault {
		t.Fatalf("/groupchat-filter off did not clear the default")
	}
	if notice.Count(roomFilterOff) != 1 {
		t.Fatalf("/groupchat-filter off did not report itself (count=%d)", notice.Count(roomFilterOff))
	}
	off = off.withDraft("@john ")
	if off.room.effectiveFocus() != "" {
		t.Fatalf("with the filter off, @john narrowed anyway: effectiveFocus=%q", off.room.effectiveFocus())
	}
	off, _ = pressKey(off, tea.KeyMsg{Type: tea.KeyCtrlA})
	if off.room.effectiveFocus() != john {
		t.Fatalf("⌃A did not narrow on demand with the filter off: effectiveFocus=%q", off.room.effectiveFocus())
	}

	// Clear the composer before typing the next command - withDraft appends.
	off, _ = pressKey(off, tea.KeyMsg{Type: tea.KeyEsc})
	on, _ := pressKey(off.withDraft(SlashPrefix+groupchatFilterCommand+" on"), tea.KeyMsg{Type: tea.KeyEnter})
	if !on.room.narrowDefault {
		t.Fatalf("/groupchat-filter on did not restore the default")
	}
	on = on.withDraft("@john ")
	if on.room.effectiveFocus() != john {
		t.Fatalf("with the filter on, @john did not narrow: effectiveFocus=%q", on.room.effectiveFocus())
	}
}

// A bare /groupchat-filter reports the current setting; a word that is neither
// on nor off is refused rather than silently doing one of them.
func TestGroupchatFilterBareReportsAndRejectsBadArg(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("john")

	bare, _ := pressKey(a.withDraft(SlashPrefix+groupchatFilterCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if notice.Count(roomFilterOn) != 1 {
		t.Fatalf("bare /groupchat-filter did not report the current (on) setting (count=%d)", notice.Count(roomFilterOn))
	}
	if bare.room.narrowDefault != true {
		t.Fatalf("bare /groupchat-filter changed the setting, want it left on")
	}

	bad, _ := pressKey(a.withDraft(SlashPrefix+groupchatFilterCommand+" maybe"), tea.KeyMsg{Type: tea.KeyEnter})
	if notice.Count(roomFilterUsage) != 1 {
		t.Fatalf("/groupchat-filter maybe was not refused (count=%d)", notice.Count(roomFilterUsage))
	}
	if bad.room.narrowDefault != true {
		t.Fatalf("a bad arg changed the setting, want it untouched")
	}
}

// ⌃A is a room view control: it refuses from a DM pane and refuses in the room
// when nothing has narrowed it, naming what to do instead.
func TestCtrlARefusedWithoutARoomTarget(t *testing.T) {
	// No lone @name resolved: nothing to widen.
	a := newRoomApp(t).withSize(200, 40).withAgents("john")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlA})
	if notice.Count(roomFilterNoTarget) != 1 {
		t.Fatalf("⌃A with no target did not refuse (count=%d)", notice.Count(roomFilterNoTarget))
	}

	// From a DM pane, ⌃A is not a key this surface owns.
	d := dmApp(newRecorder(t), Stream{}, "s1", "john").withSize(200, 40)
	d, _ = pressKey(d, tea.KeyMsg{Type: tea.KeyCtrlA})
	if notice.Count(roomFilterDMOnly) != 1 {
		t.Fatalf("⌃A from a DM did not refuse (count=%d)", notice.Count(roomFilterDMOnly))
	}
}

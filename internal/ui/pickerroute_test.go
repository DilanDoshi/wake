package ui

// Where a bare configure command goes when it is typed in the room.
//
// The DM has nothing to resolve - the pane names its recipient - so every
// question about addressing is here.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A mention aims the picker at the one named.
func TestABareCommandWithAMentionConfiguresTheOneNamed(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex")
	alex := idOfAgentNamed(t, a, "alex")

	a, _ = pressKey(a.withDraft("@alex "+SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if !a.picker.Open() {
		t.Fatal("@alex /effort opened no picker")
	}
	if len(a.picker.Targets) != 1 || a.picker.Targets[0] != alex {
		t.Errorf("the picker is aimed at %v, want just alex (%s)", a.picker.Targets, alex)
	}
}

// Open mention mode widens a message and does not widen a knob.
//
// `@alex hello` in open mode reaches the whole fleet and keeps the name in the
// text, so the others can see who was addressed - a property of something being
// *said*. `@alex /effort` is not that: widening it would retune every session
// in the room off one keystroke.
func TestOpenMentionModeDoesNotWidenAConfigureCommand(t *testing.T) {
	// A fresh App per draft, not two drafts through one. textarea.Model holds
	// its buffer behind pointers, so two Composers off one App share it - the
	// hazard legend.go names - and the second draft came out as
	// "@alex /effort@alex ship it", which is not a bare command and would have
	// made this pass for the wrong reason.
	open := func() App {
		a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "jo")
		a.mention = MentionOpen
		return a
	}
	alex := idOfAgentNamed(t, open(), "alex")

	// The floor: in this mode an ordinary message really does reach everybody,
	// so the assertion below is about the command and not about the fixture.
	wide, cmd := pressKey(open().withDraft("@alex ship it"), tea.KeyMsg{Type: tea.KeyEnter})
	if got := len(sentFrames(t, wide, cmd)); got != 3 {
		t.Fatalf("a message reached %d agents in open mode, want the whole fleet: this test cannot show "+
			"a command staying narrow in a mode that is not widening anything", got)
	}

	got, _ := pressKey(open().withDraft("@alex "+SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if !got.picker.Open() {
		t.Fatal("@alex /effort opened no picker in open mention mode")
	}
	if len(got.picker.Targets) != 1 || got.picker.Targets[0] != alex {
		t.Errorf("open mode aimed the picker at %v, want just alex (%s). Widening a message is a promise "+
			"about who can see it; widening a knob retunes sessions nobody chose",
			got.picker.Targets, alex)
	}
}

// An unaddressed command follows the rule an unaddressed message follows, and
// the header is what makes that safe - the target is on screen before a key is
// pressed rather than discovered after.
func TestAnUnaddressedCommandGoesToTheDefaultAddressee(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	a, _ = pressKey(a.withDraft(SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if !a.picker.Open() {
		t.Fatal("an unaddressed /effort opened no picker")
	}
	if len(a.picker.Targets) != 1 || a.picker.Targets[0] != managerID {
		t.Errorf("the picker is aimed at %v, want the manager %s", a.picker.Targets, managerID)
	}
	if len(a.picker.Names) != 1 || a.picker.Names[0] != core.ManagerName {
		t.Errorf("the header names %v; an unaddressed command is safe only because it says who it is for",
			a.picker.Names)
	}
}

// @all takes the live fleet, and the count on the card is the count it will
// write to.
func TestABroadcastCommandTakesTheWholeLiveFleet(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "jo")

	a, _ = pressKey(a.withDraft("@all "+SlashPrefix+modelCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if !a.picker.Open() {
		t.Fatal("@all /model opened no picker")
	}
	if len(a.picker.Targets) != 3 {
		t.Errorf("the picker is aimed at %d agents, want the three live ones", len(a.picker.Targets))
	}
}

// Confirming a broadcast writes one frame per target, from one command.
//
// /resume all's rule and its reason: bubbletea runs every tea.Cmd on its own
// goroutine and rpc's write lock is process-wide, so thirty targets must be one
// command writing thirty frames.
func TestConfirmingABroadcastWritesOneFramePerTarget(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "jo")

	a, _ = pressKey(a.withDraft("@all "+SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	after, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	frames := sentFrames(t, after, cmd)
	if len(frames) != 3 {
		t.Fatalf("%d frames went out, want one per agent: %+v", len(frames), frames)
	}
	seen := map[string]bool{}
	for _, f := range frames {
		if f.Kind != rpc.FrameSend {
			t.Errorf("a picker wrote a %v, want a send", f.Kind)
		}
		if seen[f.SessionID] {
			t.Errorf("two frames went to %s", f.SessionID)
		}
		seen[f.SessionID] = true
	}
}

// A command with nothing to configure opens no picker.
//
// A picker that cannot be confirmed is the lying surface `wake stop`'s rule
// refuses, and the caller's own refusal has already said why there is no
// target.
func TestACommandWithNoTargetsOpensNoPicker(t *testing.T) {
	// No manager, so an unaddressed draft resolves to nobody. A fresh App per
	// draft: two Composers off one App share a buffer.
	fleet := func() App { return newRoomApp(t).withSize(200, 40).withAgents("sydney") }

	got, _ := pressKey(fleet().withDraft(SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if got.picker.Open() {
		t.Error("a command with no addressee opened a picker that could never be confirmed")
	}

	// And a name that resolves to nothing is the same answer.
	got, _ = pressKey(fleet().withDraft("@nobody "+SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if got.picker.Open() {
		t.Error("@nobody /effort opened a picker")
	}
}

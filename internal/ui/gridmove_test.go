package ui

// Moving the keys between panes by direction: ⇧←→. ⇧↑↓ moved to the roster when
// the plain arrows took Claude Code's prompt-history recall, so vertical pane
// movement has no key - the lower slot of a split column is reached by ⇥ or a
// click.
//
// ⌘+arrow is what was asked for and is not bindable: bubbletea v1.3.10's arrow
// table knows modifier params 2-8 and cmd is bit 8, so ⌘→ is param 9 and the
// library names nothing for it - and no macOS terminal transmits ⌘ to the tty
// in the first place. ⌃+arrow is named and delivered and still wrong: macOS
// spends all four on spaces and Mission Control, which is the ⌃⇧+arrow trap
// TestNoKeyIsACtrlArrow already holds. ⇧+arrow is the one family free at every
// layer - bubbletea names it and the text area under the composer does not bind
// it. keyprobe_test.go holds the first half of that.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// stackedGrid is room | alex | sydney over john, which has both a plain column
// and a split one so a row-preserving move has something to preserve.
func stackedGrid() Grid {
	return NewGrid().OpenRight("", "alex").OpenRight("alex", "sydney").OpenBelow("sydney", "john")
}

func TestTowardWalksTheColumnsAndTheSplit(t *testing.T) {
	g := stackedGrid()
	for _, tc := range []struct {
		what string
		from string
		dir  Direction
		want string
		ok   bool
	}{
		{"right out of the room", "", Right, "alex", true},
		{"right along the top row", "alex", Right, "sydney", true},
		{"left back to the room, which is a real destination and not a refusal", "alex", Left, "", true},
		{"left from the room, where there is nothing", "", Left, "", false},
		{"right from the last column", "sydney", Right, "", false},
		{"a conversation the grid does not hold", "nobody", Right, "", false},
	} {
		got, ok := g.Toward(tc.from, tc.dir)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: Toward(%q) = (%q, %v), want (%q, %v)", tc.what, tc.from, got, ok, tc.want, tc.ok)
		}
	}
}

// A horizontal move keeps the row it was on, so ⇧→ out of a lower pane lands in
// the lower pane beside it rather than jumping to the top of the next column.
// Without this the key reads as two moves at once.
func TestTowardKeepsTheRowItWasOn(t *testing.T) {
	// room/mgr | alex/sydney - both columns split.
	g := NewGrid().OpenBelow("", "mgr").OpenRight("", "alex").OpenBelow("alex", "sydney")

	if got, ok := g.Toward("mgr", Right); got != "sydney" || !ok {
		t.Errorf("Toward(mgr, Right) = (%q, %v), want (sydney, true): a lower pane moves to the lower pane beside it", got, ok)
	}
	if got, ok := g.Toward("sydney", Left); got != "mgr" || !ok {
		t.Errorf("Toward(sydney, Left) = (%q, %v), want (mgr, true)", got, ok)
	}
	// And falls back to the top when the column it arrives in has no lower slot.
	if got, ok := stackedGrid().Toward("john", Left); got != "alex" || !ok {
		t.Errorf("Toward(john, Left) into an unsplit column = (%q, %v), want (alex, true)", got, ok)
	}
}

// The room is "" and so is "nothing that way". A single return value cannot
// tell those apart, which is why Toward reports both.
func TestTowardDistinguishesTheRoomFromNothing(t *testing.T) {
	g := NewGrid().OpenRight("", "alex")
	toRoom, ok := g.Toward("alex", Left)
	if toRoom != "" || !ok {
		t.Fatalf("Toward(alex, Left) = (%q, %v), want the room and true", toRoom, ok)
	}
	if _, ok := g.Toward("", Left); ok {
		t.Error("Toward(room, Left) reported a destination, and there is nothing left of the room")
	}
}

func TestShiftArrowsMoveTheKeysBetweenColumns(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlY})
	if a.focus != "s1" {
		t.Fatalf("setup: ⌃Y left the keys on %q, want s1", a.focus)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftLeft})
	if a.focus != "" {
		t.Errorf("⇧← left the keys on %q, want the room", a.focus)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftRight})
	if a.focus != "s1" {
		t.Errorf("⇧→ left the keys on %q, want s1", a.focus)
	}
}

// The difference from ⇥, and the reason both keys exist: ⇥ walks the chat ring
// and opens a conversation that is not on screen, this moves among the panes
// that are drawn and opens nothing.
func TestAShiftArrowNeverOpensAConversation(t *testing.T) {
	a := gridApp(t)
	before := panesOf(a.grid)

	notice.Reset()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftRight})

	if got := panesOf(a.grid); len(got) != len(before) {
		t.Errorf("⇧→ with only the room open changed the grid to %v, want %v: it moves the keys, it does not place a pane", got, before)
	}
	if a.focus != "" {
		t.Errorf("⇧→ moved the keys to %q with nothing to move them to", a.focus)
	}
	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.String(), "⇥") {
		t.Errorf("the refusal was %q, want it to name ⇥: a key that declines has to say what to press instead", n)
	}
}

// ⇧↓ picks the next agent - the roster nav plain ↓ gave up to the prompt
// history.
func TestShiftDownPicksTheNextAgent(t *testing.T) {
	a := pick(gridApp(t), "s1")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftDown})
	if a.roster.Selected != "s2" {
		t.Errorf("⇧↓ selected %q, want s2", a.roster.Selected)
	}
}

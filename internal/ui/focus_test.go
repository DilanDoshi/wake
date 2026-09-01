package ui

// Which pane has the keys, and how you move them there without a mouse.
//
// The gap this closes was recorded against this task by name: App.press moved
// the focus on a click and ⌃W closed the DM to get back to the room, and that
// was all - so with a conversation open the room's composer could not be
// reached from the keyboard, which made @all unreachable for as long as one was
// open. In a keyboard-first app that is a hole, not a rough edge.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// tab presses ⇥.
func tab(a App) App { a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyTab}); return a }

// openThree is three conversations open at once - the mode the amendment names
// and the one a cycle key exists for. The width is a parameter because the ring
// behaves differently either side of the takeover and every property of it needs
// asking at both.
func openThree(t *testing.T, width int) App {
	t.Helper()
	a := newRoomApp(t).withSize(width, 40).withAgents("sydney", "john", "maya")
	return a.openDMWith("s1", "sydney").openDMWith("s2", "john").openDMWith("s3", "maya")
}

// ⇥ moves the keys between the room and the conversation beside it. Asserted on
// where a keystroke lands rather than on the focus field, because a case that
// sets the field and routes nothing is the same lie as a legend entry for a key
// that does nothing.
func TestTabMovesTheKeysBetweenTheRoomAndTheConversationBesideIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")

	a = tab(a).withDraft("to the room")
	if got := a.room.Composer().Value(); got != "to the room" {
		t.Errorf("after ⇥ the room's draft is %q: the keys did not reach the room, so @all is still mouse-only", got)
	}
	if got := a.dms["s1"].Composer().Value(); got != "" {
		t.Errorf("the same keystrokes also went into the conversation: %q", got)
	}

	a = a.clearDraft()
	a = tab(a).withDraft("to sydney")
	if got := a.dms["s1"].Composer().Value(); got != "to sydney" {
		t.Errorf("after a second ⇥ the conversation's draft is %q, want it back", got)
	}
}

// The harm the gap actually caused, end to end: a broadcast typed and sent
// without touching a mouse, with a conversation open the whole time.
func TestWithAConversationOpenAtAllIsStillReachableFromTheKeyboard(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")

	a = tab(a).withDraft("@all status please")
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	frames := sentFrames(t, a, cmd)
	if len(frames) != 2 {
		t.Fatalf("the broadcast reached %d agents, want 2: with a DM open there is no keyboard-only way to address the fleet", len(frames))
	}
	for _, f := range frames {
		if f.Kind != rpc.FrameSend || f.Text != "status please" {
			t.Errorf("frame %+v, want a send carrying the message with the mention stripped", f)
		}
	}
}

// With several open, ⇥ is also how you get between them - which is what makes
// three concurrent research sessions readable, since reading happens one at a
// time. The ring is fixed and the order is the order they were opened: a ring
// built on attention rank reorders between frames, so a repeated press would
// revisit one conversation and skip another.
func TestTabCyclesEveryConversationThatIsOpenInAFixedOrder(t *testing.T) {
	a := openThree(t, 200)

	// From the newest, round the ring and back to it.
	want := []string{"", "s1", "s2", "s3"}
	for i, id := range want {
		a = tab(a)
		got := ""
		if a.focus != "" {
			got = a.focus
		}
		if got != id {
			t.Fatalf("press %d of ⇥ landed on %q, want %q (ring: room, then the order they were opened)", i+1, got, id)
		}
	}
}

// ⌃W takes a conversation out of the ring, which is what stops the ring from
// only ever growing.
//
// An id used to enter dmOrder on the first open and never leave - not on ⌃W, not
// when the agent ended - and ⇧⇥ opens a DM too, so at 15-30 agents the ring grew
// on its own as agents blocked. Five conversations meant five presses of ⇥ from
// the first one back to the room, which is the pane whose composer is the only
// place @all is typed: the exact hole this binding was taken to close, worn down
// rather than reopened. "Close" is the word for leaving the ring, and ⌃W is
// already the key that says it.
//
// Both sides of the takeover, because ⌃W and ⇥ reach the same hiding code and
// only one of them is a statement about the ring - which is exactly the pair
// the test below this one guards, and the pair whose intersection nobody
// reached the first time.
//
// Mutation check: deleting the prune in closeDM leaves the ring at 4 and fails
// the first assertion.
func TestClosingAConversationTakesItOutOfTheRing(t *testing.T) {
	for _, width := range []int{200, 110} {
		a := openThree(t, width)
		if len(a.chats()) != 4 {
			t.Fatalf("at %d columns three conversations gave a ring of %d, so this test starts from the wrong place", width, len(a.chats()))
		}

		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW}) // closes maya, which has the keys
		if got := a.chats(); len(got) != 3 || slices.Contains(got, "s3") {
			t.Errorf("at %d columns, after ⌃W the ring is %v, want the room and the two still open", width, got)
		}
		if _, held := a.dms["s3"]; !held {
			t.Errorf("at %d columns ⌃W dropped the conversation itself: leaving the ring is about the screen, not about the transcript", width)
		}

		// And ⇥ now walks the shorter ring rather than a stale one.
		for i, want := range []string{"s1", "s2", ""} {
			a = tab(a)
			got := ""
			if a.focus != "" {
				got = a.focus
			}
			if got != want {
				t.Fatalf("at %d columns, press %d of ⇥ after a close landed on %q, want %q", width, i+1, got, want)
			}
		}

		// An agent *ending* does not take it out, and that is a decision rather
		// than this same hole left half-closed: a finished conversation is still
		// worth reading, and ⌃W is how it leaves. Nothing near dmOrder reads
		// StateEnded, so there is no code here to test - only the choice.

		// Reopening puts it back, at the end - so the ring is "what is open"
		// rather than "what has ever been opened".
		a = a.openDMWith("s3", "maya")
		if got := a.chats(); len(got) != 4 || got[len(got)-1] != "s3" {
			t.Errorf("at %d columns reopening left the ring %v: a conversation that comes back has to come back into the ring", width, got)
		}
	}
}

// The intersection the two ring guards left: a takeover width *and* repeated ⇥.
//
// One of them prunes the ring at 200 columns and never reaches showRoom's
// takeover branch; the other presses ⇥ exactly once at 110. Between them sits
// the mirror image of the hole the prune was added to close: below 120 columns
// the room is never drawn beside a conversation, so showRoom reaches it by
// hiding the DM - and if hiding prunes, every ⇥ that lands on the room costs a
// ring member. A laptop at 110 columns is an ordinary terminal, and the key
// would degrade every time it was used.
//
// Mutation check: making showRoom prune fails this at "eight presses of ⇥ left
// the ring [...]".
func TestTabAtATakeoverWidthDoesNotEatTheRing(t *testing.T) {
	a := newRoomApp(t).withSize(110, 40).withAgents("sydney", "john", "maya")
	a = a.openDMWith("s1", "sydney").openDMWith("s2", "john").openDMWith("s3", "maya")
	if a.regions().Room() != 0 {
		t.Fatal("the room is drawn beside a conversation at 110 columns, so this test is not in the takeover case")
	}
	want := slices.Clone(a.chats())

	// Two full laps: room, each conversation, round again.
	var visited []string
	for range 8 {
		a = tab(a)
		if a.focus != "" {
			visited = append(visited, a.focus)
		} else {
			visited = append(visited, "")
		}
	}
	if got := a.chats(); !slices.Equal(got, want) {
		t.Errorf("eight presses of ⇥ left the ring %v, want the %v it started with - the key that moves the focus is spending the ring to do it", got, want)
	}
	if want := []string{"", "s1", "s2", "s3", "", "s1", "s2", "s3"}; !slices.Equal(visited, want) {
		t.Errorf("⇥ visited %v over two laps, want %v", visited, want)
	}
}

// Switching must cost nothing. Each conversation keeps its own transcript, and
// DM.SetSize re-wraps only on a width change - switching is not one - so a
// switch that re-rendered would be paying the 248ms-at-3,000-events re-wrap for
// a geometry nothing changed about.
//
// Mutation check: sizing the incoming pane unconditionally rather than through
// SetSize's width guard fails this at "switching between two conversations cost
// N re-wraps".
func TestSwitchingBetweenConversationsReWrapsNeitherAndKeepsBothScrollbacks(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a = a.openDMWith("s1", "sydney")
	for i := range 60 {
		a = said(a, "s1", "sydney line "+string(rune('a'+i%26)))
	}
	a = a.openDMWith("s2", "john")
	for i := range 60 {
		a = said(a, "s2", "john line "+string(rune('a'+i%26)))
	}

	// Read back in john, which is the state a switch must not disturb.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyPgUp})
	johnBefore := strings.Join(dmLines(t, a, "s2"), "\n")
	sydneyBefore := strings.Join(dmLines(t, a, "s1"), "\n")

	_, dm := countPaneRenders(t, func() {
		a = tab(a) // to the room
		a = tab(a) // to sydney
		a = tab(a) // to john
	})
	if dm != 0 {
		t.Errorf("switching between two conversations cost %d re-wraps. Each keeps its own transcript and only a width change re-wraps one, so a switch is meant to cost a map lookup", dm)
	}
	if got := strings.Join(dmLines(t, a, "s2"), "\n"); got != johnBefore {
		t.Errorf("john's conversation came back different from how it was left - the scrollback did not survive the switch:\n%s", got)
	}
	if got := strings.Join(dmLines(t, a, "s1"), "\n"); got != sydneyBefore {
		t.Errorf("sydney's conversation changed while nobody was in it:\n%s", got)
	}
}

// ⇥ with nothing open has nowhere to go, and says so rather than doing nothing
// quietly - a control that is advertised, taken and silent is the failure the
// legend rule exists for, arriving at runtime instead of in the legend.
func TestTabWithNothingOpenBesideTheRoomSaysSo(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = tab(a)

	if a.focus != "" {
		t.Fatalf("⇥ with nothing open left the keys on %q, want the room", a.focus)
	}
	if n := notice.Count(noOtherChat); n != 1 {
		t.Errorf("⇥ with nothing open beside the room reported %d times, want 1", n)
	}
}

// Below the takeover width the room is not drawn at all, so focusing it would
// put every keystroke into a composer nobody can see - which is two drafts
// diverging from what the operator can see, the failure App.composer exists to
// prevent.
//
// The correction that used to live in resizePanes is gone, and this is the
// stronger property that replaced it: below the takeover exactly one column is
// drawn, and it is whichever one holds the keys. The room no longer has to be
// swapped for the conversation, because the drawn window follows the focus in
// both directions - see Layout.window.
//
// Mutation check: pinning window() to 0 fails this at "the keys are on the
// conversation and the column drawn is the room's".
func TestTheDrawnColumnAtANarrowWidthIsTheOneWithTheKeys(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")

	a, _ = a.resized(110, 40)
	a = settle(a)
	if got := a.regions().Drawn(); got != 1 {
		t.Fatalf("%d columns are drawn at 110, want 1: this test is not in the takeover case", got)
	}
	if a.regions().Room() != 0 {
		t.Fatal("the keys are on the conversation and the column drawn is the room's")
	}

	// And back the other way: ⇥ to the room draws the room instead, rather than
	// leaving the keys in a pane nobody can see.
	a = tab(a)
	if a.focus != "" {
		t.Fatalf("⇥ left the keys on %q, want the room", a.focus)
	}
	if a.regions().Room() == 0 {
		t.Fatal("the keys are in the room and the room is not the column drawn")
	}

	a = a.withDraft("where does this go")
	if got := a.room.Composer().Value(); got != "where does this go" {
		t.Errorf("a keystroke did not reach the room's composer, which is the pane on screen: %q", got)
	}
	if got := a.dms["s1"].Composer().Value(); got != "" {
		t.Errorf("the keystroke also reached the conversation, which is off screen: %q", got)
	}
	// And the accent followed. The drawn window is a second write path into what
	// is visible, so "which pane takes a keystroke" and "which box is marked"
	// have to agree on it too - two properties each held on their own, whose
	// intersection is where this task has now been bitten twice.
	forceColour(t)
	if !accented(a.room.Composer(), accentEscape(t)) {
		t.Error("the room is the pane on screen and holds the keys, and the accent is somewhere else")
	}
}

// And ⇥ from a conversation that has the whole pane brings the room back, which
// is the same rule read the other way: ⇥ focuses the next conversation, and a
// conversation you focus is one you can see.
func TestTabToTheRoomAtATakeoverWidthPutsTheRoomBack(t *testing.T) {
	a := newRoomApp(t).withSize(110, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")
	if a.regions().Room() != 0 {
		t.Fatalf("the room is drawn at 110 columns with a DM open, so this test is not in the takeover case")
	}

	a = tab(a)
	if a.regions().Room() == 0 {
		t.Error("⇥ moved the keys to a room that is still not on screen")
	}
	a = a.withDraft("@all hello")
	if got := a.room.Composer().Value(); got != "@all hello" {
		t.Errorf("the room's draft is %q after ⇥ at a takeover width", got)
	}
}

// ⇧⇥ is where spec §6's next-agent jump went when ⇥ took pane focus. ⌃⇧A is not
// bindable in bubbletea v1.3.10 at all and ⇧⇥ is - see docs/notes/decisions.md
// and keyprobe_test.go, which drives the bytes.
func TestShiftTabJumpsToTheAgentThatNeedsYouAndOpensIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlX})
	if a.roster.Selected != "s3" {
		t.Errorf("⇧⇥ selected %q, want the blocked agent", a.roster.Selected)
	}
	if a.focus != "s3" {
		t.Errorf("⇧⇥ selected an agent and did not open it: open=%q", a.focus)
	}
	if !strings.Contains(shown(a), agentPrefix+"marco") {
		t.Errorf("the pane it opened does not name marco:\n%s", shown(a))
	}
}

// --- which box you are typing into --------------------------------------

// accentEscape is the SGR sequence lipgloss emits for the accent at the forced
// profile. Derived rather than hard-coded, for the reason appearance_test.go
// derives its background: the assertion is about whether the style is applied,
// not about how lipgloss spells it.
func accentEscape(t *testing.T) string {
	t.Helper()
	rendered := lipgloss.NewStyle().Foreground(Accent).Render("x")
	esc, _, ok := strings.Cut(rendered, "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape for the accent at this profile: %q", rendered)
	}
	return esc
}

// accented reports whether a composer's top border carries the accent. The
// border is the first thing drawn, so checking the whole box would also pass on
// the target line underneath.
func accented(c Composer, esc string) bool {
	return strings.Contains(strings.SplitN(c.View(40), "\n", 2)[0], esc)
}

// With two composers on screen, one of them accented is worth nothing: the
// accent is what answers "where do I type" without looking, and two of them
// answers it wrongly half the time. This is the first task in which there are
// two, which is why it is the task that has to decide.
//
// Mutation check: rendering every composer through ComposerStyle regardless
// fails this at "both composers are outlined in the accent".
func TestOnlyTheComposerWithTheKeysIsAccented(t *testing.T) {
	forceColour(t)
	esc := accentEscape(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")

	if !accented(a.dms["s1"].Composer(), esc) {
		t.Error("the conversation just opened is where the keys are and its composer is not marked")
	}
	if accented(a.room.Composer(), esc) {
		t.Error("both composers are outlined in the accent, so neither says where a keystroke will land")
	}

	a = tab(a)
	if !accented(a.room.Composer(), esc) {
		t.Error("⇥ moved the keys to the room and the room's composer is not marked")
	}
	if accented(a.dms["s1"].Composer(), esc) {
		t.Error("the conversation kept the accent after the keys left it")
	}

	// And through the mouse, which is the other write path into the focus. Both
	// go through withFocus, so this is asserting that they still do rather than
	// a second behaviour - the intersection of "a click moves the focus" and
	// "the focused box is marked" is otherwise reached by nothing.
	a = a.press(midOf(a.regions(), 1), 0) // into the DM column, which takes the keys
	if a.focus == "" {
		t.Fatalf("a click in the DM column did not land in the DM (regions %+v), so the accent below proves nothing", a.regions())
	}
	if !accented(a.dms["s1"].Composer(), esc) || accented(a.room.Composer(), esc) {
		t.Error("a click moved the keys and the accent stayed where it was")
	}
}

// A room with nothing beside it is the only composer there is, so it keeps the
// accent - otherwise every single-pane window would draw an unmarked box and
// the one thing the accent is for would be missing from the common case.
func TestTheRoomAloneKeepsTheAccent(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	if !accented(a.room.Composer(), accentEscape(t)) {
		t.Error("a room with no conversation beside it draws an unmarked composer")
	}
}

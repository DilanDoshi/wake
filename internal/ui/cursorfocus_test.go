package ui

// The caret comes back when a pane takes the keys.
//
// bubbles keys every blink message to one cursor's id (cursor.go), and only the
// focused composer is fed messages (app.go's Update). So a focus change routes
// the old pane's blink to a cursor that rejects it: the loop dies, and the pane
// now holding the keys is left frozen at whatever phase its own loop last
// stopped in - invisible half the time - with nothing to toggle it back until a
// keystroke moves the cursor and textarea.Update revives it. The operator sees a
// pane that takes their typing with no caret in it.
//
// These are synchronous, and the real loop's 530ms goroutine never fires here -
// which is exactly why the whole suite never saw this. darkenCursor stands in for
// "the loop toggled the caret off and then died", the state a refocus recovers.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// reverseEscape is the SGR sequence lipgloss emits for reverse video, which is
// how bubbles draws a visible cursor (cursor.View). Derived rather than
// hard-coded, for the reason accentEscape is: the assertion is about whether the
// caret is drawn, not about how the escape is spelled.
func reverseEscape(t *testing.T) string {
	t.Helper()
	rendered := lipgloss.NewStyle().Reverse(true).Render("x")
	esc, _, ok := strings.Cut(rendered, "x")
	if !ok || esc == "" {
		t.Fatalf("lipgloss emitted no escape for reverse video at this profile: %q", rendered)
	}
	return esc
}

// hasCursor reports whether a composer draws its caret at the given width.
func hasCursor(c Composer, esc string) bool {
	return strings.Contains(c.View(40), esc)
}

// darkenCursor puts a pane's caret in the off phase its blink loop leaves behind
// when the keys move away - the state that is invisible until you type. "" is the
// room.
func darkenCursor(a App, id string) App {
	if id == "" {
		c := a.room.Composer()
		c.ta.Cursor.Blink = true
		a.room = a.room.WithComposer(c)
		return a
	}
	d := a.dms[id]
	c := d.Composer()
	c.ta.Cursor.Blink = true
	return a.withDM(id, d.WithComposer(c))
}

// clickThrough delivers a left click the way Bubble Tea does - through Update, so
// the focus-change handling that follows a.update runs. clickAt goes straight to
// a.mouse and would skip it.
func clickThrough(a App, x, y int) App {
	m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	return m.(App)
}

// Refocus turns an off caret back on and hands back a command to blink it.
//
// The command matters as much as the caret: without it the cursor is solid
// rather than blinking, because the loop that toggles it is never restarted.
//
// Mutation check: returning a nil command fails "returned no blink command";
// dropping the c.ta.Focus() call fails "draws no caret".
func TestRefocusRevivesAnOffCursor(t *testing.T) {
	forceColour(t)
	esc := reverseEscape(t)
	c := NewComposer()
	c.ta.Cursor.Blink = true // the off phase the dead loop leaves behind
	if hasCursor(c, esc) {
		t.Fatal("the caret is on before Refocus, so this proves nothing")
	}

	c, cmd := c.Refocus()
	if !hasCursor(c, esc) {
		t.Error("after Refocus the composer draws no caret")
	}
	if cmd == nil {
		t.Error("Refocus returned no blink command: the caret is solid, not blinking")
	}
}

// Blurring a composer hides its caret and nothing else.
//
// The caret is the only thing focus may change inside the box - the border is
// the pane's focus tell, and the draft text reads the same everywhere. A whole
// textarea.Blur would also swap to BlurredStyle and tint the draft; Focused
// pokes only Cursor.Blink, so the text area stays focused. ta.Focused() is the
// robust discriminator - comparing rendered escapes is not, because the caret
// cell alone differs between a focused and a blurred draw whatever the style.
//
// Mutation check: Focused calling c.ta.Blur() instead flips ta.Focused() to
// false and fails at "blurring the composer blurred its text area".
func TestBlurringHidesOnlyTheCaret(t *testing.T) {
	forceColour(t)
	esc := reverseEscape(t)
	c := NewComposer().SetWidth(40)
	c.ta.SetValue("hello world")

	focused := c.Focused(true)
	blurred := c.Focused(false)

	if !hasCursor(focused, esc) {
		t.Fatal("the focused composer draws no caret, so this test proves nothing")
	}
	if hasCursor(blurred, esc) {
		t.Error("the blurred composer still draws a caret")
	}
	if !blurred.ta.Focused() {
		t.Error("blurring the composer blurred its text area: the draft text will render in BlurredStyle, not just lose its caret")
	}
}

// ⇥ back into a conversation whose caret went dark brings the caret back.
//
// Mutation check: without the refocus in App.Update the caret stays off and this
// fails at "draws no caret".
func TestTabbingBackToAConversationRevivesItsCursor(t *testing.T) {
	forceColour(t)
	esc := reverseEscape(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")

	a = tab(a) // keys to the room
	if a.focus != "" {
		t.Fatalf("⇥ left the keys on %q, want the room - the rest of this test proves nothing", a.focus)
	}
	// While s1 is blurred its loop dies in the off phase.
	a = darkenCursor(a, "s1")
	if hasCursor(a.dms["s1"].Composer(), esc) {
		t.Fatal("darkenCursor did not turn the caret off, so this test starts from the wrong state")
	}

	a = tab(a) // keys back to s1
	if a.focus != "s1" {
		t.Fatalf("⇥ landed on %q, want s1", a.focus)
	}
	if !hasCursor(a.dms["s1"].Composer(), esc) {
		t.Error("after ⇥ back to the conversation its composer draws no caret: it takes typing but shows no cursor")
	}
	// And exactly one caret: the room lost the keys, so it shows none.
	if hasCursor(a.room.Composer(), esc) {
		t.Error("the room still draws a caret after the keys left it: two carets, only one box live")
	}
}

// The reported repro: click into the room and back into the DM, and the DM's
// caret is gone until you type. Delivered through Update, which is where a real
// click lands and where the fix has to be for the mouse path to reach it.
//
// Mutation check: without the refocus in App.Update this fails at "draws no
// caret", the same as the keyboard path - one fix covers both because both move
// a.focus.
func TestClickingBackIntoAPaneRevivesItsCursor(t *testing.T) {
	forceColour(t)
	esc := reverseEscape(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a = a.openDMWith("s1", "sydney")

	roomStart, roomEnd := colRange(a.regions(), 0)
	dmStart, dmEnd := colRange(a.regions(), 1)

	a = clickThrough(a, (roomStart+roomEnd)/2, 2) // into the room
	if a.focus != "" {
		t.Fatalf("a click in the room column landed on %q, want the room (regions %+v)", a.focus, a.regions())
	}
	a = darkenCursor(a, "s1")

	a = clickThrough(a, (dmStart+dmEnd)/2, 2) // back into the DM
	if a.focus != "s1" {
		t.Fatalf("a click in the DM column landed on %q, want s1 (regions %+v)", a.focus, a.regions())
	}
	if !hasCursor(a.dms["s1"].Composer(), esc) {
		t.Error("after clicking back into the conversation its composer draws no caret - which is exactly what was reported")
	}
	if hasCursor(a.room.Composer(), esc) {
		t.Error("the room still draws a caret after the click moved the keys away: two carets on screen")
	}
}

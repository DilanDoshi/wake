package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// typeInto feeds each rune of s to the composer as a separate key message,
// the way a terminal delivers real typing.
func typeInto(t *testing.T, c Composer, s string) Composer {
	t.Helper()
	for _, r := range s {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return c
}

// The ui.Mode enum is gone, and its two tests with it. A mode is now one of
// core's PermissionMode constants end to end - the word is Claude's, it travels
// as JSON in a set_permission_mode request, and a second representation here
// would be the parallel implementation this project forbids. What used to be
// Mode.String's fallback is nextMode's, and it is tested in mode_test.go.

func TestNewComposerStartsEmptyInTheSpawnMode(t *testing.T) {
	c := NewComposer()
	if got := c.Value(); got != "" {
		t.Errorf("Value() = %q, want empty", got)
	}
	if got := c.Mode(); got != spawnedMode {
		t.Errorf("Mode() = %v, want %v", got, spawnedMode)
	}
}

func TestComposerAcceptsTypedRunes(t *testing.T) {
	c := NewComposer()
	for _, r := range "hello" {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := c.Value(); got != "hello" {
		t.Errorf("Value() = %q, want %q", got, "hello")
	}
}

func TestComposerBackspaceDeletesRune(t *testing.T) {
	c := typeInto(t, NewComposer(), "hey")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := c.Value(); got != "he" {
		t.Errorf("Value() after backspace = %q, want %q", got, "he")
	}
}

// ⇧⇥ used to be caught here, cycling the indicator: it changed a label and
// reached no agent, so a reader who cycled to "manual" believed tool calls
// would start asking, and they did not.
//
// It is a real key now, and this is still the guard - one layer over. The
// composer must not move the mode, because the *App* owns it: the key writes a
// frame and the label moves when the daemon's receipt arrives. A composer that
// cycled its own copy would be the deleted defect rebuilt underneath a working
// mechanism, and it would win, because this is what gets drawn.
func TestTheComposerDoesNotMoveTheModeItself(t *testing.T) {
	c := NewComposer()
	for range 4 {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		if got := c.Mode(); got != spawnedMode {
			t.Fatalf("shift+tab moved the indicator to %v; every session in this build runs %v", got, spawnedMode)
		}
	}
}

// It must still not type. A key with no meaning inserts nothing.
func TestShiftTabDoesNotTypeIntoTheDraft(t *testing.T) {
	c := typeInto(t, NewComposer(), "draft")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := c.Value(); got != "draft" {
		t.Errorf("Value() after shift+tab = %q, want %q", got, "draft")
	}
}

// Update returns a Composer carrying the edit, with the mode untouched.
//
// It does NOT assert that the original is unchanged, and the name says so
// deliberately. The guard that did - TestUpdateLeavesTheOriginalComposerUnchanged
// - observed value semantics through the permission mode, and the ruling that
// removed the Shift+Tab cycle removed the only thing that ever changed it.
//
// The draft cannot stand in: typing appends into a textarea the copies share by
// pointer, so `original` sees the edit too. So the value-semantics property of
// Composer.Update is **unassertable through the exported surface** - not merely
// untested, and not unassertable outright: an in-package probe of the text
// area's own cursor position would still catch Update being changed to a
// pointer receiver. What no exported accessor can distinguish is the copy from
// the original.
//
// Recorded rather than papered over with an assertion that would pass for the
// wrong reason. It becomes assertable through the exported surface again the
// moment Composer grows a second value field that Update writes, which deferred
// I7's real permission mode would be. Meanwhile the thing that IS true is
// pinned instead - see TestTwoComposersShareOneTextArea.
func TestUpdateReturnsTheEditedComposer(t *testing.T) {
	original := NewComposer()
	typed, _ := original.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if got := typed.Value(); got != "x" {
		t.Errorf("returned Value() = %q, want %q", got, "x")
	}
	if got := typed.Mode(); got != spawnedMode {
		t.Errorf("returned Mode() = %v, want %v", got, spawnedMode)
	}
}

func TestUpdateIgnoresNonKeyMessages(t *testing.T) {
	c := typeInto(t, NewComposer(), "hi")
	c, _ = c.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	if got := c.Value(); got != "hi" {
		t.Errorf("Value() = %q, want %q", got, "hi")
	}
	if got := c.Mode(); got != spawnedMode {
		t.Errorf("Mode() = %v, want %v", got, spawnedMode)
	}
}

func TestResetClearsValueButKeepsMode(t *testing.T) {
	c := NewComposer()
	mode := c.Mode()
	for _, r := range "draft" {
		c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	c = c.Reset()
	if c.Value() != "" {
		t.Errorf("Value() after Reset = %q, want empty", c.Value())
	}
	if c.Mode() != mode {
		t.Error("Reset must not change the permission mode")
	}
}

func TestResetReturnsANewComposer(t *testing.T) {
	original := typeInto(t, NewComposer(), "draft")
	cleared := original.Reset()

	if got := original.Value(); got != "draft" {
		t.Errorf("original Value() = %q, want %q", got, "draft")
	}
	if got := cleared.Value(); got != "" {
		t.Errorf("returned Value() = %q, want empty", got)
	}
}

func TestComposerAcceptsTypingAfterReset(t *testing.T) {
	c := typeInto(t, NewComposer(), "first")
	c = typeInto(t, c.Reset(), "second")

	if got := c.Value(); got != "second" {
		t.Errorf("Value() = %q, want %q", got, "second")
	}
}

// Rendered wide enough for the whole legend. It used to be 60, then 100; the
// room brought five more keys and the legend is truncated to the pane rather
// than allowed to run, so a narrow pane cuts it - which is what
// legend_test.go is about. Derived from the legend rather than written out, so
// adding a key moves it here too.
func TestViewShowsModeAndHint(t *testing.T) {
	c := NewComposer()
	out := c.View(fullLegendWidth)
	if !strings.Contains(out, c.Mode()) {
		t.Errorf("view does not show the mode:\n%s", out)
	}
	if !strings.Contains(out, "send") {
		t.Errorf("view does not show the send hint:\n%s", out)
	}
}

// What a legend too long for its pane loses, and in what order, is
// legend_test.go's business - along with the guard that the keys it names are
// the keys the App binds.

// The legend must describe only what this build does. Two of the things it
// used to advertise did not exist, and the permissions one failed in the
// unsafe direction.
//
// ⇧⇥ used to be in that list and has left it, because it now carries the
// next-blocked jump and does something. What is left is the pair that cannot
// change: ⌃⇧A produces no KeyMsg at all in this library, so a legend entry for
// it could never be honest; and the mode still has no key glyph in front of it,
// which is the residue of the ⇧⇥ story and the half that would silently come
// back if somebody re-bound a key to it without making it real.
func TestTheHintAdvertisesNoKeyThatDoesNothing(t *testing.T) {
	out := NewComposer().View(fullLegendWidth)

	if strings.Contains(out, "⌃⇧A") {
		t.Errorf("the hint advertises ⌃⇧A, which bubbletea names for no byte sequence at all:\n%s", out)
	}
	mode := fmt.Sprintf(modeFormat, spawnedMode)
	if !strings.Contains(out, hintSep+mode) {
		t.Errorf("the mode is not a bare statement after a separator - something is standing where a key glyph would be, about a mode nothing in this build can change:\n%s", out)
	}
	if !strings.Contains(out, spawnedMode) {
		t.Errorf("the hint no longer says which permissions the session runs under:\n%s", out)
	}
}

func TestViewShowsTheDraftInsideABorderedBox(t *testing.T) {
	c := typeInto(t, NewComposer(), "ship it")
	out := c.View(60)

	if !strings.Contains(out, "ship it") {
		t.Errorf("view does not show the draft:\n%s", out)
	}
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╯") {
		t.Errorf("view is not wrapped in a rounded box:\n%s", out)
	}
}

// View renders; it must not edit. Two calls in a row agree, and the draft
// survives them.
func TestViewDoesNotChangeTheComposer(t *testing.T) {
	c := typeInto(t, NewComposer(), "stable")

	first := c.View(60)
	second := c.View(60)
	if first != second {
		t.Errorf("View is not stable:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if got := c.Value(); got != "stable" {
		t.Errorf("Value() after View = %q, want %q", got, "stable")
	}
	if got := c.Mode(); got != spawnedMode {
		t.Errorf("Mode() after View = %v, want %v", got, spawnedMode)
	}
}

// A pane in a 30-way grid can get very narrow, and the layout joins panes on
// their widest line. So every line View returns — the hint included, which is
// why it is truncated rather than left to run at its natural ~27 columns —
// must measure exactly the width it was given, floored at minComposerWidth.
func TestViewFillsItsWidthAndNeverExceedsIt(t *testing.T) {
	const draft = "a draft long enough to overflow a narrow pane"

	for _, width := range []int{-5, 0, 1, 4, 8, 12, 27, 40, 60} {
		c := typeInto(t, NewComposer(), draft)
		out := c.View(width)

		want := max(width, minComposerWidth)
		if got := lipgloss.Width(out); got != want {
			t.Errorf("View(%d) measures %d columns, want %d:\n%s", width, got, want, out)
		}
		if !strings.Contains(out, "send") {
			t.Errorf("View(%d) dropped the send hint:\n%s", width, out)
		}
	}
}

// The hazard, pinned in place of the guarantee that cannot be.
//
// Composer's methods return a new Composer, which copies the mode and does not
// copy the draft: textarea.Model keeps its buffer behind pointers, so two
// Composers are two views of one text area. Everything above this line assumes
// it, dm.go's Composer accessor warns about it, and until now nothing asserted
// it - so a bubbles release that started deep-copying would have gone unnoticed
// until a caller quietly stopped seeing its own edits.
func TestTwoComposersShareOneTextArea(t *testing.T) {
	original := NewComposer()
	typed := typeInto(t, original, "x")

	if got := typed.Value(); got != "x" {
		t.Fatalf("the copy did not take the edit: Value() = %q", got)
	}
	if got := original.Value(); got != "x" {
		t.Errorf("original Value() = %q, want %q: the two no longer share a text area, "+
			"so a caller may now hold an older copy - see the note on Composer", got, "x")
	}
}

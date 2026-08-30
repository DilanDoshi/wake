package ui

import (
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

// The info bar the composer carries sits above the armed cue - the info row
// over the keys row, the order the operator asked for. The cue is only drawn
// while an arm is live, so this uses a detach-armed composer to have a cue at
// all. The composer only places the pre-rendered bar; the pane builds it (it
// reads the filesystem).
func TestComposerDrawsBarAboveLegend(t *testing.T) {
	c := NewComposer().WithBar("~/repo  main  Opus 5  effort:xhigh").WithArms(legendArms{detach: true})
	out := c.View(120)
	lines := strings.Split(out, "\n")
	barAt, hintAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "effort:xhigh") {
			barAt = i
		}
		if strings.Contains(l, armedSendLabel) {
			hintAt = i
		}
	}
	if barAt < 0 {
		t.Fatalf("the bar was not drawn:\n%s", out)
	}
	if hintAt < 0 {
		t.Fatalf("the armed cue was not drawn:\n%s", out)
	}
	if barAt > hintAt {
		t.Fatalf("the bar (row %d) must sit above the cue (row %d):\n%s", barAt, hintAt, out)
	}
}

// An empty bar draws no line, the way an empty target does.
func TestComposerWithNoBarDrawsNone(t *testing.T) {
	before := len(strings.Split(NewComposer().View(120), "\n"))
	after := len(strings.Split(NewComposer().WithBar("").View(120), "\n"))
	if before != after {
		t.Errorf("an empty bar changed the row count from %d to %d", before, after)
	}
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

// The composer no longer draws a mode or an always-on key hint - both moved to
// the status bar, and what one composer draws by default is exactly its box.
// legend_test.go's TestUnarmedComposerDrawsNoLegendRow is that guard now, and
// the armed cue is TestArmedComposerDrawsOnlyTheCue's. The ⌃⇧A-cannot-be-drawn
// property is the bijection guard's: legendEntries has no such entry.

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
// their widest line. So every line View returns — the armed cue included, which
// is why it is truncated rather than left to run — must measure exactly the
// width it was given, floored at minComposerWidth. Exercised armed and unarmed,
// because the cue is the only row whose width the box does not fix.
func TestViewFillsItsWidthAndNeverExceedsIt(t *testing.T) {
	const draft = "a draft long enough to overflow a narrow pane"

	for _, arms := range []legendArms{{}, {detach: true}} {
		for _, width := range []int{-5, 0, 1, 4, 8, 12, 27, 40, 60} {
			c := typeInto(t, NewComposer().WithArms(arms), draft)
			out := c.View(width)

			want := max(width, minComposerWidth)
			if got := lipgloss.Width(out); got != want {
				t.Errorf("View(%d) with %+v measures %d columns, want %d:\n%s", width, arms, got, want, out)
			}
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

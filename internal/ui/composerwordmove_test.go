package ui

// ⌥← and ⌥B with no word to their left (BUG-78). bubbles v1.0.0's wordLeft
// walks left until it meets a non-space and never stops when there is none, so
// the key froze the room's Update goroutine. Every test runs the key through
// `promptly`, which fails after wordMoveDeadline instead of hanging the suite.

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

const wordMoveDeadline = 500 * time.Millisecond

// caretMarker is typed where the caret stands to read it back off the draft.
const caretMarker = '¤'

type namedKey struct {
	name string
	msg  tea.KeyMsg
}

// wordBackKeys are the two keys the text area binds to wordLeft.
var wordBackKeys = []namedKey{
	{"⌥←", tea.KeyMsg{Type: tea.KeyLeft, Alt: true}},
	{"⌥B", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true}},
}

// otherWordKeys are the word keys that reach the same walk from the other side.
// None loops or panics (probed over every caret of every draft below); pinned so
// a bubbles upgrade cannot change that quietly.
var otherWordKeys = []namedKey{
	{"⌥→", tea.KeyMsg{Type: tea.KeyRight, Alt: true}},
	{"⌥F", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}, Alt: true}},
	{"⌥⌦", tea.KeyMsg{Type: tea.KeyDelete, Alt: true}},
	{"⌥D", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}, Alt: true}},
	{"⌥⌫", tea.KeyMsg{Type: tea.KeyBackspace, Alt: true}},
	{"⌃W", tea.KeyMsg{Type: tea.KeyCtrlW}},
	{"⌥U", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}, Alt: true}},
	{"⌥L", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}, Alt: true}},
	{"⌥C", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}, Alt: true}},
}

// sweepDrafts are the shapes the sweeps walk, every caret of each. The long ones
// soft-wrap at the narrow width, which puts a caret on a wrapped row.
var sweepDrafts = []string{
	"", " ", "   ", "\n", "\n\n", " \n ", "a", " a", "a ", "  a  ", "a b", "ab cd",
	"a\n", "\na", "a\n\nb", "  \n a", "a\n   ", " \n\n ", "   \n   \nx y", "a\n\n",
	"\n\n  a b", "wide 世界 x", "  世界",
	"aaaa bbbb cccc dddd", "abcdefghijklmnopqrstuvwxyz", "                    ",
	"            x y", "aaaa bbbb cccc\n      dddd eeee",
	// Spaces bubbles' unicode.IsSpace knows and an ASCII-only test does not, one of them
	// two cells wide: a guard that reads display columns or ASCII whitespace misjudges them.
	"\u00a0", "\u3000", "\u2003\u2003a", "\u3000abc", "a\u00a0", "\u202f \u205f b", "\u1680\n\u3000",
}

// sweepWidths are the box widths the sweeps run at: one the drafts fit, two they wrap in.
var sweepWidths = []int{80, 12, 8}

// wordMoveHung is set by the first key that never returns. A hung key spins a
// core until the test binary exits, so later cases skip rather than add another.
var wordMoveHung atomic.Bool

// promptly runs f and fails the test, rather than hanging it, when f does not
// return in time. A key that hangs is left spinning until the test binary exits;
// a regression costs one core per hung case, so the first one stops the others.
func promptly(t *testing.T, what string, f func()) {
	t.Helper()
	if wordMoveHung.Load() {
		t.Skipf("%s: not run, an earlier key never returned and is still spinning", what)
	}
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		f()
	}()
	select {
	case p := <-done:
		if p != nil {
			t.Fatalf("%s panicked: %v", what, p)
		}
	case <-time.After(wordMoveDeadline):
		wordMoveHung.Store(true)
		t.Fatalf("%s did not return within %s", what, wordMoveDeadline)
	}
}

func composerAt(width int, draft string, off int) Composer {
	return NewComposer().SetWidth(width).WithDraft(draft).placeCursor(draft, off)
}

// caretIndex is the caret's rune offset into the draft, read by typing a marker
// where it stands and finding it again - not through LineInfo, which the guard
// reads too. The marker edits rows the composer shares with its copies, so only a
// composer that is not read again may be measured.
func caretIndex(c Composer) int {
	c.ta.InsertString(string(caretMarker))
	return slices.Index([]rune(c.Value()), caretMarker)
}

// pressWordKey runs one key through c under the deadline.
func pressWordKey(t *testing.T, c Composer, k namedKey, width int, draft string, off int) Composer {
	t.Helper()
	var next Composer
	promptly(t, k.name+" on "+describe(width, draft, off), func() { next, _ = c.Update(k.msg) })
	return next
}

func describe(width int, draft string, off int) string {
	return fmt.Sprintf("%q with the caret at rune %d, width %d", draft, off, width)
}

func TestWordBackwardWithNoWordToItsLeftChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		what  string
		draft string
		off   int
	}{
		{"an empty draft", "", 0},
		{"spaces only, caret at the start", "   ", 0},
		{"spaces only, caret between them", "   ", 1},
		{"spaces only, caret at the end", "   ", 3},
		{"a pasted tab, which the box holds as spaces", "\t", 1},
		{"caret at the start before a word", "abc", 0},
		{"caret at the start before a space and a word", " abc", 0},
		{"caret after a leading space", " abc", 1},
		{"caret inside the spaces before a word", "  a  ", 2},
		{"an empty current line over an empty line", "\n", 1},
		{"an empty current line over blank lines", "\n  \n", 4},
		{"an empty current line, the line above only spaces", " \n", 2},
		{"leading spaces of a line under blank lines", "\n\n  a b", 4},
	} {
		for _, k := range wordBackKeys {
			t.Run(k.name+" "+tc.what, func(t *testing.T) {
				c := composerAt(80, tc.draft, tc.off)
				value := c.Value()
				next := pressWordKey(t, c, k, 80, tc.draft, tc.off)
				if next.Value() != value {
					t.Errorf("the draft became %q, want it untouched: %q", next.Value(), value)
				}
				if got := caretIndex(next); got != tc.off {
					t.Errorf("the caret moved to rune %d, want it left at %d: nowhere to go", got, tc.off)
				}
			})
		}
	}
}

func TestWordBackwardStillMovesToTheStartOfAWordToItsLeft(t *testing.T) {
	for _, tc := range []struct {
		what  string
		width int
		draft string
		off   int
		want  int
	}{
		{"from the end", 80, "hello world", 11, 6},
		{"from a word's start", 80, "hello world", 6, 0},
		{"from inside a word", 80, "hello world", 8, 6},
		{"over leading spaces", 80, "  a", 3, 2},
		{"from the start of a line to the word above", 80, "foo\n", 4, 0},
		{"through empty lines to the word above", 80, "foo\n\n", 5, 0},
		{"to the last word of the line above", 80, "foo bar\n", 8, 4},
		// A caret at the start of a wrapped row sits at ColumnOffset 0 past column 0
		// of its line: the guard must read StartColumn too, or it calls a word to its
		// left no word at all.
		{"from the start of a wrapped row", 8, "aaaa bbbb cccc dddd", 10, 5},
		{"from the start of the next wrapped row", 8, "aaaa bbbb cccc dddd", 15, 10},
		{"from a wrapped row's middle", 8, "aaaa bbbb cccc dddd", 12, 10},
		{"from a wrapped row at a wider box", 12, "aaaa bbbb cccc dddd", 15, 10},
	} {
		for _, k := range wordBackKeys {
			t.Run(k.name+" "+tc.what, func(t *testing.T) {
				next := pressWordKey(t, composerAt(tc.width, tc.draft, tc.off), k, tc.width, tc.draft, tc.off)
				if next.Value() != tc.draft {
					t.Errorf("moving the caret changed the draft to %q", next.Value())
				}
				if got := caretIndex(next); got != tc.want {
					t.Errorf("the caret is at rune %d, want %d", got, tc.want)
				}
			})
		}
	}
}

// Every caret of every draft at every width: the key returns, never edits, and
// moves exactly when a non-space lies to its left - then onto the start of a word,
// strictly left of where it was. The caret is read off a typed marker, not off the
// text area's own line info that the guard reads.
func TestWordBackwardOverEveryCaretOfEveryDraft(t *testing.T) {
	for _, k := range wordBackKeys {
		t.Run(k.name, func(t *testing.T) {
			for _, width := range sweepWidths {
				for _, draft := range sweepDrafts {
					r := []rune(draft)
					for off := 0; off <= len(r); off++ {
						c := composerAt(width, draft, off)
						if got := caretIndex(composerAt(width, draft, off)); got != off {
							t.Fatalf("%s: the caret was placed at rune %d, not %d", describe(width, draft, off), got, off)
						}
						next := pressWordKey(t, c, k, width, draft, off)
						if next.Value() != draft {
							t.Fatalf("%s edited the draft to %q", describe(width, draft, off), next.Value())
						}
						got := caretIndex(next)
						if !slices.ContainsFunc(r[:off], func(x rune) bool { return !unicode.IsSpace(x) }) {
							if got != off {
								t.Fatalf("%s moved the caret to rune %d with no word to its left", describe(width, draft, off), got)
							}
							continue
						}
						if got >= off || unicode.IsSpace(r[got]) || (got > 0 && !unicode.IsSpace(r[got-1])) {
							t.Fatalf("%s left the caret at rune %d, want the start of a word left of it", describe(width, draft, off), got)
						}
					}
				}
			}
		})
	}
}

func TestTheOtherWordKeysReturnOverEveryCaretOfEveryDraft(t *testing.T) {
	for _, k := range otherWordKeys {
		t.Run(k.name, func(t *testing.T) {
			for _, width := range sweepWidths {
				for _, draft := range sweepDrafts {
					for off := 0; off <= len([]rune(draft)); off++ {
						pressWordKey(t, composerAt(width, draft, off), k, width, draft, off)
					}
				}
			}
		})
	}
}

// The guard answers the two word-backward keys and nothing else: on a draft
// with no word to the caret's left, every other key still does its job.
func TestTheGuardAnswersOnlyTheWordBackKeys(t *testing.T) {
	for _, tc := range []struct {
		what      string
		key       namedKey
		draft     string
		off       int
		wantValue string
		wantCaret int
	}{
		{"⌥→ moves over the first word", otherWordKeys[0], "ab cd", 0, "ab cd", 2},
		{"⌥F moves over the first word", otherWordKeys[1], "ab cd", 0, "ab cd", 2},
		{"⌥⌦ deletes the first word", otherWordKeys[2], "ab cd", 0, " cd", 0},
		{"⌥D deletes the first word", otherWordKeys[3], "ab cd", 0, " cd", 0},
		{"⌥U upcases the first word", otherWordKeys[6], "ab cd", 0, "AB cd", 2},
		{"⌥C capitalises the first word", otherWordKeys[8], "ab cd", 0, "Ab cd", 2},
		{"← still steps left over spaces", namedKey{"←", tea.KeyMsg{Type: tea.KeyLeft}}, "   ", 3, "   ", 2},
		{"⌃J still breaks the line", namedKey{"⌃J", tea.KeyMsg{Type: tea.KeyCtrlJ}}, "", 0, "\n", 1},
	} {
		t.Run(tc.what, func(t *testing.T) {
			next := pressWordKey(t, composerAt(80, tc.draft, tc.off), tc.key, 80, tc.draft, tc.off)
			if next.Value() != tc.wantValue {
				t.Errorf("the draft is %q, want %q", next.Value(), tc.wantValue)
			}
			if got := caretIndex(next); got != tc.wantCaret {
				t.Errorf("the caret is at rune %d, want %d", got, tc.wantCaret)
			}
		})
	}
}

// Through App.Update, where the room's goroutine lives: the key on an empty
// draft returns and the box still takes typing after it.
func TestWordBackwardOnAnEmptyDraftDoesNotFreezeTheRoom(t *testing.T) {
	for _, k := range wordBackKeys {
		t.Run(k.name, func(t *testing.T) {
			a := openedRoom(t)
			promptly(t, k.name+" through App.Update", func() { a, _ = pressKey(a, k.msg) })
			a = a.withDraft("hi")
			if got := a.dms["s1"].Composer().Value(); got != "hi" {
				t.Errorf("the draft is %q after the key and two typed letters, want %q", got, "hi")
			}
		})
	}
}

// The no-op still ends where every key ends: the box drawn back on the caret, not
// left where the wheel scrolled it. The caret stands in the blank lines that open
// the draft, so there is nothing to move to, and the wheel has taken the view down
// to the text under it. The box must then draw as one built with the caret there.
func TestAWordBackKeyWithNowhereToGoBringsAScrolledDraftBackToTheCaret(t *testing.T) {
	draft := "\n\n" + tallDraft(18)
	build := func() Composer {
		c := NewComposer().SetWidth(60).WithMaxRows(5)
		c.ta.InsertString(draft)
		return c.placeCursor(draft, 0).fit().reposition()
	}
	want := build().View(60)
	for _, k := range wordBackKeys {
		t.Run(k.name, func(t *testing.T) {
			c := build()
			for range 7 {
				c, _ = c.Wheel(-wheelLines)
			}
			if c.View(60) == want {
				t.Fatal("the wheel did not scroll the box away from the caret")
			}
			next := pressWordKey(t, c, k, 60, draft, 0)
			if got := next.View(60); got != want {
				t.Errorf("after the key the box draws\n%s\nwant it back on the caret, as built:\n%s", got, want)
			}
		})
	}
}

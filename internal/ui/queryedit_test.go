package ui

// The filter line of the jump box and the /resume picker edits as the chat box
// does: a caret that ←→ and the word keys move, typing and paste landing at it,
// and the deletes the composer has. The composer is the oracle - the parity table
// drives a real one with the same text and key - so the boxes cannot drift from it.

import (
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// splitCaret reads "foo| bar" as the text "foo bar" with the caret 4 runes from its end.
func splitCaret(s string) (string, int) {
	i := strings.Index(s, "|")
	if i < 0 {
		return s, 0
	}
	text := s[:i] + s[i+1:]
	return text, utf8.RuneCountInString(s[i+1:])
}

// showCaret is splitCaret's inverse.
func showCaret(text string, back int) string {
	r := []rune(text)
	at := len(r) - back
	return string(r[:at]) + "|" + string(r[at:])
}

func altRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true} }
func altKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t, Alt: true} }
func runeKey(s string) tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// editKey is one key the filter edits with, and its name for a failure.
type editKey struct {
	name string
	msg  tea.KeyMsg
}

var editKeys = []editKey{
	{"←", kmsg(tea.KeyLeft)}, {"→", kmsg(tea.KeyRight)},
	{"Home", kmsg(tea.KeyHome)}, {"End", kmsg(tea.KeyEnd)},
	{"⌫", kmsg(tea.KeyBackspace)}, {"⌦", kmsg(tea.KeyDelete)},
	{"⌃H", kmsg(tea.KeyCtrlH)},
	{"⌃U", kmsg(tea.KeyCtrlU)}, {"⌃K", kmsg(tea.KeyCtrlK)},
	{"⌥←", altKey(tea.KeyLeft)}, {"⌥→", altKey(tea.KeyRight)},
	{"⌥B", altRune('b')}, {"⌥F", altRune('f')},
	{"⌥⌫", altKey(tea.KeyBackspace)},
}

func editNamed(t testing.TB, name string) tea.KeyMsg {
	t.Helper()
	for _, k := range editKeys {
		if k.name == name {
			return k.msg
		}
	}
	t.Fatalf("no key named %s", name)
	return tea.KeyMsg{}
}

func TestEachKeyEditsTheFilterWhereItsCaretIs(t *testing.T) {
	for _, tc := range []struct{ key, from, want string }{
		{"←", "ab|c", "a|bc"},
		{"←", "|abc", "|abc"},
		{"→", "a|bc", "ab|c"},
		{"→", "abc|", "abc|"},
		{"Home", "foo b|ar", "|foo bar"},
		{"End", "foo b|ar", "foo bar|"},
		{"⌫", "foo| bar", "fo| bar"},
		{"⌫", "|foo", "|foo"},
		{"⌦", "fo|o", "fo|"},
		{"⌦", "foo|", "foo|"},
		{"⌥←", "foo bar| baz", "foo |bar baz"},
		{"⌥←", "foo |bar", "|foo bar"},
		{"⌥B", "foo bar|", "foo |bar"},
		{"⌥→", "foo| bar baz", "foo bar| baz"},
		{"⌥F", "|foo bar", "foo| bar"},
		{"⌥⌫", "foo bar|", "foo |"},
		{"⌥⌫", "foo bar| baz", "foo | baz"},
		{"⌥⌫", "|foo", "|foo"},
		{"⌃U", "foo b|ar", "|ar"},
		{"⌃K", "foo b|ar", "foo b|"},
		{"⌃H", "foo|", "fo|"},
	} {
		text, back := splitCaret(tc.from)
		got, gotBack := editedQuery(text, back, editNamed(t, tc.key))
		if showCaret(got, gotBack) != tc.want {
			t.Errorf("%s on %q gives %q, want %q", tc.key, tc.from, showCaret(got, gotBack), tc.want)
		}
	}
}

func TestTypingAndPasteLandAtTheCaretUnderTheQueryRules(t *testing.T) {
	for _, tc := range []struct {
		what string
		key  tea.KeyMsg
		from string
		want string
	}{
		{"a letter", runeKey("x"), "fo|o", "fox|o"},
		{"a run", runeKey("xy"), "|foo", "xy|foo"},
		{"a space between words", kmsg(tea.KeySpace), "foo|bar", "foo |bar"},
		{"a space at the start is dropped", kmsg(tea.KeySpace), "|foo", "|foo"},
		{"a second space is dropped", kmsg(tea.KeySpace), "foo |bar", "foo |bar"},
		{"a space against the next one is dropped", kmsg(tea.KeySpace), "foo| bar", "foo| bar"},
		{"a paste lands at the caret", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x\n\ty"), Paste: true}, "a|b", "ax y|b"},
		{"a control rune is dropped", runeKey("\x01"), "ab|", "ab|"},
		{"a typed word keeps no edit-key meaning", runeKey("end"), "a|b", "aend|b"},
	} {
		text, back := splitCaret(tc.from)
		got, gotBack := editedQuery(text, back, tc.key)
		if showCaret(got, gotBack) != tc.want {
			t.Errorf("%s on %q gives %q, want %q", tc.what, tc.from, showCaret(got, gotBack), tc.want)
		}
	}
}

func TestTheQueryCapHoldsWhereTheCaretIs(t *testing.T) {
	text := strings.Repeat("a", queryMax-1)
	got, back := editedQuery(text, 3, runeKey("xyz"))
	if n := utf8.RuneCountInString(got); n != queryMax || back != 3 {
		t.Errorf("a paste at the cap left %d runes and the caret %d from the end, want %d and 3", n, back, queryMax)
	}
	if want := strings.Repeat("a", queryMax-4) + "x" + "aaa"; got != want {
		t.Errorf("the paste kept %q, want what fits: %q", got, want)
	}
}

// An Alt chord no binding names must not type its letter: the text input the edits run
// through types the runes of any key it does not bind. ⌥D and ⌥⌦ are among them: the
// input's delete-word-forward panics with one rune right of the caret.
func TestAnUnboundAltChordTypesNothing(t *testing.T) {
	for _, k := range []tea.KeyMsg{altRune('x'), altRune('q'), altRune('d'), altKey(tea.KeyDelete), altKey(tea.KeySpace), {Type: tea.KeyRunes, Runes: []rune("é"), Alt: true}} {
		got, back := editedQuery("ab", 1, k)
		if got != "ab" || back != 1 {
			t.Errorf("%q changed the filter to %s", k.String(), showCaret(got, back))
		}
	}
}

func TestACaretPastEitherEndIsHeldToIt(t *testing.T) {
	if got, back := editedQuery("ab", 99, runeKey("x")); showCaret(got, back) != "x|ab" {
		t.Errorf("a caret past the start typed %s, want x|ab", showCaret(got, back))
	}
	if got, back := editedQuery("ab", -4, runeKey("x")); showCaret(got, back) != "abx|" {
		t.Errorf("a caret past the end typed %s, want abx|", showCaret(got, back))
	}
}

// composerWordLeftSpins is the one input the composer's ⌥← and ⌥B never return from:
// bubbles' wordLeft walks left until it meets a character that is not a space and has
// no way to stop where there is none - an empty draft, or only spaces left of the caret.
// The parity table skips it; the filter's own word moves must return there.
func composerWordLeftSpins(text string, caret int) bool {
	r := []rune(text)
	if caret == 0 {
		return len(r) == 0 || unicode.IsSpace(r[0])
	}
	return !slices.ContainsFunc(r[:caret], func(c rune) bool { return !unicode.IsSpace(c) })
}

// What the composer does with the same text and the same key is what the filter does.
func TestTheFilterEditsLikeTheComposerDoes(t *testing.T) {
	texts := []string{"", "x", "foo", "foo bar", "foo bar baz", "foo  bar", "web-blur ui", "éa ñb"}
	checked, skipped := 0, 0
	for _, text := range texts {
		for back := range utf8.RuneCountInString(text) + 1 {
			for _, k := range editKeys {
				if (k.name == "⌥←" || k.name == "⌥B") && composerWordLeftSpins(text, utf8.RuneCountInString(text)-back) {
					skipped++
					continue
				}
				c := NewComposer().SetWidth(100)
				c.ta.SetValue(text)
				c = c.placeCursor(text, utf8.RuneCountInString(text)-back)
				c, _ = c.Update(k.msg)
				info := c.ta.LineInfo()
				wantBack := utf8.RuneCountInString(c.Value()) - (info.StartColumn + info.ColumnOffset)

				got, gotBack := editedQuery(text, back, k.msg)
				if got != c.Value() || gotBack != wantBack {
					t.Errorf("%s on %q: the composer gives %q, the filter %q", k.name, showCaret(text, back), showCaret(c.Value(), wantBack), showCaret(got, gotBack))
				}
				checked++
			}
		}
	}
	if checked < 500 || skipped == 0 {
		t.Fatalf("the table checked %d cases and skipped %d: it is not sweeping", checked, skipped)
	}
}

// Every edit key returns, from every caret in text that has leading spaces or none at all,
// and leaves a caret inside the text.
func TestTheFilterEditsReturnFromEveryCaret(t *testing.T) {
	for _, text := range []string{"", " ", "  x", "x", " x y ", "a  "} {
		for back := range utf8.RuneCountInString(text) + 1 {
			for _, k := range editKeys {
				got, gotBack := editedQuery(text, back, k.msg)
				if n := utf8.RuneCountInString(got); gotBack < 0 || gotBack > n {
					t.Errorf("%s on %q left the caret %d from the end of a %d-rune filter", k.name, showCaret(text, back), gotBack, n)
				}
			}
		}
	}
}

// What the keys a terminal sends are named by bubbletea, so the bindings above are
// keys somebody can press. ⌃⌫ is ^H on the terminals that send a byte for it, and
// reaches Wake as no key at all under a keyboard protocol.
func TestTheKeysTheFilterEditsWithAreTheKeysBubbleteaReports(t *testing.T) {
	for _, tc := range []struct{ what, seq, want string }{
		{"⌫", "\x7f", "backspace"},
		{"⌃⌫ or ⌃H, the byte most terminals send", "\x08", "ctrl+h"},
		{"⌥⌫, as Esc then DEL", "\x1b\x7f", "alt+backspace"},
		{"⌥B", "\x1bb", "alt+b"},
		{"⌥F", "\x1bf", "alt+f"},
		{"⌥←, in the CSI encoding", "\x1b[1;3D", "alt+left"},
		{"⌥→", "\x1b[1;3C", "alt+right"},
		{"Home", "\x1b[H", "home"},
		{"End", "\x1b[F", "end"},
		{"Home, the older encoding", "\x1b[1~", "home"},
		{"End, the older encoding", "\x1b[4~", "end"},
		{"⌃U", "\x15", "ctrl+u"},
		{"⌃K", "\x0b", "ctrl+k"},
	} {
		if got := keysFor(t, tc.seq); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s: bubbletea reported %v, want [%s]", tc.what, got, tc.want)
		}
	}
}

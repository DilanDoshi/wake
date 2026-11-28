package ui

// A modal filter line edited as the chat box edits a draft: a caret, the word keys, typing
// and paste at the caret. bubbles' text input runs the text area's own bindings, so the
// composer's word-delete is not written a second time; queryedit_test.go holds the two
// together. A caret is Back, the runes between it and the end: zero, the end, is the zero
// value. The sidebar bar and /wake-help keep typedQuery and backspacedQuery (the bar is
// no modal: a key it does not read goes on to the composer).

import (
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// queryEditKeys is the key line the help carries for the edit keys of a filter line.
const queryEditKeys = "← → caret · ⌥← ⌥→ by word · Home End · ⌫ ⌦ ⌥⌫ delete · ⌃U ⌃K to the start, end"

// queryKeys is the text area's bindings for one line, less what Wake takes first (⌃W ⌃A ⌃E ⌃D
// ⌃B ⌃F and ⌃V's clipboard command), the input's suggestion keys, ⌃T's transpose and the case
// changes. ⌃H is a backspace, as in the composer. ⌥⌦ and ⌥D are left out: the input's
// delete-word-forward panics with one rune right of the caret (bubbles v1.0.0).
var queryKeys = textinput.KeyMap{
	CharacterBackward:       key.NewBinding(key.WithKeys("left")),
	CharacterForward:        key.NewBinding(key.WithKeys("right")),
	WordBackward:            key.NewBinding(key.WithKeys("alt+left", "alt+b")),
	WordForward:             key.NewBinding(key.WithKeys("alt+right", "alt+f")),
	DeleteWordBackward:      key.NewBinding(key.WithKeys("alt+backspace")),
	DeleteAfterCursor:       key.NewBinding(key.WithKeys("ctrl+k")),
	DeleteBeforeCursor:      key.NewBinding(key.WithKeys("ctrl+u")),
	DeleteCharacterBackward: key.NewBinding(key.WithKeys("backspace", "ctrl+h")),
	DeleteCharacterForward:  key.NewBinding(key.WithKeys("delete")),
	LineStart:               key.NewBinding(key.WithKeys("home")),
	LineEnd:                 key.NewBinding(key.WithKeys("end")),
}

// editedQuery is query and its caret after one key: a typed run or a paste goes in at the
// caret under typedAt's rules, any other key is the text input's, built for the key with a
// static cursor and its commands dropped, so no blink timer starts.
func editedQuery(query string, back int, m tea.KeyMsg) (string, int) {
	n := utf8.RuneCountInString(query)
	back = clamp(back, 0, n)
	switch {
	case m.Type == tea.KeyRunes && !m.Alt:
		return typedAt(query, back, string(m.Runes)), back
	case m.Type == tea.KeySpace && !m.Alt:
		return typedAt(query, back, " "), back
	case m.Type == tea.KeyRunes || m.Type == tea.KeySpace:
		// The input types the runes of any key it has no binding for, an Alt chord included.
		if !key.Matches(m, queryKeys.WordBackward, queryKeys.WordForward) {
			return query, back
		}
	}
	in := textinput.New()
	in.KeyMap = queryKeys
	in.Cursor.SetMode(cursor.CursorStatic)
	in.SetValue(query)
	in.SetCursor(n - back)
	_ = in.Focus()
	in, _ = in.Update(m)
	return in.Value(), utf8.RuneCountInString(in.Value()) - in.Position()
}

// typedAt is text typed into query at the caret, back runes from its end: one space
// between words, none leading, nothing unprintable, and no more than queryMax runes.
func typedAt(query string, back int, text string) string {
	r := []rune(query)
	at := len(r) - clamp(back, 0, len(r))
	for _, c := range text {
		if unicode.IsSpace(c) {
			c = ' '
		}
		switch {
		case unicode.IsControl(c) || c == utf8.RuneError: // the input's sanitizer drops both
		case c == ' ' && (at == 0 || r[at-1] == ' ' || (at < len(r) && r[at] == ' ')):
		case len(r) >= queryMax:
		default:
			r = slices.Insert(r, at, c)
			at++
		}
	}
	return string(r)
}

// withCaret is query drawn in room cells with the caret between its runes, or the hint and
// the caret when it is empty. A line too long is cut around the caret, the cut ends elided,
// so the caret is always on the line and the line is never wider than room. An empty caret
// draws none, as a blurred window does.
func withCaret(query string, back int, hint, caret string, room int) string {
	avail := max(room-ansi.StringWidth(caret), 0)
	if query == "" {
		return HintStyle.Render(ansi.Truncate(hint, avail, "…")) + AccentStyle.Render(caret)
	}
	r := []rune(query)
	at := len(r) - clamp(back, 0, len(r))
	before, after := string(r[:at]), string(r[at:])
	// The text left of the caret keeps what the text right of it does not need, up to half.
	before = tailOf(before, avail-min(ansi.StringWidth(after), avail/2))
	after = ansi.Truncate(after, max(avail-ansi.StringWidth(before), 0), "…")
	return TextStyle.Render(before) + AccentStyle.Render(caret) + TextStyle.Render(after)
}

package ui

// Editing a modal filter line as the chat box edits a draft: a caret that ←→ and the
// word keys move, typing and paste landing at it, and the deletes the composer has.
//
// The text area's own bindings do it, run through bubbles' text input, which holds
// the same scan for a word: the composer's word-delete is not written a second time
// here, and queryedit_test.go drives a real composer to hold the two together. The
// jump box and the /resume search call editedQuery; the sidebar bar and /wake-help's
// filter keep typedQuery and backspacedQuery (the bar is no modal: a key it does not
// read goes on to the composer, so ⌥⌫ there edits the draft).
//
// A caret is Back, the runes between it and the end of the query: zero, the end, is
// the zero value, so a picker opened with a filter already has its caret after it.

import (
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// queryEditKeys is the key line the help carries for the edit keys of a filter line.
const queryEditKeys = "← → caret · ⌥← ⌥→ by word · Home End · ⌫ ⌦ ⌥⌫ delete · ⌃U ⌃K to the start, end"

// queryKeys is the text area's bindings less the keys Wake takes before the composer
// sees them - ⌃W closes a pane, ⌃A and ⌃E toggle the narrowing and the expanding, ⌃D
// opens, ⌃B and ⌃F stack and jump, ⌃V reads the clipboard through a command - and the
// input's own suggestion keys. ⌃H is a backspace, as it is in the composer. ⌥⌦ and ⌥D,
// which delete the word ahead, are not here: the input's version of that panics with one
// rune right of the caret (bubbles v1.0.0), and nothing asked for them.
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

// editedQuery is query and its caret after one key. A typed run or a paste goes in at
// the caret under typedAt's rules; every other key is the text input's, on a copy built
// for the key (its commands are dropped: no blink timer is started).
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
		case unicode.IsControl(c):
		case c == ' ' && (at == 0 || r[at-1] == ' ' || (at < len(r) && r[at] == ' ')):
		case len(r) >= queryMax:
		default:
			r = slices.Insert(r, at, c)
			at++
		}
	}
	return string(r)
}

// withCaret is query drawn with the caret between its runes, or the hint and the caret
// when it is empty. An empty caret draws none, as a blurred window does.
func withCaret(query string, back int, hint, caret string) string {
	if query == "" {
		return HintStyle.Render(hint) + AccentStyle.Render(caret)
	}
	r := []rune(query)
	at := len(r) - clamp(back, 0, len(r))
	return TextStyle.Render(string(r[:at])) + AccentStyle.Render(caret) + TextStyle.Render(string(r[at:]))
}

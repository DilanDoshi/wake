package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// measuredDraft is a composer at width whose draft was typed through Update,
// the path every keystroke takes, and fitted under bound.
func measuredDraft(width, bound int, draft string) Composer {
	c := NewComposer().SetWidth(width).WithMaxRows(bound)
	for _, r := range draft {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == '\n' {
			key = tea.KeyMsg{Type: tea.KeyCtrlJ} // ⌃J is the newline key
		}
		c, _ = c.Update(key)
	}
	return c
}

// A re-lay that changes nothing measures nothing: the box takes the answer the
// draft was measured to, which a planted one stands in for here.
func TestARelayThatChangesNothingMeasuresNothing(t *testing.T) {
	c := measuredDraft(40, maxComposerRows, strings.Repeat("word ", 30))
	measured := c.ta.Height()
	if measured < 2 {
		t.Fatalf("the draft measured %d rows; it needs two for a planted answer to differ", measured)
	}
	planted := measured - 1
	c.measures.rows[maxComposerRows-1] = planted

	if got := c.SetWidth(40).WithMaxRows(maxComposerRows).ta.Height(); got != planted {
		t.Fatalf("a re-lay with the same draft, prompt, width and bound drew %d rows, want the %d remembered "+
			"(measured %d): it measured again", got, planted, measured)
	}
}

// Every input a measure reads is in what it is remembered by: changing any one
// of them changes the answer, and the box must take the new one rather than
// the answer remembered for the draft before. The bound changes it only for a
// draft whose top rows hold a line of spaces, which the probe cannot tell from
// its padding: drawn three rows tall, that draft's last content row is its first.
func TestEachInputOfAMeasureMeasuresAgain(t *testing.T) {
	const width, bound = 40, 10
	words := strings.Repeat("word ", 15)
	cases := []struct {
		input, draft string
		change       func(Composer) Composer
	}{
		{"draft", words, func(c Composer) Composer { return c.InsertText(strings.Repeat(" more", 10)) }},
		{"prompt", words, func(c Composer) Composer { c.ta.Prompt = strings.Repeat(">", 12) + " "; return c.fit() }},
		{"width", words, func(c Composer) Composer { return c.SetWidth(width / 2) }},
		{"bound", "a\n \n \n \nb", func(c Composer) Composer { return c.WithMaxRows(3) }},
	}
	for _, tc := range cases {
		before := measuredDraft(width, bound, tc.draft)
		after := tc.change(before)
		fresh := min(after.draftRows(after.bound()), after.bound())
		if remembered := min(before.ta.Height(), after.bound()); fresh == remembered {
			t.Fatalf("changing the %s left the measure at %d rows, so this case cannot tell a remembered answer from a new one",
				tc.input, fresh)
		}
		if got := after.ta.Height(); got != fresh {
			t.Errorf("after the %s changed the box drew %d rows, want %d measured fresh: the remembered answer "+
				"for the draft before was used", tc.input, got, fresh)
		}
	}
}

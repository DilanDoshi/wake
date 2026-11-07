package ui

// draftMeasures remembers draftRows' answers for one draft, prompt and width:
// one per bound a pane can lend, which composerRowsIn caps at maxComposerRows.
//
// A measure lays out a fresh text area, about half a megabyte (bubbles
// allocates its 10,000-row ceiling twice), and a pane drawing a card is re-laid
// on every View, wheel and key - so unremembered, a draft cost a measure per
// frame. Every copy of a composer shares one, and an answer is used only for
// the exact inputs it was measured from, so sharing it changes no answer.
//
// Unlocked on purpose: it is touched only on the Bubble Tea goroutine, where
// fit runs, and no Cmd fits a composer. A lock would order any that did with
// Update, and hide that race from -race (glamour's mutex once did).
type draftMeasures struct {
	value, prompt string
	width         int
	rows          [maxComposerRows]int // by bound-1; zero is not yet measured
}

// rowsFor is draftRows(bound) for c, measured once per draft, prompt, width and
// bound. A bound past what a pane lends, which only a test asks, is measured
// every time.
func (m *draftMeasures) rowsFor(c Composer, bound int) int {
	if bound > maxComposerRows {
		return c.draftRows(bound)
	}
	if value := c.ta.Value(); value != m.value || c.ta.Prompt != m.prompt || c.taWidth != m.width {
		*m = draftMeasures{value: value, prompt: c.ta.Prompt, width: c.taWidth}
	}
	if m.rows[bound-1] == 0 {
		m.rows[bound-1] = c.draftRows(bound)
	}
	return m.rows[bound-1]
}

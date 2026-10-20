package ui

// A multi-select question's ticks: which options are ticked, in the order they
// were ticked, and the row under the options that walks on. Drawn by
// cards_blocks.go, answered through Card.answerOf, and matched against Claude
// Code's own question screen (docs/superpowers/notes/2026-10-18-multiselect-
// question-findings.md).

import (
	"slices"

	"github.com/DilanDoshi/wake/internal/core"
)

// Toggle ticks an option of a multi-select question, or unticks it, keeping the
// order the rest were ticked in. Out-of-range indices are ignored, as in Pick.
func (c Card) Toggle(question, option int) Card {
	if !c.inRange(question, option) || question >= len(c.Ticks) {
		return c
	}
	ticks := append([][]int(nil), c.Ticks...)
	kept := slices.DeleteFunc(slices.Clone(ticks[question]), func(o int) bool { return o == option })
	if len(kept) == len(ticks[question]) {
		kept = append(kept, option)
	}
	ticks[question] = kept
	c.Ticks = ticks
	return c
}

// ticksOf is the options ticked on question i, in the order ticked.
func (c Card) ticksOf(i int) []int {
	if i < 0 || i >= len(c.Ticks) {
		return nil
	}
	return c.Ticks[i]
}

// ticked reports whether option o of question i is ticked.
func (c Card) ticked(i, o int) bool { return slices.Contains(c.ticksOf(i), o) }

// multi reports whether question i takes several options.
func (c Card) multi(i int) bool {
	return c.Detail != nil && i >= 0 && i < len(c.Detail.Questions) && c.Detail.Questions[i].Multi
}

// nextIndex is the row under a multi-select question's Other row, which walks
// on: ticking there does not.
func nextIndex(q core.Question) int { return otherIndex(q) + 1 }

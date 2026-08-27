package ui

// The transcript behind an ask, drawn quiet.
//
// A card is a prompt and reads as one because it is framed - but the rows
// above it are still full-strength conversation, and at a glance the eye lands
// on whichever is busiest rather than on the thing that has stopped an agent.
// Dimming them is what makes the card read as modal rather than as the newest
// block.
//
// # What this costs, and why it is bounded rather than cached
//
// lipgloss cannot recolour text that already carries ANSI, so a dimmed row is
// a strip and a re-render. That is per frame, which the non-negotiables are
// about - so the work is bounded by what is *drawn* rather than by what is
// held: the transcript's view is already clipped to the pane's height, so this
// is at most a screenful of rows however long the conversation is, and it runs
// only while an agent is actually blocked.
//
// A cache was considered and is worse here. The three things that change the
// answer - the drawn window, the width, and whether an ask is up - are exactly
// the things that change on a scroll and on every arriving event, so a cache
// would be invalidated on nearly every frame it was consulted on and would add
// a stale-copy failure mode to a surface whose whole job is to be current.
// BenchmarkTranscriptBehindAnAsk is the measurement.

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// quieted redraws an already-styled block in the muted colour, one row at a
// time so a row's own width is unchanged.
//
// Rows rather than the whole block, because ansi.Strip on a joined block would
// give one string that HintStyle then re-wraps at its own idea of a width.
func quieted(block string) string {
	if block == "" {
		return block
	}
	open, close := quietSGR()
	rows := strings.Split(block, "\n")
	for i, r := range rows {
		rows[i] = open + ansi.Strip(r) + close
	}
	return strings.Join(rows, "\n")
}

// quietSGR is HintStyle's own escape pair, taken once and then applied by
// concatenation.
//
// A lipgloss Render per row is what made quieting cost a fifth of a frame: it
// re-derives the colour profile, measures and re-wraps, none of which a row
// that is already exactly one row needs. Taken from the style rather than
// written down, so a palette change carries.
//
// Lazily rather than at init, because the colour profile is detected when the
// program takes the terminal - a pair captured before that is the pair for no
// colour at all, which would silently un-dim the whole feature.
var quietSGR = sync.OnceValues(func() (string, string) {
	const sentinel = "\x00"
	painted := HintStyle.Render(sentinel)
	open, close, found := strings.Cut(painted, sentinel)
	if !found {
		// No colour profile: Render handed back the text unchanged, so there
		// is nothing to wrap a row in.
		return "", ""
	}
	return open, close
})

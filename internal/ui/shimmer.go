package ui

// Claude Code's shimmer: a highlight that sweeps along a line of text while an
// agent is working, blending the base colour into its brighter twin.
//
// The geometry is measured from the shipped binary rather than guessed. Claude
// holds one integer position, advances it one column per 50ms, and sweeps it
// from ten columns before the text to ten columns past it — in its own terms,
// `sweepStart = min - 10`, `cycleLength = (max - min) + 20`, stepped by
// `Math.floor(elapsed / 50) % cycleLength`. Two colours are defined per
// shimmering element, never a ramp.
//
// What Claude Code's own values do not fix is the falloff *curve* between
// those two colours. A linear blend over the same ten columns the lead-in
// allows is the reading that makes the fixed constants consistent: it is
// exactly wide enough to be fully entering the text as the head arrives. If a
// side-by-side ever shows a sharper edge, this is the one number to try first.

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	// shimmerStep is how long the highlight spends on one column.
	shimmerStep = 50 * time.Millisecond

	// shimmerLead is how far past each end of the text the sweep travels, and
	// so also the half-width of the highlight itself.
	shimmerLead = 10
)

// sweepPos is where the highlight's head sits after elapsed, for a line of
// width display columns. It starts a lead before the text and wraps once it is
// a lead past the end, so the line is never mid-highlight when the cycle
// closes.
func sweepPos(elapsed time.Duration, width int) int {
	if width < 1 {
		return 0
	}
	cycle := width + 2*shimmerLead
	steps := int(elapsed / shimmerStep)
	return -shimmerLead + ((steps%cycle)+cycle)%cycle
}

// shimmer renders text with the highlight centred on pos, blending base into
// hi by distance. Columns a full lead away from the head are plain base, which
// is what makes a line whose head is off either end one flat colour.
//
// Runs of equal colour are rendered together rather than character by
// character: outside the highlight that is the whole line in one Render, and
// this sits in a draw loop that repeats twenty times a second.
func shimmer(text string, pos int, base, hi lipgloss.AdaptiveColor) string {
	if text == "" {
		return ""
	}
	baseHex, hiHex := resolve(base), resolve(hi)

	var b strings.Builder
	runs := []rune(text)
	start, cur := 0, blend(baseHex, hiHex, weight(0, pos))
	for i := 1; i <= len(runs); i++ {
		next := ""
		if i < len(runs) {
			next = blend(baseHex, hiHex, weight(i, pos))
		}
		if next == cur {
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(cur)).Render(string(runs[start:i])))
		start, cur = i, next
	}
	return b.String()
}

// weight is how much of the highlight colour column i takes, 0 outside the
// sweep and 1 directly under its head.
func weight(i, pos int) float64 {
	d := i - pos
	if d < 0 {
		d = -d
	}
	if d >= shimmerLead {
		return 0
	}
	return 1 - float64(d)/shimmerLead
}

// resolve picks the hex an AdaptiveColor means on this terminal. Doing it once
// per render rather than per column keeps the background probe - which lipgloss
// caches but still branches on - out of the inner loop.
func resolve(c lipgloss.AdaptiveColor) string {
	if lipgloss.HasDarkBackground() {
		return c.Dark
	}
	return c.Light
}

// blend mixes two #rrggbb colours, t of the way from a to b. A malformed hex
// returns a unchanged: this is a decoration in a draw loop, and the palette it
// reads is asserted well-formed by palette_test.go.
func blend(a, b string, t float64) string {
	if t <= 0 {
		return a
	}
	ar, ag, ab, ok := rgb(a)
	br, bg, bb, ok2 := rgb(b)
	if !ok || !ok2 {
		return a
	}
	mix := func(x, y int) int { return x + int(float64(y-x)*t) }
	return "#" + hex2(mix(ar, br)) + hex2(mix(ag, bg)) + hex2(mix(ab, bb))
}

// rgb splits a #rrggbb colour into its channels.
func rgb(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16), int(v>>8) & 0xff, int(v) & 0xff, true
}

// hex2 is one channel as two lowercase hex digits.
func hex2(v int) string {
	if v < 16 {
		return "0" + strconv.FormatInt(int64(v), 16)
	}
	return strconv.FormatInt(int64(v), 16)
}

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The shimmer blends two nearby oranges, so its assertions need the 24-bit
// profile specifically: at 4-bit the whole gradient quantises to one code and
// a shimmer that does nothing would pass. forceColour, which the appearance
// tests use, deliberately forces a *lower* profile than this.
func forceTrueColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// distinctForegrounds counts the different 24-bit foreground SGR sequences in s.
func distinctForegrounds(s string) int {
	seen := map[string]bool{}
	for _, m := range ansiPattern.FindAllString(s, -1) {
		if strings.Contains(m, "38;2;") {
			seen[m] = true
		}
	}
	return len(seen)
}

// The sweep is the half of the shimmer that can be checked exactly: Claude
// advances the highlight one column per 50ms across the text plus ten columns
// of lead-in and lead-out either side. Everything here is that arithmetic,
// which is also what makes the animation independent of the tick rate - the
// ticker only decides how often it is redrawn, never where the sweep is.
func TestSweepAdvancesOneColumnPerStep(t *testing.T) {
	const width = 30
	for _, tc := range []struct {
		elapsed time.Duration
		want    int
	}{
		{0, -shimmerLead},
		{shimmerStep, -shimmerLead + 1},
		{2 * shimmerStep, -shimmerLead + 2},
		{10 * shimmerStep, -shimmerLead + 10},
		// One step short of the cycle is the last distinct position.
		{time.Duration(width+2*shimmerLead-1) * shimmerStep, width + shimmerLead - 1},
	} {
		if got := sweepPos(tc.elapsed, width); got != tc.want {
			t.Errorf("sweepPos(%v, %d) = %d, want %d", tc.elapsed, width, got, tc.want)
		}
	}
}

func TestSweepCyclesBackToTheStart(t *testing.T) {
	const width = 30
	cycle := time.Duration(width+2*shimmerLead) * shimmerStep
	if got, want := sweepPos(cycle, width), sweepPos(0, width); got != want {
		t.Errorf("sweepPos wrapped to %d, want %d - the cycle must close", got, want)
	}
}

// A zero-width text has no cycle to divide by. It must not panic, and there is
// nothing to draw.
func TestSweepHandlesEmptyText(t *testing.T) {
	if got := sweepPos(time.Second, 0); got != 0 {
		t.Errorf("sweepPos over empty text = %d, want 0", got)
	}
	if got := shimmer("", 0, Accent, AccentShimmer); got != "" {
		t.Errorf("shimmer of empty text = %q, want empty", got)
	}
}

// Whatever the colours do, the words must survive: this line is the only thing
// on screen saying the agent is alive, and a shimmer that eats a character is
// worse than no shimmer.
func TestShimmerPreservesTheText(t *testing.T) {
	const text = "Calculating…"
	for _, pos := range []int{-shimmerLead, 0, 5, len([]rune(text)) + shimmerLead} {
		if got := stripANSI(shimmer(text, pos, Accent, AccentShimmer)); got != text {
			t.Errorf("shimmer at %d = %q, want %q", pos, got, text)
		}
	}
}

// With the head off the end of the line every column is the base colour, so a
// line whose turn is between sweeps is not a different colour from itself.
func TestShimmerAwayFromTheTextIsOneColour(t *testing.T) {
	forceTrueColour(t)
	out := shimmer("Calculating…", 400, Accent, AccentShimmer)
	if got := distinctForegrounds(out); got != 1 {
		t.Errorf("shimmer with the head far away uses %d colours, want 1", got)
	}
}

func TestShimmerUnderTheHeadIsAGradient(t *testing.T) {
	forceTrueColour(t)
	text := strings.Repeat("x", 40)
	out := shimmer(text, 20, Accent, AccentShimmer)
	if got := distinctForegrounds(out); got < 3 {
		t.Errorf("shimmer under the head uses %d colours, want a gradient of at least 3", got)
	}
}

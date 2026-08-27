package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The glyph cycle. Claude holds six frames and animates them forward *and
// back* - its own `[...frames, ...[...frames].reverse()]` - so the sweep of
// the asterisk grows and shrinks rather than snapping back to a dot. Twelve
// positions from six frames, one every 120ms.
func TestHeartbeatGlyphPingPongs(t *testing.T) {
	if len(heartbeatFrames) != 6 {
		t.Fatalf("heartbeatFrames has %d frames, want Claude's 6", len(heartbeatFrames))
	}
	first, last := heartbeatFrames[0], heartbeatFrames[len(heartbeatFrames)-1]

	if got := heartbeatGlyph(0); got != first {
		t.Errorf("glyph at 0 = %q, want %q", got, first)
	}
	// The turn: frame 5 and frame 6 are both the last frame, which is what
	// makes it a ping-pong rather than a cycle with a stutter.
	if got, want := heartbeatGlyph(5*glyphStep), last; got != want {
		t.Errorf("glyph at the turn = %q, want %q", got, want)
	}
	if got, want := heartbeatGlyph(6*glyphStep), last; got != want {
		t.Errorf("glyph just past the turn = %q, want %q (the reversal repeats it)", got, want)
	}
	if got := heartbeatGlyph(11 * glyphStep); got != first {
		t.Errorf("glyph at the last position = %q, want %q", got, first)
	}
	if got, want := heartbeatGlyph(12*glyphStep), heartbeatGlyph(0); got != want {
		t.Errorf("glyph cycle did not close: %q vs %q", got, want)
	}
}

func TestHeartbeatGlyphAdvancesEvery120ms(t *testing.T) {
	if glyphStep != 120*time.Millisecond {
		t.Errorf("glyphStep = %v, want Claude's 120ms", glyphStep)
	}
	if a, b := heartbeatGlyph(0), heartbeatGlyph(glyphStep-time.Millisecond); a != b {
		t.Errorf("glyph changed inside one step: %q then %q", a, b)
	}
	if a, b := heartbeatGlyph(0), heartbeatGlyph(glyphStep); a == b {
		t.Error("glyph did not change after a full step")
	}
}

// The word is chosen once and held: it names the turn, and a word that changed
// every frame would be a slot machine rather than a status line.
func TestHeartbeatWordIsStableForASeed(t *testing.T) {
	for _, seed := range []uint64{0, 1, 7919, 1 << 40} {
		first := heartbeatWord(seed)
		if again := heartbeatWord(seed); again != first {
			t.Errorf("seed %d gave %q then %q", seed, first, again)
		}
		if first == "" {
			t.Errorf("seed %d gave an empty word", seed)
		}
	}
	if a, b := heartbeatWord(0), heartbeatWord(1); a == b {
		t.Errorf("two seeds gave the same word %q; the pool is not being indexed", a)
	}
}

// The pool's shape, which is the whole of what the code depends on. Its
// *contents* are Wake's own and no test can hold them to a source - the point
// of the list is that there is no source. What can be held is the form: enough
// words that a thirty-agent fleet does not visibly repeat, each one distinct,
// and each one a gerund, because a word that is not is a word that reads as a
// bug in the line rather than a joke in it.
func TestHeartbeatPoolIsWellFormed(t *testing.T) {
	// minHeartbeatWords is the repetition floor. Thirty agents drawing a word
	// each is thirty draws from the pool at any moment; below this the same
	// word is on screen twice often enough to look broken.
	const minHeartbeatWords = 180

	if len(heartbeatWords) < minHeartbeatWords {
		t.Errorf("pool has %d words, want at least %d", len(heartbeatWords), minHeartbeatWords)
	}

	seen := make(map[string]bool, len(heartbeatWords))
	for i, w := range heartbeatWords {
		switch {
		case w == "":
			t.Errorf("word %d is empty", i)
		case seen[w]:
			t.Errorf("word %d %q is a duplicate; it costs a slot and doubles its own odds", i, w)
		case !strings.HasSuffix(w, "ing"):
			t.Errorf("word %d %q is not a gerund", i, w)
		case strings.TrimSpace(w) != w:
			t.Errorf("word %d %q has surrounding space", i, w)
		case strings.ToUpper(w[:1]) != w[:1]:
			t.Errorf("word %d %q is not capitalised", i, w)
		}
		seen[w] = true
	}
}

func TestElapsedReadsLikeClaudes(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{51 * time.Second, "51s"},
		{time.Minute, "1m 0s"},
		{111 * time.Second, "1m 51s"},
		{18*time.Minute + 19*time.Second, "18m 19s"},
		{time.Hour + 2*time.Minute, "1h 2m"},
	} {
		if got := elapsedText(tc.in); got != tc.want {
			t.Errorf("elapsedText(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// What the line actually says: a glyph, the turn's word, and how long it has
// been going. The parenthesised half is Claude's, and so is the ellipsis.
func TestHeartbeatLineNamesTheWorkAndItsAge(t *testing.T) {
	line := stripANSI(heartbeatLine(heartbeatWord(0), 111*time.Second, 0, 80))

	for _, want := range []string{heartbeatWord(0), "…", "(1m 51s)"} {
		if !strings.Contains(line, want) {
			t.Errorf("heartbeat line %q is missing %q", line, want)
		}
	}
	if want := heartbeatGlyph(111*time.Second) + " "; !strings.HasPrefix(line, want) {
		t.Errorf("heartbeat line %q does not open with %q, the glyph for its own moment", line, want)
	}
}

// The line sits in a pane, so it is bounded like everything else in one - and
// the shimmer must not be what pushes it over.
func TestHeartbeatLineIsBounded(t *testing.T) {
	forceTrueColour(t)
	for _, width := range []int{10, 20, 40, 80} {
		got := heartbeatLine("Metamorphosing", 3661*time.Second, 11_600, width)
		if w := ansi.StringWidth(got); w > width {
			t.Errorf("width %d: heartbeat line is %d cells: %q", width, w, stripANSI(got))
		}
	}
}

// The whole point of the thing: it has to move. Two different moments in the
// same turn must not render identically.
func TestHeartbeatLineAnimates(t *testing.T) {
	forceTrueColour(t)
	const word = "Calculating"
	first := heartbeatLine(word, time.Second, 0, 60)
	// One shimmer step is too short to move the glyph, so this isolates the sweep.
	if later := heartbeatLine(word, time.Second+shimmerStep, 0, 60); later == first {
		t.Error("the shimmer did not move one step later")
	}
	if a, b := heartbeatGlyph(time.Second), heartbeatGlyph(time.Second+glyphStep); a == b {
		t.Errorf("the glyph did not change a step later: %q then %q", a, b)
	}
}

func TestTokensReadLikeClaudes(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{
		{0, ""},  // no turn has ended yet
		{-5, ""}, // never a negative claim
		{847, " · ↓ 847 tokens"},
		{11_600, " · ↓ 11.6k tokens"},
		{1_000, " · ↓ 1.0k tokens"},
		{2_400_000, " · ↓ 2.4M tokens"},
	} {
		if got := tokenText(tc.in); got != tc.want {
			t.Errorf("tokenText(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Both clauses in one parenthesis, in Claude's order.
func TestHeartbeatLineCarriesTheTokenCount(t *testing.T) {
	line := stripANSI(heartbeatLine("Calculating", 111*time.Second, 11_600, 80))
	if want := "(1m 51s · ↓ 11.6k tokens)"; !strings.Contains(line, want) {
		t.Errorf("heartbeat line %q is missing %q", line, want)
	}
}

// A session that has not finished a turn has no count to show, and shows none
// rather than a zero.
func TestHeartbeatLineOmitsAnUncountedTurn(t *testing.T) {
	line := stripANSI(heartbeatLine("Calculating", 5*time.Second, 0, 80))
	if strings.Contains(line, "tokens") {
		t.Errorf("heartbeat line %q claimed a token count before any turn ended", line)
	}
	if want := "(5s)"; !strings.Contains(line, want) {
		t.Errorf("heartbeat line %q is missing %q", line, want)
	}
}

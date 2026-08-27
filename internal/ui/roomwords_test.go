package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The pool's shape. Its contents are Wake's own and nautical or dawn to a word,
// which no test can hold them to - the point of the list is that there is no
// source. What can be held is the form: enough words that the room does not
// repeat itself from one turn to the next, each distinct, each past tense
// (which here means: not a gerund - a word ending in "ing" is the gerund pool
// leaking in), and each capitalised.
func TestRoomWordPoolIsWellFormed(t *testing.T) {
	// minRoomWords is variety across turns rather than an on-screen collision
	// floor: the room names one working agent at a time, so unlike the per-agent
	// gerund pool it never shows two words at once.
	const minRoomWords = 40

	if len(roomWorkingWords) < minRoomWords {
		t.Errorf("pool has %d words, want at least %d", len(roomWorkingWords), minRoomWords)
	}

	seen := make(map[string]bool, len(roomWorkingWords))
	for i, w := range roomWorkingWords {
		switch {
		case w == "":
			t.Errorf("word %d is empty", i)
		case seen[w]:
			t.Errorf("word %d %q is a duplicate; it costs a slot and doubles its own odds", i, w)
		case strings.HasSuffix(w, "ing"):
			t.Errorf("word %d %q is a gerund; the room's pool is past tense", i, w)
		case strings.TrimSpace(w) != w:
			t.Errorf("word %d %q has surrounding space", i, w)
		case strings.ToUpper(w[:1]) != w[:1]:
			t.Errorf("word %d %q is not capitalised", i, w)
		}
		seen[w] = true
	}
}

// The word is chosen once and held: it names the turn, not the frame.
func TestRoomWorkingWordIsStableForATurn(t *testing.T) {
	started := clock()
	first := roomWorkingWord("s1", "", started)
	if again := roomWorkingWord("s1", "", started); again != first {
		t.Errorf("the word changed within one turn: %q then %q", first, again)
	}
	if first == "" {
		t.Error("the word is empty")
	}
}

// It indexes the pool rather than returning a constant: across a spread of ids
// more than one word appears.
func TestRoomWorkingWordDrawsFromThePool(t *testing.T) {
	started := clock()
	seen := map[string]bool{}
	for r := 'a'; r <= 'z'; r++ {
		seen[roomWorkingWord("s"+string(r), "", started)] = true
	}
	if len(seen) < 2 {
		t.Errorf("26 ids gave %d distinct words; the pool is not being indexed", len(seen))
	}
}

// An agent that wrote its own activeForm shows it - the pool is the fallback for
// the ordinary agent that keeps no task list, exactly as the DM's line does.
func TestRoomWorkingWordPrefersTheAgentsOwnActiveForm(t *testing.T) {
	if got := roomWorkingWord("s1", "Counting lines", clock()); got != "Counting lines" {
		t.Errorf("the room drew %q, want the agent's own %q", got, "Counting lines")
	}
}

// The minimal line: a glyph, the turn's word, and "for 51s" - and none of the
// DM line's chrome. No trailing ellipsis, no parenthesised clause, no tokens.
func TestRoomHeartbeatLineIsMinimal(t *testing.T) {
	line := stripANSI(roomHeartbeatLine("Sailed", 51*time.Second, 80))

	for _, want := range []string{"Sailed", "for 51s"} {
		if !strings.Contains(line, want) {
			t.Errorf("the room's line %q is missing %q", line, want)
		}
	}
	for _, unwanted := range []string{"…", "(", "tokens", "↓"} {
		if strings.Contains(line, unwanted) {
			t.Errorf("the room's line %q carries %q, which is the DM line's chrome", line, unwanted)
		}
	}
	if want := heartbeatGlyph(51*time.Second) + " "; !strings.HasPrefix(line, want) {
		t.Errorf("the room's line %q does not open with %q, the glyph for its own moment", line, want)
	}
}

// It sits in a pane, so it is bounded like everything else - the shimmer must
// not be what pushes it over.
func TestRoomHeartbeatLineIsBounded(t *testing.T) {
	forceTrueColour(t)
	for _, width := range []int{10, 20, 40, 80} {
		got := roomHeartbeatLine("Rekindled", 3661*time.Second, width)
		if w := ansi.StringWidth(got); w > width {
			t.Errorf("width %d: the room's line is %d cells: %q", width, w, stripANSI(got))
		}
	}
}

// It moves: two moments in the same turn must not render identically.
func TestRoomHeartbeatLineAnimates(t *testing.T) {
	forceTrueColour(t)
	const word = "Fathomed"
	first := roomHeartbeatLine(word, time.Second, 60)
	if later := roomHeartbeatLine(word, time.Second+shimmerStep, 60); later == first {
		t.Error("the shimmer did not move one step later")
	}
}

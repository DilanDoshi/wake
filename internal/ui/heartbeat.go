package ui

// The working line: Claude Code's `✻ Calculating… (1m 51s · ↓ 11.6k tokens)`,
// one per agent with a turn in flight.
//
// The measurable parts match Claude's - the six frames and their reversal, the
// 120ms step, the shimmer geometry in shimmer.go. The words are not: the pool
// in heartbeatwords.go is Wake's own, and that file argues why.
//
// The token count is a session total that steps up at each turn end, where
// Claude's climbs through the turn; tokenText argues why. Effort is the one
// clause still missing and it is not a decode: no frame reports the level, so
// showing it means Wake tracking what it set - see statusbar.go.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	// glyphStep is how long one frame of the asterisk lasts.
	glyphStep = 120 * time.Millisecond

	// heartbeatEllipsis trails the word, and is Claude's own.
	heartbeatEllipsis = "…"

	// metaSep divides the clauses inside the parentheses, and tokenArrow marks
	// the count as what came back. Both are Claude's.
	metaSep    = " · "
	tokenArrow = "↓"
)

// heartbeatFrames is Claude's spinner, in its order. It is animated forward
// then back - see heartbeatGlyph - so the asterisk opens and closes rather
// than snapping from full back to a dot.
//
// Claude ships a second set for Ghostty, whose last frame is ✻ rather than ✽.
// Wake draws the default: the substitution is a font workaround for one
// terminal, and Wake has nothing that knows which terminal it is in.
var heartbeatFrames = []string{"·", "✢", "✳", "✶", "✻", "✽"}

// heartbeatGlyph is the frame showing at elapsed. The sequence is the frames
// followed by their reverse, so the two ends each show for two steps and the
// motion reads as a pulse.
func heartbeatGlyph(elapsed time.Duration) string {
	n := len(heartbeatFrames)
	i := int(elapsed/glyphStep) % (2 * n)
	if i < 0 {
		i += 2 * n
	}
	if i >= n {
		i = 2*n - 1 - i
	}
	return heartbeatFrames[i]
}

// heartbeatWord is the word an agent shows for one turn. Callers pass a seed
// that is constant for the turn, so the word is chosen once and held.
func heartbeatWord(seed uint64) string {
	return heartbeatWords[seed%uint64(len(heartbeatWords))]
}

// heartbeatLine draws the whole line, bounded to width. elapsed is the age of
// the turn, and drives all three of the glyph, the shimmer and the text - they
// are one clock at three rates, so the line cannot say one thing and animate
// another.
//
// The parenthesised half does not shimmer. It is chrome about the turn rather
// than the signal that the turn is alive, and Claude draws it in `inactive`.
func heartbeatLine(word string, elapsed time.Duration, tokens, width int) string {
	head := heartbeatGlyph(elapsed) + " " + word + heartbeatEllipsis
	meta := fmt.Sprintf(" (%s%s)", elapsedText(elapsed), tokenText(tokens))
	return boundedShimmerLine(head, elapsed, meta, width)
}

// boundedShimmerLine draws a working line bounded to width: the head shimmers as
// the signal that the turn is alive, and meta is dim chrome dropped whole rather
// than cut when it will not fit. The DM's heartbeatLine and the room's
// roomHeartbeatLine build their own head and meta and share this body.
func boundedShimmerLine(head string, elapsed time.Duration, meta string, width int) string {
	if width < 1 {
		return ""
	}
	// The head is cut before the tail: if only one of them fits, the word is
	// what says an agent is working and the age is what says for how long.
	head = ansi.Truncate(head, width, ellipsis)
	if room := width - ansi.StringWidth(head); room < ansi.StringWidth(meta) {
		meta = ""
	}
	lit := shimmer(head, sweepPos(elapsed, ansi.StringWidth(head)), Accent, AccentShimmer)
	return lit + HintStyle.Render(meta)
}

// tokenText is the token clause of the meta, and "" for a turn that has not
// been told what it has produced yet.
//
// **This is the turn in flight, and it climbs.** It used to be a session total
// stepping up at turn end, and this comment used to explain why that was the
// best available: Wake did not pass --include-partial-messages, so the only
// output count on the wire was the one on the result frame that *ends* a turn.
// Both halves changed. PR #37 passes the flag for the streamed preview, and the
// airlock now reads message_delta's usage - once per message rather than once
// per token, so the cost the old argument was about is not the cost this has.
// See core.KindTurnTokens and ui.Agent.TurnTokens.
//
// The caller passes that figure; this only renders it. **Do not "restore" it to
// Agent.Tokens** - that is every completed turn, which during turn N is turns
// 1…N−1, and it is the bug the field beside it exists to close.
func tokenText(tokens int) string {
	if tokens <= 0 {
		return ""
	}
	return metaSep + tokenArrow + " " + humanTokens(tokens) + " tokens"
}

// humanTokens abbreviates the way Claude does: 11600 reads 11.6k.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return strconv.Itoa(n)
	}
}

// elapsedText is a duration the way Claude writes one: seconds alone under a
// minute, minutes and seconds under an hour, hours and minutes past it.
func elapsedText(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// turnSeed is the number heartbeatWord is indexed by: stable for one turn of
// one agent, and different for the next. The id keeps two agents working at
// the same moment from showing the same word.
func turnSeed(id string, started time.Time) uint64 {
	var h uint64 = 14695981039346656037 // FNV-1a offset basis
	for _, b := range append([]byte(id), []byte(strings.TrimSpace(started.Format(time.RFC3339Nano)))...) {
		h = (h ^ uint64(b)) * 1099511628211
	}
	return h
}

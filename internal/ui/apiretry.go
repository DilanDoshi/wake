package ui

// Which sessions claude is retrying a request for, and what the last retry said.
//
// claude retries a request the API does not answer ten times over about three
// minutes before it fails the turn (api-error-unreachable.jsonl), and for all of
// that the agent reads as working: the heartbeat kept a pool word over a ticking
// timer and nothing said the connection was gone (BUG-60). Each retry is a
// NoticeAPIRetrying carrying its count and the pause before the next try, so the
// working line can say so instead. Held on App and folded like compacting.go's
// map, for its reason; read at draw time through WithRetrying, WithWorking and
// the roster's WithRetrying. Redrawn on the heartbeat ticker a working agent
// already runs - the countdown reads the clock, nothing is scheduled.

import (
	"fmt"
	"math"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// retryGlyph heads the line and stands in for a retrying row's glyph.
	retryGlyph = "⚠"

	// unreachableCause is a retry the API answered with no status: the
	// connection itself failed. It matches the pin that follows a give-up
	// (unreachableFormat), so the two read as one story. statusCauseFormat is
	// one it answered - an overload's 529 - named plainly rather than guessed at.
	unreachableCause  = "API unreachable"
	statusCauseFormat = "API error %d"

	// retryingWord is the try under way; retryInFormat the pause still to run.
	retryingWord  = "retrying"
	retryInFormat = "retrying in %s"

	// attemptFormat is the conversation's count, roomCountFormat the room's.
	attemptFormat   = "attempt %d of %d"
	roomCountFormat = "retrying %d/%d"
)

// apiRetry is one session's standing retry: what its last retry frame said, and
// when that frame arrived - which is when the pause began.
type apiRetry struct {
	attempt, max, status int
	delay                time.Duration
	since                time.Time
}

// active is whether a retry stands; the zero value is none.
func (r apiRetry) active() bool { return !r.since.IsZero() }

// cause words what the API did: nothing, or a status.
func (r apiRetry) cause() string {
	if r.status == 0 {
		return unreachableCause
	}
	return fmt.Sprintf(statusCauseFormat, r.status)
}

// head is the warning: the cause, and the pause left before the next try, or
// that the try is under way once it has run out.
func (r apiRetry) head(now time.Time) string {
	if left := r.since.Add(r.delay).Sub(now); left > 0 {
		return retryGlyph + " " + r.cause() + metaSep + fmt.Sprintf(retryInFormat, countdownText(left))
	}
	return retryGlyph + " " + r.cause() + metaSep + retryingWord + heartbeatEllipsis
}

// countdownText rounds a pause up to the second, so its last fraction reads 1s
// and never 0s with the try not yet begun.
func countdownText(d time.Duration) string {
	return elapsedText(time.Duration(math.Ceil(d.Seconds())) * time.Second)
}

// retryLine is the conversation's line, in the working line's row:
// `⚠ API unreachable · retrying in 38s (attempt 3 of 10 · 1m 12s)`.
func retryLine(r apiRetry, elapsed time.Duration, width int) string {
	meta := fmt.Sprintf(" (%s%s%s)", fmt.Sprintf(attemptFormat, r.attempt, r.max), metaSep, elapsedText(elapsed))
	return boundedWarnLine(r.head(clock()), meta, width)
}

// roomRetryLine is the room's, in roomHeartbeatLine's shape:
// `⚠ API unreachable · retrying 3/10 for 1m 12s`.
func roomRetryLine(r apiRetry, elapsed time.Duration, width int) string {
	head := retryGlyph + " " + r.cause() + metaSep + fmt.Sprintf(roomCountFormat, r.attempt, r.max)
	return boundedWarnLine(head, roomFor+elapsedText(elapsed), width)
}

// boundedWarnLine is boundedShimmerLine's budget without its shimmer - a
// warning, not a pulse: the head is cut before the meta is dropped whole.
func boundedWarnLine(head, meta string, width int) string {
	if width < 1 {
		return ""
	}
	head = ansi.Truncate(head, width, ellipsis)
	if room := width - ansi.StringWidth(head); room < ansi.StringWidth(meta) {
		meta = ""
	}
	return warnStyle.Render(head) + HintStyle.Render(meta)
}

// observeRetry folds one event: a retry notice replaces the session's standing
// retry, and proof the request is over clears it. Immutable: the map is copied.
func (a App) observeRetry(sessionID string, ev core.Event) App {
	if ev.Kind == core.KindSystem && ev.Notice == core.NoticeAPIRetrying && ev.Retry != nil {
		a.retrying = withRetry(a.retrying, sessionID, apiRetry{
			attempt: ev.Retry.Attempt, max: ev.Retry.Max, status: ev.Retry.Status, delay: ev.Retry.Delay, since: clock(),
		})
		return a
	}
	if requestOver(ev) {
		a.retrying = withoutRetry(a.retrying, sessionID)
	}
	return a
}

// requestOver is whether an event proves the retried request is done with: the
// API answered - a streamed start or token, prose, a tool call, thinking - or the
// turn ended, failed for good (the unreachable pin takes over), or the
// conversation was cleared. A receipt, a queued message's lifecycle, a replayed
// line or another system frame says nothing about the API.
func requestOver(ev core.Event) bool {
	switch ev.Kind {
	case core.KindMessageStart, core.KindPartialText, core.KindTurnTokens, core.KindTurnEnd, core.KindAPIError, core.KindSessionReset:
		return true
	}
	return ev.ModelOutput()
}

// pruneRetrying drops the retry of any session a report shows not working: a
// park, an ending, or an idle whose turn end a gap ate. Immutable, a no-op when
// nothing needs it.
func (a App) pruneRetrying() App {
	for id := range a.retrying {
		if agent, ok := a.fleet.Agent(id); !ok || agent.State != rpc.StateWorking {
			a.retrying = withoutRetry(a.retrying, id)
		}
	}
	return a
}

// retryOf is a session's standing retry, or the zero value for none - what the
// conversation pane draws its line from.
func (a App) retryOf(id string) apiRetry { return a.retrying[id] }

func withRetry(m map[string]apiRetry, id string, r apiRetry) map[string]apiRetry {
	next := make(map[string]apiRetry, len(m)+1)
	for k, v := range m {
		next[k] = v
	}
	next[id] = r
	return next
}

func withoutRetry(m map[string]apiRetry, id string) map[string]apiRetry {
	if _, ok := m[id]; !ok {
		return m
	}
	next := make(map[string]apiRetry, len(m))
	for k, v := range m {
		if k != id {
			next[k] = v
		}
	}
	return next
}

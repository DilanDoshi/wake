package ui

// The rate-limit warning is a timed pop-up above the composer, not a line in
// the scrollback.
//
// Claude emits a rate_limit_event once per process on the first turn that hits
// the API; a warning or exhausted status (core.NoticeRateLimited) is the one
// worth surfacing, and it is a fact about *now* - "you are close to your usage
// limit" - not a thing that happened in the conversation. So it goes to the
// notice row internal/notice already owns and clears itself after the linger
// every notice gets (noticelinger.go), rather than standing forever in the
// transcript like a failure does.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// rateLimitPrefix heads the warning. The status string is Claude's own value
// (not a wire word - see CLAUDE.md's note on Event.Text), so it is passed
// through beside it when there is one. "approaching" fits the only non-benign
// status the corpus records ("allowed_warning"); a future "exhausted" status
// would read oddly and is the note that revisits this wording.
const rateLimitPrefix = "usage limit approaching"

// rateLimited is the whole of what a rate-limit event does now. A warning pops
// the timed notice; a benign heartbeat pops nothing. Either way the event never
// reaches the room, a DM or the fleet - it is a fact about quota, not
// conversation content, so observe routes it here instead of appending it.
func (a App) rateLimited(ev core.Event) App {
	if ev.Notice != core.NoticeRateLimited {
		return a
	}
	text := rateLimitPrefix
	if ev.Text != "" {
		text += " · " + ev.Text
	}
	notice.Report("%s", text)
	return a
}

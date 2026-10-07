package core

// Notice is an event a reader should be told about, named in Wake's
// vocabulary rather than Claude's.
//
// This is the narrowed half of the airlock ruling, and the narrowing is the
// point. KindSystem's Text is a deliberate passthrough - the subtype set is
// open, so an unmodelled subtype must still arrive as a system event rather
// than degrade - but a *presentation* allowlist keyed on those raw subtypes
// is a category, not an enumeration: it grows by one map entry per subtype
// with no review, and the subagent corpus alone lands 56 more of them.
//
// So the decoder keeps the passthrough (Text is still the raw subtype) and
// the *label* is resolved here, into a closed set of Wake's own words. A
// renderer maps Notice values to glyphs and English; it never sees a wire
// subtype. Showing a new one therefore costs a constant in this file and a
// review of it, which is the price the ruling's bottom two rows were missing.
type Notice string

const (
	// NoticeContextCompacted is the conversation being summarised to fit. It is
	// the compact_boundary that lands *after* a successful compaction, and it is
	// the transcript label - distinct from the two below, which bracket the work
	// while it runs so a reader can draw a "compacting…" status line.
	NoticeContextCompacted Notice = "context_compacted"

	// NoticeCompacting and NoticeCompacted bracket a compaction. Both ride a
	// system/status frame - the start carries status:"compacting", the end
	// carries a compact_result - so the pair is resolved from the payload rather
	// than the shared subtype. The end keys on compact_result and *not* on the
	// boundary above, because a failed compaction emits a compact_result and no
	// boundary at all (slash-commands.jsonl). See systemEvent.
	NoticeCompacting Notice = "context_compacting"
	NoticeCompacted  Notice = "context_compacted_done"

	// NoticeToolDenied is the after-the-fact report that a tool call was
	// refused - not the ask, which is KindPermissionRequest.
	NoticeToolDenied Notice = "tool_denied"

	// NoticeRateLimited is quota exhaustion, and it is resolved only for a
	// status that is *not* the benign one: the single value every recorded
	// sample carries means nothing is wrong, and drawing it is chrome.
	// Event.Text still carries the status itself.
	NoticeRateLimited Notice = "rate_limited"

	// NoticeAPIError rides a KindAPIError for a login the API refused, so the UI
	// raises a pop-up rather than a transcript line and marks the session for a
	// new process. Text is the API message.
	NoticeAPIError Notice = "api_error"

	// NoticeTurnFailed rides a KindAPIError of any other named kind - an
	// overload, a rejected request. It fails one turn and the next send retries,
	// so the UI tells it and neither marks nor parks.
	NoticeTurnFailed Notice = "turn_failed"

	// NoticeUsageLimit rides a KindAPIError that is a session or weekly usage
	// limit. Unlike every other failed turn it recovers on its own when the quota
	// resets, so it neither parks the agent nor asks for /reauth.
	NoticeUsageLimit Notice = "usage_limit"

	// NoticeTurnInterrupted is Claude's own account of a turn Wake aborted.
	//
	// It is the one notice resolved from a frame's *content* rather than from
	// a subtype, because that is the only place the fact appears: the marker
	// arrives as an ordinary user frame carrying neither isReplay nor
	// isSynthetic, with the same key set a genuine user turn has. Nothing else
	// on it says the human did not type it, so a view that trusts the frame
	// draws Claude's abort notice under the operator's own name - and does it
	// on every interrupt, which is about to be the most common thing anyone
	// does here.
	NoticeTurnInterrupted Notice = "turn_interrupted"

	// NoticeQuestionAnswered and NoticeQuestionCancelled are the room's record
	// that the operator resolved an agent's question - answered, or refused.
	//
	// Unlike every notice above they are authored above the airlock (internal/ui
	// when it settles a question card), never decoded from a frame - the way
	// Event.FromRoom is set by the App rather than the decoder. So the group chat
	// shows the ask closing rather than its own warn line going stale. See
	// internal/ui/cardroom.go.
	NoticeQuestionAnswered  Notice = "question_answered"
	NoticeQuestionCancelled Notice = "question_cancelled"
)

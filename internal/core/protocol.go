// AIRLOCK. Wake's knowledge of Claude Code's stream-json wire format lives in
// four files in this package and nowhere else in the tree. Everything above
// them consumes core.Event.
//
//	protocol.go    decoding - one wire line in, core.Events out (this file)
//	wire.go        the shapes it decodes into
//	vocabulary.go  Claude's words resolved into Wake's
//	encode.go      the frames Wake writes back
//
// It was one file until it reached 1031 lines against this project's 800-line
// hard max. docs/notes/decisions.md ruled ahead of time what to do when that
// happened: restate the rule to name the airlock *files*, and restate it
// **before** the change that overflows it lands rather than during. This is
// that restatement. The boundary has not moved - the same declarations sit
// behind the same rule - and internal/core/airlock_test.go enforces it over
// the whole tree from the same list, so the set cannot quietly grow a fifth
// member either.
//
// The split is by direction and by job rather than by size, so a port has
// four reviewable units instead of one unreadable one: what arrives, what it
// becomes, what Wake calls it, and what Wake sends.
//
// Every *inbound* shape is transcribed from testdata/stream/*.jsonl, recorded
// from live sessions in Task 1 - not from documentation. The outbound shapes
// in encode.go are the one exception, and say so where they appear: Wake
// writes them and never reads them, so no recording of stdout can contain
// them.
package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// DecodeLine converts one line of Claude stream-json into zero or more Wake
// events. One assistant frame can carry several content blocks, so the
// return is a slice.
//
// An unrecognized frame type yields KindUnknown carrying the wire type
// rather than an error: a future Claude release must never crash the render
// loop. Only malformed JSON is an error.
//
// **Every event leaves here contained**: a child's strings have had the
// characters a terminal acts on replaced, because this is where its bytes first
// become a Wake value and DecodeTranscriptLine comes through here too. See
// contain.go for what that costs and what it deliberately leaves alone. It
// wraps the decode rather than sitting in each arm so a frame type added later
// cannot arrive uncontained.
func DecodeLine(line []byte) ([]Event, error) {
	events, err := decodeLine(line)
	if err != nil {
		return nil, err
	}
	for i, ev := range events {
		events[i] = ev.contained()
	}
	return events, nil
}

// decodeLine is the decode itself; DecodeLine is it plus the fence.
func decodeLine(line []byte) ([]Event, error) {
	var f wireFrame
	if err := json.Unmarshal(line, &f); err != nil {
		return nil, fmt.Errorf("decode stream-json line: %w", err)
	}
	raw := json.RawMessage(append([]byte(nil), line...))

	switch f.Type {
	case "assistant", "user":
		return messageEvents(f, raw), nil
	case "system":
		return one(systemEvent(f, raw)), nil
	case "result":
		return one(Event{Kind: KindTurnEnd, SessionID: f.SessionID, Text: f.Result, Raw: raw, Session: resultFacts(f)}), nil
	case "control_request":
		// Note f.Request, not f.Subtype: see wireFrame.RequestID.
		return one(controlRequestEvent(f, raw)), nil
	case "control_response":
		return one(controlResponseEvent(f, raw)), nil
	case "control_cancel_request":
		// The ask this names is dead. RequestID is the entire frame - see
		// KindRequestWithdrawn for what may and may not be read into it.
		return one(Event{Kind: KindRequestWithdrawn, SessionID: f.SessionID, RequestID: f.RequestID, Raw: raw}), nil
	case "command_lifecycle":
		return one(messageStateEvent(f, raw)), nil
	case "rate_limit_event":
		return one(rateLimitEvent(f, raw)), nil
	case "stream_event":
		// Deliberately not given `raw`: see partialEvent.
		return partialEvent(f), nil
	case "conversation_reset":
		// SessionID is the id that just died, and it is the whole payload:
		// the successor is not on this frame, it arrives on the next one.
		return one(Event{Kind: KindSessionReset, SessionID: f.SessionID, Raw: raw}), nil
	default:
		return one(Event{Kind: KindUnknown, SessionID: f.SessionID, Text: f.Type, Raw: raw}), nil
	}
}

// partialEvent is the tokens of an assistant block that is still being
// written, and nothing for every other shape a stream_event carries.
//
// Returning no events is the ordinary outcome here rather than a degradation:
// six of the seven streaming types describe a block that arrives complete on
// the assistant frame behind it, and an event for one would be a second
// account of the same words. KindUnknown is wrong for the same reason - these
// are frames Wake models and declines, not frames it failed to read.
//
// **It carries no Raw**, alone among everything this file decodes. Raw pins
// the whole line for the life of the event, and a partial is one token: at the
// corpus's median 43.5 output tokens a second across thirty agents that is
// ~1,300 lines a second held alive to show a preview that is replaced within
// the turn. Nothing may read it either - a partial is dropped, never stored.
func partialEvent(f wireFrame) []Event {
	if f.StreamEvent == nil {
		return nil
	}
	// A subagent's tokens are dropped rather than attributed, for the reason
	// spelled out below - checked once, ahead of both branches, because it is
	// the same ruling about both.
	if f.ParentToolUseID != "" {
		return nil
	}
	switch f.StreamEvent.Type {
	case streamMessageDelta:
		return turnTokensEvent(f)
	case streamMessageStart:
		// The boundary and nothing else - see KindMessageStart for why its own
		// usage is left alone.
		return one(Event{Kind: KindMessageStart, SessionID: f.SessionID})
	}
	if f.StreamEvent.Type != streamContentBlockDelta {
		return nil
	}
	d := f.StreamEvent.Delta
	if d == nil || d.Type != streamTextDelta || d.Text == "" {
		return nil
	}
	return one(Event{Kind: KindPartialText, SessionID: f.SessionID, Text: d.Text})
}

// turnTokensEvent is what one message of a turn produced, and nothing else.
//
// A zero is no event rather than a zero figure: a message that produced nothing
// says nothing about the turn, and a consumer holding "0" cannot tell it from
// "not said yet" - which is the distinction every count on this wire is
// careful about.
//
// The dropped subagent case is partialEvent's, checked before this is reached:
// a subagent's turn is not the one on screen, exactly as its text and its tool
// calls are not.
func turnTokensEvent(f wireFrame) []Event {
	u := f.StreamEvent.Usage
	if u == nil || u.OutputTokens <= 0 {
		return nil
	}
	return one(Event{
		Kind:      KindTurnTokens,
		SessionID: f.SessionID,
		Session:   &SessionFacts{TurnOutputTokens: u.OutputTokens},
	})
}

// DecodeTranscriptLine decodes one line of claude's **on-disk** transcript.
//
// That file is a different format from the stream: camelCase keys, a dozen
// fields stdout never carries, and record types (`custom-title`,
// `queue-operation`, `attachment`) that exist only on disk. But the two agree
// on the part Wake reads - `type`, and the Anthropic `message` under it - so
// this is a filter in front of DecodeLine rather than a second decoder. A
// second decoder is the parallel implementation this package exists to prevent,
// and it would drift on exactly the block shapes that are hardest to get right.
//
// It reads no session id. The on-disk key is `sessionId` where the stream's is
// `session_id`, and the caller knows which session's file it opened - stamping
// it there is one fact in one place rather than two spellings in the airlock.
//
// A sidechain line is a subagent's, and it is dropped: the conversation being
// restored is the one the operator was having.
// It reads one on-disk-only key: `timestamp`. That is the exception to the
// paragraph above and it has to be, because the caller cannot supply what only
// the line knows - and the room needs it, being a fold over several transcripts
// that has to interleave them. A record with no readable time keeps its turn and
// loses only the stamp; see Event.At.
func DecodeTranscriptLine(line []byte) ([]Event, error) {
	var f struct {
		Type      string `json:"type"`
		Sidechain bool   `json:"isSidechain"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(line, &f); err != nil {
		return nil, fmt.Errorf("decode transcript line: %w", err)
	}
	if f.Sidechain || (f.Type != "assistant" && f.Type != "user") {
		return nil, nil
	}
	events, err := DecodeLine(line)
	if err != nil {
		return events, err
	}
	at, tErr := time.Parse(time.RFC3339, f.Timestamp)
	if tErr != nil {
		return events, nil
	}
	for i := range events {
		events[i].At = at
	}
	return events, nil
}

// TranscriptNode is one on-disk transcript line's place in the tree - its own
// identity and its parent's - or, for a last-prompt line, the rewind marker
// it carries instead. It holds no message content; DecodeTranscriptLine
// still reads that for the lines the active-branch walk in daemon/history.go
// keeps.
type TranscriptNode struct {
	UUID, ParentUUID string
	Kind             string // "user" | "assistant" | "last-prompt" | other
	Rewound          bool   // true only on a last-prompt rewind marker
	LeafUUID         string // the active leaf, on a last-prompt marker
}

// DecodeTranscriptNode reads only the tree structure of one on-disk
// transcript line - identity and rewind markers - for the active-branch
// reconstruction in daemon/history.go. It decodes no message content;
// DecodeTranscriptLine still does that for the lines the walk keeps. ok is
// false for a line that carries no uuid and is not a rewind marker - a
// queue-operation, a latch, and the rest of the on-disk-only records that
// are neither - and for a sidechain line, the same as DecodeTranscriptLine
// and for the same reason: a subagent's line is not the tree the operator's
// conversation lives on, and letting one in could make the walk pick a
// subagent leaf as the active one.
func DecodeTranscriptNode(line []byte) (TranscriptNode, bool) {
	var f struct {
		Type       string `json:"type"`
		UUID       string `json:"uuid"`
		ParentUUID string `json:"parentUuid"`
		Rewound    bool   `json:"rewound"`
		LeafUUID   string `json:"leafUuid"`
		Sidechain  bool   `json:"isSidechain"`
	}
	if err := json.Unmarshal(line, &f); err != nil {
		return TranscriptNode{}, false
	}
	if f.Sidechain {
		return TranscriptNode{}, false
	}
	if f.Type == "last-prompt" {
		return TranscriptNode{Kind: f.Type, Rewound: f.Rewound, LeafUUID: f.LeafUUID}, f.Rewound
	}
	if f.UUID == "" {
		return TranscriptNode{}, false
	}
	return TranscriptNode{UUID: f.UUID, ParentUUID: f.ParentUUID, Kind: f.Type}, true
}

func one(ev Event) []Event { return []Event{ev} }

// systemEvent is lifecycle chatter and the few frames inside it a reader
// should be told about.
//
// Text stays the raw subtype - the passthrough is deliberate and the set is
// open, so an unmodelled subtype arrives as a system event rather than
// degrading. Notice is the resolved half: the few subtypes worth showing a
// reader, named in Wake's vocabulary. See Notice for why the resolution is
// behind the airlock and not in a renderer.
//
// permission_denied also carries a human string in Message; it is the
// after-the-fact report, not the ask - see KindPermissionRequest.
// PermissionMode rides through unresolved. init is the only subtype that
// carries one, so it is empty everywhere else without needing a branch, and
// what it means is the reader's ruling rather than this decoder's - see
// Event.PermissionMode.
func systemEvent(f wireFrame, raw json.RawMessage) Event {
	return Event{
		Kind:           KindSystem,
		SessionID:      f.SessionID,
		Text:           f.Subtype,
		Notice:         systemNoticeFor(f),
		PermissionMode: f.PermissionMode,
		Raw:            raw,
		Session:        initFacts(f),
		Task:           taskUpdate(f),
	}
}

// systemNoticeFor resolves a system frame to its notice. It reads the payload
// for the one subtype the map cannot tell apart: a compaction brackets its work
// with two status frames - the start carries statusCompacting, the end a
// compact_result - so each earns its own notice while every other status frame
// earns none. The end keys on compact_result and not on the compact_boundary
// the map resolves, because a failed compaction emits the former and never the
// latter (slash-commands.jsonl).
func systemNoticeFor(f wireFrame) Notice {
	if f.Subtype == subtypeStatus {
		switch {
		case f.Status == statusCompacting:
			return NoticeCompacting
		case f.CompactResult != "":
			return NoticeCompacted
		}
	}
	return systemNotice[f.Subtype]
}

// taskUpdate is the dispatch lifecycle a system frame reports, and nil for
// every subtype that reports none - which is all but four of them.
//
// The fields are read unconditionally within those four because absent decodes
// to the zero value and every zero here already means "this frame did not
// say". A per-subtype switch would be four branches asserting what the key
// sets already establish, and it would have to be corrected every time Claude
// moved a key.
func taskUpdate(f wireFrame) *TaskUpdate {
	phase, ok := taskPhases[f.Subtype]
	if !ok {
		return nil
	}
	return &TaskUpdate{
		ID:       f.TaskID,
		Dispatch: f.ToolUseID,
		Kind:     taskKind(f.TaskType),
		Phase:    phase,
		Status:   taskStatus(phase, f),
		Label:    f.Description,
		Type:     f.SubagentType,
		Tool:     f.LastToolName,
		Tokens:   taskTokens(f.Usage),
		Elapsed:  taskElapsed(f.Usage),
	}
}

// taskStatus is how the task ended, and TaskRunning for a frame that is not an
// ending. The two terminal frames carry the word in two different places and
// neither can stand in for the other, so both are read and whichever is
// present wins.
//
// A phase of TaskEnded with no recognised word is TaskStatusUnknown rather
// than TaskDone: the frame said the task stopped happening, not that it
// succeeded.
func taskStatus(phase TaskPhase, f wireFrame) TaskStatus {
	if phase != TaskEnded {
		return TaskRunning
	}
	word := f.Status
	if f.Patch != nil && f.Patch.Status != "" {
		word = f.Patch.Status
	}
	if s, ok := taskStatuses[word]; ok {
		return s
	}
	return TaskStatusUnknown
}

// taskKind resolves a task_type, and refuses to guess. A bare map lookup
// would give an unmapped type the zero value, which is "" and not a kind at
// all - see TaskKindUnknown for what each wrong guess costs.
func taskKind(s string) TaskKind {
	if k, ok := taskKinds[s]; ok {
		return k
	}
	return TaskKindUnknown
}

// taskTokens and taskElapsed read the *task* half of a usage object. See
// wireUsage: one wire key carries two disjoint payloads, and these two are the
// only readers of the second.
func taskTokens(u *wireUsage) int {
	if u == nil {
		return 0
	}
	return u.TotalTokens
}

func taskElapsed(u *wireUsage) time.Duration {
	if u == nil {
		return 0
	}
	return time.Duration(u.DurationMS) * time.Millisecond
}

// taskSet is the live set background_tasks_changed carries, and nil on every
// other frame. It is deliberately not folded into taskUpdate: that frame
// reports no phase, no status and no dispatch for any of its members, so a
// TaskUpdate built from one would claim three things nothing said.

// initFacts is the model an init frame names, and nil for any other system
// frame. Only init carries one; reading f.Model unconditionally would stamp an
// empty model onto every other system subtype.
func initFacts(f wireFrame) *SessionFacts {
	// Either fact is enough. The gate was the model alone, which is the wrong
	// domain for a decoder: this says what the wire said, and a frame naming one
	// of the two is not a frame naming neither.
	if f.Subtype != subtypeInit || (f.Model == "" && f.Cwd == "") {
		return nil
	}
	return &SessionFacts{
		Model:         f.Model,
		Dir:           f.Cwd,
		MCPServers:    mcpServers(f.MCPServers),
		SlashCommands: nonEmpty(f.SlashCommands),
	}
}

// nonEmpty is nil for a list the frame did not carry, for mcpServers' reason:
// "this session advertises none" and "no init has arrived yet" must not be two
// values a consumer folds the same way.
func nonEmpty(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	return words
}

// mcpServers is the wire's rows as Wake's, and nil for a frame that named none.
//
// Nil rather than an empty slice: "this session holds no MCP servers" and "no
// init has arrived yet" must not be two different values that render the same,
// and the first is the ordinary case.
func mcpServers(rows []wireMCPServer) []MCPServer {
	if len(rows) == 0 {
		return nil
	}
	out := make([]MCPServer, 0, len(rows))
	for _, r := range rows {
		out = append(out, MCPServer{Name: r.Name, State: r.Status})
	}
	return out
}

// resultFacts is how full the context is after a turn, and nil for a result
// that did not account for one.
//
// The window is looked up by the model the same frame names, rather than by
// taking whatever single entry the map holds: modelUsage is keyed by model and
// a turn that changed model mid-flight has two, in which case the frame's own
// model is the one whose window applies.
func resultFacts(f wireFrame) *SessionFacts {
	if f.Usage == nil {
		return nil
	}
	facts := SessionFacts{
		Model:         f.Model,
		ContextTokens: f.Usage.InputTokens + f.Usage.CacheCreationTokens + f.Usage.CacheReadTokens,
		OutputTokens:  f.Usage.OutputTokens,
	}
	if m, ok := f.ModelUsage[f.Model]; ok {
		facts.ContextWindow = m.ContextWindow
	} else if len(f.ModelUsage) == 1 {
		// A result naming no model, which the corpus has: one entry is
		// unambiguous whatever it is keyed by.
		for _, m := range f.ModelUsage {
			facts.ContextWindow = m.ContextWindow
		}
	}
	return &facts
}

// messageStateEvent reports what became of a message Wake sent.
//
// No degradation for a missing State, unlike the control frames either side
// of it in DecodeLine: a control frame keeps its identity in its nested body,
// so a body-less one is unrecognizable, while this frame's identity is its
// top-level type. A state Wake cannot read is an empty Text on a message
// state, the same ruling rateLimitEvent gets.
func messageStateEvent(f wireFrame, raw json.RawMessage) Event {
	return Event{
		Kind:      KindMessageState,
		SessionID: f.SessionID,
		MessageID: f.CommandUUID,
		Text:      f.State,
		Raw:       raw,
	}
}

// rateLimitEvent reports quota status. A frame with no rate_limit_info at all
// still arrives, carrying an empty status - see messageStateEvent for why a
// payload-less frame of a self-identifying type is not a degraded frame.
func rateLimitEvent(f wireFrame, raw json.RawMessage) Event {
	var status string
	if f.RateLimit != nil {
		status = f.RateLimit.Status
	}
	return Event{
		Kind:      KindRateLimit,
		SessionID: f.SessionID,
		Text:      status,
		Notice:    rateLimitNotice(status),
		Raw:       raw,
	}
}

// controlRequestEvent decodes the permission ask. Anything that is not
// can_use_tool still blocks the process, so it keeps its RequestID even
// though Phase 1 has no answer for it.
func controlRequestEvent(f wireFrame, raw json.RawMessage) Event {
	ev := Event{
		Kind:      KindUnknown,
		SessionID: f.SessionID,
		RequestID: f.RequestID,
		Text:      f.Type,
		Raw:       raw,
	}
	if f.Request == nil {
		return ev
	}
	if f.Request.Subtype != "can_use_tool" {
		ev.Text = f.Request.Subtype
		return ev
	}
	ev.Kind = KindPermissionRequest
	ev.Text = f.Request.Description
	ev.Tool = toolCall(f.Request.ToolUseID, f.Request.ToolName, f.Request.Input)
	// What this ask wants from the operator, and it is not knowable from the
	// subtype: the same can_use_tool carries all three. See AskKind for which,
	// and ToolCall.Ask for what - a client cannot answer a question without
	// both, since the answer is keyed on the question's own text.
	ev.Ask = askKind(f.Request)
	ev.Tool.Ask = askDetail(ev.Ask, f.Request.Input)
	if f.Request.AgentID != "" {
		// A subagent is asking. The envelope names no parent_tool_use_id, so
		// this is the Agent half of the two identifier spaces and Dispatch
		// stays empty - see Subagent.
		ev.Subagent = &Subagent{Agent: f.Request.AgentID}
	}
	return ev
}

// controlResponseEvent decodes the receipt for a control_request Wake sent -
// an interrupt, a set_permission_mode, or a rewind_conversation. Mirrors
// controlRequestEvent, including its ruling on an absent body: everything
// that identifies a receipt is nested, so one with no body is not a degraded
// receipt but an empty frame - no subtype to name it, no request_id to
// attribute it, and nothing to report.
//
// Any other subtype still decodes to a receipt. A receipt Wake cannot
// interpret is not a receipt Wake can afford to drop: the request it answers
// stays outstanding until something acknowledges it, and only RequestID can.
func controlResponseEvent(f wireFrame, raw json.RawMessage) Event {
	ev := Event{
		Kind:      KindUnknown,
		SessionID: f.SessionID,
		Text:      f.Type,
		Raw:       raw,
	}
	if f.Response == nil {
		return ev
	}
	ev.RequestID = f.Response.RequestID
	// A rewind receipt is discriminated by Rewound's *presence*, not its
	// truth: it always carries the key, true or false, and a
	// set_permission_mode receipt never does. Checked before the mode/generic
	// path so a rewind never falls through to it.
	if b := f.Response.Response.Rewound; b != nil {
		ev.Kind = KindRewindReceipt
		ev.Text = f.Response.Subtype
		ev.Rewind = &RewindResult{
			Rewound:                *b,
			TargetMessageUUID:      f.Response.Response.TargetMessageUUID,
			PrefillText:            f.Response.Response.PrefillText,
			PrecedingAssistantUUID: f.Response.Response.PrecedingAssistantUUID,
			Error:                  f.Response.Response.Error,
		}
		return ev
	}
	ev.Kind = KindControlReceipt
	ev.Text = f.Response.Subtype
	// The mode a set_permission_mode landed on, doubly nested like the rest of
	// the payload. Empty on every other receipt and on a refusal, which moved
	// nothing - the reason travels in Control.Error instead.
	ev.PermissionMode = f.Response.Response.Mode
	ev.Control = &ControlResult{
		StillQueued: f.Response.Response.StillQueued,
		Cancelled:   f.Response.Response.Cancelled,
		Error:       f.Response.Error,
	}
	return ev
}

// messageEvents decodes an assistant or user frame, branching on the two
// places the wire is polymorphic before it can fail on either.
func messageEvents(f wireFrame, raw json.RawMessage) []Event {
	base := frameEvent(f, raw)

	if !isJSONObject(f.Message) {
		base.Kind, base.Text, base.Notice = frameText(f.Type, jsonString(f.Message))
		return one(base)
	}
	var m wireMessage
	if err := json.Unmarshal(f.Message, &m); err != nil {
		base.Kind, base.Text = KindUnknown, f.Type
		return one(base)
	}
	if !isJSONArray(m.Content) {
		// Compaction summaries and <local-command-stdout> land here.
		base.Kind, base.Text, base.Notice = frameText(f.Type, jsonString(m.Content))
		return one(base)
	}
	// Decoded one block at a time rather than into []wireBlock, which is
	// all-or-nothing: one block whose shape collides with wireBlock would
	// otherwise collapse the whole frame to a single content-free
	// KindUnknown and discard the assistant's real prose alongside it.
	var raws []json.RawMessage
	if err := json.Unmarshal(m.Content, &raws); err != nil || len(raws) == 0 {
		base.Kind, base.Text = KindUnknown, f.Type
		return one(base)
	}
	return blockEvents(f, raws, raw, messageUsage(m.Usage))
}

// messageUsage decodes an assistant message's own usage tolerantly: a malformed
// one yields no usage rather than an error, so it can never cost the prose the
// message carried beside it. See wireMessage.Usage for the hazard.
func messageUsage(raw json.RawMessage) *wireUsage {
	if len(raw) == 0 {
		return nil
	}
	var u wireUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil
	}
	return &u
}

func blockEvents(f wireFrame, raws []json.RawMessage, raw json.RawMessage, usage *wireUsage) []Event {
	evs := make([]Event, 0, len(raws))
	// The message's output-token count belongs to the message, not to each of
	// its blocks, so it is attached to the first text block and to that one
	// alone: two text blocks must never each claim the whole message's tokens.
	tokensLeft := usage != nil && usage.OutputTokens > 0
	for _, rb := range raws {
		ev := blockEvent(f, rb, raw)
		if tokensLeft && ev.Kind == KindAssistantText {
			ev.OutputTokens = usage.OutputTokens
			tokensLeft = false
		}
		evs = append(evs, ev)
	}
	return evs
}

// ImagePlaceholder is the text a decoded image block carries up in place of
// its bytes: the transcript cannot draw the image, and a user turn rendering
// this reads far better than one rendering nothing.
//
// Exported because internal/ui's room-history reconstruction must recognise it:
// every image shares this one text, so it can never be sound proof that a turn
// was broadcast, and roomhistory.go excludes it from that rule.
const ImagePlaceholder = "[Image]"

// blockEvent decodes one content block. A block that fails to decode
// degrades to KindUnknown on its own, leaving its siblings intact.
func blockEvent(f wireFrame, rb, raw json.RawMessage) Event {
	ev := frameEvent(f, raw)

	var b wireBlock
	if err := json.Unmarshal(rb, &b); err != nil {
		ev.Kind, ev.Text = KindUnknown, blockType(rb, f.Type)
		return ev
	}
	switch b.Type {
	case blockTypeText:
		ev.Kind, ev.Text, ev.Notice = frameText(f.Type, b.Text)
	case blockTypeThinking:
		ev.Kind, ev.Text = KindThinking, b.Thinking
	case blockTypeImage:
		// An image on a user message, read back off a transcript. The bytes
		// are not carried up - a transcript has no way to draw one - so this is
		// the placeholder that keeps a reopened conversation from rendering the
		// image as nothing. See CLAUDE.md's image ruling and the findings note.
		ev.Kind, ev.Text = KindUserText, ImagePlaceholder
	case blockTypeToolUse:
		ev.Kind = KindToolUse
		ev.Tool = toolCall(b.ID, b.Name, b.Input)
	case blockTypeToolResult:
		ev.Kind = KindToolResult
		ev.Tool = &ToolCall{ID: b.ToolUseID, IsError: b.IsError}
		ev.Text = toolResultText(b.Content)
		if ev.Subagent == nil {
			// Only when the frame is not itself forwarded. A forwarded frame
			// carrying a dispatch receipt would be a subagent dispatching a
			// subagent, which §11 of the findings note lists as never
			// recorded, so the speaker attribution wins and the receipt half
			// is dropped. That errs toward showing the body rather than
			// suppressing it as a duplicate - the safe direction for a shape
			// nobody has seen.
			ev.Subagent = dispatchReceipt(f, b)
		}
	default:
		ev.Kind, ev.Text = KindUnknown, b.Type
	}
	return ev
}

// frameEvent seeds an event with what every event from a frame shares.
// Echoed and the subagent attribution are both frame-level, so a frame
// producing several blocks marks them all - see Event.Echoed for why Echoed
// is a field and not a kind, and Subagent for what the attribution is worth.
func frameEvent(f wireFrame, raw json.RawMessage) Event {
	return Event{
		SessionID: f.SessionID,
		Echoed:    f.IsReplay || f.IsSynthetic,
		Subagent:  forwardedSubagent(f),
		Raw:       raw,
	}
}

// frameText picks the kind for a run of text, cleans it, and resolves the one
// notice that can only be read off the content.
//
// The unwrapping is here rather than in a renderer because the markers are
// pure wire format with no argument on the other side - the first row of the
// airlock ruling. Only the user's side is unwrapped: that is the only side of
// the transcript the envelope has ever appeared on.
//
// The notice is the same argument one step further. Claude's abort marker is
// an ordinary user frame whose *text* is the only thing that identifies it, so
// resolving it anywhere else would mean a renderer comparing against a wire
// literal - and getting it wrong means drawing Claude's abort notice as
// something the operator typed. See interruptNotice.
func frameText(frameType, text string) (EventKind, string, Notice) {
	kind := textKind(frameType)
	if kind == KindUserText {
		text = stripLocalCommandStdout(text)
	}
	return kind, text, interruptNotice(kind, text)
}

// blockType recovers a block's own type when the rest of the block fails to
// decode, so a field collision elsewhere does not also cost us its name.
// Falls back to the frame type for a block that is not an object at all.
func blockType(rb json.RawMessage, fallback string) string {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rb, &probe); err == nil && probe.Type != "" {
		return probe.Type
	}
	return fallback
}

// textKind separates the user's side of the transcript from the agent's.
// Folding them together would render a compaction summary as agent speech.
func textKind(frameType string) EventKind {
	if frameType == "user" {
		return KindUserText
	}
	return KindAssistantText
}

// firstJSONByte is the cheapest way to tell a JSON string from an array or
// object without a second full unmarshal.
func firstJSONByte(raw json.RawMessage) byte {
	t := bytes.TrimLeft(raw, " \t\r\n")
	if len(t) == 0 {
		return 0
	}
	return t[0]
}

func isJSONObject(raw json.RawMessage) bool { return firstJSONByte(raw) == '{' }
func isJSONArray(raw json.RawMessage) bool  { return firstJSONByte(raw) == '[' }

// jsonString unquotes a JSON string, falling back to the raw bytes so a
// shape we have not seen still reaches a human instead of vanishing.
//
// The fallback is for shapes that are genuinely unrecorded. It used to catch
// a tool_result's array content as well, which was not unrecorded at all -
// 10 of the 44 recorded results carry it - and printed a JSON literal in the
// transcript. toolResultText handles that shape properly now, and this is
// left to cover what is still unknown.
func jsonString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// The shapes Claude's stream-json is decoded into - part of the airlock; see
// protocol.go.
//
// Every one of these is transcribed from testdata/stream/*.jsonl, recorded
// from live sessions, not from documentation. Two fields are polymorphic on
// the wire and so are json.RawMessage here, branched on explicitly by the
// decoder: Go fails the *entire frame* on a type mismatch, and a frame lost
// that way is invisible rather than loud.
//
// The airlock is these four files and nothing else in Wake knows Claude
// Code's stream-json format:
//
//	protocol.go    decoding - one wire line in, core.Events out
//	wire.go        the shapes it decodes into
//	vocabulary.go  Claude's words resolved into Wake's
//	encode.go      the frames Wake writes back
//
// internal/core/airlock_test.go enforces that over the whole tree and reads
// the same list. protocol.go's header carries the full rule.

package core

import "encoding/json"

// maxLineBytes bounds one stream-json line. Frames carrying a large tool
// result or a compaction summary comfortably exceed bufio's 64KB default,
// so both the decoder's tests and the session pump size their buffers here.
const maxLineBytes = 16 * 1024 * 1024

// wireFrame is the envelope every stream-json line shares.
type wireFrame struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`

	// Message is an object on assistant/user frames and a bare string on
	// system/permission_denied. Typing it as a struct loses that frame
	// whole: "cannot unmarshal string into Go struct field".
	Message json.RawMessage `json:"message"`

	// Result frames carry the turn's final text. See KindTurnEnd: a result
	// is one turn ending, not the process exiting.
	Result string `json:"result"`

	// control_request frames carry no session_id - they, control_response and
	// control_cancel_request are the only frames in the corpus that do not -
	// so RequestID is the sole correlator. They also carry no top-level
	// subtype: their keys are exactly [request, request_id, type], and the
	// subtype lives one level down in Request. Reading Subtype here for them
	// yields "" and silently demotes the most important frame Wake decodes to
	// an unrecognized control frame.
	//
	// RequestID is shared with control_cancel_request, which is the frame that
	// retires an ask and whose keys are exactly [request_id, type] - two, and
	// the id is the same one the ask carried, byte for byte. It nests nothing,
	// so unlike a receipt it needs no field of its own here. See
	// KindRequestWithdrawn.
	RequestID string          `json:"request_id"`
	Request   *wireControlReq `json:"request"`

	// control_response is the receipt for a control_request Wake sent. Its
	// keys are exactly [response, type] on all 12 recorded receipts: no
	// session_id, no uuid, and - the trap this field exists to avoid - no
	// top-level request_id either. Where control_request nests only its
	// subtype, a receipt nests its correlator too, so RequestID above is ""
	// for every one of them.
	Response *wireControlResp `json:"response"`

	// command_lifecycle reports what became of a message Wake sent. It
	// appears only for messages Wake stamped with a top-level uuid, which
	// comes back here as CommandUUID; the frame's own uuid identifies the
	// frame and is dropped like every other frame's. State is queued,
	// started, completed or cancelled across the corpus, and the set is open
	// - see KindMessageState.
	CommandUUID string `json:"command_uuid"`
	State       string `json:"state"`

	// system/init reports the mode the session is genuinely running in, and it
	// arrives on every turn. It is the second observable for a permission mode
	// and the only one that can see a change Wake did not ask for: a mode
	// changed through updatedPermissions on a permission allow produces no
	// receipt at all (permission-mode-findings.md §5).
	//
	// Its trap is one-directional. At spawn it is normalized rather than an
	// echo of --permission-mode - manual reports "default" - so it must not be
	// read as confirmation that the flag took. After a set_permission_mode it
	// reports the real mode (§4).
	PermissionMode string `json:"permissionMode"`

	// conversation_reset carries new_conversation_id, which is deliberately
	// NOT decoded: it is not the id replacing this one. b3144871... is the
	// value on slash-commands.jsonl:31 and appears on that line in all 1004
	// and never again, while every later frame carries 6524c398... A field
	// here would be handed up as "the new id" by the first person to see it
	// - which is exactly what happened - so the key is named only so nobody
	// rediscovers it as a gap. See KindSessionReset.

	RateLimit *wireRateLimit `json:"rate_limit_info"`

	// stream_event carries one Anthropic streaming event here. It is the only
	// frame in the corpus's format that Wake decodes into *nothing* on most of
	// its shapes - see wireStreamEvent, and partialEvent for why.
	StreamEvent *wireStreamEvent `json:"event"`

	// What the session is running as, and how full its context is - the two
	// facts a status bar shows that are not the conversation.
	//
	// Model is on system/init, where it is the id ("claude-sonnet-5") rather
	// than a display name. ModelUsage is on result, keyed by that same id, and
	// carries the context window - the only place in the stream any frame says
	// how large the window is, so a percentage cannot be computed without it.
	//
	// Usage is the turn's own accounting. Its three input fields sum to what
	// the model was actually shown, cache included, which is the number a
	// context percentage is a percentage of. Note that this is a *level*, not
	// a delta: it is read as the latest value and never accumulated, which is
	// what keeps it clear of the /clear reset that CLAUDE.md warns spend
	// accounting about.
	Model string `json:"model"`

	// Cwd is the directory an init frame says the session is running in. On
	// init and on no other frame, which is what makes it safe to merge.
	//
	// Read because it *moves*: EnterWorktree is advertised to every session
	// Wake spawns, so the spawn directory stops being the running directory the
	// moment an agent uses it. See SessionFacts.Dir.
	Cwd string `json:"cwd"`

	// MCPServers is what an init frame says about the servers this session
	// holds. Present on init and on nothing else, and empty for a session
	// started with none - which is most of them, and is not a warning.
	MCPServers []wireMCPServer `json:"mcp_servers"`

	// SlashCommands is every command this session's claude answers to,
	// announced on init - built-ins and the operator's own ~/.claude/commands
	// files alike, 133 of them across the recorded corpus. It is what a
	// completion menu offers; it decides no routing, because the list is per
	// session and arrives after the first frame while a draft is judged per
	// keystroke (internal/ui/slash.go's header).
	SlashCommands []string `json:"slash_commands"`

	Usage      *wireUsage           `json:"usage"`
	ModelUsage map[string]wireModel `json:"modelUsage"`

	// IsReplay marks a frame the transcript is echoing back rather than one
	// the human just produced - including, under --replay-user-messages,
	// every message Wake itself sends. IsSynthetic marks one the tooling
	// wrote, such as a compaction summary. Both are frame-level, so they
	// ride every event the frame produces; see Event.Echoed.
	//
	// This is the only file allowed to read them, so it is the only place
	// the distinction can be preserved. Dropping them here would force a
	// renderer to re-parse Event.Raw, which no file above may do.
	IsReplay    bool `json:"isReplay"`
	IsSynthetic bool `json:"isSynthetic"`

	// The subagent dimension, and it is three fields rather than one. All
	// three are top-level on an assistant or user frame the CLI forwarded
	// from a subagent, and all three arrive together: measured over the
	// corpus, 80 lines carry a non-null parent_tool_use_id and all 80 carry
	// subagent_type and task_description too, while no parent frame carries
	// any of them. Zero exceptions either way.
	//
	// parent_tool_use_id alone would be enough to *separate* two concurrent
	// subagents - it equals the id of the parent's own dispatch tool_use,
	// checked per fixture with no leftovers on either side - but it is an
	// opaque toolu_… that names nothing a human reads. task_description is
	// the name, and it rides the same frames, so nothing has to be looked up
	// or waited for.
	//
	// These are frame-level, so like IsReplay they ride every event the
	// frame produces: an assistant frame carrying thinking plus a tool_use
	// marks both blocks as the same subagent's.
	ParentToolUseID string `json:"parent_tool_use_id"`
	SubagentType    string `json:"subagent_type"`
	TaskDescription string `json:"task_description"`

	// The task lifecycle, carried on five system subtypes. Read together
	// because they overlap rather than partition: TaskID is on all five,
	// ToolUseID on three, Description on three, Usage on two, and
	// task_updated carries none of them beyond the id.
	//
	// Description is *not* TaskDescription above. They are different keys
	// with different lifetimes: task_description rides a forwarded frame and
	// repeats what the subagent was asked to do, while description changes
	// through the dispatch and reports what it is doing now. Reading either
	// into the other would make a live status line freeze at the prompt.
	//
	// Status and Patch are the two endings, and they are separate fields
	// because they are separate keys - task_notification carries a top-level
	// status, task_updated a patch object - and nothing on the wire lets one
	// stand in for the other.
	//
	// Prompt, OutputFile and Summary are deliberately not here. The first is
	// the subagent's whole instruction, the second names an on-disk
	// transcript nothing yet opens, and the third repeats prose the reader
	// has already seen. This file's rule is that a field arrives when
	// something needs it.
	// ToolUseID is top-level here and on system/permission_denied, which is
	// not a task frame - so it is read only inside the task branch. It is a
	// different key from the ToolUseID nested in a control request.
	TaskID       string         `json:"task_id"`
	ToolUseID    string         `json:"tool_use_id"`
	TaskType     string         `json:"task_type"`
	Description  string         `json:"description"`
	LastToolName string         `json:"last_tool_name"`
	Status       string         `json:"status"`
	Patch        *wireTaskPatch `json:"patch"`

	// CompactResult is the outcome a compaction's terminal system/status frame
	// carries - "success" or "failed". Its presence is what tells that frame from
	// the "compacting" start flag, both being subtype "status". See systemNoticeFor.
	CompactResult string `json:"compact_result"`

	// ToolUseResult is the structured sibling of a tool_result block, and it
	// is polymorphic in the same way Message is: an object on some frames
	// and a bare string on others (permission-deny-response.jsonl,
	// interrupt-mid-tool.jsonl), so a struct here would lose those frames
	// whole.
	//
	// Only one thing is read out of it, and only because nothing else on the
	// frame carries it: agentId, which is present on exactly the nine
	// subagent dispatch receipts in the corpus and on no other tool result.
	// The tool_result block itself names only a tool_use_id, so without this
	// key the frame that reports a subagent finishing is indistinguishable
	// from any other tool result - and its content is a verbatim repeat of
	// prose the reader has already seen. See toolResultSubagent.
	ToolUseResult json.RawMessage `json:"tool_use_result"`
}

// wireControlReq is the nested body of a control_request - the only place
// that frame's subtype exists. Subtype "can_use_tool" is a permission ask,
// and the frame blocks the process until it is answered on stdin.
type wireControlReq struct {
	Subtype     string         `json:"subtype"`
	ToolName    string         `json:"tool_name"`
	Description string         `json:"description"`
	Input       map[string]any `json:"input"`
	ToolUseID   string         `json:"tool_use_id"`

	// RequiresUserInteraction marks an ask that is a question put to the
	// operator rather than a request to run a tool. It is true on all six
	// recorded question asks and appears on none of the 26 earlier recordings
	// - absent because no agent had called either interactive tool, not
	// because those travel on another channel: the envelope is the same
	// can_use_tool.
	//
	// Absent decodes to false, which is the reading the corpus supports: no
	// recording carries an explicit false, so "not present" and "not
	// interactive" are the same observation and nothing here may pretend to
	// tell them apart.
	//
	// It is necessary and not sufficient. Both recorded interactive tools set
	// it and only one of them needs an answer inside the allow, so it is one
	// of the two inputs to askKind rather than a field handed up on its own.
	RequiresUserInteraction bool `json:"requires_user_interaction"`

	// AgentID says a subagent is asking, and its *presence* is the signal:
	// the two parent-raised asks in the corpus (permission.jsonl,
	// permission-deny-response.jsonl) have exactly the seven keys above and
	// no agent_id, while the one subagent-raised ask carries it. Dropped, a
	// subagent's ask is indistinguishable from its parent's and the operator
	// approves an irreversible write without knowing who asked.
	//
	// It is the subagent's own id, not a parent_tool_use_id - the envelope
	// carries neither that nor a session_id - so it lands on
	// Subagent.Agent. See Subagent for why the two identifier spaces are
	// kept apart.
	AgentID string `json:"agent_id"`
}

// wireToolUseResult is the object form of a frame's tool_use_result, read
// only for what it says about a subagent dispatch.
//
// Two recorded shapes: a completed foreground dispatch carries agentId,
// agentType and status "completed"; an async launch carries agentId, status
// "async_launched" and isAsync, and no agentType at all. Everything else
// either shape carries - the prompt, the model, the token counts, the output
// file - is deliberately not read: none of it is needed to attribute the
// frame, and this file's rule is that a field arrives when something needs
// it, not because the wire has it.
type wireToolUseResult struct {
	AgentID   string `json:"agentId"`
	AgentType string `json:"agentType"`
	Status    string `json:"status"`
}

// The two dispatch statuses the corpus records, and the only two mapped onto
// a SubagentResult. Anything else becomes SubagentUnknown, which shows the
// receipt's content rather than suppressing it.
const (
	dispatchCompleted = "completed"
	dispatchLaunched  = "async_launched"
)

// wireControlResp is the nested body of a control_response, and the only
// place both that frame's subtype and its correlator exist.
//
// Subtype was "success" on all 12 recorded receipts, including every one
// that interrupted nothing. It is transport level - "an answer, not a
// protocol error" - and not a verdict, the same thing the outbound half of
// this file says about the answer Wake writes.
//
// RequestID is absent when Wake's own request omitted it: one recorded
// receipt reads {"subtype":"success","response":{"still_queued":[]}} and
// names no request at all. That receipt is unattributable, which is why Wake
// must always send a request_id even though the CLI does not require one.
// Error is the refusal half, and it sits at this level rather than in the
// payload below: a refused control_request answers subtype "error" with the
// reason top-level and no nested response at all
// (permission-mode-findings.md §6). That is a different shape from a permission
// deny, which is a *successful* receipt carrying behavior "deny".
type wireControlResp struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Error     string          `json:"error"`
	Response  wireControlBody `json:"response"`
}

// wireControlBody is the receipt's payload, one level below the body that
// already holds the subtype - a control_response nests twice where a
// control_request nests once.
//
// Four shapes across the 12 recorded receipts: still_queued empty (9), it
// naming a surviving message uuid (1), and still_queued alongside cancelled
// either naming a destroyed uuid (1) or empty (1). The findings note's §3
// lists three and names the fourth in prose below the list; the bytes say
// four, and the difference is the one that matters - see ControlResult for
// why an absent cancelled and an empty one are different facts.
//
// Mode is a set_permission_mode receipt's whole payload, and it is the
// authority on what the mode became - never the string that was sent. `manual`
// is accepted and normalizes to `default` (§6), so the two disagree on a real
// cycle position rather than only in principle.
type wireControlBody struct {
	StillQueued []string `json:"still_queued"`
	Cancelled   []string `json:"cancelled"`
	Mode        string   `json:"mode"`

	// Rewind receipt payload. Rewound is a pointer so its *presence* - not its
	// truth - is the discriminator: a rewind receipt always carries the key
	// (true or false), a set_permission_mode receipt never does. Error here is
	// the rewind failure reason and sits at this innermost level, unlike a mode
	// refusal whose error is one level up on wireControlResp.
	Rewound                *bool  `json:"rewound"`
	TargetMessageUUID      string `json:"targetMessageUuid"`
	PrefillText            string `json:"prefillText"`
	PrecedingAssistantUUID string `json:"precedingAssistantUuid"`
	Error                  string `json:"error"`
}

// wireRateLimit is rate_limit_info. The frame also carries resetsAt (Unix
// seconds), rateLimitType, overageStatus, overageDisabledReason and
// isUsingOverage; Phase 1 surfaces only Status.
//
// Cadence is once per process, on the first turn that hits the API - not
// once per turn. Nine recorded processes, nine events, even though one of
// them ran seven turns. And every recorded sample says "allowed", so
// anything built on this is designing against a single observed value.
type wireRateLimit struct {
	Status string `json:"status"`
}

// wireUsage is the token accounting on a result frame.
//
// The three input fields and the output one answer different questions, which
// is why both are here. All three inputs count toward the context the model was
// shown - a cached read is still in the window, and reading only InputTokens
// reports 2 where the true figure is 50,788 on the very first fixture.
// OutputTokens is not part of that: it is what this turn *produced*, which is
// what a running total is a total of.
//
// Both are per-turn figures rather than session totals, so a consumer sums the
// output and keeps only the newest input.
//
// The last three are the *task* accounting, which travels under the same
// `usage` key on task_progress and task_notification. One struct rather than
// two because the wire has one key, and the two payloads are disjoint:
// measured over the corpus, no result frame's usage carries any of the three
// and no task frame's carries any of the four above. A second field tagged
// `json:"usage"` is not possible anyway, and a raw message decoded twice
// would be a second decoder for one key.
//
// Duration is milliseconds. It is the task's own elapsed time as Claude
// measured it, not something this process timed.
type wireUsage struct {
	InputTokens         int `json:"input_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	OutputTokens        int `json:"output_tokens"`

	TotalTokens int `json:"total_tokens"`
	ToolUses    int `json:"tool_uses"`
	DurationMS  int `json:"duration_ms"`
}

// wireTaskPatch is task_updated's whole payload beyond the id. Ten frames,
// one shape, and §11 of the subagent findings note lists anything beyond
// these two keys as unverified - so a reader must not assume a patch means
// "ended" because it exists; it means ended because of what Status says.
//
// EndTime is not read. It is an epoch millisecond stamp of when the task
// finished, and nothing draws it: a finished row shows the elapsed time the
// usage already reported, which is the number that was on screen while it ran.
type wireTaskPatch struct {
	Status string `json:"status"`
}

// wireModel is one entry of a result frame's modelUsage map. ContextWindow is
// the field this type exists for; the rest of the entry is spend accounting,
// which nothing here reads for the reasons CLAUDE.md gives.
type wireModel struct {
	ContextWindow int `json:"contextWindow"`
}

// wireMessage is the message object on assistant and user frames.
type wireMessage struct {
	Role string `json:"role"`

	// Content is an array of blocks on assistant frames and on tool-result
	// user frames, and a bare string on compaction summaries and
	// <local-command-stdout> frames.
	Content json.RawMessage `json:"content"`

	// Usage is the message's own token accounting on an assistant frame, kept
	// raw and decoded separately (messageUsage). A malformed usage must never
	// cost the prose beside it: decoding it into the struct here would fail the
	// whole message and collapse it to a content-free KindUnknown - the
	// all-or-nothing hazard messageEvents decodes content block-by-block to
	// avoid, and a RawMessage never fails to capture whatever bytes are there.
	Usage json.RawMessage `json:"usage"`
}

// wireBlock is one content block. Thinking blocks also carry "signature"
// and tool_use blocks also carry "caller"; both are ignored, and are named
// here only so nobody rediscovers them as a decode failure.
type wireBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Thinking string         `json:"thinking"`
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Input    map[string]any `json:"input"`

	// tool_result blocks. Content is a string in practice and
	// string | []block in principle. IsError is absent on some frames,
	// which decodes to false - the same as an explicit false.
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// The content-block types Wake knows. The first four are in the recorded
// corpus; blockTypeText is named because toolResultText has to recognise it a
// second time inside a tool_result's own array content, and two spellings of
// the same wire word in one file is how a decoder starts disagreeing with
// itself.
//
// blockTypeImage is the fifth and it is not in the stream corpus: Wake writes
// it (encode.go) and reads it back off a transcript a session was handed an
// image on, but no recorded stdout carries one - it is recorded only in
// testdata/input/image-block.stdin.jsonl. blockEvent decodes it to a
// placeholder so a reopened conversation shows the image was there rather than
// nothing (docs/superpowers/notes/2026-08-15-image-input-findings.md).
const (
	blockTypeText       = "text"
	blockTypeThinking   = "thinking"
	blockTypeToolUse    = "tool_use"
	blockTypeToolResult = "tool_result"
	blockTypeImage      = "image"
)

// The four streaming words Wake reads, out of the seven event types and five
// delta types a stream_event can carry. Named for the reason the block types
// above are: partialEvent recognises exactly these and declines the rest, so
// they are the whole of what this build claims to understand.
const (
	streamContentBlockDelta = "content_block_delta"
	streamTextDelta         = "text_delta"

	// streamMessageDelta states what the message it belongs to has produced so
	// far, and streamMessageStart is the boundary between one message's counts
	// and the next one's. They are the third and fourth streaming words Wake
	// reads, and together they are the only source of a turn's output count
	// *while the turn is running*.
	//
	// Both are needed because the count is **cumulative within a message** and a
	// message emits "one or more" of them - the streaming docs' own words - so a
	// reader sums messages and takes the newest figure inside each. Reading the
	// deltas alone reported 250 for a message that produced 150. See
	// KindTurnTokens.
	streamMessageDelta = "message_delta"
	streamMessageStart = "message_start"
)

// wireStreamEvent is the `event` on a stream_event frame: one Anthropic
// Messages-API streaming event, forwarded verbatim under
// --include-partial-messages.
//
// **Recorded as of 2026-08-21** — testdata/stream/partial-turn.jsonl holds
// one streamed turn, and it confirmed this shape as transcribed. The original
// source, kept because it explains the field set: claude 2.1.233's own zod
// schema, transcribed by hand:
//
//	{type:"stream_event", event:…, parent_tool_use_id:string|null,
//	 uuid:string, session_id:string, ttft_ms?:int}
//
// and, from the same bundle, the reads the CLI itself performs on it:
// `event.type==="content_block_delta" && event.delta.type==="text_delta"`
// yielding `event.delta.text`. That is a stronger source than documentation
// and a weaker one than a frame - the same standing this package gives the
// TodoWrite vocabulary - and the difference is the *envelope* rather than the
// field names. partialEvent is written to survive being wrong about it: every
// shape it does not recognise yields no event at all, so a schema that has
// moved costs the preview and never the transcript.
//
// Four of the seven event types are decoded into nothing on purpose rather than
// for want of a field: content_block_start, content_block_stop, message_stop
// and ping all describe a block whose contents arrive complete on the ordinary
// assistant frame that follows.
//
// **message_delta and message_start are read**, for the count rather than for
// the content: between them they are the only account of a turn's cost that
// arrives *while the turn is running*. The blocks they describe still arrive
// complete behind them, so nothing here draws anything. See KindTurnTokens.
type wireStreamEvent struct {
	Type  string           `json:"type"`
	Delta *wireStreamDelta `json:"delta"`

	// Usage is present on message_delta and absent everywhere else this reads.
	// A pointer so "the frame said nothing" and "the frame said zero" stay
	// different answers - the same shape wireFrame.Usage has, and for the same
	// reason.
	Usage *wireStreamUsage `json:"usage"`
}

// wireStreamUsage is the one figure Wake takes off a message_delta.
//
// Only the output count: the input and cache figures on this shape describe the
// request that is finishing rather than the context a session is carrying, and
// the context level Wake draws comes off the result frame, which states it for
// the whole turn.
type wireStreamUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// wireStreamDelta is one increment of a content block. Only a text delta is
// read: input_json_delta is a tool call's arguments, which arrive whole on the
// tool_use block, and thinking_delta is a block internal/ui folds shut anyway.
type wireStreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// wireMCPServer is one row of an init frame's mcp_servers.
//
// Two fields because two are all it carries. The status is a string rather than
// an enum for the airlock's usual reason: a value this build has not seen must
// arrive intact rather than being flattened to a default, and the one thing
// anything above cares about is whether it equals MCPNeedsAuth.
type wireMCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

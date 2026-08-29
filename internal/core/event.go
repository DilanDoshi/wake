package core

import (
	"encoding/json"
	"time"
)

// EventKind is Wake's vocabulary. Nothing above protocol.go should ever
// need to know Claude's own frame names.
type EventKind string

const (
	// KindSystem is lifecycle chatter: hook_started, hook_response, init,
	// thinking_tokens, status, compact_boundary, permission_denied. Text
	// carries the subtype. These are 118 of the 184 lines in the six
	// original fixtures, so anything rendering them unfiltered drowns.
	KindSystem EventKind = "system"

	// KindUserText is content on a user frame: the user's own turn, a
	// compaction summary, or <local-command-stdout>. Not assistant speech.
	KindUserText EventKind = "user_text"

	KindAssistantText EventKind = "assistant_text"
	KindThinking      EventKind = "thinking"
	KindToolUse       EventKind = "tool_use"
	KindToolResult    EventKind = "tool_result"

	// KindPartialText is the tokens of an assistant block that is still being
	// written - Text is the delta that just arrived, never the block so far.
	// It exists only under --include-partial-messages.
	//
	// **It is a preview and never a record.** The same words arrive again a
	// moment later as a complete KindAssistantText, which is what a transcript
	// keeps; a consumer that stored these would hold every sentence twice. So
	// the contract is: show it while it is arriving, drop it when the block
	// lands. internal/ui/partial.go is the one consumer and its header carries
	// the cost argument for why it is drawn as plain text.
	KindPartialText EventKind = "partial_text"

	// KindTurnTokens is how much the turn in flight has produced so far. It
	// carries no text and nothing to draw - only Session.TurnOutputTokens - and
	// it exists only under --include-partial-messages.
	//
	// It is the answer to a question every surface used to get wrong. The only
	// *complete* output count is on the result frame that **ends** a turn, so a
	// working line and a roster row could report a session total and nothing
	// else: during turn N they showed turns 1…N−1.
	//
	// **The figure is cumulative for one message, not an increment.** The
	// streaming docs say a message emits "one or more message_delta events" and
	// that their usage counts are *cumulative*, so a reader adds up **messages**
	// and takes the newest figure within each - see KindMessageStart, which is
	// the boundary between them. Adding the deltas themselves reported 250 for a
	// message that produced 150.
	//
	// **It is a progress figure and never an account.** The authority for what
	// a turn cost stays the result frame; these are what is known so far, and a
	// consumer that stored them as the total would double-count the moment the
	// result landed.
	KindTurnTokens EventKind = "turn_tokens"

	// KindMessageStart is one message of a turn beginning, and a turn is several.
	//
	// Its value is Wake's word rather than Claude's - `message_began` - because
	// a Kind is this package's own vocabulary and the airlock's guard polices
	// Claude's spelling everywhere outside the four files. The wire word lives
	// in wire.go as streamMessageStart.
	// It carries nothing at all: it exists so a reader can tell one message's
	// cumulative count from the next one's, which is the whole of what makes a
	// turn's figure addable. See KindTurnTokens.
	//
	// Its frame's own `usage.output_tokens` is deliberately not read - it is 1 or
	// 2 there, for a message that has produced nothing yet, and taking it would
	// add a token per message to every turn.
	KindMessageStart EventKind = "message_began"

	// KindTurnEnd is one turn finishing - the moment to send the next
	// message. It is NOT process exit. One recorded process emitted seven
	// of these (testdata/stream/slash-commands.jsonl). The name carries
	// that fact because a comment would not have survived.
	KindTurnEnd EventKind = "turn_end"

	// KindPermissionRequest is the agent asking to use a tool. The frame
	// blocks the process until answered on stdin, and it is one of the two
	// frames in the corpus carrying no session_id of its own -
	// KindControlReceipt is the other. So SessionID here is stamped by
	// Session.attribute from the pipe it arrived on, not read off the wire.
	//
	// Both ids are load-bearing and they answer different questions:
	// SessionID is which agent is blocked, RequestID is which ask an answer
	// is answering, and RequestID stays the only correlator for the answer
	// itself - it is what the control_response echoes back.
	KindPermissionRequest EventKind = "permission_request"

	// KindControlReceipt is Claude acknowledging a control_request Wake
	// sent - today that means an interrupt. Text is the transport subtype
	// ("success" on all 12 recorded receipts, including every no-op), which
	// is NOT a verdict: an interrupt that interrupted nothing still says
	// success. Control carries what the receipt actually reports.
	//
	// The frame names no session and no subtype of its own, so what it is a
	// receipt *for* is knowable only from the request Wake sent - and
	// RequestID is optional on the wire (interrupt-no-request-id.jsonl),
	// which is why Wake must always send one.
	KindControlReceipt EventKind = "control_receipt"

	// KindRewindReceipt is Claude's rewind_conversation receipt. See Event.Rewind.
	KindRewindReceipt EventKind = "rewind_receipt"

	// KindRequestWithdrawn is Claude retiring a control_request it sent -
	// today, in every recording, the permission ask an interrupt landed on.
	// RequestID names the dead request and is the whole payload: the frame
	// carries exactly two keys, type and request_id, with no session_id, no
	// subtype and no reason.
	//
	// A client holding that RequestID must stop offering the ask. Answering
	// it after this is written into the void - a well-formed allow for a
	// withdrawn request produced no frame, no error and no tool run - so
	// what is left on screen otherwise is a prompt nobody will ever answer.
	//
	// The name says **withdrawn**, not interrupted, and the distance between
	// those is the whole of what the recording licenses. Three recordings,
	// one cause: whether the frame also fires when an ask dies for any other
	// reason - the turn erroring, the process shutting down, a hook killing
	// the tool - is unrecorded, so nothing may read this as "an interrupt
	// happened". It says one request is dead and names it. Nor does it say
	// the request was a permission ask: controlRequestEvent already accepts
	// control_requests of other subtypes, and this frame carries no subtype
	// of its own to tell them apart. Retire what matches the id; conclude
	// nothing else.
	//
	// The safety around it is the CLI's ordering rather than Wake's, and
	// that is worth knowing before anything is built on it: the withdrawal
	// arrives *before* the aborted turn's result on all three recordings, so
	// a daemon clearing what a session owes on KindTurnEnd cannot hang and
	// cannot clear early. Wake enforces none of that.
	// TestAWithdrawalNamesAnEarlierAskAndATurnEndStillFollowsIt is where the
	// inheritance is checked against the bytes.
	KindRequestWithdrawn EventKind = "request_withdrawn"

	// KindMessageState is the fate of a message Wake sent: queued, started,
	// completed or cancelled. MessageID says which message; Text says what
	// became of it. Claude calls the frame command_lifecycle and the id
	// command_uuid - Wake sends messages, so this is the answer to "what
	// happened to the thing I sent".
	//
	// Only messages Wake stamps with a uuid produce these. The state set is
	// open - the 2.1.226 binary also contains "discarded", which no
	// recording has shown - so Text is a string and an unrecognized state
	// still arrives as a state rather than degrading to KindUnknown.
	KindMessageState EventKind = "message_state"

	// KindRateLimit reports quota status. Text is the status string.
	KindRateLimit EventKind = "rate_limit"

	// KindSessionReset is /clear killing a session id. SessionID is the id
	// that just ended, and that is the whole report: this event does NOT
	// name the id replacing it, because the frame does not either.
	//
	// The frame's new_conversation_id looks like the replacement and is not.
	// It appears on that one line in the whole corpus and never again, while
	// every frame after the reset carries a different id that appears
	// nowhere on the reset frame. It is dropped in the airlock rather than
	// carried up under a name that invites the mistake - Event.Raw still has
	// it for in-process debugging.
	//
	// The successor arrives the way every session id does: on the next
	// frame. session_id is on every frame, so a change in Event.SessionID
	// between events IS the re-key signal - and the change lands on a
	// hook_started before the next init, so re-keying on init misattributes
	// every frame between.
	KindSessionReset EventKind = "session_reset"

	// KindUnknown is a frame Wake does not model. Text carries the wire
	// type so it is identifiable in a bug report. Never an error.
	KindUnknown EventKind = "unknown"
)

// ToolCall is a tool invocation, its result, or a request to run one.
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`

	// Display is the one argument worth showing beside the tool's name -
	// a Bash command, a Read path, the description an Agent dispatch was
	// given - resolved in the airlock and empty for a tool with no mapped
	// argument.
	//
	// It is resolved there rather than by a renderer because the map from
	// tool name to argument key is Claude's vocabulary twice over: the key
	// names (file_path, pattern, command) and the tool names themselves.
	// The decisive case is the subagent dispatch, whose wire name is Agent
	// while init.tools advertises it as Task - a renderer keying on the
	// advertised name would silently show nothing, and only this side of
	// the airlock can know the two differ.
	Display string `json:"display,omitempty"`

	// Title heads the call in place of the tool's name and Display, and is
	// empty for a tool whose own name heads it. Today that is Bash and only
	// Bash: Claude Code heads a shell call with its description and puts the
	// command underneath, and all 33 recorded Bash calls carry both keys.
	Title string `json:"title,omitempty"`

	// Command is the argument drawn *under* the header rather than beside the
	// name - a Bash command, which is too long to sit in a header and is the
	// thing a reader actually wants under a description.
	// The tag is Wake's word rather than Claude's: this file is not one of the
	// airlock's four, so it may not spell a wire key even in a struct tag.
	Command string `json:"shell,omitempty"`

	// Receipt is a one-line stand-in for a folded result body, as a format
	// with a single %d for that body's line count, and "" for a tool whose
	// body is shown instead.
	//
	// Only a tool the corpus records earns one, because a receipt *hides* the
	// body - and that question is already settled one field over, by the rule
	// dm_blocks.go's receiptNote states for a dispatch status: an unmodelled
	// shape must degrade toward more output, never less. Read is the one
	// recorded tool whose body is a count (32 calls, cat -n output). Grep,
	// Glob and Edit appear in no recorded tool_use block in the corpus, so
	// they get none until one does.
	Receipt string `json:"receipt,omitempty"`

	// Todos is the task list a TodoWrite carries, in the order the agent wrote
	// it, and nil for every other call. Resolved here for Display's reason: the
	// key names are Claude's, and so is the vocabulary of statuses.
	Todos []Todo `json:"task_list,omitempty"`

	// Checklist is the one create-or-update a TaskCreate/TaskUpdate carries,
	// and nil for every other call. It is *one op*, not the whole list, because
	// live claude (2.1.240) builds a checklist across many calls where the
	// retired TodoWrite sent the whole list each time - so the accumulation is
	// a fold above the airlock, and Todos above is the snapshot it produces.
	Checklist *ChecklistOp `json:"checklist,omitempty"`

	// Diff is the before and after an edit carries in its own input, or nil
	// for a call that carries neither. Resolved here for the same reason as
	// Display: old_string/new_string are Claude's key names.
	//
	// Keyed on both halves being present rather than on the tool's name, so
	// a tool that does not carry them degrades to its header instead of
	// guessing.
	Diff *ToolDiff `json:"diff,omitempty"`

	// Ask is what an interactive ask is putting to the operator, resolved for
	// the same reason Display and Diff are, and nil on every call that asks
	// nothing - which is nearly all of them.
	//
	// Event.Ask and this field answer different questions and both are needed.
	// The AskKind says *which* of the three an ask is, and a view switches on
	// it; this says *what* it is asking, and a view draws it. Neither can be
	// derived from the other: an AskChoice whose payload this build cannot
	// read still has to render as something, and a resolved payload with no
	// kind beside it would put a renderer back to guessing from its shape.
	//
	// Without it a client cannot answer at all, which is the whole reason it
	// exists rather than being a nicety. An answer is keyed on a question's
	// own text and carries an option's own label (see EncodeAnswer), and both
	// live inside Input - which nothing above the airlock may index. So an
	// operator would be choosing between options they cannot see, and Wake
	// would have nothing to key their choice to.
	Ask *AskDetail `json:"ask,omitempty"`

	// Input is Claude's tool input verbatim, and it is the one field on this
	// type nothing above internal/core may index. It exists for the allow
	// path: EncodeAllow echoes it back as updatedInput, which is the only
	// behaviour any recording covers - and for the answer path, where
	// EncodeAnswer folds the operator's choices into it. Both hand it back
	// whole; neither asks a caller to open it.
	//
	// Display, Diff and Ask exist so no renderer needs to reach in here. If a
	// consumer ever wants a fourth thing out of it, the resolution belongs
	// in protocol.go beside those three - not an input[key] lookup above the
	// airlock, which is exactly the leak airlock_test.go fails the build for.
	Input map[string]any `json:"input,omitempty"`

	IsError bool `json:"is_error,omitempty"`
}

// AskDetail is what an interactive ask is putting to the operator: the
// questions an AskChoice asks, or the plan an AskApproval offers.
//
// Resolved behind the airlock, and resolved rather than passed through. The
// wire's own payload carries one thing more than this - a multi-select flag -
// and that is the one this type deliberately cannot express, because every
// recorded question carries it false and the comma-separated answer encoding
// is matched against Claude Code 2.1.226 with no recording behind it. §9 of the
// question findings says in so many words that Wake must not build against it.
// A multi-select question therefore degrades to one choice, which is a real
// label the tool accepts, rather than to a guess about how two of them are
// joined.
//
// The per-question header and the per-option preview *are* resolved, and were
// not until a question turned out to be answerable in a conversation as well as
// in the room. The old argument against them was the room's - a preview is a
// document and the room is a hub, not a reading room - and it does not reach a
// 1:1 pane, which is where the deciding information belongs. Both are bounded
// where they are drawn rather than here.
//
// The json tags are Wake's own words, not Claude's, and that is the same
// ruling rpc.Frame.UpdatedInput carries: a type spelled the way the far wire
// spells it starts looking like a passthrough, and this one is the opposite of
// a passthrough - it is the subset Wake decided to show.
type AskDetail struct {
	// Questions is what an AskChoice asks, in the order it asked. The tool's
	// schema bounds it at 1-4, and every recording carries 2.
	Questions []Question `json:"asked,omitempty"`

	// Plan is the document an AskApproval offers, as markdown. Empty on every
	// other kind - and legitimately empty on an interactive ask this build
	// does not model, which is why a consumer must degrade rather than assume.
	Plan string `json:"proposal,omitempty"`
}

// Question is one question an AskChoice puts, with the options it offers.
type Question struct {
	// Text is the question's own words, and it is not only what a reader is
	// shown: an answer is a flat map keyed on this exact string (see
	// EncodeAnswer). A view that renders one spelling and answers with another
	// answers nothing, which is why both come from here.
	Text string `json:"text,omitempty"`

	// Header is the two or three words the ask labels this question with -
	// "Format", "Region join". It is a chip rather than a sentence, and it is
	// how a reader tells one question of four from the next when only one is on
	// screen at a time.
	//
	// Presentation only: an answer is keyed on Text, never on this. A view that
	// keyed on it would answer nothing, which is the reason both are here
	// rather than one being derived from the other.
	Header string `json:"chip,omitempty"`

	// Options are what may be chosen. The schema says 2-4 and every recorded
	// question carries 2 or 3.
	Options []Option `json:"choices,omitempty"`
}

// Option is one answer a Question offers.
type Option struct {
	// Label is what the answer carries. EncodeAnswer sends this string
	// verbatim, so it is the option's identity and not a caption for it.
	Label string `json:"label,omitempty"`

	// Detail is the option's own account of what choosing it means - often
	// the deciding information, since two labels can be equally plausible and
	// the consequence is what separates them.
	Detail string `json:"detail,omitempty"`

	// Preview is what choosing this option produces, as the ask wrote it: the
	// recorded ones are a markdown table and the CSV beside it, differing in
	// exactly the way the two labels claim.
	//
	// Multi-line and arbitrary, which is the whole of what a renderer has to
	// know about it. It is text a model wrote, so it is bounded in both
	// directions where it is drawn and never rendered as markdown - a preview
	// through glamour is a per-frame cost on a surface that is pinned until
	// somebody answers it.
	Preview string `json:"sample,omitempty"`
}

// ToolDiff is the two halves of an edit, already unwrapped from the tool's
// input so a renderer never names Claude's keys.
type ToolDiff struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// SubagentResult is what a subagent dispatch's own tool result reported. It
// is set on exactly the events that are a *receipt* for a subagent - never on
// the subagent's forwarded speech - so a non-empty Result is what tells the
// two apart when Subagent is set.
type SubagentResult string

const (
	// SubagentFinished is a foreground dispatch that ran to completion. Its
	// text is a verbatim repeat of the subagent's final forwarded message -
	// byte-identical on the one pair that was compared, and the same shape
	// on all six recorded completions - so a view that has already drawn the
	// forwarded stream must not draw it again.
	//
	// That duplication holds because Wake's own argv always carries
	// --forward-subagent-text (core.Session.buildArgs). Drop that flag and a
	// foreground subagent's prose stops arriving as a forwarded frame
	// (testdata/stream/subagent-no-forward.jsonl), at which point this
	// receipt becomes the only copy and suppressing it would lose the
	// report. Whoever removes the flag owns re-deciding this.
	SubagentFinished SubagentResult = "finished"

	// SubagentLaunched is an async dispatch. The receipt arrives before the
	// subagent has done anything and carries a launch acknowledgement rather
	// than a report - its own text tells the model never to quote it - so it
	// is not the moment the subagent finished. On this path completion
	// arrives later, as a forwarded frame.
	SubagentLaunched SubagentResult = "launched"

	// SubagentUnknown is a receipt naming a status this decoder does not
	// model. Its content is not known to be a duplicate, so it must be shown
	// rather than suppressed: an unmodelled status degrades to more output,
	// never to less.
	SubagentUnknown SubagentResult = "unknown"
)

// Subagent attributes an event to a subagent rather than to the agent whose
// conversation it arrives in.
//
// Without it a subagent's work is the agent's own: 26 of the 44 tool calls in
// the recorded corpus are a subagent's, and a subagent's Bash decodes to
// byte-for-byte the shape a parent's Bash produces. Three concurrent streams -
// the parent's and two subagents' - interleave line by line
// (testdata/stream/subagent-parallel.jsonl), so this is what stops them
// reading as one monologue.
//
// Two identifier spaces, because the wire has two and no single frame joins
// them:
//
//   - Dispatch is the parent tool call that started the subagent. It rides
//     every one of the 80 forwarded frames and equals the id of the parent's
//     own dispatch tool_use, so it discriminates between concurrent
//     subagents.
//   - Agent is the subagent's own id. It rides a subagent's permission ask
//     and its dispatch receipt.
//
// THE JOIN, and it is the one thing three comments in this tree used to
// disagree about, so it is stated once here and referenced from the other
// two (protocol.go's dispatchReceipt, ui's subagentTag).
//
// The two spaces meet on exactly one frame: the dispatch receipt names both,
// its tool_result.tool_use_id being the Dispatch and its
// tool_use_result.agentId the Agent. That is verified on all 9 receipts in
// all 7 fixtures, and it is why a receipt can be tied to the speech it
// repeats with no task_started lookup.
//
// It is also **retrospective**, which is what the other two comments were
// getting at. The receipt is the last frame of a dispatch: it arrives after
// the subagent's speech and after any permission the subagent asked for
// (subagent-permission.jsonl:22 asks, :35 receipts). So a consumer holding an
// ask cannot resolve its Dispatch at the moment it has to decide something,
// and must key on whichever field is set rather than assume the two are
// comparable. Joining them *forward* - knowing at the ask which dispatch is
// asking - needs task_started, which arrives first and which nothing here
// decodes.
//
// Type and Task ride every forwarded frame alongside Dispatch - all 80, no
// exceptions - which is why a view can name a subagent from a single frame
// without waiting for a lifecycle frame or building a lookup table. Whether
// they are present for a subagent type other than the one recorded is
// unverified, so both are omitempty and a consumer must tolerate "".
type Subagent struct {
	Dispatch string         `json:"dispatch,omitempty"`
	Agent    string         `json:"agent,omitempty"`
	Type     string         `json:"type,omitempty"`
	Task     string         `json:"task,omitempty"`
	Result   SubagentResult `json:"result,omitempty"`
}

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
	// NoticeContextCompacted is the conversation being summarised to fit.
	NoticeContextCompacted Notice = "context_compacted"

	// NoticeToolDenied is the after-the-fact report that a tool call was
	// refused - not the ask, which is KindPermissionRequest.
	NoticeToolDenied Notice = "tool_denied"

	// NoticeRateLimited is quota exhaustion, and it is resolved only for a
	// status that is *not* the benign one: the single value every recorded
	// sample carries means nothing is wrong, and drawing it is chrome.
	// Event.Text still carries the status itself.
	NoticeRateLimited Notice = "rate_limited"

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
)

// AskKind is what a KindPermissionRequest wants from the operator, resolved
// in the airlock from the ask's own wire fields.
//
// It exists because one control_request subtype carries three different
// questions. can_use_tool is the envelope for all of them, and Wake answers
// all of them with a control_response allow - but what a *bare* allow means
// differs, and getting that wrong is silent in both directions.
//
// The wire says which, in two fields and never in the tool's name. A tool-name
// allowlist is the wrong shape and this is the field that makes one
// unnecessary: any tool whose permission check asks for interaction reasons
// can set requires_user_interaction, and the two the corpus records are only
// the two that have been recorded.
//
// Resolved here rather than handed up raw for the reason Notice is: a consumer
// switching on Wake's closed set cannot admit a fourth case by editing a map
// in a renderer, and requires_user_interaction alone is *not* the answer - it
// is true on both of the interactive shapes below, which is exactly the trap
// that made Wake right for one tool and wrong for the other.
type AskKind string

const (
	// AskPermission is "may I run this tool": allow or deny, and the answer is
	// the behavior field itself. Every can_use_tool in the 26 recordings that
	// predate the question corpus is one of these, and none of them carries
	// requires_user_interaction at all.
	//
	// It is the empty string so that it is also the zero value: an ask Wake
	// cannot classify degrades to the one shape whose answer has always been
	// complete, and Event.Ask stays absent from the JSON for the great
	// majority of asks.
	AskPermission AskKind = ""

	// AskApproval is an ask that requires a human and whose whole answer is
	// still the allow. ExitPlanMode is the recorded one: it carries
	// requires_user_interaction and a single plan string, there are no options
	// to choose between, and a bare allow comes back as "User has approved
	// your plan." (question-plan-bare.jsonl:76).
	//
	// So this is the case that forbids the obvious fix. Sending updatedInput
	// unconditionally would push an untested shape at the one ask Wake already
	// answers correctly.
	AskApproval AskKind = "approval"

	// AskChoice is an ask carrying questions with options, where the answer
	// rides *inside* the allow as updatedInput.answers. AskUserQuestion is the
	// recorded one.
	//
	// A bare allow here is the defect this kind exists to make visible: the
	// tool runs, the model is told "The user did not answer the questions."
	// (question-bare-allow.jsonl:37), and the turn still ends subtype
	// "success" with permission_denials empty - so nothing downstream can
	// notice on any other field. See EncodeAnswer for the answer that is not
	// lost, and daemon.agent.allow for the report when one is.
	AskChoice AskKind = "choice"
)

// ControlResult is what a KindControlReceipt reports: the message uuids that
// survived the interrupt, and the ones it destroyed.
//
// This is the only place the difference is legible. A cancelled message also
// gets a KindMessageState of "cancelled" - but so does the *running* message
// the interrupt aborted, and nothing on those frames separates the two.
// Cancelled here lists only what the cancel_queued request itself dequeued.
//
// Neither field is omitempty, deliberately. Cancelled is absent unless the
// request asked for it and present-but-empty when it asked and there was
// nothing queued (interrupt-cancel-queued-empty.jsonl), so its presence
// tracks the request and not the outcome. nil marshals to null and an empty
// slice to [], which keeps that distinction alive across internal/rpc;
// omitempty would erase it and leave a client unable to tell "you did not
// ask" from "you asked and nothing was queued".
//
// Error is the reason a control_request was refused, and it is empty on every
// receipt that was not. A refusal is subtype "error" carrying a top-level
// string - an unknown mode, or bypassPermissions on a session not launched
// dangerously - and the reason has to reach a reader, or an operator whose key
// was refused learns only that something failed.
type ControlResult struct {
	StillQueued []string `json:"still_queued"`
	Cancelled   []string `json:"cancelled"`
	Error       string   `json:"error,omitempty"`
}

// Event is the single type every layer above the airlock consumes.
type Event struct {
	Kind      EventKind `json:"kind"`
	SessionID string    `json:"session_id,omitempty"`

	// RequestID correlates an answer with a control_request. Set only on
	// control frames: assistant frames carry an unrelated top-level
	// request_id (the Anthropic API request), which is deliberately dropped.
	//
	// On a KindControlReceipt it is nested at .response.request_id, not
	// top-level, and the CLI will happily omit it if Wake did.
	RequestID string `json:"request_id,omitempty"`

	// MessageID is the uuid Wake stamped on a message it sent, echoed back
	// on a KindMessageState as command_uuid. It is a different id space from
	// RequestID - a message Wake sent, not a control request Wake sent - so
	// it gets its own field rather than sharing one and making a consumer
	// guess which it is holding. Every frame's own top-level uuid is dropped
	// here as it is everywhere else in this decoder.
	MessageID string `json:"message_id,omitempty"`

	// PermissionMode is the mode a session is running in, and it has two
	// observables that are one fact: a KindControlReceipt answering a
	// set_permission_mode, and every KindSystem init. One field rather than
	// two, so a reader reconciling its belief does not have to care which
	// arrived - it distinguishes them by Kind, which it already has.
	//
	// Empty on a refused receipt, because a refusal moved nothing.
	PermissionMode string `json:"permission_mode,omitempty"`

	// Echoed folds the frame's isReplay || isSynthetic: content the
	// transcript replayed or the tooling generated, rather than a human
	// typing it.
	//
	// Recorded, and re-measured over the whole 1004-line corpus: there are
	// **18** KindUserText events and only **2** of them are echoes - the
	// compaction summary and the <local-command-stdout> line, both on
	// compaction.jsonl. The other 16 are not.
	//
	// This comment used to say "exactly two ... and both are echoes", which
	// was true of the 231 lines that existed when it was written and has
	// been false since. It was load-bearing: a renderer reading it concludes
	// that any non-echoed user frame is the human typing, and the 16 say
	// otherwise. They are
	//
	//   - 10 [Request interrupted by user] / [... for tool use] markers,
	//     which are Claude's own abort notice attributed to the human. Those
	//     no longer mis-attribute either: both literals resolve to
	//     NoticeTurnInterrupted, keyed on the text, because the text is the
	//     only thing on the frame that identifies them. Echoed is still false
	//     on all 10 and that is the wire's own answer, not a gap.
	//   - 6 subagent prompts, forwarded user frames carrying a
	//     parent_tool_use_id. Those are no longer mis-attributed: Subagent
	//     is set on every one, so a view can tell whose turn it is without
	//     consulting Echoed at all.
	//
	// Not recorded: what --replay-user-messages emits. The fixtures were
	// captured without it, and §12 of the findings note lists it first
	// under "do not design around these" - frame type, whether the echo is
	// distinguishable from a genuine user turn, and whether it even sets
	// isReplay were all never observed.
	//
	// So do NOT key suppression or de-duplication on Echoed. It is safe
	// for presentation - a label, a style - where being wrong is cosmetic.
	// The single-source rule, that a sent message appears either as a
	// replayed frame or as a local echo and never both, belongs to the App
	// that owns both halves, not to a renderer inferring it from a flag
	// whose behavior nobody has watched.
	//
	// It is a property of the frame, not of the block, so it rides every
	// event a frame produces. That is why this is a field and not a kind:
	// a replayed frame carrying tool_result blocks must stay
	// KindToolResult, or nothing can pair the result with its call.
	Echoed bool `json:"echoed,omitempty"`

	// FromRoom marks the operator's own turn as one routed from the room
	// rather than typed into the conversation it landed in.
	//
	// Never set by the decoder: no frame carries it, and nothing on the wire
	// could - the two acts are indistinguishable by the time claude sees
	// them, because a leading @name is stripped before the frame is written.
	// It is set by the App that owns the routing, on the echo it writes, and
	// it is presentation only for Echoed's reason: a wrong value mislabels a
	// turn, and nothing may key suppression or de-duplication on it.
	FromRoom bool `json:"from_room,omitempty"`

	Text string    `json:"text,omitempty"`
	Tool *ToolCall `json:"tool,omitempty"`

	// OutputTokens is how many tokens the message this event's block belonged
	// to produced, read from the assistant frame's usage - the real figure a
	// surface reports as "how much a response ran to". The whole message's
	// count, attached to one text block of it and 0 on every other kind.
	OutputTokens int `json:"tokens,omitempty"`

	// Ask says what a KindPermissionRequest wants from the operator, and is
	// AskPermission - the zero value, and absent from the JSON - on every
	// other kind. See AskKind: it is the difference between an allow that is
	// the whole answer and an allow that has to carry one.
	Ask AskKind `json:"ask,omitempty"`

	// Subagent is set on every event a subagent produced, and on the receipt
	// the parent gets back for one. Nil means the agent itself. See Subagent
	// for the two identifier spaces and why a renderer must show it.
	Subagent *Subagent `json:"subagent,omitempty"`

	// Notice is the resolved label for an event whose wire identity would
	// otherwise have to be named above the airlock, and "" for the great
	// majority that have none. See Notice.
	Notice Notice `json:"notice,omitempty"`

	// Task is a dispatch's lifecycle reported by a KindSystem event, and nil
	// on every other kind and on every system subtype that is not one of the
	// four. It is not a Notice: a notice is a line in a transcript, and this
	// is a record a view keeps and redraws. See TaskUpdate.
	Task *TaskUpdate `json:"task,omitempty"`

	// Control is the payload of a KindControlReceipt, nil on every other
	// kind - the same pointer-payload shape Tool uses. Rewind is that shape
	// again for KindRewindReceipt; see RewindResult.
	Control *ControlResult `json:"control,omitempty"`
	Rewind  *RewindResult  `json:"rewind,omitempty"`

	// Raw is the whole stream-json line this event was decoded from, kept
	// for in-process debugging. It is excluded from JSON on purpose, so it
	// never reaches a client.
	//
	// Measured over the recorded corpus, Raw is 85.4% of the serialized
	// event stream - 496,634 bytes with it against 72,751 without, 6.8x -
	// because a Claude line is mostly metadata Wake discards. That is a
	// floor, not a worst case: a multi-block frame gives every block its
	// own copy of the same line, so one assistant turn emitting prose plus
	// a tool_use puts the line on the wire twice. Nothing above the airlock
	// is permitted to parse it anyway, so those bytes buy a client nothing.
	//
	// The tag is here rather than a strip inside internal/rpc so the
	// transport stays a dumb pipe: stripping at the transport would make
	// the wire silently lossy, and a Frame constructor would be a rule the
	// compiler cannot enforce. Raw stays fully usable in-process; what
	// stops is shipping it.
	Raw json.RawMessage `json:"-"`

	// At is when a transcript record says this happened, and the zero time on
	// every event that came off the stream.
	//
	// Stamped by DecodeTranscriptLine alone, because the on-disk record is the
	// only thing that carries one - `timestamp`, a key stdout never sends. It
	// exists for one consumer: the room is a fold over many sessions, so
	// restoring it means interleaving several transcripts, and interleaving
	// needs a clock.
	//
	// **The zero value is load-bearing rather than a gap.** A live event has no
	// time and never gets one, which is exactly what lets the room's merge
	// re-order its history without touching a line that arrived while somebody
	// was reading. A line whose timestamp is missing or unparseable reads as
	// zero too: a transcript is a file somebody else writes, and losing a turn
	// over a field the conversation does not depend on is the wrong trade.
	At time.Time `json:"at,omitempty"`

	// Session is what the session itself is running as, when a frame says so.
	// Nil on the conversation frames, which is nearly all of them - only
	// system/init and result carry any of it.
	Session *SessionFacts `json:"session,omitempty"`
}

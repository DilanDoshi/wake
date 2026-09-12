package core

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

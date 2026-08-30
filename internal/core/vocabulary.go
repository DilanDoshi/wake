// Claude's vocabulary, resolved into Wake's - part of the airlock; see
// protocol.go.
//
// Everything here exists so that no file above the airlock has to name a
// Claude string. Each table maps a wire word onto one of Wake's own, and each
// carries the reason it lives behind the airlock rather than in the renderer
// that consumes it.
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

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// --- Claude's vocabulary, resolved -------------------------------------------
//
// Everything in this section exists so that no file above the airlock has to
// name a Claude string. Each table maps a wire word onto one of Wake's own,
// and each has a documented reason for living behind the airlock rather than
// in the renderer that consumes it.

// systemNotice is the closed set of system subtypes a reader is told about.
// Everything else is lifecycle chatter - hook_started and hook_response alone
// are 396 of the 1004 recorded lines, and a view that draws them drowns.
//
// It is a resolution, not an allowlist of raw strings handed upward: the
// value is a Wake constant, so a renderer switches on Wake's vocabulary and a
// new subtype cannot be admitted by editing a map in the UI. See Notice.
var systemNotice = map[string]Notice{
	"compact_boundary":  NoticeContextCompacted,
	"permission_denied": NoticeToolDenied,
}

// The five system subtypes that report a dispatch's life, resolved to the
// phase each one reports. Membership is also what tells a task frame from
// every other system frame, so this map is the decoder's whole test for one.
//
// Four subtypes and three phases: task_updated and task_notification report
// the same ending and differ only in what they carry with it. Collapsing them
// here rather than above the airlock is the point of this file - a view that
// switched on two spellings of "ended" would be holding Claude's vocabulary.
//
// background_tasks_changed is absent deliberately and is not decoded at all:
// it heralds a membership change one line before the authoritative frame and
// carries strictly less - no dispatch, no status, no usage. See
// TestTheHeraldFrameIsRedundant, which holds that ordering to the corpus.
var taskPhases = map[string]TaskPhase{
	"task_started":      TaskStarted,
	"task_progress":     TaskProgress,
	"task_updated":      TaskEnded,
	"task_notification": TaskEnded,
}

// What a task_type means. Two are recorded, and the CLI's own wording names
// kinds this corpus has never produced - so an absent key is expected traffic
// rather than a defect, and resolves to TaskKindUnknown by the zero value
// being the wrong answer: see taskKind, which cannot use the bare lookup.
var taskKinds = map[string]TaskKind{
	"local_agent": TaskAgent,
	"local_bash":  TaskShell,
}

// How a task ended. `completed` rides nine frames; `stopped` and `killed` ride
// one each, both from the single background shell that was interrupted, and
// they are the same event reported by the two different terminal frames.
//
// Nothing else may be added here without a recording. An unmapped word is
// TaskStatusUnknown, which is the only honest reading of an ending nobody has
// seen - and unlike a Notice, where the cost of an unknown is a missing label,
// the cost here is a row claiming a subagent succeeded when it did not.
var taskStatuses = map[string]TaskStatus{
	"completed": TaskDone,
	"stopped":   TaskStopped,
	"killed":    TaskStopped,
}

// The permission modes Wake sets a running session to.
//
// They live here for the reason everything else in this file does: a mode word
// is Claude's, it travels as JSON in a set_permission_mode request and comes
// back in that request's receipt, and a file above the airlock that spelled one
// would be comparing its own state against Claude's English. internal/ui holds
// a Mode of its own and resolves it through these.
//
// This is a *subset*, and deliberately not the whole accepted set. The CLI
// names six - acceptEdits, auto, bypassPermissions, default, dontAsk, plan
// (permission-mode-findings.md §6) - and five are spelled here.
// bypassPermissions is not, because it is refused outright unless the process
// was launched with --dangerously-skip-permissions (§7), a floor Wake gets for
// free because nothing in this tree passes that flag. A word for a mode no
// session can be in is a word nothing may send.
//
// Which of the five ⇧⇥ walks is internal/ui/mode.go's ruling and not this
// file's: four are cycle positions and PermissionModeDontAsk is an exit.
//
// PermissionModeDefault is spelled the way the *receipt* spells it. `manual` is
// accepted and silently normalizes to `default`, so a cycle that sent `manual`
// would get back a word its own label could not match - I7's defect wearing a
// new hat. Sending the receipt's own word is what dissolves that rather than
// handling it.
const (
	PermissionModePlan        = "plan"
	PermissionModeAuto        = "auto"
	PermissionModeDefault     = "default"
	PermissionModeAcceptEdits = "acceptEdits"
	PermissionModeDontAsk     = "dontAsk"
)

// subtypeInit is the system frame that opens a session and is the only one
// naming its model. It arrives once per *turn*, not once per process - see
// CLAUDE.md on result and init both being per-turn.
const subtypeInit = "init"

// rateLimitAllowed means nothing is wrong, and was the only
// rate_limit_info.status recorded until partial-turn.jsonl (2026-08-21)
// carried the corpus's one "allowed_warning" — which the switch below draws
// as rate-limited, a ruling made before any such status existed. Whether a
// warning deserves a softer sentence is an open product call; see the
// fixture's findings note.
const rateLimitAllowed = "allowed"

// rateLimitNotice resolves a quota status. Only a status that is *not* the
// benign one earns a notice; the status string itself still reaches a
// consumer as Event.Text, so a value nobody has seen is reported rather than
// swallowed.
func rateLimitNotice(status string) Notice {
	if status == "" || status == rateLimitAllowed {
		return ""
	}
	return NoticeRateLimited
}

// primaryArg is the one argument worth showing beside a tool's name.
// Everything else is noise at a glance.
//
// It lives here rather than in internal/render because both halves of it are
// Claude's vocabulary: the argument keys and the tool names. Agent is the
// case that decides it - the wire name for a subagent dispatch is Agent while
// init.tools advertises Task, so a renderer keying on the advertised name
// would silently render a dispatch with no argument at all, which is what it
// did. Task is deliberately absent: no tool_use block in 1004 recorded lines
// is named Task, and adding a key on the strength of a tools list would be
// designing around a shape no recording shows.
var primaryArg = map[string]string{
	"Bash":      "command",
	"Read":      "file_path",
	"Edit":      "file_path",
	"Write":     "file_path",
	"Glob":      "pattern",
	"Grep":      "pattern",
	"WebFetch":  "url",
	"WebSearch": "query",
	"Agent":     "description",
}

// toolShape is how a tool's block is drawn beyond the one argument primaryArg
// picks: the input key whose value heads the line in place of the tool's name,
// the key drawn under the header, and what a folded result collapses to.
//
// Claude's vocabulary again, so it lives here for primaryArg's reason.
type toolShape struct {
	title   string // input key
	under   string // input key
	receipt string // a format with one %d for the result's line count
}

// toolShapes carries only tools the corpus records. A receipt hides a result
// body, so an unrecorded shape has not earned one - see ToolCall.Receipt.
var toolShapes = map[string]toolShape{
	"Bash": {title: detailKey, under: "command"},
	"Read": {receipt: "Read %d lines"},
}

// MCP server statuses, as claude spells them on the init frame.
//
// Wake's words rather than the caller's: internal/ui never spells one, which is
// the rule the permission modes already carry. Three values and no more - every
// one of the 523 server rows across 60 recorded fixtures is one of these.
const (
	MCPConnected = "connected"
	MCPNeedsAuth = "needs-auth"
	MCPPending   = "pending"
)

// The two halves of an edit, as Claude names them in the tool's own input.
const (
	diffOldKey = "old_string"
	diffNewKey = "new_string"

	// The task list, whose shape is stated by the tool's own description in
	// claude 2.1.229: each todo has content, status ("pending" | "in_progress"
	// | "completed") and activeForm, a present-tense label shown while the item
	// is the one being worked on.
	todosKey      = "todos"
	todoTextKey   = "content"
	todoStateKey  = "status"
	todoActiveKey = "activeForm"

	// The three statuses, resolved below into Wake's own vocabulary so that
	// nothing above the airlock compares against these words.
	wireTodoActive = "in_progress"
	wireTodoDone   = "completed"

	// The live checklist tools. TodoWrite is retired in 2.1.240 (off unless
	// CLAUDE_CODE_ENABLE_TASKS is false); its replacement builds a list from a
	// run of TaskCreate/TaskUpdate calls. Named rather than payload-keyed like
	// toolTodos: create and update share a generic `subject`, and the
	// create-vs-update distinction *is* the tool identity - only the name tells
	// a create that also happens to name a status from an update.
	toolTaskCreate = "TaskCreate"
	toolTaskUpdate = "TaskUpdate"
	taskSubjectKey = "subject"
	taskIDKey      = "taskId"
	wireTodoGone   = "deleted"
)

// The three keys an interactive ask and its answer are spelled with.
//
// questionsKey rides the ask's input and is echoed back unchanged inside the
// answer; answersKey is the key the answer adds; questionKey is one question's
// own text, which is what answersKey maps from. All three are recorded, on the
// ask at question-answer.jsonl:37 and on the CLI's echo of the answer it
// received at :38.
//
// They are consts here rather than literals in encode.go because the decoder
// and the encoder both have to spell questionsKey the same way: askKind reads
// it to classify an ask, and answeredInput reads it to check that the answer
// is being built against the questions that were actually asked. Two spellings
// of one word across the airlock is how a decoder starts disagreeing with its
// own encoder.
const (
	questionsKey = "questions"
	answersKey   = "answers"
	questionKey  = "question"
)

// The rest of an interactive ask's payload, read only to resolve AskDetail.
//
// detailKey is Claude's "description", which primaryArg already reads for an
// Agent dispatch - one word, two unrelated jobs, and both are here rather than
// anywhere else for that reason. planKey is the whole payload of an
// ExitPlanMode ask.
//
// Deliberately absent: multiSelect, and only that. AskDetail's doc comment
// carries why, and the short version is that its answer encoding has no
// recording behind it.
const (
	optionsKey = "options"
	labelKey   = "label"
	detailKey  = "description"
	planKey    = "plan"
	headerKey  = "header"
	previewKey = "preview"
)

// askKind classifies a permission ask by what it needs from the operator. See
// AskKind for why one control_request subtype needs three answers, and why
// this reads two wire fields rather than the tool's name.
//
// The order is the argument. requires_user_interaction first, because without
// it the ask is an ordinary "may I run this" whatever its input holds - that
// is every ask in the corpus that predates the question recordings. Then the
// payload, because the flag is true on both interactive shapes and only the
// one carrying questions has an answer that can be lost.
//
// An interactive ask carrying no questions is AskApproval, which is
// ExitPlanMode's recorded behaviour and is also the safe default for an
// interactive tool nobody has recorded: it says "a human must decide this" and
// claims nothing about a payload this decoder has never seen.
func askKind(r *wireControlReq) AskKind {
	if !r.RequiresUserInteraction {
		return AskPermission
	}
	if _, ok := r.Input[questionsKey]; ok {
		return AskChoice
	}
	return AskApproval
}

// askDetail resolves what an interactive ask is putting to the operator, and
// returns nil for an ordinary permission ask, which puts nothing.
//
// Keyed on the kind askKind already resolved rather than on the payload again,
// so the two cannot disagree about what an ask is: a payload that looks like
// questions on an ask the wire did not mark interactive is not a question, and
// reading the input twice is how a decoder starts arguing with itself.
//
// A kind whose payload this build cannot read yields nil rather than an empty
// shell. Nil is the shape a renderer already has to handle - it is what every
// ordinary permission ask carries - so an unmodelled ask degrades to a yes/no
// on a named tool instead of to a card with an empty body.
func askDetail(kind AskKind, input map[string]any) *AskDetail {
	switch kind {
	case AskChoice:
		if qs := askQuestions(input); len(qs) > 0 {
			return &AskDetail{Questions: qs}
		}
	case AskApproval:
		if plan, ok := input[planKey].(string); ok && plan != "" {
			return &AskDetail{Plan: plan}
		}
	case AskPermission:
	}
	return nil
}

// askQuestions reads the questions an ask put, dropping any that carry no text
// of their own.
//
// Dropping rather than keeping an untitled one is the only safe direction: the
// text is the key an answer is keyed on, so a question with none can be shown
// and never answered - and EncodeAnswer refuses the whole answer when one
// question is unanswerable, which would take the answerable ones down with it.
func askQuestions(input map[string]any) []Question {
	raw, ok := input[questionsKey].([]any)
	if !ok {
		return nil
	}
	out := make([]Question, 0, len(raw))
	for _, q := range raw {
		obj, ok := q.(map[string]any)
		if !ok {
			continue
		}
		text, ok := obj[questionKey].(string)
		if !ok || text == "" {
			continue
		}
		// The header is presentation and the text is the key an answer is keyed
		// on, so a question with no header is still perfectly answerable and is
		// kept. Only a missing text drops one.
		header, _ := obj[headerKey].(string)
		out = append(out, Question{Text: text, Header: header, Options: askOptions(obj[optionsKey])})
	}
	return out
}

// askOptions reads one question's options. An option with no label is dropped
// for the reason an untitled question is: the label is what the answer
// carries, so an unlabelled one is a row that cannot be chosen.
func askOptions(raw any) []Option {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]Option, 0, len(list))
	for _, o := range list {
		obj, ok := o.(map[string]any)
		if !ok {
			continue
		}
		label, ok := obj[labelKey].(string)
		if !ok || label == "" {
			continue
		}
		detail, _ := obj[detailKey].(string)
		preview, _ := obj[previewKey].(string)
		out = append(out, Option{Label: label, Detail: detail, Preview: preview})
	}
	return out
}

// The envelope Claude wraps a slash command's stdout in. The wrapper is
// transcript plumbing; the output inside it is not, and neither is anything a
// reader should be shown.
const (
	localStdoutOpen  = "<local-command-stdout>"
	localStdoutClose = "</local-command-stdout>"
)

// The two strings Claude writes into the transcript when a turn is aborted:
// the first when it was generating, the second when a tool was running. Both
// are in the 2.1.226 binary and both are recorded - five and three of the
// eight markers in the corpus - so anything matching on one has to match on
// the other.
//
// They are matched exactly rather than by prefix. A third wording would go
// unresolved and be drawn as the user's turn, which is the behaviour this
// replaces and is recoverable; a prefix would let a person who typed one of
// these have their own message replaced by a label, which is not. The bytes
// are the authority and the bytes record two.
const (
	interruptedMarker     = "[Request interrupted by user]"
	interruptedToolMarker = "[Request interrupted by user for tool use]"
)

// interruptNotice resolves Claude's abort marker, and returns "" for every
// other run of user text.
//
// It is keyed on the text because the text is the only evidence there is. The
// frame carries no subtype, no isSynthetic and no isReplay - its key set is
// identical to a genuine user turn's - so this is the one notice that cannot
// be resolved from the envelope. Applied to the user's side only, which is the
// only side either literal has ever appeared on.
func interruptNotice(kind EventKind, text string) Notice {
	if kind != KindUserText {
		return ""
	}
	if text == interruptedMarker || text == interruptedToolMarker {
		return NoticeTurnInterrupted
	}
	return ""
}

// A bare /model reply names the session's model and reasoning level:
//
//	Current model: Opus 5 (1M context) (effort: xhigh)
//
// It is the one place a session's effort is reported back at all
// (testdata/stream/bare-model.jsonl) - nothing Wake receives unasked carries
// it - so the daemon sends the bare command and reads the level out of this
// line to confirm the effort it asked for. Recognised here because it is
// Claude's rendered output shape, which is exactly what the airlock quarantines.
const modelReplyPrefix = "Current model:"

// effortClause matches the parenthesised level in a /model reply.
var effortClause = regexp.MustCompile(`\(effort:\s*([a-zA-Z]+)\)`)

// IsModelReply reports whether text is a bare /model reply. Used to suppress the
// probe's own frames and to filter its line out of a restored transcript.
func IsModelReply(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), modelReplyPrefix)
}

// EffortFromModelReply reads the reasoning level out of a /model reply, or
// reports false when there is no clause or the level is not one /effort takes.
func EffortFromModelReply(text string) (string, bool) {
	m := effortClause.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	level := strings.ToLower(m[1])
	if !ValidEffortCommand(level) {
		return "", false
	}
	return level, true
}

// toolCall builds the ToolCall for an invocation, resolving the display
// argument and the diff so nothing above has to index Input.
func toolCall(id, name string, input map[string]any) *ToolCall {
	shape := toolShapes[name]
	return &ToolCall{
		ID:        id,
		Name:      name,
		Display:   primaryArgOf(name, input),
		Title:     stringArg(input, shape.title),
		Command:   stringArg(input, shape.under),
		Receipt:   shape.receipt,
		Diff:      toolDiff(input),
		Todos:     toolTodos(input),
		Checklist: toolChecklistOp(name, input),
		Input:     input,
	}
}

// toolChecklistOp unwraps the one create-or-update a TaskCreate/TaskUpdate
// carries, and nil for every other call.
//
// Gated on the tool name for the reason stated at toolTaskCreate: unlike the
// TodoWrite list, there is no collision-free payload key that separates a
// create from an update, and the two are one concept the fold reassembles.
// A create with no subject is dropped - a nameless checklist row reads as a
// task nobody named, the rule toolTodos already keeps.
func toolChecklistOp(name string, input map[string]any) *ChecklistOp {
	switch name {
	case toolTaskCreate:
		subject, _ := input[taskSubjectKey].(string)
		if subject == "" {
			return nil
		}
		active, _ := input[todoActiveKey].(string)
		return &ChecklistOp{Text: subject, ActiveForm: active, Status: TodoPending}
	case toolTaskUpdate:
		id, _ := input[taskIDKey].(string)
		if id == "" {
			return nil
		}
		subject, _ := input[taskSubjectKey].(string)
		active, _ := input[todoActiveKey].(string)
		state, _ := input[todoStateKey].(string)
		if state == wireTodoGone {
			// A delete drops the item, so it carries no status of its own -
			// resolving "deleted" through todoStatus would land it at pending, a
			// value the fold never reads and the next caller might.
			return &ChecklistOp{Update: true, ID: id, Deleted: true}
		}
		return &ChecklistOp{
			Update:     true,
			ID:         id,
			Text:       subject,
			ActiveForm: active,
			Status:     todoStatus(state),
		}
	default:
		return nil
	}
}

// stringArg is one input value as a string, and "" for a key this tool has no
// shape for or an input that omits it.
func stringArg(input map[string]any, key string) string {
	if key == "" {
		return ""
	}
	v, ok := input[key].(string)
	if !ok {
		return ""
	}
	return v
}

// primaryArgOf returns the argument worth showing for this tool, or "" when
// the tool has no mapped argument or the input omits it.
func primaryArgOf(name string, input map[string]any) string {
	key, ok := primaryArg[name]
	if !ok {
		return ""
	}
	v, ok := input[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(v)
}

// toolDiff unwraps the before and after an edit carries in its own input.
// Both halves must be present and both must be strings, so a tool that
// carries neither - Bash, Read, Write - yields nil rather than a half diff.
func toolDiff(input map[string]any) *ToolDiff {
	oldStr, ok := input[diffOldKey].(string)
	if !ok {
		return nil
	}
	newStr, ok := input[diffNewKey].(string)
	if !ok {
		return nil
	}
	return &ToolDiff{Old: oldStr, New: newStr}
}

// toolTodos unwraps the task list a TodoWrite carries, and nil for a call that
// carries none.
//
// Keyed on the payload rather than on the tool's name, the way toolDiff is: the
// list is recognisable from its own shape, and a name test would be one more
// place Claude's vocabulary decides what a renderer draws. An entry missing its
// text is dropped rather than drawn empty - a blank row in a checklist reads as
// a task nobody named.
//
// Status is passed through as it arrives. The three values are known, but the
// set is Claude's and an unrecognised one has to reach a reader as *something*
// rather than silently becoming "pending" - see render, which draws an unknown
// status as pending's glyph and says so there.
func toolTodos(input map[string]any) []Todo {
	raw, ok := input[todosKey].([]any)
	if !ok {
		return nil
	}
	out := make([]Todo, 0, len(raw))
	for _, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text, _ := fields[todoTextKey].(string)
		if text == "" {
			continue
		}
		state, _ := fields[todoStateKey].(string)
		active, _ := fields[todoActiveKey].(string)
		out = append(out, Todo{Text: text, Status: todoStatus(state), ActiveForm: active})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// todoStatus resolves Claude's word into Wake's.
//
// Anything unrecognised is TodoPending, and that is the ruling rather than a
// fallback: the set is Claude's and can grow, an item nobody has ruled on is
// still one to do, and the alternative - a fourth state meaning "unknown" -
// would put a word no renderer knows how to draw in front of a reader. It is
// resolved here rather than above because these are Claude's strings, and a
// renderer comparing against them is the leak this airlock exists to stop.
func todoStatus(raw string) TodoStatus {
	switch raw {
	case wireTodoActive:
		return TodoActive
	case wireTodoDone:
		return TodoDone
	default:
		return TodoPending
	}
}

// stripLocalCommandStdout unwraps the envelope around a slash command's
// output (compaction.jsonl:36, the only line in 1004 that carries it).
// Applied to user text only, which is the only side of the transcript it has
// ever appeared on.
func stripLocalCommandStdout(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, localStdoutOpen) || !strings.HasSuffix(t, localStdoutClose) {
		return s
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, localStdoutOpen), localStdoutClose))
}

// forwardedSubagent attributes a frame the CLI forwarded from a subagent, and
// returns nil for one the agent itself produced.
//
// parent_tool_use_id is the discriminator and the other two are the name:
// measured over the corpus, 80 frames carry a non-null parent_tool_use_id and
// every one of those 80 also carries subagent_type and task_description,
// while none of the 40 parent frames carries any of the three.
//
// Called per block rather than per frame so no two events share a pointer -
// Event is a value type everywhere above here and a shared *Subagent would
// make one block's edit visible on its siblings.
func forwardedSubagent(f wireFrame) *Subagent {
	if f.ParentToolUseID == "" {
		return nil
	}
	return &Subagent{
		Dispatch: f.ParentToolUseID,
		Type:     f.SubagentType,
		Task:     f.TaskDescription,
	}
}

// dispatchReceipt reads the tool result the *parent* gets back when a
// subagent it dispatched finishes or launches, and returns nil for every
// other tool result.
//
// The signal is tool_use_result.agentId: present on exactly the 9 dispatch
// receipts in the corpus and on none of the other 35 tool results. The
// tool_result block itself names only a tool_use_id, so without this key the
// frame reporting a subagent's completion is indistinguishable from any other
// result - and its content is a verbatim repeat of prose the reader has
// already been shown.
//
// Dispatch comes from the block's own tool_use_id, and that is the join the
// findings note reports as missing. Checked over all 9 receipts in all 7
// subagent fixtures: the receipt's tool_use_id is always one of that file's
// Agent dispatch ids, and equals the parent_tool_use_id its forwarded frames
// carried. So a receipt and the speech it summarises can be tied to the same
// subagent from these two frames alone, with no task_started lookup - which
// §7 of the note believed was required. Agent is set as well, from agentId,
// which equals the agent_id on that subagent's permission ask
// (ab1b72d53680ae187, subagent-permission.jsonl:22 and :35).
//
// So this frame is where the two identifier spaces meet - and it is the
// *last* frame of the dispatch, which is the whole of the qualification.
// Subagent's doc comment states the consequence once and is the place to
// read: a receipt joins backward, nothing joins forward.
func dispatchReceipt(f wireFrame, b wireBlock) *Subagent {
	if !isJSONObject(f.ToolUseResult) {
		return nil
	}
	var r wireToolUseResult
	if err := json.Unmarshal(f.ToolUseResult, &r); err != nil || r.AgentID == "" {
		return nil
	}
	return &Subagent{
		Dispatch: b.ToolUseID,
		Agent:    r.AgentID,
		Type:     r.AgentType,
		Result:   dispatchResult(r.Status),
	}
}

// dispatchResult maps the receipt's status onto what a reader needs to know:
// whether this frame is a completed subagent's report (and therefore a
// duplicate of prose already drawn) or a launch acknowledgement for one that
// has not started work yet.
//
// An unrecorded status becomes SubagentUnknown rather than either of them,
// because both known values license *hiding* something and a status nobody
// has seen must not inherit that licence.
func dispatchResult(status string) SubagentResult {
	switch status {
	case dispatchCompleted:
		return SubagentFinished
	case dispatchLaunched:
		return SubagentLaunched
	default:
		return SubagentUnknown
	}
}

// toolResultText renders a tool_result block's content as prose.
//
// The content is a bare string on 34 of the 44 recorded tool results and an
// array of blocks on the other 10 - and the array shape is not exotic: it is
// what every one of the 9 subagent dispatch receipts carries, the single most
// important frame a subagent produces. Handing that array to jsonString put a
// JSON literal in front of the reader ("[{\"type\":\"text\",\"text\":\"All
// four files end with…"), which is the one thing the airlock exists to
// prevent, arriving through it rather than around it.
//
// Text and image blocks contribute; an image reads as ImagePlaceholder, the
// same text blockEvent gives a decoded image on a user turn, because a Read of
// a PNG records its tool_result as a lone image block and dropping it rendered
// the result as nothing. The corpus also holds one array with neither - a lone
// tool_reference (interrupt-cancel-queued-empty.jsonl:28) - and that still
// yields "" rather than its JSON: a result with no prose has no prose to show,
// and its ⏺ header still names the call, so the event is not lost. Modelling
// tool_reference itself would mean designing a field around a shape seen once
// whose meaning is unestablished.
func toolResultText(content json.RawMessage) string {
	if !isJSONArray(content) {
		return jsonString(content)
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, rb := range blocks {
		var b struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(rb, &b); err != nil {
			continue
		}
		switch {
		case b.Type == blockTypeText && b.Text != "":
			parts = append(parts, b.Text)
		case b.Type == blockTypeImage:
			parts = append(parts, ImagePlaceholder)
		}
	}
	return strings.Join(parts, "\n\n")
}

// The frames Wake writes back to a session - part of the airlock; see
// protocol.go.
//
// The airlock is these six files and nothing else in Wake knows Claude
// Code's stream-json format:
//
//	protocol.go    decoding - one wire line in, core.Events out
//	wire.go        the shapes it decodes into
//	vocabulary.go  Claude's words resolved into Wake's
//	encode.go      the frames Wake writes back
//	localreply.go  the text replies of local commands Wake parses
//	control.go     control requests Wake writes, and their receipts
//
// internal/core/airlock_test.go enforces that over the whole tree and reads
// the same list. protocol.go's header carries the full rule.

package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// --- encoding ---------------------------------------------------------------
//
// PROBE-DERIVED, NOT FIXTURE-DERIVED - unlike everything the rest of the
// airlock decodes. Wake writes these frames on stdin and never reads
// them, so a recording of stdout cannot contain them whatever its size. The
// corpus does hold 12 control_response lines, and they are not these: those
// are Claude's receipts coming back, the same wire word travelling the other
// way. These are transcribed from the probe that drove the recordings,
// written up in docs/superpowers/notes/2026-08-08-stream-json-findings.md §6
// and §11, and for the interrupt in
// docs/superpowers/notes/2026-08-08-interrupt-findings.md §2.
//
// What the corpus does prove is their effect. The user frame below is the
// shape written to stdin for every recording, so every transcript in
// testdata/stream is a reply to one. The allow frame's tool ran
// (permission.jsonl). The deny frame's message came back verbatim as
// tool_result content (permission-deny-response.jsonl:19). That is strong
// evidence and it is still not a recorded byte: anything changed here is
// unverified until a session is re-recorded.
//
// Outbound frames also get their own types. The inbound envelope keeps
// Message and Content raw to survive Claude's polymorphism, which makes it
// useless for constructing a frame.

type outUserFrame struct {
	Type    string         `json:"type"`
	Message outUserMessage `json:"message"`
	// UUID is the top-level command uuid. Stamping it is what makes the CLI emit
	// command_lifecycle frames (queued/started/completed/cancelled) for this
	// message - an unstamped one produces none - which is how Wake tracks the fate
	// of what it sent. omitempty so an unstamped send is byte-identical to before.
	UUID string `json:"uuid,omitempty"`
	// Set only on a send-now. "now" with a human origin moves running work to
	// the background and is read in the same turn; without the origin it ends
	// the turn instead (midturn-now-human.jsonl, midturn-now-bare.jsonl).
	Priority string     `json:"priority,omitempty"`
	Origin   *outOrigin `json:"origin,omitempty"`
}

type outOrigin struct {
	Kind string `json:"kind"`
}

type outUserMessage struct {
	Role string `json:"role"`
	// Content is a polymorphic block array: image blocks then a text block,
	// never a mix in the other order. []any rather than a typed slice because
	// the two element types are disjoint and JSON marshals each by its own
	// shape - the delta the image findings note said the write path needed.
	Content []any `json:"content"`
}

type outTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// outImageBlock and outImageSource are the wire shape of one attached image,
// recorded in testdata/input/image-block.stdin.jsonl. Only base64 sources are
// budgeted and downscaled by Claude, so it is the only source type Wake writes.
type outImageBlock struct {
	Type   string         `json:"type"`
	Source outImageSource `json:"source"`
}

type outImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// EncodeUserMessage renders one user message as a stream-json line. Written
// while a turn runs, claude reads it at the next tool boundary, or as the next
// turn if this one ends first (midturn-absent.jsonl, midturn-text-next.jsonl);
// now asks for it at once. See docs/superpowers/notes/
// 2026-10-02-mid-turn-delivery-findings.md.
//
// Three rules from the recorded corpus, all in
// docs/superpowers/notes/2026-08-15-image-input-findings.md: images go first
// and the text block last (Claude derives the prompt from the final block), an
// empty content array is silently dropped so a message with neither text nor an
// image is refused here, and the base64 is handed over raw for Claude to budget.
func EncodeUserMessage(text string, images []ImageBlock, uuid string, now bool) ([]byte, error) {
	content := make([]any, 0, len(images)+1)
	for _, img := range images {
		content = append(content, outImageBlock{
			Type:   "image",
			Source: outImageSource{Type: "base64", MediaType: img.MediaType, Data: img.Data},
		})
	}
	if text != "" {
		content = append(content, outTextBlock{Type: "text", Text: text})
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("%w: encode user message: nothing to send", ErrNotWritten)
	}
	frame := outUserFrame{
		Type:    "user",
		Message: outUserMessage{Role: "user", Content: content},
		UUID:    uuid,
	}
	if now {
		frame.Priority, frame.Origin = "now", &outOrigin{Kind: "human"}
	}
	return marshalLine(frame, "encode user message")
}

// Permission decisions. "allow" and "deny" are the two behaviors
// --permission-prompt-tool stdio accepts.
const (
	behaviorAllow = "allow"
	behaviorDeny  = "deny"
)

type outControlResponse struct {
	Type     string         `json:"type"`
	Response outControlBody `json:"response"`
}

// outControlBody nests the subtype and the request id one level down, where
// a control frame keeps them - see wireFrame.RequestID for the same trap
// read from the other direction. Subtype "success" is transport-level ("an
// answer, not a protocol error"), not a verdict: a deny is a successful
// control response carrying a refusal.
type outControlBody struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  outPermDecision `json:"response"`
}

type outPermDecision struct {
	Behavior     string         `json:"behavior"`
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	Message      string         `json:"message,omitempty"`
}

// EncodeAllow answers a permission request with yes. updatedInput is the
// input the tool will actually receive; nil omits the key and runs the tool
// exactly as asked. The probe only ever echoed request.input back unchanged,
// so sending a *different* input, or an empty one, is untested.
//
// omitempty collapses a nil map and an empty one to the same absent key, which
// is deliberate: {} is the untested shape and this cannot express it. That
// holds unchanged now that EncodeAnswer exists - an answer is a different
// frame with a different constructor, not a flag on this one - so a caller
// that has nothing to add still cannot accidentally say "run it with no
// arguments at all".
//
// It stays the right answer for an AskApproval: ExitPlanMode carries
// requires_user_interaction and a bare allow from here is a complete approval
// (question-plan-bare.jsonl:76). It is the *wrong* answer for an AskChoice,
// where the operator's choices are lost silently - see EncodeAnswer, and
// daemon.agent.allow for the report when that happens anyway.
func EncodeAllow(requestID string, updatedInput map[string]any) ([]byte, error) {
	return encodeControlResponse(requestID, outPermDecision{
		Behavior:     behaviorAllow,
		UpdatedInput: updatedInput,
	})
}

// EncodeAnswer answers an AskChoice - an ask carrying questions - with the
// operator's choices. answers maps a question's own text to the label of the
// option chosen for it.
//
// It is an allow. There is no separate answer frame on Claude's wire: the
// behavior is still "allow" and the answer rides in updatedInput beside the
// questions echoed back unchanged, which is the shape the CLI's own receipt
// confirms it received (question-answer.jsonl:38's tool_use_result). Sending
// the allow without it is not a degraded answer, it is *no* answer - the model
// is told "The user did not answer the questions." on a turn that still ends
// subtype "success" (question-bare-allow.jsonl:37).
//
// That is why every refusal below is an error rather than a best effort. This
// is the one path in the airlock where writing a well-formed frame is
// indistinguishable, from every side, from writing the right one - so the
// checks are the only place an answer that would not arrive can still be
// reported to the person who gave it. They wrap ErrNotWritten: nothing reached
// stdin, so the ask is still outstanding and still answerable.
//
// asked is the ask's own input, passed back whole rather than rebuilt. Nothing
// above the airlock may index it (ToolCall.Input says so), and nothing needs
// to: this is the only file that knows which key the questions are under.
func EncodeAnswer(requestID string, asked map[string]any, answers map[string]string) ([]byte, error) {
	input, err := answeredInput(asked, answers)
	if err != nil {
		return nil, err
	}
	return encodeControlResponse(requestID, outPermDecision{
		Behavior:     behaviorAllow,
		UpdatedInput: input,
	})
}

// answeredInput builds the updatedInput an answer rides in: the ask's own
// input with the choices added under Claude's key for them.
//
// The copy is not a style preference. asked is the caller's map - in practice
// the one hanging off a ToolCall that a renderer is still drawing - and
// writing into it would make an answer mutate the ask it answers.
func answeredInput(asked map[string]any, answers map[string]string) (map[string]any, error) {
	questions, err := askedQuestions(asked)
	if err != nil {
		return nil, err
	}
	if err := checkAnswers(questions, answers); err != nil {
		return nil, err
	}
	input := make(map[string]any, len(asked)+1)
	for k, v := range asked {
		input[k] = v
	}
	input[answersKey] = answers
	return input, nil
}

// askedQuestions reads the question texts out of the ask's own input. They are
// what the answers have to be keyed on: answers is a flat map from a question's
// text to a chosen label, so a text that does not match one the ask put is an
// answer to nothing.
func askedQuestions(asked map[string]any) ([]string, error) {
	raw, ok := asked[questionsKey].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("%w: this ask carries no questions, so an allow is already its whole answer", ErrNotWritten)
	}
	texts := make([]string, 0, len(raw))
	for _, q := range raw {
		obj, ok := q.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: a question in this ask is not an object", ErrNotWritten)
		}
		text, ok := obj[questionKey].(string)
		if !ok || text == "" {
			return nil, fmt.Errorf("%w: a question in this ask has no text, so nothing can be keyed to it", ErrNotWritten)
		}
		texts = append(texts, text)
	}
	return texts, nil
}

// defaultDenyReason stands in when a caller denies without saying why.
//
// Message is omitempty, so an empty reason leaves the key off the wire
// entirely; a whitespace-only one is worse, since it survives omitempty and
// reaches the model as "Error:    ". Either way the agent learns it was
// refused but not what to do instead, and the likeliest next move from an
// unexplained refusal is to retry the identical call - a live-lock on the
// one path in the protocol that blocks the process, paid for by the
// operator.
//
// The text is deliberate on three counts. It hedges rather than predicts: a
// reasonless deny usually means haste, and a hasty denier is the one most
// likely to approve the retry, so promising a second refusal risks being
// falsified inside the same context window - which teaches the model to
// discount every later sentence this file sends it, including the true ones.
// It says "a human operator", a noun the model can resolve, because nothing
// in the session explains what Wake is. And it names the two moves Wake
// actually leaves open, because a bare stop trades the live-lock for an
// abandoned path: assistant text reaches the operator in the chat view, so
// asking is real rather than advice into the void.
//
// This is the only layer that can know to do any of it: the only one that
// knows both that the field is omitempty and that its contents reach the
// model verbatim. A caller above the airlock has no way to discover either.
const defaultDenyReason = "A human operator denied this tool call through Wake and gave no reason. Do not retry it unchanged - it is unlikely to be approved. Ask what they want changed, or take a different approach."

// EncodeDeny answers a permission request with no. The reason is echoed
// verbatim to the model as the tool result, so it is a channel for telling
// the agent why - not just that - it was refused; a blank one falls back to
// defaultDenyReason rather than going out silent. The turn still ends
// subtype "success": a denial is not a turn failure.
func EncodeDeny(requestID, reason string) ([]byte, error) {
	// Blank, not empty: the likeliest caller is a UI text field where the
	// operator hit space and then enter, and " " clears omitempty. The trim
	// decides only whether the reason is blank - a reason with any content
	// goes out exactly as written.
	message := reason
	if strings.TrimSpace(message) == "" {
		message = defaultDenyReason
	}
	return encodeControlResponse(requestID, outPermDecision{
		Behavior: behaviorDeny,
		Message:  message,
	})
}

func encodeControlResponse(requestID string, d outPermDecision) ([]byte, error) {
	// A permission request carries no session_id, so request_id is the only
	// thing tying an answer to an ask. Without one this is not a degraded
	// answer, it is an unanswerable frame - and the process stays blocked
	// until it gets a real one.
	if requestID == "" {
		return nil, fmt.Errorf("%w: encode control response: empty request id", ErrNotWritten)
	}
	return marshalLine(outControlResponse{
		Type: "control_response",
		Response: outControlBody{
			Subtype:   "success",
			RequestID: requestID,
			Response:  d,
		},
	}, "encode control response")
}

// marshalLine renders one outbound frame as a single newline-terminated
// line, because stdin is newline-delimited JSON in exactly the way stdout
// is. json.Marshal escapes embedded newlines and quotes, which is what keeps
// a multi-line prompt from arriving as several frames.
//
// The error is reachable through EncodeAllow: updatedInput crosses into the
// airlock from outside and can hold something json cannot render. Returning
// it beats writing a half-frame to a process that is blocked on this answer -
// and it wraps ErrNotWritten for exactly that reason: the half-frame is the
// thing that did not happen.
func marshalLine(frame any, what string) ([]byte, error) {
	b, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrNotWritten, what, err)
	}
	return append(b, '\n'), nil
}

// goalOp recognises the native /goal lifecycle on an already-decoded message and
// returns the Wake op, or ok=false for any frame that is not one. It reuses the
// decoded message so a frame is never parsed twice. The announcements are gated
// on the synthetic model so an agent typing the words is not mistaken for the
// command (the markers are in wire.go beside the wire shapes); the progress
// refresh is a user frame carrying the feedback prefix.
//
// It reads, not writes - so wire.go would be its subject home - but it landed
// here while that file sat at the 800-line hard max; the placement turns on it
// being an airlock file, not the direction.
func goalOp(frameType string, m wireMessage) (GoalOp, bool) {
	if m.Model == syntheticModel {
		text := messageText(m.Content)
		switch {
		case strings.HasPrefix(text, goalSetPrefix):
			return GoalOp{Op: GoalSet, Condition: strings.TrimSpace(text[len(goalSetPrefix):])}, true
		case strings.HasPrefix(text, goalClearedPrefix):
			return GoalOp{Op: GoalCleared, Condition: strings.TrimSpace(text[len(goalClearedPrefix):])}, true
		case text == goalNoneText:
			return GoalOp{Op: GoalNone}, true
		}
		return GoalOp{}, false
	}
	if frameType == frameTypeUser {
		if text := jsonString(m.Content); strings.HasPrefix(text, goalFeedbackPrefix) {
			return goalProgress(text)
		}
	}
	return GoalOp{}, false
}

// The bundled scheduler tools a headless session reaches for when it reproduces
// /loop: a recurring CronCreate is a fixed cadence, ScheduleWakeup is self-paced,
// and CronDelete ends a fixed one. Claude's names, so they are recognised behind
// the airlock; here rather than vocabulary.go for room, beside goalOp. See
// core/loop.go for the Wake types.
const (
	toolCronCreate     = "CronCreate"
	toolScheduleWakeup = "ScheduleWakeup"
	toolCronDelete     = "CronDelete"

	cronKey         = "cron"
	recurringKey    = "recurring"
	delaySecondsKey = "delaySeconds"
	noopKey         = "noop"
	stopKey         = "stop"
	promptKey       = "prompt"
)

// toolLoopOp recognizes a /loop from a scheduler tool_use, and nil for every
// other call. A CronCreate counts as a loop only when recurring - a one-shot
// CronCreate is a reminder, not a loop. A CronDelete ends a fixed loop, and a
// ScheduleWakeup with stop:true ends a self-paced one (its own end signal, not
// CronDelete - a self-paced wakeup is one-shot, so there is no cron to delete;
// recorded against claude 2.1.270). Otherwise a ScheduleWakeup is a self-paced
// iteration whose noop flag marks a quiet tick.
//
// A self-paced iteration must carry a prompt - it is the /loop input the wakeup
// re-fires, and the tool refuses a non-stop call without one ("prompt is required
// when stop is not true"). The model still emits the tool_use of such a call, and
// reading it as a loop lit the ↻ off a call that errored - and a self-paced loop
// clears only on a stop it never sends, so the marker stuck. An absent or blank
// prompt names no work to resume, so it is not a loop. Other values are read
// tolerantly, the way toolChecklistOp reads its own: a missing or wrong-typed key
// is the zero value.
func toolLoopOp(name string, input map[string]any) *LoopOp {
	switch name {
	case toolCronCreate:
		if rec, _ := input[recurringKey].(bool); !rec {
			return nil
		}
		cron, _ := input[cronKey].(string)
		return &LoopOp{Kind: LoopFixed, Cron: cron}
	case toolScheduleWakeup:
		if stop, _ := input[stopKey].(bool); stop {
			return &LoopOp{Stop: true}
		}
		prompt, _ := input[promptKey].(string)
		if strings.TrimSpace(prompt) == "" {
			return nil
		}
		noop, _ := input[noopKey].(bool)
		return &LoopOp{Kind: LoopSelfPaced, DelaySeconds: intArg(input, delaySecondsKey), Noop: noop}
	case toolCronDelete:
		return &LoopOp{Stop: true}
	}
	return nil
}

// wireWorkflowItem is one workflow_progress entry: a phase or an agent, told
// apart by type. It reads, not writes - wire.go's own reason for encode.go
// holding the room, beside goalOp.
type wireWorkflowItem struct {
	Type          string `json:"type"`
	Index         int    `json:"index"`
	Title         string `json:"title"`
	Label         string `json:"label"`
	PhaseIndex    int    `json:"phaseIndex"`
	AgentID       string `json:"agentId"`
	Model         string `json:"model"`
	State         string `json:"state"`
	Error         string `json:"error"`
	Tokens        int    `json:"tokens"`
	ToolCalls     int    `json:"toolCalls"`
	DurationMs    int    `json:"durationMs"`
	PromptPreview string `json:"promptPreview"`
	ResultPreview string `json:"resultPreview"`
}

const (
	workflowPhaseItem = "workflow_phase"
	workflowAgentItem = "workflow_agent"
)

// Every recorded agent state: start, progress (mid-tool), done, and error -
// an agent that failed, or was refused before it started (workflow-agent-error.jsonl).
var workflowAgentStates = map[string]WorkflowAgentState{
	"start": WorkflowAgentRunning, "progress": WorkflowAgentRunning,
	"done": WorkflowAgentDone, "error": WorkflowAgentFailed,
}

// DecodeWorkflowRun decodes one run record. An unrecognised status resolves
// to TaskStatusUnknown, taskStatus's own ruling: a word this corpus has never
// seen is not "done" wearing a guess.
func DecodeWorkflowRun(raw []byte) (WorkflowRun, error) {
	var w wireWorkflowRun
	if err := json.Unmarshal(raw, &w); err != nil {
		return WorkflowRun{}, fmt.Errorf("decode workflow run: %w", err)
	}
	if w.TaskID == "" {
		return WorkflowRun{}, errors.New("decode workflow run: no task id")
	}
	status, ok := taskStatuses[w.Status]
	if !ok {
		status = TaskStatusUnknown
	}
	return containedRun(WorkflowRun{TaskID: w.TaskID, Name: w.WorkflowName, Summary: w.Summary, Status: status,
		Error: w.Error, Started: time.UnixMilli(w.StartTime), Duration: time.Duration(w.DurationMs) * time.Millisecond,
		Script: w.Script, Progress: workflowSnapshotOf(w.WorkflowProgress)}), nil
}

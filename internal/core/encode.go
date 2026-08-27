// The frames Wake writes back to a session - part of the airlock; see
// protocol.go.
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
	"errors"
	"fmt"
	"strings"
)

// ErrNotWritten wraps every error this file returns, and the wrapping is the
// point rather than any of the messages.
//
// Encoding happens before a byte reaches a process, so a failure here means
// stdin was never touched: the session is exactly as it was, the ask is still
// outstanding, and the agent is still blocked on it. That is a different fact
// from a *write* that failed, which internal/daemon reads as proof the process
// is gone (agent.noteUnreachable) - and reading a refused answer that way
// would report a perfectly healthy agent as silent and invite a kill nobody
// meant.
//
// It matters most for EncodeAnswer, whose refusals are routine rather than
// exceptional: an answer is assembled from what an operator did, so it is the
// one frame in this file a caller can get wrong at runtime.
var ErrNotWritten = errors.New("nothing was written")

// --- encoding ---------------------------------------------------------------
//
// PROBE-DERIVED, NOT FIXTURE-DERIVED - unlike everything the other three
// airlock files decode. Wake writes these frames on stdin and never reads
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

// EncodeUserMessage renders one user turn as a stream-json line. Send it
// after a KindTurnEnd: the process stays alive across turns, and closing
// stdin instead would end it.
//
// Three rules from the recorded corpus, all in
// docs/superpowers/notes/2026-08-15-image-input-findings.md: images go first
// and the text block last (Claude derives the prompt from the final block), an
// empty content array is silently dropped so a message with neither text nor an
// image is refused here, and the base64 is handed over raw for Claude to budget.
func EncodeUserMessage(text string, images []ImageBlock) ([]byte, error) {
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
	return marshalLine(outUserFrame{
		Type:    "user",
		Message: outUserMessage{Role: "user", Content: content},
	}, "encode user message")
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

// checkAnswers requires one non-blank choice per question asked, and no
// choices beyond them.
//
// Every question, because a missing one is the defect this file exists to
// close, arriving one question at a time: the tool asks 1-4 at once, the
// answers map has no way to say "skipped", and the model is given the same
// empty-ish map either way. Non-blank, because "" is what a UI produces when
// nobody chose. And nothing extra, because a choice keyed on a question this
// ask did not put reaches the model attached to nothing - it is a lost answer
// wearing the shape of a delivered one.
//
// A choice is not required to match one of the option labels. The 2.1.226
// binary phrases the tool result differently for a value it cannot match
// rather than rejecting it, and no recording ever sent one, so refusing here
// would forbid a shape the CLI tolerates on the strength of a guess.
func checkAnswers(questions []string, answers map[string]string) error {
	for _, q := range questions {
		choice, ok := answers[q]
		if !ok {
			return fmt.Errorf("%w: nothing was chosen for %q", ErrNotWritten, q)
		}
		if strings.TrimSpace(choice) == "" {
			return fmt.Errorf("%w: the choice for %q is blank", ErrNotWritten, q)
		}
	}
	if len(answers) != len(questions) {
		return fmt.Errorf("%w: %d choices for %d questions - one of them names a question this ask did not put",
			ErrNotWritten, len(answers), len(questions))
	}
	return nil
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

// outControlRequest is the envelope for a control_request Wake sends - the
// opposite direction of outControlResponse above. request_id sits on the
// envelope here, exactly where wireFrame.RequestID reads it on the inbound
// can_use_tool ask; a control_response is the one that nests it a level
// further, under "response".
// Request is any because Wake sends two subtypes with disjoint payloads. A
// struct holding both would put cancel_queued on a mode change and mode on an
// interrupt, and omitempty cannot hide the first: interrupt's cancel_queued
// tracks presence, not truth.
type outControlRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

// outInterruptRequest is the only control_request subtype Wake sends today.
//
// CancelQueued is omitempty on purpose. interrupt-queued-survives.jsonl and
// interrupt-cancel-queued.jsonl differ only in whether cancel_queued rode the
// request at all, and the receipt's own "cancelled" key tracks that same
// presence-vs-absence rather than true-vs-false - see ControlResult's doc
// comment for why the two are different facts. An always-present false here
// would erase on the way out the distinction the receipt goes out of its way
// to preserve on the way back.
//
// reason is deliberately not a field. The zod schema in the 2.1.226 binary
// allows it, but no recording in this corpus ever sent it, so its effect -
// on tool behavior, on terminal_reason, on whether it even reaches stdout -
// is unverified (interrupt-findings.md §13). This project's rule is that the
// bytes are the authority; an unrecorded field is a guess, not a feature.
type outInterruptRequest struct {
	Subtype      string `json:"subtype"`
	CancelQueued bool   `json:"cancel_queued,omitempty"`
}

// EncodeInterrupt aborts the currently running turn. cancelQueued also
// destroys messages Wake has queued but not yet started - without it they
// still run once the abort completes (interrupt-queued-survives.jsonl vs.
// interrupt-cancel-queued.jsonl). The receipt comes back as a
// control_response, decoded by controlResponseEvent into KindControlReceipt;
// its subtype is "success" even for an interrupt that interrupted nothing,
// exactly as the answer half of this file already documents for a permission
// decision - transport-level, not a verdict.
//
// The CLI accepts a control_request with no request_id and aborts the turn
// anyway (interrupt-no-request-id.jsonl) - but the receipt that comes back
// then carries no request_id either, which makes it unattributable. At
// 15-30 concurrent sessions with more than one interrupt possibly in flight,
// a receipt that names no request cannot be matched to the interrupt that
// caused it. So this makes the same non-empty check encodeControlResponse
// already makes for the answer direction, with the check earning its keep
// for a different reason: there, Wake has no other id to echo back; here,
// the CLI would silently accept the gap and it is Wake who would regret it
// later, unable to attribute the reply. A caller with no id yet has not
// decided how it means to correlate its own request, and manufacturing one
// here would hide that gap rather than surface it - so this refuses to build
// the frame instead of guessing on the caller's behalf.
//
// Session.Interrupt is the only caller and it passes cancelQueued false. That
// is a decision about Wake and not about this frame: the field itself is
// recorded working both ways, so it stays a parameter rather than being
// hard-coded away, and core.interruptCancelQueued is where the argument for
// Wake's answer lives and where it would be revisited.
func EncodeInterrupt(requestID string, cancelQueued bool) ([]byte, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: encode interrupt: empty request id", ErrNotWritten)
	}
	return marshalLine(outControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request: outInterruptRequest{
			Subtype:      "interrupt",
			CancelQueued: cancelQueued,
		},
	}, "encode interrupt")
}

// outSetModeRequest changes the permission mode of a session already running.
//
// ultraplan is deliberately not a field. It sits beside mode in the 2.1.228
// binary's request shape (permission-mode-findings.md §2), was never sent and
// never recorded, and this project's rule is that the bytes are the authority -
// the same ruling outInterruptRequest makes about reason.
type outSetModeRequest struct {
	Subtype string `json:"subtype"`
	Mode    string `json:"mode"`
}

// EncodeSetMode changes a running session's permission mode. It is the
// mechanism deferred I7 was blocked on: Config.PermissionMode reaches the
// command line once, and this is the only way to move a mode after that.
//
// The receipt is the authority on what the mode became, never the string sent
// here. `manual` is accepted and silently normalizes to `default`
// (permission-mode-findings.md §6), so a caller that moves a label on the mode
// it asked for will be wrong on that position - which is I7's own defect
// wearing a new hat. Read Event.Control.Mode instead.
//
// A refusal comes back as a receipt with subtype "error" and a top-level error
// string, not as a failure here: an unknown mode and bypassPermissions on a
// session not launched dangerously (§7) are both refused that way, after this
// function has already returned a well-formed line.
//
// The empty checks are EncodeInterrupt's, for its reason and one more. A blank
// request id makes the receipt unattributable across 15-30 sessions, and this
// is the receipt that carries the truth. A blank mode would have to mean either
// "leave it" or "reset it" and both readings are wrong, so it is refused rather
// than sent for the CLI to reject.
func EncodeSetMode(requestID, mode string) ([]byte, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: encode set mode: empty request id", ErrNotWritten)
	}
	if mode == "" {
		return nil, fmt.Errorf("%w: encode set mode: empty mode", ErrNotWritten)
	}
	return marshalLine(outControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request: outSetModeRequest{
			Subtype: "set_permission_mode",
			Mode:    mode,
		},
	}, "encode set mode")
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

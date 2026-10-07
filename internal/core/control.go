// Control requests Wake writes, and their receipts - part of the airlock; see
// protocol.go.
//
// A control_request steers a running session without a turn: interrupt it,
// change its permission mode, rewind it, stop a workflow, ask about its MCP
// servers. Each is answered by a control_response correlated on request_id, and
// both halves of that exchange live here. A permission answer is the other
// control_* exchange and is not here: Claude asks and Wake answers, so it stays
// with encode.go and protocol.go's controlRequestEvent.
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
	"fmt"
	"strings"
)

// outControlRequest is the envelope for a control_request Wake sends - the
// opposite direction of encode.go's outControlResponse. request_id sits on the
// envelope here, exactly where wireFrame.RequestID reads it on the inbound
// can_use_tool ask; a control_response is the one that nests it a level
// further, under "response".
// Request is any because each subtype Wake sends has its own payload. One
// struct holding them all would put cancel_queued on a mode change, and
// omitempty cannot hide it: interrupt's cancel_queued tracks presence, not truth.
type outControlRequest struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   any    `json:"request"`
}

// outInterruptRequest aborts the running turn.
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
// exactly as encode.go's answer half already documents for a permission
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

// outRewindRequest rewinds a running session's conversation to an earlier user
// message. Both uuids are mandatory: last_seen_user_message_uuid is the
// optimistic-concurrency guard, and omitting it returns "stale target"
// (2026-08-25 rewind spike). interrupt_if_running is a constant false — Wake
// rewinds only an idle session (the esc-esc gate), and only that shape is
// recorded.
type outRewindRequest struct {
	Subtype            string `json:"subtype"`
	TargetMessageUUID  string `json:"target_message_uuid"`
	LastSeenUUID       string `json:"last_seen_user_message_uuid"`
	InterruptIfRunning bool   `json:"interrupt_if_running"`
}

// EncodeRewind asks the session to rewind its conversation to targetUUID,
// declaring lastSeenUUID as the tip it is rewinding from. It returns the
// request_id its receipt will carry. The receipt is the authority on whether it
// rewound (Event.Rewind); a refusal comes back there, not as an error here.
// The empty checks are EncodeSetMode's: a blank request id makes the receipt
// unattributable across 15-30 sessions, and a blank uuid is refused rather than
// sent for the CLI to reject as "stale target"/"target not found".
func EncodeRewind(requestID, targetUUID, lastSeenUUID string) ([]byte, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: encode rewind: empty request id", ErrNotWritten)
	}
	if targetUUID == "" || lastSeenUUID == "" {
		return nil, fmt.Errorf("%w: encode rewind: empty target or last-seen uuid", ErrNotWritten)
	}
	return marshalLine(outControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request: outRewindRequest{
			Subtype:            "rewind_conversation",
			TargetMessageUUID:  targetUUID,
			LastSeenUUID:       lastSeenUUID,
			InterruptIfRunning: false,
		},
	}, "encode rewind")
}

// outStopTaskRequest stops a running dynamic Workflow() by its own task id -
// the wire form of the Agent SDK's documented stopTask(taskId)
// (findings.md §6). The same request at a workflow *agent's* agentId is
// answered success and does nothing, since that agent already finished; Wake
// never sends one there.
type outStopTaskRequest struct {
	Subtype string `json:"subtype"`
	TaskID  string `json:"task_id"`
}

type outCancelAsyncRequest struct {
	Subtype     string `json:"subtype"`
	MessageUUID string `json:"message_uuid"`
}

// EncodeCancelAsyncMessage takes back a message claude has queued but not yet
// taken up, by the uuid Wake stamped on it. Its lifecycle then reads cancelled
// and it never runs; one already taken up is delivered regardless
// (midturn-cancel.jsonl, midturn-cancel-late.jsonl).
func EncodeCancelAsyncMessage(requestID, messageUUID string) ([]byte, error) {
	if requestID == "" || messageUUID == "" {
		return nil, fmt.Errorf("%w: encode cancel async message: empty request or message id", ErrNotWritten)
	}
	return marshalLine(outControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request:   outCancelAsyncRequest{Subtype: "cancel_async_message", MessageUUID: messageUUID},
	}, "encode cancel async message")
}

// EncodeStopTask stops a running workflow, addressed by its task id. Pause and
// resume have no wire form (findings.md §6: pause_task is refused outright),
// so this is the only control Wake can offer over a running workflow. The
// empty checks are EncodeRewind's, for its reason.
func EncodeStopTask(requestID, taskID string) ([]byte, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: encode stop task: empty request id", ErrNotWritten)
	}
	if taskID == "" {
		return nil, fmt.Errorf("%w: encode stop task: empty task id", ErrNotWritten)
	}
	return marshalLine(outControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request: outStopTaskRequest{
			Subtype: "stop_task",
			TaskID:  taskID,
		},
	}, "encode stop task")
}

// The three MCP asks, recorded against 2.1.281 in mcp-control.stdin.jsonl.
// None needs a model turn. A refusal comes back as a subtype "error" receipt
// with the reason top-level - "Server not found: x", "Server status:
// needs-auth" - never as an error here.
type outBareRequest struct {
	Subtype string `json:"subtype"`
}

type outMCPReconnectRequest struct {
	Subtype    string `json:"subtype"`
	ServerName string `json:"serverName"`
}

// outMCPToggleRequest persists, into the project's disabledMcpServers.
type outMCPToggleRequest struct {
	Subtype    string `json:"subtype"`
	ServerName string `json:"serverName"`
	Enabled    bool   `json:"enabled"`
}

// EncodeMCPStatus asks for every server's live status. EncodeInitialize is the
// handshake every SDK host opens a session with, and the step that makes a
// headless session load claude.ai connectors (probed 2026-09-27, 2.1.281). The
// empty check is EncodeSetMode's: an id-less receipt could not be matched.
func EncodeMCPStatus(requestID string) ([]byte, error)  { return encodeBare(requestID, "mcp_status") }
func EncodeInitialize(requestID string) ([]byte, error) { return encodeBare(requestID, "initialize") }

func encodeBare(requestID, subtype string) ([]byte, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: encode %s: empty request id", ErrNotWritten, subtype)
	}
	return marshalLine(outControlRequest{Type: "control_request", RequestID: requestID,
		Request: outBareRequest{Subtype: subtype}}, "encode "+subtype)
}

// EncodeMCPReconnect reconnects one server by the name the status reply gave.
func EncodeMCPReconnect(requestID, server string) ([]byte, error) {
	if requestID == "" || server == "" {
		return nil, fmt.Errorf("%w: encode mcp reconnect: empty request id or server", ErrNotWritten)
	}
	return marshalLine(outControlRequest{Type: "control_request", RequestID: requestID,
		Request: outMCPReconnectRequest{Subtype: "mcp_reconnect", ServerName: server}}, "encode mcp reconnect")
}

// EncodeMCPToggle switches one server on or off.
func EncodeMCPToggle(requestID, server string, enabled bool) ([]byte, error) {
	if requestID == "" || server == "" {
		return nil, fmt.Errorf("%w: encode mcp toggle: empty request id or server", ErrNotWritten)
	}
	return marshalLine(outControlRequest{Type: "control_request", RequestID: requestID,
		Request: outMCPToggleRequest{Subtype: "mcp_toggle", ServerName: server, Enabled: enabled}}, "encode mcp toggle")
}

// The two states the init roster never showed, beside vocabulary.go's three.
const (
	MCPFailed   = "failed"
	MCPDisabled = "disabled"
)

// The config scopes an mcp_status row names, as MCPServerStatus.Scope carries
// them - an open set; a scope not listed here arrives intact.
const (
	MCPScopeLocal      = "local"
	MCPScopeProject    = "project"
	MCPScopeUser       = "user"
	MCPScopePlugin     = "plugin"
	MCPScopeClaudeAI   = "claudeai"
	MCPScopeManaged    = "managed"
	MCPScopeEnterprise = "enterprise"
	MCPScopeDynamic    = "dynamic"
)

// IsClaudeAIConnector reports whether a server is a claude.ai connector, which
// Claude names "claude.ai <service>" - the name its own deniedMcpServers takes.
func IsClaudeAIConnector(name string) bool { return strings.HasPrefix(name, "claude.ai ") }

// wireMCPStatus is one row of an mcp_status receipt's mcpServers. The reply's
// other fields (source, the tools' _meta) are not read.
type wireMCPStatus struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Scope      string `json:"scope"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
	Config struct {
		Type    string   `json:"type"`
		URL     string   `json:"url"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"config"`
	Tools []struct {
		Name        string `json:"name"`
		Annotations struct {
			ReadOnly bool `json:"readOnly"`
		} `json:"annotations"`
	} `json:"tools"`
}

// mcpStatusReply is a control_response that carries mcpServers, known by the
// key's presence the way a rewind receipt is known by rewound's.
func mcpStatusReply(ev Event, r *wireControlResp) (Event, bool) {
	rows := r.Response.MCPServers
	if rows == nil {
		return ev, false
	}
	servers := make([]MCPServerStatus, 0, len(*rows))
	for _, w := range *rows {
		servers = append(servers, mcpServerStatus(w))
	}
	ev.Kind = KindMCPReply
	ev.Text = r.Subtype
	ev.MCP = &MCPResult{Ask: MCPAskServers, Servers: servers, Error: r.Error}
	return ev, true
}

func mcpServerStatus(w wireMCPStatus) MCPServerStatus {
	s := MCPServerStatus{Name: w.Name, State: w.Status, Error: w.Error, Scope: w.Scope,
		Transport: w.Config.Type, Target: w.Config.URL,
		Info: strings.TrimSpace(w.ServerInfo.Name + " " + w.ServerInfo.Version)}
	if s.Target == "" {
		s.Target = strings.TrimSpace(strings.Join(append([]string{w.Config.Command}, w.Config.Args...), " "))
	}
	for _, t := range w.Tools {
		s.Tools = append(s.Tools, MCPTool{Name: t.Name, ReadOnly: t.Annotations.ReadOnly})
	}
	return s
}

// wireControlResp is the nested body of a control_response, and the only
// place both that frame's subtype and its correlator exist.
//
// Subtype was "success" on all 12 recorded receipts, including every one
// that interrupted nothing. It is transport level - "an answer, not a
// protocol error" - and not a verdict, the same thing encode.go says about
// the answer Wake writes.
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
	StillQueued []string      `json:"still_queued"`
	Cancelled   wireCancelled `json:"cancelled"`
	Mode        string        `json:"mode"`

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

	// An mcp_status receipt's payload; a pointer so presence, even of an empty
	// list, is the discriminator. See mcpStatusReply.
	MCPServers *[]wireMCPStatus `json:"mcpServers"`

	// An initialize receipt's commands, the session's slash commands before any
	// turn has sent an init. Raw so a shape this build cannot read costs the
	// facts and never the receipt: the handshake's reply must always reach the
	// daemon. See commandNames.
	Commands json.RawMessage `json:"commands"`
}

// commandNames is a receipt's `commands` reduced to each entry's name, the only
// field read, and nil for anything that is not a list of named entries.
func commandNames(raw json.RawMessage) []string {
	var rows []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	var names []string
	for _, r := range rows {
		if r.Name != "" {
			names = append(names, r.Name)
		}
	}
	return names
}

// wireCancelled is an interrupt receipt's uuid list, or the bool a
// cancel_async_message receipt carries under the same key
// (midturn-cancel.jsonl:33). Absent and null leave both nil.
type wireCancelled struct {
	uuids    []string
	recalled *bool
}

func (c *wireCancelled) UnmarshalJSON(b []byte) error {
	if json.Unmarshal(b, &c.recalled) == nil {
		return nil
	}
	return json.Unmarshal(b, &c.uuids)
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
	// Rewind and MCP status receipts are known by a payload key's presence, and
	// are checked first so neither falls through to the mode/generic path.
	if ev, ok := mcpStatusReply(ev, f.Response); ok {
		return ev
	}
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
	// The mode a set_permission_mode landed on; empty on every other receipt and
	// on a refusal, whose reason travels in Control.Error instead.
	ev.PermissionMode = f.Response.Response.Mode
	ev.Control = &ControlResult{
		StillQueued: f.Response.Response.StillQueued,
		Cancelled:   f.Response.Response.Cancelled.uuids,
		Error:       f.Response.Error,
		Recalled:    f.Response.Response.Cancelled.recalled,
	}
	// The handshake's reply names the session's commands ahead of its first
	// init; only that, so no other fact is claimed by it.
	if names := commandNames(f.Response.Response.Commands); len(names) > 0 {
		ev.Session = &SessionFacts{SlashCommands: names}
	}
	return ev
}

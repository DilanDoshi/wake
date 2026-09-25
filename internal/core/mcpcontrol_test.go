package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// mcpInputs is every control_request line Wake would write, as recorded against
// 2.1.281 in a sterile HOME: testdata/input/mcp-control.stdin.jsonl.
func mcpInputs(t *testing.T) []map[string]any {
	t.Helper()
	f, err := os.Open("../../testdata/input/mcp-control.stdin.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("fixture line is not JSON: %v", err)
		}
		out = append(out, m)
	}
	return out
}

// Each MCP request Wake encodes is byte-identical to the one recorded going in,
// so what the CLI answered in mcp-control.jsonl is what Wake's bytes get.
func TestEncodeMCPMatchesTheRecordedRequests(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/input/mcp-control.stdin.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	seen := map[string]bool{}
	for i, m := range mcpInputs(t) {
		id, _ := m["request_id"].(string)
		req, _ := m["request"].(map[string]any)
		name, _ := req["serverName"].(string)
		var got []byte
		switch sub := req["subtype"]; sub {
		case "mcp_status":
			got, err = EncodeMCPStatus(id)
		case "mcp_reconnect":
			got, err = EncodeMCPReconnect(id, name)
		case "mcp_toggle":
			got, err = EncodeMCPToggle(id, name, req["enabled"] == true)
		default:
			t.Fatalf("line %d: unexpected subtype %v", i, sub)
		}
		if err != nil {
			t.Fatalf("line %d: encode: %v", i, err)
		}
		seen[req["subtype"].(string)] = true
		if string(bytes.TrimSuffix(got, []byte("\n"))) != lines[i] {
			t.Errorf("line %d does not match the recording\n got: %s\nwant: %s", i, got, lines[i])
		}
	}
	for _, sub := range []string{"mcp_status", "mcp_reconnect", "mcp_toggle"} {
		if !seen[sub] {
			t.Errorf("the fixture no longer exercises %s", sub)
		}
	}
}

func TestEncodeMCPRefusesABlankIDOrServer(t *testing.T) {
	cases := map[string]func() ([]byte, error){
		"status without id":    func() ([]byte, error) { return EncodeMCPStatus("") },
		"reconnect without id": func() ([]byte, error) { return EncodeMCPReconnect("", "x") },
		"reconnect no server":  func() ([]byte, error) { return EncodeMCPReconnect("id", "") },
		"toggle without id":    func() ([]byte, error) { return EncodeMCPToggle("", "x", true) },
		"toggle no server":     func() ([]byte, error) { return EncodeMCPToggle("id", "", false) },
	}
	for name, encode := range cases {
		if _, err := encode(); !errors.Is(err, ErrNotWritten) {
			t.Errorf("%s: err = %v, want ErrNotWritten", name, err)
		}
	}
}

// mcpReply is the decoded control_response answering request id in
// mcp-control.jsonl.
func mcpReply(t *testing.T, id string) Event {
	t.Helper()
	line, n := lineContaining(t, "testdata/stream/mcp-control.jsonl", `"request_id":"`+id+`"`)
	return onlyEvent(t, line, n)
}

// A status reply is known by its payload, the way a rewind receipt is, and
// every state the recording caught survives the airlock.
func TestAnMCPStatusReplyCarriesEveryServer(t *testing.T) {
	ev := mcpReply(t, "mcp-02")
	if ev.Kind != KindMCPReply || ev.MCP == nil {
		t.Fatalf("kind = %q, MCP = %+v; want an MCP reply", ev.Kind, ev.MCP)
	}
	if ev.MCP.Ask != MCPAskServers {
		t.Errorf("Ask = %q, want %q", ev.MCP.Ask, MCPAskServers)
	}
	got := map[string]MCPServerStatus{}
	for _, s := range ev.MCP.Servers {
		got[s.Name] = s
	}
	echo := got["echo"]
	if echo.State != MCPConnected || echo.Scope != "user" || echo.Transport != "stdio" ||
		!strings.HasSuffix(echo.Target, "/echo_server.py") || echo.Info != "echo-probe 0.0.1" {
		t.Errorf("echo = %+v", echo)
	}
	if len(echo.Tools) != 2 || echo.Tools[0] != (MCPTool{Name: "echo", ReadOnly: true}) ||
		echo.Tools[1] != (MCPTool{Name: "shout"}) {
		t.Errorf("echo tools = %+v", echo.Tools)
	}
	if l := got["linear"]; l.State != MCPNeedsAuth || l.Transport != "http" || l.Target != "https://mcp.linear.app/mcp" {
		t.Errorf("linear = %+v", l)
	}
	if b := got["broken"]; b.State != MCPFailed || !strings.Contains(b.Error, "ENOENT") || b.Target != "/nonexistent/mcp-server" {
		t.Errorf("broken = %+v", b)
	}
	if first := mcpReply(t, "mcp-01"); first.MCP == nil || len(first.MCP.Servers) != 3 {
		t.Fatalf("the first reply lost its servers: %+v", first.MCP)
	}
	if disabled := mcpReply(t, "mcp-04"); disabled.MCP.Servers[0].State != MCPDisabled {
		t.Errorf("echo after the toggle = %+v, want disabled", disabled.MCP.Servers[0])
	}
}

// The command a stdio server is started with carries its arguments, so the
// detail view shows what actually runs.
func TestAStdioTargetJoinsItsArguments(t *testing.T) {
	line := `{"type":"control_response","response":{"subtype":"success","request_id":"r","response":{"mcpServers":[` +
		`{"name":"fc","status":"connected","config":{"type":"stdio","command":"npx","args":["-y","firecrawl-mcp"]}}]}}}`
	ev := onlyEvent(t, line, 0)
	if got := ev.MCP.Servers[0].Target; got != "npx -y firecrawl-mcp" {
		t.Errorf("Target = %q", got)
	}
}

// A session with no servers still answers, and the answer is an empty list
// rather than an ordinary receipt.
func TestAnEmptyStatusReplyIsStillAnMCPReply(t *testing.T) {
	line := `{"type":"control_response","response":{"subtype":"success","request_id":"r","response":{"mcpServers":[]}}}`
	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindMCPReply || ev.MCP == nil || len(ev.MCP.Servers) != 0 {
		t.Errorf("got kind %q MCP %+v", ev.Kind, ev.MCP)
	}
}

// Toggle and reconnect answer with the bare shape a permission-mode receipt
// uses, so the airlock alone cannot tell them apart - the session can, by the
// request id it minted. Decoded without that knowledge they stay generic.
func TestABareMCPReceiptDecodesAsAGenericReceipt(t *testing.T) {
	for _, id := range []string{"mcp-03", "mcp-07"} {
		if ev := mcpReply(t, id); ev.Kind != KindControlReceipt {
			t.Errorf("%s: kind = %q, want %q", id, ev.Kind, KindControlReceipt)
		}
	}
}

func mcpSession(t *testing.T) (*Session, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}
	return s, &buf
}

// sentRequest is the one control_request line s wrote.
func sentRequest(t *testing.T, buf *bytes.Buffer) (id string, req map[string]any) {
	t.Helper()
	var m struct {
		RequestID string         `json:"request_id"`
		Request   map[string]any `json:"request"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("not one JSON line: %q (%v)", buf.String(), err)
	}
	buf.Reset()
	return m.RequestID, m.Request
}

func TestTheSessionWritesEachMCPRequest(t *testing.T) {
	s, buf := mcpSession(t)
	if err := s.MCPServers("q1"); err != nil {
		t.Fatal(err)
	}
	if _, req := sentRequest(t, buf); req["subtype"] != "mcp_status" {
		t.Errorf("servers wrote %v", req)
	}
	if err := s.MCPReconnect("q2", "linear"); err != nil {
		t.Fatal(err)
	}
	if _, req := sentRequest(t, buf); req["subtype"] != "mcp_reconnect" || req["serverName"] != "linear" {
		t.Errorf("reconnect wrote %v", req)
	}
	if err := s.MCPSetEnabled("q3", "echo", false); err != nil {
		t.Fatal(err)
	}
	if _, req := sentRequest(t, buf); req["subtype"] != "mcp_toggle" || req["enabled"] != false {
		t.Errorf("disable wrote %v", req)
	}
}

// A reply to a request this session sent as MCP is labelled MCP, with what
// was asked and of which server - which is what keeps an MCP refusal from
// reading as a permission-mode refusal in every window.
func TestAReceiptForAnMCPRequestIsLabelledByTheAsk(t *testing.T) {
	s, buf := mcpSession(t)
	if err := s.MCPReconnect("q2", "linear"); err != nil {
		t.Fatal(err)
	}
	id, _ := sentRequest(t, buf)
	refusal := `{"type":"control_response","response":{"subtype":"error","request_id":"` + id + `","error":"Server status: needs-auth"}}`
	ev := s.attribute(onlyEvent(t, refusal, 0))
	if ev.Kind != KindMCPReply || ev.MCP == nil {
		t.Fatalf("kind %q MCP %+v", ev.Kind, ev.MCP)
	}
	if ev.MCP.Ask != MCPAskReconnect || ev.MCP.Server != "linear" || ev.MCP.Error != "Server status: needs-auth" {
		t.Errorf("MCP = %+v", ev.MCP)
	}
	if ev.Control != nil {
		t.Errorf("an MCP reply still carries a generic receipt: %+v", ev.Control)
	}
	if again := s.attribute(onlyEvent(t, refusal, 0)); again.Kind != KindControlReceipt {
		t.Errorf("a second receipt for the same id was labelled %q; an ask is answered once", again.Kind)
	}
}

func TestADisableAndAnEnableAreToldApart(t *testing.T) {
	s, buf := mcpSession(t)
	for _, enabled := range []bool{false, true} {
		if err := s.MCPSetEnabled("q4", "echo", enabled); err != nil {
			t.Fatal(err)
		}
		id, _ := sentRequest(t, buf)
		ok := `{"type":"control_response","response":{"subtype":"success","request_id":"` + id + `"}}`
		want := MCPAskDisable
		if enabled {
			want = MCPAskEnable
		}
		if ev := s.attribute(onlyEvent(t, ok, 0)); ev.MCP == nil || ev.MCP.Ask != want || ev.MCP.Error != "" {
			t.Errorf("enabled=%v: MCP = %+v, want ask %q", enabled, ev.MCP, want)
		}
	}
}

// A receipt this session did not mint for MCP is left exactly as decoded.
func TestAnUnrelatedReceiptIsLeftAlone(t *testing.T) {
	s, _ := mcpSession(t)
	mode := `{"type":"control_response","response":{"subtype":"error","request_id":"someone-else","error":"nope"}}`
	if ev := s.attribute(onlyEvent(t, mode, 0)); ev.Kind != KindControlReceipt || ev.MCP != nil {
		t.Errorf("kind %q MCP %+v", ev.Kind, ev.MCP)
	}
}

// A request that never reached the process is not remembered, so nothing is
// left waiting for an answer that cannot come.
func TestAnUnwrittenAskIsNotRemembered(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	if err := s.MCPReconnect("q5", "linear"); err == nil {
		t.Fatal("a session that never started accepted a write")
	}
	if n := s.pendingAsks(); n != 0 {
		t.Errorf("%d asks remembered after a failed write", n)
	}
}

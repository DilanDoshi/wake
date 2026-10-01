package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The self-test passes against this build's own server, and touches no fleet.
//
// The fleet is nil on purpose: initialize and tools/list must be answered
// without it, which is what lets the daemon run the check before any socket
// exists for the manager. A request that reached a tool would panic here.
func TestTheSelfTestPassesAgainstThisBuildsServer(t *testing.T) {
	requests, err := SelfTestRequests()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Serve(context.Background(), bytes.NewReader(requests), &out, nil); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if err := CheckSelfTest(out.Bytes()); err != nil {
		t.Errorf("this build's own server failed its self-test: %v\nit wrote:\n%s", err, out.String())
	}
}

// Every way a server can fail to be this build's manager server is refused, and
// only a different tool set reads as a replaced binary.
func TestTheSelfTestRefusesEveryAnswerThatIsNotThisBuildsServer(t *testing.T) {
	names := func(drop string, add ...string) []any {
		out := []any{}
		for _, tool := range Tools() {
			if tool.Name != drop {
				out = append(out, map[string]any{"name": tool.Name})
			}
		}
		for _, n := range add {
			out = append(out, map[string]any{"name": n})
		}
		return out
	}
	init := func(server string, caps map[string]any) string {
		return line(t, map[string]any{"jsonrpc": "2.0", "id": selfTestInitID, "result": map[string]any{
			"protocolVersion": protocolVersion, "capabilities": caps,
			"serverInfo": map[string]any{"name": server, "version": version},
		}})
	}
	list := func(tools []any) string {
		return line(t, map[string]any{"jsonrpc": "2.0", "id": selfTestListID, "result": map[string]any{"tools": tools}})
	}
	good := init(serverName, map[string]any{"tools": map[string]any{}})
	first := Tools()[0].Name

	cases := []struct {
		name     string
		answers  string
		replaced bool
	}{
		{"nothing at all", "", false},
		{"not JSON", "Usage: wake <verb>\n", false},
		{"initialize unanswered", list(names("")), false},
		{"tools/list unanswered", good, false},
		{"another server", init("not-wake", map[string]any{"tools": map[string]any{}}) + list(names("")), false},
		{"no tools capability", init(serverName, map[string]any{}) + list(names("")), false},
		{"tools/list refused", good + line(t, map[string]any{"jsonrpc": "2.0", "id": selfTestListID,
			"error": map[string]any{"code": codeMethodNotFound, "message": "no method"}}), false},
		{"a tool missing", good + list(names(first)), true},
		{"a tool this build does not have", good + list(names("", "stop_agent")), true},
		{"a tool twice", good + list(names("", first)), true},
	}
	for _, c := range cases {
		err := CheckSelfTest([]byte(c.answers))
		if err == nil {
			t.Errorf("%s: passed the self-test, so a manager would start with tools that are not there:\n%s", c.name, c.answers)
			continue
		}
		if got := errors.Is(err, ErrOtherTools); got != c.replaced {
			t.Errorf("%s: errors.Is(err, ErrOtherTools) = %v, want %v (err: %v)", c.name, got, c.replaced, err)
		}
	}
}

// The tool-set refusal names both sets, so the operator can see what differs.
func TestAReplacedBinaryIsToldWhichToolsDiffer(t *testing.T) {
	answers := line(t, map[string]any{"jsonrpc": "2.0", "id": selfTestInitID, "result": map[string]any{
		"capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": serverName},
	}}) + line(t, map[string]any{"jsonrpc": "2.0", "id": selfTestListID, "result": map[string]any{
		"tools": []any{map[string]any{"name": "stop_agent"}},
	}})
	err := CheckSelfTest([]byte(answers))
	if err == nil || !strings.Contains(err.Error(), "stop_agent") || !strings.Contains(err.Error(), Tools()[0].Name) {
		t.Errorf("err = %v, want both the served tools and this build's named", err)
	}
}

func line(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

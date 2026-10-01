package mcp

// The client half of the manager's startup self-test; internal/daemon's
// mcpselftest.go runs the process. It lives beside Serve so one package spells
// both halves of the protocol: the methods, the server's name and the tool set.
//
// It asks only initialize and tools/list, which answer() serves without the
// Fleet - so the check needs no daemon, no socket and no manager.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ErrOtherTools is a server that answered as wake with a different tool set
// from this build's: the binary was replaced while the daemon kept running.
var ErrOtherTools = errors.New("it serves a different set of tools from the build this daemon is")

// methodInitialized is the notification a client sends after initialize. Serve
// answers no notification; the self-test sends it because a real client does.
const methodInitialized = "notifications/initialized"

const (
	selfTestInitID = 1
	selfTestListID = 2
)

// SelfTestRequests is what the self-test writes to a server's stdin, one
// request per line: Serve's framing.
func SelfTestRequests() ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, req := range []map[string]any{
		{"jsonrpc": "2.0", "id": selfTestInitID, "method": methodInitialize, "params": map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": serverName, "version": version},
		}},
		{"jsonrpc": "2.0", "method": methodInitialized},
		{"jsonrpc": "2.0", "id": selfTestListID, "method": methodToolsList},
	} {
		if err := enc.Encode(req); err != nil {
			return nil, fmt.Errorf("build the self-test's requests: %w", err)
		}
	}
	return b.Bytes(), nil
}

type selfTestAnswer struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// CheckSelfTest reads what a server wrote back to SelfTestRequests and says
// whether it is this build's manager server: it calls itself wake, it offers
// tools (a client not told so never lists them), and it lists exactly Tools().
func CheckSelfTest(out []byte) error {
	answers := map[int]selfTestAnswer{}
	for l := range bytes.Lines(out) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		var a selfTestAnswer
		if err := json.Unmarshal(l, &a); err != nil {
			return fmt.Errorf("it answered with something that is not MCP: %s", oneLine(string(l), agentLineMax))
		}
		answers[a.ID] = a
	}
	var init struct {
		Capabilities struct {
			Tools *json.RawMessage `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := decodeAnswer(answers, selfTestInitID, methodInitialize, &init); err != nil {
		return err
	}
	if init.ServerInfo.Name != serverName {
		return fmt.Errorf("it is not wake's MCP server: it calls itself %q", oneLine(init.ServerInfo.Name, agentLineMax))
	}
	if init.Capabilities.Tools == nil {
		return errors.New("it does not say it has tools, so a client never asks for them")
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := decodeAnswer(answers, selfTestListID, methodToolsList, &list); err != nil {
		return err
	}
	served := make([]string, 0, len(list.Tools))
	for _, t := range list.Tools {
		served = append(served, oneLine(t.Name, agentLineMax))
	}
	want := make([]string, 0, len(Tools()))
	for _, t := range Tools() {
		want = append(want, t.Name)
	}
	slices.Sort(served)
	slices.Sort(want)
	if !slices.Equal(served, want) {
		return fmt.Errorf("%w: it serves %s, and this build's are %s", ErrOtherTools,
			strings.Join(served, ", "), strings.Join(want, ", "))
	}
	return nil
}

// decodeAnswer finds the answer to one request and decodes its result.
func decodeAnswer(answers map[int]selfTestAnswer, id int, method string, into any) error {
	a, ok := answers[id]
	switch {
	case !ok:
		return fmt.Errorf("it did not answer %s", method)
	case a.Error != nil:
		return fmt.Errorf("it refused %s: %s", method, oneLine(a.Error.Message, agentLineMax))
	}
	if err := json.Unmarshal(a.Result, into); err != nil {
		return fmt.Errorf("its answer to %s is not the shape MCP gives it: %w", method, err)
	}
	return nil
}

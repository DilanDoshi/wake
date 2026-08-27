package mcp

// The protocol half. Everything here drives Serve the way a tool runner does -
// bytes in, bytes out - and reads the result back through a *test-local* copy
// of the envelope rather than through the production types.
//
// That indirection is the point rather than an inconvenience. Decoding into
// the same struct the server encoded from cannot see a wrong json tag: `Result`
// tagged `results` would round-trip perfectly and reach a real client as a
// response with no result in it. The far side is what is asserted on, so the
// far side gets its own declaration.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// clientResponse is one JSON-RPC response as a client sees it. Written out
// rather than reusing `response` for the reason in the file header.
type clientResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *clientError    `json:"error"`
}

type clientError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// fakeFleet is the whole daemon, for every test in this package. No socket, no
// process, no claude - which is what writing the server against an interface
// bought.
//
// It records what reached it, and that is what most of the acting half's
// assertions are made on. The spyFleet comment in tools_test.go carries the
// reason at length: "an error came back" is not the property, and a refusal
// that happens *after* the daemon was asked has already broken the rule it was
// written to keep. Only the far side can see that, so the far side counts.
//
// One fake rather than a second one beside it. Send and Interrupt used to
// return "the reading half does not send", which was true when it was written
// and is a sentence about the build rather than about an input - the shape
// docs/notes/decisions.md names as rung 7. The acting half is the change that
// makes it false, so it is the change that deletes it.
type fakeFleet struct {
	status rpc.Status
	err    error

	// acts is where Send and Interrupt are recorded, and it is a pointer
	// because Fleet's methods have value receivers: every existing call site
	// passes a fakeFleet by value, and a slice field would record into a copy
	// the test cannot see. Nil for the tests that do not care, which is what
	// keeps `fakeFleet{}` a legal fleet.
	acts *actions

	// actErr is what Send and Interrupt fail with - the daemon refusing what a
	// tool asked of it, as distinct from err, which is a daemon that cannot be
	// reached at all.
	actErr error
}

// actions is what the far side of an acting tool actually received.
type actions struct {
	sent        []sentMessage
	interrupted []string
	spawned     []string
}

type sentMessage struct{ id, text string }

func (f fakeFleet) List(context.Context) (rpc.Status, error) { return f.status, f.err }

// Spawn records the directory and hands back a fixed id, which is enough for
// every assertion here: what the tool does with the id is return it.
func (f fakeFleet) Spawn(_ context.Context, dir string) (string, error) {
	if f.actErr != nil {
		return "", f.actErr
	}
	if f.acts != nil {
		f.acts.spawned = append(f.acts.spawned, dir)
	}
	return spawnedID, nil
}

// spawnedID is what the fake mints. A UUID because that is what the daemon
// refuses a spawn without.
const spawnedID = "11111111-2222-3333-4444-555555555555"

func (f fakeFleet) Send(_ context.Context, id, text string) error {
	if f.acts != nil {
		f.acts.sent = append(f.acts.sent, sentMessage{id: id, text: text})
	}
	return f.actErr
}

func (f fakeFleet) Interrupt(_ context.Context, id string) error {
	if f.acts != nil {
		f.acts.interrupted = append(f.acts.interrupted, id)
	}
	return f.actErr
}

// serve runs the given lines through the server and returns what a client
// would have read off the pipe.
func serve(t *testing.T, f Fleet, lines []string) []clientResponse {
	t.Helper()
	return decodeStream(t, serveRaw(t, f, lines))
}

// serveRaw is the bytes, for the one assertion that is about them.
func serveRaw(t *testing.T, f Fleet, lines []string) string {
	t.Helper()
	var out strings.Builder
	if err := Serve(t.Context(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, f); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	return out.String()
}

func decodeStream(t *testing.T, s string) []clientResponse {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	got := make([]clientResponse, 0, 4)
	for dec.More() {
		var r clientResponse
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("decoding the server's own output failed at %q: %v", s, err)
		}
		got = append(got, r)
	}
	return got
}

// advertised is tools/list as a client reads it: the names, and the schema
// under the key the MCP specification gives it.
type advertised struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func toolsList(t *testing.T, r clientResponse) []advertised {
	t.Helper()
	var res struct {
		Tools []advertised `json:"tools"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatalf("tools/list result is not readable: %v (%s)", err, r.Result)
	}
	return res.Tools
}

func toolNames(t *testing.T, r clientResponse) []string {
	t.Helper()
	list := toolsList(t, r)
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.Name)
	}
	return names
}

func requestLine(t *testing.T, id int, method string, params map[string]any) string {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshalling a request: %v", err)
	}
	return string(line)
}

func TestTheServerAnswersInitializeAndThenListsItsTools(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"claude","version":"0"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	})

	if len(out) != 2 {
		t.Fatalf("got %d responses, want 2:\n%v", len(out), out)
	}
	for i, r := range out {
		if r.JSONRPC != "2.0" {
			t.Errorf("response %d has jsonrpc %q", i, r.JSONRPC)
		}
		if r.Error != nil {
			t.Errorf("response %d is an error: %v", i, r.Error)
		}
	}

	// Derived, not restated. The plan's draft of this test held a literal
	// {"list_agents", "agent_status"}, which pins those two and says nothing
	// about a third tool that Tools() declares and tools/list drops - the
	// exact shape docs/notes/decisions.md names as the dominant defect. The
	// names themselves are pinned where they are load-bearing: by the
	// behaviour tests in tools_test.go, which call them by name through the
	// wire, and by Task 15's scoping message which spells them.
	got := toolNames(t, out[1])
	want := make([]string, 0, len(Tools()))
	for _, tool := range Tools() {
		want = append(want, tool.Name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("tools/list advertises %v, Tools() declares %v: a tool a model is never shown is a tool that does not exist", got, want)
	}
}

func TestInitializeAdvertisesToolsOrTheClientNeverAsksForThem(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{requestLine(t, 1, "initialize", nil)})
	if len(out) != 1 {
		t.Fatalf("got %d responses, want 1: %v", len(out), out)
	}
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools *struct{} `json:"tools"`
		} `json:"capabilities"`
		ServerInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(out[0].Result, &res); err != nil {
		t.Fatalf("initialize result is not readable: %v (%s)", err, out[0].Result)
	}
	if res.Capabilities.Tools == nil {
		t.Error("initialize did not advertise a tools capability. A client that is not told the server has tools does not call tools/list, so every tool below this is unreachable and nothing else in this package fails")
	}
	if res.ProtocolVersion != protocolVersion {
		t.Errorf("initialize reported protocol version %q, want %q", res.ProtocolVersion, protocolVersion)
	}
	if res.ServerInfo.Name == "" || res.ServerInfo.Version == "" {
		t.Errorf("serverInfo = %+v, want a name and a version", res.ServerInfo)
	}
}

func TestANotificationIsNotAnsweredAndDoesNotEndTheServer(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	})
	if len(out) != 1 {
		t.Fatalf("got %d responses, want 1: a JSON-RPC notification carries no id and must not be answered, and answering one desynchronises the client's own correlation", len(out))
	}
}

func TestAMalformedLineIsRefusedWithoutEndingTheServer(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{
		`{not json`,
		// A blank line is framing, not a request. Without the skip it would
		// decode as a second parse error and this count would be three.
		``,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	})
	if len(out) != 2 || out[0].Error == nil || out[1].Error != nil {
		t.Fatalf("a bad line should be one parse error and then business as usual: %v", out)
	}
	if out[0].Error.Code != codeParse {
		t.Errorf("parse failure reported code %d, want %d", out[0].Error.Code, codeParse)
	}
	if string(out[0].ID) != "null" {
		t.Errorf("a line that could not be parsed answered with id %s: there is no id to answer with, and JSON-RPC spells that null", out[0].ID)
	}
}

func TestAnUnknownMethodIsAnErrorAndNotSilence(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{`{"jsonrpc":"2.0","id":1,"method":"tools/nonesuch"}`})
	if len(out) != 1 || out[0].Error == nil {
		t.Fatal("an unknown method produced no error. A model that gets silence retries, and a manager retrying a method that does not exist is a manager stuck in a loop with the operator watching")
	}
	if out[0].Error.Code != codeMethodNotFound {
		t.Errorf("unknown method reported code %d, want %d", out[0].Error.Code, codeMethodNotFound)
	}
}

// A request larger than bufio.Scanner's 64KB default has to arrive whole.
//
// Scanner fails that case by *truncating* - Scan returns false and Err is
// ErrTooLong - so without the explicit Buffer this server would stop dead
// partway through a conversation with no response and no error the model can
// read. The condition is reachable here on purpose: 128KB is over the default
// and far under maxLine, so the only thing between a pass and a fail is the
// Buffer call.
func TestALineOverScannersDefaultIsReadWholeRatherThanTruncated(t *testing.T) {
	huge := strings.Repeat("x", 128*1024)
	out := serve(t, fakeFleet{}, []string{
		requestLine(t, 1, "tools/call", map[string]any{
			"name":      "agent_status",
			"arguments": map[string]any{agentIDArg: huge},
		}),
	})
	if len(out) != 1 {
		t.Fatalf("a %d-byte request produced %d responses, want 1: bufio.Scanner's default buffer is 64KB and it fails by truncating, which is silent corruption rather than an error", len(huge), len(out))
	}
	if out[0].Error != nil {
		t.Fatalf("a large but well-formed request was refused at the protocol level: %v", out[0].Error)
	}
}

// Angle brackets and ampersands go out as themselves.
//
// Go's encoder escapes < > & into six bytes each to protect JSON embedded in a
// script tag, and this is a pipe to a local process - the same ruling
// internal/rpc made, measured there at 1.87x on bracket-dense payloads. A
// fleet report is exactly that: file paths, shell lines and diffs. Asserted on
// the bytes rather than on the decoded value, because both spellings decode
// identically and only one of them is the cost.
func TestTheBytesOnTheWireAreNotHTMLEscaped(t *testing.T) {
	const arg = "grep '<div>' src/**/*.tsx & true"
	raw := serveRaw(t, fleetOf(rpc.SessionStatus{
		ID: idPeter, Name: "peter", State: rpc.StateWorking, Tool: "Bash", ToolArg: arg,
	}), []string{requestLine(t, 1, "tools/call", map[string]any{"name": "list_agents"})})

	if !strings.Contains(raw, "<div>") || !strings.Contains(raw, "& true") {
		t.Errorf("the encoder escaped the payload:\n%s", raw)
	}
	if strings.Contains(raw, `\u003c`) || strings.Contains(raw, `\u0026`) {
		t.Errorf("< and & went out as six bytes each. That escaping exists to protect JSON inside a <script> tag; this is a pipe to a local process, and a fleet report is all paths and shell lines:\n%s", raw)
	}
}

// The write side has one failure mode and it must end the loop rather than
// spin: if the pipe to the model is gone there is nobody left to answer.
type brokenWriter struct{ err error }

func (b brokenWriter) Write([]byte) (int, error) { return 0, b.err }

func TestAServerThatCannotWriteStopsRatherThanTalkingToAClosedPipe(t *testing.T) {
	want := errors.New("broken pipe")
	for _, c := range []struct {
		name string
		line string
	}{
		{"an answer", requestLine(t, 1, "tools/list", nil)},
		{"a parse error", `{not json`},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Serve(t.Context(), strings.NewReader(c.line+"\n"), brokenWriter{err: want}, fakeFleet{})
			if !errors.Is(err, want) {
				t.Errorf("Serve returned %v, want the write error: a loop that keeps reading after the pipe is gone answers nobody and never ends", err)
			}
		})
	}
}

func TestServeReportsAReadFailureRatherThanTreatingItAsTheEnd(t *testing.T) {
	want := errors.New("read failed")
	err := Serve(t.Context(), failingReader{err: want}, &strings.Builder{}, fakeFleet{})
	if !errors.Is(err, want) {
		t.Errorf("Serve returned %v, want the read error", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// # The package has no clock in it
//
// "Wake must be cheap to leave open" is a non-negotiable, and for a query
// surface it has one exact meaning: an answer is produced when a model asks
// and at no other time. A ticker refreshing a fleet snapshot, a poll waiting
// for a session to change, a sleep between retries - each would be a cost paid
// by a laptop with thirty claude processes on it, for a server that is idle
// almost all of the time.
//
// Derived rather than forbidden by name. A list of banned calls (Tick,
// NewTicker, After, Sleep) is a hand-written list standing in for something
// the source already declares, and it is silent about whichever spelling
// nobody thought of. This reads every time.X the package names and requires it
// to be in the set that formats a duration, so *anything* else is a build
// failure that has to be argued for here.
func TestNothingInThisPackageKeepsTime(t *testing.T) {
	allowed := map[string]string{
		"Duration":    "QuietMS is milliseconds on the wire; statusReport turns it into something a model can read",
		"Millisecond": "the unit QuietMS is in",
		"Second":      "what that duration is rounded to, because a model does not need milliseconds",
	}
	for file, f := range parsePackage(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "time" {
				return true
			}
			if _, ok := allowed[sel.Sel.Name]; !ok {
				t.Errorf("%s names time.%s. Wake must be cheap to leave open: a query answers when it is asked and does nothing in between, so a clock in this package is a cost thirty idle agents pay for nothing. If it is genuinely needed, add it here with the reason", file, sel.Sel.Name)
			}
			return true
		})
	}
}

// parsePackage is every non-test file of this package, parsed. The two derived
// guards - this one's caller and the tool-declaration bijection in
// tools_test.go - read the source rather than a list somebody maintains.
func parsePackage(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	fset := token.NewFileSet()
	files := make(map[string]*ast.File)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files[name] = f
	}
	if len(files) == 0 {
		t.Fatal("parsed no files: a derived guard that finds nothing to derive from is a test that cannot fail")
	}
	return files
}

// call runs one tool the way a model does - a tools/call request in, a
// JSON-RPC response out - and returns the text the client actually received.
//
// Never t.Call directly. HANDOFF.md's third lesson is to assert on what the
// far side received, and for an MCP server the far side is a JSON decoder: a
// tool returning perfect text behind a mis-shaped content array reaches a model
// as nothing at all, and every assertion on the returned string still passes.
func call(t *testing.T, f Fleet, name string, args map[string]any) string {
	t.Helper()
	text, err := callErr(t, f, name, args)
	if err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
	return text
}

// callErr is call, for the cases where the failure is the subject. The error
// it returns is the content of an isError result - which is how a tool's own
// failure reaches a model at all.
func callErr(t *testing.T, f Fleet, name string, args map[string]any) (string, error) {
	t.Helper()
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	out := serve(t, f, []string{requestLine(t, 1, "tools/call", params)})
	if len(out) != 1 {
		t.Fatalf("a tools/call produced %d responses, want 1: %v", len(out), out)
	}
	if out[0].Error != nil {
		return "", fmt.Errorf("jsonrpc error %d: %s", out[0].Error.Code, out[0].Error.Message)
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(out[0].Result, &res); err != nil {
		t.Fatalf("a tools/call result is not readable: %v (%s)", err, out[0].Result)
	}
	if len(res.Content) != 1 || res.Content[0].Type != "text" {
		t.Fatalf("a tools/call result carried %+v, want one text block: a model reads content and nothing else", res.Content)
	}
	if res.IsError {
		return "", errors.New(res.Content[0].Text)
	}
	return res.Content[0].Text, nil
}

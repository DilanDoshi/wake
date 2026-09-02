package mcp

// The tools, driven through the server rather than called directly - see
// call's comment in server_test.go for why.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	idPeter = "1e5c1b8a-0000-4000-8000-000000000001"
	idJohn  = "1e5c1b8a-0000-4000-8000-000000000002"
)

func fleetOf(sessions ...rpc.SessionStatus) fakeFleet {
	return fakeFleet{status: rpc.Status{Running: true, Sessions: sessions}}
}

func TestListAgentsReturnsIdsTheOtherToolsAccept(t *testing.T) {
	f := fleetOf(
		rpc.SessionStatus{ID: idPeter, Name: "peter", Label: "api-v2", State: rpc.StateWorking, Tool: "Edit", ToolArg: "auth/token.go"},
		rpc.SessionStatus{ID: idJohn, Name: "john", Label: "api-v2", State: rpc.StateWorking, Tool: "Edit", ToolArg: "auth/token.go"},
	)
	out := call(t, f, "list_agents", nil)

	if !strings.Contains(out, "peter") || !strings.Contains(out, "auth/token.go") {
		t.Fatalf("list_agents = %q", out)
	}
	// The decisive case the manager's scope rests on: two agents on one file
	// is a string comparison over two aligned lines, answerable with no
	// message history at all.
	if strings.Count(out, "Edit(auth/token.go)") != 2 {
		t.Errorf("two agents editing one file did not produce two comparable lines:\n%s", out)
	}

	// "the id from here with the other tools" is a claim, so it is exercised
	// rather than described: whatever list_agents puts in the first column has
	// to be something agent_status accepts. A tool that printed a short id, or
	// a name, would pass every assertion above and leave the manager one turn
	// from a refusal on its next call.
	for _, line := range rows(t, out) {
		id, _, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("a list_agents line has no columns: %q", line)
		}
		if _, err := callErr(t, f, "agent_status", map[string]any{agentIDArg: id}); err != nil {
			t.Errorf("agent_status refused %q, which list_agents offered as an id: %v", id, err)
		}
	}
}

// A comparison run down a column needs the column to be in the same place on
// every line, which a bare Sprintf does not give for names of different
// lengths.
func TestTheColumnsLineUpSoOneCanBeComparedDownTheFleet(t *testing.T) {
	out := call(t, fleetOf(
		rpc.SessionStatus{ID: idPeter, Name: "bartholomew", Label: "api-v2", State: rpc.StateWorking, Tool: "Edit", ToolArg: "auth/token.go"},
		rpc.SessionStatus{ID: idJohn, Name: "jo", Label: "web", State: rpc.StateIdle, Tool: "Edit", ToolArg: "auth/token.go"},
	), "list_agents", nil)

	lines := rows(t, out)
	if len(lines) != 2 {
		t.Fatalf("want two lines, got %d:\n%s", len(lines), out)
	}
	first, second := strings.Index(lines[0], "Edit("), strings.Index(lines[1], "Edit(")
	if first < 0 || second < 0 {
		t.Fatalf("the activity column is missing:\n%s", out)
	}
	if first != second {
		t.Errorf("the activity starts at column %d on one line and %d on the other, for names %d characters apart. The whole reason this is text and not JSON is that a model can run its eye down a column:\n%s", first, second, len("bartholomew")-len("jo"), out)
	}
}

// spyFleet counts what actually reaches the daemon.
//
// It exists because of a defect this test had in its first form, which is
// worth recording: it passed a name against an *empty* fleet and asserted only
// that some error came back mentioning list_agents. Deleting the isSessionID
// check entirely left it green - the name simply fell through to the lookup
// and came back as "no agent has id \"alex\"", which mentions list_agents too.
// The mutation battery found it. The property is not "an error happens", it is
// **the name never becomes an address**, and the only way to see that is from
// the far side: the daemon is not asked at all.
type spyFleet struct {
	fleet Fleet
	lists *int
}

func (f spyFleet) List(ctx context.Context) (rpc.Status, error) {
	*f.lists++
	return f.fleet.List(ctx)
}
func (f spyFleet) Send(ctx context.Context, id, text string) error {
	return f.fleet.Send(ctx, id, text)
}
func (f spyFleet) Interrupt(ctx context.Context, id string) error {
	return f.fleet.Interrupt(ctx, id)
}
func (f spyFleet) Spawn(ctx context.Context, dir string) (string, error) {
	return f.fleet.Spawn(ctx, dir)
}

func TestAToolRefusesANameAndNeverAsksTheDaemonAboutIt(t *testing.T) {
	lists := 0
	// A fleet that really does hold an agent called alex, so nothing here is
	// refused merely for being absent.
	f := spyFleet{lists: &lists, fleet: fleetOf(
		rpc.SessionStatus{ID: idPeter, Name: "alex", Label: "api-v2", State: rpc.StateWorking},
	)}

	_, err := callErr(t, f, "agent_status", map[string]any{agentIDArg: "alex"})
	if err == nil {
		t.Fatal("a display name was accepted as an agent id. names.go rules that nothing is addressed by name on the wire and a test enforces it, because the reaper proves a process group by finding a session UUID in its argv - a name arriving from a model is exactly the word that must not become an address")
	}
	if lists != 0 {
		t.Errorf("the daemon was asked %d times about a display name. Refusing after the lookup is not the property: the ruling is that a word a model chose never travels as an address, and a tool that forwards it has already broken that whatever it does with the answer", lists)
	}
	if !strings.Contains(err.Error(), "list_agents") {
		t.Errorf("the refusal does not say how to recover: %v", err)
	}
	if !strings.Contains(err.Error(), "display name") {
		t.Errorf("the refusal does not say what was wrong with it, so it reads the same as an agent that has gone away: %v", err)
	}
}

// isSessionID is the enforcement that does not depend on a model reading a
// description, so its edges are worth stating: the length, the separators, and
// the alphabet. Every case here is one a name or a truncated id could take.
func TestOnlyAWholeSessionIDIsAnAddress(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{idPeter, true},
		{strings.ToUpper(idPeter), true},
		{"", false},
		{"alex", false},
		{"1e5c1b8a", false},    // the short id `wake status` prints
		{idPeter + "a", false}, // longer than a uuid
		{idPeter[:35], false},  // one short
		{"1e5c1b8a-0000-4000-8000-00000000000g", false},  // not hex
		{"1e5c1b8a_0000-4000-8000-000000000001", false},  // wrong separator
		{"1e5c1b8a-0000-4000-8000+000000000001", false},  // wrong separator, last position
		{"1e5c1b8a00004000800000000000000100000", false}, // no separators, right length + 1
		{"peter                               ", false},  // padded to 36
	} {
		if got := isSessionID(c.in); got != c.want {
			t.Errorf("isSessionID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAnEndedAgentIsNotOfferedAsSomethingToActOn(t *testing.T) {
	for _, state := range []string{rpc.StateEnded, rpc.StateOrphaned} {
		f := fleetOf(rpc.SessionStatus{ID: idPeter, Name: "gone", State: state})
		if out := call(t, f, "list_agents", nil); strings.Contains(out, "gone") {
			t.Errorf("list_agents offered a %s session: a manager told to message it would be told the session is gone, which is a turn spent on Wake's bookkeeping:\n%s", state, out)
		}
	}
}

// The other half of that: an id the manager already holds still answers.
//
// The two tools read the same report and filter it differently on purpose.
// list_agents is a menu, so what cannot be acted on is not on it; agent_status
// is a lookup, and "that session ended, here is why" is the answer to the
// question rather than a refusal to answer it.
func TestAgentStatusStillAnswersForASessionListAgentsNoLongerOffers(t *testing.T) {
	f := fleetOf(rpc.SessionStatus{ID: idPeter, Name: "gone", State: rpc.StateEnded, Error: "exit status 1"})
	out := call(t, f, "agent_status", map[string]any{agentIDArg: idPeter})
	if !strings.Contains(out, "exit status 1") {
		t.Errorf("agent_status on an ended session did not say how it ended: a manager holding an id from four turns ago gets a refusal instead of the answer:\n%s", out)
	}
}

func TestAnEmptyFleetSaysSoRatherThanReturningNothing(t *testing.T) {
	out := call(t, fakeFleet{}, "list_agents", nil)
	if strings.TrimSpace(out) == "" {
		t.Error("list_agents returned an empty string for an empty fleet. A model reading nothing cannot tell 'no agents' from 'the tool is broken', and the second one is worth retrying")
	}
}

func TestAskingAboutAnAgentThatIsNotThereSaysWhereToLook(t *testing.T) {
	_, err := callErr(t, fakeFleet{}, "agent_status", map[string]any{agentIDArg: idPeter})
	if err == nil {
		t.Fatal("agent_status answered for an id no session has")
	}
	if !strings.Contains(err.Error(), "list_agents") {
		t.Errorf("the refusal does not say how to recover: %v", err)
	}
}

// A daemon that cannot be reached is the model's problem to route around, so
// it has to reach the model. A JSON-RPC error would too, but as "your request
// was malformed" - which is the one reading that leads nowhere.
func TestAFleetThatCannotAnswerReachesTheModelAsContentItCanRead(t *testing.T) {
	want := "dial unix /tmp/wake.sock: connect: connection refused"
	for _, name := range []string{"list_agents", "agent_status"} {
		args := map[string]any{agentIDArg: idPeter}
		if name == "list_agents" {
			args = nil
		}
		_, err := callErr(t, fakeFleet{err: errors.New(want)}, name, args)
		if err == nil {
			t.Fatalf("%s reported success with the daemon unreachable", name)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s hid why it failed: %v", name, err)
		}
	}
}

// Which *kind* of failure a tool's own refusal is, and it is not a protocol
// one.
//
// A JSON-RPC error means the request was malformed and there is nothing in it
// for a model to act on; "that is not an id, call list_agents" is an
// instruction, and an instruction only reaches a model through content. The
// two are one line apart in callTool and every other assertion in this package
// reads the text either way, so this is the only place the distinction is
// held.
func TestAToolsOwnFailureArrivesAsContentAndNotAsAProtocolError(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{requestLine(t, 1, "tools/call", map[string]any{
		"name":      "agent_status",
		"arguments": map[string]any{agentIDArg: "alex"},
	})})
	if len(out) != 1 {
		t.Fatalf("got %d responses, want 1: %v", len(out), out)
	}
	if out[0].Error != nil {
		t.Fatalf("a tool's own refusal came back as a JSON-RPC error (%v). A model is shown the protocol's errors as 'your request was malformed' and can do nothing with one; the recovery instruction has to arrive as content", out[0].Error)
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(out[0].Result, &res); err != nil {
		t.Fatalf("result is not readable: %v (%s)", err, out[0].Result)
	}
	if !res.IsError {
		t.Error("a refused call was not marked isError: a model reading a refusal as an ordinary result believes it, and reports the work as done")
	}
	if len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, "list_agents") {
		t.Errorf("the refusal did not carry the recovery: %+v", res.Content)
	}
}

func TestAToolThatDoesNotExistIsAnErrorAndNotSilence(t *testing.T) {
	_, err := callErr(t, fakeFleet{}, "delete_everything", nil)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(codeMethodNotFound)) {
		t.Errorf("calling a tool that does not exist returned %v, want a method-not-found error: a model that gets silence retries", err)
	}
}

func TestToolCallParametersThatAreNotReadableAreRefused(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not an object"}`})
	if len(out) != 1 || out[0].Error == nil {
		t.Fatalf("a tools/call whose params are not an object was not refused: %v", out)
	}
	if out[0].Error.Code != codeInvalidRequest {
		t.Errorf("code %d, want %d", out[0].Error.Code, codeInvalidRequest)
	}
}

// requiredArgs reads the schema a *model* is shown, not the Go value, so the
// two directions below are a bijection between what is advertised and what is
// enforced.
//
// Every required argument rather than agent_id alone: spawn_agent's is a
// directory, and a check that named one argument would have read as coverage
// while enforcing nothing about the next tool's.
func requiredArgs(a advertised) []string {
	req, ok := a.InputSchema["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(req))
	for _, r := range req {
		if name, ok := r.(string); ok {
			out = append(out, name)
		}
	}
	return out
}

func TestEveryToolAdvertisesASchemaThatMatchesWhatItReads(t *testing.T) {
	out := serve(t, fakeFleet{}, []string{requestLine(t, 1, "tools/list", nil)})
	for _, a := range toolsList(t, out[0]) {
		if a.Name == "" || a.Description == "" {
			t.Errorf("tool %+v is missing a name or description: a model chooses by description", a)
		}
		if a.InputSchema["type"] != "object" {
			t.Errorf("%s advertises a non-object schema: %v", a.Name, a.InputSchema)
		}
		_, err := callErr(t, fakeFleet{}, a.Name, map[string]any{})
		req := requiredArgs(a)
		switch {
		case len(req) > 0 && err == nil:
			t.Errorf("%s advertises %v as required and accepted a call without any of them", a.Name, req)
		case len(req) > 0 && !strings.Contains(err.Error(), "required"):
			t.Errorf("%s refused a call that omitted %v without saying it is required: %v. 'that is not an agent id' is what a model is told when it passed something wrong, and a model that passed nothing then goes looking for a value it never sent", a.Name, req, err)
		case len(req) == 0 && err != nil:
			t.Errorf("%s advertises no required argument and then refused a call with none: %v. A model cannot satisfy a requirement it is never shown", a.Name, err)
		}
	}
}

// A tool declared in this package and left out of Tools() is invisible - it
// compiles, it is reviewed, and no model ever sees it.
//
// Derived from the source rather than from a list here, which is the whole
// point: a list would have to be edited by the same change that forgets to
// edit Tools(), so it would only ever agree with the mistake.
func TestEveryToolDeclaredInTheSourceIsAdvertisedAndCallable(t *testing.T) {
	declared := make([]string, 0, 4)
	for file, f := range parsePackage(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "Tool" {
				return true
			}
			name, ok := literalField(lit, "Name")
			if !ok {
				t.Errorf("%s builds a Tool whose Name is not a literal, so nothing here can check it reaches a model", file)
				return true
			}
			declared = append(declared, name)
			return true
		})
	}
	if len(declared) == 0 {
		t.Fatal("found no Tool literals in the source: this guard is deriving from nothing")
	}

	registered := make([]string, 0, len(declared))
	for _, tool := range Tools() {
		registered = append(registered, tool.Name)
	}
	slices.Sort(declared)
	slices.Sort(registered)
	if !slices.Equal(declared, registered) {
		t.Errorf("the source declares tools %v and Tools() returns %v: a tool that is not in Tools() is advertised to nobody", declared, registered)
	}

	notFound := fmt.Sprintf("jsonrpc error %d", codeMethodNotFound)
	for _, name := range registered {
		if _, err := callErr(t, fakeFleet{}, name, map[string]any{}); err != nil && strings.Contains(err.Error(), notFound) {
			t.Errorf("%s is advertised by tools/list and tools/call does not dispatch it", name)
		}
	}
}

// literalField is the string value of a named field in a composite literal.
func literalField(lit *ast.CompositeLit, field string) (string, bool) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != field {
			continue
		}
		val, ok := kv.Value.(*ast.BasicLit)
		if !ok {
			return "", false
		}
		return strings.Trim(val.Value, `"`), true
	}
	return "", false
}

// notInTheStatusReport is every rpc.SessionStatus field agent_status
// deliberately does not put in front of a model, with the reason.
//
// An excuse table, so it gets a count - decisions.md's rule, and the thing
// that stops an exemption list growing quietly the way the airlock's original
// 23 leaks did.
var notInTheStatusReport = map[string]string{
	"Cwd": "where the agent moved itself to, which a manager does not need and " +
		"should not be handed: the bound on its own spawn tool is fleetOccupies, and that rests " +
		"on Dir precisely because an operator chose it. Reporting an agent-chosen directory " +
		"beside an operator-chosen one invites reading them as one kind of thing",

	"PID":            "a process id is not an address a model may hold. The reaper's only proof of identity is the session UUID in an argv, and the whole name-on-the-wire ruling is about keeping what a model chose away from that proof - a pid on the tool surface is an invitation to reach past it",
	"RequestIDs":     "the *fact* of a blocked ask is reported and the ids are not, because no tool answers a permission request: only a human can. An id a model cannot use is an id it will try to use",
	"ParentID":       "which session this one was forked from. There is no fork verb on this surface, so lineage is context a manager cannot act on, and the manager-legible form of it is the parent's *name* rather than a UUID. Resolving that name is a loop over the report agentStatus already holds - cheap, and deliberately not done here: the task that gives the manager a fork tool is the one that knows what it should say",
	"Budget":         "the spend ceiling this session was started under, and it is the one field here that is misleading rather than merely useless. Nothing reports spend-to-date on this surface, so a cap with no progress beside it tells a model that an agent *may stop* without telling it whether that is imminent or nowhere near - and a manager holding send_to_agent acts on what it is told. There is no budget verb here either, so it is not a fact this surface could do anything with. What would reopen it: a spend-to-date on the report, at which point the pair is a measurement rather than a ceiling",
	"Commands":       "the slash commands a session advertised, which is the operator's completion menu and nothing this surface can act on: there is no command-typing verb here, and the list is the agent's own (it grows when an agent writes a .claude/commands file), so it is somebody else's words with nothing for a manager to do with them. It rides the report only so a reattached *client* can draw the menu",
	"Color":          "the identity hue an operator chose for this agent, and pure display chrome: it says nothing about what the agent is doing, which is the only thing this report is for. There is no colour verb on this surface either - FrameColor is refused the manager for the same whose-decision reason - so it is a fact the manager could neither use nor act on, and one that would put the operator's own visual grouping into a model's context for nothing",
	"ConfirmedModel": "the model's display name a /model probe read back, which is the operator's status-bar chrome and not a fact this surface can act on: the init frame already names the model in use, there is no model verb here, and unlike Effort it is not a closed set (ValidModel admits any non-empty string), so it is exactly the kind of agent-influenced string this report keeps out. It rides the report only so a *client* can prefer it over the init id",
	"Model":          "the model id observed on the session's init frame, the operator's status-bar chrome and not a fact this surface can act on: there is no model verb here, and it rides the report only so a client that attached without witnessing an init can still name the model. ConfirmedModel beside it is kept out for the same reason",
	"PRs":            "the pull requests this session opened, which is the operator's status bar and nothing this surface can act on: there is no PR verb here, and the numbers are scraped from an agent's own tool output (agentAuthored), so they are somebody else's words with nothing for a manager to do with them. It rides the report only so a reattached client can draw the segment",
}

const notInTheStatusReportCount = 10

// agent_status is the daemon's facts, and which facts is a decision that
// should fail loudly when rpc.SessionStatus grows a field.
//
// Derived from the struct rather than from a list of the fields that happen to
// be rendered today: Task 1 added Dir, Tool and ToolArg to this report *for
// this server*, and the next field added for some other consumer must force
// somebody to decide whether a manager sees it, rather than defaulting to no.
func TestAgentStatusReportsEveryFactTheDaemonCarries(t *testing.T) {
	if len(notInTheStatusReport) != notInTheStatusReportCount {
		t.Fatalf("the exemption table holds %d fields, notInTheStatusReportCount says %d: adding one is a decision, so make it a deliberate three-place edit", len(notInTheStatusReport), notInTheStatusReportCount)
	}

	s := rpc.SessionStatus{QuietMS: 123_000, PID: 4242}
	v := reflect.ValueOf(&s).Elem()
	typ := v.Type()
	sentinels := make(map[string]string)
	for i := range typ.NumField() {
		f, name := v.Field(i), typ.Field(i).Name
		sentinel := "sentinel-" + strings.ToLower(name)
		switch f.Kind() {
		case reflect.String:
			sentinels[name] = sentinel
			f.SetString(sentinel)
		case reflect.Slice:
			// []string (RequestIDs) and []int (PRs). Filled so the exemption below is
			// actually tested: neither must reach the report even though the blocked
			// line does. A slice of any other element kind is a field this guard was
			// never taught, which is a decision, not a skip.
			switch f.Type().Elem().Kind() {
			case reflect.String:
				sentinels[name] = sentinel
				f.Set(reflect.ValueOf([]string{sentinel}))
			case reflect.Int:
				// A distinct number the report must not carry, so the exemption is
				// tested the way the string sentinel is.
				sentinels[name] = "909090"
				f.Set(reflect.ValueOf([]int{909090}))
			default:
				t.Fatalf("SessionStatus.%s is a slice of %s and this guard only knows []string and []int", name, f.Type().Elem().Kind())
			}
		}
	}
	// State has to survive round-tripping through the report as a word, and a
	// sentinel is as good a word as any - but the ended branch is keyed on the
	// Error field being set, not on the state, so both are populated here.
	report := statusReport(s)

	for i := range typ.NumField() {
		name := typ.Field(i).Name
		reason, exempt := notInTheStatusReport[name]
		if exempt {
			if sentinel, ok := sentinels[name]; ok && strings.Contains(report, sentinel) {
				t.Errorf("agent_status reported %s, which the exemption table says it does not: %s", name, reason)
			}
			continue
		}
		if sentinel, ok := sentinels[name]; ok {
			if !strings.Contains(report, sentinel) {
				t.Errorf("agent_status does not report SessionStatus.%s. Either render it, or add it to notInTheStatusReport with the reason a manager is better off not seeing it:\n%s", name, report)
			}
			continue
		}
		// The one non-string field that is reported. Milliseconds are what the
		// wire carries and not what a model should be reading.
		if name == "QuietMS" {
			if want := (time.Duration(s.QuietMS) * time.Millisecond).Round(time.Second).String(); !strings.Contains(report, want) {
				t.Errorf("agent_status does not report how long the agent has been quiet as %q:\n%s", want, report)
			}
			if strings.Contains(report, fmt.Sprint(s.QuietMS)) {
				t.Errorf("agent_status printed raw milliseconds. 'alex has been waiting 4 minutes' is the daemon's fact; making a model divide by a thousand to get it is where an inference starts:\n%s", report)
			}
		}
	}

	if strings.Contains(report, fmt.Sprint(s.PID)) {
		t.Errorf("agent_status printed a pid: %s\n%s", notInTheStatusReport["PID"], report)
	}
}

func TestABlockedAgentIsReportedAsStoppedDeadUntilAHumanAnswers(t *testing.T) {
	blocked := statusReport(rpc.SessionStatus{ID: idPeter, Name: "peter", State: rpc.StateBlocked, RequestIDs: []string{"req-1"}})
	free := statusReport(rpc.SessionStatus{ID: idPeter, Name: "peter", State: rpc.StateIdle})
	if !strings.Contains(blocked, "permission request") {
		t.Errorf("a blocked agent's report does not say what it is blocked on:\n%s", blocked)
	}
	if strings.Contains(free, "permission request") {
		t.Errorf("an agent with no outstanding ask was reported as blocked. RequestID empty is load-bearing in both directions - an ask can die without being answered, so a report that never clears offers a prompt whose answer goes nowhere:\n%s", free)
	}
}

// Between turns Tool is empty, and an agent that is idle is not an agent doing
// something called "".
func TestAnAgentBetweenToolCallsIsNotReportedAsInsideOne(t *testing.T) {
	line := agentLines([]rpc.SessionStatus{{ID: idPeter, Name: "peter", Label: "api-v2", State: rpc.StateIdle}})[0]
	if strings.Contains(line, "()") {
		t.Errorf("an agent inside no tool call rendered an empty one: %q", line)
	}
	if !strings.HasSuffix(line, "-") {
		t.Errorf("an agent inside no tool call left the activity column blank: %q. Every line has to have the same shape or it is not a column, and a line ending in whitespace is one a model has to guess about", line)
	}
	report := statusReport(rpc.SessionStatus{ID: idPeter, Name: "peter", State: rpc.StateIdle})
	if strings.Contains(report, "currently:") {
		t.Errorf("agent_status claimed an idle agent is currently inside something:\n%s", report)
	}
}

// An empty Label is legitimate - a session started somewhere the daemon could
// not name - and `peter <> ` reads as a bug rather than as an absence.
func TestASessionWithNoLabelRendersAsABareName(t *testing.T) {
	s := rpc.SessionStatus{ID: idPeter, Name: "peter", State: rpc.StateIdle}
	for what, got := range map[string]string{
		"list_agents":  agentLines([]rpc.SessionStatus{s})[0],
		"agent_status": statusReport(s),
	} {
		if strings.Contains(got, "<>") {
			t.Errorf("%s left a dangling separator where the label would be:\n%s", what, got)
		}
		if !strings.Contains(got, "peter") {
			t.Errorf("%s lost the name along with the label:\n%s", what, got)
		}
	}
}

// No agent's row exceeds its bound, however its fields are spelled.
//
// # Which field makes this reachable
//
// Not the one it looks like. The tool *argument* is clipped on its way into the
// column, so a kilobyte of Bash is already handled; and the two display halves
// are bounded at the producer - daemon/names.go caps a name at 24 and
// label.go's truncateLabel caps a label at 18 runes. **The tool's own name is
// bounded by nothing.** It comes off Claude's wire as core.ToolCall.Name, so an
// MCP tool a user installed decides how long it is, and a 39-character one is
// already recorded in this project's own notes.
//
// Written because the clip survived a mutation without it: with the argument
// bounded and the digest clipping its own lines, every fixture in this package
// was short enough that deleting agentLine's bound changed nothing - while
// list_agents, which has no second bound behind it, went unbounded.
func TestNoAgentsRowExceedsItsBoundHoweverItsFieldsAreSpelled(t *testing.T) {
	s := rpc.SessionStatus{
		ID:      idPeter,
		Name:    strings.Repeat("n", 24), // daemon's maxNameLen
		Label:   strings.Repeat("l", 18), // daemon's maxLabelLen
		State:   rpc.StateWorking,
		Tool:    strings.Repeat("T", 300), // bounded by nobody
		ToolArg: strings.Repeat("a", 4000),
	}
	line := agentLines([]rpc.SessionStatus{s})[0]
	if len(line) > agentLineMax {
		t.Errorf("list_agents drew a %d-byte row against a bound of %d. Thirty of these is the context roll_up exists to avoid, arriving through the other tool: %q", len(line), agentLineMax, line)
	}
	if !strings.HasPrefix(line, idPeter) {
		t.Errorf("the bound cut the id, which is the one column the other tools need: %q", line)
	}
}

// And agent_status bounds every line of itself, over a fixture whose fields are
// **every** string rpc.SessionStatus declares.
//
// # Why the fixture is derived rather than written
//
// The first version of this test set Tool and ToolArg and asserted a bound on
// the whole report - and passed against a report that clipped only those two,
// because the fixture left Error and Dir empty. That is the same "a bound
// another bound stood in front of" shape this file already carries twice, and
// the third instance arrived through a fixture's **missing** fields rather than
// through a second bound. `SessionStatus.Error` is the one that makes it
// reachable: core's exitError appends the process's stderr tail, bounded at
// core.stderrTailBytes = 4096, so one crashed session was over four kilobytes in
// a single tool result.
//
// Reflection over the struct, so a field added to the report cannot leave this
// fixture behind - the same move TestAgentStatusReportsEveryFactTheDaemonCarries
// makes for the opposite property.
//
// The report is one agent rather than thirty, so the case for showing the whole
// command is at its strongest here, and it is still no. The value is whatever
// the model wrote or whatever the process printed as it died: a heredoc, a
// generated patch, a base64 blob. "The manager asked, so give it everything" is
// how one tool call becomes twenty thousand tokens nothing budgeted for - and
// the *whole* of any of it is still on the surface a human reads, which is the
// room.
func TestAgentStatusBoundsEveryLineOfItself(t *testing.T) {
	const huge = 5_000

	s := rpc.SessionStatus{QuietMS: 123_000, PID: 4242}
	v := reflect.ValueOf(&s).Elem()
	typ := v.Type()
	unclipped := 0
	for i := range typ.NumField() {
		if v.Field(i).Kind() != reflect.String {
			continue
		}
		// A distinct leading word per field, so a failure says which one got
		// through rather than only that something did.
		v.Field(i).SetString(strings.ToLower(typ.Field(i).Name) + "-" + strings.Repeat("z", huge))
		unclipped += huge
	}
	// State has to stay a state for the report to take the shape it takes.
	s.State = rpc.StateWorking
	// The blocked line is conditional on an outstanding ask and part of the
	// report's longest shape, but RequestIDs is a slice the string loop above
	// skips - so set it here, or the report is one line short of what
	// statusReportLines counts. The ids are never rendered, so one suffices.
	s.RequestIDs = []string{"req-1"}

	report := statusReport(s)

	// The floor: without the clip this fixture is far over the bound, so the
	// assertion below is about the code rather than about a fixture that was
	// small enough anyway.
	if unclipped <= statusReportMax {
		t.Fatalf("the fixture is %d bytes of field values against a bound of %d, so it cannot reach the bound and this test asserts nothing", unclipped, statusReportMax)
	}
	if len(report) > statusReportMax {
		t.Errorf("agent_status returned %d bytes about one agent against a bound of %d:\n%s", len(report), statusReportMax, report)
	}
	got := strings.Split(report, "\n")
	// The fixture sets every conditional line's trigger, so this is the report's
	// longest shape - which is what statusReportMax is arithmetic over. A ninth
	// line added to statusReport fails here rather than quietly making the
	// bound wrong.
	if len(got) != statusReportLines {
		t.Errorf("the longest report is %d lines and statusReportLines says %d, so the bound is arithmetic over the wrong number:\n%s", len(got), statusReportLines, report)
	}
	for _, line := range got {
		if len(line) > agentLineMax {
			t.Errorf("a %d-byte line in a report bounded to %d per line: %q", len(line), agentLineMax, line)
		}
	}
	// And every fact the report carries still identifies itself: a bound that
	// ate the labels would pass every length assertion above.
	for _, want := range []string{"name-", "id-", "dir-", "tool-", "error-"} {
		if !strings.Contains(report, want) {
			t.Errorf("the bound took %q out of the report entirely:\n%s", want, report)
		}
	}
}

// A tool with a name and no argument is still a fact worth reporting: "Bash"
// with nothing beside it is what a shell command with no display argument
// looks like on the wire.
func TestAToolWithNoArgumentStillNamesTheTool(t *testing.T) {
	line := agentLines([]rpc.SessionStatus{{ID: idPeter, Name: "peter", State: rpc.StateWorking, Tool: "Bash"}})[0]
	if !strings.Contains(line, "Bash") {
		t.Errorf("a tool call with no argument lost the tool: %q", line)
	}
	if strings.Contains(line, "Bash(") {
		t.Errorf("a tool call with no argument rendered empty parentheses: %q", line)
	}
}

// rows is a tool result's agent rows: everything after the framing note.
//
// A helper rather than a `[1:]` at each call site, because it asserts the note
// is *there*. Framing that got dropped would otherwise show up as two tests
// quietly reading one row fewer.
func rows(t *testing.T, out string) []string {
	t.Helper()

	lines := strings.Split(out, "\n")
	if len(lines) == 0 || lines[0] != agentTextNote {
		t.Fatalf("a tool result does not open with the framing note:\n%s", out)
	}
	return lines[1:]
}

// The note is Wake's own words and is the one line that must never be clipped:
// framing that got cut in half would be a sentence a model reads as the start
// of something rather than as a rule.
func TestTheFramingNoteFitsInTheLineBoundItGoesThrough(t *testing.T) {
	if len(agentTextNote) > agentLineMax {
		t.Errorf("the framing note is %d bytes against a per-line bound of %d, so every surface clips it: shorten it or raise the bound deliberately", len(agentTextNote), agentLineMax)
	}
	if len(agentTextNote) > digestLineMax {
		t.Errorf("the framing note is %d bytes against the digest's %d", len(agentTextNote), digestLineMax)
	}
}

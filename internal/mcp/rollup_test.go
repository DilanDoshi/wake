package mcp

// The digest: what it puts first, what it leaves out, and the bound that stops
// it becoming the context it exists to replace.

import (
	"fmt"
	"go/ast"
	"maps"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestRollUpGroupsTheFleetByWorkspaceAndNamesWhatEachIsOn(t *testing.T) {
	out := RollUp(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Label: "api-v2", Dir: "/repos/api", State: rpc.StateWorking, Tool: "Edit", ToolArg: "auth/token.go", QuietMS: 12_000},
		{ID: idJohn, Name: "john", Label: "api-v2", Dir: "/repos/api", State: rpc.StateBlocked},
		{ID: idMira, Name: "mira", Label: "main", Dir: "/repos/web", State: rpc.StateIdle},
	}})

	for _, want := range []string{rpc.StateBlocked, "john", "peter", "auth/token.go", "api", "web"} {
		if !strings.Contains(out, want) {
			t.Errorf("roll_up does not mention %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "john") > strings.Index(out, "mira") {
		t.Errorf("roll_up buried the blocked agent below the idle one: a digest read once has to put what needs a human first\n%s", out)
	}
	// The full path is not what a person calls a workspace, and a model reads
	// past one rather than reading it.
	if strings.Contains(out, "/repos/api") {
		t.Errorf("roll_up printed the whole directory path:\n%s", out)
	}
}

// A workspace is ordered by the most urgent thing in it, not by its name.
//
// Grouping is what makes the digest readable and it is also what can bury the
// one row worth acting on: an agent blocked in `zed` sits under every quiet
// agent in `api` if the groups are alphabetical. Ordering by urgency is still
// deterministic for a fixed report - which is the property the sort exists for -
// and puts what needs a human above the fold, which is the property the digest
// exists for.
func TestAWorkspaceWithSomethingBlockedInItComesFirstHoweverItIsSpelled(t *testing.T) {
	out := RollUp(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateIdle},
		{ID: idJohn, Name: "john", Dir: "/repos/zed", State: rpc.StateBlocked},
	}})
	if strings.Index(out, "john") > strings.Index(out, "peter") {
		t.Errorf("the blocked agent is below the idle one because its workspace sorts later. A digest is read once:\n%s", out)
	}
}

// Two calls over one report read the same, because Go randomises map iteration
// and a digest that reorders between calls is one a model cannot compare
// against its last answer.
func TestRollUpReadsTheSameTwiceOverOneReport(t *testing.T) {
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateWorking},
		{ID: idJohn, Name: "john", Dir: "/repos/web", State: rpc.StateWorking},
		{ID: idMira, Name: "mira", Dir: "/repos/infra", State: rpc.StateWorking},
	}}
	first := RollUp(st)
	for range 20 {
		if got := RollUp(st); got != first {
			t.Fatalf("roll_up reordered between two calls over one report:\n%s\n---\n%s", first, got)
		}
	}
}

// Within a workspace, the stalest work of a given state is named first - the
// same rule the roster applies, for the same reason: an agent on one call for
// twelve minutes is the one worth naming.
func TestWithinAStateTheStalestIsNamedFirst(t *testing.T) {
	out := RollUp(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateWorking, QuietMS: 1_000},
		{ID: idJohn, Name: "john", Dir: "/repos/api", State: rpc.StateWorking, QuietMS: 700_000},
	}})
	if strings.Index(out, "john") > strings.Index(out, "peter") {
		t.Errorf("the agent quiet for twelve minutes is below the one quiet for a second:\n%s", out)
	}
}

// rollUpOrder decides what a digest puts first, so it has to have an answer for
// every state a digest can be handed - which is exactly the set agentStates
// says list_agents offers, because liveSessions is what feeds both.
//
// Derived from that table rather than restated: a state ruled *offered* and
// left out of the order would be ranked last by the fall-through, silently,
// which for a seventh state that needed a human would put it under everything
// quiet.
func TestEveryStateTheDigestCanBeHandedHasAPlaceInItsOrder(t *testing.T) {
	offered := map[string]bool{}
	for state, ok := range agentStates {
		if ok {
			offered[state] = true
		}
	}
	ranked := map[string]bool{}
	for _, state := range rollUpOrder {
		if ranked[state] {
			t.Errorf("rollUpOrder names %q twice", state)
		}
		ranked[state] = true
	}
	if !maps.Equal(offered, ranked) {
		t.Errorf("rollUpOrder ranks %v and liveSessions offers %v: a state the digest can be handed and cannot rank falls to the bottom of every workspace, which is where a blocked agent must never be",
			slices.Sorted(maps.Keys(ranked)), slices.Sorted(maps.Keys(offered)))
	}
}

// The bound, over the two shapes that reach it differently: many agents in one
// workspace, and one agent in each of many workspaces.
//
// The second is the one a per-line bound misses. Every workspace costs a header
// line before any agent line is measured, so a check that only weighs the agent
// rows is green on the first fixture and unbounded on the second - and a fleet
// spread over thirty repositories is the fleet this product is for.
func TestRollUpIsBoundedSoADigestCannotBecomeTheContextItReplaces(t *testing.T) {
	for _, c := range []struct {
		name     string
		sessions []rpc.SessionStatus
	}{
		{"200 agents in one workspace", manyAgents(200, func(int) string { return "/repos/api" })},
		{"200 agents in 200 workspaces", manyAgents(200, func(i int) string {
			return fmt.Sprintf("/very/long/path/to/a/workspace/number/%03d/checkout", i)
		})},
		{"200 agents with no workspace at all", manyAgents(200, func(int) string { return "" })},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := RollUp(rpc.Status{Running: true, Sessions: c.sessions})
			if len(out) > rollUpMaxBytes {
				t.Errorf("roll_up produced %d bytes against a bound of %d. It exists so awareness is paid for once on demand instead of carried always; an unbounded one is the context it was meant to replace, arriving in a single tool result", len(out), rollUpMaxBytes)
			}
			if !strings.Contains(out, "not shown") {
				t.Errorf("a truncated digest does not say it was truncated. A model reading a short list believes it is the fleet:\n%s", out)
			}
		})
	}
}

// And the truncation is honest about how many it kept back.
func TestATruncatedDigestSaysHowManyItLeftOut(t *testing.T) {
	const total = 200
	out := RollUp(rpc.Status{Running: true, Sessions: manyAgents(total, func(int) string { return "/repos/api" })})

	shown := strings.Count(out, agentIDPrefix)
	if shown == 0 || shown >= total {
		t.Fatalf("%d of %d agents were shown, so this test is asserting nothing about the count:\n%s", shown, total, out)
	}
	if want := fmt.Sprintf(rollUpTruncated, total-shown); !strings.Contains(out, strings.TrimPrefix(want, "\n")) {
		t.Errorf("roll_up showed %d of %d agents and does not say %d were left out:\n%s", shown, total, total-shown, out)
	}
}

// A fleet at the size this product is built for is not truncated at all,
// whatever its agents are doing.
//
// **Arithmetic over the constants, with no fixture in it**, because a fixture
// closes nothing here: the value space is every fleet of thirty, and the one
// that truncates is whichever one somebody did not think to write down. What
// the bound has to survive is the worst *arrangement* - every agent alone in
// its own workspace, so every row costs a blank line and a header as well, and
// every line at its maximum length.
//
// The behavioural half is the two tests either side of this one. This is the
// half that says the number was chosen rather than picked, and it fails with
// the correction in its own message the day somebody edits either constant.
func TestADesignSizedFleetCannotBeTruncatedHoweverItIsArranged(t *testing.T) {
	const (
		lines   = 2 + 3*designFleet // the note and the headline, then a blank, a header and a row each
		perLine = digestLineMax + 1 // the newline
	)
	worst := lines*perLine + len(fmt.Sprintf(rollUpTruncated, designFleet))

	if worst > rollUpMaxBytes {
		t.Errorf("the worst arrangement of %d agents needs %d bytes and rollUpMaxBytes is %d, so a fleet the size this product is built for can be cut. Either raise the bound to at least %d or lower digestLineMax to %d",
			designFleet, worst, rollUpMaxBytes, worst, (rollUpMaxBytes-len(fmt.Sprintf(rollUpTruncated, designFleet)))/lines-1)
	}
}

// And the ordinary fleet, drawn: thirty agents over seven repositories, none of
// them cut.
func TestTheFleetThisProductIsBuiltForFitsWhole(t *testing.T) {
	out := RollUp(rpc.Status{Running: true, Sessions: manyAgents(designFleet, func(i int) string {
		return fmt.Sprintf("/Users/somebody/code/repository-%d", i%7)
	})})
	if strings.Contains(out, "not shown") {
		t.Errorf("a %d-agent fleet was truncated. 15-30 sessions is the product's own stated size, so the digest has to hold one whole:\n%s", designFleet, out)
	}
	if n := strings.Count(out, agentIDPrefix); n != designFleet {
		t.Errorf("%d of %d agents are in the digest:\n%s", n, designFleet, out)
	}
}

// One agent may not spend the digest.
//
// The line bound is what makes the arithmetic above true of a real fleet rather
// than of a tidy one: core.ToolCall.Display is whatever the model wrote, so a
// single Bash heredoc is a kilobyte on one row, and thirty of them is the
// context this tool exists to avoid arriving in one result.
func TestOneAgentCannotSpendTheWholeDigest(t *testing.T) {
	out := RollUp(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateWorking,
			Tool: "Bash", ToolArg: strings.Repeat("y", 4000)},
		{ID: idJohn, Name: "john", Dir: "/repos/api", State: rpc.StateBlocked},
	}})
	for _, line := range strings.Split(out, "\n") {
		if len(line) > digestLineMax {
			t.Errorf("a %d-byte line in a digest bounded to %d per line: %q", len(line), digestLineMax, line)
		}
	}
	if !strings.Contains(out, idJohn) {
		t.Errorf("the blocked agent was crowded out by the one with a long command:\n%s", out)
	}
}

// A multi-line tool argument is one agent on one line.
//
// A Bash heredoc is the ordinary case, and a newline in that column turns one
// agent into two rows - which breaks the one-agent-per-line shape both this and
// list_agents are read with, in a way that looks like a second agent rather than
// like a wrapped value.
func TestAToolArgumentThatSpansLinesIsFlattenedOntoOne(t *testing.T) {
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateWorking,
			Tool: "Bash", ToolArg: "cat <<EOF\nhello\nEOF"},
	}}
	for what, out := range map[string]string{"roll_up": RollUp(st), "list_agents": agentLines(liveSessions(st))[0]} {
		rows := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, idPeter) {
				rows++
			}
		}
		if rows != 1 {
			t.Errorf("%s drew one agent on %d lines:\n%s", what, rows, out)
		}
		if strings.Contains(out, "EOF\nhello") {
			t.Errorf("%s carried the newlines through:\n%s", what, out)
		}
	}
}

// A value cut in the middle of a multi-byte character is invalid UTF-8, which
// Go's JSON encoder replaces with a replacement character - so the truncation
// reaches the model as corruption rather than as a shortened value.
func TestATruncatedValueIsStillValidUTF8(t *testing.T) {
	out := RollUp(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateWorking,
			Tool: "Bash", ToolArg: strings.Repeat("日本語のコマンド", 200)},
	}})
	if !utf8.ValidString(out) {
		t.Errorf("a digest was cut through a rune: %q", out)
	}
}

// manyAgents is a fleet of n working agents with a long tool argument each,
// spread across whatever workspaces dir gives them.
func manyAgents(n int, dir func(int) string) []rpc.SessionStatus {
	out := make([]rpc.SessionStatus, n)
	for i := range out {
		out[i] = rpc.SessionStatus{
			ID:      fmt.Sprintf("1e5c1b8a-0000-4000-8000-%012d", i),
			Name:    fmt.Sprintf("a%d", i),
			Label:   "main",
			Dir:     dir(i),
			State:   rpc.StateWorking,
			Tool:    "Bash",
			ToolArg: strings.Repeat("x", 500),
		}
	}
	return out
}

// agentIDPrefix is the part of a session id every fixture above shares, so a
// count of agent lines is a count of ids rather than of newlines - a workspace
// header is a line too.
const agentIDPrefix = "1e5c1b8a-0000-4000-8000-"

func TestAnEmptyFleetSaysSoRatherThanReturningAnEmptyDigest(t *testing.T) {
	if out := RollUp(rpc.Status{Running: true}); strings.TrimSpace(out) == "" {
		t.Error("roll_up returned nothing for an empty fleet. A model reading nothing cannot tell 'no agents' from 'the tool is broken', and the second one is worth retrying")
	}
}

// The digest leaves out exactly what list_agents leaves out, because both are
// liveSessions - a parked row in a digest is a row a manager would address.
func TestTheDigestOffersExactlyWhatTheRosterOffers(t *testing.T) {
	for state, offered := range agentStates {
		t.Run(state, func(t *testing.T) {
			out := RollUp(rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
				{ID: idPeter, Name: "peter", Dir: "/repos/api", State: state},
			}})
			if got := strings.Contains(out, idPeter); got != offered {
				t.Errorf("roll_up naming a %q session = %v, want %v (the verdict agentStates gives list_agents):\n%s", state, got, offered, out)
			}
		})
	}
}

func TestRollUpIsATool(t *testing.T) {
	out := call(t, fleetOf(rpc.SessionStatus{
		ID: idPeter, Name: "peter", Dir: "/repos/api", State: rpc.StateBlocked,
	}), "roll_up", nil)
	if !strings.Contains(out, "peter") {
		t.Errorf("the roll_up tool did not return the digest:\n%s", out)
	}
}

// # The bound is a property of the digest, not of the lines somebody remembered
//
// No fixture closes it. The three above are samples of a value space with no
// largest member, and the mutant that beats them is not a bigger fleet - it is
// **one more write**: a summary footer, a second header, the truncation notice
// itself appended with fmt.Fprintf after the loop has stopped measuring. The
// draft this task started from did exactly that, and its own bound test passed
// only because the overshoot was smaller than the fixture's slack.
//
// So the static half: every write into the digest goes through the one function
// that decides whether it fits. A second write site is a build failure whatever
// it writes, which is the rung-5 shape - constrain the form, not only the
// inputs.
func TestEveryWriteIntoTheDigestGoesThroughTheBoundedOne(t *testing.T) {
	f, ok := parsePackage(t)[rollUpFile]
	if !ok {
		t.Fatalf("no %s in this package: the file was renamed and this guard is reading nothing", rollUpFile)
	}

	writes := 0
	for _, decl := range f.Decls {
		fn, isFunc := decl.(*ast.FuncDecl)
		if !isFunc || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			sel, isSel := call.Fun.(*ast.SelectorExpr)
			if !isSel || !digestWriters[sel.Sel.Name] {
				return true
			}
			if fn.Name.Name != boundedAppender {
				t.Errorf("%s writes into the digest with %s, and only %s may: the bound is enforced there, so a second write site is a line nothing measured. Put it through %s or say here why it is exempt",
					fn.Name.Name, sel.Sel.Name, boundedAppender, boundedAppender)
				return true
			}
			writes++
			return true
		})
	}
	if writes == 0 {
		t.Fatalf("%s does not write anything into the digest: the scan is broken and this guard cannot fail", boundedAppender)
	}
}

// The write calls that put bytes into the digest. Fprint* rather than only
// WriteString because the draft used Fprintf, which is how a formatted line
// gets appended without anybody thinking of it as a write.
var digestWriters = map[string]bool{
	"WriteString": true, "WriteByte": true, "WriteRune": true, "Write": true,
	"Fprintf": true, "Fprint": true, "Fprintln": true,
}

const (
	rollUpFile      = "rollup.go"
	boundedAppender = "add"
)

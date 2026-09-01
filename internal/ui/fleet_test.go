package ui

// The room's filter, read twice.
//
// Every case in roomCases is behaviour a reader can check by eye, and
// TestEveryEventKindTheAirlockCanProduceHasARoomDecision reads the same table
// as an *obligation*: it derives the kind set from internal/core/event.go, so a
// fifteenth kind is a failure here rather than an event that quietly takes the
// default branch. decisions.md names the alternative - a hand-written list
// standing in for something the code already declares - as the dominant shape
// of a test that cannot fail, and a filter is exactly where that costs the
// most: three consumers read what Observe returns.

import (
	"reflect"
	"slices"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// roomCase is one event and whether the room draws anything for it.
type roomCase struct {
	name string
	ev   core.Event
	want bool
}

// roomCases covers every kind core declares, plus the sub-cases inside the
// three kinds the room does not admit wholesale.
func roomCases() []roomCase {
	return []roomCase{
		{"the agent's own prose", core.Event{Kind: core.KindAssistantText, Text: "Fixed the retry header, tests pass"}, true},
		{"your own turn", core.Event{Kind: core.KindUserText, Text: "look at the retry header"}, true},
		{"an ask", core.Event{Kind: core.KindPermissionRequest, RequestID: "r1"}, true},
		{"an ask dying", core.Event{Kind: core.KindRequestWithdrawn, RequestID: "r1"}, true},
		{"a turn that said nothing", core.Event{Kind: core.KindTurnEnd}, true},

		{"a subagent's prose", core.Event{Kind: core.KindAssistantText, Text: "hi", Subagent: &core.Subagent{Dispatch: "toolu_1"}}, false},
		{"the prompt an agent handed a subagent", core.Event{Kind: core.KindUserText, Text: "go and look", Subagent: &core.Subagent{Dispatch: "toolu_1"}}, false},
		{"prose with nothing in it", core.Event{Kind: core.KindAssistantText, Text: " \n\t "}, false},
		{"Claude's abort marker wearing a user frame", core.Event{Kind: core.KindUserText, Text: "an abort marker", Notice: core.NoticeTurnInterrupted}, false},
		{"thinking", core.Event{Kind: core.KindThinking, Text: "hmm"}, false},
		// A preview belongs to one conversation and the room is every
		// conversation. It is drawn where it can be *replaced* by the block it
		// previews - a pane that follows one agent - and the room interleaves
		// thirty, so a token arriving here would append a line rather than
		// update one, and the completed block would land under thirty
		// half-sentences that nothing supersedes. The rate settles it either
		// way: at the corpus's median 43.5 tokens a second across the fleet
		// this is ~1,300 lines a second into the one surface whose whole job is
		// to be the filtered one. See internal/ui/partial.go.
		{"an agent's block being written", core.Event{Kind: core.KindPartialText, Text: "Fixed the ret"}, false},
		// How much the turn in flight has produced. It carries no text and
		// nothing to draw - the room would be appending a line per message per
		// agent to say a number moved, on the surface whose whole job is to be
		// the filtered one. It is folded onto the agent instead, by withFacts,
		// and read by the roster row and the working line. See
		// core.KindTurnTokens.
		{"what the turn in flight has produced", core.Event{Kind: core.KindTurnTokens, Session: &core.SessionFacts{TurnOutputTokens: 412}}, false},
		// The boundary between one message's count and the next one's. It
		// carries nothing at all, so there is nothing for the room to draw -
		// it exists to make the counts addable. See core.KindMessageStart.
		{"one message of a turn beginning", core.Event{Kind: core.KindMessageStart}, false},
		{"a tool call", core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Edit"}}, false},
		{"a tool result", core.Event{Kind: core.KindToolResult, Text: "ok"}, false},
		{"system chatter", core.Event{Kind: core.KindSystem, Text: "lifecycle"}, false},
		{"a control receipt", core.Event{Kind: core.KindControlReceipt}, false},
		// Claude's answer to a rewind_conversation request - Control's own
		// receipt, one kind over, and the same non-decision: it is Wake
		// acknowledging its own request rather than conversation content.
		{"a rewind receipt", core.Event{Kind: core.KindRewindReceipt}, false},
		{"the fate of a message Wake sent", core.Event{Kind: core.KindMessageState, MessageID: "m1"}, false},
		{"a quota report", core.Event{Kind: core.KindRateLimit, Text: "fine"}, false},
		{"a session id dying", core.Event{Kind: core.KindSessionReset, SessionID: "s1"}, false},
		{"a frame Wake does not model", core.Event{Kind: core.KindUnknown, Text: "whatever"}, false},
	}
}

func TestTheRoomSeesProseAndAsksAndNothingElse(t *testing.T) {
	for _, c := range roomCases() {
		t.Run(c.name, func(t *testing.T) {
			_, out := NewFleet().Observe(c.ev, "s1")
			if got := len(out) > 0; got != c.want {
				t.Errorf("the room drew it = %v, want %v", got, c.want)
			}
		})
	}
}

// The kinds are core's, and a kind nobody decided about would take the default
// branch and vanish from the room with nothing saying so. Derived from the
// declaration rather than restated: a hand-written list of the fourteen is the
// exact failure decisions.md names.
func TestEveryEventKindTheAirlockCanProduceHasARoomDecision(t *testing.T) {
	decided := map[string]bool{}
	for _, c := range roomCases() {
		decided[string(c.ev.Kind)] = true
	}
	for _, kind := range declaredConstants(t, "../core/event.go", "Kind") {
		if !decided[kind] {
			t.Errorf("core decodes events of kind %q and no case in roomCases says what the room does with one: "+
				"it would take Observe's default branch and never reach the room, which is a decision nobody made", kind)
		}
	}
}

func TestATurnThatSaidSomethingNeedsNoMarkerAndATurnThatSaidNothingGetsOne(t *testing.T) {
	f := NewFleet()

	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "done"}, "s1")
	f, out := f.Observe(core.Event{Kind: core.KindTurnEnd}, "s1")
	if len(out) != 0 {
		t.Errorf("a turn that produced prose also drew %d marker(s): the agent's own words are the report, and Wake adding `finished` under them puts two lines in the room for one event", len(out))
	}

	f, out = f.Observe(core.Event{Kind: core.KindTurnEnd}, "s2")
	if len(out) != 1 || out[0].Kind != core.KindTurnEnd {
		t.Fatalf("a turn that produced no prose drew %v, want one turn-end marker: 8 of 52 recorded turns are silent and the room would show nothing at all for them", out)
	}
	if out[0].SessionID != "s2" {
		t.Errorf("the quiet marker names agent %q, want %q: it is synthesised rather than relayed, so nothing else on it says whose turn ended", out[0].SessionID, "s2")
	}
	if _, ok := f.Agent("s2"); !ok {
		t.Error("an agent first heard from through an event was not recorded: fan-out starts before a spawn is confirmed, so an event can precede every report")
	}
}

func TestTheProseFlagIsPerAgentAndDoesNotLeakAcrossTurns(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "done"}, "s1")

	// One agent speaking must not cover for another's silence.
	if _, out := f.Observe(core.Event{Kind: core.KindTurnEnd}, "s2"); len(out) != 1 {
		t.Errorf("a silent agent's turn end drew %d markers while another agent had spoken, want 1: the flag is per agent", len(out))
	}

	f, _ = f.Observe(core.Event{Kind: core.KindTurnEnd}, "s1")

	// A second, silent turn from the same agent must still get its marker: the
	// flag is cleared by the turn end that consumed it, not carried forward.
	_, out := f.Observe(core.Event{Kind: core.KindTurnEnd}, "s1")
	if len(out) != 1 {
		t.Errorf("a later silent turn drew %d markers, want 1: the prose flag survived the turn that spent it", len(out))
	}
}

// One pass, three consumers: the same call that hands the room a line has to
// leave the sidebar's row and the unread count right, or the three views
// disagree about what arrived.
func TestOneTurnFeedsTheRoomTheSidebarAndTheUnreadCountFromOnePass(t *testing.T) {
	f := NewFleet()

	f, out := f.Observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Edit", Display: "auth/token.go"}}, "s1")
	if len(out) != 0 {
		t.Errorf("a tool call drew %d room lines, want 0: what an agent is doing goes to the sidebar", len(out))
	}
	a, _ := f.Agent("s1")
	if a.Tool != "Edit" || a.ToolArg != "auth/token.go" {
		t.Errorf("the sidebar row is %q/%q after a tool call, want Edit/auth/token.go: the stream is what keeps it current between state pushes", a.Tool, a.ToolArg)
	}
	if a.Unread != 0 {
		t.Errorf("a tool call left %d unread, want 0: nothing was drawn in the room to be unread of", a.Unread)
	}

	f, out = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "Fixed it"}, "s1")
	if len(out) != 1 {
		t.Fatalf("the agent's prose drew %d room lines, want 1", len(out))
	}
	if a, _ = f.Agent("s1"); a.Unread != 1 {
		t.Errorf("prose left %d unread, want 1", a.Unread)
	}

	f, _ = f.Observe(core.Event{Kind: core.KindTurnEnd}, "s1")
	if a, _ = f.Agent("s1"); a.Tool != "" || a.ToolArg != "" {
		t.Errorf("the sidebar still reads %q/%q after the turn ended, want empty: nothing is being run any more", a.Tool, a.ToolArg)
	}
}

func TestUnreadAccumulatesForEveryAgentButTheOneYouAreLookingAt(t *testing.T) {
	f := NewFleet().Focus("s1")
	if f.Focused() != "s1" {
		t.Fatalf("Focused = %q, want s1", f.Focused())
	}
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "a"}, "s1")
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "b"}, "s2")
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "c"}, "s2")

	if a, _ := f.Agent("s1"); a.Unread != 0 {
		t.Errorf("the focused agent has %d unread, want 0", a.Unread)
	}
	if a, _ := f.Agent("s2"); a.Unread != 2 {
		t.Errorf("an unfocused agent has %d unread, want 2: an hour inside one DM must not cost you what accumulated", a.Unread)
	}
	if a, _ := f.Focus("s2").Agent("s2"); a.Unread != 0 {
		t.Error("opening an agent's DM did not read what had accumulated for it")
	}
	if a, _ := f.MarkRead("s2").Agent("s2"); a.Unread != 0 {
		t.Error("MarkRead left unread lines behind")
	}
}

// Everything the room draws is unread except the words you typed yourself,
// and the quiet marker is one of the things it draws.
func TestEveryLineTheRoomDrawsCountsExceptYourOwnTurn(t *testing.T) {
	for _, c := range roomCases() {
		if !c.want {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			f, _ := NewFleet().Observe(c.ev, "s1")
			a, _ := f.Agent("s1")
			want := 1
			if c.ev.Kind == core.KindUserText {
				want = 0
			}
			if a.Unread != want {
				t.Errorf("a room line of kind %q left %d unread, want %d: an unread badge is what says there is something here you have not seen", c.ev.Kind, a.Unread, want)
			}
		})
	}
}

func TestAnOlderFleetKeepsTheRosterItHad(t *testing.T) {
	older := NewFleet()
	newer, _ := older.Observe(core.Event{Kind: core.KindAssistantText, Text: "a"}, "s1")
	if len(older.Agents()) != 0 {
		t.Error("Observe wrote through to the Fleet it was called on: every value in this package is copied on write, and Bubble Tea hands models around by value")
	}
	if len(newer.Agents()) != 1 {
		t.Error("Observe did not record the agent it just heard from")
	}

	newest, _ := newer.Observe(core.Event{Kind: core.KindAssistantText, Text: "b"}, "s1")
	if a, _ := newer.Agent("s1"); a.Unread != 1 {
		t.Errorf("the older Fleet's unread count moved to %d: a copy on write that shares the map is not a copy", a.Unread)
	}
	if a, _ := newest.Agent("s1"); a.Unread != 2 {
		t.Errorf("the newer Fleet counted %d unread, want 2", a.Unread)
	}
}

// The copy is what makes a Fleet safe to hold by value, and gating it on a
// change is what keeps that affordable: 55.7% of the recorded stream is
// lifecycle chatter, which moves nothing about any agent, and a fleet-sized
// copy per chatter frame is work per frame that could be work per change.
//
// Both directions, because the first assertion alone would pass with the copy
// deleted outright - and that is the mutation that makes two holders of one
// Fleet disagree about the roster.
func TestAnEventThatChangesNothingDoesNotCopyTheFleet(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateWorking},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	}})
	before := rosterIdentity(f)

	quiet, out := f.Observe(core.Event{Kind: core.KindSystem, Text: "lifecycle"}, "s1")
	if len(out) != 0 {
		t.Fatalf("chatter drew %d room lines, want 0", len(out))
	}
	if rosterIdentity(quiet) != before {
		t.Error("an event that changed nothing copied the whole fleet: more than half the stream is chatter, and at 30 agents that is a fleet-sized copy per frame for no change at all")
	}

	loud, _ := f.Observe(core.Event{Kind: core.KindAssistantText, Text: "hi"}, "s1")
	if rosterIdentity(loud) == before {
		t.Fatal("an event that changed something did not copy either: this guard passes with the copy deleted outright, and a Bubble Tea model handed around by value would then see its history rewritten under it")
	}
	if a, _ := f.Agent("s1"); a.Unread != 0 {
		t.Errorf("the Fleet that was written through has %d unread: the copy did not happen before the write", a.Unread)
	}
}

// rosterIdentity is which map a Fleet is holding, which is the only way to see
// a copy that did or did not happen.
func rosterIdentity(f Fleet) uintptr { return reflect.ValueOf(f.agents).Pointer() }

// notCarriedOntoAnAgent is every rpc.SessionStatus field the client's own
// record deliberately drops, with the reason. Checked both ways below: a field
// added to the report with no decision here fails, and an excuse whose field no
// longer exists fails too - decisions.md's rule for a list that genuinely has
// to be hand-written.
var notCarriedOntoAnAgent = map[string]string{
	"Error":      "why an ended session ended. Nothing in the room draws it yet; the DM already gets it as a FrameError, and the roster row that would show it is the sidebar's task",
	"PID":        "the process group a later daemon reaps. Not a display field, and the UI never touches a process",
	"Dir":        "where the session was *started*. It is the daemon's own business - park writes it down, unpark launches from it, a fork runs in it - because claude locates a transcript by it. No surface here wants it: every reader of Agent.Cwd wants where the agent is now, which is what runningIn folds",
	"RequestIDs": "the asks a blocked session owes. Cards owns these, seeded from the permission-request events and reconciled against this same report - the agent record keeps no ask id, so there is nothing on Agent for a second copy to go stale against",
	"Commands":   "the slash commands a session advertised. Carried, but folded into Agent.advertised (a *commandSet the completion menu reads) via withCommands rather than a same-named field, the way Dir folds into Cwd - the report is the only route to them for a client that attached after the init event carried them",
	"PRs":        "the pull requests a session opened. Carried, but folded into Agent.prs (a *prSet the status bar reads) via withPRs rather than a same-named field - Commands' own shape, and for Commands' reason: Agent must stay comparable, so a slice is a pointer here",
}

// A fleet report is folded onto an Agent field by field, and this derives that
// obligation from rpc.SessionStatus rather than restating it: a report field
// added and not folded is a row that silently stops updating. Task 1 added
// three of them at once.
func TestWithStatusCarriesEveryFieldTheReportHasOrSaysWhyNot(t *testing.T) {
	report := everyFieldSet(t)
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{report}})

	got, ok := f.Agent(report.ID)
	if !ok {
		t.Fatalf("no agent for the session the report named")
	}

	rt, at := reflect.TypeOf(report), reflect.TypeOf(got)
	rv, av := reflect.ValueOf(report), reflect.ValueOf(got)
	for i := range rt.NumField() {
		name := rt.Field(i).Name
		af, found := at.FieldByName(name)
		if !found {
			if _, excused := notCarriedOntoAnAgent[name]; !excused {
				t.Errorf("rpc.SessionStatus.%s reaches every client and ui.Agent has no field for it: carry it, or add it to notCarriedOntoAnAgent with the reason", name)
			}
			continue
		}
		want, have := rv.Field(i).Interface(), av.FieldByIndex(af.Index).Interface()
		if !reflect.DeepEqual(want, have) {
			t.Errorf("Agent.%s = %v after folding a report carrying %v: WithStatus does not copy it", name, have, want)
		}
	}

	for name := range notCarriedOntoAnAgent {
		if _, found := rt.FieldByName(name); !found {
			t.Errorf("notCarriedOntoAnAgent excuses rpc.SessionStatus.%s, which no longer exists: a dead excuse is what makes deleting a field a three-place edit", name)
		}
		// And the third way this list goes stale, which is the one that had
		// already happened by the time it was noticed: the excuse survives the
		// change that carries the field. The loop above never consults an
		// excuse for a field Agent has, so an entry saying "no pane draws
		// lineage yet" sat beside a header drawing exactly that, green.
		if _, carried := at.FieldByName(name); carried {
			t.Errorf("notCarriedOntoAnAgent excuses rpc.SessionStatus.%s and ui.Agent carries it: the excuse is now a false statement about this build, and nothing else here reads it", name)
		}
	}
}

// everyFieldSet builds a report with every field distinctly non-zero, and
// fails on a field it does not know how to fill rather than leaving it zero -
// a zero on both sides compares equal, which is how a coverage guard quietly
// stops covering the field somebody just added.
func everyFieldSet(t *testing.T) rpc.SessionStatus {
	t.Helper()
	var s rpc.SessionStatus
	v := reflect.ValueOf(&s).Elem()
	rt := v.Type()
	for i := range rt.NumField() {
		switch f := v.Field(i); f.Kind() {
		case reflect.String:
			f.SetString(rt.Field(i).Name + "-value")
		case reflect.Int, reflect.Int64:
			f.SetInt(int64(i) + 1)
		case reflect.Slice:
			// []string (RequestIDs, Commands) and []int (PRs). A zero slice compares
			// equal on both sides, so fill it for the same reason the strings are.
			switch f.Type().Elem().Kind() {
			case reflect.String:
				f.Set(reflect.ValueOf([]string{rt.Field(i).Name + "-value"}))
			case reflect.Int:
				f.Set(reflect.ValueOf([]int{i + 1}))
			default:
				t.Fatalf("rpc.SessionStatus.%s is a slice of %s and this helper only knows []string and []int", rt.Field(i).Name, f.Type().Elem().Kind())
			}
		default:
			t.Fatalf("rpc.SessionStatus.%s is a %s and this helper cannot fill it: a field left zero compares equal on both sides, so the guard would pass over it",
				rt.Field(i).Name, f.Kind())
		}
	}
	return s
}

// The report is a snapshot and the stream is fresher between state changes, so
// a report assembled before the current call must not blank the row.
func TestAReportWithNoToolLeavesTheRowTheStreamSet(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Bash", Display: "go test ./..."}}, "s1")
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}})

	a, _ := f.Agent("s1")
	if a.Tool != "Bash" || a.ToolArg != "go test ./..." {
		t.Errorf("the row reads %q/%q after a report that named no tool, want the one the stream set: a status push fires on a state change and an agent stays working across ten tool calls", a.Tool, a.ToolArg)
	}
	if a.Name != "alex" {
		t.Errorf("Name = %q, want alex: the report is still the only source for everything else", a.Name)
	}
	if got := len(f.Agents()); got != 1 {
		t.Errorf("an agent heard from and then reported on is %d rows, want 1", got)
	}
}

func TestForgettingATurnClearsItsToolBeforeAnIdleReport(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{
		ID: "s1", Name: "alex", State: rpc.StateWorking,
		Tool: "Bash", ToolArg: "rm -rf /tmp/x",
	}}})
	older := f

	f = f.ForgetTurns()
	if a, _ := older.Agent("s1"); a.Tool != "Bash" || a.ToolArg != "rm -rf /tmp/x" {
		t.Errorf("ForgetTurns changed the older Fleet's tool to %q/%q: Fleet values are copied on write", a.Tool, a.ToolArg)
	}
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})

	a, _ := f.Agent("s1")
	if a.State != rpc.StateIdle || a.Tool != "" || a.ToolArg != "" {
		t.Errorf("after gap + idle report: state=%q tool=%q arg=%q, want idle with no tool", a.State, a.Tool, a.ToolArg)
	}
}

// ForgetTurns drops what the *stream* told this client about a turn in flight,
// and nothing else. Doing is fold's, set from a tool call and cleared by the
// ending, so it goes with the tool. spoke and inDM deliberately stay: `dropped`
// is one counter for the whole fleet, so this runs for every agent when any
// agent loses a frame, and neither of those is something the gap casts doubt on.
func TestForgettingATurnKeepsWhatTheStreamNeverSaid(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{
		ID: "s1", Name: "alex", State: rpc.StateWorking,
		Tool: "Bash", ToolArg: "rm -rf /tmp/x",
	}}})
	f = f.sending("s1", true)
	a := f.agents["s1"]
	a.spoke, a.Doing = true, "Investigating a flaky test"
	f.agents["s1"] = a

	got := f.ForgetTurns().agents["s1"]

	if got.Doing != "" {
		t.Errorf("Doing = %q, want cleared: boardDetail draws it with no state guard, so an idle agent keeps a present-tense claim about work that is not happening", got.Doing)
	}
	if !got.inDM {
		t.Error("inDM was cleared: it is set by Fleet.sending rather than by the stream, the turn it belongs to may still be running, and one agent's dropped frame would put another agent's private turn into the room")
	}
	if !got.spoke {
		t.Error("spoke was cleared: the real KindTurnEnd then draws a finished marker under prose that plainly finished")
	}
}

func TestWithStatusIgnoresNothingAndInventsNothing(t *testing.T) {
	f := NewFleet().WithStatus(nil)
	if len(f.Agents()) != 0 {
		t.Error("a nil report invented a roster")
	}
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "hi"}, "s1")
	if got := len(f.Agents()); got != 1 {
		t.Errorf("an agent reported on and then heard from is %d rows, want 1", got)
	}
}

func TestByNameResolvesALiveAgentAndNeverAnEndedOne(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateEnded},
		{ID: "s2", Name: "sydney", State: rpc.StateWorking},
	}})

	if a, ok := f.ByName("sydney"); !ok || a.ID != "s2" {
		t.Errorf("ByName(sydney) = %v/%v, want the live agent s2", a.ID, ok)
	}
	if _, ok := f.ByName("alex"); ok {
		t.Error("ByName resolved an ended session: the daemon releases a name when its session ends, so the next agent to hold it is the one anybody means")
	}
	if _, ok := f.ByName("syd"); ok {
		t.Error("ByName resolved a prefix: prefix resolution belongs to `wake attach`, where a human is typing at a shell rather than routing a message")
	}
	if _, ok := f.Agent("nobody"); ok {
		t.Error("Agent invented a session nobody reported")
	}
}

// Agents is the roster the sidebars draw, and it comes back ranked. Asserted
// against Rank itself rather than against a second copy of the order, so the
// two cannot drift.
func TestAgentsComesBackInAttentionOrder(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateIdle},
		{ID: "s2", State: rpc.StateBlocked},
		{ID: "s3", State: rpc.StateWorking, QuietMS: 5_000},
	}})

	got := ids(f.Agents())
	want := ids(Rank([]Agent{
		{ID: "s1", State: rpc.StateIdle},
		{ID: "s2", State: rpc.StateBlocked},
		{ID: "s3", State: rpc.StateWorking, QuietMS: 5_000},
	}))
	if !slices.Equal(got, want) {
		t.Errorf("Agents = %v, want %v: the roster is Rank's order, not first-seen order", got, want)
	}
}

func ids(agents []Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.ID)
	}
	return out
}

// An ended agent leaves the roster and a parked one does not.
//
// The difference is what an operator can do about it: /resume brings a parked
// session back with its context, while an ended one is gone and its name is
// already back in the pool - so its row is a handle that reaches nobody, and
// can be one a live agent now answers to. Nothing removed an agent from the
// fleet at all before this, so a dead row was drawn for the life of the window
// in a sidebar that does not scroll.
func TestTheRosterDropsAnEndedAgentAndKeepsAParkedOne(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "a", Name: "alex", State: rpc.StateIdle},
		{ID: "k", Name: "kwame", State: rpc.StateParked},
		{ID: "j", Name: "john", State: rpc.StateEnded},
	}})

	var names []string
	for _, a := range f.OnRoster() {
		names = append(names, a.Name)
	}
	if slices.Contains(names, "john") {
		t.Errorf("the roster still draws an ended agent: %v", names)
	}
	if !slices.Contains(names, "kwame") {
		t.Errorf("the roster dropped a parked agent, which /resume brings back: %v", names)
	}
	if _, ok := f.Agent("j"); !ok {
		t.Error("the ended agent left the fleet as well as the roster: the room's account of it, and this client's own ending, both read from there")
	}
}

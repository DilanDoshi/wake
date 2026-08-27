package ui

// ⌃F: branching the conversation you are reading.
//
// The key writes a frame and nothing else - the daemon spawns, this view never
// touches a process - so what these tests hold is the frame, the wait for a
// confirmation, and the two sentences the operator reads.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The id in the frame is the **fork's**, minted here, and the parent's rides
// beside it. Wake originates identity: maySpawn refuses a spawn with no id of
// its own, and the reaper's entire proof that a process group is an agent's is
// that UUID appearing in the argv - so a fork that let the daemon mint one
// would be a fork this client could not then recognise when it arrived.
func TestCtrlFAsksTheDaemonToForkTheOpenConversation(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(120, 30)

	_, cmd, handled := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	if !handled {
		t.Fatal("⌃F fell through to the composer")
	}
	go func() { _ = runCmdQuietly(cmd) }()

	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameFork {
		t.Fatalf("⌃F wrote a %q frame, want %q", f.Kind, rpc.FrameFork)
	}
	if f.ParentID != "s1" {
		t.Errorf("the frame's ParentID is %q, want the conversation that had the keys", f.ParentID)
	}
	if _, err := uuid.Parse(f.SessionID); err != nil {
		t.Errorf("the frame's SessionID is %q and is not a UUID (%v): the daemon refuses anything else, "+
			"because the reaper identifies a process group by finding that id in an argv", f.SessionID, err)
	}
	if f.SessionID == f.ParentID {
		t.Error("the fork and its parent were given the same id")
	}
}

// The payoff for ⌃F: after the daemon confirms, the operator is *in* the fork.
// Without this the key produces a new roster row and a conversation they then
// have to go and find, which is not what "branch this" means.
//
// And the sentence, which is constraint §10.2 of the live-fork recording: a
// fork is a snapshot. The parent's later turns appear in no fork's transcript
// at either recorded generation, and nothing stops the operator typing to the
// parent one keystroke later.
func TestTheForkOpensItsOwnConversationWhenTheDaemonConfirmsIt(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	// The model the key hands back, not the one it was given: fork records the
	// id it minted on the App it returns, and a test that discarded it would be
	// asserting about a client that never pressed anything.
	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	forkID := awaitFrame(t, sent).SessionID

	// The daemon's confirmation: the whole fleet, with the fork in it.
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: forkID, Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st})

	if a.focus != forkID {
		t.Errorf("the pane holds %q, want the fork %q", a.focus, forkID)
	}
	if len(a.pendingStarts) != 0 {
		t.Errorf("%d forks are still pending after the fork arrived", len(a.pendingStarts))
	}
	if got := shown(a); !strings.Contains(got, "nothing @alex does next reaches it") {
		t.Errorf("nothing says the fork is a snapshot:\n%s", got)
	}
}

// A refusal is addressed to the fork's own id - daemon.fork's comment is about
// exactly this - so it is what stops the client waiting. Without it a fork
// refused because the parent was mid-turn leaves pendingFork set, and the next
// unrelated session to be given that id would steal the pane.
func TestARefusedForkStopsTheClientWaitingForIt(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	// The model the key hands back, not the one it was given: fork records the
	// id it minted on the App it returns, and a test that discarded it would be
	// asserting about a client that never pressed anything.
	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	forkID := awaitFrame(t, sent).SessionID

	a = a.applyFrame(rpc.Frame{
		Kind:      rpc.FrameError,
		SessionID: forkID,
		Text:      "alex is in the middle of a turn. Fork it when the turn ends, or stop the turn first.",
	})

	if len(a.pendingStarts) != 0 {
		t.Fatalf("%d forks are still pending after the daemon refused the fork", len(a.pendingStarts))
	}
	if got := shown(a); !strings.Contains(got, "turn ends") {
		t.Errorf("the refusal was not reported, so the key looks like it did nothing:\n%s", got)
	}
	if a.focus != "s1" {
		t.Errorf("the pane moved to %q on a refused fork, want the conversation it was on", a.focus)
	}
}

// Two presses are two forks, and neither is lost. This is the feature's own
// stated purpose - v1_goals.md calls fork "the natural way to explore two
// approaches from one context", and two approaches is ⌃F ⌃F across a `claude`
// spawn that takes seconds - so a one-slot wait would drop the first fork on
// precisely the input the feature exists for.
//
// What "lost" meant: the first fork ran, sat in the fleet with a roster row, and
// was reachable by nothing on the keyboard. It was not in dmOrder so ⇥ missed
// it, ⌃D reopens dmTarget's which is now the fork that *did* open, and the
// roster is auto-hidden while a DM is up. Recovery was ⌃R, a click, and ⌃D.
func TestASecondForkAskedForBeforeTheFirstArrivesIsNotLost(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	first := awaitFrame(t, sent).SessionID

	m, cmd, _ = a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	second := awaitFrame(t, sent).SessionID

	if first == second {
		t.Fatalf("both presses minted %q, so this test cannot tell the two forks apart", first)
	}

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: first, Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
			{ID: second, Name: "john", State: rpc.StateIdle, ParentID: "s1"},
		},
	}})

	// Both are conversations the operator asked for, so both are in the ring ⇥
	// walks. Which one keeps the pane is a decision and the last to arrive is
	// the honest answer; being in the ring is what makes the other reachable.
	for who, id := range map[string]string{"the first fork": first, "the second fork": second} {
		if !slices.Contains(a.dmOrder, id) {
			t.Errorf("%s (%s) is not in the ⇥ ring: it is running, it has a roster row, and no key reaches it", who, id)
		}
	}
	if a.focus != second {
		t.Errorf("the pane holds %q, want the fork that arrived last (%q)", a.focus, second)
	}
	if len(a.pendingStarts) != 0 {
		t.Errorf("%d forks are still pending after both arrived", len(a.pendingStarts))
	}
}

// The reused-name rule holds for the *sentence* as well as the header, and the
// sentence is the harder half: it says what @alex does **next**, so a handle
// that now belongs to somebody else is a claim about a live agent that is not
// the parent - and it is a handle the reader can type.
//
// The header was guarded and this was not, one line below it in the same
// function: `parentName` has the cross-check, `agentName` does not.
func TestTheSnapshotSentenceDoesNotNameAParentWhoseNameWentBackToThePool(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	// Not dmApp: the parent has to end for its name to go back to the pool, and
	// dmApp makes it *this client's own* session, whose ending noteEnding
	// reports on the same row - so the assertion would be reading that notice
	// instead of this one.
	seed := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}}
	a := NewRoomApp(conn, Stream{}, &seed).withSize(160, 30).openDMWith("s1", "alex")

	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	forkID := awaitFrame(t, sent).SessionID

	// alex ended between the keypress and the confirmation, and the pool gave
	// its name to a session that has nothing to do with this fork.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusReply, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateEnded},
			{ID: "s3", Name: "alex", State: rpc.StateIdle},
			{ID: forkID, Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
		},
	}})

	got := shown(a)
	if strings.Contains(got, agentPrefix+"alex") {
		t.Errorf("the snapshot sentence names @alex, which is now a live agent that is not the parent:\n%s", got)
	}
	if !strings.Contains(got, "is a fork") {
		t.Errorf("losing the parent's name lost the snapshot promise with it:\n%s", got)
	}
}

// An error about somebody else must not cancel a fork this client is waiting
// for. At fifteen agents an unrelated crash while a fork is starting is an
// ordinary event, and under the wide version of that guard the fork then
// arrives and opens nothing - the lost-pane symptom, triggered by a stranger.
func TestAnErrorAboutAnotherAgentLeavesAPendingForkAlone(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	a = m.(App)
	go func() { _ = runCmdQuietly(cmd) }()
	forkID := awaitFrame(t, sent).SessionID

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: "exit status 1"})
	if len(a.pendingStarts) != 1 {
		t.Fatalf("an error addressed to s1 left %d forks pending, want the one that is still coming", len(a.pendingStarts))
	}

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusReply, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: forkID, Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
		},
	}})
	if a.focus != forkID {
		t.Errorf("the fork arrived and the pane holds %q: the wait had already been cancelled by somebody else's failure", a.focus)
	}
}

// A conversation that is a branch of another has to say so where its identity
// already is. Without it two DMs called alex and sydney look like two agents
// that have nothing to do with each other, which is the one thing a fork is
// not.
func TestAForksHeaderNamesTheConversationItCameFrom(t *testing.T) {
	fresh(t)
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
	}}
	a := NewRoomApp(nil, Stream{}, &st).withSize(160, 30)
	a = a.openDMWith("s2", "sydney")

	if got := shown(a); !strings.Contains(got, "@sydney  forked from @alex") {
		t.Errorf("the DM header does not name the conversation sydney was forked from:\n%s", got)
	}
}

// A name is never an address, and the DM header is *the* addressing surface -
// so a parent whose name now belongs to a different live agent is not named
// there at all.
//
// The report can hold both: a name goes back to the pool when its session ends
// and the ending stays in the report, so a fork's parent can end, a new session
// can draw that name, and both rows arrive together. `forked from @alex` would
// then name a live agent that is not the parent - and typing @alex resolves
// that live one, because Fleet.ByName skips ended sessions. cmd/wake ruled on
// exactly this one commit ago (status.go's sessionNames) and the TUI is the
// other half of the same surface.
func TestAForksHeaderDoesNotNameAParentWhoseNameWentBackToThePool(t *testing.T) {
	fresh(t)
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateEnded},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
		{ID: "s3", Name: "alex", State: rpc.StateIdle},
	}}
	a := NewRoomApp(nil, Stream{}, &st).withSize(160, 30)
	a = a.openDMWith("s2", "sydney")

	if got := shown(a); strings.Contains(got, "forked from") {
		t.Errorf("the header named a parent whose handle now addresses a different live agent:\n%s", got)
	}
}

// And a parent this client cannot name is not drawn at all, rather than drawn
// as an id.
//
// Reachable rather than defensive: a parent that ended ages out of the
// daemon's recentEndings after 32 rotations while its fork keeps running, so a
// window opened after that holds the fork's ParentID and has never been told
// anything about the session it points at. Eight hex characters in the DM
// header are exactly what names exist to replace - `wake status` is the surface
// where a short id is the right answer, because that is where ids are printed.
func TestAForkWhoseParentThisClientCannotNameDrawsNoAncestry(t *testing.T) {
	fresh(t)
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s2", Name: "sydney", State: rpc.StateIdle, ParentID: "long-gone"},
	}}
	a := NewRoomApp(nil, Stream{}, &st).withSize(160, 30)
	a = a.openDMWith("s2", "sydney")

	if got := shown(a); strings.Contains(got, "forked from") {
		t.Errorf("the header drew ancestry for a parent it holds nothing about:\n%s", got)
	}
}

// The parent is re-read on every open, not only when the DM is created — and
// the window where that matters is reachable rather than defensive.
//
// Fan-out starts before the spawn's confirmation is enqueued, so a client can
// meet an agent through an *event* first: Fleet.Observe makes a row carrying an
// id and nothing else, with no ParentID on it. A conversation opened in that
// window has no ancestry to draw, and only re-reading on the next open recovers
// it.
func TestAConversationOpenedBeforeItsParentWasKnownPicksTheAncestryUpOnReopen(t *testing.T) {
	fresh(t)
	a := NewRoomApp(nil, Stream{}, nil).withSize(160, 30)

	// Met through an event, which is all Fleet.Observe records: an id.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "starting",
	}})
	a = a.openDMWith("s2", "sydney")
	if got := shown(a); strings.Contains(got, "forked from") {
		t.Fatalf("the header named a parent before any report had one:\n%s", got)
	}

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "sydney", State: rpc.StateIdle, ParentID: "s1"},
		},
	}})
	a = a.openDMWith("s2", "sydney")

	if got := shown(a); !strings.Contains(got, "@sydney  forked from @alex") {
		t.Errorf("the header still has no ancestry after the report that carried it:\n%s", got)
	}
}

// An agent that is nobody's fork gets a bare handle, exactly as before. §7
// routes on @name and a header with a clause glued to it is not a handle.
func TestAnOrdinaryConversationsHeaderIsUnchanged(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30)

	if got := shown(a); strings.Contains(got, "forked from") {
		t.Errorf("an ordinary conversation's header grew a fork clause:\n%s", got)
	}
}

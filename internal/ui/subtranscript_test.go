package ui

// A pane opened on a subagent this client never watched speak: the row is
// openable (task_started names a dispatch) but nothing was forwarded to this
// client, so the transcript is read off claude's own file instead.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const diskSaid = "SAID BEFORE THIS WINDOW EXISTED"

// lateAttached is alex with one running subagent whose task_started this
// client folded and nothing else - what replayRunningTasks leaves a client that
// attached after the subagent had begun.
func lateAttached(t *testing.T) App {
	t.Helper()
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(120, 40)
	a = a.applyFrame(taskFrame("s1", started("a1", "toolu_1", "Count lines in alpha.txt", "general-purpose", core.TaskAgent)))
	return a.pickAgent(1)
}

// watchedLive is the same subagent for a client attached since before it
// began: it holds everything the subagent said.
func watchedLive(t *testing.T, said ...string) App {
	t.Helper()
	a := lateAttached(t).pickAgent(-1)
	for _, text := range said {
		a = a.applyFrame(taskFrame("s1", spoke("toolu_1", text)))
	}
	return a.pickAgent(1)
}

// opened presses ⌃D on the subagent row and returns the frames Update's drain
// writes for it.
func opened(t *testing.T, a App) (App, []rpc.Frame) {
	t.Helper()
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	return a, writtenFrames(t, a, cmd)
}

func subagentAsks(frames []rpc.Frame) []rpc.Frame {
	var out []rpc.Frame
	for _, f := range frames {
		if f.Kind == rpc.FrameSubagent {
			out = append(out, f)
		}
	}
	return out
}

// diskEvents is what the daemon answers: claude's own lines, with no dispatch
// on them, since a sidechain line names none.
func diskEvents(texts ...string) []core.Event {
	out := make([]core.Event, len(texts))
	for i, text := range texts {
		out[i] = core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: text}
	}
	return out
}

// subagentReply answers the ask outstanding for an agent, echoing its number the
// way the daemon does.
func subagentReply(a App, agentID string, events []core.Event) App {
	return subagentReplyTo(a, agentID, a.subTranscripts.asked[subKey{"s1", agentID}].number, events)
}

func subagentReplyTo(a App, agentID string, ask uint64, events []core.Event) App {
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameSubagentReply, SessionID: "s1",
		Workflow: &rpc.WorkflowFrame{Agent: agentID, Ask: ask}, Events: events})
}

func paneOf(t *testing.T, a App) DM {
	t.Helper()
	d, ok := a.dms["s1"]
	if !ok {
		t.Fatal("no conversation for s1")
	}
	return *d
}

// The bug: the pane opened on a subagent this client never watched, and drew
// nothing. Opening it now asks the daemon for the subagent's own transcript,
// by the id the task_started carried.
func TestOpeningASubagentThisClientNeverWatchedAsksForItsTranscript(t *testing.T) {
	a, frames := opened(t, lateAttached(t))

	asks := subagentAsks(frames)
	if len(asks) != 1 {
		t.Fatalf("opening wrote %d FrameSubagent, want exactly 1: %+v", len(asks), frames)
	}
	if f := asks[0]; f.SessionID != "s1" || f.Workflow == nil || f.Workflow.Agent != "a1" {
		t.Errorf("asked for %+v (workflow %+v), want alex's subagent a1", f, f.Workflow)
	}
	if got := viewedBy(t, a, "s1"); got != "toolu_1" {
		t.Errorf("the pane is viewing %q, want the subagent that was opened", got)
	}
}

func TestTheTranscriptFromDiskFillsThePaneThatWasEmpty(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	d := paneOf(t, a)
	assertHides(t, d, d.width, d.height, diskSaid)

	a = subagentReply(a, "a1", diskEvents(diskSaid))
	d = paneOf(t, a)
	assertShows(t, d, d.width, d.height, diskSaid)
	if got := d.Viewed(); got != "toolu_1" {
		t.Errorf("the reply moved the pane to %q, want it left on the subagent", got)
	}
}

// What comes off disk draws as the subagent's own frames, as a live one does:
// attributed to its dispatch, with the type and task the row knows. A sidechain
// line carries none of that, so it is put on here.
func TestTheDiskTranscriptIsFiledAndAttributedAsTheSubagentsOwn(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	a = subagentReply(a, "a1", diskEvents(diskSaid, "and then this"))

	held := paneOf(t, a).subs["toolu_1"]
	if held.len() != 2 {
		t.Fatalf("the subagent holds %d events, want the 2 off disk", held.len())
	}
	for i, ev := range held.slice(0, held.len()) {
		s := ev.Subagent
		if s == nil || s.Dispatch != "toolu_1" || s.Type != "general-purpose" {
			t.Errorf("event %d is attributed %+v, want dispatch toolu_1 and type general-purpose", i, s)
		}
	}
}

// A frame that landed between the ask and its answer is in both the live copy
// and the disk read, or only in the live one - which of the two is unknowable
// here, so the read is dropped rather than drawn twice. Losing it is the safe
// side: an empty pane is what the operator had, a doubled line is a lie.
func TestAFrameArrivingSinceTheAskDropsTheTranscript(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	a = a.applyFrame(taskFrame("s1", spoke("toolu_1", "LIVE")))

	a = subagentReply(a, "a1", diskEvents(diskSaid, "LIVE"))
	d := paneOf(t, a)
	assertShows(t, d, d.width, d.height, "LIVE")
	assertHides(t, d, d.width, d.height, diskSaid)
}

// A client attached the whole time holds more than the disk's bounded tail
// once a subagent runs long, and must not be shrunk to it.
func TestAShorterTranscriptNeverReplacesALongerLiveOne(t *testing.T) {
	a, _ := opened(t, watchedLive(t, subSaid, "SECOND LIVE"))

	a = subagentReply(a, "a1", diskEvents(diskSaid))
	d := paneOf(t, a)
	assertShows(t, d, d.width, d.height, subSaid)
	assertShows(t, d, d.width, d.height, "SECOND LIVE")
	assertHides(t, d, d.width, d.height, diskSaid)
}

// An answer for an id nobody asked about does not so much as rebuild the pane:
// the DM is the same one, which a rebuild would replace.
func TestAnAnswerNobodyAskedForChangesNothing(t *testing.T) {
	a := lateAttached(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD}) // opened, but the drain below is never run
	before := a.dms["s1"]

	a = subagentReply(a, "nosuchagent", diskEvents(diskSaid))
	d := paneOf(t, a)
	assertHides(t, d, d.width, d.height, diskSaid)
	if a.dms["s1"] != before {
		t.Error("an answer nobody asked for rebuilt the pane")
	}
}

// Opening again never shrinks a pane that holds the disk transcript: the
// fleet's copy a re-open seeds from is only what this client saw live, and
// replacing the pane's with it would also make the next answer easier to drop.
func TestReopeningNeverShrinksAPaneThatHoldsTheDiskTranscript(t *testing.T) {
	a, _ := opened(t, watchedLive(t, "LIVE ONE"))
	a = subagentReply(a, "a1", diskEvents(diskSaid, "and then this", "and this"))
	if n := paneOf(t, a).subs["toolu_1"].len(); n != 3 {
		t.Fatalf("the answer left the pane holding %d events, want its 3", n)
	}

	a, _ = opened(t, a)
	d := paneOf(t, a)
	if n := d.subs["toolu_1"].len(); n != 3 {
		t.Errorf("a second open left the pane holding %d events, want the 3 it had", n)
	}
	assertShows(t, d, d.width, d.height, diskSaid)
}

// Each open asks again: the fleet's copy this client seeds from is only what it
// saw, so a stale answer from an earlier open must not stand in for this one.
func TestEveryOpenAsksAgain(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	a = subagentReply(a, "a1", diskEvents(diskSaid))

	_, frames := opened(t, a)
	if got := len(subagentAsks(frames)); got != 1 {
		t.Fatalf("a second open wrote %d FrameSubagent, want 1", got)
	}
}

// Two opens before either answer are two asks, each its own number, and only the
// newer one's answer lands - whichever order the two arrive in. An ask keyed on
// the agent alone let the older answer consume the newer ask and leave the pane
// on the older snapshot.
func TestOnlyTheNewestOpensAnswerLands(t *testing.T) {
	for name, newestFirst := range map[string]bool{"older answer first": false, "newer answer first": true} {
		t.Run(name, func(t *testing.T) {
			a, first := opened(t, lateAttached(t))
			a, second := opened(t, a)
			older, newer := subagentAsks(first), subagentAsks(second)
			if len(older) != 1 || len(newer) != 1 || older[0].Workflow.Ask == newer[0].Workflow.Ask {
				t.Fatalf("two opens wrote asks %+v then %+v, want one each under different numbers", older, newer)
			}
			oldReply := func(a App) App {
				return subagentReplyTo(a, "a1", older[0].Workflow.Ask, diskEvents("OLD SNAPSHOT"))
			}
			newReply := func(a App) App {
				return subagentReplyTo(a, "a1", newer[0].Workflow.Ask, diskEvents("NEW SNAPSHOT", "AND MORE"))
			}
			if newestFirst {
				a = oldReply(newReply(a))
			} else {
				a = newReply(oldReply(a))
			}
			d := paneOf(t, a)
			assertShows(t, d, d.width, d.height, "NEW SNAPSHOT")
			assertHides(t, d, d.width, d.height, "OLD SNAPSHOT")
		})
	}
}

// Only a subagent has a transcript of its own to read: the conversation itself
// is History's, and opening it asks this for nothing.
func TestOpeningTheConversationItselfAsksForNoSubagent(t *testing.T) {
	// A subagent no frame has named a dispatch for yet: the empty dispatch the
	// conversation's own row means must not be read as naming it.
	a := lateAttached(t).applyFrame(taskFrame("s1", started("a2", "", "no dispatch yet", "general-purpose", core.TaskAgent)))
	a = a.pickAgent(-1)
	if a.roster.SelectedTask != "" {
		t.Fatalf("the cursor is still on a subagent: %+v", a.roster)
	}
	_, frames := opened(t, a)
	if asks := subagentAsks(frames); len(asks) != 0 {
		t.Errorf("opening the conversation wrote %d FrameSubagent, want none", len(asks))
	}
}

// An id the daemon's fence would refuse is never asked for: the refusal would be
// a notice on every open, and the pane still opens on what it holds.
func TestASubagentWithAnIdTheDaemonWouldRefuseIsNotAskedFor(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(120, 40)
	a = a.applyFrame(taskFrame("s1", started("Not/An-Id", "toolu_1", "x", "general-purpose", core.TaskAgent))).pickAgent(1)

	a, frames := opened(t, a)
	if asks := subagentAsks(frames); len(asks) != 0 {
		t.Errorf("wrote %d FrameSubagent for an id the daemon refuses: %+v", len(asks), asks)
	}
	if got := viewedBy(t, a, "s1"); got != "toolu_1" {
		t.Errorf("the pane is viewing %q, want it opened on the subagent all the same", got)
	}
}

// An answer is good for one ask, so the ask is not kept past it - dropped or
// not - and a second answer to it is nobody's.
func TestAnAnswerIsGoodForOneAsk(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	a = subagentReply(a, "a1", diskEvents(diskSaid))
	if n := len(a.subTranscripts.asked); n != 0 {
		t.Errorf("%d asks are still outstanding after the answer", n)
	}
}

// A /clear blanks the pane and drops what a reply in flight would have landed
// on - the history ask's own rule - or the empty count the ask recorded would
// read as "nothing arrived since" and resurrect a pre-clear transcript.
func TestAClearDropsASubagentAskStillInFlight(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	a = a.applyFrame(taskFrame("s1", core.Event{Kind: core.KindSessionReset}))

	a = subagentReply(a, "a1", diskEvents(diskSaid))
	d := paneOf(t, a)
	if held := d.subs["toolu_1"].len(); held != 0 {
		t.Errorf("a pre-clear transcript of %d events landed on the cleared pane", held)
	}
}

// ⌃W keeps the transcript but drops the pane, so a reply for it is the board's
// or nobody's - never a reason to build a conversation.
func TestAnAnswerForAClosedConversationIsIgnored(t *testing.T) {
	a, _ := opened(t, lateAttached(t))
	a.dms = map[string]*DM{}

	a = subagentReply(a, "a1", diskEvents(diskSaid))
	if len(a.dms) != 0 {
		t.Errorf("a reply built a conversation: %v", a.dms)
	}
}

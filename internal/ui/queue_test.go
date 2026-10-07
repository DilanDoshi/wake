package ui

// A message typed to a working agent is written at once and reaches it at its
// next tool boundary, or as its next turn: claude queues it
// (docs/superpowers/notes/2026-10-02-mid-turn-delivery-findings.md). Until claude
// takes it up it is pinned above the composer; then its echo lands where the
// model read it. A /rename to a busy agent is still held here. See queue.go.

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// lifecycleFrame is a decoded command_lifecycle, the way oneAgent is a status.
// "started"/"completed"/"cancelled" are the CLI's own states
// (testdata/stream/midturn-*.jsonl).
func lifecycleFrame(id, msgID, state string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
		Kind: core.KindMessageState, SessionID: id, MessageID: msgID, Text: state,
	}}
}

// idleDM is a DM on s1, idle and ready to take a message immediately.
func idleDM(t *testing.T) App {
	t.Helper()
	return dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
}

// echoedInDM is whether a conversation draws text as a turn - not as a pin row,
// and not as the draft, which is blanked first.
func echoedInDM(a App, id, text string) bool {
	if d := a.dms[id]; d != nil {
		a = a.withComposerFor(id, d.Composer().WithDraft(""))
	}
	for _, l := range strings.Split(stripANSI(a.dmFor(id).View(200, 40)), "\n") {
		if strings.Contains(l, text) && !strings.Contains(l, queuedGlyph) {
			return true
		}
	}
	return false
}

// sendTo submits text in the DM and returns the frame it wrote.
func sendTo(t *testing.T, a App, text string) (App, rpc.Frame) {
	t.Helper()
	m, cmd := typeAndSubmit(a, text)
	a = m.(App)
	return a, sentFrame(t, a, cmd)
}

// A message to a working agent is written now, stamped, and pinned - not
// echoed, since claude has not read it yet.
func TestAMessageToAWorkingAgentIsWrittenAtOnceAndPinned(t *testing.T) {
	a, f := sendTo(t, busyDM(t), "run the tests")
	if f.Kind != rpc.FrameSend || f.Text != "run the tests" || f.MessageID == "" || f.Now {
		t.Errorf("wrote %+v, want a stamped ordinary send", f)
	}
	if q := a.queued["s1"]; len(q) != 1 || q[0].id != f.MessageID || q[0].held {
		t.Errorf("queued = %+v, want the written message pinned", q)
	}
	if echoedInDM(a, "s1", "run the tests") {
		t.Error("the message was drawn as a turn before claude took it up")
	}
}

// An idle agent's message is echoed at once and marks it in flight.
func TestAnIdleAgentsMessageIsEchoedAtOnce(t *testing.T) {
	a, f := sendTo(t, idleDM(t), "hi")
	if !a.inflight["s1"][f.MessageID] || len(a.queued["s1"]) != 0 || !echoedInDM(a, "s1", "hi") {
		t.Errorf("inflight=%v queued=%v: want the message echoed and in flight", a.inflight, a.queued)
	}
}

// A follow-up typed before the daemon reports the first turn working is written
// too, and pinned: the first is in flight, so claude has not read it yet.
func TestAFastFollowUpIsWrittenAndPinned(t *testing.T) {
	a, _ := sendTo(t, idleDM(t), "first")
	a, f := sendTo(t, a, "second")
	if q := a.queued["s1"]; len(q) != 1 || q[0].id != f.MessageID {
		t.Errorf("queued = %+v, want the follow-up pinned behind the first", q)
	}
}

// claude taking the message up - mid-turn or as its own turn - moves it from
// the pin into the conversation, at that point.
func TestAPinnedMessageIsDrawnWhenClaudeTakesItUp(t *testing.T) {
	a, f := sendTo(t, busyDM(t), "use the other fixture")
	a = a.applyFrame(lifecycleFrame("s1", f.MessageID, "started"))
	if len(a.queued["s1"]) != 0 || !echoedInDM(a, "s1", "use the other fixture") {
		t.Errorf("queued = %v: the started message was not moved into the conversation", a.queued["s1"])
	}
	if !a.inflight["s1"][f.MessageID] {
		t.Error("the message claude took up is not in flight")
	}
}

// Its started lifecycle lost to a gap, a message is still known read: by its
// completed lifecycle, or by the turn end that names it.
func TestAMessageIsKnownReadWithoutItsStart(t *testing.T) {
	for name, ev := range map[string]func(string) rpc.Frame{
		"completed": func(id string) rpc.Frame { return lifecycleFrame("s1", id, "completed") },
		"turn end": func(id string) rpc.Frame {
			return rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{Kind: core.KindTurnEnd, SessionID: "s1", Answered: []string{"other", id}}}
		},
	} {
		a, f := sendTo(t, busyDM(t), "read me")
		a = a.applyFrame(ev(f.MessageID))
		if len(a.queued["s1"]) != 0 || !echoedInDM(a, "s1", "read me") {
			t.Errorf("%s: queued = %v, want the message drawn as read", name, a.queued["s1"])
		}
	}
}

// A message queued for an agent that ends is dropped, not drawn later.
func TestQueueDropsWhenTheAgentEnds(t *testing.T) {
	a, _ := sendTo(t, busyDM(t), "later")
	a, _ = a.applyFrame(oneAgent("s1", "alex", rpc.StateEnded)).flushQueued()
	if _, held := a.queued["s1"]; held {
		t.Errorf("an ended agent's queue was not dropped")
	}
}

// A reattach forgets what it cannot confirm: in-flight marks, and messages
// claude may have taken up while this client was gone. A held /rename was never
// written, so it survives to go out on the next idle.
func TestReattachForgetsWhatItCannotConfirm(t *testing.T) {
	a, _ := sendTo(t, idleDM(t), "first")
	a, _ = sendTo(t, a, "second")
	m, _ := typeAndSubmit(a, "/rename bob")
	a = m.(App)

	next, _ := a.reattached(reattachedMsg{})
	a = next.(App)
	if len(a.inflight) != 0 {
		t.Errorf("reattach left in-flight marks nothing can now clear: %v", a.inflight)
	}
	if q := a.queued["s1"]; len(q) != 1 || !q[0].held {
		t.Errorf("queued after reattach = %+v, want only the held /rename", q)
	}
}

// In the room a broadcast is written to every target at once; an idle one draws
// it now, a working one pins it fromRoom; the room's own line is drawn once.
func TestARoomBroadcastIsWrittenToEveryTargetAndPinnedForBusyOnes(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateIdle},
			{ID: "s2", Name: "alex", State: rpc.StateWorking},
			{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
		},
	}})

	m, cmd := typeAndSubmit(a, "@all ship it")
	a = m.(App)
	frames := sentFrames(t, a, cmd)
	if len(frames) != 2 { // @all is the fleet, the manager aside
		t.Fatalf("wrote %d frames, want one per target: %+v", len(frames), frames)
	}
	q := a.queued["s2"]
	if len(q) != 1 || q[0].echo != "@all ship it" || !q[0].fromRoom || q[0].id == "" {
		t.Errorf("the working target's broadcast was not pinned fromRoom with a uuid: %v", q)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("the idle target's message was pinned instead of drawn")
	}
}

// A message routed from the room keeps its addressing @name in the stored echo,
// because the transcript draws it under a "from the room" head that explains it.
// The queued pin has no such head, so in the target's own DM a bare @name reads
// as a stray self-mention - the pin strips it.
func TestAQueuedRoomBroadcastDropsItsMentionInThePin(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s2", Name: "scroll-bug", State: rpc.StateWorking},
			{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
		},
	}})
	m, _ := typeAndSubmit(a, "@scroll-bug here is an example")
	a = m.(App)

	if got := a.queuedTexts("s2"); len(got) != 1 || got[0] != "here is an example" {
		t.Errorf("the queued room broadcast's pin kept its @name self-mention, want [%q]: %#v", "here is an example", got)
	}
	// The stored echo still carries the mention: the transcript's "from the room"
	// head is what explains it there, so the strip is the pin's alone.
	if q := a.queued["s2"]; len(q) != 1 || q[0].echo != "@scroll-bug here is an example" {
		t.Errorf("the stored echo lost its mention, which the transcript head needs: %#v", q)
	}
}

// A DM-typed message that opens with an @word is prose to the agent, not a
// routing address, so its pin keeps it verbatim - the strip is fromRoom-only.
func TestAQueuedDMMessageKeepsALeadingAtWordInThePin(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	m, _ := typeAndSubmit(a, "@decorator is broken")
	a = m.(App)
	if got := a.queuedTexts("s1"); len(got) != 1 || got[0] != "@decorator is broken" {
		t.Errorf("a DM-typed leading @word was stripped from the pin: %#v", got)
	}
}

// Stripping the room mention keeps the rest, image chip and all, so the pin
// still says an image is attached.
func TestAQueuedRoomBroadcastKeepsItsImageChipInThePin(t *testing.T) {
	a := idleDM(t).enqueue("s1", newQueued(uuid.NewString(), "ship it", "@alex ship it [Image #1]", nil, true))
	if got := a.queuedTexts("s1"); len(got) != 1 || got[0] != "ship it [Image #1]" {
		t.Errorf("the pin dropped the image chip along with the mention: %#v", got)
	}
}

// A bystander to an open-mode `@john hello` broadcast: john's mention is the
// message's context, not this agent's own address, so the pin keeps it whole.
// Only the recipient's *own* @name is a redundant self-mention to strip.
func TestAQueuedRoomBroadcastKeepsADifferentAgentsMentionInThePin(t *testing.T) {
	a := idleDM(t).enqueue("s1", newQueued(uuid.NewString(), "hello", "@john hello", nil, true))
	if got := a.queuedTexts("s1"); len(got) != 1 || got[0] != "@john hello" {
		t.Errorf("a bystander pin lost another agent's mention: %#v", got)
	}
}

// A leading @word that only prefixes this agent's name (or is a path, `@a/b`) is
// not its routing address - the pin keeps it whole rather than clipping to the
// name, the whole-word failure a display regex like leadingMention would hit.
func TestAQueuedRoomMessageKeepsALeadingAtWordThatIsNotThisAgent(t *testing.T) {
	a := idleDM(t).enqueue("s1", newQueued(uuid.NewString(), "look", "@alexander take a look", nil, true))
	if got := a.queuedTexts("s1"); len(got) != 1 || got[0] != "@alexander take a look" {
		t.Errorf("the pin clipped a longer @word down to this agent's name: %#v", got)
	}
}

// A room message that is only the addressee's @name (no body) keeps the name,
// rather than stripping to an empty pin row with a bare glyph.
func TestABareQueuedRoomMentionKeepsItsNameInThePin(t *testing.T) {
	a := idleDM(t).enqueue("s1", newQueued(uuid.NewString(), "", "@alex", nil, true))
	if got := a.queuedTexts("s1"); len(got) != 1 || got[0] != "@alex" {
		t.Errorf("a bare room mention stripped to an empty pin: %#v", got)
	}
}

// The waiting messages are visible above the composer so type-ahead is not silent.
func TestAQueuedMessageShowsInThePin(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	m, _ := typeAndSubmit(a, "fix the bug")
	a = m.(App)

	out := shown(a)
	if !strings.Contains(out, "fix the bug") {
		t.Errorf("the queued message is not shown above the composer:\n%s", out)
	}
	if !strings.Contains(out, queuedGlyph) {
		t.Errorf("the queued pin is not marked with %q:\n%s", queuedGlyph, out)
	}
}

// A deep queue does not take the transcript's rows without limit: the pin is
// bounded and counts the rest, so the pane can never grow past the terminal.
func TestTheQueuedPinIsBounded(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	for range maxQueuedPinRows + 3 {
		m, _ := typeAndSubmit(a, "message")
		a = m.(App)
	}

	d := a.dmFor("s1")
	if d.queuedRows() != maxQueuedPinRows {
		t.Errorf("a deep queue draws %d rows, want it capped at %d", d.queuedRows(), maxQueuedPinRows)
	}
	if got := strings.Count(d.queuedPin(200), "\n") + 1; got != maxQueuedPinRows {
		t.Errorf("the pin drew %d rows, want the cap %d", got, maxQueuedPinRows)
	}
	if !strings.Contains(shown(a), "more queued") {
		t.Errorf("a queue past the cap does not count the rest:\n%s", shown(a))
	}
}

// The pane never draws taller than its allocation with the pin stacked above a
// growing draft - the composer must reserve the pinned rows from its own growth,
// not just from the transcript's floor. The alt-screen overflow the reviews
// caught: measured on the DM's own View, before any frame-level clip.
func TestThePaneStaysInBoundsWithPinAndDraft(t *testing.T) {
	const w, h = 80, 14
	a := idleDM(t).withSize(w, h).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	for range maxQueuedPinRows {
		m, _ := typeAndSubmit(a, "a queued follow-up")
		a = m.(App)
	}
	a = a.withDraft(strings.Repeat("line\n", 20)) // a draft that wants far more rows than fit

	got := strings.Count(a.dmFor("s1").View(w, h), "\n") + 1
	if got > h {
		t.Errorf("the pane drew %d rows into a %d-row terminal: the pin and draft overflow", got, h)
	}
}

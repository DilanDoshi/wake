// A client that attaches after an ask is already outstanding - a reattach, a
// second window, or one that missed the live event to ring eviction - used to
// learn only rpc.SessionStatus.RequestIDs: an id and a tool name, nothing
// about what the ask actually wants. internal/ui.Cards.Reconcile could build
// only a bare permission stand-in from that, and its Allow is a bare
// FrameAllow - silently wrong for a question (see internal/ui/cardreplay_test.go).
//
// replayPendingAsks closes it: a newly accepted connection is handed every
// outstanding ask as the ordinary rpc.FrameEvent a live client would have
// gotten, before it is even subscribed to broadcast.

package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// TestPendingAskFramesCarryTheFullEventNotJustTheID is the unit-level half:
// what an agent hands back for replay is the same core.Event it decoded off
// the wire, addressed the way fanOut addresses a live one - by the agent's own
// id, not whatever session id rode the event (they differ after a /clear).
func TestPendingAskFramesCarryTheFullEventNotJustTheID(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	if got := a.pendingAskFrames(); got != nil {
		t.Fatalf("a freshly built agent already has %d ask frames to replay", len(got))
	}

	asked := core.Event{
		Kind: core.KindPermissionRequest, RequestID: "r1", Ask: core.AskChoice,
		Tool: &core.ToolCall{ID: "t1", Name: "AskUserQuestion", Ask: &core.AskDetail{
			Questions: []core.Question{{Text: "Which format?", Options: []core.Option{{Label: "CSV"}, {Label: "JSON"}}}},
		}},
	}
	a.observe(asked)

	frames := a.pendingAskFrames()
	if len(frames) != 1 {
		t.Fatalf("pendingAskFrames() returned %d frames, want 1", len(frames))
	}
	f := frames[0]
	if f.Kind != rpc.FrameEvent || f.SessionID != idAlpha {
		t.Fatalf("frame = %+v, want a FrameEvent addressed to %q", f, idAlpha)
	}
	if f.Event == nil || f.Event.Ask != core.AskChoice || f.Event.Tool == nil || f.Event.Tool.Ask == nil {
		t.Fatalf("event = %+v, want the full ask this agent observed - Ask kind and question payload intact", f.Event)
	}
	if len(f.Event.Tool.Ask.Questions) != 1 || f.Event.Tool.Ask.Questions[0].Text != "Which format?" {
		t.Fatalf("the replayed event lost its question payload: %+v", f.Event.Tool.Ask)
	}

	// Settled asks are not replayed - the same rule RequestIDs already
	// follows, so a client attaching after the answer sees nothing to answer.
	a.noteAnswered("r1")
	if got := a.pendingAskFrames(); got != nil {
		t.Fatalf("an answered ask is still offered for replay: %d frames", len(got))
	}
}

// TestASecondClientAttachingAfterAnAskCanStillAnswerIt is the end-to-end half:
// a real daemon, a real fake process replaying a recorded AskUserQuestion, and
// a second client that dials in only after the ask is already outstanding -
// the shape a reattach or a second window takes. Before replayPendingAsks
// existed, this client's only route to the ask was RequestIDs, and answering
// through that report-only card would have sent a bare allow: the fixture's
// own "did not answer" tail, byte-identical to a real operator's choice being
// thrown away. See question_test.go for the single-client version this mirrors.
func TestASecondClientAttachingAfterAnAskCanStillAnswerIt(t *testing.T) {
	replayingClaudeOnPath(t, "question")
	d := startDaemon(t)

	c1 := attach(t, d.socket)
	c1.spawn(idAlpha, "sydney")
	ask := c1.awaitAsk(idAlpha, core.AskChoice)
	if blocked := c1.pollState(idAlpha, rpc.StateBlocked); soleAsk(t, blocked) != ask.RequestID {
		t.Fatalf("the roster says the session is blocked on %q, the ask says %q", soleAsk(t, blocked), ask.RequestID)
	}

	// Attaches after the ask is already outstanding - nothing about it has
	// been read live by this connection.
	c2 := attach(t, d.socket)
	replayed := c2.awaitAsk(idAlpha, core.AskChoice)
	if replayed.RequestID != ask.RequestID {
		t.Fatalf("the second client's ask names request %q, want the one already outstanding, %q", replayed.RequestID, ask.RequestID)
	}
	if replayed.Tool == nil || replayed.Tool.Ask == nil || len(replayed.Tool.Ask.Questions) == 0 {
		t.Fatalf("the replayed ask carries no question payload, so a client could only build a bare permission card: %+v", replayed)
	}

	asked, answers := recordedChoices(t)
	c2.send(rpc.Frame{
		Kind:         rpc.FrameAnswer,
		SessionID:    idAlpha,
		RequestID:    replayed.RequestID,
		UpdatedInput: asked,
		Answers:      answers,
	})

	told := c2.awaitAnswerTo(idAlpha, replayed.Tool.ID)
	for _, choice := range answers {
		if !strings.Contains(told, choice) {
			t.Errorf("the model was told %q, which does not name the chosen option %q", told, choice)
		}
	}
	if strings.Contains(told, "did not answer") {
		t.Fatalf("the model was told nobody answered: %q - the second client's answer was lost", told)
	}
}

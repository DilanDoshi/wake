package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// clearTo is a /clear as the daemon sees it: a reset naming the id that died,
// then a frame under the successor claude minted.
func clearTo(a *agent, died, successor string) {
	a.observe(core.Event{Kind: core.KindSessionReset, SessionID: died})
	a.observe(core.Event{Kind: core.KindSystem, SessionID: successor})
}

// A cleared agent parks under the conversation claude is writing now, so a wake
// resumes that one rather than the conversation the /clear left behind - and the
// parked row stays wakeable, since its book record is the record it wrote.
func TestAClearedAgentParksUnderTheConversationItIsWriting(t *testing.T) {
	s := newServer(tempSocket(t))
	a := parkedRow(t, s, idAlpha, "alex")
	clearTo(a, idAlpha, idBeta)

	rec := recordFor(a)
	if rec.ID != idBeta {
		t.Fatalf("a cleared agent parked under %s, want its current conversation %s: a wake would resume the conversation /clear left", rec.ID, idBeta)
	}
	a.markParked()
	a.markWakeable(rec, true)
	a.mu.Lock()
	wakeable := a.wakeable
	a.mu.Unlock()
	if !wakeable {
		t.Error("the row is not wakeable after its park book write, so /resume refuses a session that parked cleanly")
	}
}

// The reset names the id that died and only forgets; a late frame still carrying
// that id - a background subagent forwarding under the old conversation - must
// not bring it back, and the next new id is learned as always.
func TestAResetIsNotUndoneByALateFrameFromTheConversationThatDied(t *testing.T) {
	a := &agent{id: idAlpha, name: "alex"}
	clearTo(a, idAlpha, idBeta)
	a.observe(core.Event{Kind: core.KindSessionReset, SessionID: idBeta})
	a.observe(core.Event{Kind: core.KindAssistantText, SessionID: idBeta, Text: "late"})
	if got := a.conversation(); got == idBeta {
		t.Errorf("a frame from the conversation a /clear ended made it current again")
	}
	a.observe(core.Event{Kind: core.KindSystem, SessionID: idGamma})
	if got := a.conversation(); got != idGamma {
		t.Errorf("after the second /clear the conversation is %s, want the successor %s", got, idGamma)
	}
}

// A session id off the child's stdout reaches an argv and the park book, so one
// Wake could not have minted is never taken as the conversation.
func TestAConversationIdWakeCouldNotHaveMintedIsIgnored(t *testing.T) {
	a := &agent{id: idAlpha, name: "alex"}
	a.observe(core.Event{Kind: core.KindSystem, SessionID: "--resume"})
	if got := a.conversation(); got != idAlpha {
		t.Errorf("the conversation is %q, want the agent's own id: an id from stdout reaches a command line", got)
	}
}

// A wake of a cleared ⌃C row takes the row under its new id and drops the old
// one, and a wake that fails to start puts the old row back as it was.
func TestAWakeReKeysTheRowItReplacesAndAFailedOneRestoresIt(t *testing.T) {
	s := newServer(tempSocket(t))
	was := parkedRow(t, s, idAlpha, "alex")
	clearTo(was, idAlpha, idBeta)
	woken := newAgent(idBeta, "alex", "dev", was.dir, "", core.NewSession(core.Config{SessionID: idBeta}), func() {})

	if !s.replaceParked(woken, was) {
		t.Fatal("a wake could not take the row it read once the conversation moved on")
	}
	if _, still := s.agents[idAlpha]; still {
		t.Errorf("the old row %s is still in the fleet beside its successor", idAlpha)
	}
	if s.agents[idBeta] != woken {
		t.Errorf("the successor is not filed under %s", idBeta)
	}

	s.withdraw(woken, was, nil)
	if s.agents[idAlpha] != was {
		t.Errorf("a failed wake did not put the old row back under %s", idAlpha)
	}
	if _, still := s.agents[idBeta]; still {
		t.Errorf("a failed wake left %s in the fleet", idBeta)
	}
}

// A wake may not re-key onto an id another row already holds.
func TestAWakeDoesNotReKeyOntoAnIdAnotherRowHolds(t *testing.T) {
	s := newServer(tempSocket(t))
	was := parkedRow(t, s, idAlpha, "alex")
	clearTo(was, idAlpha, idBeta)
	other := parkedRow(t, s, idBeta, "sydney")
	woken := newAgent(idBeta, "alex", "dev", was.dir, "", core.NewSession(core.Config{SessionID: idBeta}), func() {})

	if s.replaceParked(woken, was) {
		t.Fatalf("a wake re-keyed onto %s, which another row holds", idBeta)
	}
	if s.agents[idAlpha] != was || s.agents[idBeta] != other {
		t.Error("the refused re-key moved a row")
	}
}

// A row is found by its own id or by the conversation it is writing, which is
// what a park book record names.
func TestARowIsFoundByTheConversationItIsWriting(t *testing.T) {
	s := newServer(tempSocket(t))
	a := parkedRow(t, s, idAlpha, "alex")
	clearTo(a, idAlpha, idBeta)
	for _, id := range []string{idAlpha, idBeta} {
		if got, ok := s.conversationRow(id); !ok || got != a {
			t.Errorf("conversationRow(%s) did not find the cleared row", id)
		}
	}
	if _, ok := s.conversationRow(idGamma); ok {
		t.Errorf("conversationRow found a row for an id nothing is writing")
	}
}

// A ⌃C row is reported once. Its park book record is the same session, so
// listing both sent /resume all a second wake - and after a re-key, two wakes
// racing for one conversation.
func TestAParkedRowIsNotAlsoReportedFromTheParkBook(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cleared bool
	}{{name: "never cleared"}, {name: "cleared", cleared: true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(tempSocket(t))
			a := parkedRow(t, s, idAlpha, "alex")
			if tc.cleared {
				clearTo(a, idAlpha, idBeta)
			}
			rec := recordFor(a)
			plantTranscript(t, rec.ID, `{"type":"user","message":{"role":"user","content":"hi"}}`)
			if err := s.parked.add(rec); err != nil {
				t.Fatalf("book the park: %v", err)
			}
			if got := s.fleet().Parked; len(got) != 0 {
				t.Errorf("the park book listed %d rows for a session already in the fleet: %+v", len(got), got)
			}
		})
	}
}

// Every conversation a /clear ended stays ended, not only the last: after
// A -> B -> C a late frame still carrying A must not make it current.
func TestALateFrameFromAnyEndedConversationIsIgnored(t *testing.T) {
	a := &agent{id: idAlpha, name: "alex"}
	clearTo(a, idAlpha, idBeta)
	clearTo(a, idBeta, idGamma)
	a.observe(core.Event{Kind: core.KindAssistantText, SessionID: idAlpha, Text: "late, from before the first clear"})
	if got := a.conversation(); got != idGamma {
		t.Errorf("a late frame from the first conversation made %s current, want %s", got, idGamma)
	}
}

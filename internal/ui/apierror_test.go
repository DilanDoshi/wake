package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A failed turn is the API talking, not the model. These tests hold the two
// properties that carry the surfacing half: it pops a notice that names the
// recovery and marks the session for /reauth, and it never lands in the
// transcript as if the agent said "Not logged in · Please run /login".

// apiErrorFrame is one KindAPIError, as the airlock would hand it up from a
// synthetic assistant frame.
func apiErrorFrame(sessionID, text string) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event:     &core.Event{Kind: core.KindAPIError, SessionID: sessionID, Text: text, Notice: core.NoticeAPIError},
	}
}

func TestAnAPIErrorPopsANoticeAndMarksTheSessionForReauth(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")

	m, _ := a.Update(frameMsg{Frame: apiErrorFrame("s1", "Not logged in · Please run /login")})
	app := m.(App)

	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.Text, "reauth") {
		t.Fatalf("no /reauth notice: Latest = %q, %v", n.Text, ok)
	}
	if _, marked := app.authFailed["s1"]; !marked {
		t.Errorf("session s1 was not marked authFailed, so /reauth cannot find it")
	}
}

// The error text must not render under the agent's name - that is the whole bug.
func TestAnAPIErrorNeverDrawsInTheTranscript(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")

	m, _ := a.Update(frameMsg{Frame: apiErrorFrame("s1", "Not logged in · Please run /login")})
	app := m.(App)

	if dm := app.dms["s1"]; dm != nil && strings.Contains(visible(*dm, 80, 20), "Not logged in") {
		t.Error("the API error rendered in the conversation transcript as agent speech")
	}
}

// A mark that outlived its failure would make a later /reauth re-park a session
// that has already recovered. A real model turn is what clears it; a tool call or
// any other event does not (the failed turn produces neither).
func TestAHealthyTurnClearsTheAuthFailedMarkButOtherEventsDoNot(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1").markAuthFailed("s1")

	if _, held := a.clearedAuthFailedOn("s1", core.Event{Kind: core.KindToolUse}).authFailed["s1"]; !held {
		t.Error("a tool call cleared the mark; only a model turn proves the login works")
	}
	if _, held := a.clearedAuthFailedOn("s1", core.Event{Kind: core.KindAssistantText, Text: "hi"}).authFailed["s1"]; held {
		t.Error("a healthy assistant turn did not clear the mark; /reauth would re-park a recovered session")
	}
}

// The clear is wired through observe, so a real turn arriving on the stream drops
// the mark end to end - not only when clearedAuthFailedOn is called directly.
func TestAHealthyTurnOnTheStreamClearsTheMark(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1").markAuthFailed("s1")

	m, _ := a.Update(frameMsg{Frame: rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: "s1",
		Event:     &core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "fixed it"},
	}})

	if _, held := m.(App).authFailed["s1"]; held {
		t.Error("a healthy turn on the stream left the auth-failed mark standing")
	}
}

// The mark is a set: clearing one leaves the rest, so a fleet-wide expiry that
// marked several does not lose the others when one is restarted.
func TestClearingOneAuthFailedMarkLeavesTheOthers(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")
	a = a.markAuthFailed("s1").markAuthFailed("s2")

	a = a.clearAuthFailed("s1")

	if _, held := a.authFailed["s1"]; held {
		t.Error("s1 was not cleared")
	}
	if _, held := a.authFailed["s2"]; !held {
		t.Error("clearing s1 dropped s2")
	}
}

package ui

// A turn the API never answered - the connection lost, the machine asleep -
// stops the agent until the daemon resumes it (internal/daemon/unreachable.go).
// These hold that it is told and pinned as a standing condition Wake lifts, never
// sent to /reauth or parked, and that the resume says why it came.

import (
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// unreachableFrame is one KindAPIError the airlock decoded as an unreachable API.
func unreachableFrame(sessionID string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &core.Event{
		Kind: core.KindAPIError, SessionID: sessionID, Notice: core.NoticeAPIUnreachable,
		Text: "API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)",
	}}
}

// reconnectFrame is the daemon's announcement that it resumed a session an
// unreachable API stopped, under the uuid it stamped.
func reconnectFrame(sessionID string) rpc.Frame {
	ev := resumeEvent(sessionID, time.Time{})
	ev.MessageID = rpc.NewMessageSend(rpc.MessageResumed | rpc.MessageReconnected).ID()
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &ev}
}

// However often it repeats, an unreachable API neither marks for /reauth nor
// parks - a new process cannot reach the API either - and its notice and pin say
// Wake resumes the agent.
func TestAnUnreachableAPIIsPinnedAndNeitherMarksNorParks(t *testing.T) {
	a := twoAgents(t)
	for range authRetryParkAttempt + 1 {
		a, _ = a.apply(unreachableFrame("s1")).settle()
	}
	if _, marked := a.authFailed["s1"]; marked {
		t.Error("an unreachable API marked the session for /reauth")
	}
	if _, parking := a.parking["s1"]; parking {
		t.Error("an unreachable API auto-parked the session")
	}
	n, _ := notice.Latest()
	for what, text := range map[string]string{"notice": n.Text, "pin": a.pinnedNotice()} {
		if !strings.Contains(text, "ENOTFOUND") || !strings.Contains(text, "Wake resumes it when the connection is back") || strings.Contains(text, reauthVerb) {
			t.Errorf("the %s should carry the API's message and say Wake resumes it: %q", what, text)
		}
	}
}

// The turn that goes through takes the pin and its notice; a park and a resume,
// which reach the API no better, leave it.
func TestAnUnreachablePinGoesOnlyWithATurnThatGoesThrough(t *testing.T) {
	a := twoAgents(t).apply(unreachableFrame("s1"))
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	a = reportStates(a, map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
	if pin := a.pinnedNotice(); !strings.Contains(pin, "ENOTFOUND") {
		t.Fatalf("a park and resume unpinned an unreachable API: %q", pin)
	}
	a = a.apply(healthyTurn("s1"))
	if pin := a.pinnedNotice(); pin != "" {
		t.Errorf("the turn that went through left the pin: %q", pin)
	}
	if n, ok := notice.Latest(); ok && strings.Contains(n.Text, "ENOTFOUND") {
		t.Errorf("the turn that went through left the failure's notice up: %q", n.Text)
	}
}

// An API that never answered proves nothing about the login, so an earlier 401's
// mark stays for /reauth.
func TestAnUnreachableAPILeavesALoginMark(t *testing.T) {
	a := twoAgents(t)
	a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	a, _ = a.apply(unreachableFrame("s1")).settle()
	if _, marked := a.authFailed["s1"]; !marked {
		t.Error("an unreachable API cleared a 401's mark, as if the login had answered")
	}
}

// The resume says why it came, in the conversation and the room; a usage
// limit's still says its own.
func TestAReconnectResumeSaysTheConnectionCameBack(t *testing.T) {
	view := shown(twoAgents(t).apply(reconnectFrame("s1")))
	if !strings.Contains(view, autoReconnectedLabel) || strings.Contains(view, autoResumedLabel) {
		t.Errorf("the conversation should draw the reconnect line alone:\n%s", view)
	}
	view = shown(roomWithHistoryAsked(t).apply(reconnectFrame("s1")))
	if !strings.Contains(view, "sydney"+autoReconnectedTail) || strings.Contains(view, autoResumedTail) {
		t.Errorf("the room should draw sydney's reconnect line alone:\n%s", view)
	}
	if view := shown(twoAgents(t).apply(resumeFrame("s1"))); !strings.Contains(view, autoResumedLabel) {
		t.Errorf("a usage limit's resume lost its own line:\n%s", view)
	}
}

// Restored from disk, a reconnect resume in the room still says why it came.
func TestARestoredReconnectResumeSaysTheConnectionCameBack(t *testing.T) {
	ev := resumeEvent("s1", clock().Add(-time.Hour))
	ev.MessageID = rpc.NewMessageSend(rpc.MessageResumed | rpc.MessageReconnected).ID()
	view := shown(roomWithHistoryAsked(t).apply(oneReply("s1", ev)))
	if !strings.Contains(view, "sydney"+autoReconnectedTail) || strings.Contains(view, "continue") {
		t.Errorf("the restored room should draw sydney's reconnect line, not the words:\n%s", view)
	}
}

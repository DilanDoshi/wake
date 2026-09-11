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

// A fleet-wide OAuth 401 makes Claude Code retry a dead token for ~5 min before
// giving up - the "loads for five minutes and returns nothing" hang. The retries
// arrive as KindAPIError from attempt 1, so Wake surfaces the failure within a
// second (marks the session) and, once it is clearly a dead login rather than a
// blip, auto-parks it to end the hang. The park is derived after the fold the way
// the rate-limit clear is: observe returns only App, and a park is a write. See
// docs/notes/bugs.md.
func TestARetryStormSurfacesEarlyAndAutoParksAtTheThreshold(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")
	// A retrying session is a live fleet row; the park's candidate set is
	// authFailedLive, which excludes one the fleet has no row for.
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}})
	retry := func(app App) App {
		m, _ := app.Update(frameMsg{Frame: apiErrorFrame("s1", "Failed to authenticate. API Error: 401")})
		return m.(App)
	}

	// First 401: surfaced at once, never parked - a single 401 can still recover.
	app := retry(a)
	if _, marked := app.authFailed["s1"]; !marked {
		t.Fatal("first api_retry did not mark s1 authFailed, so nothing surfaced early")
	}
	if _, parking := app.parking["s1"]; parking {
		t.Fatal("s1 was parked on the first 401 - one that might have recovered")
	}

	// Below the threshold: still only surfaced.
	for app.authFailRetries["s1"] < authRetryParkAttempt-1 {
		app = retry(app)
		if _, parking := app.parking["s1"]; parking {
			t.Fatalf("s1 parked at attempt %d, before the threshold %d", app.authFailRetries["s1"], authRetryParkAttempt)
		}
	}

	// At the threshold: auto-parked, so the 5-min hang ends.
	app = retry(app)
	if _, parking := app.parking["s1"]; !parking {
		t.Fatalf("s1 was not auto-parked at attempt %d - the hang stands", authRetryParkAttempt)
	}
}

// A session that recovers on a later retry must not carry its count into the next
// storm, or a single stale 401 much later would park it one retry in.
func TestRecoveryResetsTheRetryCount(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1").bumpAuthRetries("s1").bumpAuthRetries("s1").markAuthFailed("s1")
	a = a.clearAuthFailed("s1")
	if n := a.authFailRetries["s1"]; n != 0 {
		t.Fatalf("retry count survived recovery: got %d, want 0", n)
	}
}

// Parking closes stdin, so a blocked agent's ask would die as a deny nobody made
// and survive the wake - autoParkStalled refuses a blocked agent past the
// threshold, the way parkTarget and reauth refuse one.
func TestABlockedAuthFailedSessionIsNotAutoParked(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateBlocked}}})
	a = a.markAuthFailed("s1").bumpAuthRetries("s1").bumpAuthRetries("s1").bumpAuthRetries("s1")
	next, _ := a.autoParkStalled()
	if _, parking := next.parking["s1"]; parking {
		t.Error("a blocked agent was auto-parked; its ask dies as a deny nobody made")
	}
}

// A session the fleet has no row for - never registered, or dropped on an
// ending - must not be parked: the write reaches an id nothing owns and the mark
// never clears. authFailedLive excludes it, as reauth's own candidate set does.
func TestAnUntrackedAuthFailedSessionIsNotAutoParked(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")
	a = a.markAuthFailed("ghost").bumpAuthRetries("ghost").bumpAuthRetries("ghost").bumpAuthRetries("ghost")
	next, _ := a.autoParkStalled()
	if _, parking := next.parking["ghost"]; parking {
		t.Error("a session the fleet has no row for was auto-parked")
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

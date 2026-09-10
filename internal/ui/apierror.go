package ui

// A turn that failed on the API - an expired login, a rejected key, an overload
// - surfaces as a fleet-level pop-up and a remembered "needs re-auth" mark, not
// as agent speech in the transcript. observe routes core.KindAPIError here
// before the fold, the way it routes a rate-limit event to ratelimit.go, so the
// synthetic error frame never reaches the room, a DM, or a roster turn line.
//
// The mark is what /reauth reads to restart the affected sessions in place (see
// reauth.go). A shared OAuth token expires for the whole fleet at once, so
// several sessions land here together and the operator recovers them without
// killing the fleet - which, before this, was the only way out. Root cause and
// the upstream bug: docs/notes/bugs.md.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// apiErrored marks the failed session and pops a notice naming the recovery. It
// is the whole of what a KindAPIError does now: like rateLimited, the event
// never reaches a transcript or the fleet, because it is infrastructure failing
// rather than the model speaking.
func (a App) apiErrored(sessionID string, ev core.Event) App {
	if ev.Notice != core.NoticeAPIError {
		return a
	}
	a = a.markAuthFailed(sessionID).bumpAuthRetries(sessionID)
	who := sessionID
	if agent, ok := a.fleet.Agent(sessionID); ok && agent.Name != "" {
		who = agentPrefix + agent.Name
	}
	msg := apiErrorFallback
	if ev.Text != "" {
		msg = ev.Text
	}
	notice.Report("%s: %s — /reauth to bring it back", who, msg)
	return a
}

// apiErrorFallback stands in when the frame carried no message, so the notice
// still says which agent needs attention rather than nothing.
const apiErrorFallback = "the API rejected a turn"

// markAuthFailed adds one session to the copy-on-write set /reauth reads.
// awaitingQuit's shape, for its reason: the map is shared by value, so a fold
// that changes it writes a fresh one.
func (a App) markAuthFailed(id string) App {
	next := make(map[string]struct{}, len(a.authFailed)+1)
	for held := range a.authFailed {
		next[held] = struct{}{}
	}
	next[id] = struct{}{}
	a.authFailed = next
	return a
}

// authRetryParkAttempt is how many 401s Wake rides out before it auto-parks the
// session. One or two can still recover (the token refreshes on a later attempt),
// so Wake surfaces from the first but does not kill the process until the login is
// clearly dead - which is what ends Claude Code's ~5-min retry hang. See bugs.md.
const authRetryParkAttempt = 3

// bumpAuthRetries counts one 401 for a session, copy-on-write like markAuthFailed.
func (a App) bumpAuthRetries(id string) App {
	next := make(map[string]int, len(a.authFailRetries)+1)
	for held, n := range a.authFailRetries {
		next[held] = n
	}
	next[id]++
	a.authFailRetries = next
	return a
}

// autoParkStalled parks every auth-failed session that has 401'd enough times to
// be a dead login rather than a blip. It is derived after a frame fold the way
// armRateLimitClear is: observe returns only App and a park is a write, so the
// command cannot be issued from the fold. The parking guard makes it fire once
// per session; the count and mark clear on recovery, a wake, or /reauth.
//
// Its candidate set is authFailedLive (reauth.go), so a session the fleet has no
// row for - never registered, or dropped on an ending - and a parked or ended
// one are excluded there: a park of an id nothing owns pollutes state nothing
// ever clears. And a blocked agent is refused here as parkTarget and reauth
// refuse one - a park closes stdin, and a permission ask that dies that way reads
// as a deny nobody made and survives the wake.
func (a App) autoParkStalled() (App, tea.Cmd) {
	var frames []rpc.Frame
	for _, id := range a.authFailedLive() {
		if a.authFailRetries[id] < authRetryParkAttempt {
			continue
		}
		if _, parking := a.parking[id]; parking {
			continue
		}
		if a.blockedAgent(id) {
			continue
		}
		a = a.awaitingPark(id)
		frames = append(frames, rpc.Frame{Kind: rpc.FramePark, SessionID: id})
	}
	if len(frames) == 0 {
		return a, nil
	}
	return a, a.write(parkFailed, frames...)
}

// clearedAuthFailedOn drops an auth-failed mark the moment the session proves the
// login works again: a real model turn (KindAssistantText), which a failed turn
// never produces - its synthetic frame is the KindAPIError observe routed away.
// Without it a mark outlived the failure, and a later /reauth re-parked a session
// that had already recovered (a resume elsewhere, or the API coming back).
func (a App) clearedAuthFailedOn(sessionID string, ev core.Event) App {
	if ev.Kind == core.KindAssistantText {
		return a.clearAuthFailed(sessionID)
	}
	return a
}

// clearAuthFailed drops one session from the set. The three ways a mark stops
// describing a session all reach it: /reauth acting on it (reauth.go), a healthy
// turn (clearedAuthFailedOn), and a wake (wakeArrived) - so a stale mark cannot
// make a later /reauth re-park an already-recovered agent.
func (a App) clearAuthFailed(id string) App {
	if _, held := a.authFailed[id]; !held {
		return a
	}
	next := make(map[string]struct{}, len(a.authFailed))
	for held := range a.authFailed {
		if held != id {
			next[held] = struct{}{}
		}
	}
	a.authFailed = next
	if _, counted := a.authFailRetries[id]; counted {
		nextN := make(map[string]int, len(a.authFailRetries))
		for held, n := range a.authFailRetries {
			if held != id {
				nextN[held] = n
			}
		}
		a.authFailRetries = nextN
	}
	return a
}

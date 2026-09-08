package ui

// /reauth: bring back the sessions a shared-login expiry knocked out, in place,
// without killing the fleet.
//
// A Max-plan OAuth token expires for the whole fleet at once, and the sessions
// that lose the refresh race hold a dead login and 401 every turn thereafter
// (root cause and the upstream bug: docs/notes/bugs.md). A running claude
// process never picks up a fresh token - only a new process does - so
// re-logging in elsewhere cannot heal a live one, which is why the operator's
// external /login did not fix a running fleet. So this parks each session
// KindAPIError marked (apierror.go): a park stops the stale process and keeps
// the transcript, and /resume brings it back on a fresh login. Wake never runs
// `claude auth login` itself (no-PTY, authapp.go), so the login step stays
// theirs.
//
// It parks rather than parks-and-wakes in one step deliberately: an automatic
// wake would have to thread a tea.Cmd back through the fleet-report chain
// (applyStatus returns only App), a larger change than this fix carries. See
// docs/notes/deferred.md.

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	reauthNothing = "no session is in an auth-error state. /login shows your sign-in; if one is stuck, run `claude auth login` then /resume it"
	reauthBlocked = "the auth-failed sessions are all blocked on a permission ask - answer or interrupt those first, then /reauth"
	reauthParked  = "parked %d session(s) holding a stale login. Run `claude auth login` if /login shows you signed out, then /resume all to bring them back"
	// The mixed case: some parked, some skipped because a stop closes stdin on a
	// blocked ask. Named so a skipped, still-broken session is not silent.
	reauthSomeBlocked = "parked %d session(s); skipped %d blocked on a permission ask - answer those then /reauth. Then `claude auth login` if signed out, and /resume all"
)

// reauth parks every session a KindAPIError marked, so a fresh login reaches
// them through a new process. The set is the fleet's own (apierror.go), not a
// target the operator names, so arg is ignored.
func (a App) reauth(_ string) (App, tea.Cmd) {
	a = a.clearDraft()
	targets := a.authFailedLive()
	if len(targets) == 0 {
		notice.Report("%s", reauthNothing)
		return a, nil
	}
	var frames []rpc.Frame
	blocked := 0
	for _, id := range targets {
		if a.blockedAgent(id) {
			blocked++ // a park closes stdin; a blocked ask must be answered first
			continue
		}
		a = a.awaitingPark(id).clearAuthFailed(id)
		frames = append(frames, rpc.Frame{Kind: rpc.FramePark, SessionID: id})
	}
	if len(frames) == 0 {
		notice.Report("%s", reauthBlocked)
		return a, nil
	}
	if blocked > 0 {
		notice.Report(reauthSomeBlocked, len(frames), blocked)
	} else {
		notice.Report(reauthParked, len(frames))
	}
	// One write, sequential and fail-fast over the one connection - the way
	// takeHistoryAsks writes its per-session frames - rather than N concurrent
	// commands contending for the same write lock. See App.write.
	return a, a.write(parkFailed, frames...)
}

// authFailedLive is the marked sessions that still have a live process to park:
// a mark can outlive its session (it ended, or another window parked it), and
// asking the daemon to park one that is not running is a refusal logged for
// nobody. Sorted so the park writes and the tests have one order.
func (a App) authFailedLive() []string {
	live := make([]string, 0, len(a.authFailed))
	for id := range a.authFailed {
		agent, ok := a.fleet.Agent(id)
		if !ok || agent.State == rpc.StateEnded || agent.State == rpc.StateParked {
			continue
		}
		live = append(live, id)
	}
	slices.Sort(live)
	return live
}

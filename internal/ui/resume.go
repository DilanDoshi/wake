package ui

// `/resume` — bringing a parked session back, and the notice a woken one leaves.
//
// Split from slash.go, which had reached the 800-line hard max: the router and
// "what a leading slash means" stay there, and the resume subject's own
// machinery — the message constants, the parked-agent lookups, and the wake
// bookkeeping — lives here, the way `/new`, `/name` and `/color` each keep their
// handler in a file of their own. `resumeCommand`, `resumeVerb` and `resumeAll`
// stay in slash.go, because they are the vocabulary the `commands` map is built
// from; everything that does not spell a slash is here.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// noParkedSessions is /resume with nothing to bring back.
	//
	// **It names the two keys now, and that is the hint line's rule read
	// forwards rather than a change of mind.** It named none for one task, and
	// said so: the obvious sentence was *"⌃C parks the conversation you are in,
	// and ⌃Q parks the fleet on the way out"* - both true of the design and
	// neither true of that build, where ⌃C detached, so somebody who read it and
	// pressed ⌃C would have lost the window they were reading while believing
	// they had parked something. The lifecycle spec made park a prerequisite for
	// the rebinding rather than a companion to it. The rebinding has landed, both
	// keys do what this says, and slash_test.go now holds the sentence to the
	// legend - which is where "these keys exist" is decided.
	noParkedSessions = "nothing is parked, so there is nothing to bring back. ⌃C parks the conversation you are in, and ⌃Q parks the fleet on the way out"

	// noResumable is a bare /resume with nothing parked and nothing on disk to
	// bring back - the picker's own empty state, wider than noParkedSessions
	// because the picker offers on-disk conversations too.
	noResumable = "nothing to resume: nothing is parked, and there are no claude sessions on this machine. ⌃C parks the conversation you are in, and ⌃Q parks the fleet on the way out"

	// notParked is /resume aimed at something that is not parked.
	notParked = "%s%s is not parked, so there is nothing to bring back"

	// resumeFailed names the write that could not happen.
	resumeFailed = "bringing that session back"

	// resumeAsked is said on the keypress, because the daemon may refuse -
	// the id may be held by another process - and the operator should know the
	// command was read either way.
	resumeAsked  = "bringing %s%s back…"
	resumeAskedN = "bringing %d parked sessions back…"
)

// resume brings a parked session back: the one named, the one this
// conversation is with, or all of them.
//
// It writes frames and decides nothing about whether the resume is safe. That
// judgement is the daemon's - it is the only process that can ask the operating
// system whether anything else is running under the id, and a copy of that
// check here would be the parallel implementation this project forbids, stale
// the day resumeSafe changes and stale in the direction that resumes twice.
// Which is also why every refusal the daemon writes back is shown as it wrote
// it: those sentences name *when* the operator can act, and a local "could not
// resume" would replace the only useful half.
func (a App) resume(arg string) (App, tea.Cmd) {
	// Bare /resume opens the picker over the composer - always, on every surface
	// (owner's 2026-09-20 ruling): the old one-keypress "resume this pane's parked
	// session" fast path is gone. An argument stays the parked-only route below.
	if strings.TrimSpace(arg) == "" {
		return a.openResumePicker()
	}

	parked := a.parkedAgents()
	if len(parked) == 0 {
		notice.Report("%s", noParkedSessions)
		return a, nil
	}

	switch {
	case strings.EqualFold(arg, resumeAll):
		return a.bringBack(parked)

	default:
		who := strings.TrimPrefix(arg, agentPrefix)
		agent, ok := a.parkedNamed(who)
		if !ok {
			notice.Report(notParked+"\n%s", agentPrefix, who, parkedList(parked))
			return a, nil
		}
		return a.bringBack([]Agent{agent})
	}
}

// bringBack is the tail every arm that resumes shares: the draft goes, the
// operator is told what was read, and one command writes one wake per session.
//
// One session is named rather than counted, whichever arm asked. `/resume all`
// against a single parked session would otherwise say "1 parked sessions", and
// the name is the more useful half of that sentence anyway.
func (a App) bringBack(agents []Agent) (App, tea.Cmd) {
	a = a.clearDraft()
	if len(agents) == 1 {
		notice.Report(resumeAsked, agentPrefix, agents[0].Name)
	} else {
		notice.Report(resumeAskedN, len(agents))
	}
	ids := make([]string, 0, len(agents))
	for _, ag := range agents {
		ids = append(ids, ag.ID)
	}
	return a.awaitingWake(ids...), a.write(resumeFailed, wakeFrames(agents)...)
}

// wakeFrames is one wake per agent, built as a slice for App.write's rule:
// bubbletea runs every tea.Cmd on its own goroutine and rpc's write lock is
// process-wide, so `/resume all` against twenty parked sessions must be one
// command writing twenty frames rather than twenty commands.
func wakeFrames(agents []Agent) []rpc.Frame {
	out := make([]rpc.Frame, 0, len(agents))
	for _, agent := range agents {
		out = append(out, rpc.Frame{Kind: rpc.FrameWake, SessionID: agent.ID})
	}
	return out
}

// parkedAgents is every agent this client knows to be parked, in attention
// order - which puts them together, since they all rank the same.
//
// Two sources, because there are two ways to be parked. An agent parked with ⌃C
// is still in the fleet and still holds its name; one left in the park book by a
// previous daemon is not in the fleet at all, and is the whole reason /resume
// still has anything to name after a ⌃Q. They cannot overlap - the daemon takes
// a record out of the book as it launches, and a live row is one it is holding.
func (a App) parkedAgents() []Agent {
	var out []Agent
	for _, agent := range a.fleet.Agents() {
		if agent.State == rpc.StateParked {
			out = append(out, agent)
		}
	}
	return append(out, a.fleet.Parked()...)
}

// parkedNamed resolves a name to a parked agent. Exact and folded, the way
// Fleet.ByName is exact: the daemon guarantees no two live sessions share a
// name, a parked one still holds its name, and a prefix match belongs to
// `wake attach` where a person is typing at a shell.
func (a App) parkedNamed(who string) (Agent, bool) {
	for _, agent := range a.parkedAgents() {
		if strings.EqualFold(agent.Name, who) {
			return agent, true
		}
	}
	return Agent{}, false
}

// parkedList names what could be brought back, so a wrong name costs one line
// rather than two commands. Same job runningSessions does for `wake attach`.
func parkedList(parked []Agent) string {
	names := make([]string, 0, len(parked))
	for _, agent := range parked {
		names = append(names, agentPrefix+agent.Name)
	}
	return "parked: " + strings.Join(names, " ")
}

// resumedFormat is what a parked conversation says when `/resume` brings it
// back. The transcript comes back with it - a woken DM re-reads it from disk
// (history.go) and the room re-fetches its history (askRoomHistory) - so the
// notice is the fact of the return, not the old caveat that the history was
// claude's and gone.
const resumedFormat = "%s%s has been resumed."

// ResumedNotice is that sentence, for wakeArrived.
func ResumedNotice(name string) string {
	return fmt.Sprintf(resumedFormat, agentPrefix, name)
}

// attachedFormat is what `wake attach` says when it reconnects to a live
// session. That session was never parked, so it is a reattachment rather than a
// resume; its transcript loads from disk as the pane opens (history.go). The
// word is deliberately hangup.go's own: `wake attach` is the manual counterpart
// to the automatic socket-redial there (cmd/wake/attach.go's `reattach`), so the
// shared "reattached" names one idea on two surfaces rather than colliding.
const attachedFormat = "%s%s — reattached."

// AttachedNotice is that sentence, exported for cmd/wake's attach path - two
// events, so two sentences, rather than one that would say "resumed" about a
// session nothing parked.
func AttachedNotice(name string) string {
	return fmt.Sprintf(attachedFormat, agentPrefix, name)
}

// wakeArrived says it the first time a report shows a session this client asked
// to wake as running again.
//
// parkArrived's shape, for parkArrived's reason: the daemon refuses a wake for
// real reasons - something already holds the id, the record carries no
// directory - so the keypress may only name the ask. Once per transition, and
// only for sessions this client asked about: another window's /resume is not
// this operator's business.
func (a App) wakeArrived(st *rpc.Status) App {
	if st == nil || len(a.waking) == 0 {
		return a
	}
	for _, s := range st.Sessions {
		if _, asked := a.waking[s.ID]; !asked || s.State == rpc.StateParked {
			continue
		}
		next := make(map[string]struct{}, len(a.waking))
		for id := range a.waking {
			if id != s.ID {
				next[id] = struct{}{}
			}
		}
		a.waking = next
		// A woken session comes back on a fresh process, so any auth-failed mark
		// from before the park no longer describes it; if its login is still
		// expired the next turn re-marks it. See apierror.go.
		a = a.clearAuthFailed(s.ID)
		notice.Report("%s", ResumedNotice(s.Name))
		a = a.modeReverted(s.ID, s.Name)
		// The room is missing everything this session said before it was
		// parked, and this is the only report that says it has been resumed. A fork is
		// refused here as it is at the seed - its transcript is its parent's.
		// See roomhistory.go.
		if !isFork(s) {
			a = a.askRoomHistory(s.ID)
		}
	}
	return a
}

// resumeReadyMsg is what the disk walk hands back, folded by App.Update.
type resumeReadyMsg struct {
	disk []DiskSession
	err  error
}

// openResumePicker gathers what can be resumed and opens the picker. The parked
// half is local; the on-disk half needs the walk of ~/.claude/projects, so it
// goes off the draw goroutine (adopt.go's rule) and the picker opens when it
// returns. With no way to see the disk it opens on the parked half alone.
func (a App) openResumePicker() (App, tea.Cmd) {
	if a.sessions == nil {
		return a.showResume(nil)
	}
	a = a.clearDraft()
	return a, resumableCmd(a.sessions)
}

// resumableCmd walks the disk on its own goroutine, askMachine's arrangement.
func resumableCmd(s Sessions) tea.Cmd {
	return func() tea.Msg {
		disk, err := s.Resumable()
		return resumeReadyMsg{disk: disk, err: err}
	}
}

// resumeArrived folds the walk: the rows open the picker, and a read that failed
// falls back to the parked half rather than to nothing.
//
// A slow ~/.claude/projects walk (hundreds of transcripts on NFS) can land after
// the operator has moved on and started typing. Opening then would clear the
// draft they began - openResume calls clearDraft - so a walk that returns to a
// non-empty composer is dropped rather than stealing it. The common case is a
// sub-second walk into a still-empty box, which opens.
func (a App) resumeArrived(m resumeReadyMsg) (App, tea.Cmd) {
	if a.composer().Value() != "" {
		return a, nil
	}
	if m.err != nil {
		notice.Report("could not read this machine's sessions, showing parked only: %v", m.err)
		return a.showResume(nil)
	}
	return a.showResume(m.disk)
}

// showResume merges the parked fleet and the disk rows and opens the picker, or
// reports the empty machine.
func (a App) showResume(disk []DiskSession) (App, tea.Cmd) {
	rows, more := a.resumeRowsFrom(disk)
	if len(rows) == 0 {
		notice.Report("%s", noResumable)
		return a, nil
	}
	return a.openResume(rows, more, a.focus == ""), nil
}

// resumeRowsFrom merges parked sessions and on-disk conversations into the
// picker's rows, newest first.
//
// It is driven off the disk walk, which carries an mtime for **every**
// transcript - including a parked session's own - so recency sorts the whole set
// without a timestamp on any wire (no rpc.SessionStatus field, so no reflective
// guard to satisfy). Each disk row is annotated from the fleet: a **live** id is
// dropped (it is running, not resumable), a **parked** id becomes a FrameWake row
// named by its @name, and anything else is an on-disk stranger that resumes in
// place. A parked session whose transcript the walk missed is still appended, so
// it stays resumable.
func (a App) resumeRowsFrom(disk []DiskSession) (rows []resumeRow, more int) {
	live := map[string]bool{}
	for _, ag := range a.fleet.Agents() {
		if ag.State != rpc.StateParked {
			live[ag.ID] = true
		}
	}
	parked := map[string]Agent{}
	for _, ag := range a.parkedAgents() {
		parked[ag.ID] = ag
	}

	all := make([]resumeRow, 0, len(disk)+len(parked))
	seen := map[string]bool{}
	for _, d := range disk {
		// One id, one row - discovery does not dedup across project slugs, so a
		// transcript copied or resumed under a differently-slugging cwd can appear
		// twice; the first (newest, since disk is newest-first) wins. Without this
		// a second occurrence of a *parked* id misses the map (deleted below) and
		// is drawn as a mislabeled stranger.
		if live[d.ID] || seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		if ag, isParked := parked[d.ID]; isParked {
			all = append(all, parkedRow(ag, ago(d.Modified)))
			delete(parked, d.ID)
			continue
		}
		all = append(all, resumeRow{
			ID: d.ID, Dir: d.Dir, Preview: d.Preview, Title: d.Title,
			Age: ago(d.Modified), Resumable: d.Dir != "",
		})
	}
	// Parked sessions the walk did not surface (no transcript found, or no disk
	// at all) still belong in the list - the daemon resumes them from the book.
	// Sorted by name so the tail is deterministic, since a map is not.
	leftover := make([]Agent, 0, len(parked))
	for _, ag := range parked {
		leftover = append(leftover, ag)
	}
	sort.Slice(leftover, func(i, j int) bool { return leftover[i].Name < leftover[j].Name })
	for _, ag := range leftover {
		all = append(all, parkedRow(ag, ""))
	}

	if len(all) > resumePickerMax {
		more = len(all) - resumePickerMax
		all = all[:resumePickerMax]
	}
	return all, more
}

// parkedRow is one parked session as a picker row: it wakes in place (FrameWake),
// and it is always resumable here because the daemon holds its directory in the
// park book - if that book row somehow has none, the daemon's own refusal shows.
func parkedRow(ag Agent, age string) resumeRow {
	return resumeRow{
		ID: ag.ID, Name: ag.Name, Dir: ag.Cwd, Label: ag.Label,
		Age: age, Parked: true, Resumable: true,
	}
}

// ago is a coarse "how long since anything was written" for a row, discover.go's
// own age one package over: the number says when the file was last touched, which
// is not when the session ended and is certainly not whether it is running.
func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// awaitingWake remembers a wake this client asked for.
func (a App) awaitingWake(ids ...string) App {
	next := make(map[string]struct{}, len(a.waking)+len(ids))
	for held := range a.waking {
		next[held] = struct{}{}
	}
	for _, id := range ids {
		next[id] = struct{}{}
	}
	a.waking = next
	return a
}

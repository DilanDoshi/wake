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
	"strings"

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

	// noResumeTarget is /resume in the room with no name after it. It refuses
	// rather than guessing, for the reason the room refuses an unaddressed
	// draft: with several parked, picking one for somebody is not a
	// recoverable mistake - they type into it.
	noResumeTarget = "which one? " + resumeVerb + " <name>, or " + resumeVerb + " " + resumeAll

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
	parked := a.parkedAgents()
	if len(parked) == 0 {
		notice.Report("%s", noParkedSessions)
		return a, nil
	}

	switch {
	case strings.EqualFold(arg, resumeAll):
		return a.bringBack(parked)

	case arg == "":
		// A DM names its recipient in its own header, so a bare /resume there
		// is unambiguous. The room is not one conversation and does not guess.
		agent, ok := a.parkedHere()
		if !ok {
			notice.Report("%s\n%s", noResumeTarget, parkedList(parked))
			return a, nil
		}
		return a.bringBack([]Agent{agent})

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

// parkedHere is the parked agent this conversation is with, when the focused
// pane is a conversation at all.
func (a App) parkedHere() (Agent, bool) {
	if a.focus == "" {
		return Agent{}, false
	}
	agent, ok := a.fleet.Agent(a.focus)
	if !ok || agent.State != rpc.StateParked {
		return Agent{}, false
	}
	return agent, true
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

// transcriptFormat is what a conversation that has just come back says about
// itself, and it is one sentence because two surfaces say it.
//
// `wake attach` has said it since Phase 1: a pane that opens empty over a
// session with an hour behind it reads as a session that lost it, and the
// truth is narrower and worth stating - claude keeps the transcript on disk and
// Wake never had it. `/resume` produced the identical surprise and said
// nothing.
const transcriptFormat = "%s%s is back. What it said before now is not here - claude keeps the transcript, Wake does not"

// TranscriptNotice is that sentence, for the two callers that need it: this
// package on a wake, and cmd/wake on an attach. Exported for the second, which
// is ParkedNotice's arrangement and its reason - the sentence lives beside the
// thing it describes rather than being written twice.
func TranscriptNotice(name string) string {
	return fmt.Sprintf(transcriptFormat, agentPrefix, name)
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
		notice.Report("%s", TranscriptNotice(s.Name))
		a = a.modeReverted(s.ID, s.Name)
		// The room is missing everything this session said before it was
		// parked, and this is the only report that says it is back. A fork is
		// refused here as it is at the seed - its transcript is its parent's.
		// See roomhistory.go.
		if !isFork(s) {
			a = a.askRoomHistory(s.ID)
		}
	}
	return a
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

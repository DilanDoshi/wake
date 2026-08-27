package ui

// `/name` and `/task` — the two halves of `sydney <> dev-5748`.
//
// # What the founding message asked for
//
// *"…it would default to a random name like alex or john or sydney and then you
// can either **rename** or **assign a 'task'** so they are called like
// `sydney <> dev-5748` or `alex <> ui fixes`."* The random name shipped in Phase
// 1 and the derived label with it; neither could be set by a person, so
// `alex <> ui fixes` was not reachable from anywhere in the build.
//
// # Two commands and not one
//
// The two halves are used in different places and mean different things.
// `cmd/wake/status.go` draws the labelled title, while the DM header and the
// notice row draw the bare name, "because §7 routes on `@name` and a handle
// with a branch glued to it is not a handle". So the name is an **address** an
// operator types and the label is **prose** they read, and one command with a
// separator would be one keystroke away from setting the wrong one.
//
// The wire keeps them apart for a sharper reason - an empty name and an empty
// label want opposite defaults, see rpc.FrameRename - and this is the same
// distinction one layer up.
//
// # `@` is what makes the grammar unambiguous rather than positional
//
// `/name bob` in a conversation can only be "call this one bob". `/name @alex
// bob` can only be "call alex bob". Without the marker the same two words mean
// different things depending on which pane has the keys, and the mistake is
// unrecoverable in the way this project cares about: it changes where the
// operator's next `@` goes, and a name that resolves is never reported.
//
// A name cannot start with `@` - normalizeName requires a letter - so nothing
// an operator can legally call an agent collides with the marker.
//
// # Everything else is the daemon's, including which sessions may be renamed
//
// This resolves a handle to an id and writes a frame. Whether the name is free,
// whether it is one of the router's reserved words, whether it is short enough
// not to contain a UUID, and whether a parked session may be renamed at all are
// all decided behind the socket, because the daemon is the only process that
// can see the whole fleet - and a cheerful copy of any of those here is a
// second answer that goes stale the day the first one moves.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// renameFailed and labelFailed name the write that could not happen, so the
	// notice row says which command was typed rather than only what the socket
	// said about it.
	renameFailed = "renaming that agent"
	labelFailed  = "labelling that agent"

	// renameAsked and labelAsked are said on the keypress, for forkAsked's
	// reason: the daemon may refuse - the name is taken, the session is parked -
	// and the operator should know the command was read either way.
	//
	// **Neither one echoes the value, and that is the point rather than
	// brevity.** The client does not know what will be stored: normalizeName
	// folds case, so `/name @alex BOB` said *"to @BOB"* and stored `bob`, and
	// normalizeLabel truncates at a column bound this side cannot see without
	// keeping a second copy of it - so `/task <nineteen words>` said the whole
	// thing and stored eighteen runes of it. A confirmation that reports the
	// request as though it were the outcome is the lying-feature shape, and it
	// was worse than usual here: labelAsked ended in `…`, the same rune as
	// truncationMark, so an untruncated label read as truncated and a truncated
	// one read as in progress.
	//
	// What the operator gets instead is the stored value, from the surface that
	// has it: a rename lands on the DM header and the roster the moment the
	// report arrives (App.renamed), which is derived from the daemon and cannot
	// disagree with it. The label has no such surface for an agent that has not
	// spoken - recorded in deferred.md, and it is the roster's to answer.
	renameAsked = "renaming %s%s…"
	labelAsked  = "labelling %s%s…"

	// noSuchAgent is an `@handle` that names nobody. It lists the fleet, for
	// parkedList's reason: a wrong name then costs one line rather than two
	// commands.
	noSuchAgent = "no live agent answers to that handle"
)

// renameAgent changes what one agent is called.
func (a App) renameAgent(arg string) (App, tea.Cmd) {
	agent, to, ok := a.displayTarget(arg, nameUsage, noNameTarget)
	if !ok {
		return a, nil
	}
	if len(strings.Fields(to)) != 1 {
		// A name is one word by construction - normalizeName admits no
		// whitespace - so two is a shape this does not read rather than a name
		// the daemon would refuse with a worse sentence. A label is prose and
		// gets no such check, which is the whole of why these are two verbs.
		notice.Report("%s", nameUsage)
		return a, nil
	}
	a = a.clearDraft()
	notice.Report(renameAsked, agentPrefix, agent.Name)
	return a, a.write(renameFailed, rpc.Frame{Kind: rpc.FrameRename, SessionID: agent.ID, Text: to})
}

// labelAgent says what one agent is working on.
func (a App) labelAgent(arg string) (App, tea.Cmd) {
	agent, label, ok := a.displayTarget(arg, taskUsage, noTaskTarget)
	if !ok {
		return a, nil
	}
	a = a.clearDraft()
	notice.Report(labelAsked, agentPrefix, agent.Name)
	return a, a.write(labelFailed, rpc.Frame{Kind: rpc.FrameLabel, SessionID: agent.ID, Text: label})
}

// displayTarget reads `[@who] <value>` into the agent it is about and the value
// it carries, reporting why it could not.
//
// One function for both verbs because the grammar is one grammar; the two
// sentences it refuses with are parameters because they name different
// commands, and a refusal that names the wrong one is worse than none.
//
// The target is the conversation you are in, which is `/resume`'s rule read
// across: a DM names its recipient in its own header, so a bare command there
// is unambiguous, and the room is not one conversation and does not guess. It
// is deliberately **not** dmTarget's rule, which falls back to the roster
// cursor - ⌃F may open a pane over the wrong agent and be closed, and a rename
// changes where the next message goes.
func (a App) displayTarget(arg, usage, noTarget string) (Agent, string, bool) {
	who, value := "", strings.TrimSpace(arg)
	if handle, rest, _ := strings.Cut(value, " "); strings.HasPrefix(handle, agentPrefix) {
		who, value = strings.TrimPrefix(handle, agentPrefix), strings.TrimSpace(rest)
	}
	if value == "" {
		notice.Report("%s", usage)
		return Agent{}, "", false
	}
	if who == "" {
		agent, ok := a.conversationAgent()
		if !ok {
			notice.Report("%s", noTarget)
			return Agent{}, "", false
		}
		return agent, value, true
	}
	agent, ok := a.fleet.ByName(who)
	if !ok {
		notice.Report("%s\n%s", noSuchAgent, a.handleList())
		return Agent{}, "", false
	}
	return agent, value, true
}

// conversationAgent is the agent this conversation is with, when the focused
// pane is a conversation at all. parkedHere's shape, and its reason.
func (a App) conversationAgent() (Agent, bool) {
	if a.focus == "" {
		return Agent{}, false
	}
	return a.fleet.Agent(a.focus)
}

// handleList is every handle that resolves, so a wrong one costs a line rather
// than a hunt. live() is the list because it is already "every agent that could
// be addressed", and a second answer to that question is one that drifts.
func (a App) handleList() string {
	live := a.live()
	if len(live) == 0 {
		return noneListening
	}
	handles := make([]string, 0, len(live))
	for _, who := range live {
		handles = append(handles, agentPrefix+who.Name)
	}
	return "live: " + strings.Join(handles, " ")
}

// renamed follows a rename into every conversation this client holds.
//
// # Why the header cannot keep the name it was opened under
//
// This is the ruling's own safety argument arriving at the one surface it does
// not cover. `/name` trades on *"the old handle resolves to nothing, so the
// operator reads a refusal"* - true of a composer, of `wake attach`, of every
// place a name is **resolved**. A DM header is read by a person and then typed,
// and a name goes back to the pool the moment its session gives it up. So a
// stale header is a handle pointing at whoever draws that name next, and the
// outcome is not a refusal but a delivery to a stranger, with nothing anywhere
// reporting it. CLAUDE.md already says this about the fork header - *"the header
// is the surface `@name` routes on, so a reused name would point at somebody who
// is not the parent"* - and the same sentence is true of the name itself.
//
// # Over the whole map, not the open one
//
// `hideDM` keeps a conversation's transcript so ⌃W is reversible, and
// `openDMWith` builds a DM only when the map has none - so a refresh keyed on
// opening would miss exactly the conversation somebody closed before the rename
// and came back to after it.
//
// ParentName travels in the same pass because it is the same defect one surface
// down: a renamed *parent* leaves `forked from @alex` on a child that is already
// open. It was already re-read on every open, which is the narrower half of this.
//
// # What it costs
//
// One map lookup per session in a report, and a copy only for a conversation
// whose name actually moved - which is none, on every report but the one after a
// rename. The room already walks st.Sessions three times in this chain.
func (a App) renamed(st *rpc.Status) App {
	if len(a.dms) == 0 {
		return a
	}
	for _, s := range st.Sessions {
		dm, open := a.dms[s.ID]
		if !open {
			continue
		}
		parent := a.parentName(s.ID)
		if dm.Name == s.Name && dm.ParentName == parent {
			continue
		}
		a = a.withDM(s.ID, dm.WithName(s.Name).WithParentName(parent))
	}
	return a
}

// Sentences that *spell* a command - the usage lines, and the two refusals that
// name the way round - live in slash.go, which is where a leading slash is
// known. See newUsage. What is here is what does not spell one.

package ui

// Branching a conversation from inside it.
//
// # Why a command writes a frame and the daemon does the rest
//
// The same reason ⎋ does: this view never touches a process. /fork mints the
// fork's own id - Wake originates identity, and maySpawn refuses anything that
// is not a UUID because the reaper's only proof of a process group is that id
// in an argv - and hands the daemon a FrameFork. Everything after that is the
// daemon's: whether the parent is in a state a fork has been recorded against,
// which name the fork draws, and which directory it runs in.
//
// # Why the gate is not repeated here
//
// A fork is refused while the parent is not idle, and that rule lives in one
// place, behind the socket, because the daemon is the only process that can
// see a session's state. A cheerful local pre-check would be a second copy of
// it, drifting the first time one is edited.
//
// # A fork is a snapshot, and this file is where the UI says so
//
// A parent's later turns reach no fork, at either recorded generation. Nothing
// stops the operator typing to the parent a second later - they own its
// composer - so the honest thing is to say what was taken rather than to
// pretend it stays in step.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// forkFailed names the write that could not happen, so the notice row says
	// which command was asked for rather than only what the socket said about it.
	forkFailed = "forking that conversation"

	// noForkTarget is /fork with no agent in front of it. A fork is taken *from*
	// one conversation; the room is not one conversation.
	noForkTarget = forkVerb + " branches one conversation. " + noPaneAdvice

	// forkUsage is what a /fork argument that is not one @who is told.
	forkUsage = forkVerb + " takes one " + agentPrefix + "who, or none to branch the conversation you are in"

	// forkAsked is said when the command is read rather than on the confirmation,
	// because the daemon may refuse and the operator should know the command was
	// read either way. Two asks produce the same line, which notice folds
	// into one entry with a count - `forking @alex… (×2)` is the right reading
	// of two forks asked for, and both of them arrive.
	forkAsked = "forking %s%s…"

	// forkAskedUnnamed is the same line for a target no report has named yet -
	// an agent Fleet.Observe created from an event that outran its
	// confirmation. The alternative is agentName's fallback, which would spell
	// a raw UUID on a one-line notice row; the header takes the same decision
	// in the same words, "by name or not at all".
	forkAskedUnnamed = "forking that conversation…"

	// forkOpened is said when the fork's own conversation arrives, and the
	// second half is not decoration. A fork inherits the parent's conversation
	// **as of the moment it was taken**: the parent's later turns appear in no
	// fork's transcript, at either recorded generation. A UI that let somebody
	// believe otherwise would be wrong in the direction that costs work.
	forkOpened = "%s%s is a fork of %s%s. It has that conversation as of now - nothing %s%s does next reaches it."

	// forkOpenedUnnamed is the same promise for a parent this client cannot
	// name - one that has aged out of the report, or whose handle now belongs
	// to a live agent that is not it. The name is the decoration and the
	// snapshot is the claim, so the claim survives losing the name.
	forkOpenedUnnamed = "%s%s is a fork. It has its parent's conversation as of now - nothing the parent does next reaches it."
)

// SnapshotNotice is the sentence every confirmed fork says, wherever it is
// confirmed.
//
// Exported because there are two fork surfaces and this promise is about both:
// /fork says it through startArrived, and `wake fork <who>` says it from cmd/wake
// once the daemon has confirmed the spawn. One spelling with two callers rather
// than two spellings - the claim has already been written five times in
// artefacts and implemented once, and a second copy of the words is how that
// happens a sixth time.
//
// parent is a name or "". Never an id: this lands on the notice row of the
// fork's own conversation, beside a header that draws a parent it cannot name
// as nothing at all, and eight hex characters in a sentence about what @alex
// does next are worse than no name, because the reader can type them at nobody.
func SnapshotNotice(fork, parent string) string {
	if parent == "" {
		return fmt.Sprintf(forkOpenedUnnamed, agentPrefix, fork)
	}
	return fmt.Sprintf(forkOpened, agentPrefix, fork, agentPrefix, parent, agentPrefix, parent)
}

// forkAgent is `/fork [@who]`, which took ⌃F's fork when ⌃F became the jump picker.
// Bare is the conversation you are in or, in the room, the agent the roster has
// picked - commandTarget's rule, as /status and /mcp have it - and @who is that
// agent. It never guesses: ⌃F's fallback to the first agent in attention order
// is gone, since a typed command must not branch an agent it was not aimed at.
//
// Bare on an ended agent's own conversation is not refused here: its transcript is
// on disk, the recordings forked exited parents throughout, and the daemon decides.
// @who names a live or parked agent, as every @who does.
func (a App) forkAgent(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	agent, ok := a.commandTarget(arg, forkUsage, a.unseenOr(noForkTarget))
	if !ok {
		return a, nil
	}
	return a.forkOf(agent)
}

// forkOf asks the daemon to branch one agent's conversation, and waits on the id it minted.
func (a App) forkOf(agent Agent) (App, tea.Cmd) {
	forkID := uuid.NewString()
	a = a.awaitingStart(forkID)
	notice.Report("%s", forkAskedOf(agent.Name))
	return a, a.write(forkFailed, rpc.Frame{
		Kind:      rpc.FrameFork,
		SessionID: forkID,
		ParentID:  agent.ID,
	})
}

// forkAskedOf is the asked line for one target, named or not.
func forkAskedOf(parent string) string {
	if parent == "" {
		return forkAskedUnnamed
	}
	return fmt.Sprintf(forkAsked, agentPrefix, parent)
}

// parentName is the name of the conversation one session was forked from, or
// "" when this client cannot name it.
//
// By name or not at all. The parent's id would be available - it is on every
// report the fork appears in - and printing eight hex characters in the header
// is exactly what names exist to replace, which names.go argues at length.
// `wake status` is where an id fragment is the right answer, because that is
// where ids are printed and where `wake attach` resolves them.
//
// # And a reused name is not ancestry
//
// A name goes back to the pool when its session ends, while the ended session
// stays in the report - so a fork's parent can end, a new session can draw that
// name, and this client holds both. `forked from @alex` in the header would
// then name a *live* agent that is not the parent, on the one surface where a
// handle is an address: @alex in a composer resolves the live one, because
// Fleet.ByName skips ended sessions. So the name is drawn only while it still
// points at the parent, and otherwise nothing is drawn at all - the same ruling
// cmd/wake's sessionNames makes for the listing, where the fallback is the
// short id because that column prints ids and `wake attach` resolves them.
func (a App) parentName(sessionID string) string {
	agent, ok := a.fleet.Agent(sessionID)
	if !ok || agent.ParentID == "" {
		return ""
	}
	parent, ok := a.fleet.Agent(agent.ParentID)
	if !ok {
		return ""
	}
	if live, ok := a.fleet.ByName(parent.Name); ok && live.ID != parent.ID {
		return ""
	}
	return parent.Name
}

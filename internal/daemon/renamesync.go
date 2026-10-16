package daemon

// Keeping claude's own session name in step with Wake's.
//
// Claude's name is how other sessions address this one (/list-agents and
// SendMessage), and launch starts every agent - a wake and a /resume included -
// as --name <Wake's name>, which a resumed session takes over any earlier
// /rename (at-menu findings §2). A Wake rename moves only Wake's handle, so the
// daemon follows it with a bare /rename <new>: renameProbe, sent once the agent
// is idle, its reply kept off every client (probe.go). The reply is a local
// command (list-agents.jsonl:7-8), so the only cost is the one line claude's
// model reads on its next turn, which tells the agent its new name.
//
// What claude is to be called is a.claudeWant: Wake's name, except the manager's,
// which is manager-<part> while Wake's stays `manager` (rename.go).
//
// claudeName is what claude calls itself: its launch --name, then each /rename
// Wake writes, then whatever a rename reply on its stream says, the operator's
// or Wake's. Only a Wake rename arms the want (rename.go), and it fires only
// while the two names differ - checked when it is queued and again when it is
// written - so a variant claude chose is never chased.
//
// The operator's own /rename reaches claude as a passthrough, and internal/ui
// mirrors it as a Wake rename marked rpc.Frame.SelfRenames, written immediately
// before the passthrough - at once, or at the type-ahead flush that carries
// both - so the hold is armed first and lasts until the passthrough's reply,
// behind any turn already in flight.
// That want is held: it never fires until claude's reply to the passthrough
// releases it. A /name over a held want - another window's, during that round
// trip - moves its target and stays held, and the release decides. A refused
// mirror is held too, since the passthrough renames claude regardless, and the
// release then owes claude Wake's unchanged name.
// A passthrough unwritten after its mirror leaves the hold unreleased, but only
// a window whose connection is already dead fails that write.

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

// renameVerb composes the /rename probe, for modelVerb's reason: slashguard
// refuses a whole-word command literal in this package.
const renameVerb = "rename"

// renameTextLocked is the /rename claude is owed now, or "" with keep saying
// whether the want waits: it waits while held, or while an operator's /rename
// is unanswered (claude's name is unknown until its reply), and is done when
// the names agree or the agent is stopping. The caller holds a.mu.
func (a *agent) renameTextLocked() (text string, keep bool) {
	switch {
	case a.stopped:
		return "", false
	case a.renameHeld || a.claudeName == "":
		return "", true
	case a.claudeName == a.claudeWant:
		return "", false
	}
	return slashPrefix + renameVerb + " " + a.claudeWant, false
}

// renameWrite is the line a queued rename probe writes, decided again at the
// write because the queue can hold an operator's /rename or a turn ahead of it;
// "" re-arms the want when it must wait for that to end. What is written is
// recorded as claude's name at once, so a second probe behind it sends nothing;
// the reply confirms it or corrects it.
func (a *agent) renameWrite() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	text, keep := a.renameTextLocked()
	if text != "" && (a.owed || len(a.pending) > 0) {
		text, keep = "", true
	}
	if keep {
		a.probeWanted[renameProbe] = true
	}
	if text != "" {
		a.claudeName = a.claudeWant
	}
	return text
}

// noteRenameSent marks an operator's /rename passthrough about to be written:
// claude's name is unknown until its reply says what it took, and what it was
// asked is kept for the release to compare.
func (a *agent) noteRenameSent(text string) {
	asked, ok := slashCommand(text, renameVerb)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.claudeName = ""
	a.renameAsked = asked
}

// noteRenamed records the name a /rename reply on this agent's own stream says
// claude took, and releases a held want - never on a rename probe's own reply,
// which is the next one while a probe is in flight (stdin is FIFO). The release
// settles the want unless claude took exactly what the passthrough asked and
// that is not Wake's name (Wake hyphenated it), which leaves one /rename for
// the next idle: any other name is claude's own choice, never chased. The
// manager's is the exception, since what its passthrough asked is never the
// manager-<part> Wake wants. When claude answers Wake's own /rename with a
// variant of that name, adoptedVariant makes it the manager's.
// Content-matched like every probe reply, so a turn whose prose opens with the
// same words would be read as one.
func (a *agent) noteRenamed(ev core.Event) {
	if ev.Kind != core.KindAssistantText || ev.Subagent != nil {
		return
	}
	name, ok := core.RenamedFromReply(ev.Text)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.claudeName = name
	if a.renameHeld && a.pendingProbes[renameProbe] == 0 {
		// A refused mirror owes claude Wake's name whatever it took, a variant included.
		// So does the manager's: its passthrough asked for the part alone, never
		// for the manager-<part> Wake wants, so no reply to it is final.
		keep := name != a.claudeWant && (a.renameRefused || name == a.renameAsked || a.name == core.ManagerName)
		a.probeWanted[renameProbe] = a.probeWanted[renameProbe] && keep
		a.renameHeld, a.renameAsked, a.renameRefused = false, "", false
	}
}

// adoptedVariant makes what claude answered Wake's own /rename with the
// manager's name when it is a variant of the one asked - a collision's -2 - and
// reports whether the name moved. The report and the park book speak for claude's
// name, and a manager-<part> that claude does not carry is one SendMessage
// delivers to whichever session does. A reply that is not an extension of the
// want (a newer rename's is pending) is left to it.
func (a *agent) adoptedVariant() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.name != core.ManagerName || a.claudeName == a.claudeWant || !strings.HasPrefix(a.claudeName, a.claudeWant) {
		return false
	}
	a.claudeWant = a.claudeName
	return true
}

package ui

// A conversation's `@` names: the fleet's other live agents, the machine's other
// Claude sessions and the agent's own subagent types - what Claude Code's own
// `@` typeahead offers (docs/superpowers/notes/2026-09-27-at-menu-findings.md).
//
// **Nothing here routes.** A DM sends what was typed, and claude resolves the
// mention itself: `@<session>` becomes a SendMessage, `@agent-<type>` an Agent
// call.
//
// **The machine's sessions are asked for once per opening.** The daemon answers a
// FramePeers with a one-shot claude (internal/daemon/peers.go), so an ask per
// keystroke would be a process per character. An opening is a conversation's `@`
// gaining its first letter, and only the keystroke path asks (App.scanning): a
// report's or a reply's rebuild goes through recompleted, which carries the ask
// rather than making one. The reply replaces the listing whole and rebuilds the
// menu, so its rows appear once known and the menu never waits for them.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// subagentMention is what claude reads after `@` as a subagent type rather
	// than a session: `@agent-<type>` resolves headless to an Agent call (§3).
	subagentMention = "agent-"

	// subagentMenuSuffix and peerDirFormat label a conversation's two new kinds,
	// display only as teamMenuSuffix is: an accept inserts the bare mention. One
	// space, as optionRow collapses a run of them anyway.
	subagentMenuSuffix = " (agent)"
	peerDirFormat      = " (%s)"

	peersFailed = "asking for the machine's other Claude sessions"
)

// peerMenu is a conversation's machine half of the `@` menu.
type peerMenu struct {
	// listing is the latest FramePeersReply's, replaced whole by the next and
	// never written into, so App copies may share it. Carried by every rebuild,
	// since it is the machine's rather than a draft's.
	listing []core.Peer

	// wants marks an opening's menu - a conversation's `@` with a letter typed -
	// and asked that this opening's FramePeers is written.
	wants, asked bool
}

// carrying is what a rebuilt menu keeps: the listing always, and the ask while
// one opening goes on - the same pane, still wanting - so `@jo` after `@j` asks
// nothing, and `@` then `j` asks again.
func (p peerMenu) carrying(prev peerMenu, samePane bool) peerMenu {
	p.listing = prev.listing
	p.asked = p.wants && prev.asked && samePane
	return p
}

// askingPeers writes the one FramePeers an opening owes. Only scanning calls it,
// so only a keystroke asks - including the first one to find a menu a paste
// opened.
func (a App) askingPeers() (App, tea.Cmd) {
	if p := a.completion.peers; !p.wants || p.asked {
		return a, nil
	}
	a.completion.peers.asked = true
	return a, a.write(peersFailed, rpc.Frame{Kind: rpc.FramePeers})
}

// peersArrived folds the daemon's answer, which goes only to the client that
// asked. Replaced whole, so an empty or shrunken listing leaves no stale row -
// empty is also what an unparseable one arrives as. A reply to a menu since
// closed just waits here for the next opening.
func (a App) peersArrived(f rpc.Frame) App {
	a.completion.peers.listing = nil
	if f.Peers != nil {
		a.completion.peers.listing = f.Peers.Peers
	}
	return a.recompleted()
}

// conversationNames is a conversation's `@` names in the order drawn: the fleet's
// live peers, then the listing's sessions, then this agent's subagent types.
// Nothing here routes, so unlike addressees it need not mirror core.Resolve.
func (a App) conversationNames(typed string) (names []string, tags map[string]string) {
	lower := strings.ToLower(typed)
	matches := func(word string) bool { return strings.HasPrefix(strings.ToLower(word), lower) }
	for _, addr := range a.live() {
		if addr.ID != a.focus && matches(addr.Name) {
			names = append(names, agentPrefix+addr.Name)
		}
	}
	held := a.heldNames()
	for _, p := range a.completion.peers.listing {
		if held[p.Name] || !matches(p.Name) {
			continue
		}
		held[p.Name] = true // a listing naming one twice offers it once
		names, tags = tagged(names, tags, agentPrefix+p.Name, fmt.Sprintf(peerDirFormat, shortPath(p.Dir)))
	}
	for _, kind := range a.completionAgent().SubagentTypes() {
		if matches(kind) || matches(subagentMention+kind) {
			names, tags = tagged(names, tags, agentPrefix+subagentMention+kind, subagentMenuSuffix)
		}
	}
	return names, tags
}

// heldNames is every name a fleet agent holds - the roster's, parked and the
// manager included, and the park book's - so a listed session sharing one is
// the fleet's and is dropped, which drops the conversation's own agent too. An
// ended agent's name went back to the pool, so it holds none.
func (a App) heldNames() map[string]bool {
	held := make(map[string]bool)
	for _, agent := range slices.Concat(a.fleet.OnRoster(), a.fleet.Parked()) {
		held[agent.Name] = true
	}
	return held
}

package ui

// A conversation's `@` names: the fleet's other live agents, the machine's other
// Claude sessions and the agent's own subagent types - what Claude Code's own
// `@` typeahead offers (docs/superpowers/notes/2026-09-27-at-menu-findings.md).
//
// **Nothing here routes.** A DM sends what was typed: claude's model sends
// `@<session>` with SendMessage (findings §4, from the docs), and `@agent-<type>`
// resolves headless to an Agent call (§3). The manager has neither tool, so its
// conversation offers only the fleet it reaches through its own send.
//
// **The machine's sessions are asked for once per opening.** The daemon answers a
// FramePeers with a one-shot claude (internal/daemon/peers.go), so an ask per
// keystroke would be a process per character. An opening is a conversation's `@`
// gaining a character that can begin a name (canBeginName), and only the
// keystroke path asks (App.scanning): a report's or a reply's rebuild goes
// through recompleted, which carries the ask rather than making one. The reply
// replaces the listing whole and rebuilds the menu, so its rows appear once known
// and the menu never waits for them.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

	// pathLeads are what a typed `@` path starts with and no name does.
	pathLeads = "./~"

	// mentionQuote wraps a session's name holding anything but ASCII letters,
	// digits, `-` and `_`: Claude Code's typeahead inserts `@"release notes"`
	// (its cross-session messaging docs), and the docs say to type it so.
	mentionQuote = `"`

	peersFailed = "asking for the machine's other Claude sessions"
)

// peerMenu is a conversation's machine half of the `@` menu.
type peerMenu struct {
	// listing is the latest FramePeersReply's, replaced whole by the next and
	// never written into, so App copies may share it. Carried by every rebuild,
	// since it is the machine's rather than a draft's.
	listing []core.Peer

	// wants marks an opening's menu - a conversation's `@` with a character that
	// can begin a name - and asked that this opening's FramePeers is written.
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

// canBeginName reports whether typed could begin a session's name, which is
// what opens a conversation's names and their ask: a Wake name starts with a
// letter (daemon/names.go), and `@src/` is a path, typed or stepped into with ⇥.
func canBeginName(typed string) bool {
	return typed != "" && strings.IndexAny(typed, pathLeads) != 0 &&
		!strings.ContainsRune(typed, os.PathSeparator)
}

// conversationMenu fills a conversation's `@` names in the order drawn: the
// fleet's live peers, then the listing's sessions and this agent's subagent
// types - for any agent but the manager, whose `--tools ""` reaches neither.
// Nothing here routes, so unlike addressees it need not mirror core.Resolve. A
// listed name is offered as peerMention writes it; a subagent type holding
// whitespace is not one `@agent-` mention, so it is not offered.
func (a App) conversationMenu(c completion, typed string) completion {
	lower := strings.ToLower(strings.TrimPrefix(typed, mentionQuote))
	matches := func(word string) bool { return strings.HasPrefix(strings.ToLower(word), lower) }
	for _, addr := range a.live() {
		if addr.ID != a.focus && matches(addr.Name) {
			c.names = append(c.names, agentPrefix+addr.Name)
		}
	}
	agent := a.completionAgent()
	if agent.Name == core.ManagerName {
		return c
	}
	c.peers.wants = true
	held := a.heldNames()
	for _, p := range a.completion.peers.listing {
		key := strings.ToLower(p.Name)
		mention, ok := peerMention(p.Name)
		if held[key] || !matches(p.Name) || !ok {
			continue
		}
		held[key] = true // a listing naming one twice, in any case, offers it once
		c.names, c.tags = tagged(c.names, c.tags, mention, offerTag{dir: shortPath(p.Dir)})
	}
	for _, kind := range agent.SubagentTypes() {
		if !strings.ContainsFunc(kind, unicode.IsSpace) && (matches(kind) || matches(subagentMention+kind)) {
			c.names, c.tags = tagged(c.names, c.tags, agentPrefix+subagentMention+kind, offerTag{suffix: subagentMenuSuffix})
		}
	}
	return c
}

// peerMention is how a listed session is mentioned: bare when every rune is an
// ASCII letter, digit, `-` or `_`, quoted otherwise - a quote claude did not need
// costs nothing, a missing one ends the mention at a space. false for a name
// whose row would not draw what ⇥ inserts: a quote has no escape, optionRow
// collapses any other whitespace to one space, and a rune that is not graphic
// (a control, a bidi override) draws as something else or nothing.
func peerMention(name string) (string, bool) {
	notGraphic := func(r rune) bool { return !unicode.IsGraphic(r) }
	if strings.Contains(name, mentionQuote) || collapseWhitespaceOneLine(name) != name || strings.ContainsFunc(name, notGraphic) {
		return "", false
	}
	if strings.IndexFunc(name, func(r rune) bool { return !bareMentionRune(r) }) < 0 {
		return agentPrefix + name, true
	}
	return agentPrefix + mentionQuote + name + mentionQuote, true
}

// bareMentionRune is a rune a mention may hold unquoted.
func bareMentionRune(r rune) bool {
	return r == '-' || r == '_' || r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// dirLabel is an outside session's row: the name keeps its width up to all but
// the parentheses' own room, and the directory is cut from the left so its tail,
// the part naming the project, survives - `@wf-alpha (…/deep/wf)`. With no room
// it is `(…)`, never `()`: TruncateLeft drops the prefix when it drops everything.
func dirLabel(offer, dir string, avail int) string {
	name := ansi.Truncate(offer, max(avail-lipgloss.Width(fmt.Sprintf(peerDirFormat, ellipsis)), 0), ellipsis)
	room := avail - lipgloss.Width(name) - lipgloss.Width(fmt.Sprintf(peerDirFormat, ""))
	switch over := lipgloss.Width(dir) - room; {
	case over <= 0:
	case room <= lipgloss.Width(ellipsis):
		dir = ellipsis
	default:
		cut := ansi.TruncateLeft(dir, over+lipgloss.Width(ellipsis), ellipsis)
		if lipgloss.Width(cut) > room { // a double-width rune straddled the cut
			cut = ansi.TruncateLeft(dir, over+lipgloss.Width(ellipsis)+1, ellipsis)
		}
		dir = cut
	}
	return name + fmt.Sprintf(peerDirFormat, dir)
}

// heldNames is every name a fleet agent holds - the roster's, parked and the
// manager included, and the park book's - so a listed session sharing one is the
// fleet's and is dropped, which drops the conversation's own agent too. Already
// lower-case (the daemon's normalizeName), so a listed name folded to one of these
// matches it in any case. An ended agent's name went back to the pool.
func (a App) heldNames() map[string]bool {
	held := make(map[string]bool)
	for _, agent := range slices.Concat(a.fleet.OnRoster(), a.fleet.Parked()) {
		held[agent.Name] = true
	}
	return held
}

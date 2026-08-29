package ui

// The completion menu: what could finish the word somebody is typing.
//
// Three rulings, each of which is cheap to lose and expensive to rediscover.
//
// **It offers; it never routes.** The command set comes off every init frame
// (core.SessionFacts.SlashCommands) and so is per session and late, while a
// draft is judged per keystroke - slash.go's header is the whole argument. A
// menu may be wrong about a machine that has started nothing; a fence may not.
//
// **It is built per change, never per frame.** App.retarget's occasions, for
// App.retarget's reason and one sharper: the `@` half needs a directory read,
// which is why that half is a tea.Cmd - see completionpath.go. What is left per
// frame is a comparison against the pane and the draft it was built for, which
// is also what closes it.
//
// **`↑↓` stay the roster's and `↵` stays send.** ⇥ completes and ⌃N/⌃P walk,
// all three read above App.key's switch the way cardKey and pickerKey are - so
// they take no legend entry, and the menu advertises them on itself. The menu
// arrives while somebody types rather than because they asked, so it may not
// give the one irreversible key a second meaning.
//
// ⌃N/⌃P shadow the text area's own line keys, and that shadow is why the menu
// is scoped to the **cursor** rather than to the trailing token. ↑↓ are
// unconditionally the roster's, so ⌃N/⌃P are the only way to move between the
// lines of a multi-line draft - and a menu claimed by a `@` at the end of the
// buffer took them from every cursor position in it, which is a draft with no
// vertical movement at all. So a menu exists only while the cursor is at the
// end of the word it describes: it costs one space - which closes the menu -
// and it buys back both the arrows and the lines.

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// minCompletionRows is the floor under the menu's height. A pane one row
	// taller than it was given scrolls the alt screen away on every draw, and
	// 133 matching commands is a menu taller than any terminal.
	minCompletionRows = 8

	// moreFormat says what the bound left out. A bound that is silent reads as
	// a complete list, which is the failure a menu over 133 commands makes
	// expensive.
	moreFormat = "%d more - keep typing"

	// completionKeys is what the menu advertises about itself. It is the only
	// place these three are named: they are read above App.key's switch, so
	// legendEntries neither holds them nor could.
	completionKeys = "⇥ complete   ⌃N ⌃P walk"

	// wordBreak is what ends the word being typed. One spelling for both stems:
	// they were a literal space and a set of three, which agreed only because
	// nothing either of them offers has a tab in it.
	wordBreak = " \t\n"
)

// completionRows is the most offers drawn at once: the floor above, or the
// number of commands Wake owns, whichever is larger.
//
// **Derived rather than picked, because the two are not independent.** The bound
// is a layout fact and Wake's own verb count is a product fact. Wake's verbs now
// come *last* (commandMenu), so the bound no longer guarantees they are seen when
// a session advertises a menu's worth of its own - but it still guarantees they
// all fit when a session advertises none, which is the empty-menu floor and the
// case where the menu is only Wake's. A cut list is fine; a cut list that is
// nothing but a cut is not, which is why 133 matching commands is bounded at all.
var completionRows = max(minCompletionRows, len(wakeVerbs()))

// commandSet is one session's advertised commands, held behind a pointer so
// Agent stays comparable. Immutable: withFacts replaces it rather than
// appending, and nothing else writes one.
type commandSet struct{ names []string }

// words is what the session advertised, and nil for a session that has not
// said - which is every session before its first turn.
func (c *commandSet) words() []string {
	if c == nil {
		return nil
	}
	return c.names
}

// same reports whether this set is already exactly these words, which is what
// keeps an unchanging init from copying the whole fleet once per turn.
func (c *commandSet) same(names []string) bool {
	return c != nil && slices.Equal(c.names, names)
}

// completion is the menu under a draft.
type completion struct {
	// pane is the conversation this was built for, "" for the room. Two panes
	// holding the same characters are not holding the same menu: the offers are
	// relative to one session's own directory, and two repositories with a
	// README.md each would complete one from the other - silently, because that
	// reference resolves.
	pane string

	// draft is what this was built for. Compared with pane before the menu is
	// drawn or its keys are read, so a menu cannot outlive the word it
	// describes or follow the keys to a pane it is not about.
	draft string

	// head is the draft up to the token being completed, which an accept keeps.
	// Empty for a command, which is the whole draft by definition.
	head string

	// names is the half this goroutine can answer: the session's advertised
	// commands and skills, Wake's own commands, and the fleet's live names.
	names []string

	// paths is the `@` half, which is a directory read and so is not this
	// goroutine's. See completionpath.go.
	paths pathMenu

	// offers are what could finish the token, already bounded, in the order
	// they are drawn. more is how many matched beyond them.
	offers []string
	more   int

	cursor int
}

// open reports whether there is a menu at all. The zero value is closed, which
// is what makes "nothing to complete" the default state of an App.
func (c completion) open() bool { return len(c.offers) > 0 }

// recompleted rebuilds the menu for whatever the focused draft holds now.
//
// It sits beside retarget rather than inside it - the two answer the same
// "something was typed" occasion - and it is a separate call because retarget
// is reachable from the router's own command table, and a menu that read
// wakeVerbs from there would be an initialization cycle rather than a design.
//
// **Nothing closes the menu**, and nothing needs to: it remembers the pane and
// the draft it was built for, so a cleared or replaced draft - or a pane that
// no longer has the keys - leaves it undrawn and its keys unread on the next
// frame.
//
// It never starts a directory read - that is App.scanning, which the keystroke
// path calls and a fleet report does not. A report arrives per fleet event and
// cannot have moved the draft, so reading on one is a directory read per event
// at fleet size for a menu nobody touched. The listing is carried instead.
func (a App) recompleted() App {
	next := a.completing()
	next.paths = next.paths.carrying(a.completion.paths)
	a.completion = next.bounded()
	return a
}

// completing builds the menu for the focused draft. See this file's header for
// why it is not called from a draw.
func (a App) completing() completion {
	draft := a.composer().Value()
	if !a.composer().AtEnd() {
		// A cursor that is not at the end of the draft is not in the trailing
		// token, so there is no word being typed for a menu to be about - which
		// is what hands ⌃N/⌃P back to the text area. See this file's header for
		// why that matters more than it looks.
		return completion{pane: a.focus}
	}
	if head, word, ok := commandStem(draft); ok {
		return a.commandMenu(draft, head, word)
	}
	if head, who, ok := mentionStem(draft); ok {
		return a.mentionMenu(draft, head, who)
	}
	return completion{pane: a.focus}
}

// mentionStem is the `@…` at the end of a draft: everything before it, what
// follows the marker, and false for a draft whose last token is not one.
//
// The last token rather than the first, unlike a command: `@` addresses in the
// room and references a file everywhere, and both can follow prose. A draft
// ending in a space has no token being typed, which is how a finished mention
// takes its own menu down.
func mentionStem(draft string) (head, rest string, ok bool) {
	at := strings.LastIndexAny(draft, wordBreak) + 1
	rest, ok = strings.CutPrefix(draft[at:], agentPrefix)
	if !ok {
		return "", "", false
	}
	return draft[:at], rest, true
}

// mentionMenu is `@`, which is overloaded exactly as it is in Claude Code: a
// live session name wins, and anything else is a file path.
//
// **Names are offered in the room and nowhere else.** `@name` is Wake's
// routing, and the room is the only place it routes - a DM sends what was typed
// verbatim, so a name accepted there is one claude's own CLI reads as a file
// reference. Paths are offered in both, because that is what `@` means to the
// agent on the far side either way.
func (a App) mentionMenu(draft, head, typed string) completion {
	c := completion{pane: a.focus, draft: draft, head: head, paths: a.pathMenuFor(typed)}
	if a.focus == "" {
		c.names = a.addressees(typed)
	}
	return c
}

// addressees is every name a mention could resolve to, in the roster's own
// order, with the one that is not an agent last.
//
// The roster rather than the addressable set: these are the names on screen,
// which are the names somebody types. A parked one is included for that reason
// and refuses with a sentence naming `/resume`, which is more use than a name
// that is drawn and cannot be completed.
func (a App) addressees(typed string) []string {
	lower := strings.ToLower(typed)
	out := make([]string, 0, completionRows)
	for _, agent := range a.fleet.OnRoster() {
		if agent.Name != "" && strings.HasPrefix(strings.ToLower(agent.Name), lower) {
			out = append(out, agentPrefix+agent.Name)
		}
	}
	if strings.HasPrefix(core.BroadcastName, lower) {
		out = append(out, agentPrefix+core.BroadcastName)
	}
	return out
}

// commandMenu is the session's advertised commands and skills, and then Wake's
// own verbs.
//
// **The session's come first**, owner's 2026-08-28 override of the old "Wake
// first" ordering. An operator's skills ride in `slash_commands` - the list that
// is what `/` actually invokes - beside their `.claude/commands`
// (docs/superpowers/notes/2026-08-28-skills-in-slash-commands-findings.md), so
// Wake's twelve verbs filling the bound first was a bare `/` that never showed
// the operator's own Claude Code skills: they sat in the "N more" overflow,
// reachable only by typing past Wake's set. The session's come first so a bare
// slash surfaces them; Wake's verbs follow, so a bare `/` shows them only while
// the session advertises fewer than the bound - past that they are an overflow
// away, and narrowing the draft (`/re`) drops the advertised commands that do not
// share the prefix, so a verb returns once fewer than a menu's worth still match.
// This is what the lone-`@name` branch below has always done for the agent it
// addresses - now the rule for every path. Duplicates are dropped in this order
// too - `/effort`, `/model` and `/mcp` are words claude advertises and Wake
// claims, and two identical rows is a menu the operator cannot choose between.
func (a App) commandMenu(draft, head, word string) completion {
	typed := strings.ToLower(configureVerb(word))
	matched := make([]string, 0, completionRows+1)
	seen := make(map[string]bool, completionRows)
	add := func(verb string) {
		if seen[verb] || !strings.HasPrefix(strings.ToLower(verb), typed) {
			return
		}
		seen[verb] = true
		matched = append(matched, verb)
	}
	// A command right behind a resolved lone `@name` in the room is one the menu
	// completes for that agent, not Wake: addressedAgent reads the direct route, so
	// `@iris /command` completes iris's the way a configure command stays with iris
	// even in open mode. Wake's own fleet verbs are not iris's, and there are as
	// many of them as the bound - leaving them in pushes the agent's own, an
	// operator's custom skills, below the fold. That is the whole complaint.
	if agent, ok := a.mentionedAlone(head); ok {
		for _, name := range agent.advertised.words() {
			add(configureVerb(name))
		}
		return completion{pane: a.focus, draft: draft, head: head, names: matched}
	}
	for _, name := range a.completionAgent().advertised.words() {
		add(configureVerb(name))
	}
	for _, verb := range wakeVerbs() {
		add(verb)
	}
	return completion{pane: a.focus, draft: draft, head: head, names: matched}
}

// completionAgent is the session this draft would reach: the conversation
// holding the keys, the agent a leading @name addresses in the room, and
// otherwise whoever the roster cursor is on.
//
// One answer for both halves of the menu - a menu whose commands came from one
// session and whose paths came from another would be wrong in a way nothing on
// screen could show. In a conversation that is the pane's agent; in the room it
// is where ↵ would send, which is the addressed agent when a leading @name names
// one and the cursor ⌃D reads otherwise - mention.go's direct route read one
// surface over, since `@iris /command` configures iris. A command right behind a
// lone `@iris ` now takes commandMenu's own fast path (see mentionedAlone); this
// still resolves iris for a command with prose before it and for the `@` paths half.
func (a App) completionAgent() Agent {
	if a.focus != "" {
		agent, _ := a.fleet.Agent(a.focus)
		return agent
	}
	if agent, ok := a.addressedAgent(); ok {
		return agent
	}
	agent, _ := a.dmTarget()
	return agent
}

// addressedAgent is the one agent a room draft's leading @name reaches, resolved
// through the same router ↵ uses so the menu and the send cannot disagree about
// who @iris means. It reads the *direct* route - one agent even under open
// mention mode - because a menu is a knob and open mode widens a message, not a
// knob. Absent for a draft with no leading mention, which falls back to the
// roster cursor.
func (a App) addressedAgent() (Agent, bool) {
	c := a.room.Composer()
	r := a.route(c.WireText(c.Value()))
	if !r.mentioned {
		return Agent{}, false
	}
	targets := r.configureRoute().Targets
	if len(targets) == 0 {
		return Agent{}, false
	}
	return a.fleet.Agent(targets[0])
}

// mentionedAlone is the one agent a room command is addressed to when its head is
// a resolved leading `@name ` and nothing else before the command. It reuses
// addressedAgent's direct route, so it answers the same "who does `@iris /command`
// configure" the completion already asks. Absent in a DM, where a leading @name is
// a file reference rather than a route; when prose sits between the mention and
// the command, which is an ordinary message with a command-shaped word in it; and
// when the mention names nobody live.
func (a App) mentionedAlone(head string) (Agent, bool) {
	if a.focus != "" || !strings.HasPrefix(head, agentPrefix) {
		return Agent{}, false
	}
	i := strings.IndexAny(head, wordBreak)
	if i < 0 || strings.TrimLeft(head[i:], wordBreak) != "" {
		return Agent{}, false
	}
	return a.addressedAgent()
}

// bounded cuts what this menu offers to what a pane can hold and records what
// it left out.
//
// The names first and the paths under them, which is the overload stated as an
// order rather than as a rule: a live session name wins. It runs again when a
// directory read lands, so the cursor a walk left is clamped rather than reset -
// paths arriving under the names cannot move them.
func (c completion) bounded() completion {
	matched := append(slices.Clone(c.names), c.paths.rows()...)
	c.more = 0
	if len(matched) > completionRows {
		c.more = len(matched) - completionRows
		matched = matched[:completionRows]
	}
	c.offers = matched
	c.cursor = min(c.cursor, max(len(matched)-1, 0))
	return c
}

// completionUp reports whether the menu is on screen and answering keys: it has
// offers, and it still describes the pane and the draft it was built for.
//
// One predicate for all of it, because a menu drawn on one rule and keyed on
// another is the question card's bug one surface over - keys that answer
// something nobody can see.
func (a App) completionUp() bool {
	return a.completion.open() &&
		a.completion.pane == a.focus &&
		a.completion.draft == a.composer().Value()
}

// completionKey is ⇥ to complete and ⌃N/⌃P to walk, and nothing else.
//
// Read above App.key's switch, exactly as pickerKey is.
func (a App) completionKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.completionUp() {
		return a, nil, false
	}
	switch m.Type {
	case tea.KeyTab:
		next, scan := a.acceptCompletion()
		return next, scan, true
	case tea.KeyCtrlN:
		return a.walkCompletion(1), nil, true
	case tea.KeyCtrlP:
		return a.walkCompletion(-1), nil, true
	}
	return a, nil, false
}

// walkCompletion moves the cursor without rebuilding: the draft has not
// changed, and a rebuild would put the cursor back where it started.
//
// It clamps rather than wraps, which is the picker's ruling and its reason -
// with both directions on keys, an end that stops is an end a reader can feel.
func (a App) walkCompletion(by int) App {
	c := a.completion
	c.cursor = clamp(c.cursor+by, 0, len(c.offers)-1)
	a.completion = c
	return a
}

// acceptCompletion writes the cursored offer into the draft. Its one caller has
// already asked completionUp, and the cursor is clamped to the offers it walks,
// so there is no empty case here to guard.
//
// A directory gets no trailing space, because it is not finished - what is
// typed next goes inside it. Everything else does, which is what closes the
// menu: a word with a space after it is no longer a word being typed.
//
// Stepping into a directory is the one accept that needs a read, and it returns
// the command for it: this key is answered above App.key's switch, so Update's
// own keystroke path never runs.
func (a App) acceptCompletion() (App, tea.Cmd) {
	c := a.completion
	next := c.head + c.offers[c.cursor]
	if !strings.HasSuffix(next, string(os.PathSeparator)) {
		next += " "
	}
	return a.withComposer(a.composer().WithDraft(next)).retarget().recompleted().scanning()
}

// completionView is the menu if it belongs to this pane and still describes its
// draft, and "" otherwise. pickerView's shape and its reason.
func (a App) completionView(width int, id string) string {
	if a.focus != id || !a.completionUp() {
		return ""
	}
	return a.completion.View(width)
}

// View draws it, through the same rows a card and the picker draw.
func (c completion) View(width int) string {
	rows := make([]string, 0, len(c.offers)+1)
	for i, offer := range c.offers {
		rows = append(rows, optionRow(offer, width, i == c.cursor, false, CompletionStyle))
	}
	return strings.Join(append(rows, detailRow(c.keyLine(), width)), "\n")
}

// keyLine is the menu's own advertisement, with what the bound left out folded
// into it - two rows for that would be a row per menu spent on chrome.
func (c completion) keyLine() string {
	if c.more > 0 {
		return fmt.Sprintf(moreFormat, c.more) + hintSep + completionKeys
	}
	return completionKeys
}

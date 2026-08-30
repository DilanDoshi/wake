package ui

// Which pane has the keys, which conversation is beside the room, and what the
// two sidebars are showing. app.go owns the messages; this file owns the state
// they move.

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// noPaneAdvice is what a key that needs an agent says when there is not
	// one. Named rather than silent: a control that does nothing and says
	// nothing is the failure the legend rule exists for, arriving at runtime
	// instead of in the legend.
	noPaneAdvice = "no agent is selected. ↑↓ picks one, ⇧⇥ jumps to one that needs you, or click a row"

	// noOtherChat is ⇥ with nothing to move the keys to. It names both ways to
	// get a conversation open rather than only saying there is none, because
	// the reader's question at that moment is what to press next.
	noOtherChat = "nothing is open beside the room. ⌃D opens a conversation, ⇧⇥ jumps to one that needs you"

	// noPaneThatWay is a ⇧+arrow with no pane in that direction. It names ⇥
	// because that is the key that reaches a conversation which is not on
	// screen - the difference between the two is the whole reason both exist.
	noPaneThatWay = "no pane that way. ⇥ walks every conversation, ⌃Y opens one beside this"

	// noRowBelow is ⌃B from a pane that is already the bottom of its column.
	// A column splits once - spec §8 - so it names the key that still has
	// somewhere to put a conversation.
	noRowBelow = "this pane is already the bottom of its column. ⌃Y opens a conversation in a new column"

	// roomNeverCloses is ⌃W on the group chat, which is the one pane always
	// drawn. It says what the key is for rather than doing nothing.
	roomNeverCloses = "the room stays open. ⌃W closes a conversation - ⇥ moves the keys to one"
)

// dm is the focused conversation, or the zero DM when the room has the keys.
//
// A value rather than a pointer, like everything else here: a caller holding an
// older one keeps the transcript it had.
func (a App) dm() DM {
	if d := a.dms[a.focus]; d != nil {
		return *d
	}
	return DM{}
}

// composer is the input box of the pane that has the focus.
func (a App) composer() Composer {
	if a.focus != "" {
		return a.dm().Composer()
	}
	return a.room.Composer()
}

// withComposer writes one back to the pane that has the focus.
//
// Composers share a text area by pointer, so this is not what makes typing
// work - it is what makes the *rest* of a Composer, its target line, belong to
// the pane it was computed for.
func (a App) withComposer(c Composer) App {
	if a.focus != "" {
		return a.withDM(a.focus, a.dm().WithComposer(c))
	}
	a.room = a.room.WithComposer(c)
	return a
}

// refocusBlink revives the focused composer's caret when the keys have just
// moved to it. A focus change kills the previous pane's blink loop (see
// Composer.Refocus), and App.Update is the one place every focus path passes
// through on its way out - a click, ⇥, ⇧+arrow and opening a conversation all
// reach it. It does nothing when the focus did not move, which is every frame,
// keystroke and heartbeat: only an actual move may reset the caret to visible,
// or an idle pane would stop blinking.
func (a App) refocusBlink(before string) (App, tea.Cmd) {
	if a.focus == before {
		return a, nil
	}
	c, cmd := a.composer().Refocus()
	return a.withComposer(c), cmd
}

// composerEmpty reports whether the focused draft has nothing in it, which is
// the condition a card key is read under. Whitespace counts as empty for the
// reason submit does: it is not a message, so it is not a draft either.
func (a App) composerEmpty() bool { return blank(a.composer().Value()) }

// conversationID is which conversation the focused pane is, as a bang addresses
// one: a session id for a DM, and "" for the room, which is addressed to nobody
// in particular.
func (a App) conversationID() string { return a.focus }

// openDM opens a conversation with the agent the cursor is on.
//
// Which agent that is, in order: the roster's selection, and otherwise the
// first agent in attention order - which is the one a reader of the sidebar is
// looking at anyway, because Fleet.Agents ranks before it returns. A fleet with
// nobody in it opens nothing and says so.
func (a App) openDM() App {
	agent, ok := a.dmTarget()
	if !ok {
		notice.Report("%s", noPaneAdvice)
		return a
	}
	return a.openDMWith(agent.ID, agent.Name).viewingPicked(agent.ID)
}

// openPicked is the shared half of ⌃Y and ⌃B: they differ only in where the
// conversation lands, and they read the same cursor ⌃D does.
func (a App) openPicked(open func(App, string, string) App) App {
	agent, ok := a.dmTarget()
	if !ok {
		notice.Report("%s", noPaneAdvice)
		return a
	}
	return open(a, agent.ID, agent.Name).viewingPicked(agent.ID)
}

// dmTarget is whose DM ⌃D opens.
func (a App) dmTarget() (Agent, bool) {
	if a.roster.Selected != "" {
		if agent, ok := a.fleet.Agent(a.roster.Selected); ok {
			return agent, true
		}
	}
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return Agent{}, false
	}
	return agents[0], true
}

// pickAgent moves the cursor through the roster - which is what ⌃D opens, what
// ⌃C parks and what ⎋ interrupts.
//
// Roster.Move has existed since Phase 2 with nothing calling it, so the only
// ways to reach a conversation were ⌃D on whoever ranked first, ⇧⇥ to one that
// was blocked, and ⇥ around the ones already open. An agent that was none of
// those could not be opened from inside Wake at all.
//
// It opens the activity sidebar if it is closed, because a cursor nobody can
// see is worse than no cursor: that is how ⌃C came to park an agent the
// operator never chose - the selection was left behind by the last conversation
// they had open, on a sidebar that was not on screen.
func (a App) pickAgent(delta int) App {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		notice.Report("%s", noPaneAdvice)
		return a
	}
	if !a.layout.ShowRoster {
		a.layout.ShowRoster = true
		a = a.resizePanes()
	}
	a.roster = a.roster.Move(agents, a.fleet.RunningTasks, delta)
	return a
}

// pickedAgent is the agent the cursor is on *and the operator can see*: the
// activity sidebar is where the cursor is drawn, so with it closed there is
// nothing on screen saying who a key would be about.
//
// It is what ⌃C parks, what ⎋ interrupts and what ↵ opens. One definition,
// because three keys disagreeing about whose agent this is would be three
// different wrong answers.
func (a App) pickedAgent() (Agent, bool) {
	if !a.layout.ShowRoster || a.roster.Selected == "" {
		return Agent{}, false
	}
	return a.fleet.Agent(a.roster.Selected)
}

// clickedAgent is the roster row under a click, if the click was on one, and
// the dispatch under that agent if the click landed on one of its subagent rows.
func (a App) clickedAgent(y int) (Agent, string, bool) {
	r := a.regions()
	if r.Roster <= 0 {
		return Agent{}, "", false
	}
	return a.roster.At(a.fleet.OnRoster(), a.fleet.RunningTasks, r.Roster-dividerWidth, a.paneHeight(), y)
}

// openDMWith puts one agent's conversation beside the room and gives it the
// focus.
//
// A DM this App has already opened is re-shown rather than rebuilt: the whole
// reason dms is a map is that a conversation someone returns to still holds
// what it held.
func (a App) openDMWith(sessionID, name string) App {
	// Here means the focused pane, and the room is never replaced: from it the
	// conversation takes the column beside it, which is where a single DM has
	// always opened.
	return a.show(sessionID, name, func(g Grid) Grid {
		if a.focus != "" {
			return g.Replace(a.focus, sessionID)
		}
		if len(g.Cols) > 1 {
			return g.Replace(g.Cols[1].Top, sessionID)
		}
		return g.OpenRight("", sessionID)
	})
}

// openRight and openBelow are ⌃Y and ⌃B: the same open, placed beside the
// focused pane instead of into it.
//
// ⌃B refuses from a lower pane rather than quietly opening somewhere else -
// nothing sits under the bottom of a column, and a key that does something
// other than what it says is worse than one that declines. See Grid.CanOpenBelow.
func (a App) openRight(sessionID, name string) App {
	return a.show(sessionID, name, func(g Grid) Grid { return g.OpenRight(a.focus, sessionID) })
}

func (a App) openBelow(sessionID, name string) App {
	if _, ok := a.grid.CanOpenBelow(a.focus); !ok {
		notice.Report("%s", noRowBelow)
		return a
	}
	return a.show(sessionID, name, func(g Grid) Grid { return g.OpenBelow(a.focus, sessionID) })
}

// show puts a conversation on screen where `place` says and gives it the keys.
//
// The three open keys differ only in that placement; everything else here is
// what opening a conversation *is*, and it is one path so they cannot drift.
func (a App) show(sessionID, name string, place func(Grid) Grid) App {
	if sessionID == "" {
		return a
	}
	before := a.grid
	if _, ok := a.dms[sessionID]; !ok {
		a = a.withDM(sessionID, NewDM(sessionID, name))
	}
	if !slices.Contains(a.dmOrder, sessionID) {
		// The ring ⇥ walks, in the order a person opened things. Keyed on the
		// ring rather than on the map because closing takes a conversation out
		// of the ring and keeps its transcript, so "already have a DM for it" is
		// not the same question as "is it already in the ring".
		a.dmOrder = append(slices.Clone(a.dmOrder), sessionID)
	}
	a.grid = place(a.grid)
	if displaced, ok := displacedBy(before, a.grid, sessionID); ok {
		// The conversation that lost the slot is the one somebody is leaving,
		// and where they got to in it is what they will want when they come
		// back. Only a slot taken counts: a new column displaces nobody.
		a = a.withDM(displaced, a.dms[displaced].Leave())
	}
	// And the one taking it drops a boundary nothing arrived under. See
	// lastread.go.
	//
	// The parent is re-read on every open rather than only on the first, so a
	// conversation opened before its fork's report arrived picks the name up
	// the next time somebody looks at it.
	//
	// The dispatch rows are seeded here for the same trip: an agent may have
	// dispatched long before anybody opened its conversation, and chromeHeight
	// counts those rows the first time it is asked.
	//
	// And the agent, for a third reason on the same trip: a conversation opened
	// between two fleet reports would otherwise hold a zero Agent until the next
	// one, and DM.withBar's cache is keyed on the agent's own facts - so the
	// status bar would be re-rendered, filesystem walk included, on every frame
	// of that window. App.refreshedAgents keeps it current afterwards; this is
	// the seed. docs/notes/bugs.md BUG-5.
	a = a.withDM(sessionID, a.dms[sessionID].
		WithParentName(a.parentName(sessionID)).
		withAgent(a.fleet).
		Resume())
	// Reading a DM is reading its arrivals: Focus clears what accumulated for
	// that agent while somebody was elsewhere.
	a.fleet = a.fleet.Focus(sessionID)
	// A subagent the cursor named belongs to the agent it was under, so opening a
	// different one drops it: the pair Move and walkable cannot produce, and the
	// invariant the mouse path pairs by hand (mouse.go). Opening the *same* agent
	// keeps it, which is ⌃D toggling into the subagent the cursor is on.
	if a.roster.Selected != sessionID {
		a.roster.SelectedTask = ""
	}
	a.roster.Selected = sessionID
	// A conversation this client has not seen before comes back with what it
	// already said; Update writes the ask. See history.go.
	a = a.askHistory(sessionID)
	return a.withFocus(sessionID).resizePanes().retarget()
}

// displacedBy is the conversation that was on screen before and is not now,
// because `id` took its slot. Nothing is displaced when a column is added.
func displacedBy(before, after Grid, id string) (string, bool) {
	for _, was := range before.Panes() {
		if was != "" && was != id && !after.Has(was) {
			return was, true
		}
	}
	return "", false
}

// closeDM puts the room back across the whole pane.
//
// The conversation is kept rather than dropped. Closing a DM is a statement
// about the screen, not about the agent, and reopening one that had been
// emptied would be a scrollback silently thrown away.
// The room is not one of them: it is Cols[0] and the pane always drawn, so ⌃W
// on it names what the key is for rather than doing nothing.
func (a App) closeDM() App {
	if a.focus == "" {
		notice.Report("%s", roomNeverCloses)
		return a
	}
	return a.hideDM(true)
}

// hideDM takes the second pane away. leaveRing says whether the conversation
// also stops being one ⇥ walks through.
//
// The two are different facts and only one of them is about the ring. **The
// operator closed this** is ⌃W, and what you close is what you are done with -
// which is what keeps ⇥ short, since without it the ring only ever grows: ⇧⇥
// opens a conversation too, so at 15-30 agents it fills up on its own as they
// block, and ⇥ back to the room costs one press per conversation ever opened.
// **The layout had to close it in order to show the room** is showRoom below the
// takeover width, where the room is never drawn beside a conversation.
// Conflating the two meant every ⇥ that landed on the room permanently removed
// the conversation that had just had the keys - at 110 columns, an ordinary
// laptop, the focus key ate one ring member per lap, which is the mirror image
// of the hole the pruning was added to close.
func (a App) hideDM(leaveRing bool) App {
	if a.focus == "" {
		return a
	}
	id := a.focus
	// Closing is leaving: the transcript is kept, so where the reader got to in
	// it has to be kept as well or reopening lands them at the bottom of
	// whatever arrived meanwhile.
	a = a.withDM(id, a.dm().Leave())
	// Out of the ring, and only out of the ring: the transcript stays in dms for
	// when it comes back.
	if at := slices.Index(a.dmOrder, id); leaveRing && at >= 0 {
		a.dmOrder = slices.Delete(slices.Clone(a.dmOrder), at, at+1)
	}
	// The keys go to whatever takes the space, which is always a pane still
	// drawn. Read before the close, because afterwards the column is gone.
	next := a.grid.Neighbour(id)
	a.grid = a.grid.Close(id)
	a.fleet = a.fleet.Focus(next)
	return a.withFocus(next).resizePanes().retarget()
}

// chats is the ring ⇥ moves the keys around: the room, then every conversation
// that has been opened, in the order they were opened.
//
// The room is in the ring rather than on a key of its own, which is what lets
// one binding carry both jobs this task was handed - moving the focus between
// the two panes, and switching between the conversations open behind them. With
// one DM open it is a toggle; with three it is a cycle; and it is the only
// keyboard route to the room's composer, which is where @all is typed.
//
// "" is the room. Fixed order, because a ring that reorders under a repeated
// press revisits one conversation and skips another - which rules out attention
// rank, the tempting alternative, since it moves whenever an agent blocks.
func (a App) chats() []string { return append([]string{""}, a.dmOrder...) }

// nextChat moves the keys to the next conversation in the ring.
func (a App) nextChat() App {
	ring := a.chats()
	if len(ring) < 2 {
		notice.Report("%s", noOtherChat)
		return a
	}
	at := 0
	if a.focus != "" {
		// Not found reads as -1 and lands on the room, which is where a ring
		// that has lost track of itself should put somebody.
		at = max(slices.Index(ring, a.focus), 0)
	}
	next := ring[(at+1)%len(ring)]
	if a.grid.Has(next) {
		// Already drawn, so this is a focus change and nothing else. Opening it
		// again would move a pane somebody is looking at.
		a.fleet = a.fleet.Focus(next)
		return a.refocus(next).retarget()
	}
	return a.openDMWith(next, a.agentName(next))
}

// movePane moves the keys to the pane in a direction.
//
// The other half of ⇥, and bounded where ⇥ is not: this walks the panes that
// are *drawn* and opens nothing, so a direction with no pane in it says so
// rather than reaching into the ring for one. It does not wrap either - in a
// two-pane grid a wrapping ⇧← and ⇧→ are the same key.
//
// The focus change itself is nextChat's already-drawn branch, which is where
// the fleet's read mark and the room's target line get corrected.
func (a App) movePane(d Direction) App {
	next, ok := a.grid.Toward(a.focus, d)
	if !ok {
		notice.Report("%s", noPaneThatWay)
		return a
	}
	a.fleet = a.fleet.Focus(next)
	return a.refocus(next).retarget()
}

// showRoom moves the keys to the group chat.
//
// It needs no takeover branch of its own any more: the room is Cols[0] and the
// drawn window follows the focus, so focusing the room is what puts it back on
// screen at a width too narrow for two columns. The conversation it moved away
// from keeps its place in the grid and in the ring - ⇥ is moving the focus, not
// closing anything. See Layout.window and hideDM.
func (a App) showRoom() App { return a.refocus("") }

// refocus moves the keys and marks anything the move pushed off screen as left.
//
// Going off screen is a *drawing* outcome now rather than a grid change: below
// the takeover only the focused column is drawn, so ⇥ to the room can take a
// conversation off screen without touching the grid at all. The last-read
// boundary is anchored to leaving, so leaving has to mean "stopped being drawn"
// or the marker is silently lost at exactly the width where the conversation is
// the only pane there is. See lastread.go.
func (a App) refocus(next string) App {
	was := a.drawn()
	a = a.withFocus(next).resizePanes()
	now := a.drawn()
	for _, id := range was {
		if id != "" && !slices.Contains(now, id) {
			a = a.withDM(id, a.dms[id].Leave())
		}
	}
	return a
}

// drawn is every conversation that has a pane on screen - which is not every
// conversation in the grid, at a width or a height that cannot afford them all.
func (a App) drawn() []string {
	r := a.regions()
	out := make([]string, 0, len(r.Cols))
	for i, w := range r.Cols {
		if w == 0 {
			continue
		}
		c := a.grid.Cols[i]
		out = append(out, c.Top)
		if _, bottom := a.layout.SplitRowsIn(i, a.paneHeight()); c.Bottom != "" && bottom > 0 {
			out = append(out, c.Bottom)
		}
	}
	return out
}

// withFocus moves the keys to one pane and accents it.
//
// Every write to a.focus goes through here, so "which composer is accented" and
// "which composer takes a keystroke" cannot disagree - two boxes where only one
// is live and no way to tell which is the pane version of a legend naming a key
// nothing binds. Every pane is accented, not just the two that used to exist:
// with a grid the unfocused ones are the majority.
func (a App) withFocus(f string) App {
	if a.rewind.Session != "" && a.rewind.Session != f {
		// Hygiene rather than a second gate - rewindKey already refuses to
		// act on a picker whose Session is not a.focus. Without this, tabbing
		// back to the conversation it was opened for would silently
		// resurrect it rather than asking fresh. See rewind.go.
		a = a.closeRewind()
	}
	a.focus = f
	a.room = a.room.WithComposer(a.room.Composer().Focused(f == ""))
	for _, id := range a.grid.Panes() {
		if id == "" {
			continue
		}
		a = a.withDM(id, a.dms[id].WithComposer(a.dms[id].Composer().Focused(id == f)))
	}
	return a
}

// nextBlocked jumps to the next agent that needs you and opens its
// conversation - spec §6's "next agent", finally bound.
//
// Roster.Next selects nothing when nothing is blocked, which is deliberate
// there: a key that moves a cursor when there is nothing to move it to is a key
// that lies about the fleet. So this says so rather than opening whichever
// conversation the cursor happened to be resting on.
func (a App) nextBlocked() App {
	a.roster = a.roster.Next(a.fleet.Agents())
	if a.roster.Selected == "" {
		notice.Report("nothing is waiting on you")
		return a
	}
	agent, ok := a.fleet.Agent(a.roster.Selected)
	if !ok {
		return a
	}
	return a.openDMWith(agent.ID, agent.Name)
}

// toggleGroups and toggleRoster open and close the sidebars.
//
// Applied immediately rather than through the settle: a keystroke is one change
// and a drag is hundreds, so the debounce that makes a drag affordable would
// only make a keypress feel broken.
//
//nolint:unused // left workspaces sidebar hidden for now; restored with the multi-groupchat version, when ⌃G rebinds to this. See groups.go.
func (a App) toggleGroups() App {
	a.layout.ShowGroups = !a.layout.ShowGroups
	return a.resizePanes()
}

func (a App) toggleRoster() App {
	a.layout.ShowRoster = !a.layout.ShowRoster
	return a.resizePanes()
}

// scrollFocused moves the reader of the focused pane, forwards for a negative
// count.
func (a App) scrollFocused(lines int) App {
	if a.focus != "" {
		return a.withDM(a.focus, a.dm().ScrollUp(lines))
	}
	a.room = a.room.ScrollUp(lines)
	return a
}

// endedAgent reports whether there is anything at the other end for this
// session.
//
// Two sources, and both are needed. The fleet is the general answer and covers
// every agent in the room; a.ended is this client's own session, latched from
// the push that announced it, which is what stays true if that agent later
// falls out of a report.
func (a App) endedAgent(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	if sessionID == a.sessionID && a.ended {
		return true
	}
	agent, ok := a.fleet.Agent(sessionID)
	return ok && agent.State == rpc.StateEnded
}

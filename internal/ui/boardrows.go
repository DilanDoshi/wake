package ui

// The row view's geometry: one block per agent - its team's header band when it
// opens a section, its own row, then a row per running subagent - stacked into
// one list the window scrolls over. One layout is computed per frame and read by
// the draw, the click and the cursor, so they cannot disagree on where a line is.

// rowBlock is one agent's lines in the list: top is the first of them, its team
// header band (hdr lines) included.
type rowBlock struct {
	top, hdr int
	subs     []Task
}

func (b rowBlock) height() int { return b.hdr + 1 + len(b.subs) }

// rowLayout is the list's blocks, the stops the cursor can rest on (the
// sidebars' own walkable) and the bands they are drawn in.
type rowLayout struct {
	viewport
	agents []Agent
	blocks []rowBlock
	stops  []Roster
}

// newRowLayout lays agents out `view` lines tall and scrolled to `scroll`, the
// selection `at`.
func newRowLayout(agents []Agent, subs subsOf, view, scroll int, at Roster) rowLayout {
	l := rowLayout{agents: agents, stops: walkable(agents, subs)}
	l.view = view
	line := 0
	for i, ag := range agents {
		b := rowBlock{top: line, subs: subsFor(subs, ag.ID)}
		if teamHeaderAt(agents, i) != "" {
			b.hdr = teamHeaderRows
		}
		l.blocks = append(l.blocks, b)
		// The same order walkable lists its stops in: the agent, then each selectable
		// dispatch under it.
		l.bands = append(l.bands, band{top: line + b.hdr, h: 1, lead: b.hdr})
		for k, t := range b.subs {
			if t.Selectable() {
				l.bands = append(l.bands, band{top: line + b.hdr + 1 + k, h: 1})
			}
		}
		line += b.height()
	}
	l.total = line
	l.cur = cursorStop(l.stops, at)
	l.viewport = l.settled(scroll)
	return l
}

// cursorStop is where the selection sits among the stops: its own row, else its
// agent's when the dispatch it named has finished, else the top - Roster.Move's
// own recovery. -1 for no stops.
func cursorStop(stops []Roster, at Roster) int {
	if len(stops) == 0 {
		return -1
	}
	i := stopIndex(stops, at)
	if i < 0 && at.SelectedTask != "" {
		i = stopIndex(stops, Roster{Selected: at.Selected})
	}
	return max(i, 0)
}

func (a App) boardRowLayout(agents []Agent) rowLayout {
	return newRowLayout(agents, a.fleet.RunningTasks, a.boardRowsVisible(), a.board.Scroll,
		Roster{Selected: a.board.Selected, SelectedTask: a.board.SelectedTask})
}

// shown is the blocks the window touches, as a half-open run of agent indices.
func (l rowLayout) shown() (from, to int) {
	for i, b := range l.blocks {
		if b.top+b.height() > l.off && b.top < l.off+l.view {
			if to == 0 { // the first block the window touches
				from = i
			}
			to = i + 1
		}
	}
	return from, to
}

func (l rowLayout) visibleAgents() []Agent {
	from, to := l.shown()
	return l.agents[from:to]
}

// selectBand puts the selection on a stop.
func (l rowLayout) selectBand(a App, i int) App {
	a.board.Selected, a.board.SelectedTask = l.stops[i].Selected, l.stops[i].SelectedTask
	return a
}

// endpoint is the list's first or last stop: a subagent row when the last agent
// has one running.
func (l rowLayout) endpoint(last bool) (agent, task string) {
	stop := l.stops[0]
	if last {
		stop = l.stops[len(l.stops)-1]
	}
	return stop.Selected, stop.SelectedTask
}

// hit is the agent (by index) and running dispatch the list's line `abs` falls on,
// or -1 for a team header band.
func (l rowLayout) hit(abs int) (int, string) {
	from, to := l.shown()
	for i := from; i < to; i++ {
		b := l.blocks[i]
		if abs >= b.top+b.height() {
			continue
		}
		within := abs - b.top
		if within < b.hdr {
			return -1, "" // the team header band opens nothing
		}
		within -= b.hdr
		if within > 0 && b.subs[within-1].Selectable() {
			return i, b.subs[within-1].Dispatch
		}
		return i, ""
	}
	return -1, ""
}

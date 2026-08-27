package ui

// The one ticker in Wake, and the whole of what makes an animated status line
// affordable next to thirty agents.
//
// The first non-negotiable is that Wake is cheap to leave open: no work per
// frame that could be work per change, and no process on a timer. A shimmer is
// work per frame by definition, so the cost is bounded the only two ways left.
//
//   - One ticker for the fleet, never one per agent. Thirty working agents
//     schedule exactly as many wake-ups as one does.
//   - It stops dead. The tick is rescheduled only while some agent is actually
//     working, so an idle Wake - which is most of them, most of the time -
//     schedules nothing at all and costs what it did before this existed.
//
// The animation reads wall-clock elapsed rather than counting ticks, so the
// tick rate decides only how smooth the line looks, never where the sweep is.
// A dropped or late tick loses a frame; it never desynchronises anything.

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// heartbeatMsg is one redraw of every working agent's line.
type heartbeatMsg struct{}

// heartbeatTimer is the seam the tick is scheduled through, for the same
// reason settleTimer is one: a test that hand-delivers the message never runs
// the command, so it cannot see whether the loop stops when the fleet goes
// quiet - which is the only property here worth guarding.
var heartbeatTimer = tea.Tick

// clock is time.Now behind a seam, so a test can hold a turn at a chosen age.
var clock = time.Now

// beat schedules the next redraw, or lets the loop end.
//
// Idempotent while one is already in flight: every status frame calls this, and
// a fleet of thirty announcing themselves would otherwise start thirty
// concurrent tickers that never stop.
func (a App) beat() (App, tea.Cmd) {
	if a.beating || !a.fleet.anyWorking() {
		return a, nil
	}
	a.beating = true
	return a, heartbeatTimer(shimmerStep, func(time.Time) tea.Msg { return heartbeatMsg{} })
}

// beatArrived is one tick landing: the frame is already drawn by the time this
// runs, so all that is left is deciding whether there is another.
func (a App) beatArrived() (App, tea.Cmd) {
	a.beating = false
	return a.beat()
}

// anyWorking reports whether a single agent has a turn in flight. It is what
// the ticker asks before scheduling, so it must stay a scan of a map rather
// than anything that touches a process.
func (f Fleet) anyWorking() bool {
	for _, a := range f.agents {
		if a.State == rpc.StateWorking {
			return true
		}
	}
	return false
}

// turnAge is how long a turn in state that began at started has been running,
// and 0 for one that is not working. A turn whose start was never stamped - an
// agent already working when this client attached - reads as having just begun
// rather than as having run since the zero time.
func turnAge(state string, started time.Time) time.Duration {
	if state != rpc.StateWorking || started.IsZero() {
		return 0
	}
	return clock().Sub(started)
}

// roomMoreFormat counts the working agents the room's one row does not name.
const roomMoreFormat = metaSep + "+%d more working"

// roomWorkingLine is the same line for a surface that has many agents on it:
// the oldest running turn, named, with the rest as a count.
//
// **One row or none, at every fleet size.** A row per working agent is thirty
// rows taken from the transcript at fleet size, and a block of rows that comes
// and goes changes a pane's height at an arbitrary moment - which is the
// alt-screen failure DM.chrome exists for, where a frame one row taller than
// the pane scrolls the screen away on every draw.
//
// **Every figure on the row belongs to one agent.** A fleet-summed token total
// beside a longest-of-several age would be two agents' numbers in one sentence,
// readable as neither. The oldest turn is the one worth naming - it is the one
// that has been running long enough to wonder about - and the count beside it
// is a plain fact.
//
// It is not the awareness strip's job: the strip counts states and answers
// "should I stop what I am doing", where this answers "how long has it been and
// how much has come back". Same line as the conversation pane's, reached
// through the same function.
func roomWorkingLine(agents []Agent, width int) string {
	var oldest Agent
	working := 0
	for _, a := range agents {
		if a.State != rpc.StateWorking {
			continue
		}
		working++
		if oldest.ID == "" || a.startedAt.Before(oldest.startedAt) {
			oldest = a
		}
	}
	if working == 0 {
		return ""
	}

	more := ""
	if working > 1 {
		more = fmt.Sprintf(roomMoreFormat, working-1)
	}
	// The name is the room's own addition: a group chat has to say whose turn
	// it is, where a conversation pane names its agent in its own header. The
	// line itself is roomHeartbeatLine's minimal `✻ Sailed for 49s` rather than
	// the DM's fuller form - the room is the glance, so it drops the tokens.
	head := agentPrefix + oldest.Name + " "
	line := HintStyle.Render(head) + roomHeartbeatLine(
		roomWorkingWord(oldest.ID, oldest.Doing, oldest.startedAt),
		turnAge(oldest.State, oldest.startedAt),
		max(width-ansi.StringWidth(head)-ansi.StringWidth(more), 1),
	)
	return ansi.Truncate(line+HintStyle.Render(more), width, ellipsis)
}

// workingLine is the heartbeat drawn for an agent with a turn in flight, and ""
// for one without.
//
// A function rather than a method on either holder: the conversation pane draws
// this, the room draws it, and the fleet's own record is where the times come
// from - a copy on each would be renderings of the same line free to disagree.
// The word is held for the whole turn by seeding on the turn's start.
//
// tokens is the *turn's* figure rather than the session's, which is what the
// caller passes and what Claude Code's own bar shows. See Agent.TurnTokens.
func workingLine(id, state, doing string, started time.Time, tokens, width int) string {
	if state != rpc.StateWorking {
		return ""
	}
	// The agent's own word for what it is doing, when it has written one. This
	// is claude's activeForm and claude puts it on exactly this line; the pool
	// is the fallback for the ordinary agent that keeps no task list.
	word := doing
	if word == "" {
		word = heartbeatWord(turnSeed(id, started))
	}
	return heartbeatLine(word, turnAge(state, started), tokens, width)
}

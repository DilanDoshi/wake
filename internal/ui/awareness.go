package ui

// The awareness strip: the fleet in one row, in every view.
//
// Spec §8 asks for it "in every view including solo, so you can disappear into
// a DM for an hour without losing the fleet". That is the whole argument: the
// roster answers the same question and is the first thing a narrow window drops,
// and a conversation you are deep in is exactly where you stop looking at it.
//
// It costs a row of pane height, reserved unconditionally for the notice row's
// reason - a row taken and given back would change the panes' height at an
// arbitrary moment, and SetSize returns a reader to the newest line, so the
// fleet going quiet would silently end somebody's scrollback.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// stripHeight is the one row App reserves for it.
	stripHeight = 1

	// stripSep is what goes between the workspace and each count.
	stripSep = " · "

	// stripIndent matches the notice row and the hint line, so the three rows
	// below the panes start in the same column.
	stripIndent = " "
)

// stateLabel is what each state is called when it is counted.
//
// A label per state and a state per label, derived from stateGlyph by
// TestEveryStateTheRosterDrawsIsCountedInTheStrip - so a seventh state is a
// build failure here rather than a fleet silently under-reported by the one row
// that claims to be the whole fleet.
//
// "need you" rather than "blocked", because the strip is read at a glance and
// the question it answers is whether to stop what you are doing.
var stateLabel = map[string]string{
	rpc.StateBlocked:  "need you",
	rpc.StateWorking:  "working",
	rpc.StateIdle:     "idle",
	rpc.StateSilent:   "silent",
	rpc.StateParked:   "parked",
	rpc.StateEnded:    "ended",
	rpc.StateOrphaned: "orphaned",
}

// awarenessStrip is the fleet in one row: where you are, and what everyone is
// doing.
//
// A segment per state that has anybody in it, in the roster's own order, so the
// thing worth stopping for is leftmost. States with nobody in them are left out
// rather than drawn as zeros: a row of "0 need you · 0 silent · 0 ended" is
// four words of chrome around the one number that matters.
func awarenessStrip(agents []Agent, workspace string, width int) string {
	counts := map[string]int{}
	for _, a := range agents {
		counts[a.State]++
	}
	segs := make([]string, 0, len(counts))
	for _, state := range statesByAttention(counts) {
		segs = append(segs, glyphOf(state)+" "+strconv.Itoa(counts[state])+" "+labelOf(state))
	}
	body := strings.Join(segs, stripSep)
	if body == "" {
		body = noFleet
	}
	// One line, whatever is in it. labelOf falls back to the state itself for
	// one this build does not know, and that arrives off the wire - a newline
	// there is a frame a row taller than the terminal, on every draw.
	body = oneLine(body)

	room := max(width-len(stripIndent), 1)

	// **The counts are budgeted before the name is cut**, which is Roster's own
	// rule read across: a row truncated from the right cuts through the numbers
	// it exists to show, and `◐ 1` where seventeen are working is not
	// information missing - it is a plausible wrong number, on the surface built
	// to be read at a glance, at exactly the widths this exists for.
	row := stripIndent + HintStyle.Render(ansi.Truncate(body, room, ""))
	if workspace != "" {
		name := "#" + oneLine(workspace)
		// Below minWorkspaceWidth there is no room for a name worth reading, and
		// it is dropped whole rather than cut to a stub: the conversation's own
		// pane names it, and the counts do not.
		if fits := room - ansi.StringWidth(body) - len(stripSep); fits >= minWorkspaceWidth {
			// Styled in two spans rather than one wrapping the other: an inner
			// style's reset closes the outer colour, so everything after the
			// accent rendered in the terminal's default foreground - the strip
			// was a different colour in a conversation than in the room. Same
			// failure MentionStyle already records.
			row = stripIndent + AccentStyle.Render(ansi.Truncate(name, fits, "…")) + HintStyle.Render(stripSep+body)
		}
	}
	// Padded to the frame rather than only clipped to it: the grid asserts every
	// row is the terminal's width, and one short row makes the whole frame ragged.
	return row + strings.Repeat(" ", max(width-ansi.StringWidth(row), 0))
}

// minWorkspaceWidth is the narrowest name worth drawing: a `#`, two characters
// and the ellipsis that says there was more.
const minWorkspaceWidth = 4

// noFleet is a strip with nothing to say, which is a real state: bare `wake` on
// a machine with nothing running draws one before its first agent answers.
const noFleet = "no agents"

// statesByAttention is the states anybody is in, ranked the way the roster
// ranks agents - so the strip and the sidebar agree about what matters first.
//
// Through rankOf rather than a second ordering: two answers to "what is most
// important" is exactly the disagreement between two surfaces that a glance is
// supposed to resolve.
func statesByAttention(counts map[string]int) []string {
	// Every key, because counts only ever gains one by counting an agent into
	// it - a state nobody is in is absent rather than zero, so a `> 0` filter
	// here would be a guard over a value that cannot arrive.
	out := make([]string, 0, len(counts))
	for state := range counts {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		ri, rj := rankOf(out[i]), rankOf(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// labelOf is a state's word, or the state itself for one this build does not
// know - the same trade glyphOf makes, and for the same reason: a count with no
// word reads as a fleet with a hole in it.
func labelOf(state string) string {
	if l, ok := stateLabel[state]; ok {
		return l
	}
	return state
}

// stripWorkspace is which workspace the strip names: the one the focused
// conversation is in, and nothing in the room.
//
// Nothing rather than a guess, for the room's own reason: the room is not one
// workspace, and naming whichever agent happens to rank first would be the
// misroute Route.Resolved exists to prevent, arriving in a status bar.
func (a App) stripWorkspace() string {
	if a.focus == "" {
		return ""
	}
	agent, ok := a.fleet.Agent(a.focus)
	if !ok || agent.Cwd == "" {
		return ""
	}
	return workspaceName(agent.Cwd)
}

package ui

// The prompt history: what was typed into this pane, walked with ↑↓.
//
// # Why the bare arrows
//
// ↑↓ recall the previous prompt in Claude Code, so a hand arriving with that
// reflex gets the same thing here: on an empty or single-line draft ↑ walks
// back and ↓ walks forward, and the roster moved to ⇧↑↓ (keys.go). On a
// multi-line draft the arrow moves the text cursor instead - the recall is
// only offered when the cursor has nowhere to climb, Composer.CanCursorUp's
// job. ⌥↑↓ carry no binding of their own: bubbletea reports ⌥↑ as tea.KeyUp
// with Alt set and App.key's switch is on m.Type alone, so a ⌥ arrow behaves
// exactly as the bare one does.
//
// # Where the history comes from
//
// Derived, never stored. A conversation already holds its own user turns, and a
// conversation this window has never opened is filled from claude's transcript
// by internal/daemon/history.go before it draws - so the walk works on a
// reattach and on a session started by somebody else, without Wake keeping a
// prompt file of its own to go stale against the fleet.
//
// **The room's history is what was typed into the room**, which is the room's
// own echo - one line per thing said, mention included. The room is not one
// conversation and has no transcript of its own, and its agents' prompts were
// typed at other composers. What a history key means at a box is "what I typed
// here", and sendRoom's echo is the record of that. It is derived from what the
// room *holds* rather than from the echo path, so a room prefix restored from
// the fleet's own transcripts carries a history with it.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// promptWindow is how far back a walk looks: the newest events of the
	// conversation, rather than all of them. A 4,000-event conversation would
	// otherwise copy every event on every press of a key that can reach a few
	// dozen prompts.
	promptWindow = 400

	// noPromptHistory is a walk with nothing behind it. Named rather than
	// silent, for noPaneAdvice's reason.
	noPromptHistory = "nothing has been typed here yet, so ↑ has no prompt to bring back"
)

// promptWalk is where a walk has got to, and the draft it started from.
//
// at counts back from the newest prompt: 0 is the live draft, 1 the newest
// thing typed, len(prompts) the oldest. held is what was in the box when the
// walk began, so ↓ off the end of it returns a half-written message rather
// than an empty box.
type promptWalk struct {
	at   int
	held string
}

// walkPrompts moves one step through the focused pane's history: back for ↑,
// forward for ↓.
func (a App) walkPrompts(back int) (tea.Model, tea.Cmd, bool) {
	prompts := a.prompts()
	if len(prompts) == 0 {
		notice.Report("%s", noPromptHistory)
		return a, nil, true
	}
	c, moved := a.composer().walked(prompts, back)
	if !moved {
		// Both ends of the walk hold rather than wrapping or clearing: the
		// oldest prompt stays, and the live draft stays.
		return a, nil, true
	}
	return a.withComposer(c), nil, true
}

// prompts is the history of the pane that has the keys, oldest last.
func (a App) prompts() []string {
	if a.focus != "" {
		return a.dm().prompts()
	}
	return a.room.prompts()
}

// prompts is what the operator typed into this conversation, including turns
// routed to it from the room - those are their words to this agent, and
// core.Event.FromRoom is presentation only and may not be keyed on.
func (d DM) prompts() []string {
	n := d.events.len()
	return promptsIn(d.events.slice(max(0, n-promptWindow), n))
}

// prompts is what was typed into the room: its own echo of each message sent.
func (r Room) prompts() []string {
	n := r.said.len()
	said := r.said.slice(max(0, n-promptWindow), n)
	evs := make([]core.Event, len(said))
	for i, l := range said {
		evs[i] = l.ev
	}
	return promptsIn(evs)
}

// promptsIn is the user turns among these events, in the order they were typed.
func promptsIn(evs []core.Event) []string {
	var out []string
	for _, ev := range evs {
		if ev.Kind != core.KindUserText || !typedByHand(ev) {
			continue
		}
		out = append(out, ev.Text)
	}
	return out
}

// typedByHand reports whether a user frame is the operator speaking.
//
// A frame the airlock resolved to a **notice** is Claude's own English about
// Wake's action - the abort marker, whose text is the only thing identifying it
// - and one carrying a **subagent** is the prompt an agent handed one. An
// **echoed** frame is content the transcript replayed or the tooling generated:
// a bang line's output, an /mcp panel, a compaction summary. None was typed by
// a person, and userBlock draws all three as something other than the operator
// speaking - Echoed was the case this predicate was missing, so `↑` pulled a
// compaction summary into the draft where `↵` sends it back to the model.
//
// **Echoed may be read here and not everywhere**, which is core.Event.Echoed's
// own ruling: it is safe where being wrong is cosmetic and unsafe as
// suppression or de-duplication. This is the safe direction - a wrongly echoed
// turn costs a recall somebody can retype, and nothing here decides whether a
// turn is *drawn*, only whether a key can bring it back.
//
// One predicate, two callers: this and fleet.go's fold, which decides what the
// room draws. They were the same conditions written twice, and the room showing
// a line the history would not recall is two answers to one question.
func typedByHand(ev core.Event) bool {
	return ev.Subagent == nil && ev.Notice == "" && !ev.Echoed && !blank(ev.Text)
}

// walked returns this composer one step along the walk, and whether it moved.
//
// The draft is replaced through WithDraft, which clears the walk - the picker
// hands back a half-written command that way and a walk over it would carry a
// position that means nothing - so the position is written back after it.
//
// The position is an index into a list derived per press, so a turn arriving
// mid-walk shifts it by one. That is the cheap wrong answer rather than a
// second copy of the history kept to be right about it.
func (c Composer) walked(prompts []string, back int) (Composer, bool) {
	at := c.hist.at + back
	if at < 0 || at > len(prompts) {
		return c, false
	}
	held := c.hist.held
	if c.hist.at == 0 {
		held = c.Value()
	}
	text := held
	if at > 0 {
		text = prompts[len(prompts)-at]
	}
	c = c.WithDraft(text)
	c.hist = promptWalk{at: at, held: held}
	return c, true
}

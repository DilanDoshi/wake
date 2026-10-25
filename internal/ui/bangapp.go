package ui

// How a bang reaches a conversation and its agent, and how the answer gets back.
//
// This is the only part of the feature that knows what a conversation is, which
// is why it is its own file: bang.go runs a command and knows nothing about
// sessions, and a window that holds many DMs and a room is where a result has
// to find the one it was typed into.
//
// Three call sites in App: App.submit, which asks before it treats a draft as a
// message; sendRoom, which aims `@who !cmd` at one agent (roomBang); and App.Update, which
// folds bangResultMsg rather than letting it fall through to the composer.

import (
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// bangNotSent says a bang ran and was drawn but its agent could not take it.
	// It names what happened, and never claims a send.
	bangNotSent = "!command ran here but did not reach %s: it %s"

	// bangOneAgent refuses a bang aimed at a set: it runs in one directory and
	// goes to one agent.
	bangOneAgent = "a !command runs in one agent's directory and goes to that agent - aim it at one @name, not %s"
)

// bangGone is the words for each state an agent cannot take a bang in.
var bangGone = map[string]string{
	rpc.StateEnded:  "has ended",
	rpc.StateParked: "is parked",
}

// bang takes over a submission that is a local command, and reports whether it
// took it.
//
// The draft is cleared on dispatch rather than on completion, which is the
// difference between a composer and a modal: a command can take thirty seconds
// and the operator is not made to watch it. What they type next is theirs, and
// bangResult must not walk over it - see the note there about copies.
//
// A session that has ended is not a reason to refuse: `!git status` is as
// answerable in a conversation whose agent is gone, and a window somebody is
// reading after a crash is where that question gets asked. It runs, is drawn,
// and bangResult says it could not be sent.
func (a App) bang(text string) (App, tea.Cmd, bool) {
	cmdline, ok := isBang(text)
	if !ok {
		return a, nil, false
	}
	id := a.conversationID()
	a = a.clearDraft()
	return a, runBang(id, id, a.bangDir(id), cmdline, bangTimeout), true
}

// roomBang runs a bang the room's route aimed with a leading mention: at one
// agent, in its directory; refused at a team or @all, live members or none,
// before anything runs. A draft whose mention no route resolved - a path, a
// session outside the fleet - is no bang, and passes through as typed.
func (a App) roomBang(r roomRoute, wire string) (tea.Model, tea.Cmd, bool) {
	cmdline, ok := isBang(r.Text)
	if !ok || r.Text == wire { // no mention stripped: a draft that is itself a bang ran in submit
		return a, nil, false
	}
	if r.Team != "" || r.Broadcast || len(r.Targets) != 1 {
		notice.Report(bangOneAgent, agentPrefix+r.Resolved)
		return a, nil, true
	}
	to := r.Targets[0]
	a = a.clearDraft()
	return a, runBang("", to, a.bangDir(to), cmdline, bangTimeout), true
}

// bangRoute is App.route's answer for `@name !cmd` where core resolved no live
// agent: a parked fleet agent, which still has a directory - the bang runs there
// and says it was not sent - or a team with no live member, which is refused like
// any team. ok is false for anything else, so every other draft routes as before.
func (a App) bangRoute(r core.Route, text string) (roomRoute, bool) {
	name, rest, ok := mentionBang(text)
	if !ok || r.Text != text {
		return roomRoute{}, false
	}
	if agent, known := a.fleet.ByName(name); known {
		return roomRoute{Route: core.Route{Targets: []string{agent.ID}, Text: rest, Resolved: agent.Name}, mentioned: true, mode: MentionDirect}, true
	}
	team := strings.ToLower(name)
	if slices.ContainsFunc(a.fleet.Agents(), func(agent Agent) bool { return agent.Team == team }) {
		return roomRoute{Route: core.Route{Text: rest, Team: team, Resolved: team}, mode: a.mention}, true
	}
	return roomRoute{}, false
}

// mentionBang splits a draft `@name !cmd` into the name and the bang after it,
// as core.Resolve splits a leading mention: at the first space of any kind.
func mentionBang(text string) (string, string, bool) {
	body, ok := strings.CutPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), agentPrefix)
	i := strings.IndexFunc(body, unicode.IsSpace)
	if !ok || i <= 0 {
		return "", "", false
	}
	rest := strings.TrimLeftFunc(body[i:], unicode.IsSpace)
	_, ok = isBang(rest)
	return body[:i], rest, ok
}

// newShellSend stamps a bang's record with where it was typed: a room send to
// one agent when the room aimed it, a conversation's otherwise. claude's started
// for it places the turn it opens (shellStarted), and a restore reads it so.
func newShellSend(aimed bool) string {
	flags := rpc.MessageShell
	if aimed {
		flags |= rpc.MessageDirect | rpc.MessageSolo
	}
	return rpc.NewMessageSend(flags).ID()
}

// shellStarted marks the turn a bang opened as the conversation's or the room's,
// as echoSent marks a typed message's: read off the stamp, since a bang is in no
// queue, and asked as claude takes it up - idle, mid-turn, or after a busy turn.
func (a App) shellStarted(id, msgID string) App {
	if _, flags, ok := rpc.MessageOf(msgID); ok && flags&rpc.MessageShell != 0 {
		a.fleet = a.fleet.sending(id, flags&rpc.MessageDirect == 0)
	}
	return a
}

// bangResult puts a finished command into the conversation it was typed into,
// and into the conversation of the agent it was aimed at, then sends it there.
//
// Addressed, and checked, for the reason apply attributes a frame: a result
// that landed in whichever pane happened to be on screen would be an answer
// about one repository shown under another.
//
// It appends through the conversation the App is holding *now*, not one
// captured when the command started. Composers share a text area by pointer and
// a DM is a value: folding into a stale copy would put the transcript back to
// what it was thirty seconds ago and take whatever was typed in the meantime
// with it.
func (a App) bangResult(m bangResultMsg) (App, tea.Cmd) {
	ev := bangEvent(m)
	if m.ID == "" {
		// Typed into the room: back there as what was said - `@who !cmd`, in
		// who's thread, then what it printed - drawn as the operator's own line,
		// whose lines the room keeps as they are. Unfenced: the room draws no
		// markdown there, so a fence would show as backticks.
		line := ev
		line.Text = strings.TrimRight(bangPrefix+m.Cmd+"\n"+m.Text, "\n")
		if m.To != "" {
			line.Text = agentPrefix + a.agentName(m.To) + " " + line.Text
		}
		a = a.withRoom(a.room.appendUser(line, m.To))
	}
	// The conversation it was typed in, or the one a room mention aimed at.
	if dm, ok := a.dms[ev.SessionID]; ok && ev.SessionID != "" {
		a = a.withDM(ev.SessionID, dm.Append(ev))
	}
	return a, a.deliverBang(m)
}

// deliverBang sends a finished bang to its agent, or says in plain words why it
// did not. Asked when the command returns, so an agent that ended or was parked
// while it ran is not written to.
func (a App) deliverBang(m bangResultMsg) tea.Cmd {
	if m.To == "" {
		return nil
	}
	state := rpc.StateEnded
	if !a.endedAgent(m.To) {
		agent, _ := a.fleet.Agent(m.To)
		state = agent.State
	}
	if gone, ok := bangGone[state]; ok {
		notice.Report(bangNotSent, agentPrefix+a.agentName(m.To), gone)
		return nil
	}
	return a.write(sendFailed, rpc.Frame{
		Kind:      rpc.FrameSend,
		SessionID: m.To,
		MessageID: newShellSend(m.ID == ""), // so its turn is owed, and placed
		Shell:     &rpc.ShellRun{Command: m.Cmd, Output: m.Text},
	})
}

// bangDir is the directory a bang runs in: the one its agent runs in.
//
// Two situations get the empty string, which os/exec reads as "where Wake runs",
// and they are not the same one. A bang **aimed at nobody** - typed bare into the
// room - has no directory, and that is the honest answer. An **agent no report
// has described yet** has none either - fan-out starts before the spawn
// confirmation is enqueued - and there it is a silent revert, narrow (one report
// closes it) and named here rather than papered over.
func (a App) bangDir(id string) string {
	agent, ok := a.fleet.Agent(id)
	if !ok {
		return ""
	}
	return agent.Cwd
}

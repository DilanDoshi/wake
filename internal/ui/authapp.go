package ui

// `/login` from the outside: the command, the shell it runs, and the panel that
// comes back into the conversation it was typed into.
//
// It is the `/mcp` path (mcpapp.go) one subcommand over, and it reuses runBang
// for the same reason: bangRun already bounds a shell line off the draw goroutine
// and kills it by process group, and a second copy of that is the parallel
// implementation this project forbids. What differs is only the command and the
// renderer - `claude auth status --json`, parsed by authpanel.go.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// authStatusCmd reads whether this machine is signed in. `claude` rather than
	// an absolute path, for mcpListCmd's reason: the same binary the daemon spawns
	// agents with, so it reports on the installation the fleet runs.
	authStatusCmd = "claude auth status --json"

	// authChecking is what is said while the check runs. Brief, but not instant,
	// so a keystroke that appeared to do nothing gets a word.
	authChecking = "checking sign-in…"

	// authOnlyStatus is what `/login <anything>` is told. Wake shows status and
	// hands the sign-in command over; it does not run the login itself (the flow
	// wants a TTY, the no-PTY non-negotiable), so an argument is a shape this
	// build does not have.
	authOnlyStatus = "/login shows whether you are signed in; run `claude auth login` in a terminal to sign in"
)

// authResultMsg is a finished `claude auth status`, addressed to the
// conversation that asked. Separate from mcpResultMsg for its own reason: the two
// are parsed and rendered by different code, and a shared shape with a mode is
// how the wrong one gets drawn.
type authResultMsg struct {
	ID   string
	Text string
}

// login is `/login`: run the sign-in check, and say so while it runs.
func (a App) login(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg != "" {
		notice.Report("%s", authOnlyStatus)
		return a, nil
	}
	notice.Report("%s", authChecking)
	return a, runAuthStatus(a.conversationID(), a.bangDir())
}

// runAuthStatus runs the check off the draw goroutine, bounded like any bang.
func runAuthStatus(id, dir string) tea.Cmd {
	return func() tea.Msg {
		return authResultMsg{ID: id, Text: bangRun(dir, authStatusCmd, bangTimeout, bangWaitDelay)}
	}
}

// panelResult folds either subcommand panel into the conversation that asked -
// /mcp's server rows or /login's sign-in status. They share one Update case
// because they are one subject: a claude subcommand run in the bounded shell,
// parsed, and drawn where it was typed. The case reaches here with one of the two
// and nothing else, so the trailing assertion is the mcp result by elimination.
func (a App) panelResult(m tea.Msg) App {
	if r, ok := m.(authResultMsg); ok {
		return a.authResult(r)
	}
	return a.mcpResult(m.(mcpResultMsg))
}

// authResult puts the panel into the conversation that asked, the way mcpResult
// does and for its reason: a window holds the room and every open conversation,
// so a panel that landed in whichever was on screen would answer under the wrong
// one.
func (a App) authResult(m authResultMsg) App {
	st, ok := parseAuthStatus(m.Text)
	if m.ID == "" {
		return a.withRoom(a.room.Append(authEvent(m.ID, st, ok, a.room.blockWidth()), Agent{}))
	}
	dm, held := a.dms[m.ID]
	if !held {
		return a
	}
	return a.withDM(m.ID, dm.Append(authEvent(m.ID, st, ok, dm.blockWidth())))
}

// authEvent is the panel as something a transcript can hold. Rendered at the
// width it is added at and kept as text, the way a bang block is: it does not
// re-wrap on a resize.
func authEvent(id string, st authStatus, ok bool, width int) core.Event {
	return core.Event{
		Kind:      core.KindUserText,
		SessionID: id,
		Echoed:    true,
		Text:      authPanel(st, ok, width),
	}
}

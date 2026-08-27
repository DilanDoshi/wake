package ui

// `/mcp` from the outside: the command, the shell it runs, and the panel that
// comes back into the conversation it was typed into.
//
// It is the bang path with a different renderer, deliberately. runBang already
// solves the two hard parts - a bounded command off the draw goroutine, killed
// by process *group* so a health check that hangs cannot outlive its deadline -
// and a second copy of that is the parallel implementation this project
// forbids. What differs is only what the output becomes: a bang is fenced
// verbatim, and this is parsed into rows.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// mcpResultMsg is a finished `claude mcp list`, addressed to the conversation
// that asked. Separate from bangResultMsg rather than a flag on it: the two are
// folded by different code and a shared shape with a mode is how the wrong one
// gets rendered.
type mcpResultMsg struct {
	ID string

	// Dir is where the check ran, carried so the panel can put it in the
	// command it hands over. Taken from the message rather than re-read off the
	// fleet: the answer describes the directory it was asked in, and an agent
	// that moved between the ask and the answer must not relabel it.
	Dir  string
	Text string
}

// mcp is `/mcp`: run the health check, and say so while it runs.
//
// The argument is refused rather than ignored. `claude mcp` has add, remove,
// login and logout beside list, and somebody typing `/mcp login slack` means
// something specific - passing that through to a panel that lists would look
// like it worked and would have done nothing. Saying what this build does is
// the honest answer to a command it does not have yet.
func (a App) mcp(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if arg != "" {
		notice.Report("%s", mcpOnlyLists)
		return a, nil
	}
	// Said before the command starts, because it is not instant: every server
	// is dialled. A panel that appeared some seconds later with no warning
	// reads as a keystroke that did nothing.
	notice.Report("%s", mcpChecking)
	return a, runMCPList(a.conversationID(), a.bangDir())
}

// mcpOnlyLists is what `/mcp <anything>` is told.
const mcpOnlyLists = "/mcp lists this directory's servers; it does not add, remove or authenticate one yet"

// runMCPList runs the health check off the draw goroutine.
//
// bangTimeout, which is the bound a shell line already gets: this dials every
// configured server, so it is exactly the kind of command that can hang on one
// unreachable host, and it is bounded for that reason rather than by analogy.
func runMCPList(id, dir string) tea.Cmd {
	return func() tea.Msg {
		return mcpResultMsg{ID: id, Dir: dir, Text: bangRun(dir, mcpListCmd, bangTimeout, bangWaitDelay)}
	}
}

// mcpResult puts the panel into the conversation that asked for it.
//
// Addressed the way bangResult is, and for its reason: a window holds the room
// and every conversation somebody has opened, so a panel that landed in
// whichever was on screen would be a report about one directory shown under
// another - and MCP servers are configured per directory, which makes that
// exactly the wrong thing to get wrong.
func (a App) mcpResult(m mcpResultMsg) App {
	rows := parseMCPList(m.Text)
	if m.ID == "" {
		// The room's own width, not the App's: the two panes are different
		// sizes whenever a conversation is open beside the room.
		a = a.withRoom(a.room.Append(mcpEvent(m.ID, rows, m.Dir, a.room.blockWidth()), Agent{}))
		return a
	}
	dm, ok := a.dms[m.ID]
	if !ok {
		return a
	}
	return a.withDM(m.ID, dm.Append(mcpEvent(m.ID, rows, m.Dir, dm.blockWidth())))
}

// mcpEvent is the panel as something a transcript can hold.
//
// Rendered at the width it is added at and kept as text, which is how a bang
// block behaves too: it does not re-wrap on a resize. That is the honest trade
// for a panel whose rows are a fixed-width table - re-deriving it would mean
// keeping the rows, and a resize is not worth a second health check.
func mcpEvent(id string, rows []mcpRow, dir string, width int) core.Event {
	return core.Event{
		Kind:      core.KindUserText,
		SessionID: id,
		Echoed:    true,
		Text:      mcpPanel(rows, dir, width),
	}
}

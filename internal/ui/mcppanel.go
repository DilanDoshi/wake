package ui

// The `/mcp` panel: which MCP servers this directory has, and which of them are
// not working.
//
// # Why Wake draws this rather than passing the word through
//
// `/mcp` reaches a Wake agent and answers - it is not inert, which was the first
// guess and was wrong. What it answers is a count and an instruction to go
// somewhere else:
//
//	6 MCP server(s): 2 connected, 2 connecting, 2 not connected, 0 disabled.
//	Use `/mcp` in the terminal for details.
//
// That is the whole of what a headless session can say, because the panel behind
// it is an interactive TUI and Wake's agents are `--print --input-format
// stream-json`. So the passthrough spends the word on an answer whose own last
// sentence is "not here".
//
// **This is the exception to slash.go's rule, and it is recorded rather than
// taken quietly.** That rule says Wake's set stays small and avoids claude's,
// for a good reason: a word Wake takes is a word an operator can no longer reach.
// What buys the exception is that the thing being taken over is not a working
// feature - it is a redirection - and that the data behind the real panel is
// reachable from a *shell*, which Wake can run: `claude mcp list` health-checks
// every server and reports what is wrong with each.
//
// # Why not the init frame, which Wake already decodes
//
// Because it is less accurate, and the difference is the one that matters.
// `init.mcp_servers` reports `needs-auth` for a server whose auth is merely
// unconfigured *and* for one whose auth server is incompatible - a state no
// amount of authenticating will fix. `claude mcp list` tells them apart:
//
//	slack:      ✘ Failed to connect — Incompatible auth server: does not
//	            support dynamic client registration
//	higgsfield: ! Needs authentication
//
// The banner's count still comes off init, because it is free and arrives with
// every turn. This is the surface somebody opens when they want to act on it,
// and it pays a health check for the truth.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	// mcpListCmd is the shell line the panel is built from. `claude` rather than
	// an absolute path: it is the same binary the daemon spawns agents with, and
	// resolving it differently here would report on a different installation
	// from the one the fleet is running.
	mcpListCmd = "claude mcp list"

	// mcpTitle heads the panel.
	mcpTitle = "MCP servers"

	// mcpChecking is what the panel says while the health check runs. It is not
	// instant - every server is dialled - and a panel that appeared empty first
	// would read as "you have none".
	mcpChecking = "checking servers…"

	// mcpNone is a directory with no servers configured, which is not a problem
	// and must not look like one.
	mcpNone = "no MCP servers configured here"

	// mcpAuthHint introduces the command that authenticates one server, and it
	// is a *command to run elsewhere* rather than something Wake offers to do.
	//
	// **Because Wake cannot do it, and the reason is a non-negotiable rather
	// than a gap.** `claude mcp login <name> --no-browser` exists and is made
	// for headless sessions, and it still refuses a pipe:
	//
	//	stdin isn't a terminal, so authentication can't be completed here.
	//	Re-run in an interactive terminal
	//
	// It wants a TTY. Giving it one means Wake allocating a PTY and relaying an
	// interactive prompt, which is the first line of CLAUDE.md's
	// non-negotiables - *no PTY, no VT100* - and the failure mode that file
	// names by name: drifting into being a worse multiplexer. So the panel
	// hands the command over with the directory already in it, because that is
	// the whole of what is left to get right.
	mcpAuthHint = "authenticate in a terminal: "
)

// The three states claude reports, in Wake's words, and the glyph for each.
//
// Claude's own glyphs are ✔ ✘ !, and they are kept: this panel exists to be the
// screen somebody already knows. The *words* beside them are claude's too.
const (
	mcpOK    = "✔"
	mcpFail  = "✘"
	mcpNeeds = "!"
)

// mcpRow is one server as `claude mcp list` reports it.
type mcpRow struct {
	Name string

	// Where is the command or URL the server is reached at, with its transport
	// when claude names one - `https://mcp.linear.app/mcp (HTTP)`.
	Where string

	// Glyph is claude's own status marker, and Status the words after it.
	Glyph  string
	Status string

	// Detail is what follows the em dash on a failure, and empty otherwise: the
	// reason, which is the half that says whether authenticating would help.
	Detail string
}

// NeedsAuth reports whether authenticating this server is a thing that could
// work. **A failure is not that**, which is the distinction the init frame
// cannot make and the reason this panel pays for a health check.
func (r mcpRow) NeedsAuth() bool { return r.Glyph == mcpNeeds }

// parseMCPList turns `claude mcp list` into rows.
//
// The format is `<name>: <where> - <glyph> <status>[ — <detail>]`, and it is
// split on the **glyph** rather than on either separator: a URL contains colons
// and hyphens, so splitting on those cuts `https://mcp.linear.app/mcp` in half
// somewhere different for every server.
//
// A line that does not match is skipped rather than guessed at. The first line
// is claude's own "Checking MCP server health…" and there is no version of this
// that should render it as a server called "Checking MCP server health".
func parseMCPList(out string) []mcpRow {
	var rows []mcpRow
	for _, line := range strings.Split(out, "\n") {
		row, ok := parseMCPLine(strings.TrimSpace(line))
		if ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// parseMCPLine is one line, or false for anything that is not a server.
func parseMCPLine(line string) (mcpRow, bool) {
	name, rest, ok := strings.Cut(line, ": ")
	if !ok || name == "" {
		return mcpRow{}, false
	}
	for _, glyph := range []string{mcpOK, mcpFail, mcpNeeds} {
		where, status, found := strings.Cut(rest, " - "+glyph+" ")
		if !found {
			continue
		}
		// The reason, when there is one. An em dash, which is claude's - a
		// hyphen would collide with the separator above.
		status, detail, _ := strings.Cut(status, " — ")
		return mcpRow{
			Name:   name,
			Where:  strings.TrimSpace(where),
			Glyph:  glyph,
			Status: strings.TrimSpace(status),
			Detail: strings.TrimSpace(detail),
		}, true
	}
	return mcpRow{}, false
}

// mcpPanel renders the rows.
//
// Names are padded to a column so the statuses line up, which is the whole
// readability of a list somebody is scanning for the one that is broken. The
// detail sits under its row rather than beside it: it is a sentence, and a
// sentence in a column is a sentence nobody reads.
func mcpPanel(rows []mcpRow, dir string, width int) string {
	if len(rows) == 0 {
		return joinBlock(accentLine(mcpTitle, width), mutedLine(mcpNone, width))
	}

	wide := 0
	for _, r := range rows {
		wide = max(wide, lipgloss.Width(r.Name))
	}

	lines := make([]string, 0, len(rows)*3+1)
	for _, r := range rows {
		lines = append(lines, mcpRowLine(r, wide, width))
		if r.Detail != "" {
			lines = append(lines, mutedLine("  "+r.Detail, width))
		}
		// Only for a server authenticating could actually fix. A failure gets
		// its reason above and no command: telling somebody to authenticate a
		// server whose auth server is *incompatible* sends them to a prompt
		// that cannot succeed, which is the exact distinction this panel pays a
		// health check to be able to make.
		if r.NeedsAuth() {
			lines = append(lines, mutedLine("  "+mcpAuthHint+mcpLoginCmd(r.Name, dir), width))
		}
	}
	return joinBlock(accentLine(mcpTitle, width), strings.Join(lines, "\n"))
}

// mcpRowLine is one server's row: its glyph, its name, and what it is doing.
func mcpRowLine(r mcpRow, namePad, width int) string {
	style := TextStyle
	switch r.Glyph {
	case mcpOK:
		style = lipgloss.NewStyle().Foreground(Success)
	case mcpFail:
		style = lipgloss.NewStyle().Foreground(Warn)
	case mcpNeeds:
		style = warnStyle
	}
	name := r.Name + strings.Repeat(" ", max(0, namePad-lipgloss.Width(r.Name)))
	row := style.Render(r.Glyph) + " " + TextStyle.Render(name) + "  " + HintStyle.Render(r.Status)
	// Bounded here as well as by the transcript: a row that arrives already
	// inside the pane is one the reader does not have to scroll to finish.
	return lipgloss.NewStyle().MaxWidth(max(width, 1)).Render(row)
}

// mcpLoginCmd is the line somebody runs to authenticate one server.
//
// The directory is in it because MCP servers are configured per directory and
// the operator is, by construction, sitting in a Wake window rather than in that
// directory. A command they have to work out the `cd` for is a command with a
// step missing.
func mcpLoginCmd(name, dir string) string {
	login := "claude mcp login " + name
	if dir == "" {
		return login
	}
	return "cd " + shortPath(dir) + " && " + login
}

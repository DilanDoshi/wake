package ui

// How the /mcp menu draws: Claude Code's own layout - servers grouped by where
// they are configured, a ✔ ⚠ ✘ ⊘ glyph and a status per row, a detail view with
// numbered actions - inside the titled box the resume picker uses.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	mcpChecking = "checking servers…"
	mcpNone     = "no MCP servers configured for this agent"

	// mcpNoConnectors says why the claude.ai section Claude Code shows is not
	// here: a headless session does not load claude.ai connectors, before or
	// after a turn and with ENABLE_CLAUDEAI_MCP_SERVERS set (probed 2026-09-23,
	// 2.1.281). Said rather than left out, so the menu does not look broken
	// beside Claude's.
	mcpNoConnectors = "claude.ai connectors don't load in Wake's headless agents"
)

// mcpScopes is the list's grouping, closest scope first - the precedence
// Claude Code resolves a name clash by. where is the file a scope lives in.
var mcpScopes = []struct{ scope, title, where string }{
	{"local", "Local MCPs", "~/.claude.json, this project"},
	{"project", "Project MCPs", ".mcp.json"},
	{"user", "User MCPs", "~/.claude.json"},
	{"plugin", "Plugin MCPs", ""},
	{"claudeai", "claude.ai", ""},
	{"managed", "Managed MCPs", ""},
	{"enterprise", "Managed MCPs", ""},
	{"dynamic", "Session MCPs", ""},
}

type mcpGroup struct {
	title, where string
	servers      []core.MCPServerStatus
}

// mcpGroups splits the servers by scope, in mcpScopes' order, with any scope
// this build does not know last under its own name.
func mcpGroups(servers []core.MCPServerStatus) []mcpGroup {
	var groups []mcpGroup
	known := map[string]bool{}
	for _, sc := range mcpScopes {
		known[sc.scope] = true
		var in []core.MCPServerStatus
		for _, s := range servers {
			if s.Scope == sc.scope {
				in = append(in, s)
			}
		}
		if len(in) > 0 {
			groups = append(groups, mcpGroup{sc.title, sc.where, in})
		}
	}
	var other []core.MCPServerStatus
	for _, s := range servers {
		if !known[s.Scope] {
			other = append(other, s)
		}
	}
	if len(other) > 0 {
		groups = append(groups, mcpGroup{"Other MCPs", "", other})
	}
	return groups
}

// mcpLook is a state's glyph, its colour and its words.
func mcpLook(state string) (string, lipgloss.Style, string) {
	switch state {
	case core.MCPConnected:
		return "✔", ToolOkStyle, "connected"
	case core.MCPNeedsAuth:
		return "⚠", warnStyle, "needs authentication"
	case core.MCPFailed:
		return "✘", ErrorStyle, "failed"
	case core.MCPDisabled:
		return "⊘", HintStyle, "disabled"
	case core.MCPPending:
		return "○", HintStyle, "connecting…"
	}
	return "?", HintStyle, state
}

// mcpView is the menu if it was opened in this pane, and "" otherwise -
// resumeView's rule, and told the rows left under what is stacked over it.
func (a App) mcpView(id string, width, height int, over string) string {
	menu := a.mcpUI.menu
	if !menu.Open() || menu.Pane != id {
		return ""
	}
	room := a.menuRoom(id, width, height)
	if over != "" {
		room -= lipgloss.Height(over)
	}
	return menu.View(width, room)
}

func (m MCPMenu) View(width, maxRows int) string {
	w := max(width, minBlockWidth)
	inner := max(w-cardFrameWidth, 1)
	if maxRows < 3 {
		return HintStyle.Render(ansi.Truncate(m.title(), w, ellipsis))
	}
	var lines []string
	focus := 0
	switch m.Level {
	case mcpDetail:
		lines, focus = m.detailLines(inner)
	case mcpTools:
		lines, focus = m.toolLines(inner)
	default:
		lines, focus = m.listLines(inner)
	}
	lines = windowed(lines, focus, maxRows-2)
	body := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(lines, "\n"))
	return titledBox(body, w, BoxStyle.Padding(0), m.title(), m.keyHint(), AccentStyle, HintStyle)
}

func (m MCPMenu) keyHint() string {
	switch m.Level {
	case mcpDetail:
		return "↑↓ move · ↵ choose · esc back"
	case mcpTools:
		return "↑↓ move · esc back"
	}
	return "↑↓ move · ↵ open · esc close"
}

// windowed keeps the focused line in view when the lines outgrow the box.
func windowed(lines []string, focus, rows int) []string {
	if rows < 1 || len(lines) <= rows {
		return lines
	}
	start := clamp(focus-rows/2, 0, len(lines)-rows)
	return lines[start : start+rows]
}

func (m MCPMenu) listLines(width int) ([]string, int) {
	if !m.Loaded {
		return []string{mutedLine(mcpChecking, width)}, 0
	}
	if len(m.Servers) == 0 {
		return []string{mutedLine(mcpNone, width)}, 0
	}
	pad := 0
	for _, s := range m.Servers {
		pad = max(pad, lipgloss.Width(s.Name))
	}
	lines := []string{mutedLine(plural(len(m.Servers), "server"), width)}
	focus, row := 0, 0
	for _, g := range mcpGroups(m.Servers) {
		head := lipgloss.NewStyle().Bold(true).Foreground(Text).Render(g.title)
		if g.where != "" {
			head += " " + HintStyle.Render("("+g.where+")")
		}
		lines = append(lines, "", ansi.Truncate(head, width, ellipsis))
		for _, s := range g.servers {
			if row == m.Cursor {
				focus = len(lines)
			}
			lines = append(lines, mcpRow(s, pad, row == m.Cursor, width))
			row++
		}
	}
	return append(lines, "", mutedLine(mcpNoConnectors, width)), focus
}

// mcpRow is one server: a cursor, its glyph, its name and what it is doing - a
// tool count once it is connected, which is what Claude Code shows there.
func mcpRow(s core.MCPServerStatus, pad int, cursored bool, width int) string {
	glyph, style, word := mcpLook(s.State)
	if s.State == core.MCPConnected {
		word = plural(len(s.Tools), "tool")
	}
	lead := cardUnchosen
	nameStyle := TextStyle
	if cursored {
		lead, nameStyle = cardCursor, AccentStyle
	}
	name := s.Name + strings.Repeat(" ", max(0, pad-lipgloss.Width(s.Name)))
	row := nameStyle.Render(lead) + style.Render(glyph) + " " + nameStyle.Render(name) + "  " + HintStyle.Render(word)
	return ansi.Truncate(row, width, ellipsis)
}

// field is one "Label:  value" row of the detail view.
func field(label, value string, width int) string {
	return ansi.Truncate(HintStyle.Render(fmt.Sprintf("%-9s", label+":"))+value, width, ellipsis)
}

func (m MCPMenu) detailLines(width int) ([]string, int) {
	s, _ := m.server()
	glyph, style, word := mcpLook(s.State)
	status := style.Render(glyph+" "+word)
	if s.Info != "" {
		status += HintStyle.Render(" · " + s.Info)
	}
	lines := []string{field("Status", status, width)}
	if s.Target != "" {
		label, target := "Command", s.Target
		if strings.Contains(s.Target, "://") {
			label = "URL"
		}
		if s.Transport != "" {
			target += HintStyle.Render(" (" + s.Transport + ")")
		}
		lines = append(lines, field(label, TextStyle.Render(target), width))
	}
	lines = append(lines, field("Config", TextStyle.Render(mcpScopeName(s.Scope)), width))
	if s.State == core.MCPConnected {
		lines = append(lines, field("Tools", TextStyle.Render(fmt.Sprint(len(s.Tools))), width))
	}
	if s.Error != "" {
		lines = append(lines, field("Error", ErrorStyle.Render(collapseWhitespaceOneLine(s.Error)), width))
	}
	lines = append(lines, "")
	focus := len(lines)
	for i, act := range m.actions() {
		if i == m.Action {
			focus = len(lines)
		}
		lines = append(lines, optionRow(fmt.Sprintf("%d. %s", i+1, act), width, i == m.Action, false, AccentStyle))
	}
	if line := m.outcome(width); line != "" {
		lines = append(lines, "", line)
	}
	return lines, focus
}

// mcpScopeName is where a scope's servers are configured, for the detail view.
func mcpScopeName(scope string) string {
	for _, sc := range mcpScopes {
		if sc.scope == scope && sc.where != "" {
			return strings.TrimSuffix(sc.title, " MCPs") + "  " + sc.where
		}
	}
	return scope
}

// mcpBusy is what the detail view says while an action waits for its reply.
var mcpBusy = map[string]string{
	core.MCPAskReconnect: "reconnecting…",
	core.MCPAskEnable:    "switching it on…",
	core.MCPAskDisable:   "switching it off…",
	mcpAskSignIn:         "signing in…",
}

// outcome is the action in flight, or how the last one ended.
func (m MCPMenu) outcome(width int) string {
	switch {
	case m.Busy != "":
		return mutedLine(mcpBusy[m.Busy], width)
	case m.Result == "":
		return ""
	case m.Failed:
		return ansi.Truncate(ErrorStyle.Render("✘ "+collapseWhitespaceOneLine(m.Result)), width, ellipsis)
	}
	return ansi.Truncate(ToolOkStyle.Render("✔ "+m.Result), width, ellipsis)
}

// toolLines is the server's tools. The session reports names and whether a tool
// only reads - no descriptions (see core.MCPTool) - so that is what shows.
func (m MCPMenu) toolLines(width int) ([]string, int) {
	s, _ := m.server()
	if len(s.Tools) == 0 {
		return []string{mutedLine("this server offers no tools", width)}, 0
	}
	lines := make([]string, 0, len(s.Tools))
	for i, t := range s.Tools {
		label := t.Name
		if t.ReadOnly {
			label += "  " + HintStyle.Render("read-only")
		}
		lead, style := cardUnchosen, TextStyle
		if i == m.Action {
			lead, style = cardCursor, AccentStyle
		}
		lines = append(lines, ansi.Truncate(style.Render(lead)+style.Render(t.Name)+strings.TrimPrefix(label, t.Name), width, ellipsis))
	}
	return lines, m.Action
}

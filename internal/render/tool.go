package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// collapsedResultLines is how many rendered *rows* a folded body may
	// occupy. Rows rather than source lines, which is where this differs from
	// Claude Code: Wake draws into a pane that is often a quarter of the
	// screen, so four wrapped source lines is twenty rows - the clutter the
	// fold exists to prevent.
	collapsedResultLines = 4

	// commandChars bounds the command a Bash header shows. Claude Code's own
	// cap, matched against Claude Code 2.1.233, where its Bash renderer slices at
	// 160 and appends an ellipsis.
	commandChars = 160

	toolBullet    = "⏺ "
	resultBullet  = "  ⎿  "
	resultIndent  = "     "
	commandMarker = "$ "

	// ellipsis marks text this package had to cut to fit a width.
	ellipsis = "…"
)

// Call is one tool invocation, resolved behind the airlock.
//
// This package holds no map from tool name to argument key: the decisive case
// is the subagent dispatch, whose wire name is Agent while init.tools
// advertises Task, so a map maintained here would be keyed on a name that
// never arrives. Only the decoder can know the two names differ.
type Call struct {
	Name string

	// Title heads the line in place of Name and Display - the description a
	// Bash call carries, which is what Claude Code puts there. Empty for a
	// tool whose own name heads it.
	Title string

	// Display is the one argument worth showing beside the name.
	Display string
}

// Result is one tool result, ready to draw.
type Result struct {
	Body string

	// Receipt stands in for Body while it is folded - a format with one %d
	// for the body's line count, as in `Read %d lines`. Empty for every tool
	// no recording has earned one for; internal/core owns which those are,
	// because a receipt hides the body and an unrecorded shape has not earned
	// that.
	Receipt string

	// Continued marks a body under a header that already opened the gutter,
	// so it draws the indent rather than a second ⎿.
	Continued bool

	Collapsed bool

	// Expand names the key that shows the whole body, the way Claude Code
	// names ctrl+o under anything it folded. Empty draws no hint.
	Expand string

	// Failed draws the body in the failure style.
	Failed bool
}

// ToolStyle is what a tool block is drawn with. The palette is the caller's;
// this package owns the layout.
type ToolStyle struct {
	Bullet lipgloss.Style
	Name   lipgloss.Style
	Arg    lipgloss.Style
	Body   lipgloss.Style
	Fail   lipgloss.Style
}

// ToolCall renders the ⏺ headline for a tool invocation, bounded to width.
//
// One line, deliberately: it is the row a settling result rewrites in place,
// and a two-line return would have that rewrite draw the second line twice.
// The `$ …` line under it is ToolCommand's.
func ToolCall(c Call, st ToolStyle, width int) string {
	head, arg := c.Name, c.Display
	if c.Title != "" {
		head, arg = c.Title, ""
	}
	return callHeader(head, arg, st, width)
}

// ToolCommand renders the `$ …` line under a call that carries a command,
// opening the gutter the call's result then continues. Empty for a call with
// no command, and for a width with no room for one.
func ToolCommand(cmd string, st ToolStyle, width int) string {
	if cmd == "" {
		return ""
	}
	return withGutter(wrapBody(commandMarker+shortCommand(cmd), gutterRoom(width)), resultBullet, st.Body)
}

// ToolRollup renders the one dimmed ⎿ line a folded run of tool calls stands
// in for - `28 tool uses · 24 bash · 1 read · 3 linear-server` - with the same
// expand hint a folded body carries. summary is composed behind the airlock,
// where the tool names live; this package only draws it.
//
// One line, truncated rather than wrapped, for ToolCall's own reason: it is the
// row a growing run rewrites in place, and a two-line return would have that
// rewrite draw the second line twice.
func ToolRollup(summary, expand string, st ToolStyle, width int) string {
	if summary == "" || width < 1 {
		return ""
	}
	return st.Body.Render(ansi.Truncate(resultBullet+summary+expandHint(expand), width, ellipsis))
}

// ToolResult renders the ⎿ body under a tool call, folded by default.
func ToolResult(r Result, st ToolStyle, width int) string {
	body := strings.TrimRight(r.Body, "\n")
	room := gutterRoom(width)
	if body == "" || room < 1 {
		return ""
	}
	style, lead := st.Body, resultBullet
	if r.Failed {
		style = st.Fail
	}
	if r.Continued {
		lead = resultIndent
	}
	if !r.Collapsed {
		return withGutter(wrapBody(body, room), lead, style)
	}
	if r.Receipt != "" {
		n := len(strings.Split(body, "\n"))
		return withGutter(wrapBody(fmt.Sprintf(r.Receipt, n)+expandHint(r.Expand), room), lead, style)
	}
	rows, hidden := fold(body, room)
	if hidden > 0 {
		rows = append(rows, wrapBody(ellipsis+" +"+lineCount(hidden)+expandHint(r.Expand), room)...)
	}
	return withGutter(rows, lead, style)
}

// callHeader is the ⏺ line: the bullet, the name or title, and the argument in
// parentheses. Each part carries its own complete style, because a style
// applied around the whole line would end at the first inner reset.
func callHeader(head, arg string, st ToolStyle, width int) string {
	bullet := ansi.StringWidth(toolBullet)
	head = ansi.Truncate(collapseWhitespace(head), max(width-bullet, 0), ellipsis)
	line := st.Bullet.Render(toolBullet) + st.Name.Render(head)
	used := bullet + ansi.StringWidth(head)
	// Two columns for the parentheses themselves, so an argument is drawn only
	// when there is room for it to say something.
	if arg = collapseWhitespace(arg); arg != "" {
		if room := width - used - 2; room > 0 {
			line += st.Arg.Render("(" + ansi.Truncate(arg, room, ellipsis) + ")")
		}
	}
	return ansi.Truncate(line, width, "")
}

// gutterRoom is the width left for content once the ⎿ gutter is taken.
func gutterRoom(width int) int { return width - ansi.StringWidth(resultIndent) }

// withGutter puts rows under a tool call: the first behind lead, every one
// after it behind the plain indent so a wrapped line stays in the column.
func withGutter(rows []string, lead string, style lipgloss.Style) string {
	out := make([]string, 0, len(rows))
	for i, r := range rows {
		prefix := resultIndent
		if i == 0 {
			prefix = lead
		}
		out = append(out, style.Render(prefix+r))
	}
	return strings.Join(out, "\n")
}

// wrapBody lays text out at width: tabs expanded first, then wrapped rather
// than cut.
//
// Expanded because ansi.StringWidth counts a tab as zero and a terminal does
// not, so a grep over tab-indented source measured short and overflowed the
// pane. Wrapped because a bounded pane is a layout requirement, not a licence
// to drop the rest of the line - which is what cutting to an ellipsis did.
func wrapBody(text string, width int) []string {
	if width < 1 {
		return nil
	}
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = expandTabs(l)
		if ansi.StringWidth(l) <= width {
			out = append(out, l)
			continue
		}
		for _, w := range strings.Split(ansi.Wrap(l, width, ""), "\n") {
			out = append(out, ansi.Truncate(w, width, ""))
		}
	}
	return out
}

// fold keeps the first collapsedResultLines rendered rows of body and reports
// how many source lines are not shown whole. A line that is longer than the
// whole budget shows what fits and still counts as hidden, so the footer never
// claims a line was drawn when only its head was.
func fold(body string, room int) ([]string, int) {
	lines := strings.Split(body, "\n")
	var rows []string
	for i, l := range lines {
		w := wrapBody(l, room)
		if len(rows)+len(w) > collapsedResultLines {
			if len(rows) == 0 {
				return w[:collapsedResultLines], len(lines) - i
			}
			return rows, len(lines) - i
		}
		rows = append(rows, w...)
	}
	return rows, 0
}

// expandHint names the key that undoes a fold. Claude Code prints
// `(ctrl+o to expand)` and composes it from its own keybinding registry rather
// than from a literal, which is why the key is the caller's here too.
func expandHint(key string) string {
	if key == "" {
		return ""
	}
	return " (" + key + " to expand)"
}

// shortCommand bounds the command a header shows. See commandChars.
func shortCommand(cmd string) string {
	cmd = collapseWhitespace(cmd)
	if r := []rune(cmd); len(r) > commandChars {
		return string(r[:commandChars]) + ellipsis
	}
	return cmd
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

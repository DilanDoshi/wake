package ui

// The session-usage bar: the account's five-hour window, three rows at the
// bottom of the right sidebar (owner's 2026-10-24 picks). Hidden until a report
// names a reading, and kept once one has - the daemon never drops it, and a
// reset that passes draws as the empty bar it is. The rows are rendered per
// report and per layout (withUsageRows), never per frame: the shimmer redraws
// the frame twenty times a second.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// usageFigureCells is the widest figure, "100%"; the bar takes the rest of
	// the row less the space before it.
	usageFigureCells = 4
	usageBarGlyph    = "█"
)

var (
	usageRuleStyle  = lipgloss.NewStyle().Foreground(Subtle)
	usageFillStyle  = lipgloss.NewStyle().Foreground(UsageFill)
	usageTrackStyle = lipgloss.NewStyle().Foreground(UsageTrack)
)

// usageRows is a reading as the sidebar draws it: a rule, the bar with its
// figure, and when the window resets. None with no reading or no width.
func usageRows(u *rpc.SessionUsage, width int) []string {
	if u == nil || width <= 0 {
		return nil
	}
	clock := strings.ToLower(u.Resets.Local().Format("3:04PM"))
	fill, figure := usageTone(u)
	caption := "session · ↻ " + clock
	shown := figure.Render(fmt.Sprintf("%*d%%", usageFigureCells-1, u.Percent))
	if u.Lapsed {
		caption = "session · reset " + clock
		shown = HintStyle.Render(fmt.Sprintf("%*s", usageFigureCells, "—"))
	}
	return []string{
		fitRow(usageRuleStyle.Render(strings.Repeat(sectionRule, width)), width),
		fitRow(usageBar(u, fill, width-usageFigureCells-1)+" "+shown, width),
		fitRow(HintStyle.Render(caption), width),
	}
}

// usageTone is Claude's verdict as colour: the used cells and the figure.
func usageTone(u *rpc.SessionUsage) (fill, figure lipgloss.Style) {
	switch {
	case u.Limited:
		return ErrorStyle, ErrorStyle
	case u.Warned:
		return warnStyle, warnStyle
	}
	return usageFillStyle, TextStyle
}

// usageBar is the window as cells: the used ones in fill, the rest in Claude's
// track colour. A lapsed reading is all track - the window reset.
func usageBar(u *rpc.SessionUsage, fill lipgloss.Style, cells int) string {
	if cells <= 0 {
		return ""
	}
	used := 0
	if !u.Lapsed {
		used = min(max((u.Percent*cells+50)/100, 0), cells)
	}
	return fill.Render(strings.Repeat(usageBarGlyph, used)) + usageTrackStyle.Render(strings.Repeat(usageBarGlyph, cells-used))
}

// withUsageRows renders the last report's reading at the sidebar's width - on
// a report, and in resizePanes, which every change to that width goes through.
func (a App) withUsageRows() App {
	a.daemon.rows = usageRows(a.daemon.usage, max(a.regions().Roster-dividerWidth, 0))
	return a
}

// usageDrawn is the rows this frame draws under the roster: none with no
// reading, or in a pane too short to leave the roster a row of its own.
func (a App) usageDrawn() []string {
	if len(a.daemon.rows) >= a.paneHeight() {
		return nil
	}
	return a.daemon.rows
}

// rosterHeight is the rows the roster has above the bar - what it is drawn in
// and what a click on it is resolved against, so the two cannot disagree.
func (a App) rosterHeight() int { return a.paneHeight() - len(a.usageDrawn()) }

// withUsage puts the bar under a drawn roster column.
func (a App) withUsage(side string) string {
	if rows := a.usageDrawn(); len(rows) > 0 {
		return side + "\n" + strings.Join(rows, "\n")
	}
	return side
}

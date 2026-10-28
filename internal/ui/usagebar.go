package ui

// The session-usage bar: the account's five-hour window, three rows at the
// bottom of the right sidebar (owner's 2026-10-24 picks). Hidden until a report
// names a reading and kept after it - the daemon never drops one, and a reset
// that passes draws as the empty bar it is (decisions.md, 2026-10-24). The rows
// are rendered per report and per layout (withUsageRows), never per frame: the
// shimmer redraws the frame twenty times a second.

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

	// usageClock is how the reset is written - /usage's own "4:09pm".
	usageClock = "3:04pm"
)

var (
	usageRuleStyle  = lipgloss.NewStyle().Foreground(Subtle)
	usageFillStyle  = lipgloss.NewStyle().Foreground(UsageFill)
	usageTrackStyle = lipgloss.NewStyle().Foreground(UsageTrack)
)

// usageRows is a reading as the sidebar draws it: a rule, the bar with its
// figure, and when the window resets. The used cells and the figure wear
// Claude's verdict; a lapsed reading is all track - the window reset. None with
// no reading or no width.
func usageRows(u *rpc.SessionUsage, width int) []string {
	if u == nil || width <= 0 {
		return nil
	}
	fill, figure := usageFillStyle, TextStyle
	switch {
	case u.Limited:
		fill, figure = ErrorStyle, ErrorStyle
	case u.Warned:
		fill, figure = warnStyle, warnStyle
	}
	cells := max(width-usageFigureCells-1, 0)
	used := min(max((u.Percent*cells+50)/100, 0), cells)
	clock := u.Resets.Local().Format(usageClock)
	shown, caption := figure.Render(fmt.Sprintf("%*d%%", usageFigureCells-1, u.Percent)), "session · ↻ "+clock
	if u.Lapsed {
		used = 0
		shown, caption = HintStyle.Render(fmt.Sprintf("%*s", usageFigureCells, "—")), "session · reset "+clock
	}
	bar := fill.Render(strings.Repeat(usageBarGlyph, used)) + usageTrackStyle.Render(strings.Repeat(usageBarGlyph, cells-used))
	return []string{
		fitRow(usageRuleStyle.Render(strings.Repeat(sectionRule, width)), width),
		fitRow(bar+" "+shown, width),
		fitRow(HintStyle.Render(caption), width),
	}
}

// withUsageRows renders the last report's reading at the sidebar's width - on
// a report, and in resizePanes, which every change to that width goes through.
func (a App) withUsageRows() App {
	width := max(a.regions().Roster-dividerWidth, 0)
	a.daemon.rows = usageRows(a.daemon.usage, width)
	a.corner = a.corner.at(a.daemon.rows, width)
	return a
}

// usageDrawn is the rows this frame draws under the roster: the friend with
// the bar (friends.go), or the bar alone where the pane is too short for both;
// none with neither, or in a pane too short to leave the roster a row of its own.
func (a App) usageDrawn() []string {
	h := a.paneHeight()
	if rows := a.corner.block(a.drawnFriend(), a.friendFrame()); len(rows) > 0 && len(rows) < h {
		return rows
	}
	if len(a.daemon.rows) >= h {
		return nil
	}
	return a.daemon.rows
}

// rosterHeight is the rows the roster has above the bar - what it is drawn in
// and what a click on it is resolved against, so the two cannot disagree.
func (a App) rosterHeight() int { return a.paneHeight() - len(a.usageDrawn()) }

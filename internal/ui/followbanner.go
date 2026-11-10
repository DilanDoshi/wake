package ui

// Following: where the reader stands against the newest line, the moves across
// it, and the banner that says they have left it.
//
// The answer being written is the window's last rows (transcript.tail), so a
// move across the line is a move like any other: ScrollUp, JumpToLatest, and
// followed for every other return (⌃E, a fold that reaches the bottom, a restore).
//
// Append deliberately never yanks a scrolled-back reader to the newest line -
// see dm.go's own comment on that. But the working line is drawn
// unconditionally, regardless of scroll position, so a reader who has drifted
// even one wheel notch off the bottom sees the transcript freeze exactly where
// they left it while the agent keeps working below it, with nothing on screen
// saying why. This is that missing signal, and a click
// on it is the way back - scrolling down far enough already resumes following
// (transcript.scrolledUp clamps to bottom()), this just makes that
// discoverable and one click.
//
// It replaces the transcript's own last visible line rather than costing a
// chrome row: chrome/tr sizing is exactly the trickiest, most heavily-tested
// coupling in this file's neighbourhood (dm.go's chromeHeight, transcript.go's
// atBottom), and a banner that folds into the existing render has nothing new
// to keep in sync with it.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ScrollUp moves the reader lines back through the conversation, or forward for
// a negative count, and stops at either end.
//
// The stored layout lags the drawn one (View re-lays a copy), so it is brought up
// to date first and the move is measured on what is on screen. The answer being
// written is the window's last rows (transcript.tail), so n up is n rows back
// across it and the transcript alike.
func (d DM) ScrollUp(lines int) DM {
	d = d.drawnLayout()
	d.tr = d.tr.scrolledUp(lines)
	return d
}

// JumpToLatest returns to the newest line and resumes following - what a
// click on the follow banner means.
func (d DM) JumpToLatest() DM { return d.followed() }

// followed returns the reader to the newest row. Every return outside SetSize
// and Append goes through it.
func (d DM) followed() DM {
	d.tr = d.tr.toBottom()
	return d
}

// followBannerText is the whole of the banner. Short, because it takes the
// place of a line of real content and has to read at a glance - and it names
// the click, since nothing else on this row looks interactive.
const followBannerText = "↓ new messages below - scroll down or click here"

// followLine is the absolute transcript line the banner draws on, and -1 when
// the reader is already following. Computed fresh rather than stored, the way
// t.bottom already is: it is a pure function of scroll, content and height, so
// there is nothing to invalidate on a re-wrap or a resize.
func (t transcript) followLine() int {
	if t.atBottom() {
		return -1
	}
	top := min(max(t.scroll, t.first()), t.bottom())
	return top + t.height - 1
}

// withFollowBanner overlays the banner on a rendered transcript when tr is not
// at the bottom, and returns rendered untouched otherwise.
func withFollowBanner(rendered string, tr transcript, width int) string {
	if tr.followLine() < 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	last := len(lines) - 1
	lines[last] = HintStyle.Width(width).Render(ansi.Truncate(followBannerText, width, ""))
	return strings.Join(lines, "\n")
}

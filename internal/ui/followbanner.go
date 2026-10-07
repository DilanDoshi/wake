package ui

// Following: where the reader stands against the newest line, the moves across
// it, and the banner that says they have left it.
//
// A follower gets the pane's room for the streamed preview and a scrolled-back
// reader the floor (DM.previewCap), so every move across the line re-caps it:
// ScrollUp, JumpToLatest, and followed for every other return (⌃E, a fold that
// reaches the bottom, a subagent view, a restore). SetSize and Append re-cap with
// the follow they sampled; clear.go and roomseed.go are not returns - the pane is
// blank there, or not yet sized.
//
// Append deliberately never yanks a scrolled-back reader to the newest line -
// see dm.go's own comment on that. But the streamed preview and the working
// line are drawn unconditionally, regardless of scroll position, so a reader
// who has drifted even one wheel notch off the bottom sees the transcript
// freeze exactly where they left it while those two rows keep changing below
// it - "the old text stays put and the new text scrolls in a box under it",
// with nothing on screen saying why. This is that missing signal, and a click
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
// to date first and the move is measured on what is on screen. When the move
// changes the preview's cap the pane is laid out again with the bottom line held
// where the scroll put it, so n up is n lines back and n down is the newest line.
func (d DM) ScrollUp(lines int) DM {
	d = d.drawnLayout()
	d.tr = d.tr.scrolledUp(lines)
	following := d.tr.atBottom()
	if d.height <= 0 || d.previewCap(following) == d.partial.cap {
		return d
	}
	foot := d.tr.footLine()
	d = d.SetSize(d.width, d.height) // re-caps, and keeps a follower on the newest line
	if !following {
		d.tr = d.tr.withFootAt(foot)
	}
	return d
}

// JumpToLatest returns to the newest line and resumes following - what a
// click on the follow banner means.
func (d DM) JumpToLatest() DM { return d.followed() }

// followed returns the reader to the newest line and gives the preview the room
// a follower gets. Every return outside SetSize and Append goes through it.
func (d DM) followed() DM {
	d.tr = d.tr.toBottom()
	d.partial = d.partial.capped(d.previewCap(true))
	return d
}

// footLine is the line on the window's last row, which the banner covers.
func (t transcript) footLine() int { return t.scroll + t.height - 1 }

// withFootAt scrolls the window so line is on its last row, as far as it can.
func (t transcript) withFootAt(line int) transcript {
	t.scroll = min(max(line-t.height+1, t.first()), t.bottom())
	return t
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

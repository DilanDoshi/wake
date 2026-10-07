package ui

// Following: a reader's place against the newest line, every move across it, and
// the banner that says they have left it.
//
// A reader at the bottom is following, and that decides what the streamed preview
// may take (DM.previewCap): a follower gets the pane's room, each row of the answer
// pushing the transcript up one, while a reader who has scrolled back keeps the
// floor so nothing they are reading moves. So the moves across the line live
// together here, and each re-caps the preview: ScrollUp (the wheel, a drag at a
// pane's edge, the keys), JumpToLatest (a click on the banner) and followed, the
// one helper every other return goes through (⌃E, a subagent view, a restore).
// SetSize and Append cross the line too, and re-cap with the follow they sampled
// before the content moved. A return that skips followed leaves the preview in the
// floor's box over a pane that wants it larger.
//
// The banner is the one line that tells a reader they have scrolled away from the
// newest message.
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

// ScrollUp moves the reader lines back through the conversation, or forward
// for a negative count, and stops at either end. The wheel, a drag at a pane's
// edge and the scroll keys all come here.
//
// It is the only way in to a scroll position transcript has tracked and Append
// has sampled since both were written: Append deliberately does not return a
// reader who has scrolled back to the newest line, which is a promise nothing
// could keep - or break - while no caller could scroll. The preview is re-capped
// for wherever the reader landed, so leaving the bottom gives it back to the
// floor and wheeling down to the bottom gives it its room again.
func (d DM) ScrollUp(lines int) DM {
	d.tr = d.tr.scrolledUp(lines)
	d.partial = d.partial.capped(d.previewCap(d.tr.atBottom()))
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

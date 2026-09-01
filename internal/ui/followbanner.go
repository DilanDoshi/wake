package ui

// The follow banner: the one line that tells a reader they have scrolled away
// from the newest message.
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

package ui

// The notice row is one row, so a long notice is cut - and a cut must say so,
// or a row cut inside a command reads as a shorter command (the 2026-11-07
// deferred entry: the cap refusal's `wake stop --fleet dev` drawn as `wake stop`).

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// noticeRowWidth is the window sizedApp opens, so the cells the row may draw.
const noticeRowWidth = 80

// longNotice is wider than noticeRowWidth with the prefix in front.
var longNotice = strings.TrimSpace(strings.Repeat("the daemon refused that because ", 6))

// assertCutAt fails unless row is a cut of full: the words up to the edge, then
// "…" as the row's last cell, and no wider than width.
func assertCutAt(t *testing.T, row, full string, width int) {
	t.Helper()
	if !strings.HasSuffix(row, ellipsis) {
		t.Errorf("a cut row does not end in %q: %q", ellipsis, row)
	}
	if !strings.HasPrefix(full, strings.TrimSuffix(row, ellipsis)) {
		t.Errorf("the cut row is not the start of its text:\n row %q\nfull %q", row, full)
	}
	if got := ansi.StringWidth(row); got > width || got < width-1 {
		t.Errorf("the cut row draws %d cells, want %d (or one less, a wide rune at the cut)", got, width)
	}
}

func TestACutNoticeEndsInAnEllipsisAtTheRowsWidth(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")
	notice.Report("%s", longNotice)
	row := stripANSI(a.noticeLine())
	assertCutAt(t, row, noticePrefix+longNotice, noticeRowWidth)
	if got := ansi.StringWidth(row); got != noticeRowWidth {
		t.Errorf("the cut row draws %d cells, want all %d", got, noticeRowWidth)
	}
}

// The pinned API failure is drawn through the same row, so it is cut the same way.
func TestACutPinnedFailureEndsInAnEllipsis(t *testing.T) {
	ticks := recordNoticeTicks(t)
	a := sizedApp(t, nil, nil, "s1")
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}})
	m, _ := a.Update(frameMsg{Frame: apiErrorFrame("s1", "Failed to authenticate. "+longNotice)})
	m, _ = m.Update((*ticks)[len(*ticks)-1].msg)
	a = m.(App)

	pin := a.pinnedNotice()
	if pin == "" {
		t.Fatal("nothing pinned, so this asserted nothing")
	}
	assertCutAt(t, stripANSI(a.noticeLine()), noticePrefix+pin, noticeRowWidth)
}

// Only a cut is marked: a notice the row holds exactly, or with room to spare,
// is drawn as it was written. One cell more is the first that cuts.
func TestANoticeThatFitsIsNotMarkedCut(t *testing.T) {
	room := noticeRowWidth - ansi.StringWidth(noticePrefix)
	for _, text := range []string{"attached to @alex", strings.Repeat("x", room)} {
		fresh(t)
		a := sizedApp(t, nil, nil, "s1")
		notice.Report("%s", text)
		if row := stripANSI(a.noticeLine()); row != noticePrefix+text {
			t.Errorf("a notice that fits was changed:\n got %q\nwant %q", row, noticePrefix+text)
		}
	}

	fresh(t)
	a := sizedApp(t, nil, nil, "s1")
	over := strings.Repeat("x", room+1)
	notice.Report("%s", over)
	assertCutAt(t, stripANSI(a.noticeLine()), noticePrefix+over, noticeRowWidth)
}

// The colour opens before the words and closes after the "…", so the cut never
// leaves the row's style on for the cells after it; and a control sequence in
// the words is still neutralised before the cut, never cut in half.
func TestACutNoticeKeepsItsColourSealed(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, so warnStyle renders an escape
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	a := sizedApp(t, nil, nil, "s1")
	notice.Report("boom \x1b[2J\u009b2J %s", longNotice)
	raw := a.noticeLine()
	if !strings.HasPrefix(raw, "\x1b[") || !strings.HasSuffix(raw, ellipsis+"\x1b[0m") {
		t.Errorf("the cut row's colour is not opened before it and closed after the %q: %q", ellipsis, raw)
	}
	for _, forbidden := range []string{"\x1b[2J", "\u009b"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the cut row carries %q verbatim: %q", forbidden, raw)
		}
	}
	if got := ansi.StringWidth(raw); got != noticeRowWidth {
		t.Errorf("the coloured cut row draws %d cells, want %d", got, noticeRowWidth)
	}
}

// A wide rune that will not fit before the "…" is dropped whole, so the row is
// never wider than the window - a row one cell too wide wraps and scrolls the
// alt screen on every draw.
func TestACutNoticeOfWideRunesStaysInsideTheRow(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")
	wide := strings.Repeat("漢", noticeRowWidth)
	notice.Report("%s", wide)
	assertCutAt(t, stripANSI(a.noticeLine()), noticePrefix+wide, noticeRowWidth)
}

// "Drawn cells" in the linger is the row after the cut, the "…" one of them:
// a cut row stands for the cells it shows, not for the words it lost.
func TestACutNoticeLingersForTheCellsItDraws(t *testing.T) {
	ticks := recordNoticeTicks(t)
	const width = 150 // wide enough that the cut row stands past the floor
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(width, 24)
	notice.Report("%s %s", longNotice, longNotice)
	_, _ = a.Update(nudge{})
	if want := time.Duration(width) * noticePerCell; len(*ticks) != 1 || (*ticks)[0].d != want {
		t.Errorf("a cut row armed %v; want one linger of %v, its drawn cells", *ticks, want)
	}
}

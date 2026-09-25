package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The notice row is a moment, not a record: every notice clears itself after a
// linger scaled to what there is to read, and an API failure stays pinned under
// it until its session recovers.

// armedTick is one linger the seam was asked for, and the expiry it would send.
type armedTick struct {
	d   time.Duration
	msg tea.Msg
}

// recordNoticeTicks swaps the timer seam for a recorder, for beat_test.go's
// countTicks reason: a hand-delivered expiry never runs the real timer.
func recordNoticeTicks(t *testing.T) *[]armedTick {
	t.Helper()
	var ticks []armedTick
	prev := noticeTimer
	noticeTimer = func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
		tick := armedTick{d: d, msg: fn(time.Now())}
		ticks = append(ticks, tick)
		return func() tea.Msg { return tick.msg }
	}
	t.Cleanup(func() { noticeTimer = prev })
	return &ticks
}

// nudge is a message update ignores: any message at all is when a notice
// reported off the Update path (a draw, a goroutine, main before Run) is armed.
type nudge struct{}

// The screenshot's case: `/name` says `renaming @alex…`, and that line then
// stood until something else overwrote it - on a quiet fleet, for good.
func TestARenameNoticeClearsItselfAfterItsLinger(t *testing.T) {
	ticks := recordNoticeTicks(t)
	var m tea.Model = sizedApp(t, nil, nil, "s1").applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})

	m = typeText(m, "/name @alex bob")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(stripANSI(m.(App).noticeLine()), "renaming @alex") {
		t.Fatalf("no rename notice to time: %q", m.(App).noticeLine())
	}
	if len(*ticks) != 1 || (*ticks)[0].d != noticeMinLinger {
		t.Fatalf("armed %v; want one linger of %v", *ticks, noticeMinLinger)
	}

	m, _ = m.Update((*ticks)[0].msg)
	if row := m.(App).noticeLine(); row != "" {
		t.Errorf("the notice outlived its linger: %q", row)
	}
}

// A notice reported off the Update path is timed by the next message of any
// kind, and a message with no fresh notice arms nothing - an idle Wake keeps no
// timer running.
func TestOnlyAFreshNoticeArmsALinger(t *testing.T) {
	ticks := recordNoticeTicks(t)
	var m tea.Model = sizedApp(t, nil, nil, "s1")

	m, _ = m.Update(nudge{})
	if len(*ticks) != 0 {
		t.Fatalf("no notice, yet %d lingers armed", len(*ticks))
	}

	notice.Report("attached to @alex")
	m, _ = m.Update(nudge{})
	m, _ = m.Update(nudge{})
	if len(*ticks) != 1 {
		t.Errorf("one notice armed %d lingers, want 1", len(*ticks))
	}
	_ = m
}

// More to read stands longer: the linger is the drawn row's width at
// noticePerCell, floored at noticeMinLinger - and only what the row can show
// counts, so a long message in a narrow window gets the floor.
func TestALongerNoticeLingersLonger(t *testing.T) {
	ticks := recordNoticeTicks(t)
	long := strings.Repeat("the daemon refused that because ", 6)
	cells := lipgloss.Width(noticePrefix + strings.TrimSpace(long))

	fresh(t)
	wide := dmApp(nil, Stream{}, "s1", "alex").withSize(240, 24)
	notice.Report("%s", strings.TrimSpace(long))
	_, _ = wide.Update(nudge{})
	if want := time.Duration(cells) * noticePerCell; len(*ticks) != 1 || (*ticks)[0].d != want || want <= noticeMinLinger {
		t.Fatalf("wide window armed %v; want one linger of %v, past the floor", *ticks, want)
	}

	narrow := dmApp(nil, Stream{}, "s1", "alex").withSize(80, 24)
	notice.Report("%s", strings.TrimSpace(long))
	_, _ = narrow.Update(nudge{})
	if got := (*ticks)[len(*ticks)-1].d; got != noticeMinLinger {
		t.Errorf("80 columns armed %v; want the floor %v, since only 80 cells are drawn", got, noticeMinLinger)
	}
}

// A repeat restarts the clock: the first report's expiry finds a newer one and
// leaves it standing, and the repeat's own expiry clears it.
func TestARepeatedNoticeRestartsItsLinger(t *testing.T) {
	ticks := recordNoticeTicks(t)
	var m tea.Model = sizedApp(t, nil, nil, "s1")

	notice.Report("forking @alex…")
	m, _ = m.Update(nudge{})
	notice.Report("forking @alex…")
	m, _ = m.Update(nudge{})
	if len(*ticks) != 2 {
		t.Fatalf("two reports armed %d lingers, want 2", len(*ticks))
	}

	m, _ = m.Update((*ticks)[0].msg)
	if !strings.Contains(stripANSI(m.(App).noticeLine()), "(×2)") {
		t.Fatalf("the first expiry cleared its repeat: %q", m.(App).noticeLine())
	}
	m, _ = m.Update((*ticks)[1].msg)
	if row := m.(App).noticeLine(); row != "" {
		t.Errorf("the repeat outlived its own linger: %q", row)
	}
}

// apiFailedApp is a fleet with alex working and just failed on the API, its
// transient notice already expired - so the row shows only what stays pinned.
func apiFailedApp(t *testing.T) (App, *[]armedTick) {
	t.Helper()
	ticks := recordNoticeTicks(t)
	a := sizedApp(t, nil, nil, "s1")
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateWorking}}})
	m, _ := a.Update(frameMsg{Frame: apiErrorFrame("s1", "Session limit reached ∙ resets 5pm")})
	m, _ = m.Update((*ticks)[len(*ticks)-1].msg)
	return m.(App), ticks
}

// A session or usage limit is not a moment: the agent stays stopped until it
// is brought back, so its notice outlives its linger, gives way to a newer
// notice while that one stands, and comes back when it goes.
func TestAnAPIFailureStaysPinnedUntilItsSessionRecovers(t *testing.T) {
	a, ticks := apiFailedApp(t)
	row := stripANSI(a.noticeLine())
	if !strings.Contains(row, "@alex: Session limit reached") || !strings.Contains(row, "/reauth") {
		t.Fatalf("the failure did not stay pinned past its linger: %q", row)
	}

	notice.Report("copied 12 chars")
	m, _ := a.Update(nudge{})
	if row := stripANSI(m.(App).noticeLine()); !strings.Contains(row, "copied") {
		t.Fatalf("a newer notice did not take the row: %q", row)
	}
	m, _ = m.Update((*ticks)[len(*ticks)-1].msg)
	if row := stripANSI(m.(App).noticeLine()); !strings.Contains(row, "Session limit reached") {
		t.Errorf("the pinned failure did not return when the newer notice went: %q", row)
	}
}

// A healthy turn proves the login works again, so the pin goes with the mark.
func TestAHealthyTurnUnpinsTheFailure(t *testing.T) {
	a, _ := apiFailedApp(t)
	turn := core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "back"}
	m, _ := a.Update(frameMsg{Frame: rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &turn}})
	if pin := m.(App).pinnedNotice(); pin != "" {
		t.Errorf("a recovered session is still pinned: %q", pin)
	}
}

// /reauth parks the session but has not brought it back, so the pin stays and
// names the step still owed; the resume that follows is what clears it.
func TestTheFailureStaysPinnedThroughReauthUntilTheResume(t *testing.T) {
	a, _ := apiFailedApp(t)
	a, _ = a.reauth("")
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateParked}}})
	if pin := a.pinnedNotice(); !strings.Contains(pin, "/resume") || strings.Contains(pin, "/reauth") {
		t.Fatalf("a reauth-parked failure should point at /resume: %q", pin)
	}

	a.waking = map[string]struct{}{"s1": {}}
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})
	if pin := a.pinnedNotice(); pin != "" {
		t.Errorf("the resumed session is still pinned: %q", pin)
	}
}

// The failed turn ends and its session reports idle; that is not a recovery.
// A resume is, whichever window asked for it: this one never populated waking,
// yet the park-then-live transition still unpins (Codex review, 2026-09-24).
func TestAResumeFromAnyWindowUnpinsButAnIdleReportDoesNot(t *testing.T) {
	a, _ := apiFailedApp(t)
	report := func(state string) {
		a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: state}}})
	}

	report(rpc.StateIdle)
	if a.pinnedNotice() == "" {
		t.Fatal("the failed turn's idle report unpinned a session that has not recovered")
	}
	report(rpc.StateParked)
	report(rpc.StateIdle)
	if pin := a.pinnedNotice(); pin != "" {
		t.Errorf("another window's resume left the failure pinned: %q", pin)
	}
}

// After a reattach a parked session can be reported only in the park book
// (rpc.Status.Parked); that park counts too, so the resume after it unpins.
func TestAParkBookEntryCountsAsTheParkBeforeAResume(t *testing.T) {
	a, _ := apiFailedApp(t)
	a = a.applyStatus(&rpc.Status{Parked: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateParked}}})
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})
	if pin := a.pinnedNotice(); pin != "" {
		t.Errorf("a resume out of the park book left the failure pinned: %q", pin)
	}
}

// Several failures pin one row: the first by name, and a count of the rest. An
// ended session is nothing to recover and pins nothing.
func TestSeveralFailuresPinOneRowAndAnEndedOneNone(t *testing.T) {
	a, _ := apiFailedApp(t)
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateWorking},
		{ID: "s2", Name: "bea", State: rpc.StateWorking},
	}})
	m, _ := a.Update(frameMsg{Frame: apiErrorFrame("s2", "Session limit reached ∙ resets 5pm")})
	if pin := m.(App).pinnedNotice(); !strings.Contains(pin, "@alex") || !strings.Contains(pin, "+1 more") {
		t.Fatalf("two failures should pin @alex and a count: %q", pin)
	}

	a = m.(App).applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateEnded},
		{ID: "s2", Name: "bea", State: rpc.StateWorking},
	}})
	if pin := a.pinnedNotice(); !strings.Contains(pin, "@bea") || strings.Contains(pin, "more") {
		t.Errorf("an ended session is still pinned: %q", pin)
	}
}

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// listing is a conversation with two dispatches under it, one of each shape
// the rows have to tell apart, plus whatever else a caller wants folded.
//
// Folded through a Fleet and projected onto the DM, which is exactly what
// App.dmFor does: the fold is the Fleet's, because the sidebar draws dispatches
// for an agent nobody has opened, and the DM's copy is set for the draw.
func listing(extra ...core.Event) DM {
	f := NewFleet()
	for _, ev := range append([]core.Event{
		started("a1", "toolu_1", "Count lines in alpha.txt", "general-purpose", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading alpha.txt", "Read", 27000, 4100*time.Millisecond),
		started("b1", "toolu_2", "waiting for the sentinel", "", core.TaskShell),
	}, extra...) {
		f, _ = f.Observe(ev, "s1")
	}
	d := NewDM("s1", "alex")
	d.tasks = f.Tasks("s1")
	return d
}

// A conversation with no dispatches draws no list at all. Every pane in the
// fleet would otherwise carry a row saying nothing is running.
func TestAConversationWithNoDispatchesDrawsNoList(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 24)

	if got := d.taskView(80); got != "" {
		t.Errorf("taskView = %q, want empty", got)
	}
	if got := d.taskRowCount(); got != 0 {
		t.Errorf("taskRowCount = %d, want 0", got)
	}
}

// The list is the conversation and its dispatches: what each one is, what it is
// doing now, and what it has spent.
func TestTheListNamesEachDispatchAndWhatItHasSpent(t *testing.T) {
	out := stripANSI(listing().taskView(90))

	if !strings.Contains(out, "alex") {
		t.Errorf("the conversation's own row is missing - there is no way back:\n%s", out)
	}
	if !strings.Contains(out, "general-purpose") {
		t.Errorf("the subagent's type is missing:\n%s", out)
	}
	if !strings.Contains(out, "Reading alpha.txt") {
		t.Errorf("the row shows the prompt rather than what it is doing now:\n%s", out)
	}
	if !strings.Contains(out, "4s") || !strings.Contains(out, "27.0k tokens") {
		t.Errorf("the elapsed time and token count are missing:\n%s", out)
	}
	if !strings.Contains(out, "waiting for the sentinel") {
		t.Errorf("the shell is not listed - it is running and worth seeing:\n%s", out)
	}
}

// One row per dispatch plus the conversation's own, and taskRowCount says so
// without rendering anything. SetSize asks it on every re-lay, so it must not
// be a draw.
func TestTheRowCountMatchesWhatIsDrawn(t *testing.T) {
	d := listing()

	want := len(strings.Split(strings.TrimRight(stripANSI(d.taskView(90)), "\n"), "\n"))
	if got := d.taskRowCount(); got != want {
		t.Errorf("taskRowCount = %d, want %d - the chrome would be sized wrong by the difference", got, want)
	}
	if want != 3 {
		t.Errorf("drew %d rows, want 3: the conversation and its two dispatches", want)
	}
}

// The list is chrome, so its rows come out of the transcript's height. A pane
// that draws rows it did not budget for is one row too tall, and a frame taller
// than its terminal scrolls the alt screen away on every draw.
func TestTheListIsBudgetedAsChrome(t *testing.T) {
	bare := NewDM("s1", "alex").SetSize(80, 24)
	withRows := listing().SetSize(80, 24)

	// Without this the whole test passes on 0 - 0 == 0, which is what it did
	// while the fold was being moved off DM and the builder still drove it
	// through Append.
	if withRows.taskRowCount() == 0 {
		t.Fatal("the listing has no rows, so this test is measuring nothing")
	}
	if got := withRows.chromeHeight() - bare.chromeHeight(); got != withRows.taskRowCount() {
		t.Errorf("chrome grew by %d for %d rows", got, withRows.taskRowCount())
	}
	if got := lipgloss.Height(withRows.View(80, 24)); got != 24 {
		t.Errorf("View is %d rows tall, want exactly 24", got)
	}
}

// A finished dispatch keeps its row and says so, because it is the one most
// likely to be worth reading.
func TestAFinishedDispatchSaysSoInItsRow(t *testing.T) {
	d := listing(ended("a1", core.TaskDone))

	if out := stripANSI(d.taskView(90)); !strings.Contains(out, "Reading alpha.txt") {
		t.Errorf("the finished dispatch left the list:\n%s", out)
	}
}

// A halted dispatch is not a finished one. The glyphs are one per status and
// one status per glyph, so nothing can quietly report the two as the same.
func TestEveryTaskStatusHasItsOwnGlyph(t *testing.T) {
	seen := map[string]core.TaskStatus{}
	for _, s := range []core.TaskStatus{core.TaskRunning, core.TaskDone, core.TaskStopped, core.TaskStatusUnknown} {
		g := taskGlyph(s)
		if g == "" {
			t.Errorf("%q has no glyph - a row with none reads as a task with no state", s)
		}
		if other, clash := seen[g]; clash {
			t.Errorf("%q and %q share the glyph %q: a halted subagent would report as a finished one", s, other, g)
		}
		seen[g] = s
	}
}

// Which row is open, drawn rather than inferred. Somebody reading a subagent
// has to be able to see which one they are inside.
func TestTheOpenDispatchIsMarkedInTheList(t *testing.T) {
	d := listing()

	closed := stripANSI(d.taskView(90))
	open := stripANSI(d.Viewing("toolu_1").taskView(90))
	if closed == open {
		t.Error("the list is identical whether a dispatch is open or not: nothing says where the reader is")
	}
}

// Narrow panes are the common case with four columns on screen, and the row
// must never be wider than the pane it is drawn in.
func TestARowNeverOutgrowsItsPane(t *testing.T) {
	for _, w := range []int{20, 30, 40, 60, 80, 120} {
		out := stripANSI(listing().taskView(w))
		for _, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: a row measured %d columns: %q", w, got, line)
			}
		}
	}
}

// What a dispatch is doing outranks how long it has taken: the meta is dropped
// before the description is cut, because one says what is happening and the
// other says how long it has been happening.
func TestTheDescriptionOutlivesTheMetaInANarrowPane(t *testing.T) {
	wide := stripANSI(listing().taskView(90))
	narrow := stripANSI(listing().taskView(34))

	if !strings.Contains(wide, "27.0k tokens") {
		t.Fatalf("the wide row is missing its meta, so this test is measuring nothing:\n%s", wide)
	}
	if strings.Contains(narrow, "27.0k tokens") {
		t.Errorf("the meta survived into a pane too narrow for it:\n%s", narrow)
	}
	if !strings.Contains(narrow, "Reading") {
		t.Errorf("the description was cut before the meta was dropped:\n%s", narrow)
	}
}

// The conversation's own row wears its state's glyph, the same one the roster
// draws for it - one visual vocabulary, not two.
func TestTheConversationsRowWearsItsOwnStateGlyph(t *testing.T) {
	d := listing()
	d.Agent = Agent{State: rpc.StateWorking}

	if out := stripANSI(d.taskView(90)); !strings.Contains(out, glyphOf(rpc.StateWorking)) {
		t.Errorf("the conversation's row does not carry its state:\n%s", out)
	}
}

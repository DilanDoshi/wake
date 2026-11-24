package ui

// The jump box keeps the height it opened with while the query narrows: fewer
// matches are fewer rows inside it, with blank rows under them, so the room above
// does not jump on every keystroke and the composer under it does not move.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// boxSpot is where the box's top edge, filter line and key line sit in the frame,
// and the rows under it: the composer the box must not move.
type boxSpot struct {
	top, filter, keys int
	frame             int
	below             string
}

func spotOf(t testing.TB, a App) boxSpot {
	t.Helper()
	rows := jumpPanes(a)
	s := boxSpot{top: rowHaving(rows, "jump to agent"), filter: rowHaving(rows, "⌕"), keys: rowHaving(rows, "↑↓ move"), frame: len(rows)}
	if s.top < 0 || s.filter < 0 || s.keys < 0 {
		t.Fatalf("the box is not drawn whole (top %d, filter %d, keys %d):\n%s", s.top, s.filter, s.keys, strings.Join(rows, "\n"))
	}
	s.below = strings.Join(rows[s.keys+1:], "\n")
	return s
}

// numberedFleet is n idle agents named a1..an.
func numberedFleet(t testing.TB, w, h, n int) App {
	t.Helper()
	specs := make([]jumpSpec, n)
	for i := range specs {
		specs[i] = jumpSpec{name: fmt.Sprintf("a%d", i+1)}
	}
	return jumpApp(t, w, h, specs...)
}

func TestTheBoxKeepsItsHeightWhileTheQueryNarrows(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	want := spotOf(t, a)
	for _, typed := range []string{"b", "l", "u", "e", "x"} { // blueFleet: 8, then 6, 5, 5, 3, 0 matches
		a = jumpTyped(a, typed)
		if got := spotOf(t, a); got != want {
			t.Fatalf("after typing up to %q the box sits at %+v, want %+v (a box that shrinks moves the room above it)", a.jumpPicker.Query, got, want)
		}
	}
	for range 5 { // and ⌫ widening it again
		a, _ = pressKey(a, kmsg(tea.KeyBackspace))
		if got := spotOf(t, a); got != want {
			t.Fatalf("after ⌫ to %q the box sits at %+v, want %+v", a.jumpPicker.Query, got, want)
		}
	}
}

func TestAFleetSmallerThanTheWindowKeepsTheBoxItsOwnHeight(t *testing.T) {
	a := jumpOpened(t, numberedFleet(t, 140, 40, 3))
	want := spotOf(t, a)
	if got := want.keys - want.filter; got != 1+3+1 { // the rule, three results, the lower edge
		t.Fatalf("three agents open a box %d rows from filter to keys, want the rule and three results", got)
	}
	for _, q := range []string{"a1", "a1x"} {
		if got := spotOf(t, jumpTyped(a, q)); got != want {
			t.Errorf("%q: the box sits at %+v, want %+v", q, got, want)
		}
	}
}

// pastTheWindow is the count a window says of the rows it leaves out.
var pastTheWindow = regexp.MustCompile(`\+\d+ (above|below)`)

func rowMatching(rows []string, re *regexp.Regexp) int {
	return slices.IndexFunc(rows, re.MatchString)
}

func TestTheCountsStayWhenTheBoxKeepsItsHeight(t *testing.T) {
	a := jumpOpened(t, numberedFleet(t, 140, 40, 12))
	rows := jumpPanes(a)
	if rowHaving(rows, "12 of 12") < 0 || rowHaving(rows, "+4 below") < 0 {
		t.Fatalf("twelve agents in an eight-row window say neither 12 of 12 nor +4 below:\n%s", strings.Join(rows, "\n"))
	}
	narrow := jumpTyped(a, "a1") // a1 a10 a11 a12
	rows = jumpPanes(narrow)
	if rowHaving(rows, "4 of 12") < 0 {
		t.Errorf("four matches do not say 4 of 12:\n%s", strings.Join(rows, "\n"))
	}
	if rowMatching(rows, pastTheWindow) >= 0 {
		t.Errorf("four matches in an eight-row window claim rows past it:\n%s", strings.Join(rows, "\n"))
	}
	none := jumpTyped(a, "zzz")
	rows = jumpPanes(none)
	if rowHaving(rows, "0 of 12") < 0 || rowHaving(rows, "no agent matches") < 0 {
		t.Errorf("a filter nothing finds does not say 0 of 12 and no match inside the box:\n%s", strings.Join(rows, "\n"))
	}
	if got, want := spotOf(t, none), spotOf(t, a); got != want {
		t.Errorf("the no-match box sits at %+v, want %+v", got, want)
	}
}

// At every window size the box that fits is the same height whatever is typed, and
// never taller than the pane allows.
func TestTheBoxKeepsItsHeightAtEverySize(t *testing.T) {
	opened := 0
	for _, w := range []int{70, 100, 200} {
		for h := 6; h <= 46; h++ {
			a := blueFleet(t, w, h)
			a, _ = pressKey(a, kmsg(tea.KeyCtrlF))
			if !a.jumpPicker.Open() {
				continue
			}
			opened++
			want := lipgloss.Height(a.View())
			wantRows := boxRows(t, a)
			for _, q := range []string{"b", "bl", "blu", "blux", strings.Repeat("zq", 20)} {
				a.jumpPicker.Query = q
				a = a.jumpHomed()
				if got := lipgloss.Height(a.View()); got != want || got > h {
					t.Fatalf("%dx%d %q: the frame is %d rows, want %d (window %d)", w, h, q, got, want, h)
				}
				if got := boxRows(t, a); got != wantRows {
					t.Fatalf("%dx%d %q: the box draws %d rows, want %d", w, h, q, got, wantRows)
				}
			}
		}
	}
	if opened < 20 {
		t.Fatalf("the sweep opened the box %d times: it is not sweeping", opened)
	}
}

// boxRows is the rows the box block itself takes, found where the pane draws it.
func boxRows(t testing.TB, a App) int {
	t.Helper()
	w, h, ok := a.paneSize(a.focus)
	if !ok {
		t.Fatal("no pane holds the keys")
	}
	room := a.menuRoom(a.focus, w, h)
	return lipgloss.Height(a.jumpBox(w, room))
}

func TestResumePickerKeepsItsHeightWhileTheQueryNarrows(t *testing.T) {
	var rows []resumeRow
	for i := range 7 {
		rows = append(rows, resumeRow{ID: fmt.Sprintf("%08d-0000-4000-8000-000000000000", i), Title: fmt.Sprintf("session %d", i), Dir: "/work", Resumable: true})
	}
	for maxRows := 1; maxRows <= 40; maxRows++ {
		want := lipgloss.Height(ResumePicker{Rows: rows}.View(100, maxRows))
		for _, q := range []string{"s", "session 3", "session 31", "zzz"} { // 7, 1, 0, 0 matches
			got := lipgloss.Height(ResumePicker{Rows: rows, Query: q}.View(100, maxRows))
			if got != want {
				t.Fatalf("%d rows, query %q: the picker draws %d rows, want the %d it opened with:\n%s",
					maxRows, q, got, want, stripANSI(ResumePicker{Rows: rows, Query: q}.View(100, maxRows)))
			}
		}
	}
}

func TestResumePickerOfOneSessionKeepsItsHeightToo(t *testing.T) {
	rows := []resumeRow{{ID: irisID, Name: "iris", Dir: "/work", Resumable: true}}
	want := lipgloss.Height(ResumePicker{Rows: rows}.View(100, 30))
	for _, q := range []string{"iris", "nobody"} {
		if got := lipgloss.Height(ResumePicker{Rows: rows, Query: q}.View(100, 30)); got != want {
			t.Errorf("%q: %d rows, want %d", q, got, want)
		}
	}
	no := stripANSI(ResumePicker{Rows: rows, Query: "nobody"}.View(100, 30))
	if !strings.Contains(no, "no session matches") || !strings.Contains(no, "no match of 1") {
		t.Errorf("the no-match picker does not say so inside the same box:\n%s", no)
	}
}

// The height is the roster's, not the matches': agents arriving is a report, which
// may move the box, and a keystroke never does.
func TestARosterChangeMovesTheBoxAndATypedKeyDoesNot(t *testing.T) {
	a := jumpOpened(t, numberedFleet(t, 140, 40, 3))
	before := spotOf(t, a)
	st := rpc.Status{Running: true}
	for i := 1; i <= 5; i++ {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: fmt.Sprintf("s%d", i), Name: fmt.Sprintf("a%d", i)})
	}
	b := a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	after := spotOf(t, b)
	if after.top >= before.top {
		t.Errorf("two agents arrived and the box still starts at row %d, was %d", after.top, before.top)
	}
	if typed := spotOf(t, jumpTyped(b, "a1")); typed != after {
		t.Errorf("a typed query moved the box from %+v to %+v", after, typed)
	}
}

// A filter line wider than the box would wrap and make the box taller, so the line is
// cut to the box with the caret kept in view: the box is one height for every query
// and caret in every width a pane can have.
func TestTheBoxKeepsItsHeightAndTheCaretInANarrowPane(t *testing.T) {
	long := []string{"", "t", "blu", strings.Repeat("w", queryMax), strings.Repeat("宽", queryMax/2), strings.Repeat("😀", queryMax/2)}
	a := jumpOpened(t, blueFleet(t, 140, 40))
	for width := minBlockWidth; width <= 70; width++ {
		want := lipgloss.Height(a.jumpBox(width, 24))
		for _, q := range long {
			for _, back := range []int{0, len([]rune(q)) / 2, len([]rune(q))} {
				a.jumpPicker.Query, a.jumpPicker.Back = q, back
				box := a.jumpBox(width, 24)
				if got := lipgloss.Height(box); got != want {
					t.Fatalf("width %d, %q with the caret %d from the end: the box is %d rows, want %d:\n%s", width, q, back, got, want, ansi.Strip(box))
				}
				if w := lipgloss.Width(box); w > max(width, minBlockWidth) {
					t.Fatalf("width %d, %q: the box is %d cells wide", width, q, w)
				}
				if filter := strings.Split(ansi.Strip(box), "\n")[1]; !strings.Contains(filter, jumpCaret) {
					t.Fatalf("width %d, %q with the caret %d from the end: the caret is cut off the filter line %q", width, q, back, filter)
				}
			}
		}
	}
}

func TestTheResumeSearchKeepsItsHeightAndTheCaretInANarrowPane(t *testing.T) {
	rows := twoRows()
	for width := minBlockWidth; width <= 70; width++ {
		want := lipgloss.Height(ResumePicker{Rows: rows}.View(width, 30))
		for _, q := range []string{"", "t", "iri", strings.Repeat("w", queryMax), strings.Repeat("宽", queryMax/2)} {
			for _, back := range []int{0, len([]rune(q)) / 2, len([]rune(q))} {
				v := ResumePicker{Rows: rows, Query: q, Back: back}.View(width, 30)
				if got := lipgloss.Height(v); got != want {
					t.Fatalf("width %d, %q: the picker is %d rows, want %d:\n%s", width, q, got, want, ansi.Strip(v))
				}
				if !strings.Contains(ansi.Strip(v), jumpCaret) {
					t.Fatalf("width %d, %q with the caret %d from the end: the caret is cut off:\n%s", width, q, back, ansi.Strip(v))
				}
			}
		}
	}
}

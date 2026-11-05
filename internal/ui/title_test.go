package ui

// The terminal's tab title is the fleet's name (the owner's 2026-11-01 ask),
// written through the terminal writer: once when a window opens, again when
// the fleet is renamed, never per frame, and nothing a terminal acts on rides
// in it. See title.go.

import (
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// titleSet is the OSC 2 a terminal reads a title from.
func titleSet(name string) string { return "\x1b]2;" + name + "\x07" }

// titlesWritten is how many titles out was handed.
func titlesWritten(out *syncBuffer) int { return strings.Count(out.String(), "\x1b]2;") }

// nudged drives one message through Update and runs everything it returned,
// the way Bubble Tea would. A size is the first message every window gets.
func nudged(t *testing.T, a App) App {
	t.Helper()
	m, cmd := a.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	var wg sync.WaitGroup
	runAll(cmd, &wg)
	allFinished(t, &wg)
	return m.(App)
}

// titledRoom is a window opened on a fleet called name, with a terminal to
// write to.
func titledRoom(t *testing.T, out *syncBuffer, name string) App {
	t.Helper()
	fresh(t)
	return NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{Running: true, Fleet: name}).WithOutput(out)
}

// fleetReport is a push naming the fleet, as every report does.
func fleetReport(name string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Fleet: name}}
}

// A window opened on a fleet titles the terminal with the fleet's name on its
// first message - the room, and a conversation beside it alike.
func TestAWindowTitlesTheTerminalWithTheFleetsName(t *testing.T) {
	out := &syncBuffer{}
	a := nudged(t, titledRoom(t, out, "atlas"))
	if got := out.String(); got != titleSet("atlas") {
		t.Errorf("the room's first message wrote %q, want %q", got, titleSet("atlas"))
	}
	if a.titled != "atlas" {
		t.Errorf("the window remembers the title as %q, want atlas", a.titled)
	}

	fresh(t)
	dm := &syncBuffer{}
	d := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").WithOutput(dm)
	d = d.applyFrame(fleetReport("atlas"))
	nudged(t, d)
	if got := dm.String(); got != titleSet("atlas") {
		t.Errorf("a conversation window wrote %q, want %q", got, titleSet("atlas"))
	}
}

// Two windows on one fleet both title their terminals, and a rename from
// either retitles both: every window folds the report that names the new fleet.
func TestEveryWindowOnTheFleetIsRetitledByARename(t *testing.T) {
	outs := []*syncBuffer{{}, {}}
	var windows []App
	for _, out := range outs {
		windows = append(windows, nudged(t, titledRoom(t, out, "atlas")))
	}
	for i := range windows {
		windows[i] = windows[i].applyFrame(fleetReport("dev"))
		notice.Reset() // the rename's announcement is fleetname_test.go's; its linger is not this test's
		windows[i] = nudged(t, windows[i])
	}
	for i, out := range outs {
		if got, want := out.String(), titleSet("atlas")+titleSet("dev"); got != want {
			t.Errorf("window %d wrote %q, want %q", i+1, got, want)
		}
		if windows[i].titled != "dev" {
			t.Errorf("window %d remembers the title as %q, want dev", i+1, windows[i].titled)
		}
	}
}

// The unnamed fleet is titled by the word Wake uses for it everywhere, and a
// socket set by hand outside ~/.wake/fleets names no fleet, so the title is
// Wake's own name.
func TestTheUnnamedFleetAndAHandSetSocketHaveATitle(t *testing.T) {
	for _, tc := range []struct{ fleet, want string }{
		{fleet: "default", want: "default"},
		{fleet: "", want: "wake"},
	} {
		out := &syncBuffer{}
		nudged(t, titledRoom(t, out, tc.fleet))
		if got := out.String(); got != titleSet(tc.want) {
			t.Errorf("a fleet reported as %q wrote %q, want %q", tc.fleet, got, titleSet(tc.want))
		}
	}
}

// Nothing a terminal acts on rides in the title: a BEL or an ESC would end the
// sequence or open another, so every control character is dropped, a byte that
// is not UTF-8 with them, and a name with nothing left falls back to Wake's own.
func TestATitleCarriesNothingATerminalActsOn(t *testing.T) {
	for _, tc := range []struct{ fleet, want string }{
		{fleet: "sing-dev", want: "sing-dev"},
		{fleet: "at\x1blas", want: "atlas"},
		{fleet: "at\x07las", want: "atlas"},
		{fleet: "at\u009blas", want: "atlas"},
		{fleet: "at\x9blas", want: "atlas"},
		{fleet: "at\x7flas", want: "atlas"},
		{fleet: "at\nlas", want: "atlas"},
		{fleet: "\x1b]0;x\x07", want: "]0;x"},
		{fleet: "\x1b\x07", want: "wake"},
	} {
		if got := tabTitle(tc.fleet); got != tc.want {
			t.Errorf("tabTitle(%q) = %q, want %q", tc.fleet, got, tc.want)
		}
	}
	out := &syncBuffer{}
	nudged(t, titledRoom(t, out, "at\x07las\x1b[2J"))
	if got := out.String(); got != titleSet("atlas[2J") {
		t.Errorf("a hostile fleet name wrote %q, want %q", got, titleSet("atlas[2J"))
	}
}

// The title is written once per change, never per frame: a window that draws
// a hundred times and folds the same report again writes nothing more, and a
// frame itself never carries it.
func TestTheTitleIsWrittenOncePerChangeNeverPerFrame(t *testing.T) {
	out := &syncBuffer{}
	a := nudged(t, titledRoom(t, out, "atlas"))
	for range 20 {
		a = a.applyFrame(fleetReport("atlas"))
		a = nudged(t, a)
		_ = a.View()
	}
	if n := titlesWritten(out); n != 1 {
		t.Errorf("an unchanged fleet was titled %d times, want once", n)
	}
	if strings.Contains(a.View(), "\x1b]2;") {
		t.Error("a frame carries the title; only the terminal writer may")
	}
	a = a.applyFrame(fleetReport("dev"))
	notice.Reset()
	a = nudged(t, a)
	for range 5 {
		a = nudged(t, a)
	}
	if got, want := out.String(), titleSet("atlas")+titleSet("dev"); got != want {
		t.Errorf("a rename wrote %q in all, want %q", got, want)
	}
}

// A hand-over gives the terminal to a child for as long as it runs, and the
// child may have titled it; the title is written again when it comes back,
// whether or not the sign-in finished.
func TestTheTitleIsWrittenAgainAfterAHandOver(t *testing.T) {
	for _, m := range []mcpSignedInMsg{
		{Session: "s1", Server: "x", Err: errors.New("cancelled")},
		{Session: "s1", Server: "x"},
	} {
		out := &syncBuffer{}
		a := nudged(t, titledRoom(t, out, "atlas"))
		_, cmd := a.mcpSignedIn(m)
		var wg sync.WaitGroup
		runAll(cmd, &wg)
		allFinished(t, &wg)
		if got, want := out.String(), titleSet("atlas")+titleSet("atlas"); !strings.HasPrefix(got, want) {
			t.Errorf("after a hand-over (err %v) the terminal got %q, want it to begin %q", m.Err, got, want)
		}
	}
}

// Two title commands can run at once - a rename's behind a hand-over's
// re-assert - and the terminal must keep the newer: an older write that would
// land after it is skipped, in the order the commands were asked for.
func TestAnOlderTitleNeverLandsAfterANewerOne(t *testing.T) {
	out := &syncBuffer{}
	older := writeTitle(out, "atlas")
	newer := writeTitle(out, "dev")
	_ = newer()
	_ = older()
	if got := out.String(); got != titleSet("dev") {
		t.Errorf("the terminal got %q, want only %q", got, titleSet("dev"))
	}
	out = &syncBuffer{}
	a := nudged(t, titledRoom(t, out, "dev"))
	_, cmd := a.mcpSignedIn(mcpSignedInMsg{Session: "s1", Server: "x", Err: errors.New("cancelled")})
	a = a.applyFrame(fleetReport("ops"))
	notice.Reset()
	nudged(t, a) // the rename's write lands first
	var wg sync.WaitGroup
	runAll(cmd, &wg) // then the hand-over's older re-assert
	allFinished(t, &wg)
	if got, want := out.String(), titleSet("dev")+titleSet("ops"); got != want {
		t.Errorf("after a re-assert and a rename the terminal got %q, want %q", got, want)
	}
}

// A window with no terminal to write to titles nothing and remembers nothing,
// so one given a terminal later still titles it.
func TestNoTerminalNoTitle(t *testing.T) {
	fresh(t)
	a := nudged(t, NewRoomApp(newRecorder(t), Stream{}, &rpc.Status{Running: true, Fleet: "atlas"}))
	if a.titled != "" {
		t.Fatalf("a window with no terminal recorded the title %q", a.titled)
	}
	out := &syncBuffer{}
	nudged(t, a.WithOutput(out))
	if got := out.String(); got != titleSet("atlas") {
		t.Errorf("given a terminal later, the window wrote %q, want %q", got, titleSet("atlas"))
	}
}

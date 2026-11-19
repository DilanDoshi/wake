//go:build unix

package main

// The terminal's focus reports (DECSET 1004, asked for by tea.WithReportFocus)
// through the kill switch's pipe: whole, never as keys, never in the way of the
// emergency exit, and off around a child that owns the terminal.

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

const (
	reportGained = "\x1b[I"
	reportLost   = "\x1b[O"
	wheelReport  = "\x1b[<65;10;10M"
	clickReports = "\x1b[<0;10;10M\x1b[<0;10;10m" // a press and its release
)

// Bubble Tea v1.3.10 decodes a focus report only as all that is left of a read, so
// one sharing a read with what follows was dropped - and a click that focuses a
// window sends its report and its press together. focusLast puts the report at the
// end of such a read, and of several the last is the state. Anything else sharing
// a read is left alone: a paste, an Escape keypress and Shift+Up (ESC [ O A) are not
// ours to reorder.
//
// Mutation check: returning chunk unchanged fails every moved case; moving a report
// whatever shares its read fails the left-alone ones.
func TestFocusLastMovesTheReportBehindMouseReportsAndNothingElse(t *testing.T) {
	paste := "\x1b[200~abc\x1b[201~"
	for name, tc := range map[string]struct{ in, want string }{
		"none":                           {"abc", "abc"},
		"a mouse report":                 {wheelReport, wheelReport},
		"alone":                          {reportLost, reportLost},
		"already last":                   {wheelReport + reportGained, wheelReport + reportGained},
		"first":                          {reportLost + wheelReport, wheelReport + reportLost},
		"middle":                         {wheelReport + reportGained + wheelReport, wheelReport + wheelReport + reportGained},
		"the last of two":                {reportLost + wheelReport + reportGained, wheelReport + reportGained},
		"the last of two, lost":          {reportGained + wheelReport + reportLost, wheelReport + reportLost},
		"two with nothing between":       {reportLost + reportGained, reportGained},
		"the click that focused it":      {reportGained + clickReports, clickReports + reportGained},
		"typing is left alone":           {"a" + reportGained + "b", "a" + reportGained + "b"},
		"a paste is left alone":          {reportGained + paste, reportGained + paste},
		"an escape is left alone":        {reportLost + "\x1b", reportLost + "\x1b"},
		"Shift+Up is left alone":         {"\x1b[OA", "\x1b[OA"},
		"Shift+Up behind a mouse report": {wheelReport + "\x1b[OB", wheelReport + "\x1b[OB"},
		"urxvt Ctrl+Up is left alone":    {"\x1b[Oa", "\x1b[Oa"},
		"an unfinished mouse report":     {reportLost + "\x1b[<65;1", reportLost + "\x1b[<65;1"},
		"an X10 mouse report":            {reportGained + "\x1b[M#!!", reportGained + "\x1b[M#!!"},
		"not quite a mouse report":       {reportGained + "\x1b[<x", reportGained + "\x1b[<x"},
		"a report each side of a click":  {reportLost + clickReports + reportGained, clickReports + reportGained},
		"an unfinished CSI":              {"a\x1b[", "a\x1b["},
	} {
		t.Run(name, func(t *testing.T) {
			if got := string(focusLast([]byte(tc.in))); got != tc.want {
				t.Errorf("focusLast(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// What a real Bubble Tea decodes off the pipe: a report beside a click's mouse
// reports arrives as a report, and the neighbours as they were. With typed text
// beside it the report is dropped, as it always was, and nothing it leaves is typed.
//
// Mutation check: aligning reads without focusLast fails every mouse neighbour,
// by stalling for the report that never decodes.
func TestAFocusReportReachesBubbleTeaWhateverSharesItsRead(t *testing.T) {
	for name, tc := range map[string]struct {
		input                string
		focus, blur, mice    int
		keys                 string
		wantReports, wantKey int
	}{
		"lost alone":                  {input: reportLost, blur: 1, wantReports: 1},
		"gained alone":                {input: reportGained, focus: 1, wantReports: 1},
		"lost, then a wheel":          {input: reportLost + wheelReport, blur: 1, mice: 1, wantReports: 1},
		"a wheel, then lost":          {input: wheelReport + reportLost, blur: 1, mice: 1, wantReports: 1},
		"gained, then two wheels":     {input: reportGained + wheelReport + wheelReport, focus: 1, mice: 2, wantReports: 1},
		"gained between wheels":       {input: wheelReport + reportGained + wheelReport, focus: 1, mice: 2, wantReports: 1},
		"gained, then its click":      {input: reportGained + clickReports, focus: 1, mice: 2, wantReports: 1},
		"typing around a loss":        {input: "x" + reportLost + "y", keys: "xy", wantKey: 2}, // left alone: dropped as before, nothing typed
		"lost and gained again":       {input: reportLost + reportGained, focus: 1, wantReports: 1},
		"gained and lost again":       {input: reportGained + reportLost, blur: 1, wantReports: 1},
		"a loss behind a wheel flood": {input: strings.Repeat(wheelReport, 40) + reportLost, blur: 1, mice: 40, wantReports: 1},
	} {
		t.Run(name, func(t *testing.T) {
			got := decodeThroughThePipe(t, tc.input, func(d decodedModel) bool {
				return d.focus+d.blur >= tc.wantReports && d.mice >= tc.mice && len(d.keys) >= tc.wantKey
			})
			if got.focus != tc.focus || got.blur != tc.blur || got.mice != tc.mice || strings.Join(got.keys, "") != tc.keys {
				t.Errorf("decoded focus=%d blur=%d mice=%d keys=%q, want focus=%d blur=%d mice=%d keys=%q",
					got.focus, got.blur, got.mice, got.keys, tc.focus, tc.blur, tc.mice, tc.keys)
			}
		})
	}
}

// Shift+Up in the DECCKM form shares three bytes with a focus report, and a paste
// can follow one in a read. Both arrive as what they are, with focusLast in the way.
//
// Mutation check: a focusLast that moved any report fails both: the arrow arrives
// as the rune A, the paste with the report typed into it.
func TestAKeyOrAPasteBesideAReportPrefixIsNotMangled(t *testing.T) {
	got := decodeThroughThePipe(t, "\x1b[OA", func(d decodedModel) bool { return len(d.keys) >= 1 })
	if strings.Join(got.keys, ",") != "shift+up" || got.blur != 0 {
		t.Errorf("ESC [ O A decoded as keys %q and %d blurs, want shift+up alone", got.keys, got.blur)
	}

	body := strings.Repeat("a", 400) // past one read, so the paste's end marker comes in a later one
	got = decodeThroughThePipe(t, reportGained+"\x1b[200~"+body+"\x1b[201~", func(d decodedModel) bool { return len(d.keys) >= 1 })
	if want := "[" + body + "]"; strings.Join(got.keys, "") != want {
		t.Errorf("a paste behind a focus report decoded as %q, want it whole", got.keys)
	}
}

// A focus report is the terminal's, not the operator's: it neither completes the
// ⌃C⌃C pair nor breaks one. The wedged window this exists for may be one the
// operator has just clicked back into, and what that sends is a report.
//
// Mutation check: counting a report as a keystroke fails the second case.
func TestAFocusReportNeitherStartsNorStopsTheEmergencyExit(t *testing.T) {
	now := time.Now()
	for name, tc := range map[string][]string{
		"before the pair": {reportGained, "\x03\x03"},
		"between the two": {"\x03", reportLost, "\x03"},
		"inside one read": {"\x03" + reportGained + "\x03"},
		"after the first": {"\x03" + reportGained, "\x03"},
		"either side":     {reportLost + "\x03" + reportGained + "\x03" + reportLost},
	} {
		t.Run(name, func(t *testing.T) {
			var k killTrigger
			fired := false
			for _, read := range tc {
				k, fired = k.saw([]byte(read), now)
			}
			if !fired {
				t.Errorf("reads %q did not fire the emergency exit", tc)
			}
		})
	}
	// And the control: anything that is input still disarms.
	var k killTrigger
	k, _ = k.saw([]byte("\x03"), now)
	if _, fired := k.saw([]byte(wheelReport+"\x03"), now); fired {
		t.Error("a mouse report between two ⌃C stopped disarming them")
	}
	if _, fired := k.saw([]byte("\x1b[OA\x03"), now); fired {
		t.Error("Shift+Up between two ⌃C was taken for a focus report")
	}
}

// The pump hands a report to Bubble Tea whole, and the emergency exit still fires
// behind one - on the real pump, over a real pty, as Bubble Tea is handed it.
//
// Mutation check: a pump that held the report (alignedCut treating its final
// byte as unfinished) fails the first wait; one that dropped it, the second.
func TestTheKillSwitchPassesFocusReportsThroughAndStillFires(t *testing.T) {
	r := newHandOverRig(t)
	for _, report := range []string{reportLost, reportGained} {
		r.type_(t, report)
		r.awaitForwarded(t, report)
	}

	r.type_(t, reportLost+"\x03"+reportGained+"\x03")
	deadline := time.Now().Add(testTimeout)
	for r.exits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r.exits.Load() != 1 {
		t.Fatalf("⌃C, a focus report and ⌃C fired the emergency exit %d times, want 1", r.exits.Load())
	}
}

// markedHandOver brackets the hand-over's Run with markers in the bytes Bubble Tea
// wrote, so their order against the mode switches is readable.
type markedHandOver struct {
	tea.ExecCommand
	out *syncBuffer
}

func (m markedHandOver) Run() error {
	_, _ = io.WriteString(m.out, "<RUN>")
	err := m.ExecCommand.Run()
	_, _ = io.WriteString(m.out, "</RUN>")
	return err
}

type handedBack struct{ err error }

type handOverModel struct{ run tea.ExecCommand }

func (m handOverModel) Init() tea.Cmd {
	return tea.Exec(m.run, func(err error) tea.Msg { return handedBack{err} })
}

func (m handOverModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(handedBack); ok {
		return m, tea.Quit
	}
	return m, nil
}

func (handOverModel) View() string { return "" }

// A child that owns the terminal gets it with focus reporting off, and Wake gets
// it back with the reporting on: otherwise `claude mcp login` is sent `^[[I` and
// `^[[O` whenever the window moves, at a prompt that echoes them.
//
// It is Bubble Tea's release and restore that do it, around the hand-over's Run;
// this pins that they keep doing so across the kill switch's own suspend and
// resume, which is what a Bubble Tea upgrade could change. It reads Bubble Tea's
// writes, not the tty the child is handed, so it cannot see what Wake's own
// hand-over might write there.
//
// Mutation check: a Bubble Tea that skipped the release (or the restore) fails the
// second (or fifth) wait.
func TestTheHandOverSwitchesFocusReportingOffForTheChildAndBackOn(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open a pty: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close(); _ = tty.Close() })
	go func() { _, _ = io.Copy(io.Discard, ptmx) }() // what the child prints has somewhere to go
	cooked, err := term.MakeRaw(tty.Fd())
	if err != nil {
		t.Fatalf("raw mode: %v", err)
	}
	pipe, feed, err := os.Pipe()
	if err != nil {
		t.Fatalf("input pipe: %v", err)
	}
	t.Cleanup(func() { _ = pipe.Close(); _ = feed.Close() })
	k := newKillSwitch(tty, tty, cooked, pipe, feed)
	k.exit = func() {}
	k.startReading()
	go k.pump()
	go k.forward()
	t.Cleanup(func() { stopPump(k) })

	out := &syncBuffer{}
	run := markedHandOver{ExecCommand: k.handOver(exec.Command("true"), "Signing in"), out: out}
	p := tea.NewProgram(handOverModel{run: run}, tea.WithInput(k.Input()), tea.WithOutput(out),
		tea.WithReportFocus(), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("the program ended with %v", err)
		}
	case <-time.After(testTimeout):
		p.Kill()
		t.Fatalf("the hand-over never came back; wrote %q", out.String())
	}

	written := out.String()
	at := 0
	for _, want := range []struct{ what, mark string }{
		{"asks the terminal to report focus", "\x1b[?1004h"},
		{"switches it off for the child", "\x1b[?1004l"},
		{"runs the child", "<RUN>"},
		{"takes the child back", "</RUN>"},
		{"switches it back on", "\x1b[?1004h"},
		{"leaves it off on exit", "\x1b[?1004l"},
	} {
		i := strings.Index(written[at:], want.mark)
		if i < 0 {
			t.Fatalf("the program never %s (%q) after byte %d of %q", want.what, want.mark, at, written)
		}
		at += i + len(want.mark)
	}
}

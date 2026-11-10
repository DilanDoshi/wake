package ui

// What a streaming fleet costs the draw loop when its frames arrive as they do:
// spread over the second, one agent's token at a time, rather than in the
// 256-frame batches BenchmarkStreamingFleetSecond folds (BUG-67). The real
// Bubble Tea program runs the real App, so a View follows every message the way
// it does on a terminal, and the run reports what that costs and what the
// operator feels: the loop's busy share, how long a token for the conversation
// on screen waits for the View that draws it, and the same for a wheel notch
// and a keystroke sent while the fleet streams.
//
// Run: go test ./internal/ui -run XXX -bench SteadyArrival -benchtime 1x -count 5

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// steadyRun is the wall time measured, after steadyWarm of the same arrival.
	steadyRun  = 3 * time.Second
	steadyWarm = 500 * time.Millisecond

	// steadyMessageTokens is one message of a turn, the corpus's mean block at
	// four characters a token; each ends in its landed block and a turn-tokens
	// figure, and the next begins with a message start.
	steadyMessageTokens = medianBlockChars / 4

	// steadyInputEvery is how often a wheel notch or a key is sent while it streams.
	steadyInputEvery = 50 * time.Millisecond
)

// steadyMeter is what a run measures. Written only by the program's goroutine.
type steadyMeter struct {
	views, batches, frames int
	busy, folding, drawing time.Duration
	tokenQueue             []time.Duration // sent to the Update that folds it
	tokenWait, wheelWait   []time.Duration
	keyWait                []time.Duration

	sent    []time.Time // when each token of the drawn conversation was sent
	pending []time.Time // tokens folded since the last View
	input   time.Time   // a wheel or key folded since the last View
	inputIs string
	last    string // the last View
}

// steadyInput is a wheel notch or a key, stamped when it was sent.
type steadyInput struct {
	msg tea.Msg
	at  time.Time
}

// steadyReset starts the measured window; steadyRead hands the meter over.
type (
	steadyReset struct{}
	steadyRead  struct{ to chan<- steadyMeter }
)

// metered is the program's model: the App as cmd/wake runs it, margins and all,
// timed.
type metered struct {
	tea.Model
	m *steadyMeter
}

func (s metered) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case steadyReset:
		sent := s.m.sent
		*s.m = steadyMeter{sent: sent}
		return s, nil
	case steadyRead:
		v.to <- *s.m
		return s, nil
	case steadyInput:
		s.m.input, s.m.inputIs, msg = v.at, inputKind(v.msg), v.msg
	case streamMsg:
		s.m.batches++
		s.m.frames += len(v.frames)
		now := time.Now()
		for _, at := range s.m.tokensIn(v.frames) {
			s.m.tokenQueue = append(s.m.tokenQueue, now.Sub(at))
			s.m.pending = append(s.m.pending, at)
		}
	}
	start := time.Now()
	next, cmd := s.Model.Update(msg)
	took := time.Since(start)
	s.m.busy += took
	if _, ok := msg.(streamMsg); ok {
		s.m.folding += took
	}
	s.Model = next
	return s, cmd
}

func (s metered) View() string {
	start := time.Now()
	v := s.Model.View()
	end := time.Now()
	s.m.busy += end.Sub(start)
	s.m.drawing += end.Sub(start)
	s.m.views++
	for _, at := range s.m.pending {
		s.m.tokenWait = append(s.m.tokenWait, end.Sub(at))
	}
	s.m.pending = s.m.pending[:0]
	if !s.m.input.IsZero() {
		if s.m.inputIs == "wheel" {
			s.m.wheelWait = append(s.m.wheelWait, end.Sub(s.m.input))
		} else {
			s.m.keyWait = append(s.m.keyWait, end.Sub(s.m.input))
		}
		s.m.input = time.Time{}
	}
	s.m.last = v
	return v
}

func inputKind(msg tea.Msg) string {
	if _, ok := msg.(tea.MouseMsg); ok {
		return "wheel"
	}
	return "key"
}

// tokensIn is when each of the drawn conversation's tokens in frames was sent. A
// fold carries several, so every word is read.
func (m *steadyMeter) tokensIn(frames []rpc.Frame) []time.Time {
	var out []time.Time
	for _, f := range frames {
		if f.SessionID != benchAgentID(0) || !partialFrame(f) {
			continue
		}
		for _, w := range strings.Fields(f.Event.Text) {
			if i, err := strconv.Atoi(strings.TrimPrefix(w, "w")); err == nil && i < len(m.sent) {
				out = append(out, m.sent[i])
			}
		}
	}
	return out
}

// steadyFleet sends benchFleetSize agents' turns at medianTokensPerSecond each,
// one token at a time, evenly spaced across the fleet, until stop closes. The
// drawn conversation's tokens are numbered words, stamped in sent as they go.
func steadyFleet(out chan<- rpc.Frame, sent []time.Time, stop <-chan struct{}) {
	every := time.Second / time.Duration(benchFleetSize*medianTokensPerSecond)
	words := strings.Fields(benchParagraph)
	told := make([]int, benchFleetSize) // tokens each agent has said
	drawn := 0
	start := time.Now()
	for k := 0; ; k++ {
		if d := time.Until(start.Add(time.Duration(k) * every)); d > 0 {
			time.Sleep(d)
		}
		select {
		case <-stop:
			return
		default:
		}
		i := k % benchFleetSize
		id := benchAgentID(i)
		if told[i]%steadyMessageTokens == 0 {
			out <- kindFrame(id, core.KindMessageStart, "")
		}
		text := words[told[i]%len(words)] + " "
		if i == 0 && drawn < len(sent) {
			text = fmt.Sprintf("w%06d ", drawn)
			sent[drawn] = time.Now()
			drawn++
		}
		out <- tokenFrame(id, text)
		told[i]++
		if told[i]%steadyMessageTokens == 0 {
			out <- kindFrame(id, core.KindAssistantText, fmt.Sprintf("Message %d of %s.", told[i]/steadyMessageTokens, id))
			out <- rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
				Kind: core.KindTurnTokens, SessionID: id, Session: &core.SessionFacts{TurnOutputTokens: told[i]},
			}}
		}
	}
}

// steadyApp is streamingApp with a live inbox: the whole fleet working, the first
// agent's conversation open beside the room.
func steadyApp(tb testing.TB, frames <-chan rpc.Frame) App {
	tb.Helper()
	fresh(tb)
	seed := &rpc.Status{Running: true}
	for i := range benchFleetSize {
		seed.Sessions = append(seed.Sessions, rpc.SessionStatus{
			ID: benchAgentID(i), Name: benchAgentName(i), Dir: "/Users/someone/code/api-v2",
			State: rpc.StateWorking,
		})
	}
	a := NewRoomApp(nil, Stream{Frames: frames, Errs: make(chan error)}, seed).WithOpenDM(benchAgentID(0), benchAgentName(0))
	a.layout.ShowRoster = true
	return a.withSize(idleTerminalWidth, idleTerminalHeight)
}

// wheelOver is a column of the window, margins included, over the drawn conversation.
func wheelOver(tb testing.TB, a App) int {
	tb.Helper()
	for x := range a.layout.Width {
		if region, col := a.layout.Hit(a.frameRegions(a.regions()), x); region == RegionPane {
			if id, _, _, ok := a.paneAt(col, 5); ok && id == benchAgentID(0) {
				return x + SideMargin
			}
		}
	}
	tb.Fatal("no column draws the open conversation")
	return 0
}

func BenchmarkStreamingFleetSteadyArrival(b *testing.B) {
	for b.Loop() {
		m := steadyRunOnce(b)
		wall := steadyRun.Seconds()
		b.ReportMetric(float64(m.views)/wall, "views/s")
		b.ReportMetric(float64(m.frames)/wall, "frames/s")
		b.ReportMetric(float64(m.frames)/float64(max(m.batches, 1)), "frames/batch")
		b.ReportMetric(100*m.busy.Seconds()/wall, "loop-busy-%")
		b.ReportMetric(steadyCPU*100/wall, "proc-cpu-%")
		b.ReportMetric(ms(m.folding)/float64(max(m.batches, 1)), "fold-ms/batch")
		b.ReportMetric(ms(m.drawing)/float64(max(m.views, 1)), "view-ms")
		b.ReportMetric(ms(percentile(m.tokenQueue, 50)), "token-queue-p50-ms")
		b.ReportMetric(ms(percentile(m.tokenWait, 50)), "token-p50-ms")
		b.ReportMetric(ms(percentile(m.tokenWait, 99)), "token-p99-ms")
		b.ReportMetric(ms(percentile(m.wheelWait, 50)), "wheel-p50-ms")
		b.ReportMetric(ms(percentile(m.wheelWait, 100)), "wheel-max-ms")
		b.ReportMetric(ms(percentile(m.keyWait, 50)), "key-p50-ms")
		b.ReportMetric(ms(percentile(m.keyWait, 100)), "key-max-ms")
	}
}

// steadyCPU is the process's CPU seconds over the last run's measured window.
var steadyCPU float64

// steadyRunOnce runs the program over a streaming fleet and returns the meter.
func steadyRunOnce(tb testing.TB) steadyMeter {
	frames := make(chan rpc.Frame, takeLimit)
	a := steadyApp(tb, frames)
	x := wheelOver(tb, a)
	meter := &steadyMeter{sent: make([]time.Time, 2*int((steadyRun+steadyWarm).Seconds()+1)*medianTokensPerSecond)}
	p := tea.NewProgram(metered{Model: Margined{Model: a}, m: meter},
		tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	p.Send(tea.WindowSizeMsg{Width: idleTerminalWidth + 2*SideMargin, Height: idleTerminalHeight})

	stop := make(chan struct{})
	go steadyFleet(frames, meter.sent, stop)
	time.Sleep(steadyWarm)
	p.Send(steadyReset{})
	cpu0 := cpuSeconds(tb)
	inputs := []tea.Msg{
		tea.MouseMsg{X: x, Y: 5, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}},
		tea.MouseMsg{X: x, Y: 5, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
		tea.KeyMsg{Type: tea.KeyBackspace},
	}
	for i := 0; i < int(steadyRun/steadyInputEvery); i++ {
		time.Sleep(steadyInputEvery)
		p.Send(steadyInput{msg: inputs[i%len(inputs)], at: time.Now()})
	}
	read := make(chan steadyMeter, 1)
	p.Send(steadyRead{to: read})
	got := <-read
	steadyCPU = cpuSeconds(tb) - cpu0
	close(stop)
	p.Quit()
	if err := <-done; err != nil {
		tb.Fatal(err)
	}
	if got.views == 0 || len(got.tokenWait) == 0 {
		tb.Fatalf("the run drew %d views and %d of the open conversation's tokens: it measured nothing", got.views, len(got.tokenWait))
	}
	return got
}

func percentile(ds []time.Duration, p int) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[min(len(s)-1, len(s)*p/100)]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

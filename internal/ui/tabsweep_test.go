package ui

// The class guard: no tab reaches the terminal from any surface the recorded
// corpus can drive. Every string an event carries has its spaces turned into
// tabs, the events are applied to a room with a conversation open and to the
// board in both of its forms, and every frame - and every row the transcript
// stores - is held to frameHoldsNoTab.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// frameHoldsNoTab reports the first row of a drawn frame that would not be
// drawn as measured: one holding a tab (no cell to ansi, up to eight to a
// terminal), or one wider than the frame.
func frameHoldsNoTab(view string, width int) (int, string, bool) {
	for i, row := range strings.Split(view, "\n") {
		if strings.ContainsRune(row, '\t') || ansi.StringWidth(row) > width {
			return i, row, false
		}
	}
	return 0, "", true
}

// tabbed turns every space of every settable string under v into a tab.
func tabbed(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(strings.ReplaceAll(v.String(), " ", "\t"))
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			tabbed(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			tabbed(v.Field(i))
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			for i := range v.Len() {
				tabbed(v.Index(i))
			}
		}
	}
}

// corpusEvents is every event the recorded stream decodes to, tabbed and filed
// under s1.
func corpusEvents(t *testing.T) []core.Event {
	t.Helper()
	files, _ := filepath.Glob("../../testdata/stream/*.jsonl")
	if len(files) == 0 {
		t.Fatal("no stream fixtures: the sweep would assert nothing")
	}
	var out []core.Event
	for _, path := range files {
		blob, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(blob), "\n") {
			evs, err := core.DecodeLine([]byte(line))
			if err != nil {
				continue
			}
			for _, ev := range evs {
				tabbed(reflect.ValueOf(&ev).Elem())
				ev.SessionID = "s1"
				out = append(out, ev)
			}
		}
	}
	return out
}

func TestNoRecordedEventLeavesATabInAFrameOrATranscript(t *testing.T) {
	events := corpusEvents(t)
	for _, size := range [][2]int{{150, 45}, {90, 30}} {
		w, h := size[0], size[1]
		a := newRoomApp(t).withSize(w, h).withAgents("alex").WithOpenDM("s1", "alex").withSize(w, h)
		seen := map[string]bool{}
		for _, ev := range events {
			was := a.dms["s1"].tr.lines.len()
			a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
			if i, row, ok := frameHoldsNoTab(a.View(), w); !ok && !seen["frame "+string(ev.Kind)] {
				seen["frame "+string(ev.Kind)] = true
				t.Errorf("%dx%d: a %s leaves frame row %d drawn unlike it is measured: %q", w, h, ev.Kind, i, row)
			}
			d := a.dms["s1"]
			for _, row := range d.tr.lines.slice(was, d.tr.lines.len()) {
				// Visible width: padding past the pane is clipped by the view, unseen.
				visible := ansi.StringWidth(strings.TrimRight(ansi.Strip(row), " "))
				if (strings.ContainsRune(row, '\t') || visible > d.tr.width) && !seen["stored "+string(ev.Kind)] {
					seen["stored "+string(ev.Kind)] = true
					t.Errorf("%dx%d: a %s stores a row drawn unlike it is measured (%d cells seen, pane %d): %q", w, h, ev.Kind, visible, d.tr.width, row)
				}
			}
		}
	}
}

// The board draws every agent's last words in its rows and its tiles; neither
// may carry a tab either. Both flatten or go through lipgloss today, so this is a
// guard against a later surface, not a reproduction of BUG-49.
func TestNoRecordedEventLeavesATabOnTheBoard(t *testing.T) {
	events := corpusEvents(t)
	a := boardApp(t)
	for _, tiled := range []bool{false, true} {
		if a.board.Tiled != tiled {
			next, _, _ := a.boardKey(tea.KeyMsg{Type: tea.KeyTab})
			a = next
		}
		for _, ev := range events {
			a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
			if i, row, ok := frameHoldsNoTab(a.View(), 120); !ok {
				t.Fatalf("tiled=%v: a %s leaves board row %d drawn unlike it is measured: %q", tiled, ev.Kind, i, row)
			}
		}
	}
}

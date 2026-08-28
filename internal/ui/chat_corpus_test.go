package ui

// What the room costs against real agent output rather than against prose
// somebody made up for a test.
//
// The whole design rests on a claim about lengths - most replies are short, a
// few are long, collapse the tail - and a claim like that is only worth
// anything measured against what agents actually said. So the fixtures here
// are the recorded corpus: every parent assistant block in
// testdata/stream/*.jsonl, decoded through core, which is exactly the set
// Fleet.Observe admits to the room.
//
// Measured over the fixtures at room widths: the median reply is one rendered
// row and the tail is a handful of tall ones (a /context dump renders to
// hundreds), which is the shape the height cap is built for. The count that
// crosses the cap depends on width - more collapse in a narrow column than at
// full width - which is the whole point, so nothing here pins an exact count;
// it pins the shape the design claims.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

const (
	// broadcastAgents is the fleet size the product is designed for.
	broadcastAgents = 30

	// collapsedShareDivisor bounds how much of a broadcast may arrive as
	// pointers rather than as content: at most one in this many.
	//
	// The design's claim is "roughly one collapsed reply per broadcast, not
	// thirty" - so the pointer is right at the tail and wrong as the primary
	// mechanism. A cap set far too low turns the room into a wall of things to
	// go and read elsewhere, which is the same failure as having no room. Four
	// is loose enough that re-recording a fixture cannot break it and tight
	// enough that a cap of a couple of rows would.
	collapsedShareDivisor = 4

	// narrowRoom is the room at 120 columns with both sidebars and a DM open -
	// the layout's marginal case. wideRoom is the same at 200 columns, which is
	// what the design brief's measurement table calls comfortable.
	narrowRoom = 52
	wideRoom   = 92
)

// corpusReplies is every parent assistant block in the recordings, in the
// order the recordings hold them.
//
// It fails rather than returning a short answer. A reader that silently found
// nothing turns every assertion below into a loop over an empty slice, which
// is the shape of a test that passes over everything it was written to check.
func corpusReplies(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../testdata/stream/*.jsonl")
	if err != nil {
		t.Fatalf("globbing the corpus: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no recorded fixtures found: every assertion here would pass over nothing")
	}
	var out []string
	for _, path := range files {
		out = append(out, repliesIn(t, path)...)
	}
	if len(out) == 0 {
		t.Fatalf("%d fixtures decoded to no agent prose at all", len(files))
	}
	return out
}

func repliesIn(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var out []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			continue // a recorded stderr line or a frame this build does not decode
		}
		for _, ev := range evs {
			// The fold's own admission rule: the agent's own prose, never a
			// subagent's, never an empty block.
			if ev.Kind == core.KindAssistantText && ev.Subagent == nil && strings.TrimSpace(ev.Text) != "" {
				out = append(out, strings.TrimSpace(ev.Text))
			}
		}
	}
	return out
}

// The cap's whole purpose is that one reply cannot take the room over. Past it,
// the cost of a reply stops depending on its height: a header, two lines of
// preview and the key that opens it, whatever the agent wrote.
//
// The corpus contains a block that renders to hundreds of rows (a /context
// dump), so this is not a hypothetical bound - without the collapse that one
// event is hundreds of rows in a pane that is twenty.
func TestNoRecordedReplyCostsMoreThanThePointerAllows(t *testing.T) {
	replies := corpusReplies(t)
	const wantRows = 1 + roomCollapseLines + 1

	for _, w := range []int{narrowRoom, wideRoom} {
		var past, tallest int
		for _, text := range replies {
			rows := renderedRows(render.Markdown(strings.TrimSpace(text), w))
			if rows <= roomInlineRows {
				continue
			}
			past++
			tallest = max(tallest, rows)
			b := roomBlock(core.Event{Kind: core.KindAssistantText, Text: text}, Agent{Name: "sydney", Label: "auth-fix"}, w, false)
			if got := strings.Count(b.text, "\n") + 1; got > wantRows {
				t.Errorf("width %d: a reply rendering %d rows drew a %d-row pointer, want at most %d. Past the cap a reply is a pointer, so its cost stops depending on its height",
					w, rows, got, wantRows)
			}
		}
		if past == 0 {
			t.Fatalf("width %d: no recorded reply renders past the %d-row cap, so this test cannot tell a room that collapses the tail from one that does not", w, roomInlineRows)
		}
		t.Logf("width %d: %d of %d recorded replies render past the %d-row cap; tallest is %d rows", w, past, len(replies), roomInlineRows, tallest)
	}
}

// A broadcast to 30 agents means 30 replies, and that is what was asked for
// rather than clutter. What would make it clutter is thirty pointers: a room
// that answers every question with "somebody has a message, go and read it"
// has stopped being a conversation.
//
// The count is not asserted exactly, on purpose. It is a property of the
// recordings, and pinning it would make re-recording a fixture a test failure
// for no design reason. What is asserted is the shape the design claims.
func TestABroadcastToThirtyAgentsCollapsesItsTailAndNotItsBody(t *testing.T) {
	replies := corpusReplies(t)
	names := []string{"sydney", "john", "alex", "peter", "mara", "kenji"}

	for _, w := range []int{narrowRoom, wideRoom} {
		r := NewRoom().SetSize(w, 40)
		r = r.Append(core.Event{Kind: core.KindUserText, Text: "everyone: pause and report where you are"}, Agent{})

		collapsed := 0
		for i := range broadcastAgents {
			// Spread across the whole corpus rather than taking the first
			// thirty. The recordings are in glob order and the alphabetically
			// early fixtures are the short probe sessions, so the first thirty
			// are almost all one-line sign-offs: against that fixture a cap that
			// collapsed nothing would look the same as the designed room, and
			// this test could not tell it from a wall of pointers.
			text := replies[i*len(replies)/broadcastAgents]
			if renderedRows(render.Markdown(strings.TrimSpace(text), w)) > roomInlineRows {
				collapsed++
			}
			r = r.Append(core.Event{Kind: core.KindAssistantText, Text: text},
				Agent{ID: names[i%len(names)], Name: names[i%len(names)], Label: "api-v2"})
		}

		t.Logf("width %d: %d replies drew %d rows, %d of them collapsed", w, broadcastAgents, r.tr.lines.len(), collapsed)
		if want := broadcastAgents / collapsedShareDivisor; collapsed > want {
			t.Errorf("width %d: %d of %d replies arrived as pointers, want at most %d. A pointer is right at the threshold and wrong as the primary mechanism - a room that is mostly links to elsewhere is not the surface it was built to be",
				w, collapsed, broadcastAgents, want)
		}
		if r.tr.lines.len() == 0 {
			t.Errorf("width %d: a broadcast to %d agents drew nothing at all", w, broadcastAgents)
		}
	}
}

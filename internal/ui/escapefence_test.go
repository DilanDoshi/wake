package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// docs/notes/bugs.md BUG-9, at the surface it was measured on.
//
// Nothing in this tree could witness this before. The two stripANSI helpers are
// SGR-only - `\x1b\[[0-9;]*m` - so an OSC 52 or a `\x1b[2J` passes straight
// through them, and the frame guards measure with ansi.StringWidth, which
// reports both as zero cells. So a test asserting "the frame fits" passed with
// an escape in every row. These assert on the bytes.
//
// The payloads are what a terminal acts on rather than what a sequence looks
// like: an introducer is the thing with power, and a test naming `\x1b[2J` and
// `\x1b]52` would be a guard somebody has to keep in step with a terminal's
// whole vocabulary.

// actsOnATerminalHere is written out rather than reached through core, for
// core's own reason: a fence narrowed by mistake would narrow the assertion
// with it.
func actsOnATerminalHere(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\u2028' || r == '\u2029'
}

// decodedHostile is the event a real child would produce, built by the airlock
// rather than by hand. Going through core.DecodeLine is the whole point: a test
// that called core.Contained itself would pass with the fence unwired, which is
// the shape of guard this project fails builds over.
func decodedHostile(t *testing.T, block string) core.Event {
	t.Helper()
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[` + block + `]}}`
	events, err := core.DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("want one event, got %d", len(events))
	}
	return events[0]
}

// hostileJSON is the payload as a child would send it: JSON escapes on the
// wire, real control bytes after decoding.
const hostileJSON = `"before\u001b]52;c;cHduZWQ=\u0007middle\u001b[2J\u001b[H\u009b2Jafter"`

func TestAnAgentsWordsCannotDriveTheTerminal(t *testing.T) {
	for _, tc := range []struct{ name, block string }{
		{"a reply", `{"type":"text","text":` + hostileJSON + `}`},
		{"thinking", `{"type":"thinking","thinking":` + hostileJSON + `}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := decodedHostile(t, tc.block)
			frame := NewDM("s1", "alex").SetSize(80, 24).Append(ev).View(80, 24)

			if i := strings.IndexFunc(frame, actsOnATerminalHere); i >= 0 {
				t.Errorf("the drawn frame keeps %q at %d", frame[i:i+1], i)
			}
			// And the words are still there: a fence that ate the message would
			// satisfy every assertion above it.
			for _, want := range []string{"before", "middle", "after"} {
				if !strings.Contains(stripANSI(frame), want) {
					t.Errorf("the fence ate %q out of the message:\n%s", want, stripANSI(frame))
				}
			}
		})
	}
}

// The room is the other surface, and it interleaves thirty agents - so a forged
// line there is a line attributed to somebody.
func TestAnAgentsWordsCannotDriveTheTerminalInTheRoom(t *testing.T) {
	ev := decodedHostile(t, `{"type":"text","text":`+hostileJSON+`}`)
	r := NewRoom().SetSize(80, 24).Append(ev, Agent{ID: "s1", Name: "alex"})

	frame := r.View(80, 24)
	if i := strings.IndexFunc(frame, actsOnATerminalHere); i >= 0 {
		t.Errorf("the room frame keeps %q at %d", frame[i:i+1], i)
	}
	for _, want := range []string{"before", "middle", "after"} {
		if !strings.Contains(stripANSI(frame), want) {
			t.Errorf("the fence ate %q out of the room line:\n%s", want, stripANSI(frame))
		}
	}
}

// The newline is exempt from containment because prose needs it, and that
// exemption has to stop at a row. A question's chip reaches the step strip,
// which is structurally one row, and the strip's own width guard cannot catch a
// two-line chip: ansi.StringWidth sums across lines rather than measuring the
// widest, so "A\nB" measures 2 and fits a strip of any width.
func TestAQuestionsChipCannotAddARowToTheStepStrip(t *testing.T) {
	if got := stripTabName("first\nsecond", 0); strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("a chip reached the strip with a line break in it: %q", got)
	}
	if got := stripTabName("first\nsecond", 0); !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("flattening the chip ate its words: %q", got)
	}
}

// The third producer, which the other two have had a test since they were
// written. A filename is external bytes and an agent writes files, so a name
// carrying an escape is a delivery route rather than a curiosity - and this is
// the one fence with no airlock and no bangRun behind it.
func TestAFilenameCannotDriveTheTerminal(t *testing.T) {
	dir := t.TempDir()
	hostile := "note\x1b]52;c;cHduZWQ=\a\x1b[2Jx.txt"
	if err := os.WriteFile(filepath.Join(dir, hostile), []byte("x"), 0o600); err != nil {
		t.Skipf("this filesystem will not hold the name: %v", err)
	}

	entries := readDirBounded(dir)
	if len(entries) != 1 {
		t.Fatalf("want one entry, got %d", len(entries))
	}
	if i := strings.IndexFunc(entries[0].name, actsOnATerminalHere); i >= 0 {
		t.Errorf("the entry keeps a character a terminal acts on at %d: %q", i, entries[0].name)
	}
	if !strings.Contains(entries[0].name, "note") || !strings.HasSuffix(entries[0].name, ".txt") {
		t.Errorf("containment ate the name: %q", entries[0].name)
	}
}

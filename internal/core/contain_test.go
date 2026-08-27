package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// docs/notes/bugs.md BUG-9. A child's own words reach the drawn frame, and they
// reached it with their escape sequences intact - so what the model wrote was
// what the terminal executed: OSC 52 sets the operator's clipboard with no
// keystroke, and CSI clears the alt screen and homes the cursor, which is enough
// to erase Wake's frame and draw a forged one in its place.
//
// The fence is here because this is where a child's bytes first become a Wake
// value, and because DecodeTranscriptLine delegates to DecodeLine - so one call
// covers a live session and a conversation read back off disk.

func TestAChildsTextCannotDriveTheTerminal(t *testing.T) {
	for _, tc := range []struct{ name, payload string }{
		{"osc 52 sets the clipboard", "\x1b]52;c;cHduZWQ=\a"},
		{"csi clears the screen and homes the cursor", "before\x1b[2J\x1b[Hafter"},
		{"a bare escape", "a\x1bb"},
		{"a carriage return redraws the row from column zero", "real\rforged"},
		{"an 8-bit CSI has no ESC in front of it", "x\u009b2Jy"},
		{"a bell", "ding\a"},
		{"DEL", "a\x7fb"},
		{"a line separator", "one\u2028two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			line := fmt.Sprintf(
				`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":%s}]}}`,
				text)
			events, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(events) == 0 {
				t.Fatal("no events decoded")
			}
			for _, ev := range events {
				if i := strings.IndexFunc(ev.Text, actsOnATerminal); i >= 0 {
					t.Errorf("decoded text keeps a character a terminal acts on, at %d: %q", i, ev.Text)
				}
			}
		})
	}
}

// actsOnATerminal is the test's own predicate, written out rather than reached
// through the implementation: a fence narrowed by mistake would narrow the
// assertion with it. internal/mcp's own guard states that rule and is why.
func actsOnATerminal(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == '\u2028' || r == '\u2029'
}

// The two exemptions, and they are the whole difference between this fence and
// the four oneLine-shaped ones already in the tree. Prose is legitimately
// multi-line - a markdown paragraph, a code fence, a tool result - and a fence
// that flattened it would collapse every one of them into a single row.
func TestContainmentKeepsTheTwoCharactersProseNeeds(t *testing.T) {
	const src = "para one\n\npara two\n\n```\n\tcode\n```"
	if got := Contained(src); got != src {
		t.Errorf("containment altered ordinary prose:\n got %q\nwant %q", got, src)
	}
}

// Substitute, never delete. The recorded ruling: a padded row measures its
// columns before containment runs, so a deletion shifts every column right of
// the character an agent chose to insert.
func TestContainmentSubstitutesRatherThanDeleting(t *testing.T) {
	const src = "a\x1b[2Jb"
	got := Contained(src)
	if len([]rune(got)) != len([]rune(src)) {
		t.Errorf("containment changed the rune count: %q -> %q", src, got)
	}
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("the escape survived: %q", got)
	}
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("containment ate the text around the escape: %q", got)
	}
}

// Totality, derived from the struct rather than from a list somebody keeps in
// step. internal/mcp's TestEveryFieldOfAFleetReportHasAProvenanceVerdict is the
// pattern: a string field added to core.Event later is a build failure until
// somebody rules on whether a child authors it.
//
// Reflection here and an explicit walker in the implementation, deliberately:
// this runs once, and DecodeLine runs at roughly 1,300 events a second across a
// fleet streaming at the corpus median.
func TestEveryStringAChildAuthorsIsContained(t *testing.T) {
	const hostile = "\x1b]52;c;x\a"
	ev := Event{}
	seeded, opaque := map[string]bool{}, map[string]bool{}
	seedStrings(reflect.ValueOf(&ev).Elem(), "Event", hostile, seeded, opaque)

	// A kind this walk cannot see through is a hole in the word "every", so it
	// has to be ruled on rather than skipped. Today that is exactly
	// Event.Tool.Input, a map[string]any that goes back to the CLI as
	// updatedInput and is drawn nowhere; a second one added later fails here
	// until somebody says which it is.
	for path := range opaque {
		if notAuthoredByTheChild[path] == "" {
			t.Errorf("%s is a kind this guard cannot walk and has no written excuse", path)
		}
	}

	checkStrings(reflect.ValueOf(ev.contained()), "Event", func(path, s string) {
		if !strings.ContainsRune(s, 0x1b) {
			return
		}
		if notAuthoredByTheChild[path] == "" {
			t.Errorf("%s keeps a child's escape sequence and has no written excuse", path)
		}
	})
	for path, why := range notAuthoredByTheChild {
		if !seeded[path] && !opaque[path] {
			t.Errorf("%s is excused as %q but is no longer reachable from Event", path, why)
		}
	}
}

// seedStrings puts hostile text in every string field it can reach, recording
// the paths it seeded so an excuse for a field that no longer exists fails too.
func seedStrings(v reflect.Value, path, with string, seeded, opaque map[string]bool) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(with)
			seeded[path] = true
		}
	case reflect.Pointer:
		if v.IsNil() && v.CanSet() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if !v.IsNil() {
			seedStrings(v.Elem(), path, with, seeded, opaque)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				seedStrings(v.Field(i), path+"."+v.Type().Field(i).Name, with, seeded, opaque)
			}
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return // json.RawMessage and friends: bytes, not a rendered string
		}
		if v.CanSet() {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			seedStrings(v.Index(0), path+"[]", with, seeded, opaque)
		}
	case reflect.Map, reflect.Interface:
		// Cannot be seeded through: a map's values are not addressable and an
		// interface has no field to set. Recorded rather than skipped - see the
		// caller.
		opaque[path] = true
	}
}

func checkStrings(v reflect.Value, path string, report func(path, s string)) {
	switch v.Kind() {
	case reflect.String:
		report(path, v.String())
	case reflect.Pointer:
		if !v.IsNil() {
			checkStrings(v.Elem(), path, report)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				checkStrings(v.Field(i), path+"."+v.Type().Field(i).Name, report)
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			checkStrings(v.Index(i), path+"[]", report)
		}
	}
}

// Every control character is in the class, checked against unicode rather than
// against the predicate's own spelling.
func TestTheContainedClassIsTheControlCharacters(t *testing.T) {
	for r := rune(0); r < 0xa0; r++ {
		if r == '\n' || r == '\t' {
			continue
		}
		if !unicode.IsControl(r) {
			continue
		}
		if got := Contained(string(r)); got != " " {
			t.Errorf("control rune %#x contained to %q, want a space", r, got)
		}
	}
}

// The fence runs on every decoded event, and a fleet streaming at the corpus
// median produces roughly 1,300 a second - so what it costs when it finds
// nothing is the number that matters. strings.Map allocates only when it
// substitutes, and no line of the recorded corpus carries a character this
// replaces.
func BenchmarkContainedOrdinaryProse(b *testing.B) {
	const prose = "Here is a paragraph of an agent's reply, with `code` and a list:\n\n" +
		"- alpha\n- beta\n- gamma\n\nand a closing sentence that runs on a little."
	b.ReportAllocs()
	for b.Loop() {
		_ = Contained(prose)
	}
}

func BenchmarkContainedHostile(b *testing.B) {
	const hostile = "before\x1b[2J\x1b[Hafter\x1b]52;c;cHduZWQ=\a and some trailing prose"
	b.ReportAllocs()
	for b.Loop() {
		_ = Contained(hostile)
	}
}

func BenchmarkDecodeLineWithTheFence(b *testing.B) {
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"a reply of about the length an agent actually writes, with a couple of clauses in it"}]}}`)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeLine(line); err != nil {
			b.Fatal(err)
		}
	}
}

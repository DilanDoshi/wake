// Reading testdata/stream. Shared by every test in this package that consults
// the recording, airlock_test.go included.
//
// This file, fixtures_subagent_test.go, fixtures_helpers_test.go,
// protocol_test.go, encode_test.go and airlock_test.go are the airlock's own
// tests, so together they are the only files besides the airlock itself that
// may name Claude's frame types - and only ever to prove it decodes and
// encodes them. session_test.go is the one further exception, for the same
// narrow reason: its fake process has to speak the wire to prove session.go
// never does.

package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureGlob = "../../testdata/stream/*.jsonl"

// transcriptGlob is claude's **on-disk** transcript, which is a different
// format from the stream: camelCase keys, record types stdout never carries,
// and no session_id. Kept apart from fixtureGlob because the golden tests
// decode every line of that one as a stream frame, which these are not.
const transcriptGlob = "../../testdata/transcript/*.jsonl"

// --- fixture helpers -------------------------------------------------------

func fixtureFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(fixtureGlob)
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no fixtures matched %s (run Task 1)", fixtureGlob)
	}
	return files
}

// transcriptFiles is the recorded on-disk corpus.
func transcriptFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(transcriptGlob)
	if err != nil {
		t.Fatalf("glob transcripts: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no transcripts matched %s", transcriptGlob)
	}
	return files
}

func fixtureLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	// Explicitly discarded: this handle is read-only, so a Close error says
	// nothing about the bytes already scanned. The scanner's own Err is
	// what decides whether the fixture was read completely, and it is
	// checked below.
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var lines []string
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return lines
}

// findFixtureLine returns the one line carrying the marker, and fails if
// there is not exactly one.
//
// Both failure modes are silent otherwise. A marker matching nothing would
// let a pin "pass" against a frame that is not in the corpus at all; a marker
// matching several would pin whichever happened to come first, so
// re-recording could swap the assertion's subject without anyone noticing.
func findFixtureLine(t *testing.T, fixture, marker string) (string, int) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "stream", fixture)
	var found string
	at, matches := 0, 0
	for i, line := range fixtureLines(t, path) {
		if strings.Contains(line, marker) {
			found, at, matches = line, i+1, matches+1
		}
	}
	if matches != 1 {
		t.Fatalf("%d lines in %s contain %s, want exactly 1", matches, path, marker)
	}
	return found, at
}

// decodedLine is one decoded event and the fixture line it came from, so a
// property asserted over a whole file can name the line it failed on.
type decodedLine struct {
	Event
	line int
}

// decodeFixture decodes a whole fixture in arrival order.
//
// Order is the point: a client sees these frames in exactly this sequence, so
// a property about what precedes or follows a frame can only be checked
// against the file as a sequence. Every other helper here reads one line.
func decodeFixture(t *testing.T, path string) []decodedLine {
	t.Helper()
	var out []decodedLine
	for n, line := range fixtureLines(t, path) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
		}
		for _, ev := range evs {
			out = append(out, decodedLine{Event: ev, line: n + 1})
		}
	}
	return out
}

// wireTypeOf reads a line's top-level type without decoding it, so a test can
// say what a frame *is* independently of what DecodeLine made of it.
func wireTypeOf(t *testing.T, line string) string {
	t.Helper()
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		t.Fatalf("fixture line is not JSON: %v", err)
	}
	return probe.Type
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.Kind)
	}
	return out
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

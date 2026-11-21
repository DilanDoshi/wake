//go:build unix

package main

import (
	"os"
	"strings"
	"testing"
)

const (
	pasteBegin = "\x1b[200~"
	pasteClose = "\x1b[201~"
)

// A paste is open from its start marker to its end marker, across reads: the last
// marker in a chunk decides, and a chunk with none keeps what was.
//
// Mutation check: ignoring the state a chunk with no marker inherits fails the
// "was open" rows; reading the first marker, not the last, fails the two-marker ones.
func TestPasteOpenFollowsTheMarkers(t *testing.T) {
	for name, tc := range map[string]struct {
		chunk string
		was   bool
		want  bool
	}{
		"nothing, closed":            {"abc", false, false},
		"nothing, open":              {"abc", true, true},
		"its start":                  {"x" + pasteBegin + "abc", false, true},
		"its end":                    {"abc" + pasteClose, true, false},
		"a whole paste":              {pasteBegin + "abc" + pasteClose, false, false},
		"an end, then another start": {pasteClose + "x" + pasteBegin + "abc", true, true},
		"a start, then its end":      {pasteBegin + pasteClose, true, false},
		"a paste, then another":      {pasteBegin + "a" + pasteClose + pasteBegin + "b", false, true},
		"reports in the body":        {reportGained + wheelReport + reportLost, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := pasteOpen([]byte(tc.chunk), tc.was); got != tc.want {
				t.Errorf("pasteOpen(%q, %v) = %v, want %v", tc.chunk, tc.was, got, tc.want)
			}
		})
	}
}

// While a paste is open every byte is the paste's, however it looks: a read of
// nothing but report-shaped bytes is left alone, and reordering comes back with
// the end marker.
//
// Mutation check: running focusLast in a paste fails the second read.
func TestTheInputPipeLeavesAPastesBodyAloneUntilItEnds(t *testing.T) {
	pipe, feed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pipe.Close(); _ = feed.Close() })
	r := &alignedPipe{File: pipe}
	read := func(in string) string {
		t.Helper()
		if _, err := feed.WriteString(in); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 256)
		n, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return string(buf[:n])
	}

	for _, step := range []struct{ what, in, want string }{
		{"the start of a paste", pasteBegin + "abc", pasteBegin + "abc"},
		{"a body of reports", reportLost + wheelReport + reportGained, reportLost + wheelReport + reportGained},
		{"another, still inside", reportGained + reportLost, reportGained + reportLost},
		{"its end", reportLost + pasteClose, reportLost + pasteClose},
		{"a click after it", reportGained + clickReports, clickReports + reportGained},
	} {
		if got := read(step.in); got != step.want {
			t.Errorf("%s: read %q, want %q", step.what, got, step.want)
		}
	}
}

// The same through a real Bubble Tea: a paste of nothing but focus and mouse
// reports, long enough to span reads, arrives whole. The first version of the pipe
// took the reports out of its middle read.
//
// Mutation check: running focusLast in a paste drops reports from the body.
func TestAPasteOfReportShapedBytesArrivesWholeAcrossReads(t *testing.T) {
	for name, unit := range map[string]string{
		"gained and a press": reportGained + "\x1b[<0;1;1M",
		"lost and a wheel":   reportLost + wheelReport,
	} {
		t.Run(name, func(t *testing.T) {
			body := strings.Repeat(unit, 60) // past one read, so the end marker comes in a later one
			got := decodeThroughThePipe(t, pasteBegin+body+pasteClose, func(d decodedModel) bool { return len(d.keys) >= 1 })
			if want := "[" + body + "]"; strings.Join(got.keys, "") != want {
				t.Errorf("the paste arrived as %d bytes, want its %d whole", len(strings.Join(got.keys, "")), len(want))
			}
			if got.focus+got.blur+got.mice != 0 {
				t.Errorf("a paste's body decoded as terminal input: focus=%d blur=%d mice=%d", got.focus, got.blur, got.mice)
			}
		})
	}
}

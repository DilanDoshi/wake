//go:build unix

package main

import (
	"math/rand"
	"strings"
	"testing"
)

// focusLast runs on every read the input pipe makes, beside invariants that kept
// a mouse report out of the composer, so it is held to its own over reads built
// from what a terminal sends: mouse reports, focus reports, typed text, a paste's
// markers, Shift+arrows, an X10 report, partial sequences and a bare Escape.
//
// A read of mouse reports and focus reports comes out as its mouse reports, in
// order, with the last focus report (if any) after them. Any other read comes out
// byte for byte, and nothing it is given is written to.
//
// Mutation check: reordering whatever shares a read fails the second property;
// keeping the first report, not the last, fails the first.
func TestFocusLastOnlyEverMovesAReportBehindMouseReports(t *testing.T) {
	mouse := []string{wheelReport, "\x1b[<64;1;1M", "\x1b[<0;120;45M", "\x1b[<0;120;45m", "\x1b[<35;7;9M"}
	reports := []string{reportGained, reportLost}
	others := []string{
		"a", "xyz", "hello", "\x1b", "\x1b[", "\x1b[<65;1", "\x1b[<x", "\x1b[OA", "\x1b[Ob", "\x1bOP",
		"\x1b[M#!!", "\x1b[200~paste\x1b[201~", "\x1b[200~", "\x1b[201~", "\x1b[1;2A", "\x03", "\r",
	}
	pick := func(rng *rand.Rand, from []string) string { return from[rng.Intn(len(from))] }
	dropReports := strings.NewReplacer(reportGained, "", reportLost, "")

	rng := rand.New(rand.NewSource(1))
	for range 50000 {
		var read strings.Builder
		hasOther, last := false, ""
		for range 1 + rng.Intn(8) {
			switch n := rng.Intn(100); {
			case n < 40:
				read.WriteString(pick(rng, mouse))
			case n < 65:
				last = pick(rng, reports)
				read.WriteString(last)
			default:
				hasOther = true
				read.WriteString(pick(rng, others))
			}
		}
		in := read.String()
		given := []byte(in)

		out := string(focusLast(given))

		want := in
		if !hasOther && last != "" {
			want = dropReports.Replace(in) + last
		}
		if out != want {
			t.Fatalf("focusLast(%q) = %q, want %q", in, out, want)
		}
		if string(given) != in {
			t.Fatalf("focusLast wrote to its input: %q became %q", in, given)
		}
		if again := string(focusLast([]byte(out))); again != out {
			t.Fatalf("focusLast is not idempotent on %q: %q then %q", in, out, again)
		}
	}
}

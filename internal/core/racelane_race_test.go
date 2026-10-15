//go:build race

package core

import "testing"

// TestTheRaceLaneCollectsNoCoverage fails a -race run that also collects
// coverage, however the flag arrived - the Makefile, GOFLAGS, `go env -w`.
// -race forces atomic cover counters, whose sync/atomic calls the detector
// treats as synchronisation, so they can hide real data races. Coverage belongs
// to the plain lane, `make cover` (decisions.md, 2026-10-14).
func TestTheRaceLaneCollectsNoCoverage(t *testing.T) {
	if mode := testing.CoverMode(); mode != "" {
		t.Fatalf("this -race run collects coverage (covermode %s), and atomic cover counters can hide "+
			"real data races from the detector. Run coverage without -race (`make cover`).", mode)
	}
}

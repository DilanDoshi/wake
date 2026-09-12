package ui

// The compaction rendering: an animated indeterminate bar with an elapsed timer
// while /compact runs, and a completion line carrying the boundary's real
// figures once it lands.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// The bar animates: a lit block sweeps across it, so two moments a step apart
// draw differently, and it holds a fixed display width so the line never jumps.
func TestCompactBarSweeps(t *testing.T) {
	first := ansi.Strip(compactBar(0))
	later := ansi.Strip(compactBar(compactBarStep * time.Duration(compactBarCells/2)))
	if first == later {
		t.Errorf("the bar did not move between two moments a step apart: %q vs %q", first, later)
	}
	if w := ansi.StringWidth(first); w != compactBarCells {
		t.Errorf("bar width = %d, want a fixed %d", w, compactBarCells)
	}
}

// The compacting line names the work, shows the elapsed time, and animates -
// the elapsed timer is what the operator watches to see how long it has run.
func TestCompactingLineShowsElapsedAndAnimates(t *testing.T) {
	forceTrueColour(t)
	start := clock()
	defer func() { clock = time.Now }()

	clock = func() time.Time { return start.Add(3 * time.Second) }
	first := compactingLine(start, 60)
	if got := stripANSI(first); !strings.Contains(got, "Compacting conversation") {
		t.Errorf("compacting line = %q, want it to name the work", got)
	}
	if got := stripANSI(first); !strings.Contains(got, "3s") {
		t.Errorf("compacting line = %q, want the elapsed 3s", got)
	}

	clock = func() time.Time { return start.Add(3*time.Second + compactBarStep*time.Duration(compactBarCells/2)) }
	if compactingLine(start, 60) == first {
		t.Error("the compacting line did not animate between two moments a step apart")
	}
}

// As the pane narrows the compacting line drops clauses whole and in order -
// the timer first (it rides on the bar), then the bar - and the word that says a
// compaction is live always survives rather than being cut.
func TestCompactingLineDropsClausesInOrder(t *testing.T) {
	forceTrueColour(t)
	start := clock()
	defer func() { clock = time.Now }()
	clock = func() time.Time { return start.Add(3 * time.Second) }

	hasBar := func(s string) bool { return strings.Contains(s, compactBarFull) || strings.Contains(s, compactBarEmpty) }

	wide := stripANSI(compactingLine(start, 60))
	if !strings.Contains(wide, "3s") || !hasBar(wide) {
		t.Fatalf("a wide pane dropped the bar or timer: %q", wide)
	}
	medium := stripANSI(compactingLine(start, 40))
	if strings.Contains(medium, "3s") {
		t.Errorf("the timer did not drop before the bar at 40 cols: %q", medium)
	}
	if !hasBar(medium) {
		t.Errorf("the bar dropped too early at 40 cols: %q", medium)
	}
	narrow := stripANSI(compactingLine(start, 28))
	if hasBar(narrow) || strings.Contains(narrow, "3s") {
		t.Errorf("the bar/timer did not drop at 28 cols: %q", narrow)
	}
	for _, got := range []string{wide, medium, narrow} {
		if !strings.Contains(got, "Compacting conversation") {
			t.Errorf("the word did not survive: %q", got)
		}
	}
}

func compactSummary(trigger string) *core.CompactSummary {
	return &core.CompactSummary{Trigger: trigger, PreTokens: 50826, PostTokens: 4477, Dropped: 46349, DurationMs: 16813}
}

// The completion line shows every figure the boundary reported - before, after,
// freed and how long - all real, none derived.
func TestCompactedSummaryLineShowsTheRealFigures(t *testing.T) {
	forceTrueColour(t)
	got := stripANSI(compactedSummaryLine(compactSummary("manual"), 80))
	for _, want := range []string{"Compacted", "50.8k", "4.5k", "46.3k", "16s"} {
		if !strings.Contains(got, want) {
			t.Errorf("completion line = %q, want it to contain %q", got, want)
		}
	}
}

// A manual compaction stays quiet about its trigger; an auto one says so - the
// done line's rule for the ordinary case.
func TestCompactedSummaryLineMarksAutoOnly(t *testing.T) {
	forceTrueColour(t)
	if got := stripANSI(compactedSummaryLine(compactSummary("manual"), 80)); strings.Contains(got, "auto") {
		t.Errorf("a manual compaction named its trigger: %q", got)
	}
	if got := stripANSI(compactedSummaryLine(compactSummary("auto"), 80)); !strings.Contains(got, "auto") {
		t.Errorf("an auto compaction did not say so: %q", got)
	}
}

// The boundary's transcript block is the rich summary when metadata is present,
// and the plain label when it is not - a boundary restored off disk carries no
// metadata, and must still say a compaction happened rather than nothing.
func TestNoticeBlockEnrichesTheCompactionWhenMetadataIsPresent(t *testing.T) {
	forceTrueColour(t)
	rich := core.Event{Kind: core.KindSystem, Notice: core.NoticeContextCompacted, Compaction: compactSummary("manual")}
	if got := stripANSI(noticeBlock(rich, 80)); !strings.Contains(got, "46.3k") {
		t.Errorf("a boundary with metadata drew %q, want the freed figure", got)
	}
	plain := core.Event{Kind: core.KindSystem, Notice: core.NoticeContextCompacted}
	got := stripANSI(noticeBlock(plain, 80))
	if !strings.Contains(got, "Compacted") {
		t.Errorf("a boundary without metadata drew %q, want the plain label", got)
	}
	if strings.Contains(got, "46.3k") {
		t.Errorf("a boundary without metadata invented a figure: %q", got)
	}
}

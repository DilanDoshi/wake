//go:build unix

package daemon

import (
	"strings"
	"testing"
)

// **An import goes through resumeSafe, and this is the test that says so.**
//
// A transcript on disk is not evidence about a process. The one instrument Wake
// has is idsInUse, which matches core.SessionArgvMarkers against a live `ps` -
// so when a process *does* carry the id, Wake can see it and must refuse. The
// mutation this kills is the tempting one, and it is tempting precisely because
// the import is a fork and a fork does not write to the source's file: "forking
// is safe, so skip the check."
//
// It is not the whole safety and the refusal is not the whole story - see the
// test below for the half this cannot reach - but it is the half that is
// checkable, and dropping it would give away the twelve-of-sixteen case for
// nothing.
func TestAnImportIsRefusedWhileAProcessHoldsTheSessionId(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "eeeeeeee-5555-4555-8555-555555555555"
	writeTranscript(t, projects, slugOf(real), id, real)

	// The premise, read back through a mechanism importSource does not use.
	// Without this the absence of a refusal below would be about nothing.
	if argv := bystander(t, "--session-id", id); !holdsTheSession(argv, id) {
		t.Fatalf("the bystander runs as %q and core would not read that as a process holding session %s", argv, id)
	}

	src, err := s.importSource(id)
	if err == nil {
		t.Fatalf("an import of %+v was allowed while another process carried its id in its argv. "+
			"Wake could see that process and let it through anyway", src)
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Errorf("the refusal is %q and does not say a process is still running under that id: "+
			"nothing on claude's wire reports the collision, so this sentence is the only account anyone gets", err.Error())
	}
	if !strings.Contains(err.Error(), "close it there first") {
		t.Errorf("the refusal is %q and does not tell the operator what to do about it. "+
			"2026-08-09 findings §5 is explicit that the flow must say close it there first, because the picker "+
			"cannot learn from claude whether it is closed", err.Error())
	}
}

// The half the test above cannot reach, stated as a test so that it is not only
// a comment.
//
// 2026-08-12 findings §5 counted four live `claude` processes on the recording
// machine whose entire command line is the word `claude` - no --session-id, no
// --resume, nothing SessionArgvMarkers can match. That is the shape the feature
// was asked for, and Wake **cannot see it**.
//
// So a pass from resumeSafe is not proof, and this asserts the consequence
// rather than the sentiment: with a process holding the transcript but carrying
// no id, importSource **allows** the import. That is not a bug to be fixed here
// - it is why the import is a fork rather than a resume, and a fork leaves the
// source's transcript byte-identical (2026-08-10 findings §5).
//
// If somebody later makes liveness detectable for this shape - the lsof spike
// §7 names - this test fails, and that failure is the correct signal: come back
// and decide whether the import should now refuse.
func TestAnImportCannotSeeAProcessThatCarriesNoIdAndIsAForkBecauseOfIt(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "ffffffff-6666-4666-8666-666666666666"
	writeTranscript(t, projects, slugOf(real), id, real)

	// A live process working on that session, spelled the way a person's
	// terminal spells it: the word and nothing else.
	if argv := bystander(t, "claude"); holdsTheSession(argv, id) {
		t.Fatalf("the bystander runs as %q and core reads that as holding session %s: "+
			"the premise of this test is that it cannot, so there is nothing here to demonstrate", argv, id)
	}

	if _, err := s.importSource(id); err != nil {
		t.Fatalf("importSource refused with %q. This test exists to pin that Wake **cannot** detect a bare "+
			"`claude`, which is why an import must be a fork rather than a resume. If a refusal is now possible, "+
			"that is a better world - delete this test, and revisit whether the fork is still the right primitive", err)
	}
}

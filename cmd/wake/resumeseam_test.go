package main

import (
	"testing"
)

// Resumable is the structured half of the same discovery Listing formats: it
// returns the same session ids, each with the directory discovery proved, so the
// resume picker can build and sort rows without walking the disk itself.
func TestResumableReturnsStructuredDiscovery(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)

	got, err := machineSessions{}.Resumable()
	if err != nil {
		t.Fatalf("Resumable: %v", err)
	}
	var found bool
	for _, d := range got {
		if d.ID != importA {
			continue
		}
		found = true
		if d.Dir != real {
			t.Errorf("Resumable Dir = %q, want the proven directory %q", d.Dir, real)
		}
	}
	if !found {
		t.Fatalf("Resumable did not return the seeded session %s; got %d rows", importA, len(got))
	}
}

// A machine with no sessions is a real, common shape — the first day — and
// Resumable answers it with an empty slice and no error, so the picker can take
// the empty-state path rather than crashing on a nil walk.
func TestResumableOnAnEmptyMachine(t *testing.T) {
	projectsTree(t)
	got, err := machineSessions{}.Resumable()
	if err != nil {
		t.Fatalf("Resumable on an empty machine errored: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Resumable found %d sessions on an empty machine", len(got))
	}
}

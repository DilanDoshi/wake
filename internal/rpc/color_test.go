package rpc

import (
	"slices"
	"strings"
	"testing"
)

// The colour set is a closed vocabulary, and NormalizeColor is the fence both
// sides of the socket apply - the client before it sends and the daemon before
// it stores. It lives here for the same reason ValidWorktreeName does: internal/ui
// may not import the daemon, and it needs the names for the completion menu.

func TestNormalizeColorAcceptsEveryNameCanonically(t *testing.T) {
	for _, name := range ColorNames {
		got, err := NormalizeColor(name)
		if err != nil {
			t.Errorf("NormalizeColor(%q) refused a name in ColorNames: %v", name, err)
		}
		if got != name {
			t.Errorf("NormalizeColor(%q) = %q, want the canonical name unchanged", name, got)
		}
	}
}

func TestNormalizeColorFoldsCase(t *testing.T) {
	got, err := NormalizeColor("GREEN")
	if err != nil {
		t.Fatalf("NormalizeColor(%q) refused an upper-case known name: %v", "GREEN", err)
	}
	if got != "green" {
		t.Errorf("NormalizeColor(%q) = %q, want the canonical lower-case %q", "GREEN", got, "green")
	}
}

func TestNormalizeColorClearsOnNoneAndEmpty(t *testing.T) {
	for _, clear := range []string{"", "none", "NONE", "  none  "} {
		got, err := NormalizeColor(clear)
		if err != nil {
			t.Errorf("NormalizeColor(%q) should clear, not refuse: %v", clear, err)
		}
		if got != "" {
			t.Errorf("NormalizeColor(%q) = %q, want %q (cleared)", clear, got, "")
		}
	}
}

func TestNormalizeColorRefusesAnUnknownNameAndListsTheSet(t *testing.T) {
	_, err := NormalizeColor("chartreuse")
	if err == nil {
		t.Fatal("NormalizeColor accepted a colour that is not in the set")
	}
	// The refusal has to be actionable: an operator who typed a wrong colour is
	// told which ones exist, the way the daemon lists the fleet on a bad handle.
	for _, name := range ColorNames {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name %q; it should list every colour: %q", name, err.Error())
		}
	}
}

// The clear word is a colour name nobody may claim, or clearing and setting a
// real colour would collide.
func TestColorNoneIsNotAColour(t *testing.T) {
	if slices.Contains(ColorNames, ColorNone) {
		t.Errorf("%q is both the clear word and a colour name; the two cannot share a spelling", ColorNone)
	}
}

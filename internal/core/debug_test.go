package core

import (
	"strings"
	"testing"
)

// The two spellings claude's own --help gives, plus the shapes between them.
func TestValidDebugFilterTakesTheRecordedSpellings(t *testing.T) {
	for _, filter := range []string{"api", "api,hooks", "!1p,!file", "api,!file", "mcp_client", "a-b"} {
		if !ValidDebugFilter(filter) {
			t.Errorf("%q is refused, and claude's own --help gives it as an example", filter)
		}
	}
}

// A link naming nothing is the shape a trailing comma produces, and claude
// reports nothing about it afterwards - ValidFallbackModel's own rule, one flag
// over.
func TestValidDebugFilterRefusesALinkNamingNothing(t *testing.T) {
	for _, filter := range []string{"", "api,", ",api", "api,,hooks", "!", "api, hooks"} {
		if ValidDebugFilter(filter) {
			t.Errorf("%q is accepted as a debug filter", filter)
		}
	}
}

// The category set is closed on purpose: what is not obviously a category is
// refused rather than reaching a command line as a category name.
func TestValidDebugFilterRefusesWhatIsNotACategory(t *testing.T) {
	for _, filter := range []string{"--verbose", "api;rm", "api/hooks", "api hooks", "!!api", "api!"} {
		if ValidDebugFilter(filter) {
			t.Errorf("%q is accepted as a debug filter", filter)
		}
	}
}

func TestValidDebugFilterIsBounded(t *testing.T) {
	if ValidDebugFilter(strings.Repeat("a", maxDebugFilter+1)) {
		t.Error("an unbounded debug filter is accepted")
	}
}

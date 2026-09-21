package rpc

import (
	"strings"
	"testing"
)

// A team tag is an operator's own grouping of the fleet, and NormalizeTeam is
// the fence both sides of the socket apply - the client before it sends and the
// daemon before it stores. It lives here for ValidWorktreeName's reason:
// internal/ui may not import the daemon, and it sends what was typed.
//
// Unlike a colour it is not a closed vocabulary - the operator names the team -
// so the fence is a shape, not a set: one lower-case token of the mention
// charset, so `@team` can address it, bounded so a header and a mention stay
// sane, with "none" and the empty string clearing, NormalizeColor's own rule.

func TestNormalizeTeamAcceptsAToken(t *testing.T) {
	for _, name := range []string{"backend", "frontend", "infra", "v2", "data-pipeline", "web_app"} {
		got, err := NormalizeTeam(name)
		if err != nil {
			t.Errorf("NormalizeTeam(%q) refused a valid team name: %v", name, err)
		}
		if got != name {
			t.Errorf("NormalizeTeam(%q) = %q, want it unchanged", name, got)
		}
	}
}

func TestNormalizeTeamFoldsCase(t *testing.T) {
	got, err := NormalizeTeam("Backend")
	if err != nil {
		t.Fatalf("NormalizeTeam(%q) refused a mixed-case name: %v", "Backend", err)
	}
	if got != "backend" {
		t.Errorf("NormalizeTeam(%q) = %q, want the canonical lower-case %q", "Backend", got, "backend")
	}
}

func TestNormalizeTeamClearsOnNoneAndEmpty(t *testing.T) {
	for _, clear := range []string{"", "none", "NONE", "  none  "} {
		got, err := NormalizeTeam(clear)
		if err != nil {
			t.Errorf("NormalizeTeam(%q) should clear, not refuse: %v", clear, err)
		}
		if got != "" {
			t.Errorf("NormalizeTeam(%q) = %q, want %q (cleared)", clear, got, "")
		}
	}
}

// A team name has to be one token: it is addressed as `@team`, and splitWord
// takes a whitespace-delimited word, so a two-word team could never be reached.
func TestNormalizeTeamRefusesWhitespaceAndPunctuation(t *testing.T) {
	for _, bad := range []string{"back end", "a/b", "team!", "front.end", "@backend", "back\nend", "a\tb", "x\x1b[0m"} {
		if _, err := NormalizeTeam(bad); err == nil {
			t.Errorf("NormalizeTeam(%q) was accepted; a team name must be one mention-safe token", bad)
		}
	}
}

func TestNormalizeTeamRefusesAnOverlongName(t *testing.T) {
	long := strings.Repeat("a", maxTeamName+1)
	_, err := NormalizeTeam(long)
	if err == nil {
		t.Fatalf("NormalizeTeam accepted a %d-character name; it must be bounded", len(long))
	}
	if !strings.Contains(err.Error(), TeamNone) {
		t.Errorf("the refusal does not name the clear word %q: %q", TeamNone, err.Error())
	}
}

// The clear word is a team name nobody may claim, or clearing and setting a team
// called "none" would share a spelling - ColorNone's own rule.
func TestNormalizeTeamCannotBeNamedNone(t *testing.T) {
	got, err := NormalizeTeam(TeamNone)
	if err != nil {
		t.Fatalf("NormalizeTeam(%q) refused the clear word: %v", TeamNone, err)
	}
	if got != "" {
		t.Errorf("NormalizeTeam(%q) = %q, want it to clear rather than name a team", TeamNone, got)
	}
}

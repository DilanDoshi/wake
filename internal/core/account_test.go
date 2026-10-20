package core

import (
	"path/filepath"
	"testing"
	"time"
)

// The account a login names is read off Claude's config file by its ids alone:
// a profile rewrite with the same ids is the same account.
func TestDecodeAccountReadsTheSignedInIds(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want Account
		ok   bool
	}{
		{"signed in", `{"numStartups":3,"oauthAccount":{"accountUuid":"a1","organizationUuid":"o1","emailAddress":"x"}}`, Account{ID: "a1", Org: "o1"}, true},
		{"no organisation", `{"oauthAccount":{"accountUuid":"a1"}}`, Account{ID: "a1"}, true},
		{"signed out", `{"numStartups":3}`, Account{}, false},
		{"no account id", `{"oauthAccount":{"organizationUuid":"o1"}}`, Account{}, false},
		{"half written", `{"oauthAccount":{"accountUuid":"a1"`, Account{}, false},
		{"not an object", `[1,2]`, Account{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DecodeAccount([]byte(tc.raw))
			if got != tc.want || ok != tc.ok {
				t.Errorf("DecodeAccount(%s) = %+v, %v; want %+v, %v", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// Claude's config file is .claude.json in CLAUDE_CONFIG_DIR when that is set,
// and in the home directory otherwise.
func TestConfigFileFollowsClaudesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if got, err := ConfigFile(); err != nil || got != filepath.Join(dir, ".claude.json") {
		t.Errorf("ConfigFile() = %q, %v under CLAUDE_CONFIG_DIR=%s", got, err, dir)
	}
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", home)
	if got, err := ConfigFile(); err != nil || got != filepath.Join(home, ".claude.json") {
		t.Errorf("ConfigFile() = %q, %v with no CLAUDE_CONFIG_DIR, want it in %s", got, err, home)
	}
}

// A usage limit's message names its reset as a wall-clock time in a zone; the
// reset is the next such time at or after the limit hit, to the minute.
func TestUsageLimitResetIsTheNextNamedTime(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}
	hit := time.Date(2026, 10, 17, 14, 43, 25, 0, la)
	for _, tc := range []struct {
		name string
		text string
		hit  time.Time
		want time.Time
		ok   bool
	}{
		{"later today", "You've hit your session limit · resets 6:50pm (America/Los_Angeles)", hit,
			time.Date(2026, 10, 17, 18, 50, 0, 0, la), true},
		{"on the hour", "You've hit your session limit · resets 9pm (America/Los_Angeles)", hit,
			time.Date(2026, 10, 17, 21, 0, 0, 0, la), true},
		{"after midnight", "You've hit your session limit · resets 2:20am (America/Los_Angeles)", hit,
			time.Date(2026, 10, 18, 2, 20, 0, 0, la), true},
		{"noon and midnight", "You've hit your session limit · resets 12am (America/Los_Angeles)", hit,
			time.Date(2026, 10, 18, 0, 0, 0, 0, la), true},
		{"the hit's own minute", "You've hit your session limit · resets 2:43pm (America/Los_Angeles)", hit,
			time.Date(2026, 10, 17, 14, 43, 0, 0, la), true},
		{"another zone", "You've hit your session limit · resets 11pm (Europe/London)", hit,
			time.Date(2026, 10, 17, 23, 0, 0, 0, mustZone(t, "Europe/London")), true},
		{"the year turns", "You've hit your session limit · resets 1am (America/Los_Angeles)",
			time.Date(2026, 12, 31, 23, 30, 0, 0, la), time.Date(2027, 1, 1, 1, 0, 0, 0, la), true},
		{"a weekly limit's date", "You've hit your weekly limit · resets Oct 20 at 9am (America/Los_Angeles)", hit,
			time.Date(2026, 10, 20, 9, 0, 0, 0, la), true},
		{"a weekly date into next year", "You've hit your weekly limit · resets Jan 2 at 10:30pm (America/Los_Angeles)",
			time.Date(2026, 12, 30, 9, 0, 0, 0, la), time.Date(2027, 1, 2, 22, 30, 0, 0, la), true},
		{"not a month", "You've hit your weekly limit · resets Foo 20 at 9am (America/Los_Angeles)", hit, time.Time{}, false},
		{"an unknown zone", "You've hit your session limit · resets 6:50pm (Mars/Olympus)", hit, time.Time{}, false},
		{"no reset named", "You've hit your session limit", hit, time.Time{}, false},
		{"not a time", "You've hit your session limit · resets soon (America/Los_Angeles)", hit, time.Time{}, false},
		{"a minute past the hour's end", "You've hit your session limit · resets 6:75pm (America/Los_Angeles)", hit, time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := UsageLimitReset(tc.text, tc.hit)
			if ok != tc.ok || !got.Equal(tc.want) {
				t.Errorf("UsageLimitReset(%q) = %v, %v; want %v, %v", tc.text, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no zone %s: %v", name, err)
	}
	return loc
}

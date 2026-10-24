// Who Claude is signed in as, how much of its session window it has used, and
// when a usage limit says it resets - part of the airlock; see protocol.go.
//
// All are Claude Code's formats: the account is a key of its config file,
// which every claude process rewrites, and the reset is the English of its
// usage-limit message. The daemon uses them to resume agents a limit stopped
// once a login elsewhere or the reset lifts it (owner's 2026-10-17 ruling). The
// session window is a rate_limit_event's, drawn under the roster (2026-10-24).

package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// Account is the signed-in account and organisation ids. A login to another
// account changes them; a rewrite of the same profile does not.
type Account struct{ ID, Org string }

// wireConfig is the one key of Claude's config file Wake reads.
type wireConfig struct {
	OAuthAccount *struct {
		AccountUUID      string `json:"accountUuid"`
		OrganizationUUID string `json:"organizationUuid"`
	} `json:"oauthAccount"`
}

// DecodeAccount reads the signed-in account off Claude's config file. False
// for a file naming no account - signed out, or caught half written.
func DecodeAccount(raw []byte) (Account, bool) {
	var c wireConfig
	if json.Unmarshal(raw, &c) != nil || c.OAuthAccount == nil || c.OAuthAccount.AccountUUID == "" {
		return Account{}, false
	}
	return Account{ID: c.OAuthAccount.AccountUUID, Org: c.OAuthAccount.OrganizationUUID}, true
}

// claudeConfigDirEnv moves Claude's config directory, file included.
const claudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// claudeConfigName is the config file's name in that directory, or in home.
const claudeConfigName = ".claude.json"

// ConfigFile is the file a /login in any claude process rewrites.
func ConfigFile() (string, error) {
	if dir := os.Getenv(claudeConfigDirEnv); dir != "" {
		return filepath.Join(dir, claudeConfigName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, claudeConfigName), nil
}

// resetClause is the reset a usage-limit message names: a session limit's
// clock time, "resets 6:50pm (America/Los_Angeles)", or a weekly limit's date
// and time, "resets Oct 20 at 9am (America/Los_Angeles)".
var resetClause = regexp.MustCompile(`resets (?:([A-Z][a-z]{2}) (\d{1,2}) at )?(\d{1,2})(?::(\d{2}))?(am|pm) \(([^)]+)\)`)

// UsageLimitReset is when a usage-limit message says the limit resets: the next
// time it names at or after the hit, to the minute. False for wording it does
// not recognise, or a zone this machine cannot load.
func UsageLimitReset(text string, hit time.Time) (time.Time, bool) {
	m := resetClause.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, false
	}
	hour, minute, ok := clockOf(m[3], m[4], m[5])
	loc, err := time.LoadLocation(m[6])
	if !ok || err != nil {
		return time.Time{}, false
	}
	from := hit.In(loc).Truncate(time.Minute)
	month, day := from.Month(), from.Day()
	if m[1] != "" {
		named, err := time.Parse("Jan 2", m[1]+" "+m[2])
		if err != nil {
			return time.Time{}, false
		}
		month, day = named.Month(), named.Day()
	}
	reset := time.Date(from.Year(), month, day, hour, minute, 0, 0, loc)
	switch {
	case !reset.Before(from):
	case m[1] != "":
		reset = reset.AddDate(1, 0, 0)
	default:
		reset = reset.AddDate(0, 0, 1)
	}
	return reset, true
}

// clockOf is a 12-hour clock reading as an hour and minute of the day.
func clockOf(hour, minute, half string) (int, int, bool) {
	h, _ := strconv.Atoi(hour)
	mm := 0
	if minute != "" {
		mm, _ = strconv.Atoi(minute)
	}
	if h < 1 || h > 12 || mm > 59 {
		return 0, 0, false
	}
	h %= 12
	if half == "pm" {
		h += 12
	}
	return h, mm, true
}

// wireUsageWindow is one of rate_limit_info.unifiedWindows: the share used and
// the reset, in Unix seconds. A field missing is no figure, not a zero.
type wireUsageWindow struct {
	Used   *float64 `json:"utilization"`
	Resets int64    `json:"resetsAt"`
}

// sessionUsage is the five-hour window a rate_limit_info reports, nil when it
// names no figure for it - or one of a shape Wake does not know, which costs
// the reading and not the frame. Claude's verdict is the top-level status, and
// only while Type names this window: a weekly warning says nothing about it.
func sessionUsage(r *wireRateLimit) *SessionUsage {
	var windows struct {
		FiveHour *wireUsageWindow `json:"five_hour"`
	}
	if r == nil || json.Unmarshal(r.Windows, &windows) != nil {
		return nil
	}
	w := windows.FiveHour
	if w == nil || w.Used == nil || w.Resets == 0 {
		return nil
	}
	u := &SessionUsage{Used: *w.Used, Resets: time.Unix(w.Resets, 0)}
	if r.Type == rateLimitFiveHour {
		u.Warned = r.Status == rateLimitWarning
		u.Limited = r.Status != "" && r.Status != rateLimitAllowed && !u.Warned
	}
	return u
}

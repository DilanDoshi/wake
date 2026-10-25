package rpc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A report carries the account's session window whole, and a daemon with no
// reading - or one from before readings were reported - names none at all.
func TestAStatusCarriesTheSessionUsageAndOmitsNone(t *testing.T) {
	resets := time.Date(2026, 10, 24, 16, 9, 0, 0, time.UTC)
	in := Status{Running: true, SessionUsage: &SessionUsage{Percent: 91, Resets: resets, Warned: true, Lapsed: true}}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Status
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if u := out.SessionUsage; u == nil || u.Percent != 91 || !u.Resets.Equal(resets) || !u.Warned || u.Limited || !u.Lapsed {
		t.Errorf("round trip gave %+v, want %+v", out.SessionUsage, in.SessionUsage)
	}
	raw, err = json.Marshal(Status{Running: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "session_usage") {
		t.Errorf("a report with no reading names one: %s", raw)
	}
}

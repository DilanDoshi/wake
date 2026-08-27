// Can one session forge a second row in the picker?
//
// `internal/mcp/untrusted_test.go` holds this class for the **manager's**
// surface, where the untrusted author is an agent's model. This is the same
// question on the **human's** surface, where the authors are a transcript's
// recorded `cwd` and a directory name off the filesystem — neither of which
// Wake wrote.
//
// It shipped broken once. `oneLine` was applied to `Preview` at *construction*
// and `Dir` was interpolated raw beside it on the same row, so one session in
// gave two rows out with the forged one indistinguishable from a real session.
// The fix was to contain the assembled **line**, which is CLAUDE.md's own rule
// for the manager's surface; this is what holds it, and what makes a seventh
// field on FoundSession inherit it.

package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
)

// forgery is what an attacker would put in a field: a line break, then a row
// shaped exactly like a real one.
const forgery = "ok\n  deadbeef  2h ago     /Users/someone/private"

// stringFieldsOfFoundSession reads the field set out of the struct rather than
// listing it, so a seventh field is covered the day it is added rather than the
// day somebody remembers.
func stringFieldsOfFoundSession(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(daemon.FoundSession{})
	var out []string
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.IsExported() && f.Type.Kind() == reflect.String {
			out = append(out, f.Name)
		}
	}
	if len(out) < 4 {
		t.Fatalf("found %d exported string fields on daemon.FoundSession (%v): the scan is broken, and a scan "+
			"that finds nothing agrees with every mutation", len(out), out)
	}
	return out
}

// baseline is the listing for one ordinary session, whose row count every
// forgery below is measured against.
func baseline(t *testing.T) int {
	t.Helper()
	return len(strings.Split(strings.TrimRight(formatImportable([]daemon.FoundSession{{
		ID:       importA,
		Dir:      "/tmp/a",
		Slug:     "-tmp-a",
		Preview:  "harmless",
		Modified: time.Now(),
	}}, importUsage, allRows), "\n"), "\n"))
}

// No value of any field may add a row.
//
// The assertion is on the **line count** rather than on the absence of the
// forged text: containment substitutes rather than deletes, so the characters
// are still there and only their power to end a line is gone. A test looking
// for the string would fail on the correct behaviour.
func TestNoFieldOfADiscoveredSessionCanForgeARowInThePicker(t *testing.T) {
	want := baseline(t)

	for _, field := range stringFieldsOfFoundSession(t) {
		t.Run(field, func(t *testing.T) {
			f := daemon.FoundSession{
				ID:       importA,
				Dir:      "/tmp/a",
				Slug:     "-tmp-a",
				Preview:  "harmless",
				Modified: time.Now(),
			}
			v := reflect.ValueOf(&f).Elem().FieldByName(field)
			v.SetString(forgery)

			got := formatImportable([]daemon.FoundSession{f}, importUsage, allRows)
			lines := len(strings.Split(strings.TrimRight(got, "\n"), "\n"))
			if lines != want {
				t.Errorf("a %s carrying a newline turned one session into %d lines, want %d. "+
					"A row of this listing is one session, named by the id it begins with — and %s is text "+
					"Wake did not write, so a forged row is a session that does not exist at a directory "+
					"that is not its own:\n%s", field, lines, want, field, got)
			}
		})
	}
}

// A session with no provable directory renders its reason from Slug, which is a
// directory name off the filesystem and is therefore the same class of input.
// It takes a different path through whereOrWhyNot, so it gets its own case.
func TestTheNoDirectoryReasonCannotForgeARowEither(t *testing.T) {
	want := baseline(t)
	got := formatImportable([]daemon.FoundSession{{
		ID:       importA,
		Slug:     forgery,
		Preview:  "harmless",
		Modified: time.Now(),
	}}, importUsage, allRows)
	if lines := len(strings.Split(strings.TrimRight(got, "\n"), "\n")); lines != want {
		t.Errorf("a Slug carrying a newline on the no-directory path gave %d lines, want %d:\n%s", lines, want, got)
	}
}

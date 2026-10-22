package daemon

// The park-ready file: one park book row per live agent, kept so a daemon that
// dies without parking anything still leaves its successor everything a wake
// needs (docs/notes/bugs.md BUG-53). These tests hold the store itself; the
// handover that reads it is crashpark_unix_test.go.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// everyWakeInput is a record with every field a wake reads set, so a field the
// file drops shows up as a difference rather than as an absence nobody sees.
func everyWakeInput() parkedRecord {
	return parkedRecord{
		ID: idBeta, Name: "alex", Label: "dev", Dir: "/tmp/repo", Parked: time.Now(),
		Effort: "max", Model: "opus", MaxBudgetUSD: "5", FallbackModel: "sonnet",
		Color: "blue", Team: "api", ClaudeName: "manager-ops",
	}
}

func readyFile(t *testing.T) (*parkReady, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), parkReadyName)
	return newParkReady(path), path
}

// The format is asserted from both sides: the key set is read out of raw JSON,
// and a record is decoded from a literal somebody typed. A writer and a reader
// that agree on the wrong key are consistent and unreadable by the next build -
// the park book's own lesson (decisions.md, 2026-08-10).
func TestTheParkReadyFileIsTheShapeAnotherBuildWouldRead(t *testing.T) {
	p, path := readyFile(t)
	if err := p.set(idAlpha, everyWakeInput()); err != nil {
		t.Fatalf("set: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil || len(rows) != 1 {
		t.Fatalf("the file is not one JSON object per live session: %v\n%s", err, data)
	}
	var keys []string
	for k := range rows[0] {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"claude_name", "color", "dir", "effort", "fallback_model", "id", "label",
		"max_budget_usd", "model", "name", "parked", "session", "team"}
	if !slices.Equal(keys, want) {
		t.Errorf("the on-disk keys are %v, want %v: a missing one is a wake input that does not survive a crash, "+
			"and an extra one - above all a pid - is state Wake has quietly started owning", keys, want)
	}

	typed := `[{"session":"` + idAlpha + `","id":"` + idBeta + `","name":"alex","dir":"/tmp/repo","parked":"2026-10-21T00:00:00Z","team":"api"}]`
	if err := os.WriteFile(path, []byte(typed), parkReadyPerm); err != nil {
		t.Fatalf("write literal: %v", err)
	}
	got := loadParkReady(path)
	if rec, ok := got[idAlpha]; !ok || rec.ID != idBeta || rec.Name != "alex" || rec.Team != "api" || rec.Dir != "/tmp/repo" {
		t.Errorf("a record typed by hand decoded as %+v: the key the reader looks for is not the one the file spells", got)
	}
}

func TestAParkReadyRecordSurvivesTheDaemonThatWroteIt(t *testing.T) {
	p, path := readyFile(t)
	want := everyWakeInput()
	if err := p.set(idAlpha, want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got := loadParkReady(path)[idAlpha]
	if !sameWakeInputs(got, want) {
		t.Errorf("the record read back is %+v, want %+v", got, want)
	}
}

// Every push of the fleet's status offers every agent's record, so writing on
// each one would be a file rewrite per state change per agent. Only a change to
// what a wake reads is written, and the park timestamp is not one of them.
func TestAParkReadyRecordIsRewrittenOnlyWhenWhatAWakeNeedsChanges(t *testing.T) {
	p, path := readyFile(t)
	rec := everyWakeInput()
	if err := p.set(idAlpha, rec); err != nil {
		t.Fatalf("set: %v", err)
	}
	stamp := func() time.Time {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		return info.ModTime()
	}
	first := stamp()
	time.Sleep(20 * time.Millisecond)

	again := rec
	again.Parked = rec.Parked.Add(time.Hour)
	if err := p.set(idAlpha, again); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !stamp().Equal(first) {
		t.Error("a record that differed only in its timestamp rewrote the file: that is a disk write per status push per agent")
	}

	changed := rec
	changed.Team = "web"
	if err := p.set(idAlpha, changed); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := loadParkReady(path)[idAlpha]; got.Team != "web" {
		t.Errorf("a changed team was not written: %+v", got)
	}
}

// A write that failed must not be remembered as done: the next offer of the
// same record has to try the disk again, or one full disk leaves the file
// stale for as long as the record never changes.
func TestAParkReadyWriteThatFailedIsRetriedByTheNextSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone", parkReadyName)
	p := newParkReady(path)
	rec := everyWakeInput()
	if err := p.set(idAlpha, rec); err == nil {
		t.Fatal("set succeeded into a directory that does not exist")
	}
	if err := os.Mkdir(filepath.Join(dir, "gone"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := p.set(idAlpha, rec); err != nil {
		t.Fatalf("the retry failed: %v", err)
	}
	if _, ok := loadParkReady(path)[idAlpha]; !ok {
		t.Error("an identical record was skipped after its write had failed, so it never reached the disk")
	}
}

func TestRemovingAParkReadyRecordForgetsOnlyThatSession(t *testing.T) {
	p, path := readyFile(t)
	other := everyWakeInput()
	other.ID = idGamma
	if err := p.set(idAlpha, everyWakeInput()); err != nil {
		t.Fatal(err)
	}
	if err := p.set(idBeta, other); err != nil {
		t.Fatal(err)
	}
	if err := p.remove(idAlpha); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got := loadParkReady(path)
	if _, ok := got[idAlpha]; ok || len(got) != 1 {
		t.Errorf("after removing %s the file holds %+v, want only %s", idAlpha, got, idBeta)
	}
}

// The file goes rather than being emptied, for the roster's reason: its absence
// is the legible marker that a daemon shut down properly.
func TestClearingTheParkReadyFileRemovesIt(t *testing.T) {
	p, path := readyFile(t)
	if err := p.set(idAlpha, everyWakeInput()); err != nil {
		t.Fatal(err)
	}
	if err := p.clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file survives clear: %v", err)
	}
	if err := p.clear(); err != nil {
		t.Errorf("clearing a file that is already gone is the ordinary case, got %v", err)
	}
}

// What the reaper could not finish stays, and only that: retain replaces the
// file the way the roster's retain does, and an empty set removes it.
func TestRetainingKeepsExactlyTheRecordsGiven(t *testing.T) {
	p, path := readyFile(t)
	if err := p.set(idAlpha, everyWakeInput()); err != nil {
		t.Fatal(err)
	}
	keep := everyWakeInput()
	keep.ID = idGamma
	if err := p.retain(map[string]parkedRecord{idBeta: keep}); err != nil {
		t.Fatalf("retain: %v", err)
	}
	got := loadParkReady(path)
	if _, ok := got[idBeta]; !ok || len(got) != 1 {
		t.Errorf("after retain the file holds %+v, want only %s", got, idBeta)
	}
	if err := p.retain(nil); err != nil {
		t.Fatalf("retain(nil): %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("retaining nothing left the file: %v", err)
	}
}

func TestTheParkReadyFileIsPrivateToItsOwner(t *testing.T) {
	p, path := readyFile(t)
	if err := p.set(idAlpha, everyWakeInput()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != parkReadyPerm {
		t.Errorf("mode %o, want %o: it names every agent on the machine and the directories they ran in", perm, parkReadyPerm)
	}
}

func TestAnUnreadableParkReadyFileIsReportedAndReadsAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), parkReadyName)
	if err := os.WriteFile(path, []byte("{not json"), parkReadyPerm); err != nil {
		t.Fatal(err)
	}
	var logged strings.Builder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if got := loadParkReady(path); len(got) != 0 {
		t.Errorf("an unreadable file read as %+v", got)
	}
	if !strings.Contains(logged.String(), path) {
		t.Errorf("nothing said which file could not be read: %q", logged.String())
	}
}

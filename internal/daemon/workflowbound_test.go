// What WorkflowRuns and WorkflowAgentHistory refuse to read: a session
// directory that is a symlink, and more of a session's runs than one reply
// holds - chosen before any record is opened.

package daemon

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A session directory that is itself a symlink would make wherever it points
// the containment root, so nothing under it is read - valid records and
// transcripts included.
func TestASymlinkedSessionDirectoryIsNeverRead(t *testing.T) {
	path := plantTranscript(t, wfID, userLine("hello"))
	elsewhere := t.TempDir()
	if err := os.MkdirAll(filepath.Join(elsewhere, "workflows"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeWorkflowFile(t, elsewhere, "wf_a.json", runRecordA)
	plantWorkflowAgentTranscript(t, elsewhere, "wf_a", "abc123")
	if err := os.Symlink(elsewhere, strings.TrimSuffix(path, ".jsonl")); err != nil {
		t.Fatal(err)
	}

	if runs, err := WorkflowRuns(wfID); err != nil || len(runs) != 0 {
		t.Errorf("WorkflowRuns = %d runs, %v; want none through a symlinked session directory", len(runs), err)
	}
	if events, err := WorkflowAgentHistory(wfID, "abc123"); err != nil || len(events) != 0 {
		t.Errorf("WorkflowAgentHistory = %d events, %v; want none through a symlinked session directory", len(events), err)
	}
}

// recordJSON is a minimal record for task id, padded by pad bytes of summary.
func recordJSON(id string, started time.Time, pad int) string {
	return fmt.Sprintf(`{"taskId":%q,"workflowName":"n","summary":%q,"status":"completed","startTime":%d,"script":"phase('x')\n"}`,
		id, strings.Repeat("s", pad), started.UnixMilli())
}

// plantRuns writes n records, the i-th last modified i minutes after the
// first, and returns their paths oldest first.
func plantRuns(t *testing.T, dir string, n, pad int) []string {
	t.Helper()
	base := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	paths := make([]string, n)
	for i := range n {
		at := base.Add(time.Duration(i) * time.Minute)
		paths[i] = writeWorkflowFile(t, dir, fmt.Sprintf("wf_%02d.json", i), recordJSON(fmt.Sprintf("w%02d", i), at, pad))
		if err := os.Chtimes(paths[i], at, at); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

// capturedLog is everything logf writes until the test ends.
func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

func TestOnlyTheNewestRunsByModificationTimeAreRead(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	paths := plantRuns(t, dir, maxWorkflowRuns+10, 0)
	sentinel := paths[0] // the oldest: unreadable, so opening it would log
	if err := os.Chmod(sentinel, 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(sentinel); err == nil {
		_ = f.Close()
		t.Skip("this user can read a mode-000 file, so the sentinel proves nothing")
	}
	logged := capturedLog(t)

	runs, err := WorkflowRuns(wfID)
	if err != nil {
		t.Fatalf("WorkflowRuns: %v", err)
	}
	if len(runs) != maxWorkflowRuns {
		t.Fatalf("read %d runs, want the %d newest", len(runs), maxWorkflowRuns)
	}
	for i, r := range runs {
		if want := fmt.Sprintf("w%02d", maxWorkflowRuns+9-i); r.TaskID != want {
			t.Errorf("run %d is %s, want %s: newest first, the oldest ten never read", i, r.TaskID, want)
		}
	}
	if strings.Contains(logged.String(), filepath.Base(sentinel)) {
		t.Errorf("the oldest record was opened: %s", logged)
	}
}

// Records that together outgrow one reply stop the read before the one that
// would take it over the bound.
func TestTheRunReadStopsAtTheReplyBound(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	plantRuns(t, dir, 4, maxRunsBytes/2-1024)

	runs, err := WorkflowRuns(wfID)
	if err != nil {
		t.Fatalf("WorkflowRuns: %v", err)
	}
	if len(runs) != 2 || runs[0].TaskID != "w03" || runs[1].TaskID != "w02" {
		t.Fatalf("read %d runs (%v), want the two newest that fit in %d bytes", len(runs), taskIDs(runs), maxRunsBytes)
	}
}

// The daemon reads a run's script to save it; no client ever draws one, so
// none travels.
func TestAWorkflowsReplyCarriesNoScript(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	writeWorkflowFile(t, dir, "wf_a.json", runRecordA)
	s := newServer(filepath.Join(t.TempDir(), "s"))
	c := newClient(nil)
	s.sendWorkflows(c, wfID)

	f := sentFrame(t, c)
	if f.Kind != rpc.FrameWorkflowsReply || f.Workflow == nil || len(f.Workflow.Runs) != 1 {
		t.Fatalf("reply = %+v, want one run", f)
	}
	if got := f.Workflow.Runs[0].Script; got != "" {
		t.Errorf("the reply carries the run's script %q", got)
	}
}

func taskIDs(runs []core.WorkflowRun) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.TaskID
	}
	return out
}

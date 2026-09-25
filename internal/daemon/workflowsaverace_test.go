// A save that fails part-way, and a directory swapped for a symlink between
// saveWorkflow's checks and its writes - the two seams workflowsave.go keeps
// for exactly these, since neither failure can be produced from outside it.

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// failingWrite swaps the script write for one that writes half and fails,
// the way a full disk does, until the test ends.
func failingWrite(t *testing.T) {
	t.Helper()
	was := writeWorkflowScript
	writeWorkflowScript = func(f *os.File, s string) (int, error) {
		n, _ := f.WriteString(s[:len(s)/2])
		return n, errors.New("no space left on device")
	}
	t.Cleanup(func() { writeWorkflowScript = was })
}

// swapAfterChecks runs swap between saveWorkflow's symlink checks and its
// writes, until the test ends.
func swapAfterChecks(t *testing.T, swap func()) {
	t.Helper()
	was := afterWorkflowChecks
	afterWorkflowChecks = swap
	t.Cleanup(func() { afterWorkflowChecks = was })
}

func requireEmptyDir(t *testing.T, dir, why string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		t.Errorf("%s: %s holds %s", why, dir, e.Name())
	}
}

// A write that fails leaves no file at the target and no temp beside it, so
// the retry is a save rather than "already exists" over half a script.
func TestAFailedSaveLeavesNothingAndARetrySucceeds(t *testing.T) {
	for _, project := range []bool{true, false} {
		dir := filepath.Join(t.TempDir(), ".claude", "workflows")
		t.Run(map[bool]string{true: "project", false: "personal"}[project], func(t *testing.T) {
			failingWrite(t)
			if path, err := saveWorkflow(dir, "count-lines", "phase('Count')\n", project); err == nil {
				t.Fatalf("a failed write reported a save at %s", path)
			}
			requireEmptyDir(t, dir, "after a failed write")
		})
		path, err := saveWorkflow(dir, "count-lines", "phase('Count')\n", project)
		if err != nil {
			t.Fatalf("project=%v: the retry after a failed write was refused: %v", project, err)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "phase('Count')\n" {
			t.Errorf("project=%v: the retry wrote %q (%v), want the whole script", project, raw, err)
		}
	}
}

// The frame answers a failed write as the refusal it is, never as a save.
func TestAFailedSaveIsAnsweredAsARefusal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	failingWrite(t)
	f := saveFrameReply(t, "count-lines")
	if f.Kind != rpc.FrameError || !strings.Contains(f.Text, "no space left") {
		t.Fatalf("reply = %+v, want a refusal naming the failed write", f)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "workflows", "count-lines.js")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed save left a file at the target (stat: %v)", err)
	}
}

// .claude/workflows swapped for a symlink out of the project after the checks
// passed: every write goes through the project's own root, so it is refused and
// nothing lands where the link points.
func TestAWorkflowsDirSwappedForASymlinkAfterTheChecksIsRefused(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(base, ".claude", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	swapAfterChecks(t, func() {
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, dir); err != nil {
			t.Fatal(err)
		}
	})
	if path, err := saveWorkflow(dir, "count-lines", "phase('Count')\n", true); err == nil {
		t.Errorf("saved through a swapped-in symlink to %s", path)
	}
	requireEmptyDir(t, outside, "the symlink's target")
}

// The same at personal scope: the workflows directory moved away and a
// symlink left in its place after the checks. The write follows the directory
// it opened, never the link.
func TestAPersonalDirSwappedForASymlinkAfterTheChecksWritesNothingThere(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(base, "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	swapAfterChecks(t, func() {
		if err := os.Rename(dir, dir+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, dir); err != nil {
			t.Fatal(err)
		}
	})
	_, _ = saveWorkflow(dir, "count-lines", "phase('Count')\n", false)
	requireEmptyDir(t, outside, "the symlink's target")
}

// saveFrameReply is the daemon's answer to a personal-scope save of a running
// workflow's script under name.
func saveFrameReply(t *testing.T, name string) rpc.Frame {
	t.Helper()
	s := newServer(filepath.Join(t.TempDir(), "s"))
	a := &agent{id: wfID, name: "iris", dir: t.TempDir(), runningTasks: make(map[string]core.Event)}
	a.runningTasks["w1"] = core.Event{Task: &core.TaskUpdate{ID: "w1",
		Workflow: &core.WorkflowUpdate{Script: "phase('Count')\n"}}}
	s.agents[wfID] = a
	c := newClient(nil)
	s.saveWorkflowFrame(c, rpc.Frame{Kind: rpc.FrameSaveWorkflow, SessionID: wfID,
		Workflow: &rpc.WorkflowFrame{Task: "w1", Name: name, Scope: rpc.ScopeUser}})
	select {
	case f := <-c.out:
		return f
	default:
		t.Fatal("saveWorkflowFrame enqueued nothing")
		return rpc.Frame{}
	}
}

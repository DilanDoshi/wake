// Save writes a workflow run's own script to a reusable /<name> command
// file - workflowsave.go's counterpart, one directory over from
// workflowdisk_test.go's read-back tests.

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// --- projectWorkflowDir: Claude Code's own project-scope rule ---

func TestProjectWorkflowDirFindsTheRepositoryRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	cwd := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	want := filepath.Join(repo, ".claude", "workflows")
	if got := projectWorkflowDir(cwd); got != want {
		t.Errorf("projectWorkflowDir(%q) = %q, want %q", cwd, got, want)
	}
}

func TestProjectWorkflowDirPrefersAnExistingNearerOne(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	nearer := filepath.Join(repo, "a", ".claude", "workflows")
	if err := os.MkdirAll(nearer, 0o755); err != nil {
		t.Fatalf("mkdir nearer: %v", err)
	}
	cwd := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	if got := projectWorkflowDir(cwd); got != nearer {
		t.Errorf("projectWorkflowDir(%q) = %q, want the existing nearer one %q", cwd, got, nearer)
	}
}

func TestProjectWorkflowDirWithNoRepositoryUsesCwd(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	want := filepath.Join(cwd, ".claude", "workflows")
	if got := projectWorkflowDir(cwd); got != want {
		t.Errorf("projectWorkflowDir(%q) = %q, want %q", cwd, got, want)
	}
}

// TestProjectWorkflowDirWithNoRepositoryNeverCrossesIntoAnAncestorsDirectory
// is the regression for the scope-crossing bug: a non-git cwd whose ancestor
// (standing in for $HOME) already has an existing .claude/workflows must
// still resolve to <cwd>/.claude/workflows, not the ancestor's - a
// project-scope save from a repository-less directory under $HOME must never
// silently land in the personal-scope directory.
func TestProjectWorkflowDirWithNoRepositoryNeverCrossesIntoAnAncestorsDirectory(t *testing.T) {
	base := t.TempDir()
	ancestorWorkflows := filepath.Join(base, ".claude", "workflows")
	if err := os.MkdirAll(ancestorWorkflows, 0o755); err != nil {
		t.Fatalf("mkdir ancestor's .claude/workflows: %v", err)
	}
	cwd := filepath.Join(base, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	want := filepath.Join(cwd, ".claude", "workflows")
	if got := projectWorkflowDir(cwd); got != want {
		t.Fatalf("projectWorkflowDir(%q) = %q, want %q (an ancestor's existing .claude/workflows must not be used with no repository)",
			cwd, got, want)
	}
}

// TestProjectWorkflowDirNeverReturnsAboveTheRepositoryRoot is the same
// bound the other direction: an ancestor *above* the repository root having
// an existing .claude/workflows must not be found either, even though it is
// the nearer one on disk once the boundary is ignored.
func TestProjectWorkflowDirNeverReturnsAboveTheRepositoryRoot(t *testing.T) {
	outer := t.TempDir()
	outerWorkflows := filepath.Join(outer, ".claude", "workflows")
	if err := os.MkdirAll(outerWorkflows, 0o755); err != nil {
		t.Fatalf("mkdir outer .claude/workflows: %v", err)
	}
	repo := filepath.Join(outer, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	cwd := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("mkdir cwd: %v", err)
	}

	want := filepath.Join(repo, ".claude", "workflows")
	if got := projectWorkflowDir(cwd); got != want {
		t.Fatalf("projectWorkflowDir(%q) = %q, want the repository's own %q (an ancestor above the root must not be searched)",
			cwd, got, want)
	}
}

// --- userWorkflowDir: $CLAUDE_CONFIG_DIR, else ~/.claude/workflows ---

func TestUserWorkflowDirUsesConfigDirWhenSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	want := filepath.Join(dir, "workflows")
	if got := userWorkflowDir(); got != want {
		t.Errorf("userWorkflowDir() = %q, want %q", got, want)
	}
}

func TestUserWorkflowDirFallsBackToHome(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".claude", "workflows")
	if got := userWorkflowDir(); got != want {
		t.Errorf("userWorkflowDir() = %q, want %q", got, want)
	}
}

// --- saveWorkflow: the write itself ---

func TestSaveWorkflowWritesTheScriptAndCreatesDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude", "workflows")

	path, err := saveWorkflow(dir, "count-lines", "phase('Count')\n", true)
	if err != nil {
		t.Fatalf("saveWorkflow: %v", err)
	}
	want := filepath.Join(dir, "count-lines.js")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(raw) != "phase('Count')\n" {
		t.Errorf("wrote %q, want the script verbatim", raw)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %o, want 0644", info.Mode().Perm())
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o755 {
		t.Errorf("dir mode = %o, want 0755", dirInfo.Mode().Perm())
	}
}

func TestSaveWorkflowRefusesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "count-lines.js")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := saveWorkflow(dir, "count-lines", "new", false); err == nil {
		t.Fatal("saveWorkflow overwrote an existing file")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to say the file already exists", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(raw) != "old" {
		t.Errorf("file content = %q, want the original untouched", raw)
	}
}

func TestSaveWorkflowRefusesASymlinkedClaudeDirAtProjectScope(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	claudeLink := filepath.Join(base, ".claude")
	if err := os.Symlink(outside, claudeLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	dir := filepath.Join(claudeLink, "workflows")

	if _, err := saveWorkflow(dir, "count-lines", "script", true); err == nil {
		t.Fatal("saveWorkflow wrote through a symlinked .claude")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want it to name the symlink", err)
	}
}

func TestSaveWorkflowRefusesASymlinkedWorkflowsDirAtProjectScope(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, ".claude"), 0o755); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	outside := t.TempDir()
	dir := filepath.Join(base, ".claude", "workflows")
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := saveWorkflow(dir, "count-lines", "script", true); err == nil {
		t.Fatal("saveWorkflow wrote through a symlinked .claude/workflows")
	} else if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want it to name the symlink", err)
	}
}

// TestSaveWorkflowUserScopeIgnoresASymlinkedClaudeDir proves the documented
// asymmetry: personal scope refuses only the target file, not .claude on
// the way there.
func TestSaveWorkflowUserScopeIgnoresASymlinkedClaudeDir(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	claudeLink := filepath.Join(base, ".claude")
	if err := os.Symlink(outside, claudeLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	dir := filepath.Join(claudeLink, "workflows")

	path, err := saveWorkflow(dir, "count-lines", "script", false)
	if err != nil {
		t.Fatalf("saveWorkflow refused a symlinked ~/.claude at personal scope: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("stat written file: %v", statErr)
	}
}

func TestSaveWorkflowRefusesASymlinkedTargetAtBothScopes(t *testing.T) {
	for _, project := range []bool{true, false} {
		dir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "elsewhere.js")
		if err := os.WriteFile(outside, []byte("old"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "count-lines.js")); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		if _, err := saveWorkflow(dir, "count-lines", "script", project); err == nil {
			t.Fatalf("project=%v: saveWorkflow overwrote a symlinked target", project)
		} else if !strings.Contains(err.Error(), "symlink") {
			t.Errorf("project=%v: error = %q, want it to name the symlink", project, err)
		}
	}
}

func TestSaveWorkflowRefusesAnInvalidName(t *testing.T) {
	dir := t.TempDir()
	if _, err := saveWorkflow(dir, "Not Valid!", "script", false); err == nil {
		t.Fatal("saveWorkflow accepted an invalid name")
	}
}

// --- workflowScript: running task, ended run record, neither ---

func TestWorkflowScriptFromARunningTask(t *testing.T) {
	a := &agent{runningTasks: make(map[string]core.Event)}
	a.runningTasks["w1"] = core.Event{Task: &core.TaskUpdate{ID: "w1",
		Workflow: &core.WorkflowUpdate{Script: "phase('Count')\n"}}}

	got, err := workflowScript(a, "unused-id", "w1")
	if err != nil {
		t.Fatalf("workflowScript: %v", err)
	}
	if got != "phase('Count')\n" {
		t.Errorf("script = %q, want the retained start event's own script", got)
	}
}

func TestWorkflowScriptFromAnEndedRunRecord(t *testing.T) {
	a := &agent{runningTasks: make(map[string]core.Event)}
	dir := workflowSessionDir(t, wfID)
	writeWorkflowFile(t, dir, "wf_a.json", runRecordA)

	got, err := workflowScript(a, wfID, "wsmc7r0xw")
	if err != nil {
		t.Fatalf("workflowScript: %v", err)
	}
	if got != "phase('Fan')\n" {
		t.Errorf("script = %q, want the run record's own script", got)
	}
}

func TestWorkflowScriptOfNeitherIsRefused(t *testing.T) {
	a := &agent{runningTasks: make(map[string]core.Event)}

	_, err := workflowScript(a, "some-id-with-no-transcript", "nope")
	if err == nil {
		t.Fatal("workflowScript found a script for a task that was never running and never ended")
	}
	if !strings.Contains(err.Error(), "no script for task nope") {
		t.Errorf("error = %q, want it to name the task", err)
	}
}

// --- saveWorkflowFrame: the wire verb ---

func TestSaveWorkflowFrameWritesAndAnswers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	s := newServer(filepath.Join(t.TempDir(), "s"))
	a := &agent{id: wfID, name: "iris", dir: t.TempDir(), runningTasks: make(map[string]core.Event)}
	a.runningTasks["w1"] = core.Event{Task: &core.TaskUpdate{ID: "w1",
		Workflow: &core.WorkflowUpdate{Script: "phase('Count')\n"}}}
	s.agents[wfID] = a

	c := newClient(nil)
	s.saveWorkflowFrame(c, rpc.Frame{Kind: rpc.FrameSaveWorkflow, SessionID: wfID,
		Workflow: &rpc.WorkflowFrame{Task: "w1", Name: "count-lines", Scope: rpc.ScopeUser}})

	var f rpc.Frame
	select {
	case f = <-c.out:
	default:
		t.Fatal("saveWorkflowFrame enqueued nothing")
	}
	if f.Kind != rpc.FrameWorkflowSaved || f.Workflow == nil {
		t.Fatalf("reply = %+v, want FrameWorkflowSaved{Workflow.Path}", f)
	}
	want := filepath.Join(home, ".claude", "workflows", "count-lines.js")
	if f.Workflow.Path != want {
		t.Fatalf("saved path = %q, want %q", f.Workflow.Path, want)
	}

	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(raw) != "phase('Count')\n" {
		t.Errorf("wrote %q, want the script", raw)
	}
}

func TestSaveWorkflowFrameRefusesAnUnknownSession(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "s"))
	c := newClient(nil)

	s.saveWorkflowFrame(c, rpc.Frame{Kind: rpc.FrameSaveWorkflow, SessionID: "nope",
		Workflow: &rpc.WorkflowFrame{Task: "w1", Name: "x", Scope: rpc.ScopeUser}})

	var f rpc.Frame
	select {
	case f = <-c.out:
	default:
		t.Fatal("saveWorkflowFrame enqueued nothing")
	}
	if f.Kind != rpc.FrameError || !strings.Contains(f.Text, "unknown session") {
		t.Fatalf("reply = %+v, want a refusal naming the unknown session", f)
	}
}

// TestSaveWorkflowFrameRefusalNamesTheReason drives an invalid name through
// the full frame handler and checks the FrameError's text is the specific
// reason, not a generic failure.
func TestSaveWorkflowFrameRefusalNamesTheReason(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newServer(filepath.Join(t.TempDir(), "s"))
	a := &agent{id: wfID, name: "iris", dir: t.TempDir(), runningTasks: make(map[string]core.Event)}
	a.runningTasks["w1"] = core.Event{Task: &core.TaskUpdate{ID: "w1",
		Workflow: &core.WorkflowUpdate{Script: "phase('Count')\n"}}}
	s.agents[wfID] = a

	c := newClient(nil)
	s.saveWorkflowFrame(c, rpc.Frame{Kind: rpc.FrameSaveWorkflow, SessionID: wfID,
		Workflow: &rpc.WorkflowFrame{Task: "w1", Name: "Not Valid!", Scope: rpc.ScopeUser}})

	var f rpc.Frame
	select {
	case f = <-c.out:
	default:
		t.Fatal("saveWorkflowFrame enqueued nothing")
	}
	if f.Kind != rpc.FrameError || !strings.Contains(f.Text, "workflow name") {
		t.Fatalf("reply = %+v, want a refusal naming the bad workflow name", f)
	}
}

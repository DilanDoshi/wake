package daemon

// Save writes a dynamic Workflow() run's own script to a reusable
// /<name> command file, the way Claude Code's own /workflows -> s does (spec
// 3.2 "Save"). **The wire carries a name and a scope; the daemon owns the
// path and the bytes** - --debug-file's own ruling, one field over: a path
// on the wire would let anything that can dial this socket choose where a
// file lands, so a client names a scope and Wake resolves the directory.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// workflowDirPerm and workflowFilePerm are what saveWorkflow creates with -
// a directory anyone in this session can list into, a script anyone can
// read, matching an ordinary saved command file rather than the debug log's
// 0o700 (that one carries a session's prompts; this one is meant to be run).
const (
	workflowDirPerm  = 0o755
	workflowFilePerm = 0o644
	workflowExt      = ".js"
)

// workflowScript is one workflow dispatch's own script: the retained
// task_started event's Workflow.Script while the run is still going -
// a.runningTasks, the same map replayRunningTasks hands a late client - or
// the matching wf_*.json record's Script once the run has ended and left
// that map. id is the transcript id WorkflowRuns reads under (the caller's
// s.transcriptID(sessionID)), which can differ from a's own id after a
// /clear.
func workflowScript(a *agent, id, taskID string) (string, error) {
	a.mu.Lock()
	ev, running := a.runningTasks[taskID]
	a.mu.Unlock()
	if running && ev.Task != nil && ev.Task.Workflow != nil && ev.Task.Workflow.Script != "" {
		return ev.Task.Workflow.Script, nil
	}

	runs, err := WorkflowRuns(id)
	if err != nil {
		return "", err
	}
	for _, r := range runs {
		if r.TaskID == taskID && r.Script != "" {
			return r.Script, nil
		}
	}
	return "", fmt.Errorf("no script for task %s", taskID)
}

// projectWorkflowDir is Claude Code's own documented project-scope rule: the
// closest existing .claude/workflows walking up from cwd to the repository
// root, else <root>/.claude/workflows, else <cwd>/.claude/workflows when cwd
// is not inside a repository at all.
//
// The repository root has to be found *first* and the existing-directory
// search bounded to it: searching upward for an existing .claude/workflows
// before knowing where the root is (or whether one exists) lets a cwd with
// no repository at all walk all the way to the filesystem root and return
// the first ancestor that happens to have one - almost always
// $HOME/.claude/workflows, the personal-scope directory this same feature
// creates, which would cross project/personal scope silently. So a cwd with
// no repository never searches its ancestors at all, and a cwd with one
// never searches past it.
func projectWorkflowDir(cwd string) string {
	root, ok := repositoryRoot(cwd)
	if !ok {
		return filepath.Join(cwd, ".claude", "workflows")
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if candidate := filepath.Join(dir, ".claude", "workflows"); existingDir(candidate) {
			return candidate
		}
		if dir == root {
			break
		}
	}
	return filepath.Join(root, ".claude", "workflows")
}

// repositoryRoot walks up from cwd looking for the nearest ancestor with a
// .git entry - file or directory, no git subprocess (hasGitEntry's own
// reason). ok is false when no ancestor up to the filesystem root has one.
func repositoryRoot(cwd string) (string, bool) {
	dir := cwd
	for {
		if hasGitEntry(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false // reached the filesystem root; no repository found
		}
		dir = parent
	}
}

// userWorkflowDir is Claude Code's own personal scope:
// $CLAUDE_CONFIG_DIR/workflows when that is set, else ~/.claude/workflows.
func userWorkflowDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "workflows")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "workflows")
}

func existingDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// hasGitEntry reports whether dir is a repository root - a .git entry marks
// one whether it is an ordinary checkout's directory or a worktree's
// "gitdir: ..." file.
func hasGitEntry(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// saveWorkflow writes script to <dir>/<name>.js, matching Claude Code's
// documented /workflows save, and says where only once the file is whole.
// Every write goes through workflowRoot's os.Root, so a directory swapped for
// a symlink after the checks below cannot take the write outside it. The
// target is refused as a symlink in both scopes, and an existing file is
// refused too - overwriting a saved workflow is not a keystroke - made atomic
// by publish's link rather than a stat-then-write race.
func saveWorkflow(dir, name, script string, project bool) (string, error) {
	if err := rpc.ValidWorkflowName(name); err != nil {
		return "", err
	}
	root, rel, err := workflowRoot(dir, project)
	if err != nil {
		return "", err
	}
	defer closeRoot(root)
	path, file := filepath.Join(dir, name+workflowExt), filepath.Join(rel, name+workflowExt)
	if rootSymlink(root, file) {
		return "", fmt.Errorf("%s is a symlink, refusing to overwrite it", path)
	}
	afterWorkflowChecks()
	if err := root.MkdirAll(rel, workflowDirPerm); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	if err := publish(root, file, script, path); err != nil {
		return "", err
	}
	return path, nil
}

// workflowRoot opens the directory a save writes through, and where the
// workflows directory sits inside it. At project scope that is the project
// base above .claude, whose .claude and .claude/workflows are refused if
// either is a symlink - a name chosen on the wire must not redirect the write
// outside the repository it names. At personal scope it is the workflows
// directory itself, which may sit behind the operator's own symlinked
// ~/.claude, so only the target is fenced there.
func workflowRoot(dir string, project bool) (*os.Root, string, error) {
	base, rel := dir, "."
	if project {
		base, rel = filepath.Dir(filepath.Dir(dir)), filepath.Join(filepath.Base(filepath.Dir(dir)), filepath.Base(dir))
	} else if err := os.MkdirAll(dir, workflowDirPerm); err != nil {
		return nil, "", fmt.Errorf("create %s: %w", dir, err)
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", fmt.Errorf("open %s: %w", base, err)
	}
	if !project {
		return root, rel, nil
	}
	for _, p := range []string{filepath.Dir(rel), rel} {
		if rootSymlink(root, p) {
			closeRoot(root)
			return nil, "", fmt.Errorf("%s is a symlink, refusing to save a workflow through it", filepath.Join(base, p))
		}
	}
	return root, rel, nil
}

// publish writes script to a temp file beside file, syncs and closes it, and
// links it into place: the link fails if file exists, so the no-overwrite
// rule stays atomic, and every path out removes the temp - a failed save
// leaves nothing a retry would call "already exists". path is file as the
// operator knows it, for the errors.
func publish(root *os.Root, file, script, path string) error {
	tmp := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+"."+uuid.NewString()+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, workflowFilePerm)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() {
		if err := root.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			logf("wake: could not remove %s's temp file %s: %v", path, tmp, err)
		}
	}()
	_, err = writeWorkflowScript(f, script)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := root.Link(tmp, file); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists", path)
		}
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}

// rootSymlink reports whether name exists under root and is itself a
// symlink. A name that does not exist yet is not refused - creating it is
// saveWorkflow's own job - which is why this is Lstat.
func rootSymlink(root *os.Root, name string) bool {
	info, err := root.Lstat(name)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// closeRoot closes a save's directory handle. A failure is logged rather than
// returned: by then the save it served has already succeeded or failed.
func closeRoot(root *os.Root) {
	if err := root.Close(); err != nil {
		logf("wake: could not close %s: %v", root.Name(), err)
	}
}

// saveWorkflowFrame answers a client's FrameSaveWorkflow: write one of this
// session's workflow runs to a reusable /<name> command file. Refused like
// every other verb for an unknown session; every other refusal names its
// reason in the FrameError's text.
func (s *server) saveWorkflowFrame(c *client, f rpc.Frame) {
	a, ok := s.agent(f.SessionID)
	if !ok {
		c.enqueue(errorFrame(f.SessionID, "unknown session "+f.SessionID))
		return
	}
	if f.Workflow == nil {
		c.enqueue(errorFrame(f.SessionID, "a workflow save needs a task, a name and a scope"))
		return
	}

	script, err := workflowScript(a, s.transcriptID(f.SessionID), f.Workflow.Task)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}

	var dir string
	project := f.Workflow.Scope == rpc.ScopeProject
	switch f.Workflow.Scope {
	case rpc.ScopeProject:
		a.mu.Lock()
		cwd := a.runningIn()
		a.mu.Unlock()
		dir = projectWorkflowDir(cwd)
	case rpc.ScopeUser:
		dir = userWorkflowDir()
	default:
		c.enqueue(errorFrame(f.SessionID, fmt.Sprintf("unknown workflow save scope %q", f.Workflow.Scope)))
		return
	}

	path, err := saveWorkflow(dir, f.Workflow.Name, script, project)
	if err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
		return
	}
	c.enqueue(rpc.Frame{Kind: rpc.FrameWorkflowSaved, SessionID: f.SessionID, Workflow: &rpc.WorkflowFrame{Path: path}})
}

// Test seams (workflowsaverace_test.go): the moment between the symlink checks
// and the writes, and the write itself.
var (
	afterWorkflowChecks = func() {}
	writeWorkflowScript = (*os.File).WriteString
)

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

	"github.com/DilanDoshi/wake/internal/rpc"
)

// workflowDirPerm and workflowFilePerm are what saveWorkflow creates with -
// a directory anyone in this session can list into, a script anyone can
// read, matching an ordinary saved command file rather than the debug log's
// 0o700 (that one carries a session's prompts; this one is meant to be run).
const (
	workflowDirPerm  = 0o755
	workflowFilePerm = 0o644
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

// isSymlink reports whether path exists and is itself a symlink. A path
// that does not exist yet is not refused here - creating it is
// saveWorkflow's own job - which is why this is Lstat rather than the
// followed-through Stat existingDir uses.
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// saveWorkflow writes script to <dir>/<name>.js, matching Claude Code's
// documented /workflows save. project gates the two checks that only apply
// to a project-scope save: .claude and .claude/workflows are refused if
// either is a symlink, since a name chosen on the wire must not redirect the
// write outside the repository it names. The target file is refused as a
// symlink in both scopes, and an existing file is refused too - overwriting
// a saved workflow is not a keystroke - made atomic by O_EXCL rather than a
// stat-then-write race.
func saveWorkflow(dir, name, script string, project bool) (string, error) {
	if err := rpc.ValidWorkflowName(name); err != nil {
		return "", err
	}
	if project {
		claudeDir := filepath.Dir(dir)
		if isSymlink(claudeDir) {
			return "", fmt.Errorf("%s is a symlink, refusing to save a workflow through it", claudeDir)
		}
		if isSymlink(dir) {
			return "", fmt.Errorf("%s is a symlink, refusing to save a workflow through it", dir)
		}
	}
	if err := os.MkdirAll(dir, workflowDirPerm); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	path := filepath.Join(dir, name+".js")
	if isSymlink(path) {
		return "", fmt.Errorf("%s is a symlink, refusing to overwrite it", path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, workflowFilePerm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%s already exists", path)
		}
		return "", fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(script); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
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

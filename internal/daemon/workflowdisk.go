package daemon

// A dynamic Workflow() run's own records and its agents' own transcripts,
// read back off claude's disk exactly as an ordinary conversation is -
// history.go's own reasons, one directory over: never built from a slug,
// found beside a session's own transcript, and a malformed record costs
// itself, never the whole read.
//
// <sessiondir>/workflows/wf_<runId>.json is one run's own record, written by
// claude's Workflow() runtime once a run ends or is stopped.
// <sessiondir>/subagents/workflows/wf_<runId>/agent-<agentId>.jsonl is one
// workflow agent's own isSidechain:true transcript - the only route to its
// words, since a workflow agent forwards nothing live
// (core.DecodeSidechainLine's header).

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// maxWorkflowRuns bounds how many of a session's own runs WorkflowRuns hands
// back, newest first - a long-lived session's older runs are simply never
// read rather than read and then truncated.
const maxWorkflowRuns = 50

// maxRunBytes bounds one run record. Its progress snapshot carries a
// prompt/result preview per agent, so a wide fan-out is not a fixed size -
// and unlike a conversation's tail-bounded scan, a record is read whole.
const maxRunBytes = 8 << 20

// claudeSessionDir is a session's own directory beside its transcript file -
// <projects>/<slug>/<uuid> with ".jsonl" trimmed off - where claude's dynamic
// Workflow() runtime writes a run's own record and each agent's own
// transcript. Named for worktree.go's sessionDir, the directory a *spawn*
// runs in - a different idea entirely, and both live in this package.
func claudeSessionDir(transcript string) string {
	return strings.TrimSuffix(transcript, ".jsonl")
}

// resolvedSessionDir is claudeSessionDir(path) with every symlink in it
// resolved - the baseline withinSessionDir checks a matched file against. Its
// own absence is not an error: a session that has never run a workflow has no
// <uuid>/ directory at all, which WorkflowRuns/WorkflowAgentHistory read the
// same way History reads a session with no transcript - nothing to read.
func resolvedSessionDir(path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(claudeSessionDir(path))
	return resolved, err == nil
}

// withinSessionDir resolves every symlink in path - an intermediate
// directory as well as a final component - and reports whether the result is
// still inside dir, which is already resolved.
//
// regularTranscript's Lstat alone is not this fence: Lstat refuses only a
// symlinked *final* component, but it still follows a symlinked intermediate
// directory to reach whatever the final component names, so filepath.Glob
// under a symlinked workflows/ or a symlinked subagents/workflows/<run>/
// returns a path whose Lstat reports an ordinary regular file - a match this
// package used to accept from anywhere on the machine. EvalSymlinks resolves
// the whole path, which is what a directory-level escape needs caught on.
func withinSessionDir(dir, path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(dir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return resolved, true
}

// WorkflowRuns is a session's own dynamic Workflow() runs, read back off
// their wf_*.json records - newest first, capped at maxWorkflowRuns. A
// session with no transcript answers with nothing rather than an error,
// History's own ruling for one that has never taken a turn.
func WorkflowRuns(id string) ([]core.WorkflowRun, error) {
	path, ok := transcriptPath(id)
	if !ok {
		return nil, nil
	}
	dir, ok := resolvedSessionDir(path)
	if !ok {
		return nil, nil // no workflow has ever run under this session
	}
	matches, err := filepath.Glob(filepath.Join(claudeSessionDir(path), "workflows", "wf_*.json"))
	if err != nil {
		return nil, err
	}

	var runs []core.WorkflowRun
	for _, m := range matches {
		info, ok := regularTranscript(m)
		if !ok {
			continue // a symlinked final component, or gone since Glob listed it
		}
		resolved, ok := withinSessionDir(dir, m)
		if !ok {
			logf("wake: workflow run record %s resolves outside its session directory, skipped", m)
			continue
		}
		if info.Size() > maxRunBytes {
			logf("wake: workflow run record %s is %d bytes, over the %d bound, skipped", m, info.Size(), maxRunBytes)
			continue
		}
		raw, err := os.ReadFile(resolved)
		if err != nil {
			logf("wake: could not read workflow run record %s: %v", m, err)
			continue
		}
		run, err := core.DecodeWorkflowRun(raw)
		if err != nil {
			// One bad record costs itself, never the rest of the list.
			logf("wake: workflow run record %s could not be decoded: %v", m, err)
			continue
		}
		runs = append(runs, run)
	}

	sort.Slice(runs, func(i, j int) bool { return runs[i].Started.After(runs[j].Started) })
	if len(runs) > maxWorkflowRuns {
		runs = runs[:maxWorkflowRuns]
	}
	return runs, nil
}

// WorkflowAgentHistory is one workflow agent's own transcript - the
// isSidechain:true lines under <sessiondir>/subagents/workflows/wf_*/agent-
// <agentID>.jsonl, read through the same bounded scanner and
// historyEvents/historyBytes ring History reads an ordinary conversation's
// tail through. An unknown agent id is not an error: the run may have named
// an agent this session never reached - the caller draws nothing, which is
// what it would have drawn anyway.
func WorkflowAgentHistory(id, agentID string) ([]core.Event, error) {
	if err := rpc.ValidWorkflowAgentID(agentID); err != nil {
		return nil, err
	}
	path, ok := transcriptPath(id)
	if !ok {
		return nil, nil
	}
	dir, ok := resolvedSessionDir(path)
	if !ok {
		return nil, nil // no workflow has ever run under this session
	}
	pattern := filepath.Join(claudeSessionDir(path), "subagents", "workflows", "*", "agent-"+agentID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var agentPath string
	for _, m := range matches {
		if _, ok := regularTranscript(m); !ok {
			continue // a symlinked final component, or gone since Glob listed it
		}
		resolved, ok := withinSessionDir(dir, m)
		if !ok {
			logf("wake: workflow agent transcript %s resolves outside its session directory, skipped", m)
			continue
		}
		agentPath = resolved
		break
	}
	if agentPath == "" {
		return nil, nil
	}

	f, err := os.Open(agentPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var ring []core.Event
	total := 0
	br := bufio.NewReaderSize(f, 64*1024)
	for {
		line, lineErr := readTranscriptLine(br)
		if len(line) > 0 {
			events, decErr := core.DecodeSidechainLine(line)
			if decErr != nil {
				// One unreadable line is not an unreadable transcript.
				logf("wake: workflow agent %s has a transcript line that could not be decoded: %v", agentID, decErr)
			}
			for _, ev := range events {
				ev.Raw = nil
				// Stamped to the id this function was called with - which is
				// s.transcriptID's *translated* post-/clear id at the
				// sendWorkflowAgent call site, not the id the client knows.
				// sendWorkflowAgent restamps with the client-facing one
				// afterward, the way answerHistory restamps History's own
				// events for the same reason.
				ev.SessionID = id
				total += len(ev.Text)
				ring = append(ring, ev)
				ring, total = trimRing(ring, total)
			}
		}
		if lineErr != nil {
			if errors.Is(lineErr, io.EOF) {
				return ring, nil
			}
			return nil, lineErr
		}
	}
}

// sendWorkflows answers a client's FrameWorkflows: one session's own runs.
func (s *server) sendWorkflows(c *client, id string) {
	runs, err := WorkflowRuns(s.transcriptID(id))
	if err != nil {
		c.enqueue(errorFrame(id, "could not read workflows: "+err.Error()))
		return
	}
	c.enqueue(rpc.Frame{Kind: rpc.FrameWorkflowsReply, SessionID: id, Workflow: &rpc.WorkflowFrame{Runs: runs}})
}

// sendWorkflowAgent answers a client's FrameWorkflowAgent: one workflow
// agent's own transcript, echoing back the agent id it was asked for.
func (s *server) sendWorkflowAgent(c *client, id, agentID string) {
	events, err := WorkflowAgentHistory(s.transcriptID(id), agentID)
	if err != nil {
		c.enqueue(errorFrame(id, "could not read that workflow agent's transcript: "+err.Error()))
		return
	}
	// Addressed by the id the client knows, whatever file it came out of -
	// answerHistory's own reason: s.transcriptID(id) above may be the
	// post-/clear claude id, and WorkflowAgentHistory stamped every event
	// with that one.
	for i := range events {
		events[i].SessionID = id
	}
	c.enqueue(rpc.Frame{Kind: rpc.FrameWorkflowAgentReply, SessionID: id,
		Workflow: &rpc.WorkflowFrame{Agent: agentID}, Events: events})
}

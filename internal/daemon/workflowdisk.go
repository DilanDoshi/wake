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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// maxWorkflowRuns bounds how many of a session's own runs WorkflowRuns reads:
// the newest by modification time, chosen before any record is opened, so a
// long-lived session's older runs cost a directory entry each and no read.
const maxWorkflowRuns = 50

// maxRunsBytes bounds what one WorkflowRuns reads in all, historyBytes's reason
// one reply over: a record carries a preview per agent, so a wide fan-out is
// not a fixed size, and every run read travels in the one reply.
const maxRunsBytes = historyBytes

// claudeSessionDir is a session's own directory beside its transcript file -
// <projects>/<slug>/<uuid> with ".jsonl" trimmed off - where claude's dynamic
// Workflow() runtime writes a run's own record and each agent's own
// transcript. Named for worktree.go's sessionDir, the directory a *spawn*
// runs in - a different idea entirely, and both live in this package.
func claudeSessionDir(transcript string) string {
	return strings.TrimSuffix(transcript, ".jsonl")
}

// sessionRoot opens id's own session directory as the os.Root every read
// under it goes through, so no symlinked directory or file inside can lead a
// read outside it. Its absence is not an error: a session that has never run
// a workflow has no <uuid>/ directory, which reads as nothing to read -
// History's own ruling for a session with no transcript. A session directory
// that is itself a symlink is refused, since it would make wherever it points
// the root; the directory opened is checked to be the one Lstat saw, so a swap
// between the two is refused too.
func sessionRoot(id string) (*os.Root, bool) {
	path, ok := transcriptPath(id)
	if !ok {
		return nil, false
	}
	dir := claudeSessionDir(path)
	seen, err := os.Lstat(dir)
	if err != nil || !seen.IsDir() {
		if err == nil {
			logf("wake: session directory %s is not a directory (%s), not read", dir, seen.Mode().Type())
		}
		return nil, false
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		logf("wake: could not open session directory %s: %v", dir, err)
		return nil, false
	}
	if opened, err := root.Stat("."); err != nil || !os.SameFile(seen, opened) {
		logf("wake: session directory %s changed while it was opened, not read", dir)
		closeRoot(root)
		return nil, false
	}
	return root, true
}

// runRecord is one wf_*.json record found under a session, before it is read.
type runRecord struct {
	file string
	info fs.FileInfo
}

// runRecords is the session's regular wf_*.json records, newest modified
// first. A workflows/ it cannot list - missing, unreadable, or a symlink out
// of the root - reads as none, the way a failed Glob did.
func runRecords(root *os.Root) []runRecord {
	entries, err := fs.ReadDir(root.FS(), "workflows")
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logf("wake: could not list %s/workflows: %v", root.Name(), err)
		}
		return nil
	}
	var out []runRecord
	for _, e := range entries {
		if ok, _ := filepath.Match("wf_*.json", e.Name()); !ok || !e.Type().IsRegular() {
			continue // a symlinked record is never followed
		}
		if info, err := e.Info(); err == nil {
			out = append(out, runRecord{file: filepath.Join("workflows", e.Name()), info: info})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].info.ModTime().After(out[j].info.ModTime()) })
	return out
}

// WorkflowRuns is a session's own dynamic Workflow() runs, read back off
// their wf_*.json records - the maxWorkflowRuns newest, read until the next
// would take the reply past maxRunsBytes, and answered newest first. A
// session with no transcript answers with nothing rather than an error,
// History's own ruling for one that has never taken a turn.
func WorkflowRuns(id string) ([]core.WorkflowRun, error) {
	root, ok := sessionRoot(id)
	if !ok {
		return nil, nil
	}
	defer closeRoot(root)

	var runs []core.WorkflowRun
	read := 0
	for _, rec := range firstN(runRecords(root), maxWorkflowRuns) {
		if rec.info.Size() > maxRunsBytes {
			logf("wake: workflow run record %s is %d bytes, over the %d bound, skipped", rec.file, rec.info.Size(), maxRunsBytes)
			continue
		}
		raw, err := readRecord(root, rec.file, maxRunsBytes-read)
		if errors.Is(err, errOverBound) {
			break // the rest are older still
		}
		if err != nil {
			logf("wake: could not read workflow run record %s: %v", rec.file, err)
			continue
		}
		read += len(raw)
		run, err := core.DecodeWorkflowRun(raw)
		if err != nil {
			// One bad record costs itself, never the rest of the list.
			logf("wake: workflow run record %s could not be decoded: %v", rec.file, err)
			continue
		}
		runs = append(runs, run)
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Started.After(runs[j].Started) })
	return runs, nil
}

// errOverBound is a record that would take the reply past its bound.
var errOverBound = errors.New("over the reply bound")

// readRecord reads one record through root, refusing it if it holds more than
// limit bytes - read through a LimitReader, so a record that grew since it was
// listed is refused rather than read whole.
func readRecord(root *os.Root, name string, limit int) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: nothing a Close error could lose
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err == nil && len(raw) > limit {
		return nil, errOverBound
	}
	return raw, err
}

func firstN[T any](s []T, n int) []T { return s[:min(len(s), n)] }

// WorkflowAgentHistory is one workflow agent's own transcript - the
// isSidechain:true lines under <sessiondir>/subagents/workflows/wf_*/agent-
// <agentID>.jsonl, read through the session's own root and the same bounded
// scanner and historyEvents/historyBytes ring History reads an ordinary
// conversation's tail through. An unknown agent id is not an error: the run
// may have named an agent this session never reached - the caller draws
// nothing, which is what it would have drawn anyway.
func WorkflowAgentHistory(id, agentID string) ([]core.Event, error) {
	if err := rpc.ValidWorkflowAgentID(agentID); err != nil {
		return nil, err
	}
	root, ok := sessionRoot(id)
	if !ok {
		return nil, nil
	}
	defer closeRoot(root)
	name, ok := agentTranscript(root, agentID)
	if !ok {
		return nil, nil
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only: nothing a Close error could lose
	return sidechainTail(f, id, agentID)
}

// agentTranscript is agentID's own transcript under root: the first regular
// file matching, since a symlinked one is never followed. fs.Glob reads through
// the root, so a symlinked run directory leading out of it matches nothing.
func agentTranscript(root *os.Root, agentID string) (string, bool) {
	matches, err := fs.Glob(root.FS(), "subagents/workflows/*/agent-"+agentID+".jsonl")
	if err != nil {
		return "", false // only a malformed pattern, and the id fence rules that out
	}
	for _, m := range matches {
		if info, err := root.Lstat(m); err == nil && info.Mode().IsRegular() {
			return m, true
		}
	}
	return "", false
}

// sidechainTail decodes a workflow agent's transcript into the tail History's
// ring keeps.
func sidechainTail(r io.Reader, id, agentID string) ([]core.Event, error) {
	var ring []core.Event
	total := 0
	br := bufio.NewReaderSize(r, 64*1024)
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
	for i := range runs {
		runs[i].Script = "" // the daemon's alone: save reads it here, and no client draws one
	}
	c.enqueue(rpc.Frame{Kind: rpc.FrameWorkflowsReply, SessionID: id, Workflow: &rpc.WorkflowFrame{Runs: runs}})
}

// sendWorkflowAgent answers a client's FrameWorkflowAgent: one workflow
// agent's own transcript, echoing back the agent id it was asked for.
func (s *server) sendWorkflowAgent(c *client, id, agentID string) {
	// A malformed id is the request's fault, so it is refused; everything past
	// it is the disk's.
	if err := rpc.ValidWorkflowAgentID(agentID); err != nil {
		c.enqueue(errorFrame(id, err.Error()))
		return
	}
	events, err := WorkflowAgentHistory(s.transcriptID(id), agentID)
	if err != nil {
		// Unreadable answers as missing: the client draws the snapshot's previews,
		// where an error frame would be a notice on every re-ask.
		logf("wake: could not read workflow agent %s's transcript for session %s: %v", agentID, id, err)
		events = nil
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

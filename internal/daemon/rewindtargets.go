package daemon

// What a session could be rewound to: its active-branch user prompts, uuid
// and text, oldest first. FrameRewind's own RewindTarget and RewindLastSeen
// are a transcript message's own uuid, and core.Event carries neither - the
// UI has no other source for one, since only the daemon reads claude's disk.
// This reuses history.go's active-branch reconstruction rather than a second
// walk of the tree.

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// sendRewindTargets answers one client's FrameRewindTargets.
//
// Read on the connection's own goroutine rather than handed to an agent, for
// sendHistory's own reason: there may be no agent, and the file is claude's
// rather than Wake's.
func (s *server) sendRewindTargets(c *client, id string) {
	targets, err := RewindTargets(s.transcriptID(id))
	if err != nil {
		c.enqueue(errorFrame(id, "could not read that conversation's transcript: "+err.Error()))
		return
	}
	c.enqueue(rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: id, RewindTargets: targets})
}

// RewindTargets is one session's active-branch user prompts, oldest first.
// The newest entry is the newest active user turn, which is the last_seen
// tip a FrameRewind's RewindLastSeen wants.
//
// An id with no transcript is not an error, on History's own terms: a
// session that has never taken a turn has nothing to rewind to, and the
// caller draws an empty list, which is what it would have drawn anyway.
//
// Two passes over the file, exactly as History takes: the first reconstructs
// the active branch from tree structure alone, the second reads the user
// lines the first pass kept.
func RewindTargets(id string) ([]rpc.RewindTarget, error) {
	path, ok := transcriptPath(id)
	if !ok {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	active, err := activeBranchOf(f)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return livePrompts(f, active)
}

// rewindTargetsMax bounds how many prompts one reply carries: the tail, not
// the whole active branch. history.go's own reasoning - an unbounded reply
// can approach rpc's 16MB frame cap - applies here too, and the picker only
// ever shows recent prompts, so the newest rewindTargetsMax is what a caller
// can use regardless of how long the conversation has run.
const rewindTargetsMax = 100

// livePrompts collects the active branch's user prompts, in file order,
// tail-bounded at rewindTargetsMax. A line whose tree node is off the active
// branch is a rewound turn and is skipped whole - its content is never
// decoded - on liveHistory's own terms.
//
// A ring rather than everything-then-slice, liveHistory's own reason: memory
// stays proportional to the bound rather than to the whole active branch.
func livePrompts(r io.Reader, active map[string]bool) ([]rpc.RewindTarget, error) {
	ring := make([]rpc.RewindTarget, 0, rewindTargetsMax)
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := readTranscriptLine(br)
		if len(line) > 0 {
			if t, ok := promptTarget(line, active); ok {
				ring = append(ring, t)
				if len(ring) > rewindTargetsMax {
					// Trimmed from the front, which is what keeps this the
					// tail: the newest entry has to stay LastSeen's true tip.
					ring = ring[1:]
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ring, nil
			}
			return nil, err
		}
	}
}

// promptTarget is one line's rewind target, or false for a line that is not
// a genuine typed prompt on the active branch: no tree node, a node off the
// branch, or a line whose decoded events carry no genuine typed text.
//
// Which line is a "user" turn is never spelled here - DecodeTranscriptLine
// only ever produces a core.KindUserText event for a line Claude's own wire
// called "user", so filtering on that Wake-native kind is the airlock's own
// answer rather than a second copy of its vocabulary. An event carrying a
// Notice (Claude's own "[Request interrupted by user]" marker resolves to
// one) or one that is Echoed is on-disk chatter rather than something typed.
func promptTarget(line []byte, active map[string]bool) (rpc.RewindTarget, bool) {
	node, ok := core.DecodeTranscriptNode(line)
	if !ok || !active[node.UUID] {
		return rpc.RewindTarget{}, false
	}
	events, err := core.DecodeTranscriptLine(line)
	if err != nil {
		logf("wake: an active-branch prompt could not be decoded: %v", err)
		return rpc.RewindTarget{}, false
	}
	var text strings.Builder
	for _, ev := range events {
		if ev.Kind != core.KindUserText || ev.Notice != "" || ev.Echoed {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(ev.Text)
	}
	if text.Len() == 0 {
		return rpc.RewindTarget{}, false
	}
	return rpc.RewindTarget{UUID: node.UUID, Text: text.String()}, true
}

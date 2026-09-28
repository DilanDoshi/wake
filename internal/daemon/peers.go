package daemon

// The machine's other Claude sessions, asked of a bare one-shot claude.
//
// A /list-agents sent to a live agent stays in its transcript and reaches its
// model on the next turn (2026-09-27-at-menu-findings.md §1a), so no agent is
// asked. The daemon runs core.ListAgentsCommand instead - one bare /list-agents
// on stdin, then EOF; ~0.7s, $0, no hooks, MCP servers or transcript - one run
// at a time, answering every client that asked while it ran. Asked once per
// menu opening, so nothing runs on a timer.

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// defaultPeersDeadline bounds one run: the recorded one answers in ~0.7s, so
// one still going after this is hung, not slow.
const defaultPeersDeadline = 10 * time.Second

// peersDeadline is a var only so tests can compress it.
var peersDeadline = defaultPeersDeadline

// peersOutputBytes bounds what a run may print: the recorded one prints about
// 3.9 KB, so this is a loose ceiling, not a size.
const peersOutputBytes = 1 << 20

// listAgentsVerb composes the bare /list-agents the one-shot is sent, and names
// the command an agent's init must advertise.
const listAgentsVerb = "list-agents"

// errModelTurn is a one-shot whose result ran a model turn: not the recorded
// local command ($0, num_turns 0), so its text is no listing.
var errModelTurn = errors.New("the one-shot ran a model turn")

// peerBook is the run in flight and who waits on it. Its lock is taken before
// s.mu or any a.mu and never while holding one.
type peerBook struct {
	mu      sync.Mutex
	running bool
	waiting []*client
	off     bool // a one-shot ran a model turn, so this daemon runs no more
}

// askPeers answers one client's FramePeers: it joins the run in flight or
// starts one off the dispatch goroutine - unless no agent's claude advertised
// the command, when it answers empty at once.
func (s *server) askPeers(ctx context.Context, c *client) {
	b := &s.peers
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Contains(b.waiting, c) {
		b.waiting = append(b.waiting, c)
	}
	switch {
	case b.running:
	case b.off || !s.advertised(listAgentsVerb):
		b.answerLocked(nil)
	default:
		b.running = true
		s.start(func() { s.answerPeers(s.listPeers(ctx)) })
	}
}

// answerPeers ends the run: every client that asked is answered once. A run
// that ran a model turn turns the one-shot off, bounding any spend to one turn.
func (s *server) answerPeers(peers []core.Peer, err error) {
	b := &s.peers
	b.mu.Lock()
	defer b.mu.Unlock()
	b.running = false
	b.off = b.off || errors.Is(err, errModelTurn)
	b.answerLocked(peers)
}

// answerLocked sends peers to every waiting client. The caller holds b.mu;
// enqueue never blocks.
func (b *peerBook) answerLocked(peers []core.Peer) {
	f := rpc.Frame{Kind: rpc.FramePeersReply, Peers: &rpc.PeersFrame{Peers: peers}}
	for _, c := range b.waiting {
		c.enqueue(f)
	}
	b.waiting = nil
}

// advertised reports whether some agent's last init named cmd, so a claude
// without it is never run for one.
func (s *server) advertised(cmd string) bool {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()
	for _, a := range agents {
		a.mu.Lock()
		named := slices.Contains(a.commands, cmd)
		a.mu.Unlock()
		if named {
			return true
		}
	}
	return false
}

// listPeers runs the one-shot beside the socket, a directory Wake owns, and
// reads its listing. Every failure - no claude, a failed exec, a non-zero exit,
// the deadline, a model turn, a text this build cannot read - is nil peers and
// the reason: no outside sessions, never a wrong row.
func (s *server) listPeers(ctx context.Context) ([]core.Peer, error) {
	ctx, cancel := context.WithTimeout(ctx, peersDeadline)
	defer cancel()
	// The daemon ending ends the run, rather than shutdown waiting it out.
	s.start(func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	})
	peers, err := runListAgents(ctx, filepath.Dir(s.socket))
	if err != nil {
		logf("wake: could not list the machine's Claude sessions: %v", err)
	}
	return peers, err
}

// runListAgents runs one bare /list-agents in dir and parses the text of the
// result it prints.
func runListAgents(ctx context.Context, dir string) ([]core.Peer, error) {
	ask, err := core.EncodeUserMessage(slashPrefix+listAgentsVerb, nil, "")
	if err != nil {
		return nil, err
	}
	var out capped
	cmd := core.ListAgentsCommand(ctx, dir)
	cmd.Stdin = bytes.NewReader(ask)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.over {
		return nil, errors.New("it printed more than a listing")
	}
	for line := range bytes.Lines(out.buf.Bytes()) {
		events, err := core.DecodeLine(line)
		if err != nil {
			continue // one unreadable line is not an unreadable run
		}
		for _, ev := range events {
			if ev.Kind != core.KindTurnEnd {
				continue
			}
			if !ev.LocalCommand {
				return nil, errModelTurn
			}
			if peers, ok := core.PeersFromListAgents(ev.Text); ok {
				return peers, nil
			}
			return nil, errors.New("its listing is a shape this build cannot read")
		}
	}
	return nil, errors.New("it ended without a result")
}

// capped keeps a run's stdout up to peersOutputBytes and notes anything past
// it, which fails the run. It never refuses a write: a refused copy would
// leave the child blocked on a full pipe until the deadline. The buffer is a
// field, not embedded, so io.Copy cannot reach its ReadFrom around Write.
type capped struct {
	buf  bytes.Buffer
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if c.over || c.buf.Len()+len(p) > peersOutputBytes {
		c.over = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

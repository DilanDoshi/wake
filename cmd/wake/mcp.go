// `wake mcp`: the MCP server the manager's --mcp-config names.
//
// It is an ordinary RPC client. Everything the tools need already crosses the
// socket - FrameStatus answers list_agents and roll_up, FrameSend is
// send_to_agent, FrameInterrupt is interrupt - so this adds no daemon verb and
// no new frame kind. It is `wake daemon`'s shape: this binary, re-executed with
// a subcommand, wired to a different pair of pipes.
//
// Not a user command, and not in the usage text. It is spawned by a claude
// process and speaks JSON-RPC on stdin and stdout, so a person who runs it by
// hand gets a program that appears to hang.
//
// # What this file may write to the socket, and what it may not
//
// The manager is a model calling tools with nobody watching, so which verbs
// reach it is a safety decision rather than a feature list. It is held in
// managerVerbs in mcpguard_test.go, per frame kind, derived from the daemon's
// own dispatch - so a verb added to the daemon has to be ruled on rather than
// silently inherited, and a verb ruled out that appears in this file is a build
// failure.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/mcp"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// serveMCP answers a manager's tool calls over the pipes it was given.
func serveMCP(socket string, in io.Reader, out io.Writer) error {
	return mcp.Serve(context.Background(), in, out, socketFleet{socket: socket})
}

// socketFleet is mcp.Fleet over a real daemon.
//
// Dialled per call rather than held, and that is the cheaper shape here: the
// manager asks a question every few minutes at most, and a connection held open
// for the life of a manager session is one more client the daemon fans every
// event out to - at 30 agents that is a stream nobody reads.
type socketFleet struct{ socket string }

// List is the fleet report.
//
// daemon.Status rather than a hand-rolled exchange, because it already answers
// the four shapes correctly - including the one that is easy to get wrong, where
// a reply and a push are two kinds and only a reply answers a question. It also
// answers from the disk when nothing is listening, which is right here: a
// machine with no daemon has no live agents, and every state liveSessions offers
// is one only a running daemon writes, so the tools report an empty fleet rather
// than an error about a socket a model can do nothing about.
func (f socketFleet) List(context.Context) (rpc.Status, error) { return daemon.Status(f.socket) }

func (f socketFleet) Send(_ context.Context, id, text string) error {
	return f.act(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: text})
}

func (f socketFleet) Interrupt(_ context.Context, id string) error {
	return f.act(rpc.Frame{Kind: rpc.FrameInterrupt, SessionID: id})
}

// Spawn mints the id here rather than reading one back, which is the rule every
// other spawn path in this build follows: Wake originates identity, and the
// daemon refuses a frame that arrives without it. It also means the tool can
// answer with an id the manager can address before the daemon has finished
// starting anything.
func (f socketFleet) Spawn(_ context.Context, dir string) (string, error) {
	id := uuid.NewString()
	if err := f.act(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: id, Dir: dir}); err != nil {
		return "", err
	}
	return id, nil
}

// actTimeout bounds an acting call's wait for the daemon to answer.
//
// A bound rather than the no-deadline waits `wake fork` and `wake attach` use,
// and the difference is who is waiting. Those wait on an *event* with a person
// at a terminal who can see that nothing has happened; this one is answering a
// model that is blocked on a tool result, and a dial into the listen backlog of
// a daemon in graceful shutdown succeeds and then says nothing at all. An
// unbounded wait there is a manager stopped dead with no reason on any surface.
//
// The same three seconds daemon.Status gives its own exchange, for its reason:
// two ends of a unix socket on one machine have the same answer to "how long can
// a peer that is behaving take".
//
// A var only so tests can compress it; nothing outside a test assigns it, and
// that is unsynchronised on the same terms as stopCeiling - no test in this
// package calls t.Parallel().
var actTimeout = defaultActTimeout

const defaultActTimeout = 3 * time.Second

// act writes one verb and does not return until the daemon has taken it.
//
// # Why it waits for anything
//
// Nothing on this socket acknowledges a send or an interrupt. Writing the frame
// and hanging up would put every refusal the daemon can produce - "unknown
// session", "that session has ended", "not reading its input" - on a connection
// nobody is reading, and the model would be told the work was delegated. A
// manager that believes it delegated something is worse than one that knows it
// failed: it reports the work as assigned and nobody looks at it again.
//
// # What makes a status request an acknowledgement
//
// serveClient dispatches one connection's frames **synchronously and in order**.
// So a FrameStatus written behind the verb cannot be answered until the verb has
// been dispatched, and a refusal - enqueued to this client during that same
// dispatch - is in the queue ahead of the reply. Reaching the reply therefore
// means the daemon took the verb and did not refuse it. No new frame kind, no
// daemon change: the ordering the daemon already has is the acknowledgement.
//
// # What it does not prove
//
// That the bytes reached the agent's stdin. agent.submit queues the frame and
// the write happens on the agent's own input goroutine, so a write that fails
// there is reported afterwards, to a connection this has already closed. That is
// the same residual every client in this tree has - the TUI reports it on the
// notice row if it is still attached - and it is a far smaller window than "the
// daemon never had it".
func (f socketFleet) act(verb rpc.Frame) error {
	conn, err := daemon.Dial(f.socket)
	if err != nil {
		// Never daemon.EnsureRunning, which forks a daemon when nothing is
		// listening. That is right for a person typing `wake` and wrong twice
		// over here: this process was spawned by a manager session rather than
		// by somebody who asked for a fleet, and the daemon it forked would
		// hold no sessions - so every later tool call would answer confidently
		// about an empty fleet that is not the one being managed.
		return fmt.Errorf("no daemon is listening on %s, so nothing could be sent; call list_agents to see what is running", f.socket)
	}
	defer func() { _ = conn.Close() }()

	// Both directions, not only the read. rpc.WriteFrameTo sets its own write
	// bound, so the write half is covered either way - but a deadline on the
	// connection rather than on one call removes the class rather than the
	// instance, and the two frames below go into a socket buffer this process
	// does not own.
	if err := conn.SetDeadline(time.Now().Add(actTimeout)); err != nil {
		return fmt.Errorf("bounding the exchange with the daemon: %w", err)
	}
	if err := rpc.WriteFrameTo(conn, verb); err != nil {
		return fmt.Errorf("asking the daemon to %s: %w", verb.Kind, err)
	}
	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameStatus}); err != nil {
		return fmt.Errorf("asking the daemon whether it took the %s: %w", verb.Kind, err)
	}
	return awaitTaken(conn, verb)
}

// awaitTaken reads until the daemon answers the question written behind the
// verb, or refuses the verb itself.
//
// The refusal has to be addressed to this session and no other, and the reason
// is narrower than it looks. **Every `rpc.FrameError` internal/daemon writes is
// unicast** - `c.enqueue(errorFrame(...))` in server.go, spawn.go and park.go,
// `p.from.enqueue(...)` in agent.go - and this connection is private to this
// call, so today every error frame arriving here is this call's. The check is
// for the two frames that are *not*: `client.go`'s dropped-frames notice
// carries no session id at all, and anything the daemon later broadcasts would
// arrive here too. Reading either as this send's refusal reports a failure that
// did not happen, after which the manager sends the same message again.
//
// # What this also does not prove
//
// That the reply was not *dropped*. `client.enqueue` is a non-blocking send onto
// a 512-frame queue with nothing prioritised, and this connection is in
// `s.clients` from the top of `serveClient`, so it takes every broadcast event
// while it waits. A burst that filled the queue would lose the status reply and
// this would report a timeout on a send that succeeded. Unlikely - the reader is
// a tight loop with two frames to consume - and the failure is in the honest
// direction, which is why it is a paragraph rather than a redesign.
func awaitTaken(conn net.Conn, verb rpc.Frame) error {
	frames, errs := rpc.ReadFrames(conn)
	// Closed and drained on every path out, including the early returns:
	// abandoning frames while the reader still has data parks that goroutine on
	// a send forever, and closing the connection does not release one already
	// parked there.
	defer func() {
		_ = conn.Close()
		for range frames {
		}
		<-errs
	}()

	for f := range frames {
		switch {
		case f.Kind == rpc.FrameError && f.SessionID == verb.SessionID:
			return errors.New(f.Text)
		case f.Kind == rpc.FrameStatusReply:
			return nil
		}
	}
	if err := <-errs; err != nil {
		return fmt.Errorf("waiting for the daemon to take the %s: %w", verb.Kind, err)
	}
	return fmt.Errorf("the daemon closed the connection without saying whether it took the %s", verb.Kind)
}

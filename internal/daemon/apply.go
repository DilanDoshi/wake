// One agent's stdin path: the queue a client's frames wait on, the goroutine
// that drains it, and what applying each frame means. Split from agent.go at
// the subject seam - what the daemon *knows* about a session stays there, and
// what it *writes* to one is here - with agent.go against the 800-line hard
// max.

package daemon

import (
	"errors"
	"fmt"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// agentQueue is how many stdin-bound frames may be waiting for one agent.
// Deep enough for a human typing ahead and answering permission prompts,
// shallow enough that an agent which stopped reading its stdin says so
// instead of hoarding.
const agentQueue = 64

// pending is one stdin-bound frame and the client that asked for it, so a
// failure is reported to whoever can do something about it.
//
// probe marks a daemon-originated effort probe: a bare /model with no client
// behind it. It is sent like any other line but reports to nobody, counts as no
// turn, and opens the suppression window that keeps its reply off every client.
type pending struct {
	from  *client
	frame rpc.Frame
	probe bool
}

// submit queues one stdin-bound frame for this agent.
//
// A full queue is an error rather than a wait: the queue filling means this
// agent has stopped reading its stdin, and blocking here would park the
// client's read goroutine on exactly the agent it can no longer control.
func (a *agent) submit(from *client, f rpc.Frame) error {
	select {
	case <-a.gone:
		return fmt.Errorf("session %s has ended", a.id)
	default:
	}
	select {
	case a.in <- pending{from: from, frame: f}:
		return nil
	default:
		return fmt.Errorf("session %s is not reading its input: %d messages already queued", a.id, agentQueue)
	}
}

// serveInput performs this agent's stdin writes, one at a time and in the
// order they were queued.
//
// One at a time because two frames interleaved on stdin is a line claude
// cannot parse, and an unparseable line kills the process. In order because
// two messages to one agent that swap places are two turns in the wrong
// order.
func (a *agent) serveInput() {
	for {
		select {
		case <-a.gone:
			return
		case p := <-a.in:
			a.apply(p)
		}
	}
}

// apply writes one frame to the agent and reports a failure to the client
// that asked for it.
func (a *agent) apply(p pending) {
	var err error
	switch p.frame.Kind {
	case rpc.FrameSend:
		if p.probe {
			// A probe is not an operator turn: no noteSent (so the agent is not
			// marked owed and never looks busy), no noteEffort, and no client to
			// report a failure to. setProbing before the write opens the window
			// fanOut uses to swallow the reply; a failed write closes it again.
			a.setProbing(true)
			if err := a.sess.Send(p.frame.Text, nil); err != nil {
				a.setProbing(false)
				logf("wake: session %s: effort probe not sent: %v", a.id, err)
			}
			return
		}
		if err = a.sess.Send(p.frame.Text, p.frame.Images); err == nil {
			a.noteSent()
			if a.noteEffort(p.frame.Text) {
				// The level just changed under our record; confirm it.
				a.probeEffort()
			}
		}
	case rpc.FrameAllow:
		err = a.allow(p)
	case rpc.FrameAnswer:
		if err = a.sess.AnswerQuestion(p.frame.RequestID, p.frame.UpdatedInput, p.frame.Answers); err == nil {
			a.noteAnswered(p.frame.RequestID)
		}
	case rpc.FrameDeny:
		if err = a.sess.DenyTool(p.frame.RequestID, p.frame.Reason); err == nil {
			a.noteAnswered(p.frame.RequestID)
		}
	case rpc.FrameInterrupt:
		// The minted request_id is discarded, deliberately. Nothing here has a
		// question outstanding to match the receipt against - the receipt goes
		// to every attached client on the event stream, carrying that id, and
		// a client that wants to correlate one has it. Holding it here would
		// be state with no reader, and the liveness policy needs none of it:
		// an aborted turn still ends with a turn end, which is what clears
		// what this agent owes.
		_, err = a.sess.Interrupt()
	case rpc.FrameMode:
		// Refused while an ask is outstanding. internal/ui makes the same
		// check so it can give the operator a sentence, but it reads a fleet
		// report - the ask can be raised here after that report went out and
		// before the keystroke arrives. This agent is holding the ask, so it
		// needs to ask nobody; what stays open is only the process's own
		// asynchrony, an ask already in flight on stdout when this frame
		// reaches stdin, which no check on this side of the pipe can close.
		//
		// The verdict itself is ui.modeWouldRaceAnAsk's: what happens to an
		// outstanding permission request when the mode changes under it is
		// unrecorded (permission-mode-findings.md §9 item 3), and the operator
		// is mid-decision on a card.
		if a.blockedOnAsk() {
			a.refuse(p, errors.New("this session is stopped on a permission request; answer or withdraw it before changing its permissions"))
			return
		}
		// The minted request_id is discarded for FrameInterrupt's reason: the
		// receipt reaches every attached client on the event stream carrying
		// that id, and a client that wants to correlate one has it. Holding it
		// here would be state with no reader.
		//
		// Nothing is recorded about the mode either, and that is the point of
		// the design rather than an omission. The mode is a property of the
		// running process, not of the session: it does not survive a park
		// (permission-mode-findings.md §8), and every turn's init reports the
		// real one. A remembered mode here would be a second answer that could
		// go stale, on the one question this feature exists to stop lying about.
		_, err = a.sess.SetMode(p.frame.Mode)
	case rpc.FrameRewind:
		// Refused while an ask is outstanding, on FrameMode's own guard and
		// for the same reason: this agent is holding the ask, so it needs to
		// ask nobody, and the operator is mid-decision on a card a rewind
		// would pull the rug out from under.
		if a.blockedOnAsk() {
			a.refuse(p, errors.New("this session is stopped on a permission request; answer or withdraw it before rewinding"))
			return
		}
		// The minted request_id is discarded for FrameMode's reason: the
		// receipt reaches every attached client on the event stream carrying
		// that id, and a client that wants to correlate one has it.
		_, err = a.sess.Rewind(p.frame.RewindTarget, p.frame.RewindLastSeen)
	case rpc.FrameStop:
		// Here rather than on the connection's goroutine so it lands behind
		// the messages already queued for this agent - see dispatch.
		err = a.stop()
	case rpc.FramePark:
		// The label before the stop, never after: retire runs the moment the
		// events channel closes, which can be before this function returns.
		a.beginPark()
		err = a.stop()
	default:
		a.refuse(p, fmt.Errorf("frame kind %q is not something an agent can be sent", p.frame.Kind))
		return
	}
	if err != nil {
		if !errors.Is(err, core.ErrNotWritten) {
			// A failed write to a session nobody stopped is proof enough to
			// *report* it and deliberately not enough to reclaim it: EPIPE says
			// stdin has no reader, which is not the same as the process being
			// gone, and reclaiming kills a process group. The watchdog asks the
			// OS and is the only caller that may (docs/notes/bugs.md BUG-17).
			// Nothing is lost by waiting: an agent that really is gone stays
			// eligible for probeQuietAgents even after this report.
			//
			// noteUnreachable's own guard is the single answer to "was this
			// session stopped", which is why stop and park carry no exemption
			// here: agent.stop sets stopped before the call that can fail.
			//
			// ErrNotWritten is the case that is not that. core refuses some
			// frames before a byte reaches stdin - an answer with a choice
			// missing is the routine one, since it is assembled from what an
			// operator did - and reading a refusal as a failed write would
			// report a healthy blocked agent as silent, on the one path where
			// the operator is already looking at it.
			a.noteUnreachable(err)
		}
		logf("wake: session %s: %v", a.id, err)
		p.from.enqueue(errorFrame(a.id, err.Error()))
	}
}

// refuse reports a frame this agent will not apply. Nothing was written to the
// process, so it is never evidence the process is gone: marking the agent
// unreachable over the daemon's own sentence turns a healthy session silent -
// with an unnecessary OS probe schedule - while an operator is looking at it.
func (a *agent) refuse(p pending, err error) {
	logf("wake: session %s: %v", a.id, err)
	p.from.enqueue(errorFrame(a.id, err.Error()))
}

// answerLost is what a client is told when an allow settled an ask that had
// questions on it.
//
// It says what happened rather than what went wrong, because nothing did go
// wrong from any other angle: the frame was well formed, the tool ran, and the
// turn ends subtype "success" with permission_denials empty. The model was
// simply told nobody answered. Before this the whole event was invisible -
// there is no field anywhere on Claude's wire that reports it, and the operator
// would watch their agent carry on as though they had not replied.
//
// It is Wake's own English on purpose: the CLI's sentence for this is Claude's
// wire vocabulary and does not belong outside the airlock.
const answerLost = "a question was approved without an answer, so the agent was told nobody answered it"

// allow answers a permission ask with yes, and reports the one case where yes
// is not the whole answer.
//
// The allow still goes out. Refusing it would leave the agent stopped dead on
// an ask with nothing but deny and interrupt left to offer, which is a worse
// failure than the one being reported and lands on an operator who did nothing
// wrong. So the frame is delivered, exactly as before, and the loss is said out
// loud instead of being the silence it was.
func (a *agent) allow(p pending) error {
	lost := a.awaitsChoice(p.frame.RequestID)
	if err := a.sess.AllowTool(p.frame.RequestID, p.frame.UpdatedInput); err != nil {
		return err
	}
	a.noteAnswered(p.frame.RequestID)
	if lost {
		logf("wake: session %s: %s", a.id, answerLost)
		p.from.enqueue(errorFrame(a.id, answerLost))
	}
	return nil
}

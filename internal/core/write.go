// One session's write path: send a user turn, answer a permission ask,
// interrupt a turn, change a permission mode, rewind a conversation. Split
// from session.go at the subject seam that file's own header already
// draws - "this file owns the process" - once Rewind pushed it over the
// 800-line hard max: what a session *is* (spawn it, pump its stdout, end it)
// stays there, and what gets written to its stdin once it is running is here.
//
// Every one of these goes through writeLine, which is why writer and the
// write-side mutex live in this file too rather than back with the state they
// guard: they exist only to serve this file's writes, and Stop's own
// stdin.Close() is the one caller outside it, reached through the shared
// *Session rather than through anything exported here.

package core

import (
	"fmt"
	"io"

	"github.com/google/uuid"
)

// Send writes one user turn to the process.
//
// A send that lands also ends the licence a previous Interrupt bought. The
// recorded rule is that the exit code follows the *last turn's* is_error
// (findings §6), and a turn can fail for reasons that have nothing to do with
// an interrupt - so once Wake asks for another turn, the aborted one is no
// longer last and its excuse has to lapse. Five of the seven runs in §6 exited
// 0 because their last turn completed, two of them after being interrupted
// earlier in their lives; this is that observation, applied. Without it a
// session interrupted once would have every later silent failure forgiven for
// the rest of its life, which at 15-30 sessions is most of them.
func (s *Session) Send(text string, images []ImageBlock) error {
	line, err := EncodeUserMessage(text, images)
	if err != nil {
		return err
	}
	if err := s.writeLine(line); err != nil {
		return err
	}
	// Only after the write. A send that never arrived started no turn, so the
	// aborted one is still the last one - the mirror of Interrupt's own
	// ordering.
	s.noteTurnSent()
	return nil
}

// AllowTool answers a KindPermissionRequest with yes. The process is blocked
// until this lands. Pass a non-nil updatedInput to run the tool with
// different arguments than it asked for.
func (s *Session) AllowTool(requestID string, updatedInput map[string]any) error {
	line, err := EncodeAllow(requestID, updatedInput)
	if err != nil {
		return err
	}
	return s.writeLine(line)
}

// AnswerQuestion answers a KindPermissionRequest whose Ask is AskChoice with
// the operator's choices: asked is the ask's own input, answers maps each
// question's text to the label chosen for it.
//
// It is not an alternative spelling of AllowTool. Both write an allow, and
// that is the whole of what they share: an allow with no answers is recorded
// reaching the model as "the user did not answer", so the two calls settle the
// same ask with opposite outcomes. EncodeAnswer carries the argument and does
// the refusing; a refusal here wraps ErrNotWritten, so nothing reached stdin
// and the ask is still outstanding for a second attempt.
func (s *Session) AnswerQuestion(requestID string, asked map[string]any, answers map[string]string) error {
	line, err := EncodeAnswer(requestID, asked, answers)
	if err != nil {
		return err
	}
	return s.writeLine(line)
}

// DenyTool answers a KindPermissionRequest with no. The reason reaches the
// model verbatim as the tool result, so it is worth writing for the agent
// rather than for a log.
func (s *Session) DenyTool(requestID, reason string) error {
	line, err := EncodeDeny(requestID, reason)
	if err != nil {
		return err
	}
	return s.writeLine(line)
}

// interruptCancelQueued is whether Wake's interrupt also destroys the messages
// it has already handed the agent but which have not started yet.
//
// It is false, and that is a decision rather than a default. The capability is
// real and recorded both ways - without it a queued message still runs
// (interrupt-queued-survives.jsonl), with it the receipt lists what it
// destroyed (interrupt-cancel-queued.jsonl) - but the receipt names those
// messages by the uuid the *sender* stamped on them, and EncodeUserMessage
// stamps none. So every message Wake sends is one the CLI emits no lifecycle
// frame for and the receipt cannot name: Wake would be destroying work it
// could not then tell the operator it had destroyed, and the transcript would
// keep showing that message as sent, because App.submit echoes it locally the
// moment it goes out. Whether cancel_queued even reaches an unstamped message
// is unrecorded - every queued message in the corpus was stamped.
//
// The safe end of that failure is the one recorded to lose nothing. Stamping
// outgoing messages is what unblocks the other choice; KindMessageState
// already decodes the frames it would produce.
const interruptCancelQueued = false

// Interrupt aborts the turn this session is running, and returns the
// request_id its receipt will carry.
//
// It is not an ending. The session survives with an unchanged session id and
// takes the next message normally; what stops is the turn. An interrupt with
// no turn running is a harmless no-op that still gets a receipt, and firing
// one per keystroke needs no debouncing - every extra interrupt costs exactly
// one "success" receipt (findings note §9).
//
// The id is minted here because this is the layer that owns the session's
// stdin and therefore the only one that can promise the id it returns is the
// id that went out. It is mandatory rather than optional: the CLI accepts a
// control_request without one and aborts the turn anyway, but the receipt then
// carries no id either and cannot be matched to the interrupt that caused it -
// and the receipt names no session either, so across 15-30 sessions there
// would be nothing left to attribute it by. EncodeInterrupt refuses a blank
// one for that reason; this is where the id comes from.
//
// The returned id is a correlator, not a completion: the receipt arrives
// later, on the events channel, as a KindControlReceipt. A caller with nothing
// to correlate may discard it, which is what the daemon does today.
func (s *Session) Interrupt() (string, error) {
	requestID := uuid.NewString()
	line, err := EncodeInterrupt(requestID, interruptCancelQueued)
	if err != nil {
		return "", err
	}
	if err := s.writeLine(line); err != nil {
		return "", err
	}
	// After the write, never before. A frame that never reached the process
	// interrupted nothing, and remembering it would suppress the very exit
	// status that says the write failed.
	s.noteInterrupted()
	return requestID, nil
}

// SetMode changes the permission mode of this session while it runs, and
// returns the request_id its receipt will carry.
//
// It is the mechanism deferred I7 was blocked on. Config.PermissionMode reaches
// the command line once at spawn and there was no way to move it afterwards, so
// the composer showed a mode it could not set.
//
// The returned id is a correlator, not a completion, and here that distinction
// has teeth: the receipt carries the mode the session actually landed on, and
// that is the only authority on what happened. A caller that moves a label on
// the mode it *asked* for is wrong on a real cycle position, because `manual`
// normalizes to `default` (permission-mode-findings.md §6). Wait for the
// KindControlReceipt and read Event.PermissionMode.
//
// A refusal arrives the same way rather than as an error here - subtype "error"
// with a reason in Control.Error - because the CLI decides it after this
// function has already put a well-formed line on stdin.
//
// Unlike Interrupt this records nothing afterwards. An interrupt buys a licence
// to forgive an exit 1 with an empty stderr, because that is what an aborted
// process looks like; a mode change aborts no turn and is owed no such licence.
// Taking one would let a keystroke that moved a label silence the ending of a
// session that failed to start.
func (s *Session) SetMode(mode string) (string, error) {
	requestID := uuid.NewString()
	line, err := EncodeSetMode(requestID, mode)
	if err != nil {
		return "", err
	}
	if err := s.writeLine(line); err != nil {
		return "", err
	}
	return requestID, nil
}

// Rewind asks this session to rewind its conversation to targetUUID, declaring
// lastSeenUUID as the tip, and returns the request_id its receipt will carry.
// The receipt (KindRewindReceipt) is the authority on whether it rewound; a
// refusal arrives there, not as an error here. Records nothing afterward, for
// SetMode's own reason: a rewind aborts no turn and is owed no
// forgive-the-exit licence.
func (s *Session) Rewind(targetUUID, lastSeenUUID string) (string, error) {
	requestID := uuid.NewString()
	line, err := EncodeRewind(requestID, targetUUID, lastSeenUUID)
	if err != nil {
		return "", err
	}
	if err := s.writeLine(line); err != nil {
		return "", err
	}
	return requestID, nil
}

// noteInterrupted records that Wake aborted a turn here, and noteTurnSent
// records that it has since asked for another one. See Session.interrupted.
func (s *Session) noteInterrupted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupted = true
}

func (s *Session) noteTurnSent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interrupted = false
}

func (s *Session) writeLine(line []byte) error {
	w, err := s.writer()
	if err != nil {
		return err
	}

	// Held for the write alone. mu is free throughout, so Stop can close this
	// writer out from under a blocked write - which is the point: the write
	// then fails instead of the session becoming unstoppable.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	n, err := w.Write(line)
	if err != nil {
		return fmt.Errorf("write to session %s: %w", s.cfg.SessionID, err)
	}
	// A short write leaves half a frame on stdin, and claude exits 1 on a
	// line it cannot parse. Reporting it beats letting a truncated message
	// kill the session it was meant for.
	if n != len(line) {
		return fmt.Errorf("write to session %s: wrote %d of %d bytes", s.cfg.SessionID, n, len(line))
	}
	return nil
}

// writer returns the process's stdin, or the reason there is none. It exists
// to keep the state lock off the write itself.
func (s *Session) writer() (io.WriteCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.stopped:
		return nil, fmt.Errorf("session %s is stopped", s.cfg.SessionID)
	case s.stdin == nil:
		return nil, fmt.Errorf("session %s is not started", s.cfg.SessionID)
	}
	return s.stdin, nil
}

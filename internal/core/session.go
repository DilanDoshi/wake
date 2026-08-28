// Session supervises one agent launch: directly as `claude`, or through the
// Unix supervisor that owns its process group. It streams events, sends
// messages, answers permission requests, and stops the launch.
//
// This file knows nothing about Claude's JSON. Every byte in either direction
// crosses protocol.go - DecodeLine inbound, EncodeUserMessage / EncodeAllow /
// EncodeAnswer / EncodeDeny / EncodeInterrupt outbound - so a wire change stays
// one file's problem. Nothing here names a frame type, a key or a subtype.
//
// It no longer knows Claude's command line either. buildArgs, identityArgs and
// sameSession live in argv.go, which owns the argv and nothing else; this file
// owns the process. Start calls buildArgs and can still refuse before there is
// a process - see its own comment - but the shapes it refuses are argued for
// over there.
//
// It no longer owns writing to that process either. Send, AllowTool,
// AnswerQuestion, DenyTool, Interrupt, SetMode and Rewind - and writeLine and
// writer, which exist only to serve them - moved to write.go once Rewind
// pushed this file over the 800-line hard max: this file owns spawning the
// process and reading what it says, that one owns telling it something. Stop
// stayed here regardless, beside the rest of "Ending a session" below, since
// closing stdin is this file's own idea of how a session ends rather than a
// message written to a running one.
//
// The failure modes here are process lifecycle, not JSON. Observed directly
// against claude 2.1.226 while building this file:
//
//   - Spawned and never prompted, the process emits hook frames, **no init at
//     all**, and exits 0 when stdin closes. init is a turn header; waiting on
//     one as a readiness signal waits forever on an idle session.
//   - With stdin held open and nothing written it idles indefinitely, then
//     exits 0 on EOF. That is what Stop relies on.
//   - **One malformed line on stdin kills it**: exit 1, "Error parsing
//     streaming input line" on stderr, stdout silent. So a frame goes out
//     whole or not at all - see writeLine on short writes.
//   - Parseable-but-meaningless input is ignored: an unknown frame type, or a
//     control_response answering a request that does not exist, both leave it
//     running and exit 0. Answering a permission ask twice is survivable.
//   - SIGTERM ends it with status 143 and no orphans.
//   - Startup rejections (a missing --verbose, a session id already in use)
//     exit 1 with the reason on stderr and zero bytes on stdout. Nothing about
//     them reaches the JSON channel, which is why stderr is captured here and
//     reported through Err.
//
// # Ending a session
//
// Two calls, and they are not alternatives. Read this before building a
// wedged-agent path on either.
//
// Neither of them is Interrupt. **Stopping a turn and stopping a session are
// different operations on the wire** and the recordings are unambiguous about
// it: an interrupted process keeps its session_id, takes the next message
// normally, and can still be resumed later
// (docs/superpowers/notes/2026-08-08-interrupt-findings.md §6). Interrupt
// aborts what the agent is doing; Stop ends the agent.
//
// Stop closes stdin: the agent finishes the turn it is in and exits on EOF.
// Nothing is signalled and nothing it spawned is touched. It is the default
// ending because an agent killed mid-Edit leaves a half-written file.
//
// Cancelling the ctx passed to Start is the hard kill. It ends every way the
// *pump* can park - a scan waiting on stdout a grandchild holds
// (closeOnCancel), a pump handing an event to a consumer that stopped reading
// (send), a Wait stuck on stderr a grandchild holds (waitDelay) - and it
// SIGKILLs the agent's whole process group, so what the agent spawned dies
// with it. It does not end the fourth park in this package, which is not on
// the pump: see the last bullet below.
//
// What cancelling does not do, all of which is a caller's problem:
//
//   - It does not unblock a caller inside Send, AllowTool or DenyTool. Those
//     write to stdin with no ctx, and an agent that stopped draining stdin
//     parks the writer. The group kill frees it only because the pipe's read
//     end dies with the group; a descendant that left the group and holds
//     stdin keeps that writer parked for good. So call Stop as well as
//     cancel: Stop closes stdin, which fails the write at once, and Stop is
//     never blocked by the write it closes out from under.
//   - It does not reach a process that left the group, by setsid or otherwise.
//   - It does nothing once the agent has already exited: os/exec stops
//     watching ctx the moment Wait reaps the process, so anything a cleanly
//     ended session left behind is nobody's to reap. Wake does not hunt it.
//   - It does not wait. cancel returns at once; the session is over when
//     Events closes, and Err then says why. Err needs no consumer.
//   - Events still buffered are dropped rather than delivered.
//   - It does not reach logSink.run, which parks in log.Print if whatever is
//     behind log blocks. The session still ends - that park is deliberately
//     off the pump - but the goroutine stays, and log's mutex is process-wide.
//     See logSink.
package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// execCommand is a seam so tests can substitute a fake process. Testing
// against a live model is slow, nondeterministic and billed per run.
var execCommand = exec.CommandContext

// claudeBinary is resolved on PATH. Hazard worth knowing: on a machine
// running cmux, `claude` on PATH can be a wrapper shim that execs the real
// binary. stdout stays clean JSON, but the shim installs extra hooks, so hook
// frame counts are not comparable between a shimmed and an unshimmed spawn.
const claudeBinary = "claude"

// eventBuffer is how many events a session may run ahead of its reader.
//
// It is slack, not safety: the pump blocks once it is full, which stops
// draining the process's stdout pipe, which blocks the process itself. A
// consumer that stops reading Events() freezes the agent. Whoever ranges over
// Events() owns keeping up.
const eventBuffer = 256

// initialLineBytes is the scanner's starting buffer, sized so an ordinary
// frame never reallocates - a recorded init frame is ~11KB. maxLineBytes is
// the ceiling it may grow to.
const initialLineBytes = 64 * 1024

// Config describes one agent. Zero values are omitted from the command line.
type Config struct {
	SessionID string
	Name      string
	Dir       string
	Model     string
	Effort    string
	Launcher  AgentLauncher

	// PermissionMode is what we spawn with. Do not round-trip it from the
	// init frame: init normalizes "manual" to "default", so reading it back
	// reports a mode nobody asked for. Config is the source of truth.
	PermissionMode string

	// MaxBudgetUSD caps what this session may spend and FallbackModel is the
	// chain it fails over to, both empty for a session that chose neither. There
	// is no runtime command for either, so both are properties of the process
	// and a woken session gets what its park record carries. See spend.go, which
	// owns them and says what nothing confirms.
	MaxBudgetUSD  string
	FallbackModel string

	// Color is this session's identity hue, carried so a woken session comes back
	// the colour it was set to. Display only and **never an argv word** - like
	// Launcher it is a field the daemon reads and buildArgs does not, because the
	// colour is Wake's own rather than something claude is told. See
	// daemon/color.go and the argv-path guard in argvguard_test.go, which permits
	// a Config field buildArgs never reads.
	Color string

	// AddDir are directories outside Dir that this session's tools may reach,
	// each fenced by rpc.ValidAddDir before it gets here because they reach the
	// argv as written. Debug is a category filter for this session's logging
	// and DebugFile is the **absolute path** it is written to - a path and not
	// a name, because the daemon has already resolved the name a client chose.
	// See debug.go for why a filter never arrives without a file.
	AddDir    []string
	Debug     string
	DebugFile string

	// ForkFrom is the session whose conversation this one inherits, or empty
	// for an ordinary spawn. Setting it turns this session's identity flags
	// into the fork triple - see identityArgs, which is the only place the
	// three are allowed to be spelled.
	//
	// It is not "resume". A plain --resume reuses the parent's id and appends
	// to the parent's transcript; a fork mints its own id, lands in its own
	// <uuid>.jsonl and does not touch the parent's file
	// (2026-08-09 findings §7, 2026-08-10 findings §5). ResumeFrom is the
	// other one.
	ForkFrom string

	// ResumeFrom is the session this one continues, or empty for a session
	// that is starting fresh. It is that session's **own** id: --resume reuses
	// the id it is given (resume-park.jsonl and resume-wake.jsonl are two
	// processes over one session id), so a resumed session keeps its identity,
	// its roster row and its name.
	//
	// It is not "fork". A fork mints its own id, lands in its own
	// <uuid>.jsonl and does not touch the parent's file; a resume appends to
	// the parent's own transcript (2026-08-09 findings §7). Setting both is
	// refused - see identityArgs.
	//
	// SessionID is set alongside it, to the same id, and identityArgs
	// suppresses the flag rather than the field: Session.attribute stamps
	// cfg.SessionID onto the one frame Claude's wire never names a session on
	// - the permission ask - so a resumed session with an empty SessionID
	// would deliver every can_use_tool unroutable, which at 15-30 sessions is
	// a blocked agent no view can name.
	ResumeFrom string

	// MCPConfig is a path to an MCP server configuration, passed as
	// --mcp-config with --strict-mcp-config beside it. Empty for every agent
	// but the manager.
	//
	// **Deliberately not a default, in both directions.** The tools it names
	// can message and interrupt any agent in the fleet, so giving them to
	// every agent would let thirty of them act on each other - a fleet that
	// can deadlock itself with nobody having asked for it. One session gets
	// them, and the spawn site is where that is visible.
	//
	// There is no second field for the strict flag, and that absence is the
	// safety property rather than a convenience. --mcp-config *adds* servers
	// to whatever the machine already has configured, so a config emitted
	// without --strict-mcp-config gives the manager every MCP server in the
	// user's own configuration - a browser, a ticket tracker, a chat client -
	// while every artefact about this session still says it can only send and
	// interrupt. That failure is accepted, exit 0, empty stderr, and a session
	// that looks right, which is the same shape identityArgs exists for. The
	// pair is therefore one literal from one append in argv.go, and no value of
	// this field can express one without the other.
	MCPConfig string

	// AppendSystemPrompt is text added to claude's own system prompt, passed
	// as --append-system-prompt. Empty for every agent but the manager.
	//
	// A system prompt rather than a first message, and the difference is what
	// it is for. Everything the manager reads through its tools is text an
	// agent's model wrote, so the sentence that says that text is data rather
	// than instruction is the one thing in its context that must not be
	// movable: a first message is a turn, later turns can argue a model out of
	// a turn, and `/clear` drops it. This is not a turn.
	//
	// Append rather than replace. claude's default system prompt is what makes
	// the process a working agent at all, and there is no recording of what a
	// session started with --system-prompt does with Wake's stream-json
	// vocabulary - so the flag with a recording behind it is the one used.
	AppendSystemPrompt string
}

// defaultPermissionMode is what a Config that names none spawns with.
const defaultPermissionMode = PermissionModeAuto

// Session supervises one claude process in stream-json mode.
type Session struct {
	cfg Config

	// mu guards the fields below it and nothing else. It is never held across
	// an operation that can block: a wedged agent must not be able to take
	// Stop, Err and the pump's teardown down with it.
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan Event
	closed bool
	// starting covers the interval before cmd exists. Once cmd is published,
	// cmd itself keeps a second Start out while launcher readiness is pending.
	starting bool

	// stopped separates "stdin closed on purpose" from "never started", so a
	// send after Stop reports the truth rather than blaming the caller.
	stopped bool

	// interrupted records that Wake aborted a turn here **and has not asked
	// for another since**. It is the fact that tells a deliberate abort from a
	// crash when the process later exits - see Interrupt and interruptedExit.
	//
	// Cleared by a successful Send, and that is the narrowing rather than
	// bookkeeping: the exit code follows the last turn's is_error, so once a
	// new turn goes in the aborted one is no longer last and the excuse
	// expires with it.
	interrupted bool

	// err is why the process ended. Written once, as the events channel
	// closes; see Err.
	err error

	// waitErr is cmd.Wait's result, set by awaitExit and read by finish across
	// the goroutine boundary between them. stdoutClosedByExit records that
	// awaitExit force-closed the read end after the process exited - the wedge -
	// the one bit that tells that ErrClosed from a cancel's. Both under mu.
	waitErr            error
	stdoutClosedByExit bool

	// pgid is the process group the agent leads, recorded at Start and kept
	// afterwards. See Pgid.
	pgid int

	// logs carries diagnostics off the pump goroutine. Immutable after
	// NewSession, so no lock guards it.
	logs *logSink

	// writeMu serializes stdin writes so two frames cannot interleave - half
	// of one inside the other is a line claude cannot parse, and an
	// unparseable line kills the process.
	//
	// Separate from mu because a write *can* block indefinitely: a child that
	// stops draining stdin fills the 64KB pipe buffer and the write never
	// returns. Nothing ever holds both, and mu is never acquired while
	// writeMu is held, so there is no ordering to get wrong.
	writeMu sync.Mutex
}

func NewSession(cfg Config) *Session {
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = defaultPermissionMode
	}
	return &Session{cfg: cfg, events: make(chan Event, eventBuffer), logs: newLogSink()}
}

// Events returns the stream of decoded events. It is closed when the process
// exits - not when a turn ends - so a range loop terminates only once, at the
// end of the process's life.
//
// When that loop ends, call Err: a session that died before writing a single
// frame closes this channel empty, and the reason exists nowhere else.
func (s *Session) Events() <-chan Event { return s.events }

// Err reports why the process ended: a non-zero exit paired with whatever it
// said on stderr, or nil for a clean one. It is meaningful only once Events()
// has closed, and is the only account of a session that died before it ever
// wrote a frame.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Pgid is the process group this session's agent leads, or 0 where the
// platform has none. It is the only handle to the agent's tree that survives
// the process that spawned it.
//
// It exists for one caller: whoever has to reap a fleet whose daemon died.
// Setpgid separated the agents from Wake's own death - which is what makes
// "detach" mean anything - and the cost is that a SIGKILLed daemon leaves
// 15-30 trees running with no *exec.Cmd anywhere in the world. Recorded at
// Start and deliberately never cleared: cmd.Process.Pid is gone the moment
// finish reaps, and the whole point is to be readable off disk long after
// this Session is not.
//
// It names a group, not a process. Hand it to core.KillGroup, never to
// kill(2) directly, and never treat a non-zero value as "still running" - the
// group may be empty, which is precisely what the reaper is checking.
func (s *Session) Pgid() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pgid
}

// pump reads stream-json lines until EOF, then closes the events channel.
//
// EOF is the only thing that ends the stream. A result frame is a turn
// boundary, not an exit: one recorded process emitted seven of them, and
// anything treating the first as death tears down a live session.
func (s *Session) pump(ctx context.Context, process *agentProcess, scanDone chan<- struct{}) {
	var scanErr error
	// Registered first so it runs last. A send on a closed channel panics even
	// inside a select with a default, so anything that logs during teardown -
	// finish being the obvious place to say why a session ended - must run
	// while the sink is still open.
	defer s.logs.close()
	defer func() { s.finish(process, scanErr) }()
	defer close(scanDone) // LIFO: retires closeOnCancel before finish waits

	sc := bufio.NewScanner(process.stdout)
	sc.Buffer(make([]byte, 0, initialLineBytes), maxLineBytes)
	for sc.Scan() {
		s.emit(ctx, sc.Bytes())
	}
	if scanErr = sc.Err(); scanErr != nil && !s.exitClosed() {
		// The scan ended on something other than EOF, and not because awaitExit
		// force-closed the reader: a line past maxLineBytes, or closeOnCancel
		// taking the reader away. Nobody is draining stdout any more, and a
		// still-alive process would block writing into it, so reap the group -
		// whatever the agent spawned inherited this stdout, and one of them
		// still holding it is the likeliest reason the scan is here.
		//
		// The wedge is the case the gate carves out: there awaitExit already
		// reaped the leader, so the surviving group is retire's park-aware job,
		// not a blind kill from here.
		_ = killProcessGroup(process.cmd)
	}
}

// emit decodes one line into events, attributes each to this session, and
// hands them on. A malformed line becomes an error event and is skipped - it
// must never take down the stream. DecodeLine errors only on malformed JSON;
// a frame Wake does not model decodes to KindUnknown.
func (s *Session) emit(ctx context.Context, line []byte) {
	if len(line) == 0 {
		return
	}
	evs, err := DecodeLine(line)
	if err != nil {
		// Logged as well as emitted. The event is KindUnknown, which the DM
		// renders as the empty string, so the event alone would leave a
		// decoder bug invisible to everyone including whoever is debugging
		// it. Truncated because one frame can be megabytes.
		//
		// Through the sink, never log.Printf: a direct write would park the
		// pump on whatever is behind log. See logSink.
		s.logs.printf("wake: session %s could not decode a line: %v: %.200s", s.cfg.SessionID, err, line)
		s.send(ctx, Event{Kind: KindUnknown, SessionID: s.cfg.SessionID, Text: err.Error()})
		return
	}
	for _, ev := range evs {
		s.send(ctx, s.attribute(ev))
	}
}

// attribute names the session an event came from, for the events that cannot
// name it themselves.
//
// A control_request - the permission ask - carries no session_id on the wire.
// It is the only frame type in the recorded corpus that does not, and it is
// also the highest-priority "needs you" trigger there is: an agent blocked on
// stdin until a human answers. Delivered unattributed it is unroutable, and
// at 15-30 sessions an unroutable one is a blocked agent no view can name.
//
// The pipe it arrived on is the only evidence of which agent asked, and this
// is the only component that knows the pipe. protocol.go cannot do it - it
// decodes bytes and has no idea whose bytes they are - so declining to do it
// here means nobody does.
//
// The ordering is the load-bearing half. A frame that names its own session
// keeps that name: /clear mints a new session id mid-process, and from that
// point the frames are the authority and cfg.SessionID is the id the process
// was *spawned* under. Stamping unconditionally would re-label every
// post-clear event with a stale id.
//
// The residual, which belongs to whoever owns re-keying: an ask that arrives
// *after* a /clear is still stamped with the spawn id, because that is the
// only id this file is told about. It routes to the session Wake spawned,
// which is the right process; it is the wrong id the moment something above
// re-keys the session to its successor.
//
// Returns a new Event rather than editing one in place - the range variable
// is already a copy, and this keeps that deliberate rather than incidental.
func (s *Session) attribute(ev Event) Event {
	if ev.SessionID == "" {
		ev.SessionID = s.cfg.SessionID
	}
	return ev
}

// send hands one event to the consumer, and gives up on it once ctx is done.
//
// Giving up is the whole point. Events() is buffered but finite, so a consumer
// that stops reading fills it and parks the pump on a *channel send* - not in
// Scan, which is the one park closeOnCancel cannot reach: it takes stdout
// away, and a pump parked here is not reading stdout. Without the ctx case the
// scan never resumes, scanDone never closes, finish never runs and Events()
// never closes - the third of four routes this file has found to a session
// that looks healthy, holds a live-cap slot and cannot be killed. The fourth
// is logging from the pump; see logSink.
//
// The non-blocking attempt comes first so cancelling never costs an event
// there was room for: the last frames before a kill are the ones worth having.
func (s *Session) send(ctx context.Context, ev Event) {
	select {
	case s.events <- ev:
		return
	default:
	}
	select {
	case s.events <- ev:
	case <-ctx.Done():
		// Dropped, deliberately and without a log: the session is being
		// killed with nobody reading it, so a per-event line would be noise
		// about a stream that has no audience. Why the session ended is
		// reported once, through Err.
	}
}

// finish closes the events channel - Wake's signal that the session is over -
// once awaitExit has confirmed the process gone. It waits on procGone rather
// than reaping itself: there is exactly one cmd.Wait in a session and it is
// awaitExit's, so a wedged scan can no longer keep the reaping from happening.
// awaitExit reaps the supervisor on the launcher path exactly as it reaps
// claude on the direct one, so the wedge - a grandchild holding the read end
// open past the leader's exit - self-detects on both.
//
// Two launcher causes outrank the ending awaitExit would otherwise classify,
// because on their paths that ending is only the teardown artifact - an
// os.ErrClosed from the force-close, a signal-kill exit from the group
// terminate - not the reason. A failed *start* (ownership rejection, a missing
// target, a bad Dir) latches startErr. A failed *lifetime* (the supervisor gone
// before DONE) latches lifetimeError, reported when the scan carried no cause
// of its own.
func (s *Session) finish(process *agentProcess, scanErr error) {
	<-process.procGone
	// Only after the lifetime watcher has settled is a launcher cause safe to
	// read: on the direct path this closed at once, on the launcher path it is
	// the watcher's last act. Always closed, so it can never wedge finish.
	<-process.lifetimeSettled
	startErr := process.startError()
	lifetimeErr := process.lifetimeError()
	tail := process.errTail.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case startErr != nil:
		s.err = startErr
	case lifetimeErr != nil && (scanErr == nil || errors.Is(scanErr, os.ErrClosed)):
		s.err = fmt.Errorf("session %s %w", s.cfg.SessionID, lifetimeErr)
	default:
		s.err = endErr(s.cfg.SessionID, scanErr, s.waitErr, tail, s.interrupted, s.stdoutClosedByExit, s.logs)
	}
	if !s.closed {
		s.closed = true
		close(s.events)
	}
}

// Stop closes stdin, which is how a claude process ends cleanly: idle, it
// exits within moments of the EOF; mid-turn, it finishes the turn first.
//
// Stop does not wait, does not signal, and does not touch the agent's process
// group - a hard kill here would take an agent's own children down mid-tool,
// which the spec reserves for the cancel path and a confirmation. Events()
// closes when the process is actually gone, so that is the signal to wait on.
// Stopping twice is not an error.
func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.stdin == nil {
		s.stopped = true
		return nil
	}

	s.stopped = true
	err := s.stdin.Close()
	s.stdin = nil
	if err != nil {
		return fmt.Errorf("close stdin for session %s: %w", s.cfg.SessionID, err)
	}
	return nil
}

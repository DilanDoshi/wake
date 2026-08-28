// Package rpc is the daemon ↔ client transport: newline-delimited JSON
// over a net.Conn. Deliberately boring - the daemon and TUI ship together,
// so there is no versioning story to get wrong yet.
//
// The package carries core.Event values; it never interprets them. Nothing
// here may know Claude's JSON - that lives behind the airlock in
// internal/core/protocol.go. Since core.Event excludes Raw from its own
// JSON, not one byte of Claude's wire format crosses this socket: what a
// client receives is Wake's vocabulary and nothing else.
package rpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// Frame kinds. These strings are the wire contract: a second client - the
// SwiftUI app in the design's later phase - reads them without sharing this
// type, so renaming one is a protocol break, not a refactor.
const (
	FrameEvent = "event" // daemon → client: one session event

	// FrameHistory, FrameRoomHistory and FrameRewindTargets - the read
	// queries a client can ask about a session's transcript - and their
	// reply kinds and payload type are history.go, split out for the reason
	// its own header gives.

	FrameSend  = "send"  // client → daemon: user text for a session
	FrameSpawn = "spawn" // client → daemon: start a session

	// FrameFork branches an existing session: a new agent that inherits the
	// named parent's conversation as of the moment it is taken.
	//
	// It carries the fork's **own** SessionID - Wake mints it exactly as it
	// does for a spawn, so maySpawn is unchanged and the reaper still finds
	// that UUID in an argv - plus ParentID, and Text for a name the operator
	// chose. It is confirmed with the same FrameStatusReply a spawn is
	// confirmed with, because from the moment it starts a fork is an ordinary
	// session.
	//
	// A separate kind rather than a Spawn carrying an optional parent, for the
	// reason FrameStop and FrameKill are separate: a spawn frame arriving with
	// that field empty needs a meaning, and "spawn fresh" is the wrong answer
	// to a fork whose parent id was dropped in transit. An unrecognized kind
	// starts nothing, which is the safe end.
	FrameFork = "fork" // client → daemon: start a session as a fork of another

	// FrameImport adopts a session Wake never started: a transcript sitting in
	// ~/.claude/projects, from a `claude` somebody ran in a terminal.
	//
	// It carries the same two ids FrameFork does - the new session's own
	// SessionID, minted by Wake, and ParentID naming the transcript - and it
	// carries **no Dir**, because the directory an import runs in is the one
	// discovery *proved* and never one a client chose. claude locates a
	// transcript by the directory the process was started in, so a directory on
	// the wire would be a client's guess at the one fact that cannot be guessed
	// at (2026-08-10 findings §12).
	//
	// **A separate kind from FrameFork, and the reason is the gate rather than
	// the argv.** Both end in the same `--resume <src> --fork-session
	// --session-id <new>`, so a shared kind is tempting. But forkSource refuses
	// a parent this daemon has never held, deliberately - its own comment says
	// discovery across ~/.claude/projects belongs to importing - and widening
	// it would make every `wake fork <stranger-uuid>` reach into a tree of
	// other people's conversations. Two verbs whose refusals differ are two
	// kinds here, exactly as FrameStop and FrameKill are.
	//
	// It is confirmed with the FrameStatusReply a spawn and a fork are
	// confirmed with, because from the moment it starts an imported session is
	// an ordinary Wake session.
	FrameImport = "import" // client → daemon: adopt a transcript Wake did not write

	FrameHello = "hello" // daemon → client: handshake, sent on connect
	FrameError = "error" // either direction: something went wrong

	// Answering a permission request. The ask arrives as an ordinary
	// FrameEvent carrying a core.KindPermissionRequest; these two are the
	// way back. Without them Wake surfaces a blocked agent and has no way
	// to unblock it: --permission-prompt-tool stdio is in every spawn's
	// argv, which makes the block indefinite rather than a timeout.
	//
	// Two kinds rather than one kind with a behavior field, for three
	// reasons, and the third is the one that decides it:
	//
	//   - The payloads are genuinely different. An allow carries the input
	//     the tool will actually receive; a deny carries prose the model
	//     reads verbatim. Neither field means anything on the other answer,
	//     so one kind would make both optional and let a caller send an
	//     allow carrying a refusal.
	//   - It matches the verbs either side already has -
	//     core.Session.AllowTool / DenyTool below, core.EncodeAllow /
	//     EncodeDeny beneath those - so the daemon's dispatch is one switch
	//     with no second branch inside it.
	//   - A malformed answer must not be able to grant anything. With a
	//     behavior field, a frame that arrives with it empty needs a
	//     default, and every default is wrong: allow grants a tool call
	//     nobody approved, deny silently refuses one somebody did. With two
	//     kinds an unrecognized kind is simply unrecognized, and the agent
	//     stays blocked - which is the safe end of that failure.
	FrameAllow = "allow" // client → daemon: answer a permission request with yes
	FrameDeny  = "deny"  // client → daemon: answer a permission request with no

	// FrameAnswer settles an ask that put questions to the operator - a
	// core.AskChoice - by carrying the choices they made. Beneath the daemon
	// it is still an allow on Claude's wire, with the answer inside it; here
	// it is a third kind for the third reason FrameAllow and FrameDeny are
	// two, and that reason decides it again.
	//
	// A malformed answer must not be able to settle anything. Folding this
	// into FrameAllow with an optional Answers field means a frame arriving
	// with that field empty needs a meaning, and the only available one is
	// "allow it bare" - which is precisely the defect: the tool runs, the
	// model is told nobody answered, and the turn ends successfully with
	// nothing anywhere reporting the loss. With a separate kind an answer
	// carrying no choices is refused and said out loud, and the ask stays
	// answerable.
	//
	// The payloads are genuinely different too, in the same way the comment
	// above describes. An allow's UpdatedInput replaces what a tool will run
	// with; an answer's is the ask's own input travelling back so the airlock
	// can fold the choices into it - the client never opens either.
	FrameAnswer = "answer" // client → daemon: answer the questions an ask put

	// FrameRename changes what a session is called, and FrameLabel changes what
	// it is working on: the two halves of `sydney <> dev-5748`, which the
	// founding message asks for as *"you can either rename or assign a 'task'"*.
	//
	// # A rename changes the handle, and that is the decision rather than a
	// # consequence
	//
	// **Nothing below this line is affected.** A name has never been an address:
	// this frame carries `SessionID`, the reaper proves a process group by
	// finding that UUID in an argv, and `wake attach sydney` resolves the name
	// to an id in the *client*. So a rename is display everywhere except the one
	// place a name has ever been an address — a composer, where `@sydney` is how
	// an operator addresses one agent.
	//
	// It changes that too, and the alternative was considered and is worse.
	// Keeping `@old` working as an alias would mean two words reaching one
	// agent while the roster advertises one of them — and names are **released
	// and reissued**: `alex` goes back to the pool the moment its session ends,
	// so an alias outlives the thing it named and `@alex` starts resolving to
	// two live agents. `core.Resolve`'s exact match is unambiguous *because* the
	// daemon guarantees no two live sessions share a name, and an alias breaks
	// that guarantee at its root, in the direction that misroutes silently.
	// So the daemon's registry releases the old name and takes the new one in
	// one locked step, and `@old` afterwards resolves to nothing — a refusal the
	// operator reads, which is what `Route.Resolved` exists to produce.
	//
	// # Two kinds and not one field
	//
	// FrameAllow / FrameDeny's reason, and it decides it the same way: a frame
	// arriving with the field empty needs a meaning, and the two meanings are
	// not the same. An empty **name** is what `claim` reads as *"pick one from
	// the pool"*, so a rename that silently accepted it would rename an agent
	// somebody named to a random word. An empty **label** is plausibly *"put it
	// back to the branch"*, which is a different verb nothing has asked for. One
	// kind with an optional field would have to pick one of those as its
	// default, and both are wrong.
	//
	// Both carry `Text` and `SessionID`, and both are refused for a session that
	// is parked or ended - see daemon/rename.go for the verdict per state and
	// why the park book is the reason.
	FrameRename = "rename" // client → daemon: call this session something else
	FrameLabel  = "label"  // client → daemon: say what this session is working on

	// FrameColor sets a session's identity colour: the hue its name-tag, status
	// bar and roster row are drawn in, so thirty agents' turns in the room are
	// told apart by more than name text. Display only, and it never reaches an
	// argv - the colour is Wake's, not something claude is told about.
	//
	// It carries Text (a colour name, or rpc.ColorNone to clear) and SessionID,
	// and is refused for a parked or ended session for FrameRename's reason: a
	// parked session's display halves live in the park book, and the colour rides
	// with them rather than being rewritten there out of band. The value is fenced
	// by NormalizeColor on both sides.
	FrameColor = "color" // client → daemon: set a session's identity colour

	// FrameMode changes the permission mode of a session already running - the
	// mechanism behind ⇧⇥, and the thing that was missing while the composer
	// showed a mode it could not set.
	//
	// # A kind, not a field
	//
	// FrameAllow / FrameDeny's argument again, and FrameRename / FrameLabel's:
	// a frame arriving with the field empty needs a meaning, and here both
	// available readings are wrong. "Leave it alone" makes the frame a no-op
	// nobody would send; "put it back to the spawn mode" is a different verb
	// nobody has asked for. So an empty Mode is refused, and the refusal reaches
	// the operator rather than being resolved by a default.
	//
	// # It is a write to a running process, so it goes through the queue
	//
	// Unlike FrameWake, which starts something, this is a line on the stdin of a
	// process that already exists - the same path FrameSend and FrameInterrupt
	// take, and it must stay behind the messages already queued for that agent.
	// Two frames interleaved on stdin is a line claude cannot parse.
	//
	// # The answer comes back as an event, not as a reply
	//
	// There is no FrameModeReply. The receipt is a control_response the agent
	// emits, so it arrives on the event stream as a core.KindControlReceipt
	// carrying Event.PermissionMode - and that is the authority on what the mode
	// became, never the Mode sent here. `manual` is accepted by the CLI and
	// silently normalizes to `default`, so the two genuinely differ
	// (docs/superpowers/notes/2026-08-12-permission-mode-findings.md §6).
	//
	// A refusal arrives the same way: subtype "error" with the reason in
	// Event.Control.Error. An unknown mode is refused, and so is
	// bypassPermissions unless the process was launched with
	// --dangerously-skip-permissions - which nothing in this tree passes, so
	// that mode is unreachable by construction rather than by a check here.
	FrameMode = "mode" // client → daemon: change a running session's permission mode

	// FrameRewind asks a running session to rewind its conversation to an
	// earlier user turn - RewindTarget and RewindLastSeen below. It travels the
	// same control-write path as FrameSend / FrameInterrupt / FrameMode: a line
	// on the stdin of a process that already exists, behind whatever was
	// already queued for that agent. And it answers the way FrameMode does -
	// no FrameRewindReply, only a control_response on the event stream,
	// arriving as a core.KindRewindReceipt, which is the only authority on
	// whether it rewound.
	FrameRewind = "rewind" // client → daemon: rewind a running session's conversation
)

// RoleManager marks the one session that gets Wake's own tools.
//
// One value, because there is one role. It is a *word on the wire* rather than
// an alias of core.ManagerName, which is the same string: that one is what a
// person types in a composer, this one is what a spawn frame says it is asking
// for, and tying them together would make a change to either a silent change to
// the other.
//
// What it decides is the **name**, and the name decides everything else. The
// daemon refuses this name to an ordinary spawn (daemon/names.go's reserved
// set), so a session called `manager` is one the daemon deliberately named, and
// daemon/manager.go keys the tools and the scoping prompt off that. So this
// field is not "the manager's configuration on the wire" - there is deliberately
// no such thing, because a configuration off the wire is one any process that
// can dial this socket could choose, and what it would be choosing is a command
// line for the one session holding tools that act on the whole fleet.
const RoleManager = "manager"

// maxFrameBytes bounds one frame, and equals core's maxLineBytes so that a
// line the airlock admitted can still reach a client. bufio.Scanner's 64KB
// default is far too small and fails by *truncating*, so an oversized frame
// would arrive as silent corruption rather than an error; here it is an
// error, and one that ends the connection for every session on it.
//
// Equal limits only work because core.Event.Raw is tagged json:"-". While
// Raw crossed, a frame was its whole source line *plus* the decoded text -
// measured at 1.95x the largest recorded line (28,832 bytes from 14,778) -
// so the effective ceiling on a deliverable line was about 8MB, and a 10MB
// tool result would decode cleanly and then kill the connection. With Raw
// off the wire the largest frame in the corpus is 13,949 bytes, 0.944x the
// largest line: the frame carries text that was already escaped inside that
// line, minus the metadata Wake discards.
//
// The other half of holding that ratio is WriteFrame's SetEscapeHTML(false)
// - see TestWriteFrameDoesNotEscapeAngleBrackets. Go's default would turn
// every <, > and & into six bytes, which costs a rounding error on prose
// but inflates a tool result that is 10% angle brackets - an HTML file, an
// SVG, a JSX component - by about half, putting a frame at ~1.4x its line
// and failing anything past ~11MB.
//
// Sub-1.0 is the behavior at the ceiling, not a rule every frame obeys.
// Three ways a frame can exceed its line, none of them a problem where the
// limit actually bites:
//
//   - Fixed overhead dominates a short line. The worst ratio in the corpus
//     is 1.0638 - a 200-byte session_reset frame from a 188-byte line -
//     because the envelope plus a session id repeated in both Frame and
//     Event outweighs a line with almost no content. It amortizes to
//     nothing by the megabyte.
//   - core.jsonString falls back to string(raw) when a block's content is
//     not a JSON string, the tool_result-content-as-array shape the
//     findings note lists as never observed. That text was not escaped in
//     the source line, so the frame escapes every quote in it for the
//     first time: roughly 1.1-1.2x on the payload.
//   - Angle-bracket density, above, if SetEscapeHTML ever comes back.
const maxFrameBytes = 16 * 1024 * 1024

// readBufBytes is the scanner's starting buffer. It grows to maxFrameBytes
// on demand, so this only decides how many frames avoid a realloc, not
// which frames are readable.
const readBufBytes = 64 * 1024

// framesBuffer lets the reader stay ahead of a busy consumer by a burst of
// frames rather than lock-stepping with it. It is a smoothing buffer, not a
// backlog: a consumer that stops reading entirely still stalls the reader.
const framesBuffer = 64

// Frame is one message on the socket. Every field but Kind is optional and
// omitted when empty, so the common event frame stays small across a fleet
// of 15-30 sessions.
type Frame struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id,omitempty"`

	// RequestID correlates an answer with the permission request it
	// answers, and it is not interchangeable with SessionID. SessionID says
	// which process the answer is written to; RequestID says which ask
	// inside that process it settles. One agent can have more than one ask
	// outstanding, so an answer addressed by session alone would settle
	// whichever the daemon happened to pick.
	//
	// It is also the id the *ask* had no choice about: a can_use_tool
	// control_request carries no session_id on Claude's wire at all, and
	// the SessionID a client reads off the event was stamped by
	// core.Session from the pipe it arrived on.
	RequestID string `json:"request_id,omitempty"`

	Text string `json:"text,omitempty"`

	// Images are attachments on a FrameSend, dropped into the composer and
	// carried alongside Text. omitempty keeps every text-only send on the wire
	// unchanged - the additive rule this frame's header states. The daemon
	// hands them to core.Session.Send, which renders Claude's wire shape.
	Images []core.ImageBlock `json:"images,omitempty"`

	Event *core.Event `json:"event,omitempty"`

	// Events is a conversation read back off disk, on FrameHistoryReply and
	// nowhere else. A slice rather than one frame per event because it is one
	// answer to one question: a client folding it has to know when the past
	// ends and the present begins, and N frames make that a guess.
	Events []core.Event `json:"events,omitempty"`

	// RewindTargets is a session's active-branch user prompts, on
	// FrameRewindTargetsReply and nowhere else. See Events, just above, and
	// history.go's RewindTarget.
	RewindTargets []RewindTarget `json:"rewind_targets,omitempty"`

	// Dir is the directory a spawned session runs in, and it must be
	// absolute. **Spawn only, and that is a rule about FrameFork rather than
	// a note about scope**: a fork runs where its parent ran and this field is
	// *ignored* on one, because claude derives the project slug from the
	// working directory and the transcript being forked lives under the
	// parent's. A fork frame may carry one - the daemon still refuses a
	// relative path on any frame, since a malformed field is worth a sentence
	// either way - but nothing reads it, and a second client that filled it in
	// expecting the fork to run there would be wrong with no error to say so.
	//
	// It is on the wire because the daemon is one process per machine and
	// the client is wherever the user is. Without it an agent runs in the
	// daemon's working directory - whichever repository the client that
	// happened to start the daemon was in - so an agent asked for from repo
	// B edits repo A. That also decides where claude persists the
	// transcript, so a later --resume inherits the mistake.
	//
	// Absent means "the daemon's own directory" **on a spawn**, which is the
	// honest extent of what it can know, and omitempty keeps a frame that does
	// not carry one exactly as it was before this field existed. On a fork it
	// means nothing at all, present or absent.
	Dir string `json:"dir,omitempty"`

	// Worktree is the name of a git worktree to create under Dir's repository
	// and run this session in, or empty for a session that runs in Dir itself.
	// Spawn only, exactly as Dir is.
	//
	// A name and never a path: it becomes one directory segment under the
	// repository root, and a path here would let anything that can dial the
	// socket choose where a session runs - the same argument that keeps the
	// manager's --mcp-config off the wire. daemon.validWorktreeName is the
	// fence, and it runs before git is reached.
	Worktree string `json:"worktree,omitempty"`

	// AddDir are directories outside Dir that this session's tools may reach.
	// Spawn only, exactly as Dir is, and every element is fenced by ValidAddDir
	// before anything is started.
	//
	// It is a path where Worktree is a name, and paths.go argues why: a client
	// that can dial this socket already chooses Dir, so an added directory names
	// nothing it could not have named there. What the fence adds is that a
	// directory arrives as the directory it says.
	//
	// **It does not survive a park**, unlike Effort and the two spend fields,
	// and that is scope rather than a ruling: a cap survives because nothing
	// can put one back afterwards, and a directory can simply be asked for
	// again. Carrying it across one is a field on parkedRecord.
	AddDir []string `json:"add_dir,omitempty"`

	// Debug is a category filter for this session's debug logging and DebugFile
	// is what to call the log. Spawn only, and neither survives a park, on
	// AddDir's terms.
	//
	// **DebugFile is a name and never a path**, which is the whole fence: it
	// becomes a file the daemon's child creates and truncates, and a path here
	// would let anything that can dial this socket choose where that write
	// lands. Same argument as the manager's --mcp-config, which is off this
	// wire entirely for it. The daemon resolves the name beside its own socket;
	// rpc.ValidDebugFileName is the fence and daemon/debuglog.go is the
	// directory.
	//
	// A filter with no file is refused rather than emitted: `--debug` alone
	// produces nothing readable in the mode every agent runs in, so it is a
	// flag an operator believes they turned on and no log anywhere. Recorded in
	// core/debug.go's header.
	Debug     string `json:"debug,omitempty"`
	DebugFile string `json:"debug_file,omitempty"`

	// ParentID is the session a FrameFork branches from, and it is not
	// interchangeable with SessionID: SessionID is the id the *new* agent will
	// run under, ParentID is the conversation it inherits. Fork only.
	//
	// Nothing else on this socket carries it, and nothing ever addresses an
	// agent by name here - the client resolves `wake fork sydney` to an id
	// before it writes this frame.
	ParentID string `json:"parent_id,omitempty"`

	// UpdatedInput is the input an allowed tool will actually receive.
	// Absent means "run it exactly as it asked", which is the only
	// behaviour with a recording behind it - the probe that produced
	// testdata/stream/permission.jsonl echoed request.input back unchanged,
	// and the findings note says in so many words that sending {} instead
	// was never tested.
	//
	// omitempty therefore does real work: it collapses both a nil map and
	// an empty one to an absent key, so this transport cannot express the
	// untested shape even by accident. core.EncodeAllow omits the wire key
	// for a nil map in the same way.
	//
	// Spelled updated_input and not updatedInput on purpose. Claude's own
	// key is camelCase; this package is not allowed to know that, and a
	// field named the way the far wire names it is how a transport starts
	// looking like a passthrough.
	//
	// It carries a second thing on a FrameAnswer, and the two do not conflict
	// because the absent case is unreachable there: an answer frame carries
	// the ask's own input straight back, and core.EncodeAnswer refuses one
	// that arrives without it rather than inventing the {} this field cannot
	// express. So "absent means run it exactly as it asked" remains the whole
	// meaning on a FrameAllow, which is the only kind that can be sent bare.
	//
	// Meaningless on a deny.
	UpdatedInput map[string]any `json:"updated_input,omitempty"`

	// Answers is what the operator chose, keyed by the text of the question
	// they chose it for. FrameAnswer only.
	//
	// A map[string]string rather than a second map[string]any, so it cannot be
	// confused with UpdatedInput by a caller or by a reader. Nothing here
	// knows what the far wire does with it: core.EncodeAnswer folds it into
	// the ask's input under Claude's key, and this package never sees that
	// key.
	//
	// omitempty here is presentation only, not a semantic like UpdatedInput's.
	// An answer with no choices is refused behind the daemon rather than
	// silently meaning something, so an empty map and an absent one are the
	// same error rather than two different behaviours.
	Answers map[string]string `json:"answers,omitempty"`

	// Reason is why a tool call was refused, and it is not a log line: it
	// reaches the model verbatim as the tool result, prefixed with
	// "Error: ", so it is the one channel for telling an agent what to do
	// instead of retrying the identical call.
	//
	// Passed through exactly as given, blank included. core.EncodeDeny
	// substitutes a default for a blank reason and is the only layer that
	// can - it is the only one that knows both that the field is omitted
	// when empty and that its contents are read by the model. A second
	// default here would be a competing one. Deny only.
	Reason string `json:"reason,omitempty"`

	// Mode is the permission mode a FrameMode asks for, and it is required on
	// one - see that kind for why an empty one is refused rather than defaulted.
	//
	// The value is one of core's PermissionMode constants. This package does not
	// spell them: the words are Claude's, core.EncodeSetMode is what puts one on
	// the far wire, and a transport that named them would be deciding what a
	// mode means. The json tag is Wake's own socket, deliberately spelled the
	// same as the receipt's key so a second client reading both sees one word
	// for one thing - Answers' case exactly.
	Mode string `json:"mode,omitempty"`

	// RewindTarget is the message uuid a FrameRewind asks the session to
	// rewind to, and RewindLastSeen is the uuid it declares as the tip -
	// Claude's own optimistic-concurrency guard, so a rewind aimed at a
	// conversation that moved on since is refused rather than landing
	// somewhere nobody asked for. Both required, on Mode's own argument: core.
	// EncodeRewind refuses an empty one rather than guessing what it means.
	RewindTarget   string `json:"rewind_target,omitempty"`
	RewindLastSeen string `json:"rewind_last_seen,omitempty"`

	// Role marks a spawn as something other than an ordinary agent. Empty is
	// an agent, which is what every existing client sends and what every
	// existing frame means.
	//
	// A field rather than a second frame kind, and the difference from
	// FrameAllow / FrameDeny is which way the default fails. There, a frame
	// arriving with an empty behavior would need a default and every default
	// destroys something. Here the default is "an ordinary agent" - the safe,
	// existing, overwhelmingly common case - so an unrecognized role degrades
	// to the thing Wake was already doing.
	//
	// **Spawn only, and it decides a name rather than an address**: the manager
	// is still addressed by session id like every other agent. What the daemon
	// does with it is claim the one name it refuses to every ordinary spawn,
	// which is also the whole of "only one manager exists at a time" - the
	// second claim of a taken name fails, with nothing started.
	//
	// It carries no configuration, deliberately. The obvious next field is a
	// path to the manager's MCP config, and an MCP config names a *command to
	// execute*: putting one on this socket would let anything that can dial it
	// choose the command line of the one session holding tools that act on the
	// whole fleet. The daemon derives it instead - see daemon/manager.go, which
	// also explains why that is the only version that survives a park.
	Role string `json:"role,omitempty"`

	// Effort is the reasoning level a spawned session runs at, and it is the
	// one piece of configuration this socket does carry. Role's paragraph above
	// refuses the obvious next field for a reason that does not reach this one:
	// an MCP config names a command to execute, so a client choosing it chooses
	// a command line. Effort names nothing. It is a closed five-value set the
	// daemon checks against core.ValidEffort before a session is started, and a
	// value outside it is refused with nothing spawned - so the worst a hostile
	// client can do here is pick between five levels of thinking on a session
	// it was already permitted to start.
	//
	// **Spawn only, and on a fork it is ignored rather than inherited.** A fork
	// starts at no chosen effort whatever this field says - daemon/fork reads
	// it nowhere, exactly as it reads no Model and no MCP config - so a client
	// that sets it on a FrameFork has it dropped in silence. Said plainly
	// because the alternative reading, that a fork picks its own level here, is
	// what this comment used to imply and no code does.
	//
	// Absent means "Wake chose nothing", which is not the same as any
	// level - it leaves --effort off the argv entirely and lets claude apply
	// its own default, and it is why "" is deliberately not a valid level.
	//
	// It survives a park, unlike Role's configuration: see parkedRecord.
	Effort string `json:"effort,omitempty"`

	// Model is what a spawned session runs as, and it is the second piece of
	// configuration this socket carries. Role's paragraph refuses the obvious
	// next field because an MCP config names a *command to execute*; a model
	// names nothing, exactly as an effort names nothing. The worst a hostile
	// client can do here is pick the model of a session it was already
	// permitted to start.
	//
	// **Not checked against a list, and that is the difference from Effort.**
	// Effort is safe to carry because its set is closed; this one has no
	// knowable set at all - nothing on claude's wire enumerates the models and
	// --help gives only examples - so a daemon-side allowlist would refuse
	// names claude accepts. core.ValidModel therefore asks only whether a
	// model was chosen, and what protects the command line is that a model is
	// one argv word which cannot introduce another: it is passed as a single
	// element of an exec argv, never through a shell.
	//
	// Spawn only, and ignored on a fork exactly as Effort is. It survives a
	// park - see parkedRecord.
	Model string `json:"model,omitempty"`

	// MaxBudgetUSD is the ceiling on what a spawned session may spend and
	// FallbackModel is the chain it fails over to. They are the third and fourth
	// pieces of configuration this socket carries, and they are here on Effort's
	// argument rather than Model's: neither names a command to execute, so the
	// worst a hostile client can do is cap - or fail over - a session it was
	// already permitted to start.
	//
	// **A budget is checked and a chain is only bounded**, which is the same
	// split Effort and Model already have one line up. core.ValidBudget knows
	// what an amount looks like, so a value that is not one is refused with
	// nothing spawned. core.ValidFallbackModel cannot know what a model looks
	// like - Model's own paragraph says why there is no knowable set - so it
	// asks Model's question of every link and refuses only an empty one, the
	// shape a trailing comma produces and no frame afterwards reports.
	//
	// Spawn only, ignored on a fork exactly as Effort and Model are. Both
	// survive a park, and for them that is load-bearing rather than tidy: there
	// is no runtime command for either, so a cap the wake dropped would make ⌃Q
	// the way to uncap a fleet. See parkedRecord.
	//
	// Absent means "Wake chose nothing" for both - the flag is left off the argv
	// and claude applies its own default, the meaning "" already carries for
	// Effort and Model.
	MaxBudgetUSD  string `json:"max_budget_usd,omitempty"`
	FallbackModel string `json:"fallback_model,omitempty"`

	// Status is the fleet report, carried by the two kinds that report one -
	// FrameStatusReply, which answers a request, and FrameStatusPush, which
	// is the daemon announcing a change nobody asked about. Nil on every
	// other kind. Which of the two a frame is matters to a client that has a
	// question outstanding, and lifecycle.go is where that distinction is
	// explained.
	//
	// A pointer for the same reason Event is one: an event frame crosses
	// this socket thousands of times per session and must not carry an empty
	// object for a field it never uses.
	Status *Status `json:"status,omitempty"`
}

// writeMu serializes every write in this package. Sessions fan out to one
// connection from many goroutines, and two concurrent Writes interleave
// bytes and corrupt the stream beyond recovery.
//
// It is process-wide rather than per-connection because io.Writer has no
// identity to key on and no close hook to clean up by. That makes it a
// correctness backstop, not a throughput strategy, and it puts a liveness
// requirement on every caller: a writer that blocks - a client whose socket
// buffer filled because it stopped reading, a laptop whose lid shut - holds
// this lock and stalls every write in the process, to every other client.
//
// A per-client writer goroutine with a bounded queue does NOT fix that. It
// bounds memory; the goroutine still blocks inside conn.Write holding this
// lock, and every other client's writer then blocks acquiring it. Add a
// mutex the daemon holds across the write and one wedged client wedges the
// daemon permanently.
//
// What actually restores liveness is a bound on the write itself:
// conn.SetWriteDeadline before each WriteFrame, treating the timeout as
// "this client is gone" and closing the connection. A per-connection lock
// instead of this one would also do it, at the cost of an ownership story
// io.Writer cannot express. Either way the fix belongs to whoever owns the
// connections, because that is the only layer that knows when to hang up.
var writeMu sync.Mutex

// WriteFrame writes one newline-terminated JSON frame. It serializes writes
// so concurrent senders cannot interleave bytes on one connection, and the
// JSON encoding escapes any newline in the payload so a multi-line message
// stays a single frame.
//
// It encodes into a local buffer rather than straight to w for three
// reasons: the encode stays outside the lock, an encode failure writes
// nothing at all rather than half a frame, and the critical section is
// exactly one Write of the complete frame. Encode supplies the trailing
// newline, so there is no separate terminator to forget.
//
// This is the only write path in the package; adding a second one that
// bypasses writeMu would reintroduce interleaving.
//
// Writing a frame *into a buffer* and then writing that buffer to a socket is
// not a second path, and internal/daemon does exactly that on purpose. The
// hazard this lock exists for is two goroutines interleaving bytes on one
// connection; the daemon gives each connection a single writer goroutine, so
// the only thing it needs from this function is the encoding, and taking the
// socket write out from under a process-wide lock is what stops one stalled
// peer from stalling every other client. Do not "simplify" that back to
// WriteFrame(conn, f).
func WriteFrame(w io.Writer, f Frame) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}

	writeMu.Lock()
	defer writeMu.Unlock()
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

// defaultWriteTimeout bounds one client write to the daemon.
//
// It is the same five seconds the daemon gives a write to a client, and
// deliberately so: the two ends of a unix socket on one machine have the same
// answer to "how long can a peer that is behaving take", and a bound either
// side can outlast is a bound the other side has to guess at.
//
// writeTimeout is a var only so tests can compress it; nothing outside a test
// assigns it. Unsynchronised, and safe on the same terms as the compressible
// timeouts elsewhere in this tree: no test that assigns it runs in parallel.
const defaultWriteTimeout = 5 * time.Second

var writeTimeout = defaultWriteTimeout

// WriteFrameTo writes one frame to a socket under a deadline. Every client in
// this tree writes through this and not through WriteFrame.
//
// # Why the deadline is not optional
//
// writeMu is process-wide, and WriteFrame holds it across w.Write. A daemon
// that stops draining a client's frames fills that client's socket buffer, and
// the write then parks *inside the lock* - so the caller that parks takes every
// other write in the process with it, including the ones a user is waiting on.
// The header above states the hazard and prescribes exactly this fix; the
// daemon applied it to its own writes and the client side never did, which is
// how one wedged write became three call sites with no bound at all.
//
// This is not a second write path in the sense the header forbids. It adds a
// deadline and calls WriteFrame; the encoding, the lock and the single Write of
// a complete frame are unchanged, so nothing here can interleave bytes.
//
// # Why the deadline is set and never cleared
//
// Clearing it is what an earlier version of this function did, and it restored
// the exact failure it exists to prevent - at this function's worst call site.
// The deadline belongs to a *connection*, not to a call, and the clearing
// necessarily happens after WriteFrame has released writeMu. Two goroutines
// writing to one connection then interleave like this:
//
//	second: SetWriteDeadline(+5s)
//	first:  Write completes, unlocks writeMu
//	first:  SetWriteDeadline(zero)      <- removes the second's bound
//	second: takes writeMu, Write parks   <- forever, inside the lock
//
// and every write in the process queues behind it. bubbletea runs every tea.Cmd
// on its own goroutine, so two Enter presses in quick succession are exactly
// two concurrent calls here on the one TUI connection. There is no ordering
// that makes a set/write/clear triple safe unless the clear is inside the lock,
// and putting it there means a second write path through rpc's encoder.
//
// Nothing needs it cleared. A leftover write deadline does not bound a read -
// `wake stop` writes its quit and then reads for two minutes under its own
// SetReadDeadline, unaffected - and every write on these connections comes
// through this function, which sets its own bound first. A caller that writes
// to a wake connection by any other path inherits whatever is on it, which is
// one more reason for there to be no other path.
//
// Two writers that both set a deadline before either writes leave the second
// with a bound that started earlier and so expires sooner. Tighter, never
// absent, on a connection already demonstrating that it is sick.
//
// # What a timeout means, and what the caller owes
//
// The deadline can expire mid-write, which leaves a partial frame on the wire.
// The peer's reader then reports a decode error and ends the connection, which
// is the correct outcome and not a recoverable one: **a connection this
// returned an error for must not be written to again.** The caller owns the
// connection and so owns the hanging up - this cannot do it, because a
// transport that closed its caller's socket would be deciding a policy only the
// caller knows (the TUI reattaches; `wake stop` reports and exits).
func WriteFrameTo(c net.Conn, f Frame) error {
	if err := c.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("bounding the write: %w", err)
	}
	return WriteFrame(c, f)
}

// ReadFrames decodes frames until the reader is exhausted. Both channels
// are closed when the goroutine finishes, so a caller can range over frames
// and then check errs.
//
// The reader ends on the first malformed frame: newline framing means a
// decode failure is a peer that is not speaking this protocol or a stream
// already desynced, and continuing would launder corruption into plausible
// frames. Ending is not crashing - the error is surfaced, both channels
// close, and the goroutine returns, which for a daemon means "close this
// connection and let the client reconnect".
//
// A vanishing client is the ordinary case, not an error: a closed
// connection reads as EOF and closes both channels with nothing on errs.
//
// The caller must drain frames until it is closed. Abandoning it while the
// reader still has data parks the goroutine on a send forever; closing the
// underlying reader does not unblock a goroutine already stalled there.
func ReadFrames(r io.Reader) (<-chan Frame, <-chan error) {
	frames := make(chan Frame, framesBuffer)
	errs := make(chan error, 1)

	go func() {
		defer close(frames)
		defer close(errs)

		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, readBufBytes), maxFrameBytes)
		for sc.Scan() {
			// A bare newline is not a frame. Skipping it keeps a stray
			// blank line from surfacing as a zero-valued Frame.
			if len(sc.Bytes()) == 0 {
				continue
			}
			var f Frame
			// Unmarshal allocates every string it keeps, so the decoded
			// frame does not alias the buffer the scanner reuses.
			if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
				errs <- fmt.Errorf("decode frame: %w", err)
				return
			}
			frames <- f
		}
		if err := sc.Err(); err != nil {
			errs <- fmt.Errorf("read frames: %w", err)
		}
	}()

	return frames, errs
}

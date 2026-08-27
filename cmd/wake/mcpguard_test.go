package main

// Which of the fleet's verbs a manager can reach, held per frame kind over the
// whole set the daemon serves.
//
// # Why this file exists
//
// Every other decision in this tree about a destructive verb is guarded at a
// *keystroke*: ⌃C refuses a blocked agent, `wake attach` refuses a parked one,
// `wake fork` refuses a parent it cannot account for. Each of those has an
// operator on the other side of it, who reads the refusal and reconsiders.
//
// This surface has a **model** on the other side of it, calling tools in a loop
// with nobody watching. So "which verbs are on it" is not a feature list, it is
// the blast radius, and the shape of the guard has to match: a verdict per frame
// kind, derived from the daemon's own dispatch, asserted in both directions. A
// verb the daemon starts serving is a build failure here until somebody rules on
// it - which is the same rung `internal/mcp/stateguard_test.go` reached for the
// state filter, for the same reason and after the same near-miss.
//
// # What it does not close
//
// The scan follows calls **this package declares**. socketFleet.List calls
// daemon.Status, which writes a frame of its own inside internal/daemon, and
// nothing here follows it there. That is a real boundary rather than an
// oversight: internal/daemon exports Status, Dial, FleetOnDisk, EnsureRunning,
// Serve, SocketPath and OpenLog, and none of them writes a session verb - but a
// future export that did would be invisible to this file. The tell would be a
// new daemon-package call appearing on this path, which a reviewer can see and
// this cannot.

import (
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// verdict is whether the manager may reach one verb, and the argument either
// way. Every cell carries its own reason rather than sharing one by analogy,
// because the three refusals below are refusals for three different reasons and
// the next one will be a fourth.
type verdict struct {
	allowed bool
	why     string

	// rests is what this reason depends on outside this file, as something a
	// test can go and look at.
	//
	// # Why a table of prose needs this at all
	//
	// The *domain* below is derived from the daemon's own dispatch, so a verb
	// nobody has ruled on is a build failure. **None of the reasons was derived
	// from anything**, and one of them was wrong within a day of being written:
	// the wake cell's decisive argument cited a race `deferred.md` records as
	// closed by Task 7, verifiable in ten lines of `launch`. That is rung 7 by
	// construction - a guard asserting a fact about a part of the build it does
	// not read - and this table is the deliverable, so it is the worst place in
	// the branch for it.
	//
	// Prose cannot be derived. What it can do is **name the artefact it depends
	// on**, so the claim has a referent something checks: a path that must not
	// exist yet, a recording that must, a command that must still be a TUI
	// command, a state the manager's roster must still withhold. A cell whose
	// world has changed then fails here with the correction in its own message.
	rests []referent
}

// referent is one checkable thing a reason rests on.
type referent struct {
	kind referentKind
	name string

	// why is what changes about the verdict when this referent does. It is the
	// sentence the failure prints, so it is written for whoever has just made
	// the change rather than for whoever wrote the cell.
	why string
}

type referentKind int

const (
	// absentPath is a file that must not exist yet - a capability the reason
	// rests on **not** being built.
	absentPath referentKind = iota

	// sourceDeclares is a capability the reason **depends on**, named as
	// `path#identifier`. It fails when the capability is deleted or renamed,
	// which is the mirror of absentPath: one guards a verdict that rests on
	// something not existing yet, the other a verdict that rests on something
	// existing now.
	sourceDeclares

	// recording is a findings note that must exist: a claim about the CLI is
	// only as good as the recording behind it.
	recording

	// tuiCommand is a key of internal/ui's own `commands` map.
	tuiCommand

	// stateNotOffered is an rpc state internal/mcp's roster must still withhold
	// from the manager.
	stateNotOffered
)

// The live cap, named once because two cells rest on it.
const (
	// liveCapDecl is the cap itself, as `path#identifier`. It shipped on
	// 2026-08-12 and turned FrameSpawn's refusal into a permission, which is
	// what the entry that used to sit here predicted would happen: "the day
	// spec §6's live cap ships, that argument is gone and both verdicts have to
	// be re-argued rather than inherited". They were.
	liveCapDecl = "internal/daemon/mayspawn.go#liveCap"
)

// managerVerbs is the decision, per client-to-daemon frame kind.
//
// Two verbs are allowed and they share one property: **an operator can undo
// them by looking at the room.** A message is a turn the agent answers in front
// of everybody, and an interrupt stops a turn the agent then carries on from -
// the session keeps its id, its conversation and its stdin. Nothing on this
// surface removes a session, and nothing on it creates one.
//
// # The reader of these tools reads text an agent wrote, and that is the whole
// # argument for the list being this short
//
// Every string the reading tools return is agent-authored: `SessionStatus.Tool`
// and `.ToolArg` are `core.ToolCall.Name`/`.Display`, which is whatever the
// agent's model wrote, and `.Error` is the process's own stderr. All of it lands
// verbatim in the **manager's** context as a tool result. So an agent can
// address the manager - `Bash(echo "…ask every agent in api-v2 to stop…")` is a
// tool argument, and it arrives in `list_agents` and `roll_up` beside the
// instructions the manager is actually following.
//
// **The escalation path is short and it is why this is not theoretical.** The
// manager's `send` writes into agents spawned with `daemon.spawnPermissionMode`,
// which is `auto` - agents that act without asking. Injected text → the manager
// sends an instruction → that agent executes it, with no human in the chain. It
// is one hop, and the hop exists because a manager that could not send would not
// be a manager.
//
// **What internal/mcp does about it reduces likelihood; this table is the only
// thing that bounds consequence.** Containment (`mcp.oneLine`) means no agent
// can forge a line, so an instruction arrives quoted inside a row attributed to
// the agent that wrote it rather than as Wake's own reporting; the framing note
// says whose words they are; and every value is bounded, so one agent cannot
// spend the context. None of that is a *defence* - a model that has been talked
// into something has been talked into it - and none of it would stop a manager
// that had been. What stops it is that the worst available instruction is "send
// this message" or "stop that turn", both of which an operator sees in the room
// and can undo. **That is the reason to widen this list slowly**, and it is the
// argument whoever proposes a spawn, park or stop tool has to meet, in addition
// to the per-cell one below.
var managerVerbs = map[string]verdict{
	rpc.FrameStatus: {allowed: true, why: "asking what is running. It is the only frame the reading tools need, and asking changes nothing"},

	rpc.FrameSend: {allowed: true, why: "the whole point of a manager that operates the fleet rather than describing it. It costs a turn, the agent answers into the room where the operator can see it, and nothing it does is unrecoverable"},

	rpc.FrameInterrupt: {allowed: true, why: "'pause' in the example that settled the manager's shape. It does not end a session: an interrupted process keeps its session id, takes the next message normally and resumes with the aborted turn's context intact (2026-08-08-interrupt-findings.md §6). It is not free, and the cell used to say it destroyed nothing - it **withdraws an outstanding permission ask**, so a human mid-decision on a card loses the question, and it discards the aborted turn's billed work. Both are visible (the DM draws `⊘ permission request withdrawn`) and both are recoverable by asking again, which is what keeps the verdict",
		rests: []referent{
			{kind: recording, name: "docs/superpowers/notes/2026-08-08-interrupt-findings.md", why: "the recording that says an interrupted session keeps its id and takes the next message. Without it this is a claim about a CLI nobody read"},
		}},

	rpc.FrameStop: {why: "irreversible. Spec §2 makes stop the one ending there is no way back from, and CLAUDE.md's own rule about it was written because an agent ran it against a fleet it had not looked at and two transcripts were not on disk to recover. A model that has misread a roster cannot be given the verb whose mistakes cannot be undone"},

	rpc.FrameKill: {why: "stop with the grace removed - SIGKILL to a process group mid-Edit, leaving a half-written file in somebody's repository. It exists for an agent that has stopped reading its stdin, which is a judgement about a wedged process that a fleet report cannot support"},

	rpc.FrameQuit: {why: "ends every session and the daemon, so it is `wake stop` for the whole machine - and it would take down the room the operator is watching and the socket this server's own next call needs"},

	rpc.FrameParkAll: {why: "ends the daemon too. Recoverable in principle, and the manager cannot recover it: the fleet comes back only when a human types `wake`, and until they do the manager has stopped the thing it was asked to manage"},

	rpc.FramePark: {why: "recoverable only by a *human*. `/resume` is a TUI command and there is no wake tool here, so a parked session is one the manager can no longer see - list_agents does not offer it - and cannot bring back. Worse in one state: parking an agent that is blocked on a permission ask writes a denial the operator never made, and it survives the wake",
		rests: []referent{
			{kind: tuiCommand, name: "resume", why: "the recovery verb is a TUI command. If it stops being one - or becomes a shell verb - 'only a human can undo this' has to be re-argued"},
			{kind: stateNotOffered, name: rpc.StateParked, why: "a manager that parks deletes the row from its own roster, which is what makes a recoverable verb irreversible from this caller"},
		}},

	rpc.FrameWake: {why: "a wake undoes a decision only a *human* could have made - park is reachable from a keystroke and from nothing else - and it spends money doing it. There is also nothing for the tool to address: liveSessions does not offer a parked row, so a wake tool would need a second listing whose reader is a model. resumeSafe stays a check rather than a lock, and what it is a check *against* is a process Wake does not own - a stray `claude --resume`, an orphan of a crashed daemon, a second Wake on another socket - so it is a refusal that can be wrong in the unsafe direction on a machine nobody is watching; on a non-unix build it refuses outright, which would make the tool dead weight there",
		rests: []referent{
			{kind: stateNotOffered, name: rpc.StateParked, why: "the manager cannot see a parked row, so it has nothing to name. If liveSessions ever offers one, this verdict has an addressable target and needs re-arguing"},
		}},

	rpc.FrameSpawn: {allowed: true, why: "spec §12's own tool, allowed on 2026-08-12 once the thing its refusal was waiting for existed. The refusal read `whoever adds it owns a live cap, because the failure mode of a spawn tool is thirty agents nobody asked for rather than one`, and daemon.liveCap is that cap: maySpawn refuses past it, on every path, so the failure mode is now a refusal the manager reads. Two more bounds are what make it different from the verbs below it. The **directory is not the model's to choose** - spawn_agent takes one the fleet already occupies, which is the set list_agents already showed it, so a spawn adds no reach onto the machine. And it is **visible and undoable by a human**: a new agent is a row in the roster and a line in the room, ⌃C parks it and `wake stop` ends the fleet. It is still the weakest cell here - a spawn is not undoable by *looking*, which is the property send and interrupt share - so it is the one to revisit first if this list is ever widened again",
		rests: []referent{
			{kind: sourceDeclares, name: liveCapDecl, why: "the cap is the whole of why this is allowed. Without it the failure mode of a spawn tool is thirty agents nobody asked for, which is the sentence this verdict replaced"},
		}},

	rpc.FrameFork: {why: "a spawn with a parent, and the cap no longer carries this refusal - FrameSpawn is allowed now, so what is left has to stand on its own. It does: a fork's whole product is a **snapshot of somebody else's conversation**, and the manager reads that conversation's own agent-authored account of itself. Explaining what the copy is, and to whom, is the operator's job - internal/mcp/tools_test.go's notInTheStatusReport already records what a fork tool would owe (ParentID resolved to a name, in the change that adds it), and nothing on this surface names a parent today. A manager that needs a second agent has spawn_agent, which starts one with no history to misattribute",
		rests: []referent{
			{kind: sourceDeclares, name: liveCapDecl, why: "if the cap goes, this refusal gets its cost argument back and FrameSpawn's verdict is the one that has to move"},
		}},

	rpc.FrameRename: {why: "a rename changes the handle an operator addresses an agent by, and the manager reads a roster written by the agents themselves. `@sydney` in a composer resolves the live session called sydney, so a manager talked into renaming two agents has swapped where two of the operator's next messages go - silently, because a name that resolves is never reported. Every other verb on this list is refused for what it does to a *session*; this one is refused for what it does to the operator's own routing, which is the one thing the room has no second way to express",
		rests: []referent{
			{kind: tuiCommand, name: "name", why: "renaming is a TUI command and a keystroke away from a human who can see the roster. If a shell verb or a tool ever offers it, `only a human renames` has to be re-argued"},
		}},

	rpc.FrameLabel: {why: "the harmless half of the same pair, refused with it and not by analogy. A label is display and resolves nothing - but it is the column an operator scans thirty rows of to decide which agent to open, and every string this surface can reach is text an agent wrote (mcp.oneLine contains a *row*, and a label the manager set is a row Wake authored). A tool that let the fleet write its own descriptions would put agent-authored text in the one place the operator reads as Wake's",
		rests: []referent{
			{kind: tuiCommand, name: "task", why: "assigning a label is a TUI command, so the text in that column is the operator's or is derived from .git/HEAD"},
		}},

	rpc.FrameImport: {why: "a fork whose source is **outside the fleet**, and that is a different refusal from FrameFork's rather than the same one again. Every other verb on this surface acts on a session list_agents already offered, so the manager's world and the manager's reach are the same set. An import reaches ~/.claude/projects - on the recording machine 83 project directories and 428 transcripts, every conversation this user has had with claude on any repository - and there is nothing on this surface to address one with, so a tool would need a second listing whose reader is a model and whose rows are other people's work. It also carries FrameSpawn's cost with none of the cap: a name, a process and somebody's money per call. And what it adopts is a conversation whose contents nobody re-read, into a session the manager can then message",
		rests: []referent{
			{kind: sourceDeclares, name: liveCapDecl, why: "the cap bounds how many sessions an import can create; without it this refusal's cost clause is load-bearing again"},
		}},

	rpc.FrameHistory: {why: "the whole of one agent's conversation, which is the widest read on this socket and the one thing roll_up exists to avoid. The reading tools are bounded on purpose - a row per agent, a digest sized by arithmetic over a 30-agent fleet - because everything they return lands verbatim in this context and one agent's Bash heredoc is already a kilobyte. A transcript is unbounded and is *the operator's own words* as much as the agent's: what they asked for, what they pasted in, what they decided not to do. A manager that needed it would be a manager reading over somebody's shoulder to write a summary the digest already writes",
		rests: []referent{
			{kind: sourceDeclares, name: "internal/mcp/rollup.go#rollUpMaxBytes", why: "the digest is the bounded answer to the question a transcript would answer without a bound. If it goes, this refusal is leaving the manager with no way to be broadly aware at all"},
		}},

	rpc.FrameRoomHistory: {why: "the same read as FrameHistory under a different kind, and refused for the same reason rather than by analogy: the daemon answers both with History(), so what crosses the socket is byte for byte the transcript the cell above refuses - the whole of one agent's conversation, the operator's own words included. The *room's* filter that makes this a group chat rather than a log runs in internal/ui, on the client, after the reply has already been read. A manager reaching this kind would get everything FrameHistory returns and none of the filtering, so a verdict of 'allowed because the room only draws a little of it' would be a rule about a surface the daemon does not implement",
		rests: []referent{
			{kind: sourceDeclares, name: "internal/mcp/rollup.go#rollUpMaxBytes", why: "the digest is the bounded answer to the question a transcript would answer without a bound, and it is the alternative both history refusals leave in place"},
		}},

	rpc.FrameMode: {why: "it is the permission system itself, one level up. FrameAllow is refused because a manager that could answer an ask would be the fleet approving its own tool calls; a manager that could set a mode would be the fleet deciding it will not be asked at all - `dontAsk` and `acceptEdits` are both in the CLI's accepted set, and reaching either ends the human's turn in every *future* decision that session makes rather than one. That is the escalation this whole list exists to prevent, arriving by the shortest path on it.\n\nAnd it fails the test send and interrupt pass - undoable by *looking*, which the cell above notes is the property spawn already does not have. A mode is a property of a running process that appears in no row this surface returns, so a manager that loosened one would leave nothing for the operator to notice at all: not a row, not a line in the room, not a thing to park. The composer's indicator is per-agent and lives beside the conversation - a fleet of thirty has twenty-nine of them off screen.\n\nbypassPermissions is the one position it could not reach, and that is the CLI's doing rather than this table's: it is refused unless the process was launched with --dangerously-skip-permissions, which nothing in this tree passes (2026-08-12-permission-mode-findings.md §7). A floor, not a fence - it stops the worst position and none of the others",
		rests: []referent{
			{kind: recording, name: "docs/superpowers/notes/2026-08-12-permission-mode-findings.md", why: "the recording that says bypassPermissions is refused for a session not launched dangerously. If a Wake session is ever spawned with --dangerously-skip-permissions, the floor this cell leans on is gone and the refusal has to stand on the first two paragraphs alone"},
		}},

	rpc.FrameAllow:  {why: permissionsAreAHumans},
	rpc.FrameDeny:   {why: permissionsAreAHumans},
	rpc.FrameAnswer: {why: permissionsAreAHumans},
}

// permissionsAreAHumans is one reason for three kinds, which is the one place a
// shared argument is right: they are the three answers to a single question, and
// the question is the one the permission system exists to put in front of a
// person.
const permissionsAreAHumans = "answering a permission request. --permission-prompt-tool stdio makes every ask blocking precisely so a human decides, and agent_status already tells the manager an agent is 'stopped dead until a human answers it'. A manager that could allow would be the fleet approving its own tool calls, with the operator's name on them"

// notAClientVerb is every frame kind rpc declares that a client never sends,
// with the direction that is the reason. They have no verdict above because
// there is nothing for a manager to be allowed to do with them.
var notAClientVerb = map[string]string{
	rpc.FrameEvent:            "daemon to client: one session event",
	rpc.FrameHello:            "daemon to client: the handshake on connect",
	rpc.FrameError:            "daemon to client in practice - every writer of one is on the daemon's side of the socket",
	rpc.FrameStatusReply:      "daemon to client: the answer to a status request",
	rpc.FrameStatusPush:       "daemon to client: a state change nobody asked about",
	rpc.FrameHistoryReply:     "daemon to client: the conversation a session already had",
	rpc.FrameRoomHistoryReply: "daemon to client: the same conversation, answered for the room",
}

// The verbs the daemon serves are the ones dispatch names, and every one of
// them has a verdict.
//
// Derived from the **producer** - internal/daemon's dispatch switch - rather
// than from rpc's constant block, which is wider: it declares the frames going
// the other way too. Asserted in both directions, so a kind the daemon starts
// dispatching must gain a verdict and a verdict for something it no longer
// dispatches has to be moved or deleted.
func TestEveryVerbTheDaemonServesHasAVerdictForTheManager(t *testing.T) {
	declared := frameKindConstants(t)
	served := verbsTheDaemonDispatches(t)

	for name, kind := range declared {
		_, decided := managerVerbs[kind]
		why, oneWay := notAClientVerb[kind]
		switch {
		case decided && oneWay:
			t.Errorf("rpc.%s = %q both has a manager verdict and is excused as one-way (%s): one of the two is wrong", name, kind, why)
		case !decided && !oneWay:
			t.Errorf("rpc.%s = %q is a frame kind and nothing here says whether a manager may reach it. "+
				"The reader of this surface is a model with nobody watching, so an unruled verb is one that "+
				"gets inherited rather than decided", name, kind)
		case decided && !served[kind]:
			t.Errorf("rpc.%s = %q has a manager verdict and internal/daemon's dispatch does not serve it, "+
				"so the cell rules on something no client can do - which reads as coverage", name, kind)
		case oneWay && served[kind]:
			t.Errorf("rpc.%s = %q is excused here as one-way (%s) and the daemon dispatches it now, "+
				"so it needs a verdict", name, kind, why)
		}
	}
	for kind := range served {
		if _, decided := managerVerbs[kind]; !decided {
			t.Errorf("internal/daemon dispatches %q and nothing here says whether a manager may reach it", kind)
		}
	}
}

// Every cell says why, in both directions.
//
// A table of bare booleans is a table somebody can flip. The reason is what a
// reviewer reads and it is what the next person has to argue against, so an
// empty one is a decision nobody made.
func TestEveryVerdictCarriesItsArgument(t *testing.T) {
	for kind, v := range managerVerbs {
		if strings.TrimSpace(v.why) == "" {
			t.Errorf("the verdict for %q carries no reason. Allowing and refusing are both decisions about what an unsupervised model can do to somebody's fleet", kind)
		}
	}
}

// Every reason that quantifies over the rest of the build names something a test
// can look at, and every one of those things is still what the reason says.
//
// This is rung 7's own closing move applied to prose: *derive a cross-surface
// claim from the surface that owns it*. The claims cannot be derived - they are
// arguments - but their premises can be checked, and a premise that has expired
// is exactly the failure this table produced once already.
//
// Each kind fails in the direction the reason would go wrong:
//
//   - absentPath fails when the capability **ships**, which is when a "nothing
//     in this build does X" argument stops holding.
//   - recording fails when the note behind a CLI claim is gone.
//   - tuiCommand fails when the recovery verb the reason names stops being one.
//   - stateNotOffered fails when the manager's roster starts offering a row the
//     reason says it cannot see.
func TestEveryReasonThatRestsOnTheBuildNamesSomethingThatIsStillTrue(t *testing.T) {
	checked := 0
	for kind, v := range managerVerbs {
		for _, r := range v.rests {
			checked++
			switch r.kind {
			case absentPath:
				if _, err := os.Stat(filepath.Join("..", "..", r.name)); err == nil {
					t.Errorf("the verdict on %q rests on %s not existing, and it does now: %s", kind, r.name, r.why)
				}
			case sourceDeclares:
				path, ident, _ := strings.Cut(r.name, "#")
				src, err := os.ReadFile(filepath.Join("..", "..", path))
				if err != nil || !strings.Contains(string(src), ident+" = ") {
					t.Errorf("the verdict on %q rests on %s declaring %s and it does not (%v): %s", kind, path, ident, err, r.why)
				}
			case recording:
				if _, err := os.Stat(filepath.Join("..", "..", r.name)); err != nil {
					t.Errorf("the verdict on %q cites %s and it is not there (%v): %s", kind, r.name, err, r.why)
				}
			case tuiCommand:
				if !tuiCommands(t)[r.name] {
					t.Errorf("the verdict on %q rests on /%s being a TUI command and internal/ui no longer declares one: %s", kind, r.name, r.why)
				}
			case stateNotOffered:
				if managerIsOffered(t, r.name) {
					t.Errorf("the verdict on %q rests on the manager not being offered a %q session, and internal/mcp offers one now: %s", kind, r.name, r.why)
				}
			default:
				t.Errorf("the verdict on %q rests on a referent kind nothing checks", kind)
			}
		}
	}
	// The floor. A referent list nobody filled in agrees with every reason in
	// the table, which is the state this test was written to leave behind.
	if checked < len(referentFloor) {
		t.Fatalf("only %d referents were checked; the reasons that quantify over the build are %v, and a table with no referents is the one this test exists to replace", checked, referentFloor)
	}
	for _, kind := range referentFloor {
		if len(managerVerbs[kind].rests) == 0 {
			t.Errorf("the verdict on %q argues from a fact about the rest of the build and names nothing that can be checked", kind)
		}
	}
}

// referentFloor is the verbs whose reasons argue from the state of the build
// rather than from their own frame's meaning, so each must name a referent.
//
// Hand-written, and it is the one list here that is: "does this sentence
// quantify over the build" is a judgement about prose, which is exactly what
// rung 7 says cannot be derived. What it buys is that removing a cell's
// referents fails rather than passing quietly.
var referentFloor = []string{rpc.FrameWake, rpc.FramePark, rpc.FrameSpawn, rpc.FrameFork, rpc.FrameInterrupt}

// tuiCommands is the set of slash commands internal/ui declares, read off its
// own `commands` map.
func tuiCommands(t *testing.T) map[string]bool {
	t.Helper()

	f := parseFile(t, filepath.Join("..", "..", "internal", "ui", "slash.go"))
	consts := map[string]string{}
	for name, value := range stringConstants(t, filepath.Join("..", "..", "internal", "ui", "slash.go"), "") {
		consts[name] = value
	}
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "commands" || len(spec.Values) != 1 {
			return true
		}
		lit, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			switch key := kv.Key.(type) {
			case *ast.BasicLit:
				out[strings.Trim(key.Value, `"`)] = true
			case *ast.Ident:
				if value, ok := consts[key.Name]; ok {
					out[value] = true
				}
			}
		}
		return false
	})
	if len(out) == 0 {
		t.Fatal("internal/ui declares no slash commands: the scan is broken and every claim resting on it is asserting nothing")
	}
	return out
}

// managerIsOffered reports whether internal/mcp's roster offers a session in one
// state, read off the verdict table that owns that decision.
//
// `internal/mcp/stateguard_test.go`'s agentStates is the surface that owns it -
// its own totality guard holds it to both producers - so reading it here is the
// rung-7 move rather than a shortcut: this file makes a claim about that filter
// and now reads the thing that decides it.
func managerIsOffered(t *testing.T, state string) bool {
	t.Helper()

	f := parseFile(t, filepath.Join("..", "..", "internal", "mcp", "stateguard_test.go"))
	states := sessionStateConstants(t)
	found, offered := false, false
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "agentStates" || len(spec.Values) != 1 {
			return true
		}
		lit, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.SelectorExpr)
			if !ok || states[key.Sel.Name] != state {
				continue
			}
			value, ok := kv.Value.(*ast.Ident)
			if !ok {
				continue
			}
			found, offered = true, value.Name == "true"
		}
		return false
	})
	if !found {
		t.Fatalf("internal/mcp's agentStates has no verdict for %q, so this check is reading nothing", state)
	}
	return offered
}

// And the code agrees with the table: the frames `wake mcp` can put on the wire
// are exactly the permitted ones.
//
// # Why the unit is the path and not one function
//
// The property is about what reaches the socket, and socketFleet.act is not the
// only thing that could reach it. A verb moved into a helper, or written from a
// method the interface calls rather than from serveMCP, is invisible to a check
// pointed at one function - which is rung 5, won here the cheap way because
// internal/core/argvguard_test.go already paid for the lesson.
//
// The seed is serveMCP plus every method of socketFleet, because mcp.Serve is
// what calls those methods and it is in another package: a walk from serveMCP
// alone would follow no call at all and report an empty set, which is the shape
// of a guard that cannot fail.
func TestTheFramesWakeMCPCanWriteAreExactlyTheOnesTheManagerIsAllowed(t *testing.T) {
	written := framesWrittenOnTheMCPPath(t)

	for kind := range written {
		v, decided := managerVerbs[kind]
		switch {
		case !decided:
			t.Errorf("`wake mcp` writes %q, which has no verdict in managerVerbs", kind)
		case !v.allowed:
			t.Errorf("`wake mcp` writes %q, which the manager is refused: %s", kind, v.why)
		}
	}
	for kind, v := range managerVerbs {
		if v.allowed && !written[kind] {
			t.Errorf("managerVerbs allows %q and nothing on the MCP path writes it. Either the capability was "+
				"removed - in which case say so here rather than leaving a permission for it - or the scan is "+
				"broken and every other assertion in this test is passing over nothing", kind)
		}
	}
	// The floor, because a scan that finds nothing agrees with every mutation
	// of the code it is scanning.
	for _, kind := range []string{rpc.FrameSend, rpc.FrameInterrupt} {
		if !written[kind] {
			t.Fatalf("the MCP path writes no %q frame: the acting half is gone or this scan no longer reaches it (found %v)", kind, sortedKeys(written))
		}
	}
}

// The verb is wired, and it is wired to the server rather than to something
// else this package could plausibly dispatch.
//
// Static because the alternative is running it: `wake mcp` reads stdin until it
// is closed, so a test that called run() would be asserting on whatever the test
// binary's stdin happens to be.
func TestTheMCPVerbReachesTheServer(t *testing.T) {
	if !callsFunctionNamed(funcDeclIn(t, "main.go", "run"), "serveMCP") {
		t.Error("`run` never calls serveMCP: the verb is accepted by the dispatch and does nothing, which is a manager whose tools all fail on the first call with no reason on any surface")
	}
	// And an unknown verb is still an unknown verb, so adding this one did not
	// turn a typo into a server.
	if err := run([]string{"mcpp"}, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("`wake mcpp` returned %v, want an unknown-command error", err)
	}
}

// --- the derived domains ------------------------------------------------------

// frameKindConstants reads every `Frame… = "…"` constant rpc declares.
//
// Globbed rather than pointed at wire.go, because the ending verbs live in
// lifecycle.go and a kind added to a third file would otherwise leave the scan
// blind while the count still looked right.
func frameKindConstants(t *testing.T) map[string]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "rpc", "*.go"))
	if err != nil {
		t.Fatalf("glob the rpc package: %v", err)
	}
	out := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		for name, value := range stringConstants(t, file, "Frame") {
			out[name] = value
		}
	}
	if len(out) == 0 {
		t.Fatalf("no Frame… constants found across %v: the scan is broken and the test over it is asserting nothing", files)
	}
	return out
}

// verbsTheDaemonDispatches is every frame kind internal/daemon's dispatch switch
// names - the set a client can actually ask for.
func verbsTheDaemonDispatches(t *testing.T) map[string]bool {
	t.Helper()

	byName := frameKindConstants(t)
	out := map[string]bool{}
	fn := funcDeclIn(t, filepath.Join("..", "..", "internal", "daemon", "server.go"), "dispatch")
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if kind, ok := rpcConstant(n, byName); ok {
			out[kind] = true
		}
		return true
	})
	if len(out) == 0 {
		t.Fatal("the daemon's dispatch names no rpc.Frame… constant: the scan is broken, and every claim resting on it is asserting nothing")
	}
	return out
}

// rpcConstant resolves a node that is `rpc.Something` into the string that
// constant holds.
func rpcConstant(n ast.Node, byName map[string]string) (string, bool) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "rpc" {
		return "", false
	}
	value, declared := byName[sel.Sel.Name]
	return value, declared
}

// framesWrittenOnTheMCPPath is every frame kind constructed by a function the
// MCP server can reach in this package.
//
// Constructed rather than merely named, which is the distinction that makes the
// scan mean anything: awaitTaken *reads* rpc.FrameError and rpc.FrameStatusReply
// off the wire, and a check that counted every mention would have to excuse
// those - and would then be excusing the shape a written frame takes.
func framesWrittenOnTheMCPPath(t *testing.T) map[string]bool {
	t.Helper()

	byName := frameKindConstants(t)
	out := map[string]bool{}
	for _, fn := range mcpPath(t) {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				requireNoAssignedKind(t, fn, as)
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isRPCFrame(lit.Type) {
				return true
			}
			kind, ok := frameKindOf(lit, byName)
			if !ok {
				t.Errorf("%s builds an rpc.Frame whose Kind is not an rpc constant, so nothing here can tell which verb it is", fn.Name.Name)
				return true
			}
			out[kind] = true
			return true
		})
	}
	return out
}

// requireNoAssignedKind refuses the one shape this scan cannot read.
//
// The scan above sees `rpc.Frame{Kind: rpc.FrameX}` composite literals, which is
// what makes "the frames this path can write" answerable at all. It is blind to
// the other shape: `var f rpc.Frame; f.Kind = rpc.FrameStop` writes a refused
// verb and constructs no literal for anything to look at.
//
// So the *form* is constrained rather than only the contents - rung 5 again. A
// Kind that arrives by assignment is a build failure here, which costs nothing
// today (both frames are literals at the point they are named) and closes the
// one way past this table that does not involve editing it.
//
// Assignment rather than "the argument must be a literal", because `act` takes
// the frame as a parameter on purpose - the two verbs differ in the frame and in
// nothing else, and forcing the write inline would mean two copies of the
// confirmation exchange. What has to be pinned is where a Kind can come from.
func requireNoAssignedKind(t *testing.T, fn *ast.FuncDecl, as *ast.AssignStmt) {
	t.Helper()

	for _, lhs := range as.Lhs {
		sel, ok := lhs.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != frameKindField {
			continue
		}
		t.Errorf("%s assigns a frame's %s rather than naming it in a literal, so the scan over which verbs this path can reach cannot see which one it is. That is exactly how a refused verb gets past this table",
			fn.Name.Name, frameKindField)
	}
}

// frameKindField is the field that decides what a frame *is*.
const frameKindField = "Kind"

func isRPCFrame(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Frame" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "rpc"
}

func frameKindOf(lit *ast.CompositeLit, byName map[string]string) (string, bool) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Kind" {
			continue
		}
		return rpcConstant(kv.Value, byName)
	}
	return "", false
}

// mcpPath is every function of this package the MCP server can reach, found by
// following calls from the two entry points rather than by listing them.
//
// Two entry points because there are two ways in: `wake mcp` runs serveMCP, and
// mcp.Serve calls socketFleet's methods from the other side of an interface. A
// walk from serveMCP alone follows nothing - it hands the fleet to another
// package and returns - so the methods have to be seeded, and they are seeded
// from the interface mcp declares rather than from a list here.
func mcpPath(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()

	declared := packageFuncsByName(t)
	seed := []string{mcpEntrypoint}
	for _, method := range fleetMethods(t) {
		if _, ok := declared[method]; !ok {
			t.Fatalf("mcp.Fleet declares %s and this package has no such function: the seed cannot reach the method the interface calls", method)
		}
		seed = append(seed, method)
	}

	found := map[string]*ast.FuncDecl{}
	for len(seed) > 0 {
		name := seed[len(seed)-1]
		seed = seed[:len(seed)-1]
		fn, ok := declared[name]
		if !ok || found[name] != nil {
			continue
		}
		found[name] = fn
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if callee, ok := calleeName(call); ok {
					seed = append(seed, callee)
				}
			}
			return true
		})
	}
	if len(found) <= len(fleetMethods(t)) {
		t.Fatalf("the MCP path is %v, which is the seed and nothing it calls: the walk is broken", sortedKeys(found))
	}
	return found
}

// mcpEntrypoint is what `wake mcp` runs.
const mcpEntrypoint = "serveMCP"

// calleeName is the name a call site names, for the resolution this scan can do
// without types: a bare identifier, or the selector's own name.
//
// A selector whose name is not a function this package declares is another
// package's - daemon.Status, rpc.WriteFrameTo - and falls out of the walk by not
// matching the index. That is an over-approximation in the safe direction: a
// local method and a foreign function with the same name would both be followed,
// which can only add functions to the path.
func calleeName(call *ast.CallExpr) (string, bool) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name, true
	case *ast.SelectorExpr:
		return fn.Sel.Name, true
	}
	return "", false
}

// packageFuncsByName indexes this package's non-test declarations by name.
// Methods and plain functions share one index, which is what makes the call
// walk above resolvable without type information.
func packageFuncsByName(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()

	out := map[string]*ast.FuncDecl{}
	for _, file := range packageFiles(t, ".") {
		for _, decl := range parseFile(t, file).Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			out[fn.Name.Name] = fn
		}
	}
	if len(out) == 0 {
		t.Fatal("this package declares no functions: the index is broken")
	}
	return out
}

// fleetMethods is the method set mcp.Fleet declares - the whole of what this
// package implements for a manager, read off the interface rather than off
// socketFleet, so a method added there without a verdict is not simply inherited
// into the path.
func fleetMethods(t *testing.T) []string {
	t.Helper()

	f := parseFile(t, filepath.Join("..", "..", "internal", "mcp", "fleet.go"))
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "Fleet" {
			return true
		}
		iface, ok := spec.Type.(*ast.InterfaceType)
		if !ok {
			return true
		}
		for _, m := range iface.Methods.List {
			for _, name := range m.Names {
				out = append(out, name.Name)
			}
		}
		return false
	})
	if len(out) == 0 {
		t.Fatal("mcp.Fleet declares no methods: the interface was renamed and this seed is empty")
	}
	slices.Sort(out)
	return out
}

// packageFiles is the non-test .go files in a directory.
func packageFiles(t *testing.T, dir string) []string {
	t.Helper()

	all, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	out := make([]string, 0, len(all))
	for _, file := range all {
		if !strings.HasSuffix(file, "_test.go") {
			out = append(out, file)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no non-test files in %s: the walk is broken", dir)
	}
	return out
}

// sortedKeys is the keys of a map, for a failure message that names what was
// found. keysOf in forkguard_test.go is the same thing over map[string]bool
// alone; this one is generic because the two callers hold different values.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

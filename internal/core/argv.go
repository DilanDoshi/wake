// How a session names itself on the command line, and nothing else.
//
// Split out of session.go, which owns process lifecycle. The three functions
// here are called from exactly one place (Start) and share no state with the
// pump, the write path or the ending path, which is what makes the cut clean.
//
// # Why this file exists rather than a section of session.go
//
// Two reasons and the second is the one that lasts. session.go reached 767 of
// this project's 800-line hard max, and park/wake's arm in identityArgs
// crosses it. And the CLI flags below are the *second* airlock leak -
// airlock_test.go polices Claude's JSON vocabulary and says in its own SCOPE
// paragraph that session.go's flags are a leak of a different shape wanting
// its own ruling. This is that ruling: they are spelled here and nowhere else
// in the tree, checked by TestTheIdentityFlagsAreSpelledOnlyInArgv, and
// anything outside internal/core that needs to recognise one asks
// SessionArgvMarkers.

package core

import (
	"fmt"

	"github.com/google/uuid"
)

// identityArgs is how this session names itself on the command line, and it is
// a closed choice rather than three independent appends.
//
// The shape is what matters, more than the flags do. Recorded against 2.1.226:
//
//	--resume <id> --session-id <new>   refused at startup, exit 1, and with
//	                                   **nothing on stdout**, so the only
//	                                   diagnosis is the stderr tail
//	                                   (resume-session-id-without-fork.stderr.txt)
//	--fork-session with no --resume    accepted and silently ignored: exit 0,
//	                                   empty stderr, SessionStart:startup, and
//	                                   an ordinary empty session under the id
//	                                   that was meant to *receive* the fork
//	                                   (fork-session-no-resume.jsonl)
//
// Both of those are what a flag-per-if produces the first time somebody edits
// one branch, and the second one does not fail loudly - it produces a
// plausible-looking empty agent. Returning the whole identity block from one
// switch makes them unrepresentable rather than merely untaken: there is no
// statement here that can append --resume alone, and none that can append
// --fork-session alone.
//
// The order inside the fork case is the recorded order and is not to be
// permuted. Nothing has recorded a different one.
//
// Park and wake are the third case, and it is a bare `--resume <id>` with
// **no** --session-id. The flag reuses the id it is given, so supplying one as
// well is the first of the two punished shapes above, and it is the natural
// edit: cfg.SessionID is still set on a resumed session, because attribute
// needs it for the permission ask that carries no session id of its own. The
// arm suppresses the *flag*, not the field.
//
// The fourth arm is the refusal at the top. Setting ForkFrom and ResumeFrom
// together is not an argv shape at all - it is two different verbs - and
// letting one silently win would make a dropped ResumeFrom look like a
// successful fork, which is the same class of failure as a dropped --resume
// producing a plausible-looking empty agent.
//
// **The domain of this switch is Config's shape space, not today's callers'
// image of it.** That is deliberate and it is why the fork arm already refuses
// a fork with no id, which daemon.fork can never produce: the punished shapes
// have to be *unrepresentable*, which is a statement about the type. Rung 4 of
// docs/notes/decisions.md ("derive the domain from the producer") governs a
// guard over a *verdict* somebody consumes; this is a constructor whose whole
// job is that a wrong Config cannot be built into a wrong argv.
//
// What this switch does **not** own is whether an id is well-formed. It refuses
// the two id *relationships* that produce an unrecorded argv - a fork with no
// id, and a fork onto the parent's own id - and passes everything else through
// as given. `Config{ForkFrom: " "}` builds the right shape around a resume
// target that names nothing, and that is deliberate: the id space is
// daemon.mintedByWake's to police, because it is the layer that knows which ids
// this fleet ever issued. See sameSession for the one place that distinction
// bites here.
func (s *Session) identityArgs() ([]string, error) {
	switch {
	case s.cfg.ForkFrom != "" && s.cfg.ResumeFrom != "":
		return nil, fmt.Errorf("session %s cannot both fork %s and resume %s: a fork mints a new id and lands in its own transcript, a resume reuses one and appends to that one",
			s.cfg.SessionID, s.cfg.ForkFrom, s.cfg.ResumeFrom)
	case s.cfg.ForkFrom != "":
		if s.cfg.SessionID == "" {
			return nil, fmt.Errorf("a fork of session %s needs a Wake-assigned id of its own", s.cfg.ForkFrom)
		}
		if sameSession(s.cfg.SessionID, s.cfg.ForkFrom) {
			return nil, fmt.Errorf("session %s cannot be forked onto its own id (as %s)", s.cfg.ForkFrom, s.cfg.SessionID)
		}
		return []string{"--resume", s.cfg.ForkFrom, "--fork-session", "--session-id", s.cfg.SessionID}, nil
	case s.cfg.ResumeFrom != "":
		if s.cfg.SessionID != "" && !sameSession(s.cfg.SessionID, s.cfg.ResumeFrom) {
			return nil, fmt.Errorf("session %s cannot be resumed under a different id (%s): --resume reuses the id it is given, and passing both is refused at startup with nothing on stdout",
				s.cfg.ResumeFrom, s.cfg.SessionID)
		}
		return []string{"--resume", s.cfg.ResumeFrom}, nil
	case s.cfg.SessionID != "":
		return []string{"--session-id", s.cfg.SessionID}, nil
	default:
		return nil, nil
	}
}

// sameSession reports whether two ids name the same session, which is a
// different question from whether they are the same string.
//
// Wake's id space is uuid.Parse's, because that is what decides an id came from
// Wake at all: daemon.mintedByWake is a uuid.Parse call. And uuid.Parse reads
// six spellings as one UUID - canonical, uppercase, braced, urn:uuid:, and the
// 32-char undashed form in either case. So a parent recorded canonically and a
// child handed in uppercase are the same session, pass every other check, and
// build `--resume <x> --fork-session --session-id <X>`: two live processes
// under one id, and on a case-insensitive filesystem one <uuid>.jsonl between
// them. Nothing has recorded what the CLI does with that, which is the
// condition for refusing rather than finding out.
//
// A string that is not a UUID is compared as a string and nothing more. This
// function decides identity, not validity - identityArgs says who owns
// validity, and it is not this package.
func sameSession(a, b string) bool {
	if a == b {
		return true
	}
	ua, err := uuid.Parse(a)
	if err != nil {
		return false
	}
	ub, err := uuid.Parse(b)
	if err != nil {
		return false
	}
	return ua == ub
}

func (s *Session) buildArgs() ([]string, error) {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		// Without --verbose, stream-json exits 1 with an error on stderr.
		"--verbose",
		// Without this, every permission ask is auto-denied and reported
		// after the fact; the request itself never reaches us. The flag is
		// undocumented - it is absent from `claude --help`.
		"--permission-prompt-tool", "stdio",
		"--brief",
		"--include-hook-events",
		"--forward-subagent-text",
		// The token stream, which is what makes a pane show an answer being
		// written rather than arriving. It multiplies this session's frame rate
		// by roughly its output token rate - the corpus's median is 43.5 a
		// second - so it is affordable only because nothing above renders one:
		// see KindPartialText and internal/ui/partial.go.
		//
		// Refused by the CLI without --print and --output-format=stream-json,
		// both of which are above it in this same literal.
		"--include-partial-messages",
		// Puts a peer's cross-session message on the live stream so the room
		// sees it as it arrives; without it that message reaches only the
		// on-disk transcript. It also replays Wake's own sends, which carry
		// isReplay and stay dropped as Echoed - see KindCrossSession and the
		// room fold. Recorded 2026-08-31.
		"--replay-user-messages",
	}
	identity, err := s.identityArgs()
	if err != nil {
		return nil, err
	}
	args = append(args, identity...)
	if s.cfg.Name != "" {
		args = append(args, "--name", s.cfg.Name)
	}
	if s.cfg.PermissionMode != "" {
		args = append(args, "--permission-mode", s.cfg.PermissionMode)
	}
	if s.cfg.Model != "" {
		args = append(args, "--model", s.cfg.Model)
	}
	if s.cfg.Effort != "" {
		args = append(args, "--effort", s.cfg.Effort)
	}
	// Both are documented "only works with --print", which is the first flag in
	// this same literal. Verified present in claude 2.1.233:
	// `--max-budget-usd <amount>` and `--fallback-model <model>`, the latter
	// comma-separated and tried in order.
	//
	// Emitted on an emptiness test and nothing else, which is the shape
	// argvguard_test.go's grammar permits: whether a value is legal is
	// ValidBudget's and ValidFallbackModel's question, asked at the CLI and
	// again at the daemon, and never on the argv path.
	if s.cfg.MaxBudgetUSD != "" {
		args = append(args, "--max-budget-usd", s.cfg.MaxBudgetUSD)
	}
	if s.cfg.FallbackModel != "" {
		args = append(args, "--fallback-model", s.cfg.FallbackModel)
	}
	// One flag per directory, though claude's own `--add-dir <directories...>`
	// is variadic and would take them all after one. **Both spellings were
	// recorded** (2026-08-16, 2.1.233, two directories outside the session's
	// own tree: identical in the debug log either way) and the repeated one is
	// emitted because it needs no decision at all - the variadic form has to
	// ask whether this is the first directory, and a question about a Config
	// field's *value* on this path is what argvguard_test.go exists to refuse.
	// The range is the emptiness test: no directories emit no flag.
	for _, dir := range s.cfg.AddDir {
		args = append(args, "--add-dir", dir)
	}
	// `--debug-file` implicitly enables debug mode, so it is the flag that
	// turns logging on for one agent; `--debug` only narrows the categories.
	// Emitted on an emptiness test each, which is the shape argvguard_test.go
	// permits: whether a filter is legal is ValidDebugFilter's question, and
	// whether a file may be written where it says is the daemon's - both asked
	// before anything reaches here. Verified present in claude 2.1.233.
	if s.cfg.DebugFile != "" {
		args = append(args, "--debug-file", s.cfg.DebugFile)
	}
	if s.cfg.Debug != "" {
		args = append(args, "--debug", s.cfg.Debug)
	}
	// One append, and the two flags are one literal inside it. --mcp-config
	// *adds* servers to whatever the machine already has configured, so without
	// --strict-mcp-config the manager inherits every MCP server in the user's
	// own configuration - and that failure is accepted, exit 0, empty stderr,
	// and a session that looks right. It is the identity block's shape for the
	// identity block's reason: a statement that could drop half of it is what
	// there must not be. See Config.MCPConfig, and
	// TestTheMCPFlagsAreEmittedFromOneAppendOrNotAtAll, which is what holds it.
	//
	// `--tools ""` is the third member of that literal and bounds the *built-in*
	// set: measured at 2.1.228, a session spawned with it reports exactly its
	// MCP tools in `init.tools` and zero built-ins, and says so when asked to
	// write a file. MCP tools pass through the flag untouched, named or not, so
	// the manager keeps everything internal/mcp gives it and holds no Bash, no
	// Write and no Edit.
	//
	// It is `--tools` and not `--allowed-tools`: that flag bounds nothing at
	// all, and in `auto` it does nothing whatever - recorded in
	// docs/superpowers/notes/2026-08-12-tool-bounding-findings.md §3, which is
	// where this project's own plan for it was struck.
	//
	// Verified present in claude 2.1.228: `--mcp-config <configs...>` (variadic
	// - it takes more than one path, and Wake passes exactly one),
	// `--strict-mcp-config` and `--tools <tools...>`.
	if s.cfg.MCPConfig != "" {
		args = append(args, "--mcp-config", s.cfg.MCPConfig, "--strict-mcp-config", "--tools", "")
	}
	// Verified present in claude 2.1.228: `--append-system-prompt <prompt>`.
	// Append and not --system-prompt, which replaces claude's own.
	if s.cfg.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", s.cfg.AppendSystemPrompt)
	}
	return args, nil
}

// SessionArgvMarkers are the argv fragments that say a live process is running
// one session, for whoever has to answer "is anything holding this id".
//
// Exported because the answer is the daemon's to want and the flags are this
// file's to spell. It matters that this is a *flag* and its value rather than
// the id alone: `wake attach <uuid>` carries a bare id in its own argv, and a
// client that had attached to a session by id would otherwise look like a
// second process holding it - a false positive on exactly the path where the
// operator has just parked the thing they were attached to.
//
// Both spellings are here because both mean a live claude on that transcript.
// --resume covers two cases and the second is deliberate: a fork in flight
// carries `--resume <parent>` in its argv, so a parent whose fork is still
// starting reads as held. That is the right answer - forking a session that
// another process is also --resume-ing is 2026-08-10 findings §12's "nastiest
// composition available" and is unrecorded.
//
// --fork-session is not here. It carries no id, so it cannot name a session.
func SessionArgvMarkers(sessionID string) []string {
	if sessionID == "" {
		return nil
	}
	return []string{"--session-id " + sessionID, "--resume " + sessionID}
}

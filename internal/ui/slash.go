package ui

// Slash commands: the ones Wake owns, and everything else, which is a message.
//
// # Why this is a layer and not a special case for /resume
//
// Because three more were already asked for. docs/goals.md §3 named
// `/new agent in <dir>` and `/add-<agent-name>` as the largest single gap
// between the founding message and this build - *Wake can manage agents but
// cannot create or name them from inside itself* - and each needs exactly this:
// a name, an argument, a target resolved out of the fleet, and a frame. Adding
// one is a line in commands below, and `/new`, `/name` and `/task` each cost
// exactly that.
//
// # `/add-<agent-name>` is refused as spelled, and the reason is evidence
//
// It is the one founding verb this layer does not answer. The router keys on
// the exact first word, so `/add-sydney` gives `word = "add-sydney"` and falls
// through as a message - correctly, because that is the passthrough rule
// working. The only shapes that would reach it are a prefix rule and a second
// lookup, and slashguard_test.go makes both a build failure on purpose: they
// are the shape of every mutant that swallows an operator's own commands.
//
// Three arguments, and the first is the one that settles it.
//
//  1. **A `<verb>-<suffix>` rule claims an operator's whole command set, not
//     one built-in.** deferred.md named the near-miss as *"claude ships
//     `/add-dir`"* - which this build cannot check, and the recordings do not
//     carry it. What they do carry is worse: of the 133 commands claude
//     advertised on the `init` frames in testdata/stream, most are hyphenated
//     and **eight begin `new-`** - `/new-oscar`, `/new-victor`, `/new-sierra` and
//     the rest, which are an operator's own rather than claude's. So the
//     argument does not
//     rest on a built-in anybody has to take on trust; the rule that would reach
//     `add-sydney` is the rule that eats eight commands we have a recording of.
//  2. **It is not decidable from the draft.** `/add-dir` and `/add-sydney` are
//     the same shape, and telling them apart means asking the fleet - so the
//     set of drafts Wake claims would change between keystrokes, and an
//     operator would lose whichever `/add-…` they own for exactly as long as
//     some agent happened to share its suffix. The rule is *resolve against a
//     closed set Wake owns*, and the fleet is closed but not **stable**; a claim that
//     changes under the operator is the least debuggable version of the failure
//     this fence exists for.
//  3. **There is nothing for it to do.** The verb means *"add a live agent to
//     the groupchat"*, and in this build every live agent is already in it -
//     Fleet.Observe folds every session's events and App.apply filters none of
//     them. It becomes a real verb the day a group has membership, which is
//     internal/ui/groups.go's work, and it can be spelled then by whoever knows
//     what it does.
//
// So the founding spelling is recorded as refused rather than approximated, and
// `/add <name>` is **not** shipped in its place: a command that does nothing is
// the lying feature the legend rule exists to prevent. docs/notes/deferred.md
// carries the entry.
//
// # The overload, which is the whole difficulty
//
// `@` is a session name **or** a file path, resolved live-name-first. `/` is
// worse: **claude's own slash commands have to keep working.** `/model`,
// `/clear`, `/compact` and `/context` all survive stream-json mode
// (2026-08-08 stream-json findings), so a router that swallowed anything
// starting with `/` would take four working features away - and there is no
// list of the rest to defend against, because a user's own
// `.claude/commands/*.md` are slash commands too, and **nothing can enumerate
// them at the moment the question is asked**.
//
// That sentence used to read *"nothing on this side of the socket can enumerate
// them"*, and this branch's own corpus falsified it: claude announces its whole
// command set on the `init` frame as `slash_commands`, an operator's own
// `~/.claude/commands/*.md` included - which is why the recorded corpus carries
// `new-papa` and its seven siblings. The airlock **decodes** that key as of
// 2026-08-15 (core.SessionFacts.SlashCommands); it used to drop it.
//
// **The rule does not change, and now it has to be said rather than implied.**
// The list arrives **per session and after the first frame**, and this decision
// is per keystroke and belongs to a room that may hold thirty agents with
// different command sets and none at all before its first spawn. A router
// consulting it would claim a different set of drafts per agent and per second -
// which is argument 2 for refusing `/add-<agent-name>`, arriving one door over.
// So the list is read by internal/ui/completion.go, which **offers** and never
// routes, and by nothing else. What the frame could still support is a **derived
// guard** rather than a hand-written passthrough list; docs/notes/deferred.md
// carries that with the evidence.
//
// So the rule is the one `@` uses, read the same way round: **resolve against a
// closed set Wake owns, and anything else is text.** A name Wake does not know
// is not an error - it is a message, passed through byte for byte. That also
// means Wake's set has to stay small and has to avoid claude's, which `/resume`
// does by the one route that is not a coincidence: the findings note records
// that `/resume` is the one command that does **not** survive stream-json mode,
// so passing it through does nothing at all today.
//
// The set being closed is the property, and it is asserted statically in
// slashguard_test.go rather than sampled: "does this string start a Wake
// command" is a question about a value space, and no list of examples closes
// one. A router that answered it with a hand-written list of claude's commands
// would pass every example in slash_test.go and swallow every command the
// operator wrote themselves.
//
// # Where it sits in submit
//
// Directly after the bang, and for the bang's reason: `!ls` and `/resume` are
// both addressed to Wake rather than to an agent, and a session that has ended
// - or is parked - is still one somebody wants to type either into. It is
// below the bang rather than above it only because `!` has a documented
// ordering and moving it would change that for no gain.

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// SlashPrefix is what a command starts with, spelled once.
//
// Exported for a reader rather than for a caller, and the distinction is not
// pedantry: `TestNothingButTheRouterKnowsWhatASlashMeans` fails the build on
// this identifier appearing in any other non-test file in this package, so the
// composer hint the first draft of this comment promised it to would be a build
// failure. Nothing outside the package references it either. The guard is the
// valuable half, so the comment gave — a surface that wants to *show* the prefix
// takes a rendered sentence from here rather than the constant, which is what
// noResumeTarget already does.
const SlashPrefix = "/"

// mcpCommand is **the one word here that is claude's**, and taking it is an
// exception to this file's own rule rather than an oversight.
//
// The rule exists because a word Wake takes is a word an operator can no longer
// reach. What buys this one is that the thing being taken over is not a working
// feature: `/mcp` in a headless session answers with a count and *"Use `/mcp` in
// the terminal for details"* - it is a redirection, and Wake is not a terminal
// claude can open a picker in. The real panel's data is reachable from a shell
// (`claude mcp list`, which health-checks every server), so Wake can draw the
// screen the redirection points at.
//
// It is the only such word, and the bar for a second one is this same paragraph
// written again with evidence: a recording showing the passthrough does not work,
// and a shell route that does. See internal/ui/mcppanel.go.
const mcpCommand = "mcp"

// resumeCommand is the first citizen. Named as a constant because two places
// need it - the router and the sentence the room says about a parked fleet -
// and a second spelling is how the offer starts naming a command that is not
// there.
const resumeCommand = "resume"

// resumeVerb is `/resume` as somebody types it, and it exists so that a
// surface which needs to *show* the command does not have to spell the prefix.
//
// That is this file's own ruling read forwards: `TestNothingButTheRouterKnowsWhatASlashMeans`
// fails the build on `SlashPrefix` - or on a bare `"/"` - appearing in any
// other non-test file in this package, because a second place that knows what a
// leading slash means is a second place that can swallow one of claude's
// commands. A rendered sentence carries no such knowledge: it is a string to
// print. noResumeTarget was already built this way; ⌃C's confirmation and the
// advice a parked DM gives are the callers that made it a named constant.
const resumeVerb = SlashPrefix + resumeCommand

// resumeAll is /resume's one keyword. A word rather than a flag, for
// core.BroadcastName's reason read across: `@all` is typed and understood, and
// a second syntax for "everything" in a build that already has one is a second
// thing to learn.
const resumeAll = "all"

// newCommand is the founding message's own verb: *"a main pane … where you can
// `/new agent(session) in <project dir or by default at root dir>`"*.
//
// **The word is safe to take, and that is now checked against a recording
// rather than against a list of five.** `slash_commands` on claude's `init`
// frame is what it advertises, and the corpus in testdata/stream carries 133 of
// them across the machines these were recorded on. `new` is in none of them —
// eight `new-…` commands are, which is a different word and is the evidence
// behind refusing `/add-<agent-name>` (see the header of new.go's neighbour,
// and TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising).
const newCommand = "new"

// newVerb is `/new` as somebody types it, for resumeVerb's reason: a surface
// that shows a command takes a rendered spelling, so that "what a leading slash
// means" stays in the router.
const newVerb = SlashPrefix + newCommand

// newUsage is every refusal `/new` can produce out of what was typed, and it is
// one sentence for all of them.
//
// Each of those failures is the operator having typed a shape the parser does
// not read, and the useful answer to all of them is the same: what the shapes
// are. A sentence per malformation is three refusals that each say less than
// this one does — and this is the surface where a refusal is read at the moment
// somebody has already failed to do what they wanted.
//
// It lives here rather than in new.go because it *spells the command*, which is
// this file's own ruling: the prefix is known in one place, and everywhere else
// takes a rendered sentence.
// Bracket notation rather than the four spellings enumerated: with six flags
// the enumeration no longer fits the one-line notice row at 200 columns, and a
// usage the operator cannot read to its end teaches nothing. Same shapes, one
// line.
const newUsage = newVerb + " [name] [" + dirKeyword + " <dir>]" +
	"; flags: " + worktreeFlag + " <name> · " + addDirFlag + " <dir> (repeatable) · " +
	debugFileFlag + " <name> · " + debugFlag + " <categories> · " +
	budgetNewFlag + " <usd> · " + fallbackNewFlag + " <m,m>"

// nameCommand and taskCommand are the two halves of the founding message's
// *"you can either rename or assign a 'task' so they are called like
// `sydney <> dev-5748` or `alex <> ui fixes`"*.
//
// **Neither word is `rename`, and that is a finding rather than a preference.**
// The obvious verb is `/rename`, and `rename` is in the recorded corpus of what
// claude advertises on its `init` frame - so taking it would have replaced a
// working command with a refusal on the machine these recordings came from, and
// the hand-written list of five would have said nothing. `name` and `task` are
// in neither list. See TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising.
const (
	nameCommand = "name"
	taskCommand = "task"
)

// managerCommand is the switch for the one session that has tools over the
// fleet: on when it is off, off when it is on.
//
// **The word is safe to take on the same evidence `new` is.** The recorded
// corpus in testdata/stream carries 133 commands claude advertises on its `init`
// frames and `manager` is in none of them - which is what
// TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising checks, so this
// sentence is derived rather than trusted. It is not `mcp` either: that word is
// already taken here for a per-directory server health check, which is a
// different subject from the fleet's manager.
//
// **It is a command rather than a key, and that is a ruling rather than a
// leftover.** With the manager started by default there is nothing to press in
// the ordinary case, which makes this the rarest verb in the build - and the
// legend is a bijection with App.key, so a chord would spend a legend slot on
// the entry an 80-column pane loses first. Every remaining ctrl byte is worse
// than that: ⌃M and ⌃I are Enter and Tab, ⌃J is the composer's newline, ⌃S and
// ⌃Q are XOFF and XON - and ⌃Q is already bound to *park the fleet and quit*, so
// a ⌃S that some layer between the keyboard and Wake still treats as flow
// control freezes the screen and sends the operator's reflex straight into it.
// docs/notes/decisions.md carries the measurement.
const managerCommand = "manager"

// boardCommand opens the fleet overview - spec §8's "one row per agent", a
// command rather than the spec's ⌃B because that byte is spent on stacking a
// pane below. See board.go for the whole argument.
const boardCommand = "board"

// boardVerb is `/board` as somebody types it, for managerVerb's reason.
const boardVerb = SlashPrefix + boardCommand

// managerVerb is `/manager` as somebody types it, for resumeVerb's reason: the
// sentences that offer it are built from this rather than spelled, so a refusal
// cannot name a command that is not in the map below.
const managerVerb = SlashPrefix + managerCommand

// managerStopCommand ends the manager, where `/manager` only parks it.
//
// **A separate word rather than an argument**, because managerTakesNoArgument
// one door up refuses `/manager off` on the grounds that a toggle which fired
// anyway would do the opposite of what was typed half the time. The answer to
// that is a verb of its own, and `stop` is the word this project already spends
// on the ending nothing comes back from - `wake stop`, rpc.FrameStop.
//
// **The hyphen is safe here and is not the rule `/add-<agent-name>` refuses.**
// That refusal is about a *prefix* — `/add-sydney` is undecidable from the draft
// and claiming the family eats eight commands the corpus records. This claims
// one exact word, which the router matches exactly, and the corpus advertises no
// `manager-stop`: see TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising.
const managerStopCommand = "manager-stop"

// managerStopVerb is `/manager-stop` as somebody types it, for managerVerb's
// reason.
const managerStopVerb = SlashPrefix + managerStopCommand

const (
	nameVerb = SlashPrefix + nameCommand
	taskVerb = SlashPrefix + taskCommand

	// nameUsage and taskUsage are what each verb takes. Both spell the `@`,
	// because the marker is what makes the grammar unambiguous rather than
	// positional, and an operator who guessed the positional form would rename
	// the conversation they are in to somebody else's handle.
	nameUsage = nameVerb + " <new-name>, or " + nameVerb + " " + agentPrefix + "<who> <new-name>"
	taskUsage = taskVerb + " <what it is working on>, or " + taskVerb + " " + agentPrefix + "<who> <what it is working on>"

	// noNameTarget and noTaskTarget are each verb in the room with no handle in
	// front of it. They refuse rather than guessing for noResumeTarget's reason
	// and one sharper: renaming whichever agent the roster cursor rests on
	// changes where the operator's next `@` goes, and a name that resolves is
	// never reported.
	noNameTarget = "which one? " + nameUsage
	noTaskTarget = "which one? " + taskUsage
)

// commands is every slash command Wake owns. **Closed on purpose**: anything
// not here is a message, which is what keeps claude's own commands working.
//
// A map from the bare word to what it does, so adding /new is one entry rather
// than an arm in a switch somebody has to find.
var commands = map[string]func(App, string) (App, tea.Cmd){
	resumeCommand:      App.resume,
	newCommand:         App.newAgent,
	nameCommand:        App.renameAgent,
	taskCommand:        App.labelAgent,
	adoptCommand:       App.adopt,
	mcpCommand:         App.mcp,
	managerCommand:     App.manager,
	managerStopCommand: App.managerStop,
	boardCommand:       App.openBoard,
}

// # The second kind of command, and why it is in this file
//
// `/resume`, `/new`, `/name` and `/task` are addressed to **Wake**: they are
// target-independent, and `/resume` has to work on a parked session, which is
// why submit runs the router above before anything is routed. `/effort` and
// `/model` are addressed to a **session** - they configure one - so they need a
// target and run after App.route has resolved one.
//
// They are claude's own words, which the corpus rule refuses. The rule narrows
// rather than bends: **a word claude advertises may be claimed only in a form
// claude is recorded doing nothing with.** testdata/stream/bare-effort.jsonl
// and bare-model.jsonl are that recording - each is nine frames ending in a
// result with `num_turns: 0` and a cost of zero, so the bare form is handled by
// the CLI without a model turn at all. Anything with an argument is untouched
// and reaches the agent byte for byte, exactly as it did before.
//
// The fixture filename is not decoration: slashguard_test.go checks each one
// exists, so a word cannot be exempted by assertion. That is what keeps this
// from being the prefix rule the whole fence was built against - it admits the
// forms somebody recorded and nothing else.
//
// It lives here rather than in a file of its own because
// TestNothingButTheRouterKnowsWhatASlashMeans holds *what a leading slash
// means* to this one file, and a second file cutting the prefix off a draft is
// the exact mutation that test's header records surviving every other check.

const (
	effortCommand = "effort"
	modelCommand  = "model"
)

// bareOnlyCommands are the words Wake claims in their bare form and in no
// other, mapped to the recording that earned each one.
var bareOnlyCommands = map[string]string{
	effortCommand: "bare-effort.jsonl",
	modelCommand:  "bare-model.jsonl",
}

// bareOnlyCommandCount holds that map to its size, for wakeCommandCount's
// reason and one sharper: every entry is a claim on a word claude owns, so one
// cannot be added without the recording rule being looked at.
const bareOnlyCommandCount = 2

// configureVerb is one of these commands as somebody types it, for resumeVerb's
// reason: a surface that has to *build* a command takes a rendered spelling, so
// that "what a leading slash means" stays in this file. internal/ui/picker.go
// and the completion menu are the callers.
func configureVerb(word string) string { return SlashPrefix + word }

// wakeVerbs is every command Wake owns, rendered as somebody types them.
//
// Derived from the two maps rather than written out, so a command added to
// either reaches the menu without anybody remembering to - and so the menu
// cannot advertise a word this file does not answer. Sorted, because a Go map
// has no order and a menu that reshuffled between frames is unreadable.
func wakeVerbs() []string {
	out := make([]string, 0, len(commands)+len(bareOnlyCommands))
	for word := range commands {
		out = append(out, configureVerb(word))
	}
	for word := range bareOnlyCommands {
		out = append(out, configureVerb(word))
	}
	slices.Sort(out)
	return out
}

// commandStem is the command a draft is part-way through typing: the text
// before the word at the cursor, the word after the prefix, and false when that
// word is not one.
//
// It lives here for resumeVerb's reason read backwards - answering it means
// knowing what a leading slash means, and that is this file - and it decides
// nothing: the caller is internal/ui/completion.go, which offers what could
// finish the word and routes nothing.
//
// The word at the cursor rather than the whole draft, which is `mentionStem`'s
// own shape for the same reason: a command completes mid-text - `look at /cle`
// is a message with one being typed at its end - the way Claude Code offers one.
// An argument is still refused, but because the last token is not a command
// (`/clear now` ends in `now`), not because the draft has a space in it.
//
// Any whitespace ends the word, which is `wordBreak` - the set the `@` half has
// always used.
func commandStem(draft string) (head, word string, ok bool) {
	at := strings.LastIndexAny(draft, wordBreak) + 1
	word, ok = strings.CutPrefix(draft[at:], SlashPrefix)
	if !ok {
		return "", "", false
	}
	return draft[:at], word, true
}

// configure draws a picker if this draft is one of Wake's bare configure
// commands, reporting whether Wake took it.
//
// Targets come from the caller, which has already routed. This function does
// not route and may not: sendRoom's header rules that a second caller of the
// router is a second answer to one question, with the promise on screen and the
// turns on the wire free to disagree.
//
// Every return is `false` or the flag the lookup bound, which is slash's own
// property and is checked the same way - see
// TestOnlyTheCommandLookupCanAnswerThatWakeTookAConfigureDraft.
func (a App) configure(targets []string, text string) (App, tea.Cmd, bool) {
	body, ok := strings.CutPrefix(strings.TrimSpace(text), SlashPrefix)
	if !ok {
		return a, nil, false
	}
	word, arg, _ := strings.Cut(body, " ")
	// Only the bare form is Wake's. A draft with an argument is a message, and
	// passing it through is what keeps `/effort max` and `/model opus` working.
	if strings.TrimSpace(arg) != "" {
		return a, nil, false
	}
	_, mine := bareOnlyCommands[strings.ToLower(word)]
	if !mine {
		return a, nil, false
	}
	if len(targets) == 0 {
		// Nothing to configure, and the caller has already said so in its own
		// words. A picker here would be one that cannot be confirmed.
		return a, nil, false
	}
	return a.openPicker(strings.ToLower(word), targets), nil, mine
}

// wakeCommandCount holds that map to its size. The list is hand-written - it
// is a vocabulary rather than something the code declares elsewhere - and
// docs/notes/decisions.md's rule for one of those is that it carries a count,
// so a command cannot be added without the passthrough guard being looked at.
//
// Named for its half of the overload rather than `commandCount`, which this
// package's tests already use for how many goroutines one tea.Cmd costs.
const wakeCommandCount = 9

// slash routes one draft, reporting whether Wake took it.
//
// False for everything that is not exactly one of Wake's commands, including a
// message that merely begins with `/`. The first word decides and the rest is
// the argument, which is the same shape `!cmd` and `@name` already use.
//
// Every mutation of this function's decision is a working claude feature
// silently replaced by a refusal, so its shape is pinned in slashguard_test.go:
// the only thing that may make it answer true is the lookup below.
func (a App) slash(text string) (App, tea.Cmd, bool) {
	body, ok := strings.CutPrefix(strings.TrimSpace(text), SlashPrefix)
	if !ok {
		return a, nil, false
	}
	word, arg, _ := strings.Cut(body, " ")
	run, mine := commands[strings.ToLower(word)]
	if !mine {
		// Not Wake's, so it is a message. Passed through byte for byte:
		// claude's own commands work in stream-json mode and this is the layer
		// that would otherwise quietly stop them.
		return a, nil, false
	}
	// mine rather than true, and the guard requires it: the only thing that may
	// answer "Wake took this" is the lookup that just said so.
	next, cmd := run(a, strings.TrimSpace(arg))
	return next, cmd, mine
}

const (
	// noParkedSessions is /resume with nothing to bring back.
	//
	// **It names the two keys now, and that is the hint line's rule read
	// forwards rather than a change of mind.** It named none for one task, and
	// said so: the obvious sentence was *"⌃C parks the conversation you are in,
	// and ⌃Q parks the fleet on the way out"* - both true of the design and
	// neither true of that build, where ⌃C detached, so somebody who read it and
	// pressed ⌃C would have lost the window they were reading while believing
	// they had parked something. The lifecycle spec made park a prerequisite for
	// the rebinding rather than a companion to it. The rebinding has landed, both
	// keys do what this says, and slash_test.go now holds the sentence to the
	// legend - which is where "these keys exist" is decided.
	noParkedSessions = "nothing is parked, so there is nothing to bring back. ⌃C parks the conversation you are in, and ⌃Q parks the fleet on the way out"

	// noResumeTarget is /resume in the room with no name after it. It refuses
	// rather than guessing, for the reason the room refuses an unaddressed
	// draft: with several parked, picking one for somebody is not a
	// recoverable mistake - they type into it.
	noResumeTarget = "which one? " + resumeVerb + " <name>, or " + resumeVerb + " " + resumeAll

	// notParked is /resume aimed at something that is not parked.
	notParked = "%s%s is not parked, so there is nothing to bring back"

	// resumeFailed names the write that could not happen.
	resumeFailed = "bringing that session back"

	// resumeAsked is said on the keypress, because the daemon may refuse -
	// the id may be held by another process - and the operator should know the
	// command was read either way.
	resumeAsked  = "bringing %s%s back…"
	resumeAskedN = "bringing %d parked sessions back…"
)

// resume brings a parked session back: the one named, the one this
// conversation is with, or all of them.
//
// It writes frames and decides nothing about whether the resume is safe. That
// judgement is the daemon's - it is the only process that can ask the operating
// system whether anything else is running under the id, and a copy of that
// check here would be the parallel implementation this project forbids, stale
// the day resumeSafe changes and stale in the direction that resumes twice.
// Which is also why every refusal the daemon writes back is shown as it wrote
// it: those sentences name *when* the operator can act, and a local "could not
// resume" would replace the only useful half.
func (a App) resume(arg string) (App, tea.Cmd) {
	parked := a.parkedAgents()
	if len(parked) == 0 {
		notice.Report("%s", noParkedSessions)
		return a, nil
	}

	switch {
	case strings.EqualFold(arg, resumeAll):
		return a.bringBack(parked)

	case arg == "":
		// A DM names its recipient in its own header, so a bare /resume there
		// is unambiguous. The room is not one conversation and does not guess.
		agent, ok := a.parkedHere()
		if !ok {
			notice.Report("%s\n%s", noResumeTarget, parkedList(parked))
			return a, nil
		}
		return a.bringBack([]Agent{agent})

	default:
		who := strings.TrimPrefix(arg, agentPrefix)
		agent, ok := a.parkedNamed(who)
		if !ok {
			notice.Report(notParked+"\n%s", agentPrefix, who, parkedList(parked))
			return a, nil
		}
		return a.bringBack([]Agent{agent})
	}
}

// bringBack is the tail every arm that resumes shares: the draft goes, the
// operator is told what was read, and one command writes one wake per session.
//
// One session is named rather than counted, whichever arm asked. `/resume all`
// against a single parked session would otherwise say "1 parked sessions", and
// the name is the more useful half of that sentence anyway.
func (a App) bringBack(agents []Agent) (App, tea.Cmd) {
	a = a.clearDraft()
	if len(agents) == 1 {
		notice.Report(resumeAsked, agentPrefix, agents[0].Name)
	} else {
		notice.Report(resumeAskedN, len(agents))
	}
	ids := make([]string, 0, len(agents))
	for _, ag := range agents {
		ids = append(ids, ag.ID)
	}
	return a.awaitingWake(ids...), a.write(resumeFailed, wakeFrames(agents)...)
}

// wakeFrames is one wake per agent, built as a slice for App.write's rule:
// bubbletea runs every tea.Cmd on its own goroutine and rpc's write lock is
// process-wide, so `/resume all` against twenty parked sessions must be one
// command writing twenty frames rather than twenty commands.
func wakeFrames(agents []Agent) []rpc.Frame {
	out := make([]rpc.Frame, 0, len(agents))
	for _, agent := range agents {
		out = append(out, rpc.Frame{Kind: rpc.FrameWake, SessionID: agent.ID})
	}
	return out
}

// parkedAgents is every agent this client knows to be parked, in attention
// order - which puts them together, since they all rank the same.
//
// Two sources, because there are two ways to be parked. An agent parked with ⌃C
// is still in the fleet and still holds its name; one left in the park book by a
// previous daemon is not in the fleet at all, and is the whole reason /resume
// still has anything to name after a ⌃Q. They cannot overlap - the daemon takes
// a record out of the book as it launches, and a live row is one it is holding.
func (a App) parkedAgents() []Agent {
	var out []Agent
	for _, agent := range a.fleet.Agents() {
		if agent.State == rpc.StateParked {
			out = append(out, agent)
		}
	}
	return append(out, a.fleet.Parked()...)
}

// parkedNamed resolves a name to a parked agent. Exact and folded, the way
// Fleet.ByName is exact: the daemon guarantees no two live sessions share a
// name, a parked one still holds its name, and a prefix match belongs to
// `wake attach` where a person is typing at a shell.
func (a App) parkedNamed(who string) (Agent, bool) {
	for _, agent := range a.parkedAgents() {
		if strings.EqualFold(agent.Name, who) {
			return agent, true
		}
	}
	return Agent{}, false
}

// parkedHere is the parked agent this conversation is with, when the focused
// pane is a conversation at all.
func (a App) parkedHere() (Agent, bool) {
	if a.focus == "" {
		return Agent{}, false
	}
	agent, ok := a.fleet.Agent(a.focus)
	if !ok || agent.State != rpc.StateParked {
		return Agent{}, false
	}
	return agent, true
}

// parkedList names what could be brought back, so a wrong name costs one line
// rather than two commands. Same job runningSessions does for `wake attach`.
func parkedList(parked []Agent) string {
	names := make([]string, 0, len(parked))
	for _, agent := range parked {
		names = append(names, agentPrefix+agent.Name)
	}
	return "parked: " + strings.Join(names, " ")
}

// transcriptFormat is what a conversation that has just come back says about
// itself, and it is one sentence because two surfaces say it.
//
// `wake attach` has said it since Phase 1: a pane that opens empty over a
// session with an hour behind it reads as a session that lost it, and the
// truth is narrower and worth stating - claude keeps the transcript on disk and
// Wake never had it. `/resume` produced the identical surprise and said
// nothing.
const transcriptFormat = "%s%s is back. What it said before now is not here - claude keeps the transcript, Wake does not"

// TranscriptNotice is that sentence, for the two callers that need it: this
// package on a wake, and cmd/wake on an attach. Exported for the second, which
// is ParkedNotice's arrangement and its reason - the sentence lives beside the
// thing it describes rather than being written twice.
func TranscriptNotice(name string) string {
	return fmt.Sprintf(transcriptFormat, agentPrefix, name)
}

// wakeArrived says it the first time a report shows a session this client asked
// to wake as running again.
//
// parkArrived's shape, for parkArrived's reason: the daemon refuses a wake for
// real reasons - something already holds the id, the record carries no
// directory - so the keypress may only name the ask. Once per transition, and
// only for sessions this client asked about: another window's /resume is not
// this operator's business.
func (a App) wakeArrived(st *rpc.Status) App {
	if st == nil || len(a.waking) == 0 {
		return a
	}
	for _, s := range st.Sessions {
		if _, asked := a.waking[s.ID]; !asked || s.State == rpc.StateParked {
			continue
		}
		next := make(map[string]struct{}, len(a.waking))
		for id := range a.waking {
			if id != s.ID {
				next[id] = struct{}{}
			}
		}
		a.waking = next
		notice.Report("%s", TranscriptNotice(s.Name))
		a = a.modeReverted(s.ID, s.Name)
		// The room is missing everything this session said before it was
		// parked, and this is the only report that says it is back. A fork is
		// refused here as it is at the seed - its transcript is its parent's.
		// See roomhistory.go.
		if !isFork(s) {
			a = a.askRoomHistory(s.ID)
		}
	}
	return a
}

// awaitingWake remembers a wake this client asked for.
func (a App) awaitingWake(ids ...string) App {
	next := make(map[string]struct{}, len(a.waking)+len(ids))
	for held := range a.waking {
		next[held] = struct{}{}
	}
	for _, id := range ids {
		next[id] = struct{}{}
	}
	a.waking = next
	return a
}

// adoptCommand is the room's half of session importing, and the word is a
// finding rather than a preference.
//
// The obvious spelling is `/import`, matching the shell verb - and `import` is
// in the recorded corpus of what claude advertises on its `init` frame, in 45
// of the 45 files that carry the key. Taking it would replace a working
// command with a refusal. `adopt` is in none of the 133, and it is the word
// internal/daemon/import.go's own header already uses for the action. See
// adopt.go's header and TestImportIsNotWakesWordBecauseTheCorpusShowsClaudeAdvertisingIt.
const adoptCommand = "adopt"

// adoptVerb is `/adopt` as somebody types it, for resumeVerb's reason: a
// surface that shows a command takes a rendered spelling, so that "what a
// leading slash means" stays in the router.
const adoptVerb = SlashPrefix + adoptCommand

// AdoptUsage is what the picker says about taking one, and about taking
// several - which is the founding message's plural and the half `wake import`
// does not do.
//
// Exported because the picker is *rendered by cmd/wake*: internal/ui may not
// read this machine, so the rows come from over there while the sentence
// naming a command only this package answers comes from here. That is
// ParkedNotice's arrangement and its reason - and it is also what keeps every
// `/command` this build tells an operator to type inside the two packages
// TestEverySlashCommandAnySentenceNamesIsOneThisPackageAnswers walks.
const AdoptUsage = adoptVerb + " <id> adopts one, " + adoptVerb + " <id> <id> adopts several"

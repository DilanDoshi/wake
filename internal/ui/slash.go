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
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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

// renameCommand is claude's own word for what `/name` does, and Wake does not
// own it: the corpus shows it advertised, so the router leaves it a message and
// it still reaches the agent (see nameCommand above and
// TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising). It is spelled
// here only so renameMirror can recognise the draft it mirrors - a `/rename bob`
// that reached claude but moved nothing Wake showed was the reported confusion.
const renameCommand = "rename"

// colorCommand sets an agent's identity hue. **The word is claude's own theme
// command** (advertised on 71 recorded `init` frames), so this file's corpus
// rule refuses it the way it refused `rename` and `import`; it is claimed anyway
// on the owner's 2026-08-27 override, recorded with the full argument in
// slashguard_test.go's ownerClaimedCommands and docs/notes/deferred.md.
const colorCommand = "color"
const colorVerb = SlashPrefix + colorCommand // for resumeVerb's reason

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

// quitCommand ends one agent and removes it from this window - `/manager-stop`
// for an ordinary session. It writes `rpc.FrameStop`, the ending nothing brings
// back, and unlike ⌃C park it is not recoverable.
//
// **The word is safe to take on the evidence `new` and `manager` are:** the
// recorded corpus carries 133 commands claude advertises on its `init` frames
// and `quit` is in none of them, which is what
// TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising checks.
//
// It takes an `@who` and so is a roomTargetCommand: `@sydney /quit` aims at
// sydney, the same mention→target bridge `/color`, `/name` and `/task` ride.
// Bare, it ends the conversation it is typed in - see quit.go's quitTarget.
const quitCommand = "quit"

// quitVerb is `/quit` as somebody types it, for managerVerb's reason.
const quitVerb = SlashPrefix + quitCommand

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

// colorUsage names the colours because the set is closed and small - a guessed
// hue reads the seven that exist rather than round-tripping a refusal.
// noColorTarget is noNameTarget's: the room does not guess a target.
var (
	colorUsage = colorVerb + " <colour>, or " + colorVerb + " " + agentPrefix + "<who> <colour>" +
		"; colours: " + strings.Join(rpc.ColorNames, " ") + " (or " + rpc.ColorNone + " to clear)"
	noColorTarget = "which one? " + colorUsage
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
	colorCommand:       App.colorAgent,
	adoptCommand:       App.adopt,
	mcpCommand:         App.mcp,
	managerCommand:     App.manager,
	managerStopCommand: App.managerStop,
	quitCommand:        App.quitAgent,
	boardCommand:       App.openBoard,
	loginCommand:       App.login,
}

// roomTargetCommands are the Wake commands that take an `@who` and so can be
// aimed by a room mention: `@thea /color green` means `/color @thea green`.
// name, task and colour read displayTarget for the value beside the target;
// quit takes the target alone - `@thea /quit` ends thea. `/effort` and `/model`
// are the room's other addressed path (configure, a picker over the bare form);
// every other Wake verb takes no `@who`, and claude's own commands must pass
// through to the agent untouched. So the set is closed and a subset of commands,
// held to both by slashguard_test.go for the reason the passthrough itself is
// guarded.
var roomTargetCommands = map[string]struct{}{
	nameCommand:  {},
	taskCommand:  {},
	colorCommand: {},
	quitCommand:  {},
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

// leadingCommand reports whether a draft body is addressed to the agent as a
// slash command - text that begins with the command prefix, Wake's own or one
// claude owns.
//
// It lives here for SlashPrefix's reason: recognising a leading slash is this
// file's alone (TestNothingButTheRouterKnowsWhatASlashMeans), so route() asks
// this rather than spelling the prefix in mention.go. It decides nothing about
// *which* command - only that the body is one - because that is all route()
// needs to know that open mode is widening a knob rather than a message.
func leadingCommand(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), SlashPrefix)
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

// mentionCommand runs a Wake target-command that a room mention addressed: it
// folds the mention back into the `@who` the command already reads, so `@thea
// /color green` is dispatched as `/color @thea green`. ok is false for any draft
// that is not one of roomTargetCommands, so claude's own `/clear` and every
// non-target Wake verb fall through to an ordinary send - which is what keeps
// them reaching the agent.
//
// It is here, not in send.go, because recognising a leading slash is this file's
// alone (TestNothingButTheRouterKnowsWhatASlashMeans). who is a name a mention
// resolved to, so it is one of the fleet's own, and the reconstructed `@who`
// routes back through displayTarget's fleet.ByName exactly as an embedded one
// typed by hand would.
func (a App) mentionCommand(who, text string) (App, tea.Cmd, bool) {
	body, ok := strings.CutPrefix(strings.TrimSpace(text), SlashPrefix)
	if !ok {
		return a, nil, false
	}
	word, arg, _ := strings.Cut(body, " ")
	word = strings.ToLower(word)
	if _, mine := roomTargetCommands[word]; !mine {
		return a, nil, false
	}
	next, cmd := commands[word](a, strings.TrimSpace(agentPrefix+who+" "+arg))
	return next, cmd, true
}

// wakeCommandCount holds that map to its size. The list is hand-written - it
// is a vocabulary rather than something the code declares elsewhere - and
// docs/notes/decisions.md's rule for one of those is that it carries a count,
// so a command cannot be added without the passthrough guard being looked at.
//
// Named for its half of the overload rather than `commandCount`, which this
// package's tests already use for how many goroutines one tea.Cmd costs.
const wakeCommandCount = 12

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

// renameMirror is Wake's half of a `/rename bob` typed in a conversation: the
// write that moves this conversation's own handle for its agent, so the roster
// and claude's title do not drift - or nil for any draft that is not one. The
// room's `@who /rename bob` is renameMirrorFor, off the router's resolved
// mention.
//
// It is here, not in send.go, because recognising the draft means knowing what
// a leading slash means, which is this file's alone
// (TestNothingButTheRouterKnowsWhatASlashMeans). It is deliberately not a
// commands entry: `rename` is a word claude advertises, so slash leaves the
// draft a message and it still reaches the agent - the caller writes this
// *beside* the send, never instead of it, so claude's own rename keeps working.
//
// It mirrors claude's grammar rather than `/name`'s, and the difference is the
// whole correctness of it. Claude's `/rename` renames the session it is typed
// in, so a focused conversation's mirror moves *that* agent and a leading `@who`
// is just a word - two of them, so renameMirrorArg declines - never a Wake
// target, which would rename one agent while claude renamed the one the DM is
// with. The room is the opposite case: an `@who` there is the router's routing
// target, so claude and Wake rename the same agent, and renameMirrorFor honours
// it the way `/color` and `/name` take a room mention.
func (a App) renameMirror(text string) tea.Cmd {
	if a.focus == "" {
		return nil
	}
	agent, ok := a.fleet.Agent(a.focus)
	if !ok {
		return nil
	}
	return a.renameMirrorArg(agent, text)
}

// renameMirrorFor is the room's half of `@who /rename bob`: it mirrors claude's
// rename onto the mentioned agent, beside the passthrough that carries /rename
// to claude. who is the router's resolved mention - a live fleet name - so it
// resolves the same way mentionCommand's reconstructed `@who` does, and nil when
// the mention no longer names a live agent.
func (a App) renameMirrorFor(who, text string) tea.Cmd {
	agent, ok := a.fleet.ByName(who)
	if !ok {
		return nil
	}
	return a.renameMirrorArg(agent, text)
}

// renameMirrorArg is the shared recogniser behind both mirrors: a one-word
// `/rename bob` becomes a rename of agent, or nil for anything else - a second
// word Wake cannot hold, a folded case claude will not read, or an empty name.
// One copy, so the focused and room mirrors cannot drift on what a `/rename` is.
func (a App) renameMirrorArg(agent Agent, text string) tea.Cmd {
	body, ok := strings.CutPrefix(strings.TrimSpace(text), SlashPrefix)
	if !ok {
		return nil
	}
	word, name, _ := strings.Cut(body, " ")
	name = strings.TrimSpace(name)
	if word != renameCommand || name == "" || len(strings.Fields(name)) != 1 {
		return nil
	}
	return a.renameTo(agent, name)
}

// loginCommand draws the auth panel: whether this machine is signed in, and the
// command to sign in from a terminal when it is not.
//
// **The word is safe to take on the same evidence `new` is.** The recorded
// corpus in testdata/stream advertises no `login` (nor `auth` or `logout`) on any
// `init` frame, which is what TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising
// checks - so this needs no `mcp`-style exception, and taking it replaces no
// working headless feature. Wake shows status and hands `claude auth login` over;
// it never runs the login, which is the no-PTY non-negotiable. See authapp.go.
const loginCommand = "login"

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

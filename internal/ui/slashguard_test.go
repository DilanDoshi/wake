package ui

// What may decide that a draft is Wake's, and what the set of Wake's commands
// may hold.
//
// # Why this file exists at all
//
// slash_test.go walks five of claude's commands and requires each to reach the
// agent. That is a table of examples, and "does this string start a Wake
// command" is a question about a **value space** - docs/notes/decisions.md's
// third rung says no finite sample closes one. The mutant it does not see is
// not a strawman, it is the shape somebody actually writes:
//
//	var claudes = map[string]bool{"model": true, "clear": true, ...}
//	if !claudes[word] { notice.Report("unknown command: %s", word); return a, nil, true }
//
// Every entry in slash_test.go's list passes. Every command the operator wrote
// themselves - `.claude/commands/*.md` are slash commands too, and **nothing
// can enumerate them at the moment this question is asked** - stops working,
// with no error anybody can act on. (Claude *does* announce them, on the `init`
// frame, which the airlock drops; the list is per session and arrives after the
// first frame, so it cannot decide a per-keystroke question in a room holding
// thirty agents. See slash.go's header, which carries the correction and what
// the frame could support instead.) So the close is static: **the only thing that may answer
// "Wake took this" is the lookup in commands.**
//
// # The three rungs this stands on
//
//   - Rung 2, enumerate the domain from the code that declares it: Wake's set
//     is the `commands` map literal, read out of the AST and cross-checked
//     against the map the program actually runs, so a command registered
//     anywhere else - an init(), a second file - is a build failure rather
//     than a silent widening.
//   - Rung 3, assert statically what a sample cannot close: every `return` in
//     the router hands back either `false` or the flag the lookup bound, and
//     that flag is written exactly once.
//   - Rung 5, guard the unit and not one function: a router that declined
//     correctly and a `submit` that refused `/foo` on its own way past would
//     satisfy every check pointed at `slash` alone. So the prefix, the map and
//     the decision are held to this one file across the package.
//
// # What this does not close, stated because a boundary a reviewer discovers
// # is worth less than one the file admits
//
//   - **It is syntax, not types.** A second package-level `commands` would be
//     resolved by spelling. This package has one, and the floors below fail
//     loudly rather than quietly if the resolution stops finding what it
//     expects.
//   - **What a command *does* once it is Wake's is unconstrained.** These
//     checks are about the routing decision. `/resume`'s own behaviour rests on
//     slash_test.go, where a refusal is asserted by the absence of a frame.
//   - **The passthrough list is still hand-written**, and it is no longer the
//     only sample: TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising
//     reads `slash_commands` off the recorded `init` frames, which is 133 words
//     against five. Both are samples and neither closes the value space - that
//     is what the static checks above are for. The hand-written one carries a
//     count because it is hand-written; it is kept beside the corpus because it
//     is not a subset of it (`help` is in the list and not in the recordings).

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// slashFile is the one file in this package that may know what a slash
	// means.
	slashFile = "slash.go"

	// slashRouter is the function that decides.
	slashRouter = "slash"

	// commandsVar is the map that decision has to be made out of.
	commandsVar = "commands"

	// prefixCut is how the router asks whether a draft carried the prefix at
	// all - and keeps the answer, which strings.TrimPrefix throws away.
	prefixCut = "strings.CutPrefix"

	// argSeparator is the one string literal the router is allowed to spell.
	// Anything else in there is a command name written down beside the map,
	// which is the mutant this file exists for.
	argSeparator = " "
)

// Wake's commands and claude's do not overlap, checked both ways.
//
// One direction is slash_test.go's passthrough test. This is the other: a
// command added to Wake's set that claude also has would silently stop working
// for the agent, and nothing else in this package would notice - the
// passthrough test only walks the list, and the list is hand-written.
func TestWakeOwnsNoCommandClaudeAlsoHas(t *testing.T) {
	if len(commands) != wakeCommandCount {
		t.Fatalf("Wake owns %d slash commands and wakeCommandCount says %d: a command added without "+
			"looking at the passthrough list is one that may already work for the agent", len(commands), wakeCommandCount)
	}
	for _, text := range claudeCommandsThatMustPassThrough {
		word, _, _ := strings.Cut(strings.TrimPrefix(text, SlashPrefix), " ")
		if _, mine := commands[word]; mine {
			t.Errorf("Wake owns %q and claude has it too. Taking it here replaces a working feature "+
				"with a refusal, and the operator's only symptom is that the command stopped doing "+
				"what it used to", word)
		}
	}
}

// commandsClaudeAdvertised is where a recording says what claude's command set
// actually is, and it is the directory `internal/core`'s goldens already read.
const commandsClaudeAdvertised = "slash_commands"

// recordedCommandCorpus is the recorded stream, relative to this package.
var recordedCommandCorpus = filepath.Join("..", "..", "testdata", "stream")

// Wake owns no command the recorded corpus shows claude advertising.
//
// # Why this exists beside the hand-written list rather than instead of it
//
// `TestWakeOwnsNoCommandClaudeAlsoHas` walks five words somebody typed into a
// slice. That is rung 1 — a sample — and deferred.md has carried the upgrade
// since the layer shipped: *"the set claude actually ships is on the `init`
// frame as `slash_commands`, which the airlock drops - reading it would make
// the guard exact"*. The airlock drops it for a **non-test** file's benefit;
// a test may read the bytes, and this one does.
//
// It is still a sample — one machine's `claude`, on the days these were
// recorded — so it does not close the value space either, and nothing here
// pretends to. What it closes is the **overlap**, in the one direction no
// static check can reach: whether a word Wake takes is a word claude has. A
// larger sample is strictly more of that, and this one is 133 words against 5.
//
// # It found a real one, which is why it is not ceremony
//
// The obvious word for the founding message's *"you can either rename or
// assign a task"* is `/rename`, and **`rename` is in this corpus**. Taking it
// would have replaced a working command with a refusal on the owner's own
// machine, and the hand-written five would have said nothing. `/name` and
// `/task` are in neither list, which is why those are the words.
//
// # And it is the evidence behind refusing `/add-<agent-name>`
//
// The corpus is mostly **hyphenated**, and eight of its entries begin `new-`
// (`new-oscar`, `new-victor`, `new-sierra`, …). So a router that read
// `<verb>-<suffix>` — the only shape that reaches `/add-sydney` — would claim
// every one of them. The near-miss deferred.md names (`claude ships /add-dir`)
// understates it: the failure is not one built-in, it is the whole shape of an
// operator's own command set. See slash.go's header for the ruling.
func TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising(t *testing.T) {
	seen := recordedClaudeCommands(t)
	for word := range commands {
		if _, exempt := redirectOnlyCommands[word]; exempt {
			continue
		}
		if _, claimed := ownerClaimedCommands[word]; claimed {
			// A word claude advertises that Wake claims anyway, on the owner's
			// explicit instruction rather than on this file's usual evidence. The
			// exception is recorded and checked below, not a hole in the loop.
			continue
		}
		if where, has := seen[word]; has {
			t.Errorf("Wake owns %q and %s records claude advertising it. Taking it here replaces a "+
				"working command with a refusal, and the operator's only symptom is that the command "+
				"stopped doing what it used to", word, where)
		}
	}
	// The second exemption, and it is a different claim from bareOnlyCommands
	// rather than more of it. That set is for a word claude does **nothing**
	// with here. This one is for a word claude *answers* and whose answer is
	// "not here" - a redirection to a surface a headless session does not have.
	// Taking one of those replaces a signpost, not a feature.
	//
	// It is the narrower and more dangerous exemption of the two, because the
	// bare form does work: the recording has to show the redirect itself, not
	// merely a reply. See the check below.
	for word, fixture := range redirectOnlyCommands {
		if _, mine := commands[word]; !mine {
			t.Errorf("%q is exempted as a redirect-only command and Wake does not own it. An exemption "+
				"for a word nobody took is a rule with a hole in it", word)
		}
		if _, has := seen[word]; !has {
			t.Errorf("%q is exempted as redirect-only and the corpus does not show claude advertising "+
				"it. A word claude does not have needs no exemption", word)
		}
		if fixture == "" {
			t.Errorf("%q is exempted with no recording named, which is exemption by assertion", word)
		}
	}
	// The narrowed rule, and the only exemption from the one above: a word
	// claude advertises may be claimed **in a form claude is recorded doing
	// nothing with**. bareOnlyCommands is that set and the fixture it names is
	// the evidence, which TestEveryBareOnlyCommandNamesARecordingThatExists
	// requires to exist - so a word cannot be exempted by assertion.
	for word, fixture := range bareOnlyCommands {
		if _, has := seen[word]; !has {
			t.Errorf("%q is claimed as a bare-only command and the corpus does not show claude "+
				"advertising it. The exemption is for words that need one; a word claude does not have "+
				"belongs in %s, where the whole draft is Wake's", word, commandsVar)
		}
		if fixture == "" {
			t.Errorf("%q is exempted with no recording named, which is the exemption-by-assertion this "+
				"rule exists to refuse", word)
		}
	}
}

// Every exemption names a recording that is really there.
//
// This is the whole weight of the narrowed rule. "claude does nothing with the
// bare form" is a claim about a program, and the only thing that settles one in
// this repository is a recording - so the exemption carries a filename and the
// filename is checked. A word exempted with a fixture that does not exist is
// the fence being reasoned around, which is exactly what it is for.
func TestEveryBareOnlyCommandNamesARecordingThatExists(t *testing.T) {
	if len(bareOnlyCommands) != bareOnlyCommandCount {
		t.Fatalf("%d bare-only commands and the count says %d: one added without being counted is a "+
			"word taken from claude with nobody looking at the rule",
			len(bareOnlyCommands), bareOnlyCommandCount)
	}
	if len(bareOnlyCommands) == 0 {
		t.Fatal("no bare-only commands: delete this guard rather than leaving it passing over nothing")
	}
	for word, fixture := range bareOnlyCommands {
		path := filepath.Join(recordedCommandCorpus, fixture)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%q is exempted from the corpus rule by %s, which does not exist: %v\n"+
				"Record it - the rule is that a word claude advertises may be claimed only in a form a "+
				"committed recording shows inert", word, fixture, err)
		}
	}
}

// A bare-only command is taken bare and in no other form.
//
// The behavioural half of the rule, beside the static one below. picker_test.go
// asserts it end to end through submit; this asserts it of the router itself,
// so a caller that stopped reaching configure could not hide it.
func TestABareOnlyCommandIsOnlyTakenBare(t *testing.T) {
	a := newRoomApp(t).withAgents("alex").withSize(160, 30)
	for word := range bareOnlyCommands {
		if _, _, ok := a.configure([]string{"s1"}, SlashPrefix+word); !ok {
			t.Errorf("/%s bare is not claimed by Wake", word)
		}
		for _, arg := range []string{" max", " opus", " anything at all"} {
			if _, _, ok := a.configure([]string{"s1"}, SlashPrefix+word+arg); ok {
				t.Errorf("/%s%s was swallowed; only the bare form is Wake's", word, arg)
			}
		}
	}
}

// roomTargetCommands is a subset of commands and holds exactly the three that
// take an @who. A word here that commands does not have would dispatch through a
// nil function on `@who /that`; the count is the vocabulary's own guard, so a
// fourth cannot be added without this rule being looked at - the same reason
// wakeCommandCount and bareOnlyCommandCount carry one.
func TestRoomTargetCommandsAreASubsetOfCommands(t *testing.T) {
	const roomTargetCommandCount = 3
	if len(roomTargetCommands) != roomTargetCommandCount {
		t.Errorf("roomTargetCommands has %d entries, want %d: a change to the set of @who commands has to be "+
			"looked at, not slipped in", len(roomTargetCommands), roomTargetCommandCount)
	}
	for word := range roomTargetCommands {
		if _, ok := commands[word]; !ok {
			t.Errorf("roomTargetCommands has %q and commands does not, so `@who /%s` dispatches through a nil "+
				"function", word, word)
		}
	}
}

// A mention in front of one of claude's own commands still reaches the agent: it
// is not a Wake target-command, so mentionCommand declines and the draft is sent
// as a message. `@sydney /clear` clearing claude's context is the case this
// keeps working while `@sydney /color` is claimed.
func TestAMentionedClaudeCommandStillReachesTheAgent(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(160, 30).showRoom()

	_, cmd := typeAndSubmit(a, "@sydney /clear")
	go func() { _ = runCmdQuietly(cmd) }()
	f := awaitFrame(t, sent)

	if f.Kind != rpc.FrameSend {
		t.Fatalf("@sydney /clear wrote a %q frame, want a send: claude's own /clear must reach the agent", f.Kind)
	}
	if f.SessionID != "s2" || f.Text != "/clear" {
		t.Errorf("@sydney /clear reached %q with %q, want sydney (s2) with %q", f.SessionID, f.Text, "/clear")
	}
}

// configureRouter is the second decision, and it is guarded like the first.
const configureRouter = "configure"

// Only the lookup may answer that Wake took a configure draft.
//
// TestOnlyTheCommandLookupCanAnswerThatWakeTookADraft's argument, one router
// over. This one claims words claude actually has, so the mutation it denies is
// worse here than there: a `configure` that decided on anything but the lookup
// would take `/model opus` away from every agent in the fleet.
func TestOnlyTheCommandLookupCanAnswerThatWakeTookAConfigureDraft(t *testing.T) {
	fn := funcDeclIn(t, slashFile, configureRouter)
	found := commandLookup(t, fn, bareOnlyVar)

	returns := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		returns++
		if len(ret.Results) != 3 {
			t.Errorf("%s returns %d values from one of its returns, want 3", configureRouter, len(ret.Results))
			return true
		}
		id, ok := ret.Results[2].(*ast.Ident)
		if !ok || (id.Name != "false" && id.Name != found) {
			t.Errorf("%s answers %q as its handled flag. It may answer only `false` or %q, the flag the "+
				"%s lookup bound: anything else is a router claiming one of claude's own commands "+
				"without asking whether Wake has it", configureRouter, exprName(ret.Results[2]), found, bareOnlyVar)
		}
		return true
	})
	if returns < 2 {
		t.Fatalf("%s has %d return statements: a router that cannot decline is not one", configureRouter, returns)
	}
	if n := assignmentsTo(fn, found); n != 1 {
		t.Errorf("%s writes %s %d times; it has to be written once, by the lookup", configureRouter, found, n)
	}
	requireThePrefixIsCutAndTested(t, fn)
	sawNoArgument := false
	for _, lit := range spelledLiteralsIn(t, fn.Body) {
		switch lit {
		case argSeparator:
		case noArgument:
			// The empty string is not slack here, it is the decision: "was
			// there an argument" is the whole of what makes this router
			// bare-only, and the test for it is a comparison against "".
			sawNoArgument = true
		default:
			t.Errorf("%s spells the literal %q. It may write down the argument separator and the empty "+
				"string it tests for - a command name written here is a decision made somewhere other "+
				"than the map", configureRouter, lit)
		}
	}
	if !sawNoArgument {
		t.Errorf("%s never compares against %q, so nothing in it distinguishes a bare command from one "+
			"with an argument. That distinction is the entire exemption this router runs under: without "+
			"it, Wake has swallowed one of claude's own commands whole", configureRouter, noArgument)
	}
}

// noArgument is how "the draft carried no argument" is spelled, which is the
// one thing configure decides.
const noArgument = ""

// bareOnlyVar is the map the second decision has to be made out of.
const bareOnlyVar = "bareOnlyCommands"

// recordedClaudeCommands is every command name the corpus shows claude
// advertising, mapped to a file that shows it.
//
// Floors on both halves, because a scan that matched nothing would report the
// strongest possible pass for the weakest possible reason: the walk has to find
// recordings, and the recordings have to carry the key. The distinct-word floor
// is deliberately far below what is there (133 on the day this was written) so
// that re-recording the corpus does not fail it, while deleting the key does.
func recordedClaudeCommands(t *testing.T) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(recordedCommandCorpus)
	if err != nil {
		t.Fatalf("read %s: %v", recordedCommandCorpus, err)
	}
	seen, files := map[string]string{}, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(recordedCommandCorpus, e.Name())
		raw, readErr := os.ReadFile(path) //nolint:gosec // a path this test built out of its own constant
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		files++
		for _, line := range strings.Split(string(raw), "\n") {
			if line == "" {
				continue
			}
			var frame map[string]any
			if json.Unmarshal([]byte(line), &frame) != nil {
				continue
			}
			list, ok := frame[commandsClaudeAdvertised].([]any)
			if !ok {
				continue
			}
			for _, item := range list {
				if word, ok := item.(string); ok {
					seen[word] = e.Name()
				}
			}
		}
	}
	if files == 0 {
		t.Fatalf("no recordings under %s: the corpus moved and this check approves everything", recordedCommandCorpus)
	}
	if len(seen) < 50 {
		t.Fatalf("%d commands were read out of %d recordings under %q: a corpus that stopped carrying "+
			"the key leaves this guard passing over nothing", len(seen), files, commandsClaudeAdvertised)
	}
	return seen
}

// The set Wake owns is exactly the map literal, and nothing adds to it later.
//
// Rung 2 read in both directions. The runtime map is what the router consults
// and the literal is what a reader reviews, so a command registered from an
// init() - or from a second file nobody opened while reviewing this one -
// widens the first without appearing in the second. That is the same widening
// the passthrough test exists to prevent, arriving where a table cannot look.
func TestTheCommandSetIsExactlyTheMapLiteralInThisFile(t *testing.T) {
	consts := stringConstants(t, slashFile, "")
	declared := map[string]bool{}
	for _, key := range commandMapKeys(t) {
		word, ok := consts[key]
		if !ok {
			t.Fatalf("the %s map is keyed on %q, which is not a string constant in %s: a command's word "+
				"is spelled once, as a constant, because the router and the sentences about it must agree",
				commandsVar, key, slashFile)
		}
		declared[word] = true
	}
	if len(declared) == 0 {
		t.Fatalf("no key was read out of %s in %s: the scan is broken, and every claim resting on it "+
			"asserts nothing", commandsVar, slashFile)
	}
	for word := range commands {
		if !declared[word] {
			t.Errorf("the program routes %q and the map literal in %s does not declare it: something "+
				"outside this file added a command, so reviewing this file no longer tells anybody what "+
				"Wake takes", word, slashFile)
		}
	}
	for word := range declared {
		if _, ok := commands[word]; !ok {
			t.Errorf("%s declares %q and the program does not route it", slashFile, word)
		}
	}
}

// Only the lookup may answer "Wake took this".
//
// The one mutation that matters is a router that decides on anything else - a
// hand-written list of claude's commands, a prefix test, a second map - and
// every one of those has to make the router say true somewhere other than
// where the lookup did. So that is what is denied.
func TestOnlyTheCommandLookupCanAnswerThatWakeTookADraft(t *testing.T) {
	fn := funcDeclIn(t, slashFile, slashRouter)
	found := commandLookup(t, fn, commandsVar)

	returns := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		returns++
		if len(ret.Results) != 3 {
			t.Errorf("%s returns %d values from one of its returns, want 3", slashRouter, len(ret.Results))
			return true
		}
		id, ok := ret.Results[2].(*ast.Ident)
		if !ok || (id.Name != "false" && id.Name != found) {
			t.Errorf("%s answers %q as its handled flag. It may answer only `false` or %q, the flag the "+
				"%s lookup bound: anything else is a router deciding a draft is Wake's without asking "+
				"whether Wake has it, which takes a working claude command away and replaces it with a "+
				"refusal", slashRouter, exprName(ret.Results[2]), found, commandsVar)
		}
		return true
	})
	if returns < 2 {
		t.Fatalf("%s has %d return statements: a router that cannot decline is not one, and a scan that "+
			"found fewer than two is looking at the wrong function", slashRouter, returns)
	}
	if n := assignmentsTo(fn, found); n != 1 {
		t.Errorf("%s writes %s %d times. It has to be written once, by the lookup: a second assignment "+
			"is where `mine = mine || somethingElse` goes, and every return still names the flag the "+
			"lookup bound", slashRouter, found, n)
	}
	requireThePrefixIsCutAndTested(t, fn)
	for _, lit := range spelledLiteralsIn(t, fn.Body) {
		if lit != argSeparator {
			t.Errorf("%s spells the literal %q. The only string it may write down is the argument "+
				"separator - a command name, a prefix or a list of claude's commands written here is a "+
				"decision made somewhere other than the map", slashRouter, lit)
		}
	}
}

// slashIsAPathSeparatorIn names the files that spell `/` about something other
// than a draft, with what it is about.
//
// airlock_test.go's shape: an exemption is a *reason*, held to a count, so the
// list cannot grow quietly the way the thing it guards would. roster.go cuts a
// tool's path argument at its last separator - `go test ./internal/ui` shortened
// for a 20-column sidebar - which is a fact about a filename and never a
// decision about what somebody typed.
//
// The entries are checked for being **used**, not only for being allowed: a file
// that stops spelling it fails here, so the excuse is deleted in the change that
// makes it untrue rather than left to cover the next one.
var slashIsAPathSeparatorIn = map[string]string{
	"roster.go": "shortArg cuts a tool's path argument at its last separator",
}

const slashExemptionCount = 1

// The prefix, the map and the decision live in one file.
//
// Rung 5: a check pointed at `slash` says nothing about a caller that refuses a
// `/`-prefixed draft on its own account before or after asking. `submit` is one
// line away from being able to, and the failure would be invisible to every
// check above - a reviewer built exactly that and it survived the whole package.
// So the vocabulary is held here, across the package's non-test files, out of
// the AST rather than the bytes, because prose may say `/` as often as it likes
// and a comment cannot route anything.
func TestNothingButTheRouterKnowsWhatASlashMeans(t *testing.T) {
	if len(slashIsAPathSeparatorIn) != slashExemptionCount {
		t.Fatalf("%d files are excused from spelling %q and the count says %d: an exemption added without "+
			"being counted is how the one place that decides becomes two",
			len(slashIsAPathSeparatorIn), SlashPrefix, slashExemptionCount)
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	scanned := 0
	excused := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == slashFile {
			continue
		}
		scanned++
		file := parseGoFile(t, name)
		for _, lit := range spelledLiteralsIn(t, file) {
			if lit != SlashPrefix {
				continue
			}
			if _, ok := slashIsAPathSeparatorIn[name]; ok {
				excused[name] = true
				continue
			}
			t.Errorf("%s spells %q. A draft is Wake's or it is a message, and that is decided in %s "+
				"alone: a second place that knows what a leading slash means is a second place that "+
				"can swallow one of claude's commands - including every one an operator wrote "+
				"themselves, which nothing here can enumerate in time to answer this", name, lit, slashFile)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if ok && (id.Name == commandsVar || id.Name == "SlashPrefix") {
				t.Errorf("%s names %s. The routing vocabulary belongs to %s, so that a reviewer reading "+
					"one file has read the whole rule", name, id.Name, slashFile)
			}
			return true
		})
	}
	for name, why := range slashIsAPathSeparatorIn {
		if !excused[name] {
			t.Errorf("%s is excused from spelling %q because %s, and it no longer spells one: delete the "+
				"exemption in the change that made it untrue, or it covers whatever is written there next",
				name, SlashPrefix, why)
		}
	}
	if scanned < 20 {
		t.Fatalf("only %d non-test files were scanned: internal/ui is larger than that, so the glob is "+
			"looking at the wrong directory and this check is passing over nothing", scanned)
	}
}

// --- scans ---------------------------------------------------------------

// commandMapKeys is the keys of the commands map literal, as they are spelled.
func commandMapKeys(t *testing.T) []string {
	t.Helper()

	var keys []string
	for _, decl := range parseGoFile(t, slashFile).Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != commandsVar || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				t.Fatalf("%s is not a map literal in %s: this scan reads the set a reviewer sees, and "+
					"it can no longer see it", commandsVar, slashFile)
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					t.Fatalf("%s holds an entry with no key", commandsVar)
				}
				keys = append(keys, exprName(kv.Key))
			}
		}
	}
	slices.Sort(keys)
	return keys
}

// The prefix is cut, and whether it was there is a value the router keeps.
//
// The other half of "is this ours" - the lookup has the first half - and the
// mutant is a one-word edit: strings.TrimPrefix in place of strings.CutPrefix
// silently answers "yes it was" for every draft, so every sentence starting
// with the word `resume` becomes a command that brings back a fleet. Nothing
// about the returns or the lookup can see that, because both are still
// correct.
//
// The flag has to be bound to a real name rather than to `_`, and the compiler
// closes the rest: a name bound by := and never read does not build, so a
// router that takes the answer and ignores it is not a program.
func requireThePrefixIsCutAndTested(t *testing.T, fn *ast.FuncDecl) {
	t.Helper()

	var cuts []*ast.AssignStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && exprName(call.Fun) == prefixCut && len(call.Args) == 2 {
			if got := exprName(call.Args[1]); got != "SlashPrefix" {
				t.Errorf("%s cuts %q off the draft rather than SlashPrefix: the prefix is spelled once, "+
					"as a constant", fn.Name.Name, got)
			}
			cuts = append(cuts, as)
		}
		return true
	})

	if len(cuts) != 1 {
		t.Fatalf("%s calls %s %d times, want once. Without it - strings.TrimPrefix is the one-word edit - "+
			"a draft that never carried a slash is routed as though it had, and every message beginning "+
			"with one of Wake's words becomes a command", fn.Name.Name, prefixCut, len(cuts))
	}
	if len(cuts[0].Lhs) != 2 {
		t.Fatalf("%s does not keep whether the prefix was there", fn.Name.Name)
	}
	if id, ok := cuts[0].Lhs[1].(*ast.Ident); !ok || id.Name == "_" {
		t.Errorf("%s discards whether the draft carried the prefix, which is half of whether it is a "+
			"command at all", fn.Name.Name)
	}
}

// commandLookup is the name the one `commands[…]` binds its found flag to,
// having established that there is exactly one, that nothing else is indexed,
// and that its key is derived from the draft rather than written here.
func commandLookup(t *testing.T, fn *ast.FuncDecl, commandsVar string) string {
	t.Helper()

	var lookups []*ast.AssignStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if idx, ok := n.(*ast.IndexExpr); ok && exprName(idx.X) != commandsVar {
			t.Errorf("%s indexes %s. The only set it may consult is %s: a second one is a list of "+
				"claude's commands by another name", fn.Name.Name, exprName(idx.X), commandsVar)
		}
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		if idx, ok := as.Rhs[0].(*ast.IndexExpr); ok && exprName(idx.X) == commandsVar {
			lookups = append(lookups, as)
		}
		return true
	})

	if len(lookups) != 1 {
		t.Fatalf("%s looks a command up %d times, and this guard is written for the one lookup: two "+
			"decisions cannot both be the decision", fn.Name.Name, len(lookups))
	}
	as := lookups[0]
	if len(as.Lhs) != 2 {
		t.Fatalf("the %s lookup in %s does not take the found flag: without it the router cannot tell a "+
			"command it has from one it does not", commandsVar, fn.Name.Name)
	}
	id, ok := as.Lhs[1].(*ast.Ident)
	if !ok || id.Name == "_" {
		t.Fatalf("the %s lookup in %s discards the found flag", commandsVar, fn.Name.Name)
	}
	local := boundInside(fn)
	key := as.Rhs[0].(*ast.IndexExpr).Index
	if !derivedFromTheDraft(fn, key, local, map[string]bool{}) {
		t.Fatalf("%s looks up %q, which is not derived from the draft: a key this function did not "+
			"compute - a literal, or a constant naming one of Wake's own commands - makes every "+
			"`/`-prefixed message one of Wake's commands", fn.Name.Name, exprName(key))
	}
	// And each name the key is built out of is written once, **and is itself
	// derived from the draft**. `boundInside` records *that* a name was bound
	// here, never *what from*, so one extra assignment launders any value into
	// the grammar - which is how a reviewer got `/r`, `/re` and `/res` swallowed
	// with every check in this file green:
	//
	//	key := strings.ToLower(word)
	//	for c := range commands { if strings.HasPrefix(c, key) { key = c } }
	//
	// The single write denies that one. **It did not deny the one-bind version**,
	// and this file's header claimed it did - *"what it denies is the step every
	// such laundering needs"* was not what it did. A second reviewer wrote
	//
	//	key := alias(strings.ToLower(word))
	//
	// with a two-entry table in slash.go, claimed `/n` and `/nm`, and survived
	// `go test ./...` with exit 0: one bind, one write, and a value the grammar
	// never saw. So the grammar now **follows a local back to what bound it**
	// (derivedFromTheDraft below), which is the narrowing rather than a second
	// rule: today's router passes it unchanged, and `alias(…)` fails at the bind
	// because its `Fun` is not a `strings` selector.
	leaves := leavesOf(key, local)
	if len(leaves) == 0 {
		t.Fatalf("the %s lookup in %s is keyed on %q, which is built out of no local value at all: the "+
			"grammar passed over nothing", commandsVar, fn.Name.Name, exprName(key))
	}
	for _, leaf := range leaves {
		if n := assignmentsTo(fn, leaf); n != 1 {
			t.Errorf("%s writes %s %d times and looks a command up by it. The key is written once, where "+
				"it is cut out of the draft: a second write is where an abbreviation, an alias or a "+
				"correction goes, and each of those claims a word the operator may own", fn.Name.Name, leaf, n)
		}
	}
	return id.Name
}

// leavesOf is the local names a lookup key is built out of.
func leavesOf(e ast.Expr, local map[string]bool) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && local[id.Name] {
			out = append(out, id.Name)
		}
		return true
	})
	return out
}

// derivedFromTheDraft is the closed grammar a lookup key may take: a value the
// router computed out of what was typed, optionally folded by strings.
//
// It exists to deny `commands["resume"]` and `commands[resumeCommand]`, which
// answer true for every draft that starts with a slash - and the shape beside
// them: a helper mapping claude's words onto Wake's would be a call to
// something that is not strings, and one mapping them with a literal is caught
// by the literal rule above.
//
// "Computed here" is the test rather than "is an identifier", and that
// distinction was earned: the first draft of this check accepted any ident,
// so `commands[resumeCommand]` walked past it and was caught only by the
// behavioural half - which is the wrong way round for a static guard whose
// whole job is the value space.
//
// # It is transitive, and that is what closes the one-bind laundering
//
// Accepting any local was the hole a reviewer walked through with
// `key := alias(strings.ToLower(word))`: one bind, one write, and a value the
// grammar never inspected. So a local is followed **back to the expression that
// bound it** and that expression is held to the same grammar. A name this
// function binds nothing for is a parameter or the receiver, which is the draft
// itself and is where the recursion stops; `seen` stops it going round.
//
// Two things are admitted at the leaves and each is spelled once. `SlashPrefix`,
// because cutting the prefix is how the draft becomes a body and the constant is
// the one this file already requires that cut to name. And a string literal
// equal to argSeparator, because splitting the body on a space is how the body
// becomes a word - any *other* literal is a command name written into the
// dataflow, which is the mutant the literal rule above catches at one level and
// this catches at every other.
func derivedFromTheDraft(fn *ast.FuncDecl, e ast.Expr, local map[string]bool, seen map[string]bool) bool {
	switch e := e.(type) {
	case *ast.Ident:
		if e.Name == "SlashPrefix" {
			return true
		}
		if !local[e.Name] || seen[e.Name] {
			return false
		}
		bound, ok := bindingOf(fn, e.Name)
		if !ok {
			// A parameter or the receiver: the draft, or the model holding it.
			return true
		}
		seen[e.Name] = true
		return derivedFromTheDraft(fn, bound, local, seen)
	case *ast.CallExpr:
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || exprName(sel.X) != "strings" {
			return false
		}
		for _, arg := range e.Args {
			if !derivedFromTheDraft(fn, arg, local, seen) {
				return false
			}
		}
		return true
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return false
		}
		value, err := strconv.Unquote(e.Value)
		return err == nil && value == argSeparator
	default:
		return false
	}
}

// bindingOf is the expression a `:=` inside fn binds one name from, when there
// is exactly one such statement and it is readable.
//
// A multi-value bind - `word, arg, _ := strings.Cut(body, " ")` - has one RHS
// for several names, and that RHS is the expression every one of them comes
// out of, which is exactly what the grammar wants to inspect. A parallel bind
// with matching arity takes the one in the same position.
//
// Not found is not a failure here: the caller reads it as "a parameter", and
// the single-write rule beside it is what stops a name being *re*-bound.
func bindingOf(fn *ast.FuncDecl, name string) (ast.Expr, bool) {
	var found ast.Expr
	binds := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name != name {
				continue
			}
			binds++
			switch {
			case len(as.Rhs) == 1:
				found = as.Rhs[0]
			case len(as.Rhs) == len(as.Lhs):
				found = as.Rhs[i]
			}
		}
		return true
	})
	return found, binds == 1 && found != nil
}

// boundInside is every name a function binds itself: its receiver, its
// parameters, and everything it declares in its body.
//
// Anything else is a package-level name, which is a value written down away
// from the draft - and the draft is the only thing a routing decision may be
// made out of.
func boundInside(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	for _, list := range []*ast.FieldList{fn.Recv, fn.Type.Params} {
		if list == nil {
			continue
		}
		for _, f := range list.List {
			for _, name := range f.Names {
				out[name.Name] = true
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				return true
			}
			for _, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		case *ast.ValueSpec:
			for _, name := range n.Names {
				out[name.Name] = true
			}
		}
		return true
	})
	return out
}

// assignmentsTo counts how often a function writes one name.
func assignmentsTo(fn *ast.FuncDecl, name string) int {
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		as, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
				n++
			}
		}
		return true
	})
	return n
}

// spelledLiteralsIn is every literal in a syntax tree that spells text, as
// text: strings and **runes**.
//
// The rune half was added after a mutant used it to walk past this file. `"/"`
// was caught and `'/'` was not, so one character of syntax moved an
// "unknown command" refusal into `send.go` with the whole suite green - and the
// two are the same decision written two ways, which is the only thing a guard
// over spelling can be about. `"\x2f"` was already caught, because unquoting
// happens before the comparison; nothing here sees a bare `47`, and that
// boundary is stated in the header.
func spelledLiteralsIn(t *testing.T, n ast.Node) []string {
	t.Helper()

	var out []string
	ast.Inspect(n, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", lit.Value, err)
		}
		out = append(out, value)
		return true
	})
	return out
}

// slashInAProse matches a `/word` a sentence tells somebody to type.
//
// Bounded on both sides, because a path is the thing it must not match.
// The left anchor - start of string, space, backtick or `(` - keeps it off
// `.claude/commands`; the right one keeps it off `/bin/sh`, which
// internal/ui/bang.go legitimately spells and which the version with only a
// left anchor reported as an unanswered command. A check that fails on prose
// this tree has to write is one somebody deletes.
var slashInAProse = regexp.MustCompile("(?:^|[ \x60(])(/[a-z][a-z-]*)(?:[^a-z/-]|$)")

// Every `/command` any package's prose tells an operator to type is one this
// package answers.
//
// **This is the half of rung 7 that deferred.md carried as still owed**, and it
// arrived with a second offender. `wake attach`'s parked refusal names
// `/resume`, and so does `internal/daemon`'s refusal of a second manager when
// the first one is parked — two packages, neither of which can import this one,
// both asserting a fact about this package's command set while reading none of
// it. That is the same shape as the sentence that told an operator to run
// `wake fork` to get a parked conversation back, which is the defect Phase 3
// rewrote and this one repeated.
//
// It walks internal/daemon as well as this package because that is where the
// second offender lives, and the alternative — a constant exported from here
// and imported over there — is the import CLAUDE.md forbids (the daemon may not
// depend on a view, and a view may not import the daemon).
//
// Floors on both halves: the walk has to find files, and the scan has to find
// at least one command named in prose. A scan that matched nothing would report
// the strongest possible pass for the weakest possible reason.
func TestEverySlashCommandAnySentenceNamesIsOneThisPackageAnswers(t *testing.T) {
	dirs := []string{".", filepath.Join("..", "daemon")}
	named, scanned := 0, 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			scanned++
			path := filepath.Join(dir, name)
			file, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", path, perr)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					return true
				}
				for _, m := range slashInAProse.FindAllStringSubmatch(text, -1) {
					word := strings.TrimPrefix(m[1], SlashPrefix)
					named++
					if _, answered := commands[word]; !answered {
						t.Errorf("%s tells somebody to type %q and this package answers no such command "+
							"(it has %v). A missing feature is not trusted and a lying one is, and this "+
							"one is advice given at the moment somebody has already failed to do what "+
							"they wanted", path, m[1], slices.Sorted(maps.Keys(commands)))
					}
				}
				return true
			})
		}
	}
	if scanned == 0 {
		t.Fatal("the walk found no non-test files: the scan is broken and this check approves everything")
	}
	if named == 0 {
		t.Fatal("no sentence in either package names a `/command`: either the refusals stopped naming the " +
			"way out - which is the defect this exists for - or the pattern no longer matches them")
	}
}

// redirectOnlyCommands are the words claude advertises, answers, and answers
// with a redirection to somewhere a Wake agent is not.
//
// One entry, and the bar for a second is this paragraph written again with its
// own evidence. `/mcp` in a headless session replies:
//
//	6 MCP server(s): 2 connected, 2 connecting, 2 not connected, 0 disabled.
//	Use `/mcp` in the terminal for details.
//
// That is not a feature being replaced - it is a signpost, and the place it
// points is an interactive TUI Wake's agents do not have. The panel behind it
// is reachable from a shell (`claude mcp list`), so Wake draws the screen the
// signpost names instead of forwarding somebody to a terminal they are already
// sitting in front of.
var redirectOnlyCommands = map[string]string{
	mcpCommand: "bare-mcp.jsonl",
}

// Every redirect-only exemption names a recording that exists **and shows the
// redirect**, which is the half that makes this stricter than bareOnlyCommands.
//
// "claude does nothing with it" can be read off a result frame - num_turns=0.
// "claude answers and tells you to go elsewhere" is a claim about the words in
// the answer, so the words are what is checked.
func TestEveryRedirectOnlyCommandNamesARecordingThatShowsTheRedirect(t *testing.T) {
	for word, fixture := range redirectOnlyCommands {
		path := filepath.Join(recordedCommandCorpus, fixture)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%q is exempted by %s, which does not exist: %v", word, fixture, err)
			continue
		}
		if !strings.Contains(string(raw), "in the terminal") {
			t.Errorf("%s does not show claude redirecting to a terminal, so it does not earn %q the "+
				"exemption it is named for. The recording is the whole weight of this rule", fixture, word)
		}
	}
}

// ownerClaimedCommands are words claude advertises that Wake claims anyway, on
// the owner's explicit instruction rather than on this file's usual evidence.
//
// It is the weakest of the three exemptions and the only one not backed by a
// recording, so it is the one a reviewer should be most suspicious of - which is
// why it is a named map with a dated reason rather than a word slipped past the
// loop. `color` is claude's own theme command (advertised on 71 of the recorded
// inits), and the disciplined path to claim it is redirectOnlyCommands, backed
// by a recording of claude's headless /color showing an inert or redirect
// answer. No such recording exists in this session, and the owner ruled on
// 2026-08-27 that the shadowing is acceptable: Wake renders in its own palette,
// so an agent's per-session claude theme is irrelevant inside Wake. If a
// recording later shows the headless form is a redirect, this entry moves to
// redirectOnlyCommands and the exception is retired. See docs/notes/deferred.md.
var ownerClaimedCommands = map[string]string{
	colorCommand: "owner ruling 2026-08-27; no headless recording yet",
}

// The owner exemption is not a hole: every word in it is one Wake actually owns
// and one claude actually advertises, or the exemption is covering nothing.
func TestEveryOwnerClaimedCommandIsOwnedAndAdvertised(t *testing.T) {
	seen := recordedClaudeCommands(t)
	for word, reason := range ownerClaimedCommands {
		if _, mine := commands[word]; !mine {
			t.Errorf("%q is owner-claimed and Wake does not own it: an exemption for a word nobody took "+
				"is a rule with a hole in it", word)
		}
		if _, has := seen[word]; !has {
			t.Errorf("%q is owner-claimed and the corpus does not show claude advertising it, so it needs "+
				"no exemption: a word claude does not have belongs in %s with the rest", word, commandsVar)
		}
		if reason == "" {
			t.Errorf("%q is owner-claimed with no reason recorded, which is exemption by assertion - the "+
				"whole point of this map is that the exception is written down", word)
		}
	}
}

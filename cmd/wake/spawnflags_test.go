package main

import (
	"io"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// The flag comes off wherever it is written, so the arity check that already
// exists still counts names rather than flags.
func TestSpawnFlagsStripsTheEffortWhereverItIs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		rest []string
		want string
	}{
		{"after the name", []string{"new", "alex", "--effort", "max"}, []string{"new", "alex"}, "max"},
		{"before the name", []string{"new", "--effort", "max", "alex"}, []string{"new", "alex"}, "max"},
		{"no name", []string{"new", "--effort", "low"}, []string{"new"}, "low"},
		{"no flag", []string{"new", "alex"}, []string{"new", "alex"}, ""},
		{"bare new", []string{"new"}, []string{"new"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rest, opts, err := spawnFlags(tc.args)
			if err != nil {
				t.Fatalf("spawnFlags(%v): %v", tc.args, err)
			}
			if opts.Effort != tc.want {
				t.Errorf("effort = %q, want %q", opts.Effort, tc.want)
			}
			if strings.Join(rest, " ") != strings.Join(tc.rest, " ") {
				t.Errorf("rest = %v, want %v", rest, tc.rest)
			}
		})
	}
}

// A typo names the five levels rather than starting a session at a default the
// operator did not choose.
func TestSpawnFlagsRefusesAnUnknownLevel(t *testing.T) {
	_, _, err := spawnFlags([]string{"new", "--effort", "ludicrous"})
	if err == nil {
		t.Fatal("an unknown effort was accepted")
	}
	for _, level := range core.EffortLevels {
		if !strings.Contains(err.Error(), level) {
			t.Errorf("the refusal %q does not name the legal level %q", err, level)
		}
	}
}

func TestSpawnFlagsRefusesAMissingLevel(t *testing.T) {
	if _, _, err := spawnFlags([]string{"new", "--effort"}); err == nil {
		t.Fatal("a bare --effort with no level was accepted")
	}
}

func TestSpawnFlagsRefusesTwoDifferentLevels(t *testing.T) {
	if _, _, err := spawnFlags([]string{"new", "--effort", "low", "--effort", "max"}); err == nil {
		t.Fatal("two conflicting levels were accepted; one of them would be silently ignored")
	}
	if _, _, err := spawnFlags([]string{"new", "--effort", "low", "--effort", "low"}); err != nil {
		t.Errorf("the same level twice was refused: %v", err)
	}
}

// Every level core admits survives the parser, so the two cannot drift.
func TestEveryCoreLevelParses(t *testing.T) {
	for _, level := range core.EffortLevels {
		_, opts, err := spawnFlags([]string{"new", "--effort", level})
		if err != nil || opts.Effort != level {
			t.Errorf("core admits %q but the parser gave (%q, %v)", level, opts.Effort, err)
		}
	}
}

// An invocation that is nothing but flags used to index an empty slice and
// dump a Go stack trace, where every other malformed one here returns a
// sentence. `wake --effort max` is a plausible typo for `wake new --effort max`.
func TestAnInvocationOfNothingButFlagsIsRefusedNotPanicked(t *testing.T) {
	for _, args := range [][]string{
		{"--effort", "max"},
		{"--effort", "max", "--effort", "max"},
	} {
		rest, _, err := spawnFlags(args)
		if err != nil {
			continue // refused in the parser, which is also fine
		}
		if len(rest) != 0 {
			continue // a verb survived, so nothing to guard
		}
		// The parser let it through with no words left, so run must not index.
		if err := runGuard(t, args); err == nil {
			t.Errorf("%v produced no error at all", args)
		}
	}
}

// Both flags come off wherever they are written, in either order.
func TestBothFlagsComeOffInEitherOrder(t *testing.T) {
	for _, args := range [][]string{
		{"new", "--model", "opus", "alex", "--effort", "max"},
		{"new", "--effort", "max", "--model", "opus", "alex"},
		{"new", "alex", "--effort", "max", "--model", "opus"},
	} {
		rest, opts, err := spawnFlags(args)
		if err != nil {
			t.Fatalf("spawnFlags(%v): %v", args, err)
		}
		if strings.Join(rest, " ") != "new alex" {
			t.Errorf("rest = %v, want [new alex]", rest)
		}
		if opts.Effort != core.EffortMax || opts.Model != "opus" {
			t.Errorf("spawnFlags(%v) gave %+v", args, opts)
		}
	}
}

// The manager is the one session an operator cannot attach to and reconfigure
// by hand, so it is the one that most needs configuring at spawn. `--effort`
// was refused on every verb but `new`, which left it unconfigurable entirely.
func TestTheManagerTakesBothFlags(t *testing.T) {
	rest, opts, err := spawnFlags([]string{"manager", "--effort", "max", "--model", "opus"})
	if err != nil {
		t.Fatalf("spawnFlags: %v", err)
	}
	if strings.Join(rest, " ") != "manager" {
		t.Errorf("rest = %v, want [manager]", rest)
	}
	if opts.Effort != core.EffortMax || opts.Model != "opus" {
		t.Errorf("opts = %+v", opts)
	}
}

// A model this build has never heard of is passed through, because the alias
// list is a sample and nothing can enumerate the set. A parser that refused one
// would block every model released after it was written.
func TestSpawnFlagsPassesAModelItDoesNotRecognise(t *testing.T) {
	_, opts, err := spawnFlags([]string{"new", "--model", "claude-not-invented-yet"})
	if err != nil {
		t.Fatalf("an unrecognised model was refused: %v", err)
	}
	if opts.Model != "claude-not-invented-yet" {
		t.Errorf("model = %q", opts.Model)
	}
}

func TestSpawnFlagsRefusesAMissingOrEmptyModel(t *testing.T) {
	if _, _, err := spawnFlags([]string{"new", "--model"}); err == nil {
		t.Error("a bare --model with no name was accepted")
	}
	if _, _, err := spawnFlags([]string{"new", "--model", ""}); err == nil {
		t.Error(`--model "" was accepted; empty is the absence of a model, not one`)
	}
}

func TestSpawnFlagsRefusesTwoDifferentModels(t *testing.T) {
	if _, _, err := spawnFlags([]string{"new", "--model", "opus", "--model", "sonnet"}); err == nil {
		t.Error("two conflicting models were accepted; one of them would be silently ignored")
	}
	if _, _, err := spawnFlags([]string{"new", "--model", "opus", "--model", "opus"}); err != nil {
		t.Errorf("the same model twice was refused: %v", err)
	}
}

// A spawn flag on a verb that starts nothing configures nothing, so it is
// refused by name rather than ignored: a flag silently dropped is configuration
// the operator believes they applied.
//
// `fork` is on this list deliberately. The daemon reads no effort and no model
// off a fork frame - rpc.Frame.Effort says so - so accepting either there would
// be the exact failure this refusal exists to prevent, one verb over.
//
// The flags are derived from knownFlags rather than listed, which is what makes
// this cover the sixth the day it is added rather than the day somebody
// remembers. Each carries an invocation the parser accepts, because a refusal
// for a *bad value* would pass this test while proving nothing about the verb.
func TestASpawnFlagOnAVerbThatStartsNothingIsRefusedByName(t *testing.T) {
	for _, verb := range []string{"status", "stop", "attach", "import", "fork"} {
		for _, flag := range knownFlags {
			args := append([]string{verb}, legalArgsFor(t, flag)...)
			if _, _, err := spawnFlags(args); err == nil {
				t.Errorf("%v was accepted; %s starts no session", args, verb)
			} else if !strings.Contains(err.Error(), verb) {
				t.Errorf("the refusal for %v does not name %q: %v", args, verb, err)
			}
		}
	}
}

// And it is accepted on the two that do start one, which is the floor under the
// test above: without this, "refuses the flag" could be satisfied by refusing
// it everywhere.
func TestASpawnFlagIsAcceptedOnEveryVerbThatStartsASession(t *testing.T) {
	for _, verb := range spawningVerbs {
		for _, flag := range knownFlags {
			args := append([]string{verb}, legalArgsFor(t, flag)...)
			if _, _, err := spawnFlags(args); err != nil {
				t.Errorf("%s starts a session and refused %v: %v", verb, args, err)
			}
		}
	}
}

// legalArgsFor is an invocation the flag under test actually survives, so a test
// about *verbs* cannot be satisfied by a refusal about anything else.
//
// The whole invocation rather than one value, because one flag is legal only
// beside another: flagsAgree refuses a `--debug` with no log to write to, so a
// bare one would refuse in both loops and prove nothing in either.
//
// A table keyed by flag name rather than a guess, and every flag has to be in
// it: a flag added to knownFlags with no entry here fails loudly instead of
// being skipped by the two loops above.
func legalArgsFor(t *testing.T, flag spawnFlag) []string {
	t.Helper()
	legal := map[string][]string{
		effortFlag:    {effortFlag, core.EffortMax},
		modelFlag:     {modelFlag, "opus"},
		worktreeFlag:  {worktreeFlag, "fix"},
		budgetFlag:    {budgetFlag, "5"},
		fallbackFlag:  {fallbackFlag, "sonnet"},
		addDirFlag:    {addDirFlag, "/repo/lib"},
		debugFlag:     {debugFlag, "api", debugFileFlag, "alex"},
		debugFileFlag: {debugFileFlag, "alex"},
	}
	args, ok := legal[flag.name]
	if !ok {
		t.Fatalf("%s is in knownFlags and legalArgsFor has no invocation for it: the loops that derive from knownFlags would test it with something it refuses, which proves nothing about the verb", flag.name)
	}
	if args[0] != flag.name || !flag.valid(args[1]) {
		t.Fatalf("legalArgsFor gives %s the invocation %v, which does not start with the flag and a value its own validator accepts", flag.name, args)
	}
	return args
}

// The budget is validated at the CLI as well as at the daemon, and neither check
// is redundant: this one turns a typo into a sentence, the daemon's is what makes
// the wire field safe against a client that never ran this code.
func TestSpawnFlagsRefusesABudgetThatIsNotAnAmount(t *testing.T) {
	for _, amount := range []string{"lots", "0", "-1", "$5", ""} {
		if _, _, err := spawnFlags([]string{"new", budgetFlag, amount}); err == nil {
			t.Errorf("%s %q was accepted", budgetFlag, amount)
		}
	}
	if _, _, err := spawnFlags([]string{"new", budgetFlag}); err == nil {
		t.Errorf("a bare %s with no amount was accepted", budgetFlag)
	}
	for _, amount := range []string{"0.25", "5", "12.50"} {
		_, opts, err := spawnFlags([]string{"new", budgetFlag, amount})
		if err != nil {
			t.Errorf("%s %q was refused: %v", budgetFlag, amount, err)
		} else if opts.MaxBudgetUSD != amount {
			t.Errorf("budget = %q, want %q - the operator's own spelling reaches the argv", opts.MaxBudgetUSD, amount)
		}
	}
}

// A chain is passed through whole, separators included, because it is one argv
// word. The one refusal is a link naming nothing.
func TestSpawnFlagsTakesAChainAndRefusesAnEmptyLink(t *testing.T) {
	_, opts, err := spawnFlags([]string{"new", "alex", fallbackFlag, "sonnet,haiku"})
	if err != nil {
		t.Fatalf("a chain was refused: %v", err)
	}
	if opts.FallbackModel != "sonnet,haiku" {
		t.Errorf("chain = %q, want the whole comma-separated word", opts.FallbackModel)
	}
	for _, chain := range []string{"opus,", ",opus", "opus,,haiku", ""} {
		if _, _, err := spawnFlags([]string{"new", fallbackFlag, chain}); err == nil {
			t.Errorf("%s %q was accepted; a link naming nothing reaches claude as a model with no name", fallbackFlag, chain)
		}
	}
}

// A chain naming a model this build has never heard of is passed through, for
// TestSpawnFlagsPassesAModelItDoesNotRecognise's reason one flag over: there is
// no knowable set, so a parser that refused one would block every model released
// after it was written.
func TestSpawnFlagsPassesAChainItDoesNotRecognise(t *testing.T) {
	const chain = "claude-not-invented-yet,claude-also-not"
	_, opts, err := spawnFlags([]string{"new", fallbackFlag, chain})
	if err != nil {
		t.Fatalf("an unrecognised chain was refused: %v", err)
	}
	if opts.FallbackModel != chain {
		t.Errorf("chain = %q, want %q", opts.FallbackModel, chain)
	}
}

// The usage text names both flags and every verb they work on, and it derives
// the levels rather than spelling them - so a level added to core appears here
// without anybody remembering to edit a string.
func TestTheUsageTextNamesTheFlagsAndDerivesTheirValues(t *testing.T) {
	for _, flag := range knownFlags {
		if !strings.Contains(usage, flag.name) {
			t.Errorf("the usage text does not name %s", flag.name)
		}
	}
	for _, verb := range spawningVerbs {
		if !strings.Contains(usage, verb) {
			t.Errorf("the usage text does not name %q, which takes the flags", verb)
		}
	}
	for _, level := range core.EffortLevels {
		if !strings.Contains(usage, level) {
			t.Errorf("the usage text does not name the level %q; it should be derived from core", level)
		}
	}
}

// runGuard calls run and turns a panic into an error, so a regression fails as
// a test rather than as a crashed suite.
func runGuard(t *testing.T, args []string) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("run(%v) panicked: %v", args, r)
			err = nil
		}
	}()
	return run(args, io.Discard)
}

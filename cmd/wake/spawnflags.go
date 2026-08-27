package main

// The flags a spawn takes, and the only flag parsing in this binary.
//
// Hand-rolled rather than flag.FlagSet, and that is the smaller change here:
// every verb's arity is checked by checkArity against a plain []string, and a
// FlagSet would have to be threaded through all nine of them to keep one honest
// error message. This strips what it understands and hands the rest back, so
// `wake new alex --effort max` and `wake new --effort max alex` both leave
// exactly ["new", "alex"] for the arity check that already exists.

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	effortFlag    = "--effort"
	modelFlag     = "--model"
	worktreeFlag  = "--worktree"
	budgetFlag    = "--max-budget-usd"
	fallbackFlag  = "--fallback-model"
	addDirFlag    = "--add-dir"
	debugFlag     = "--debug"
	debugFileFlag = "--debug-file"
)

// spawningVerbs are the verbs that start a session, and so the verbs a spawn
// flag means something on.
//
// Named rather than spelled inline: a flag silently ignored on a verb that
// starts nothing is configuration the operator believes they applied.
//
// **`fork` is deliberately not here.** It starts a session, but the daemon
// reads neither field off a fork frame - rpc.Frame.Effort says so in as many
// words - so accepting one would be exactly the failure this list exists to
// prevent, arriving through the verb that looks most entitled to it.
var spawningVerbs = []string{cmdNew, cmdManager}

// spawnOpts is the configuration an invocation carries to a new session.
//
// Empty means "Wake chose nothing", which is what it means everywhere else:
// the flag is left off the argv and claude applies its own default.
type spawnOpts struct {
	Effort string
	Model  string

	// Worktree is a git worktree to create and run the session in, by name.
	// Empty is a session that runs where the client is, which is every session
	// this project started before the flag existed.
	Worktree string

	// MaxBudgetUSD caps what the session may spend and FallbackModel is the
	// chain it fails over to when Model is overloaded. Both matter at fleet
	// scale and nowhere else - thirty unbudgeted agents, and one overloaded
	// model stopping all thirty at once - which is why they are here rather
	// than on a runtime command: there is no runtime command for either.
	MaxBudgetUSD  string
	FallbackModel string

	// AddDir are directories outside the spawn directory this session's tools
	// may also reach. The one repeatable flag, because claude's own is variadic
	// and a session's reach is a list rather than a choice.
	AddDir []string

	// Debug is a category filter for this session's debug logging and DebugFile
	// is what to call the log - a name, not a path: the daemon places it beside
	// its own socket. See rpc/paths.go for why the wire may not carry the path.
	Debug     string
	DebugFile string
}

// chose reports whether this invocation configured anything at all.
//
// A method rather than `opts == (spawnOpts{})`, which stopped compiling the day
// a flag became repeatable: a struct holding a slice is not comparable. Derived
// from the fields rather than listed, so the flag after the next one is covered
// by construction.
func (o spawnOpts) chose() bool { return !reflect.ValueOf(o).IsZero() }

// spawnFlag is one flag this parser understands.
//
// A table rather than an arm each, so that adding the second flag was an entry
// rather than a second copy of the loop below - and so the two cannot drift in
// how they refuse a missing value or a contradiction.
type spawnFlag struct {
	name string
	// into is where a single value lands, so the loop stays one loop. A second
	// one that disagrees is a refusal: one of them would otherwise be silently
	// ignored.
	into func(*spawnOpts) *string
	// onto is where a **repeatable** value accumulates, and exactly one of the
	// two is set. The table was widened once, for --add-dir, because claude's
	// own flag is variadic: a session's reach is a list, so a second directory
	// is not a contradiction the way a second effort is. An exact repeat is
	// kept rather than folded - it costs one duplicate argv word and no
	// behaviour, where deduplicating would be a rule to remember.
	onto func(*spawnOpts) *[]string
	// valid decides whether a value may be carried at all. The answers differ
	// in kind: effort's set is closed, model's is unknowable, a path's is
	// neither, which is why this is a function rather than a list.
	valid func(string) bool
	// legal is what a refusal says the values are. For model it names examples
	// rather than the permitted set, because there is no permitted set.
	legal func() string
}

// take puts one value into the options, or says why it cannot.
func (f spawnFlag) take(o *spawnOpts, value string) error {
	if f.onto != nil {
		held := f.onto(o)
		*held = append(*held, value)
		return nil
	}
	held := f.into(o)
	if *held != "" && *held != value {
		return fmt.Errorf("%s given twice, as %q and %q", f.name, *held, value)
	}
	*held = value
	return nil
}

var knownFlags = []spawnFlag{
	{
		name:  effortFlag,
		into:  func(o *spawnOpts) *string { return &o.Effort },
		valid: core.ValidEffort,
		legal: func() string { return "one of " + list(core.EffortLevels) },
	},
	{
		name:  worktreeFlag,
		into:  func(o *spawnOpts) *string { return &o.Worktree },
		valid: func(v string) bool { return rpc.ValidWorktreeName(v) == nil },
		legal: func() string {
			return "a name, which becomes one directory under the repository: letters, digits, dot, dash and underscore"
		},
	},
	{
		name:  modelFlag,
		into:  func(o *spawnOpts) *string { return &o.Model },
		valid: core.ValidModel,
		legal: func() string { return "an alias such as " + list(core.ModelAliases) + ", or a model's full name" },
	},
	{
		name:  budgetFlag,
		into:  func(o *spawnOpts) *string { return &o.MaxBudgetUSD },
		valid: core.ValidBudget,
		legal: func() string { return "an amount in dollars above zero, such as 0.25 or 5" },
	},
	{
		name:  fallbackFlag,
		into:  func(o *spawnOpts) *string { return &o.FallbackModel },
		valid: core.ValidFallbackModel,
		legal: func() string {
			return "one model or several separated by commas, tried in order, each named as " + modelFlag + " names one"
		},
	},
	{
		name:  addDirFlag,
		onto:  func(o *spawnOpts) *[]string { return &o.AddDir },
		valid: func(v string) bool { return rpc.ValidAddDir(v) == nil },
		legal: func() string {
			return "an absolute path to a directory this session's tools may also reach, written out rather than relative; repeat the flag for a second one"
		},
	},
	{
		name:  debugFlag,
		into:  func(o *spawnOpts) *string { return &o.Debug },
		valid: core.ValidDebugFilter,
		legal: func() string {
			return "categories separated by commas, each optionally negated with !, as in api,hooks or !1p,!file"
		},
	},
	{
		name:  debugFileFlag,
		into:  func(o *spawnOpts) *string { return &o.DebugFile },
		valid: func(v string) bool { return rpc.ValidDebugFileName(v) == nil },
		legal: func() string {
			return "a name, which becomes one file in the fleet's own debug directory: letters, digits, dot, dash and underscore"
		},
	},
}

// spawnFlags strips the flags off an invocation, returning the words that are
// left and what was chosen.
//
// Values are validated here as well as at the daemon, and neither check is
// redundant: this one turns a typo into a sentence naming what is legal, and
// the daemon's is what makes the wire fields safe against a client that never
// ran this code.
func spawnFlags(args []string) (rest []string, opts spawnOpts, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		flag := flagNamed(args[i])
		if flag == nil {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) {
			return nil, spawnOpts{}, fmt.Errorf("%s needs a value: %s", flag.name, flag.legal())
		}
		i++
		if !flag.valid(args[i]) {
			return nil, spawnOpts{}, fmt.Errorf("unknown %s %q: %s", strings.TrimPrefix(flag.name, "--"), args[i], flag.legal())
		}
		if err := flag.take(&opts, args[i]); err != nil {
			return nil, spawnOpts{}, err
		}
	}
	if err := flagsAgree(opts); err != nil {
		return nil, spawnOpts{}, err
	}
	if err := flagsSuitFor(rest, opts); err != nil {
		return nil, spawnOpts{}, err
	}
	return rest, opts, nil
}

// flagsAgree refuses a combination whose parts are each legal.
//
// One rule, and it is the one an operator reads as working. `--debug` on its
// own produces nothing readable in the mode every agent runs in - recorded in
// internal/core/debug.go - so it is logging somebody turned on and no log
// anywhere. The daemon refuses it too, and that is the check that makes the
// wire safe; this one names the flag that fixes it before a socket is dialled.
func flagsAgree(opts spawnOpts) error {
	if opts.Debug != "" && opts.DebugFile == "" {
		return fmt.Errorf("%s needs %s <name>: on its own it writes no log anywhere that can be read", debugFlag, debugFileFlag)
	}
	return nil
}

// flagNamed is the flag this word is, or nil for a word that is not one.
func flagNamed(word string) *spawnFlag {
	for i := range knownFlags {
		if knownFlags[i].name == word {
			return &knownFlags[i]
		}
	}
	return nil
}

// flagsSuitFor refuses a spawn flag on a verb that starts no session.
//
// Here rather than in run so that the refusal is one sentence in one place for
// both flags, and so a test can reach it without a socket. run still owns the
// case this cannot see: an invocation that was nothing but flags, which leaves
// no verb to name.
func flagsSuitFor(rest []string, opts spawnOpts) error {
	if !opts.chose() || len(rest) == 0 {
		return nil
	}
	if slices.Contains(spawningVerbs, rest[0]) {
		return nil
	}
	return fmt.Errorf("%s are for %s, which start a session; %s does not\n\n%s",
		list(flagNames()), list(spawningVerbs), rest[0], usage)
}

// flagNames is every flag this parser understands, derived rather than spelled
// again - the refusal above named three while the table held three, and the
// fourth would have been silently missing from the sentence that lists them.
func flagNames() []string {
	names := make([]string, 0, len(knownFlags))
	for _, flag := range knownFlags {
		names = append(names, flag.name)
	}
	return names
}

// list is a slice as a sentence, derived from its argument rather than spelled
// again at each call site.
func list(items []string) string {
	var s string
	for i, item := range items {
		switch {
		case i == 0:
			s = item
		case i == len(items)-1:
			s += " or " + item
		default:
			s += ", " + item
		}
	}
	return s
}

package ui

// The flags `/new` takes, stripped out of the draft before the words that are
// left are counted as a name and a directory.
//
// Split out of new.go, which owns the grammar - `[name] [in <dir>]` - and the
// frame. These are the shell verb's own tokens rather than a keyword like `in`,
// because every keyword shape is ambiguous against a value: `/new sydney
// worktree fix` cannot be told from an agent called worktree.
//
// **Validated here as well as at the daemon, and neither check is redundant**:
// this turns a typo into a sentence before a socket is dialled, and the
// daemon's is what makes the wire field safe against a client that never ran
// this code. Same rule cmd/wake/spawnflags.go states for the shell verb.
//
// A value is one whitespace-separated word, which is what a composer can give
// it: `in <dir>` joins its trailing fields because a directory may hold spaces
// and there is nothing after it, and a flag has no such luxury.

import (
	"fmt"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// worktreeFlag is how `/new` asks for a worktree, addDirFlag for a
	// directory outside the session's own, and the two debug flags for a log.
	// The shell verb spells the same six; internal/ui and cmd/wake may not
	// import each other, and the manual holds them to one grammar.
	worktreeFlag  = "--worktree"
	addDirFlag    = "--add-dir"
	debugFlag     = "--debug"
	debugFileFlag = "--debug-file"

	// The spend pair, spelled as the shell verb spells them. They are here
	// because the room is the ordinary spawn path and neither has a runtime
	// command - an agent spawned without a budget is uncapped for its whole
	// life, which is deferred.md's 2026-08-20 entry.
	budgetNewFlag   = "--max-budget-usd"
	fallbackNewFlag = "--fallback-model"
)

// newFlags is what `/new` collected out of the draft.
type newFlags struct {
	Worktree      string
	AddDir        []string
	Debug         string
	DebugFile     string
	MaxBudgetUSD  string
	FallbackModel string
}

// takeNewFlags strips the flags out of the words and returns the rest, so the
// shapes in new.go still count names and directories in the words the operator
// meant as names and directories.
//
// A table rather than an arm each, so a flag added later is an entry rather
// than a second copy of the loop - and so the four cannot drift in how they
// refuse a missing value.
func takeNewFlags(fields []string) (rest []string, flags newFlags, err error) {
	rest = make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		flag := newFlagNamed(fields[i])
		if flag == nil {
			rest = append(rest, fields[i])
			continue
		}
		if i+1 >= len(fields) {
			return nil, newFlags{}, fmt.Errorf("%s needs a value: %s", flag.name, newUsage)
		}
		i++
		// A flag standing where a value should be is a sentence somebody did
		// not finish, and --add-dir is the one that would otherwise swallow it
		// in silence: absoluteDir turns any word into a path, so `--add-dir
		// --worktree fix` would start an agent called fix with a directory
		// called --worktree. The other three refuse a leading dash for
		// themselves; this says which word was the problem.
		if next := newFlagNamed(fields[i]); next != nil {
			return nil, newFlags{}, fmt.Errorf("%s needs a value and %s is another flag: %s", flag.name, next.name, newUsage)
		}
		if err := flag.take(&flags, fields[i]); err != nil {
			return nil, newFlags{}, fmt.Errorf("%s: %w", flag.name, err)
		}
	}
	if flags.Debug != "" && flags.DebugFile == "" {
		// The one combination whose parts are each legal. `--debug` on its own
		// writes no log anywhere that can be read - see internal/core/debug.go.
		return nil, newFlags{}, fmt.Errorf("%s needs %s <name>: on its own it writes no log anywhere that can be read", debugFlag, debugFileFlag)
	}
	return rest, flags, nil
}

// newFlag is one flag `/new` understands: where its value lands, and what may
// be one.
type newFlag struct {
	name string
	take func(*newFlags, string) error
}

// newFlagNamed is the flag this word is, or nil for a word that is not one.
//
// Case-insensitive, which is the composer's own rule for `in`: what is typed
// into a chat surface is not an argv.
func newFlagNamed(word string) *newFlag {
	for i := range newFlagTable {
		if strings.EqualFold(newFlagTable[i].name, word) {
			return &newFlagTable[i]
		}
	}
	return nil
}

var newFlagTable = []newFlag{
	{name: worktreeFlag, take: func(f *newFlags, v string) error {
		if err := rpc.ValidWorktreeName(v); err != nil {
			return err
		}
		return once(&f.Worktree, v)
	}},
	// The one repeatable flag: claude's own is variadic, and a session's reach
	// is a list rather than a choice. Resolved against this client's own
	// directory first, for absoluteDir's reason - the daemon refuses a relative
	// path rather than resolving it against the wrong process's cwd.
	{name: addDirFlag, take: func(f *newFlags, v string) error {
		dir, err := absoluteDir(v)
		if err != nil {
			return err
		}
		if err := rpc.ValidAddDir(dir); err != nil {
			return err
		}
		f.AddDir = append(f.AddDir, dir)
		return nil
	}},
	{name: debugFlag, take: func(f *newFlags, v string) error {
		if !core.ValidDebugFilter(v) {
			return fmt.Errorf("%q is not a filter: categories separated by commas, each optionally negated with !, as in api,hooks or !1p,!file", v)
		}
		return once(&f.Debug, v)
	}},
	{name: debugFileFlag, take: func(f *newFlags, v string) error {
		if err := rpc.ValidDebugFileName(v); err != nil {
			return err
		}
		return once(&f.DebugFile, v)
	}},
	{name: budgetNewFlag, take: func(f *newFlags, v string) error {
		if !core.ValidBudget(v) {
			return fmt.Errorf("%q is not a dollar amount: digits with at most one dot, above zero, as in 5 or 0.25", v)
		}
		return once(&f.MaxBudgetUSD, v)
	}},
	{name: fallbackNewFlag, take: func(f *newFlags, v string) error {
		if !core.ValidFallbackModel(v) {
			return fmt.Errorf("%q is not a model chain: model names separated by commas, as in sonnet or opus,sonnet", v)
		}
		return once(&f.FallbackModel, v)
	}},
}

// once takes a value that may be given at most one way, refusing a second that
// disagrees: one of the two would otherwise be silently ignored.
func once(held *string, value string) error {
	if *held != "" && *held != value {
		return fmt.Errorf("given twice, as %q and %q", *held, value)
	}
	*held = value
	return nil
}

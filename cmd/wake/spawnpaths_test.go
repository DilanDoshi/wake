package main

import (
	"strings"
	"testing"
)

// --add-dir is the one repeatable flag, because claude's own is variadic and
// the reach of a session is a list rather than a choice.
func TestAddDirAccumulatesInsteadOfConflicting(t *testing.T) {
	rest, opts, err := spawnFlags([]string{"new", addDirFlag, "/repo/lib", "alex", addDirFlag, "/repo/docs"})
	if err != nil {
		t.Fatalf("spawnFlags: %v", err)
	}
	if strings.Join(rest, " ") != "new alex" {
		t.Errorf("rest = %v, want [new alex]", rest)
	}
	if strings.Join(opts.AddDir, " ") != "/repo/lib /repo/docs" {
		t.Errorf("AddDir = %v, want both directories in the order they were written", opts.AddDir)
	}
}

// The single-value flags refuse a second, different value because one of them
// would be silently ignored. A repeatable one has no such ambiguity, which is
// the whole reason it is a second kind of entry in the table.
func TestASecondEffortIsAConflictAndASecondDirectoryIsNot(t *testing.T) {
	if _, _, err := spawnFlags([]string{"new", debugFileFlag, "one", debugFileFlag, "two"}); err == nil {
		t.Error("two debug log names were accepted; one of them would be silently ignored")
	}
	if _, _, err := spawnFlags([]string{"new", addDirFlag, "/a", addDirFlag, "/b"}); err != nil {
		t.Errorf("two added directories were refused: %v", err)
	}
}

// Validated here as well as at the daemon, and neither is redundant: this turns
// a typo into a sentence before a socket is dialled.
func TestSpawnFlagsRefusesAPathItWouldNotSend(t *testing.T) {
	for _, args := range [][]string{
		{"new", addDirFlag, "lib"},
		{"new", addDirFlag, "../lib"},
		{"new", addDirFlag},
		{"new", debugFileFlag, "../../.zshrc"},
		{"new", debugFileFlag, "/tmp/log"},
		{"new", debugFileFlag},
		{"new", debugFlag, "api;rm", debugFileFlag, "alex"},
		{"new", debugFlag, "api,", debugFileFlag, "alex"},
	} {
		if _, _, err := spawnFlags(args); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

// A filter with nothing to write to is refused here rather than left to the
// daemon, because it is the one combination an operator reads as "logging is
// on" and gets no log from.
func TestADebugFilterWithNoLogNameIsRefused(t *testing.T) {
	_, _, err := spawnFlags([]string{"new", debugFlag, "api"})
	if err == nil {
		t.Fatal("a filter with no log name was accepted")
	}
	if !strings.Contains(err.Error(), debugFileFlag) {
		t.Errorf("the refusal %q does not name the flag that fixes it", err)
	}
	if _, _, err := spawnFlags([]string{"new", debugFlag, "api", debugFileFlag, "alex"}); err != nil {
		t.Errorf("a filter with a log name was refused: %v", err)
	}
	if _, _, err := spawnFlags([]string{"new", debugFileFlag, "alex"}); err != nil {
		t.Errorf("a log name on its own was refused: %v", err)
	}
}

// Exactly one of into and onto, on every entry. The table's comment says so and
// nothing enforced it: a flag with neither panics in take on the first use, and
// a flag with both silently drops one of the two destinations.
func TestEveryKnownFlagHasExactlyOneDestination(t *testing.T) {
	for _, flag := range knownFlags {
		switch {
		case flag.into == nil && flag.onto == nil:
			t.Errorf("%s has no destination: take would dereference a nil function the first time it is used", flag.name)
		case flag.into != nil && flag.onto != nil:
			t.Errorf("%s has both a single and a repeatable destination, so one of them is dead", flag.name)
		}
		if flag.valid == nil || flag.legal == nil {
			t.Errorf("%s cannot say what it accepts, so its refusal names nothing", flag.name)
		}
	}
	if len(knownFlags) == 0 {
		t.Fatal("knownFlags is empty, so this asserted nothing")
	}
}

// chose is what replaced `opts == (spawnOpts{})` when a field became a slice,
// and it has to keep answering for every field rather than the ones it was
// written against.
func TestChoseAnswersForEveryField(t *testing.T) {
	if (spawnOpts{}).chose() {
		t.Error("an invocation that configured nothing reports that it did")
	}
	for _, opts := range []spawnOpts{
		{Effort: "max"}, {Model: "opus"}, {Worktree: "fix"},
		{MaxBudgetUSD: "5"}, {FallbackModel: "sonnet"},
		{AddDir: []string{"/repo"}}, {Debug: "api"}, {DebugFile: "alex"},
	} {
		if !opts.chose() {
			t.Errorf("%+v reports that it configured nothing", opts)
		}
	}
}

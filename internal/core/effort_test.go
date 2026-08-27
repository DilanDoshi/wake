package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidEffortAdmitsExactlyTheLevels(t *testing.T) {
	for _, level := range EffortLevels {
		if !ValidEffort(level) {
			t.Errorf("ValidEffort(%q) = false for a level in EffortLevels", level)
		}
	}
	for _, level := range []string{"", "LOW", "Max", "ultra", "high ", "low;rm -rf /", "--model"} {
		if ValidEffort(level) {
			t.Errorf("ValidEffort(%q) = true; the set is closed so nothing else reaches an argv", level)
		}
	}
}

// The empty string is how "nobody chose" is spelled, and it must not be a
// level: a caller that passed it through would emit `--effort ""`.
func TestTheEmptyLevelIsNotALevel(t *testing.T) {
	if ValidEffort("") {
		t.Error("the empty string is a valid effort; it means nobody chose")
	}
}

// A level that passed validation reaches the argv as its own word, and nothing
// else does. This is the property that makes the wire field safe.
func TestOnlyAValidLevelReachesTheArgv(t *testing.T) {
	for _, level := range EffortLevels {
		args := argsFor(t, Config{SessionID: "s1", Effort: level})
		i := indexOf(args, "--effort")
		if i < 0 {
			t.Fatalf("level %q produced no --effort in %v", level, args)
		}
		if args[i+1] != level {
			t.Errorf("--effort carried %q, want %q", args[i+1], level)
		}
	}
	if args := argsFor(t, Config{SessionID: "s1"}); indexOf(args, "--effort") >= 0 {
		t.Errorf("a config with no effort emitted --effort: %v", args)
	}
}

// argsFor is the command line a config produces.
func argsFor(t *testing.T, cfg Config) []string {
	t.Helper()
	args, err := (&Session{cfg: cfg}).buildArgs()
	if err != nil {
		t.Fatalf("building args for %+v: %v", cfg, err)
	}
	return args
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// The levels are spelled once. The permission-mode values are duplicated across
// two packages and CLAUDE.md names that an open leak; this keeps effort from
// becoming the second one, and fails with the file that copied it.
func TestTheEffortLevelsAreSpelledOnlyInCore(t *testing.T) {
	var offenders []string
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		if filepath.Base(path) == "effort.go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// "high" and "max" are ordinary English; the pair that identifies a
		// copied list is the two Wake would never write by accident.
		if strings.Contains(string(raw), `"xhigh"`) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	for _, path := range offenders {
		t.Errorf("%s spells an effort level; ask core.ValidEffort instead - see effort.go for why the set has one home", path)
	}
}

// The flag takes five levels and the command takes seven, so the two predicates
// are not interchangeable. Swapping them puts a level on an argv claude refuses,
// or drops one the operator really typed.
func TestTheCommandSetIsWiderThanTheFlagSet(t *testing.T) {
	for _, level := range EffortLevels {
		if !ValidEffortCommand(level) {
			t.Errorf("%q is a flag level the command set does not admit", level)
		}
	}
	for _, extra := range []string{EffortUltracode, EffortAuto} {
		if !ValidEffortCommand(extra) {
			t.Errorf("%q is in claude's /effort usage line and this build refuses it", extra)
		}
		if ValidEffort(extra) {
			t.Errorf("%q reached the argv predicate; --effort takes five levels and refuses it", extra)
		}
	}
}

// Neither predicate admits the empty string. It means "Wake chose nothing"
// everywhere in this build, which is the absence of a level rather than one.
func TestNeitherEffortSetAdmitsTheEmptyString(t *testing.T) {
	if ValidEffort("") || ValidEffortCommand("") {
		t.Fatal("the empty string is not a level; it is the absence of one")
	}
}

// A level still reaches the argv, whoever built the Config.
func TestAValidEffortStillReachesTheArgv(t *testing.T) {
	for _, level := range EffortLevels {
		args := argsFor(t, Config{SessionID: "s1", Effort: level})
		if i := indexOf(args, "--effort"); i < 0 || args[i+1] != level {
			t.Errorf("level %q did not reach the argv: %v", level, args)
		}
	}
}

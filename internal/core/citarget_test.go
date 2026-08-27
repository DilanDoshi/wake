package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `make ci` is a second copy of the workflow's step list, and this is what
// stops it from becoming a lie.
//
// The target exists because a macOS runner minute bills at ten times a Linux
// one and a spend cap stops CI dead, so there has to be a way to run what CI
// runs without a runner. The cost of that is the thing CLAUDE.md otherwise
// forbids outright: two places stating the same list, either of which can be
// edited alone. A step added to the workflow and not to the target would leave
// `make ci` reporting green over a check it never ran - and it would report it
// most confidently on exactly the days it is being trusted instead of CI.
//
// So the two are read against each other, both directions. A workflow step
// missing from the target is the drift above; a target command missing from
// the workflow is a local check nobody is actually gated on, which is the same
// defect wearing the other hat.
//
// It lives in `internal/core` beside the conflict-marker and airlock guards
// for that guard's own stated reason: it is a property of the whole tree,
// asserted from the one package that already walks it, and has nothing to do
// with Claude's JSON.
const (
	workflowFile = ".github/workflows/ci.yml"
	makefileFile = "Makefile"
)

// ciOnlySteps are workflow steps `make ci` deliberately does not run, keyed by
// the name the workflow gives them and carrying the reason.
//
// Keyed on the step name rather than counted, because a count says a step was
// skipped and not which one - and the whole question here is which. A step
// that leaves this map, or one that arrives in the workflow without being
// added to it, fails below: the exemption is a ruling somebody has to make
// rather than a hole that widens on its own.
var ciOnlySteps = map[string]string{
	"Pin TMPDIR": "it exports the runner's own temp root into $GITHUB_ENV. " +
		"Locally TMPDIR is already set, and whether it is short enough for darwin's " +
		"104-byte sun_path is measured by internal/daemon's " +
		"TestEverySocketPathThisSuiteBuildsFitsInSunPath against the paths the suite " +
		"really builds - so there is nothing for this target to decide.",
}

// TestMakeCIRunsEveryStepTheWorkflowRuns holds the two lists together.
func TestMakeCIRunsEveryStepTheWorkflowRuns(t *testing.T) {
	workflow, _ := workflowCommands(t)
	target := makeCICommands(t)

	inTarget := make(map[string]bool, len(target))
	for _, cmd := range target {
		inTarget[cmd] = true
	}
	for _, cmd := range workflow {
		if !inTarget[cmd] {
			t.Errorf("the workflow runs %q and `make ci` does not.\n"+
				"Add it to the ci target in the Makefile, or - if it cannot run off a runner - "+
				"give its step a name and an entry in ciOnlySteps saying why.\n"+
				"`make ci` exists to stand in for CI when CI cannot run, and a stand-in that "+
				"skips a step in silence is worse than none.", cmd)
		}
	}

	inWorkflow := make(map[string]bool, len(workflow))
	for _, cmd := range workflow {
		inWorkflow[cmd] = true
	}
	for _, cmd := range target {
		if !inWorkflow[cmd] {
			t.Errorf("`make ci` runs %q and the workflow does not.\n"+
				"Either CI should be running it too - in which case add the step - or it does "+
				"not belong in a target whose whole claim is that it is what CI runs.", cmd)
		}
	}
}

// TestEveryCIOnlyStepIsOneTheWorkflowStillHas keeps the exemptions honest from
// the other side. An entry left behind after its step was renamed or deleted
// reads as a considered decision and is worth nothing.
func TestEveryCIOnlyStepIsOneTheWorkflowStillHas(t *testing.T) {
	_, blocked := workflowCommands(t)

	present := make(map[string]bool, len(blocked))
	for _, name := range blocked {
		present[name] = true
		if _, ok := ciOnlySteps[name]; !ok {
			t.Errorf("the workflow step %q runs a shell block that `make ci` does not reproduce, "+
				"and nothing says whether that is deliberate.\n"+
				"Either give the target the same commands, or add %q to ciOnlySteps with the "+
				"reason it cannot run off a runner.", name, name)
		}
	}
	for name := range ciOnlySteps {
		if !present[name] {
			t.Errorf("ciOnlySteps exempts the workflow step %q, and the workflow has no such step.\n"+
				"It was renamed or removed; delete the exemption or point it at the new name. "+
				"A stale exemption is indistinguishable from a considered one.", name)
		}
	}
}

// workflowCommands returns every command the workflow runs as a single-line
// `run:`, and the names of the steps whose `run:` is a block scalar.
//
// A line scan and not a YAML parse, deliberately: the alternative is a
// dependency in go.mod for one test, and what is being read here is a fixed
// handful of lines in a file this repository owns. The scan is strict about
// what it will not understand - a block whose step has no name arrives as ""
// and fails the exemption check above rather than passing quietly.
func workflowCommands(t *testing.T) (cmds []string, blockSteps []string) {
	t.Helper()

	path := filepath.Join(repoRoot, workflowFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", workflowFile, err)
	}

	name := ""
	for _, line := range strings.Split(string(b), "\n") {
		item := strings.TrimPrefix(strings.TrimSpace(line), "- ")
		switch {
		case strings.HasPrefix(item, "name:"):
			// Also matches the workflow's own top-level name, which is
			// harmless: it is overwritten by the first step's name, and a
			// block run is only ever reported under the name most recently
			// seen above it.
			name = strings.TrimSpace(strings.TrimPrefix(item, "name:"))
		case strings.HasPrefix(item, "run:"):
			rest := strings.TrimSpace(strings.TrimPrefix(item, "run:"))
			if rest == "" || strings.HasPrefix(rest, "|") || strings.HasPrefix(rest, ">") {
				blockSteps = append(blockSteps, name)
			} else {
				cmds = append(cmds, rest)
			}
			// Cleared so a nameless step cannot inherit the previous step's
			// name and land on its exemption.
			name = ""
		}
	}

	if len(cmds) == 0 {
		t.Fatalf("no single-line `run:` commands found in %s - the scan below no longer "+
			"understands the file it is reading, so this test proves nothing", workflowFile)
	}
	return cmds, blockSteps
}

// makeCICommands returns the commands in the Makefile's ci target.
//
// `$(MAKE)` is normalised to `make`: the recipe has to use the variable so
// flags reach the sub-make, and the workflow spells the same step `make test`.
// Comments and echoes are not commands and are skipped - the target opens with
// one, reporting the TMPDIR that CI pins for itself.
func makeCICommands(t *testing.T) []string {
	t.Helper()

	path := filepath.Join(repoRoot, makefileFile)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", makefileFile, err)
	}

	var cmds []string
	found, inRecipe := false, false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "ci:") {
			found, inRecipe = true, true
			continue
		}
		if !inRecipe {
			continue
		}
		// A blank line inside a recipe is ignored by make rather than ending
		// it, so it is ignored here too.
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		cmd := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "\t"), "@"))
		if cmd == "" || strings.HasPrefix(cmd, "#") || strings.HasPrefix(cmd, "echo ") {
			continue
		}
		cmds = append(cmds, strings.ReplaceAll(cmd, "$(MAKE)", "make"))
	}

	if !found {
		t.Fatalf("no `ci:` target in the %s - `make ci` is what this file's guard is about", makefileFile)
	}
	if len(cmds) == 0 {
		t.Fatalf("the `ci:` target in the %s has no commands, so it would pass by running nothing", makefileFile)
	}
	return cmds
}

// TestMakeCIBuildsBeforeItTests pins the one ordering the workflow argues for
// in writing: the Build step runs before Test "because the tests do not build
// cmd/wake and would not notice it was broken".
//
// The set checks above are order-blind on purpose - the workflow's two jobs
// run in parallel, so there is no global order to compare against - which
// leaves this one relation to state on its own.
func TestMakeCIBuildsBeforeItTests(t *testing.T) {
	cmds := makeCICommands(t)

	at := func(want string) int {
		for i, cmd := range cmds {
			if cmd == want {
				return i
			}
		}
		t.Fatalf("`make ci` does not run %q at all\nit runs: %v", want, cmds)
		return -1
	}

	if build, test := at("make build"), at("make test"); build > test {
		t.Errorf("`make ci` runs `make test` (step %d) before `make build` (step %d).\n"+
			"The workflow orders them the other way and says why: the tests do not build "+
			"cmd/wake, so a broken entrypoint passes the suite. .gitignore once excluded "+
			"that whole directory.", test, build)
	}
}

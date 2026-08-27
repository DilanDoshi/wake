// The identity flags have one home, and the whole tree is checked against it.
//
// airlock_test.go's SCOPE paragraph records these as a leak of a different
// shape from Claude's JSON - the airlock polices wire *words*, and a CLI flag
// is not one - and says they want their own ruling. This is that ruling, and
// it is enforceable rather than stated: --resume, --fork-session and
// --session-id decide which of three unrecorded argv shapes a process gets,
// and two of the three fail silently. A second file spelling one of them is
// how a park path grows a --resume beside a --session-id.
//
// Two things about *how* it checks, each chosen against a specific way the
// obvious version is wrong here:
//
//   - It walks goFiles, the walk airlock_test.go already declares, rather than
//     a second filepath.WalkDir of its own. That walk skips .worktrees, and
//     this repository is developed in worktrees under its own root: a private
//     copy of the tree would otherwise be scanned as part of it, and every
//     branch carrying an argv.go would report the *other* branch's argv.go as a
//     second file spelling the flags. A tree-wide guard that fails because a
//     worktree exists is a guard somebody deletes.
//
//   - It reads string literals out of the AST, not bytes out of the file. These
//     non-test files name one of these flags in a comment, every one of them
//     explaining why the flag matters somewhere else:
//
//     cmd/wake/attach.go        why Dir is not optional
//     internal/rpc/wire.go      the same argument for Frame.Dir
//     internal/daemon/reap.go   what survives pid reuse
//     internal/daemon/reap_unix.go   why ps -ww is load-bearing
//     internal/daemon/spawn.go  why a spawn with no id is refused
//     internal/core/session.go  why ForkFrom is not "resume"
//
//     Prose cannot put a flag on a command line, and a guard that made those
//     sentences unwriteable would be paid for in comments deleted to get the
//     build green. It is the ruling airlock_test.go already made, for the same
//     reason and in the same words: it sees whole literals, so a word inside a
//     sentence is not policed. The list is written out rather than counted
//     because the first version of this comment said "five" and was wrong the
//     day it was written - session.go's own header has named --resume since
//     before the split - which is this repository's most-repeated defect
//     arriving inside the change that re-derived three other numbers correctly.
//
// Matching is substring-within-a-literal rather than equality, because a flag
// does not have to be its own argv element to reach one: SessionArgvMarkers
// builds "--session-id " with the id after it, and "--resume=<id>" is the same
// flag again. Equality sees neither.
//
// The 800-line hard max is here too, and for the same reason the flag rule is:
// it is the *other* half of why argv.go exists. session.go reached 767 with
// nothing watching, and the crossing was found by a planner reading a number in
// a note - which is how every unenforced rule in this repository has been
// found. Both checks walk the same tree through the same walk, so they share
// their floors rather than growing two copies that drift.
package core

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// identityFlags are the claude flags that name a session on the command line.
// --continue is here having never been sent: the help text names it beside
// --resume for both --fork-session and the --session-id rejection, so it is
// the next one somebody reaches for, and it should land in argv.go too.
var identityFlags = []string{"--session-id", "--resume", "--fork-session", "--continue"}

// identityFlagCount holds the list above to its size, so an exemption cannot
// grow quietly - the rule docs/notes/decisions.md gives for any list that
// genuinely has to be hand-written.
//
// This one genuinely is. The domain is claude's own command line, which no file
// in this repository declares; everything else here is derived from the tree.
const identityFlagCount = 4

// argvFile is the one non-test file allowed to spell them.
const argvFile = "internal/core/argv.go"

// minScannedGoFiles is airlock_test.go's own floor on the same walk, restated
// where this file depends on it: a walk that returned almost nothing would make
// every check below pass by finding nothing to check.
//
// It is not sufficient on its own and is deliberately kept anyway, as the
// cheap first line. goFiles skips directories by *base name at any depth*, so
// one entry added to its list - or a future internal/notes/, or a docs/
// directory inside any package - hides a whole package from this file's two
// guards and from the airlock's leak check at once. With internal/daemon
// skipped the walk still returns 60 files against a floor of 20, which is not
// a floor, it is a formality. The real floor is coverage, below.
const minScannedGoFiles = 20

// nonTestGoFiles is goFiles held to both floors.
func nonTestGoFiles(t *testing.T) []string {
	t.Helper()
	files := goFiles(t)
	if len(files) < minScannedGoFiles {
		t.Fatalf("walked %d non-test .go files, want the whole tree - the walk is broken and the checks over it are asserting nothing", len(files))
	}
	requireEveryPackageIsWalked(t, files)
	return files
}

// requireEveryPackageIsWalked holds the walk to the producer rather than to a
// count: go list is what decides which packages this module has, and a walk
// that misses one is a package no tree-wide guard in this repository can see.
//
// Asked of the toolchain rather than derived from the tree, because deriving it
// from the tree means writing a second walk with a second skip list, which is
// the thing being checked. A go that will not run is a hard failure and never a
// skip - the whole point is that this cannot be quietly satisfied.
func requireEveryPackageIsWalked(t *testing.T, files []string) {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{.Dir}} {{len .GoFiles}}", "./...")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list ./... in %s: %v", repoRoot, err)
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %s: %v", repoRoot, err)
	}

	walked := map[string]bool{}
	for _, rel := range files {
		walked[filepath.ToSlash(filepath.Dir(rel))] = true
	}

	packages := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		dir, count, ok := strings.Cut(line, " ")
		if !ok || count == "0" {
			// A package with no non-test files has nothing for the walk to
			// find, so its absence is not a hole.
			continue
		}
		rel, rerr := filepath.Rel(root, dir)
		if rerr != nil {
			t.Fatalf("relate %s to %s: %v", dir, root, rerr)
		}
		packages++
		if !walked[filepath.ToSlash(rel)] {
			t.Errorf("go list reports package %s and the walk found no non-test file in it: the tree-wide "+
				"guards over that walk - these two and the airlock's leak check - are all blind to that "+
				"package, and every one of them stays green", rel)
		}
	}
	if packages == 0 {
		t.Fatal("go list reported no package with non-test files: the producer scan is broken, and holding the walk to it asserts nothing")
	}
}

// identityFlags has to hold every flag the switch actually emits, and that half
// of it is derivable even though the list as a whole is not.
//
// The hand-written list was licensed on the grounds that the domain is claude's
// own command line, which nothing here declares. That is true of --continue,
// which nothing emits. It is false of the other three: identityArgs' return
// statements declare them, so a one-character typo - "--fork-sessionX" - can
// silently disarm a flag tree-wide with identityFlagCount none the wiser, and a
// second non-test file may then spell --fork-session with everything green.
//
// Read off the producer, over identityArgs' whole input space, the same way
// idMarkers reads the argv. The other direction is deliberately not checked:
// --continue is in the list precisely because nothing emits it yet.
func TestIdentityFlagsHoldsEveryFlagTheSwitchEmits(t *testing.T) {
	emitted := map[string]bool{}
	for _, from := range identityDomain() {
		for _, res := range identityDomain() {
			for _, id := range identityDomain() {
				args, err := NewSession(Config{ForkFrom: from.spelling, ResumeFrom: res.spelling, SessionID: id.spelling}).identityArgs()
				if err != nil {
					continue
				}
				for _, a := range args {
					if strings.HasPrefix(a, "--") {
						emitted[a] = true
					}
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("identityArgs emitted no flag over its whole input space: the producer scan is broken and the cross-check below holds over nothing")
	}
	for flag := range emitted {
		if !slices.Contains(identityFlags, flag) {
			t.Errorf("identityArgs emits %q and identityFlags does not hold it: the tree-wide guard is disarmed "+
				"for that flag, and a second non-test file may spell it with everything green", flag)
		}
	}
}

func TestTheIdentityFlagsAreSpelledOnlyInArgv(t *testing.T) {
	if len(identityFlags) != identityFlagCount {
		t.Fatalf("identityFlags holds %d flags and identityFlagCount says %d: update both deliberately", len(identityFlags), identityFlagCount)
	}

	found := 0
	for _, rel := range nonTestGoFiles(t) {
		for _, lit := range stringsIn(t, filepath.Join(repoRoot, rel)) {
			if lit.fromTag {
				continue
			}
			for _, flag := range identityFlags {
				if !strings.Contains(lit.text, flag) {
					continue
				}
				if rel == argvFile {
					found++
					continue
				}
				t.Errorf("%s:%d spells %q. The identity flags decide which of three argv shapes a "+
					"process gets and two of the three fail silently, so they belong in %s and are "+
					"reached from elsewhere through SessionArgvMarkers", rel, lit.line, flag, argvFile)
			}
		}
	}
	// The floor: a walk that found nothing reads as a walk that found nothing
	// wrong, which is this project's most-repeated way of shipping a guard that
	// cannot fail.
	if found == 0 {
		t.Fatalf("no identity flag appears in %s at all: the scan is broken, or the switch moved", argvFile)
	}
}

// idMarkers is every "<flag> <value>" a given id can appear in, read off the
// argvs identityArgs actually builds over its whole input space.
//
// This is the producer, and it is deliberately not a list. SessionArgvMarkers
// answers "is a live process holding this id", and the only correct answer is
// the set of ways *this package* can put that id on a command line - so the way
// to hold it to that is to build them and look, not to write two strings down
// beside two other strings and check they match.
//
// **The walk is the switch's whole input space, all three fields**, and the
// third arrived late: park/wake added ResumeFrom, extended fork_test.go's cross
// product in the same change, and left this one at two dimensions with a
// comment still claiming otherwise - so the guard that ties SessionArgvMarkers
// to the switch had never once exercised the arm a wake takes. It cost nothing
// on the day, because the fork arm independently emits `--resume <v>` and the
// derived set was unchanged. What it cost was the guard's *second* direction:
// "no shape identityArgs cannot build" was being evaluated against a producer
// blind to the wake arm, so narrowing or deleting the fork case would have made
// `--resume <id>` read as surplus while every woken agent still carried it -
// a guard demanding the removal of the one marker that catches a stray
// `claude --resume`, which is worse than no guard at all.
func idMarkers(t *testing.T, want string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, from := range identityDomain() {
		for _, res := range identityDomain() {
			for _, id := range identityDomain() {
				collectIDMarkers(out, Config{ForkFrom: from.spelling, ResumeFrom: res.spelling, SessionID: id.spelling}, want)
			}
		}
	}
	return out
}

// collectIDMarkers adds every "<flag> <want>" one Config's argv carries.
func collectIDMarkers(out map[string]bool, cfg Config, want string) {
	args, err := NewSession(cfg).buildArgs()
	if err != nil {
		// A refused Config builds no argv, so it names no id. The refusals are
		// fork_test.go's subject, not this one's.
		return
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i+1] != want || want == "" {
			continue
		}
		for _, flag := range identityFlags {
			if args[i] == flag {
				out[flag+" "+want] = true
			}
		}
	}
}

// SessionArgvMarkers must name every shape identityArgs can build around an id,
// and no shape it cannot.
//
// Both directions, because the two failures are opposite and both are silent. A
// missing marker is a false *negative*: a fork in flight carries
// `--resume <parent>` in its argv, so a parent whose fork is still starting
// would read as free at the moment something is about to hold it. A surplus
// marker is a false positive over a shape no argv here produces, which is a
// verdict about an input that cannot arrive - and this project has a name for
// asserting over one of those.
//
// Derived from the producer rather than from the type, because the type would
// say "any string": identityFlags declares four flags and only the ones
// identityArgs puts an id *after* can name a session. --fork-session carries no
// value, so it is excluded by construction here rather than by being left out
// of a list, and park/wake's bare `--resume <id>` joined the set the day the
// switch built it rather than the day somebody remembered to - which is only
// true because idMarkers walks all three identity fields. **A dimension added
// to that switch is a dimension added to *both* products, in the same change.**
func TestSessionArgvMarkersAreExactlyTheShapesIdentityArgsBuilds(t *testing.T) {
	checked := 0
	for _, v := range identityDomain() {
		want := idMarkers(t, v.spelling)
		got := map[string]bool{}
		for _, m := range SessionArgvMarkers(v.spelling) {
			got[m] = true
		}

		for m := range want {
			if !got[m] {
				t.Errorf("identityArgs can build %q and SessionArgvMarkers(%q) does not offer it: whoever "+
					"asks whether anything is holding that session would be told no while a process holds it",
					m, v.spelling)
			}
		}
		for m := range got {
			if !want[m] {
				t.Errorf("SessionArgvMarkers(%q) offers %q and no argv identityArgs builds carries it: a "+
					"marker that matches nothing this package emits is a verdict over an argv that cannot occur",
					v.spelling, m)
			}
		}
		checked++
		if v.spelling != "" && len(want) == 0 {
			t.Errorf("no argv identityArgs builds names %q at all: the producer scan is broken, and both "+
				"directions above then hold over nothing", v.spelling)
		}
	}
	if checked != len(identityDomain()) {
		t.Fatalf("checked %d of %d identity values", checked, len(identityDomain()))
	}
}

// An empty id must name nothing. It is the one value where "every shape
// identityArgs builds" is the empty set and a marker would therefore be a
// substring of every command line on the machine.
func TestSessionArgvMarkersRefuseAnEmptyId(t *testing.T) {
	if got := SessionArgvMarkers(""); got != nil {
		t.Errorf("SessionArgvMarkers(\"\") returned %q, want nil: \"--session-id \" with nothing after it "+
			"is carried by every claude on the machine, so a caller matching on it holds the whole fleet", got)
	}
}

// hardMaxLines is CLAUDE.md's own limit on a non-test file. Test files are
// out of scope and visibly so - internal/core/session_test.go is 1,395 lines -
// because a table test's size is its coverage, and splitting one by line count
// rather than by subject is how a suite loses the reason it was arranged.
//
// This guard exists because the max was reached by a file nobody was watching
// and found by a planner reading the number in a note. internal/core/conflict_test.go
// is the precedent for a tree-wide check living in this package: the walk is
// already here, and a property of the whole tree belongs where the walk is.
const hardMaxLines = 800

func TestNoNonTestFileCrossesTheHardMax(t *testing.T) {
	measured := map[string]bool{}
	for _, rel := range nonTestGoFiles(t) {
		n := strings.Count(string(readSource(t, filepath.Join(repoRoot, rel))), "\n")
		measured[rel] = true
		if n > hardMaxLines {
			t.Errorf("%s is %d lines against a %d-line hard max. It is doing too much; find the "+
				"subject seam in its own header rather than cutting at the line count", rel, n, hardMaxLines)
		}
	}
	// Two floors, not one. nonTestGoFiles catches a walk that returned almost
	// nothing; this catches a walk that works and does not reach the package
	// the split happened in - which is the one shape a check written *by* that
	// split would never notice it had.
	if !measured[argvFile] {
		t.Fatalf("%s was never measured: the walk does not reach the file this check exists for", argvFile)
	}
}

// claudeMD is the file whose largest-file claim the check below derives rather
// than trusts.
const claudeMD = "CLAUDE.md"

// largestClaim matches the two `path at N` pairs in CLAUDE.md's sentence about
// the largest non-test files. Anchored on a backticked repo-relative .go path
// followed by "at" and a number, which is a shape that appears nowhere else in
// that document.
//
// The gap is `\s+` rather than a space because that document is hard-wrapped
// and the second claim's number is on the following line. A single-space
// version matched one of the two, which reads as "the sentence was reworded"
// and is the wrong failure.
var largestClaim = regexp.MustCompile("`((?:internal|cmd)/[^`]*\\.go)`\\s+at\\s+([0-9]+)")

// CLAUDE.md's claim about the largest non-test files is derived from the tree
// rather than believed.
//
// *"A number in a comment that nothing asserts is wrong by default"* is this
// project's own rule and it has now been broken five times. This is its fifth
// instance and the first one where the fix is cheap: the walk that finds every
// non-test file is already in this file for the hard max, so holding the
// sentence to it costs a regexp and a sort. `TestNoNonTestFileCrossesTheHardMax`
// cannot catch it — that one checks 800, not which file is nearest to it.
//
// It asserts the *ordering claim* and both counts, because the sentence makes
// both: the two files it names must be the two largest, in that order, at the
// lengths it states. Any of the three going stale is a failure that names the
// correction, so the fix is a copy-paste rather than a re-count by hand.
//
// Two floors. The sentence has to be found at all — a reworded paragraph
// otherwise yields "no violation", which reads as the strongest possible pass —
// and the walk has to have found more files than the sentence names, or the
// comparison is against itself.
func TestCLAUDEmdNamesTheTwoLargestNonTestFiles(t *testing.T) {
	files := nonTestGoFiles(t)
	lines := make(map[string]int, len(files))
	for _, rel := range files {
		lines[rel] = strings.Count(string(readSource(t, filepath.Join(repoRoot, rel))), "\n")
	}
	ranked := slices.SortedFunc(maps.Keys(lines), func(a, b string) int {
		if n := lines[b] - lines[a]; n != 0 {
			return n
		}
		return strings.Compare(a, b)
	})

	claims := largestClaim.FindAllStringSubmatch(string(readSource(t, filepath.Join(repoRoot, claudeMD))), -1)
	switch {
	case len(claims) != 2:
		t.Fatalf("%s makes %d `path at N` claims about file size, want the 2 its largest-non-test-file "+
			"sentence is written as. Either the sentence was reworded - in which case this check has to "+
			"follow it - or it is gone, and a check that matches nothing reports the strongest possible "+
			"pass", claudeMD, len(claims))
	case len(ranked) < 3:
		t.Fatalf("the walk found %d non-test files, so \"the two largest\" is not a claim about anything", len(ranked))
	}

	for i, claim := range claims {
		named, stated := claim[1], claim[2]
		want := strconv.Itoa(lines[ranked[i]])
		if named != ranked[i] || stated != want {
			t.Errorf("%s says the #%d largest non-test file is `%s` at %s; it is `%s` at %s.\n"+
				"The number is re-counted rather than incremented, which is this project's own rule and "+
				"the sentence three lines above this claim says so. Full order: %s",
				claudeMD, i+1, named, stated, ranked[i], want, topFew(ranked, lines))
		}
	}
}

// topFew renders the head of the ranking, so a failure carries the correction
// rather than only the contradiction.
func topFew(ranked []string, lines map[string]int) string {
	var b strings.Builder
	for _, rel := range ranked[:min(4, len(ranked))] {
		fmt.Fprintf(&b, "%s at %d; ", rel, lines[rel])
	}
	return b.String()
}

// readSource reads a file that must exist, because every path scanned here is
// derived from the tree rather than typed in.
func readSource(t *testing.T, path string) []byte {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return src
}

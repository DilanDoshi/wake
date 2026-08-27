package core

// The corpus carries no environment but its own.
//
// A recording is a photograph of the machine that took it. Claude's
// `system/init` frame is an environment dump - the skills installed, the
// plugins and their paths, the agents, the memory directories, the socket -
// and it is the *first line of every recording*, so nobody ever chooses to
// commit one.
//
// Two checks, because there are two ways it comes back:
//
//   - a **home-shaped path**, which is the leak in its general form and catches
//     a different machine's home as well as this one's;
//   - an **environment key on an init frame**, which is the leak in the exact
//     shape it arrives in, and is what a scrub that only rewrote paths would
//     leave behind.
//
// Neither subsumes the other: an environment key can be a bare word list with
// no path in it, and a tool result naming a home directory is on no init frame.
//
// # What this cannot see, so that nobody reads it as more than it is
//
// A **bare username** with no path around it. The corpus carries committed
// `ls -l` output inside tool results, where the owner is a column rather than a
// path, and the first version of this guard passed over it in nine fixtures.
// Nothing here can catch that in general - the check would need to know the
// name - so it is handled at scrub time instead, by
// scripts/scrub-fixtures.py's first pass, and by recording into a sterile HOME
// so it never arrives. This guard promises *shapes*, not anonymity.
//
// The repository knows whose it is regardless: `go.mod` says
// `github.com/DilanDoshi/wake`. The thing worth preventing is not the owner's
// name, it is their machine - a home directory listing, an installed skill set,
// a socket path.
//
// A failure here is fixed with scripts/scrub-fixtures.py, not by hand, and
// never by adding a case to the allowlist below.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// homePaths matches a Unix home directory with its owner's name attached, in
// **both** spellings it occurs in. The name is captured so the failure can say
// whose it is, and so the placeholder can be told from a real one.
//
// The second spelling exists because Claude's project directories slugify a
// path by replacing every separator with a dash, so `/Users/someone/Documents`
// is also on disk - and in the corpus - as `-Users-someone-Documents`. A check
// written for the first spelling alone passes over the second while it sits in
// the same file, which is what this guard did until the scrubber was written
// against it.
//
// They are **two patterns** rather than one alternation, and the difference is
// a single `-` in the name class: a path ends a name at `/` so a hyphen belongs
// to it, while a slug ends it *at* the hyphen. One shared pattern has to drop
// the hyphen to keep slugs working, and then a name like
// `some-one` yields only `some` - so the half-scrubbed result keeps the tail,
// matches an allowed name, and this guard reports clean on a corpus that still
// names somebody.
var homePaths = []*regexp.Regexp{
	regexp.MustCompile(`/(?:Users|home)/([A-Za-z0-9._-]+)`),
	regexp.MustCompile(`-(?:Users|home)-([A-Za-z0-9._]+)`),
}

// scrubbedUsers are the only names allowed to follow a home prefix. `dev` is
// what the scrubber writes; `someone` and `somebody` were already the
// hand-written placeholder in most of this tree's Go tests, and are kept
// rather than churned. A real name must never be added here - what the check
// is for is the machine, not the owner's name, which go.mod carries anyway.
// `runner` is GitHub Actions' own home on a hosted runner, which appears in
// the workflow and names no person.
var scrubbedUsers = map[string]bool{
	"dev": true, "someone": true, "somebody": true, "runner": true,
	// Hyphenated, and it exists so the comments above can show the shape the
	// two patterns are for. A guard whose own documentation trips it is a guard
	// nobody can write about.
	"some-one": true,
}

// environmentKeys are the init-frame fields that describe the machine rather
// than the session, and that no production decoder *and no test* reads. That
// second half is the whole of what makes deleting them safe.
//
// Four are deliberately absent, and two of those were learned by going red.
//
//   - `cwd` and `mcp_servers` are decoded by initFacts, so they are scrubbed in
//     place rather than removed.
//   - `tools` is decoded by nothing, and TestTheVocabularyDescribesTheRecorded\
//     Corpus still needs it: that array is the only place `Task`, `Edit`,
//     `WebFetch` and `WebSearch` appear in the corpus at all.
//   - `slash_commands` is decoded by nothing, and
//     TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising reads it as
//     the 133-word set that makes the overlap check exact rather than a
//     five-word sample. Its own floor caught the deletion.
//
// **"Read by nothing" has to mean read by no *test* either.** Checking wire.go
// answers what the product decodes, not what the suite depends on, and this
// list was wrong twice on exactly that distinction.
//
// `slash_commands` is also the most identifying field in the frame - it names
// an operator's own skills - and it stays anyway, which is a trade rather than
// an oversight. See scripts/scrub-fixtures.py's header.
var environmentKeys = []string{
	"agents", "memory_paths", "messaging_socket_path",
	"plugins", "skills",
}

// corpusFiles is every file the repository tracks *or* would track, as a path
// this test can open.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	// --others --exclude-standard adds files that exist but are not staged yet.
	// Without them a fixture is unchecked for exactly as long as it takes to
	// write it, run the suite green, and only then `git add` - which is the
	// order everybody works in, and with CI unfunded nothing catches it later.
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var found []string
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel != "" {
			found = append(found, filepath.Join(repoRoot, rel))
		}
	}
	return found
}

// sortedKeys is for the failure message, so it names the same placeholders in
// the same order every run.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// firstRealHome returns the first home directory in body whose owner is not an
// agreed placeholder: the owner, the whole match, and whether there was one.
// One report per file - the scrubber fixes them all in one pass, so listing
// every occurrence would be one failure repeated a thousand times.
func firstRealHome(body string) (name, where string, found bool) {
	for _, pattern := range homePaths {
		for _, m := range pattern.FindAllStringSubmatch(body, -1) {
			if !scrubbedUsers[m[1]] {
				return m[1], m[0], true
			}
		}
	}
	return "", "", false
}

// scrubberLiteral pulls the quoted strings out of one `NAME = [...]` or
// `NAME = {...}` assignment in the scrubber. Enough Python to read two
// constants, and no more - anything cleverer would be a parser this repository
// has no other use for.
var scrubberLiteral = regexp.MustCompile(`(?s)\n(\w+) = [\[{](.*?)[\]}]`)

// The scrubber and this guard share two lists, in two languages. They are held
// together by a test rather than by the "keep in step with" comment on each
// side, because that comment is exactly what a hurried edit skips - and the two
// failing apart is silent in the worst direction: the scrubber stops removing a
// key the guard has also stopped checking for, and the corpus quietly keeps it.
func TestTheScrubberAndThisGuardAgree(t *testing.T) {
	body := readSource(t, filepath.Join(repoRoot, "scripts", "scrub-fixtures.py"))
	found := map[string][]string{}
	for _, m := range scrubberLiteral.FindAllStringSubmatch(string(body), -1) {
		for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m[2], -1) {
			found[m[1]] = append(found[m[1]], q[1])
		}
	}

	for _, pair := range []struct {
		python, golang string
		want, got      []string
	}{
		{"DEAD_KEYS", "environmentKeys", found["DEAD_KEYS"], environmentKeys},
		{"KEEP", "scrubbedUsers", found["KEEP"], sortedKeys(scrubbedUsers)},
	} {
		if len(pair.want) == 0 {
			t.Fatalf("read no %s out of the scrubber: the literal moved and this "+
				"check is now passing over nothing", pair.python)
		}
		a, b := append([]string(nil), pair.want...), append([]string(nil), pair.got...)
		sort.Strings(a)
		sort.Strings(b)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			t.Errorf("scrub-fixtures.py's %s is %v but %s is %v; one was edited "+
				"without the other", pair.python, a, pair.golang, b)
		}
	}
}

func TestTheCorpusNamesNobodysHomeDirectory(t *testing.T) {
	files := corpusFiles(t)
	// A floor, for the failure mode a check like this cannot otherwise see: a
	// walk that returns nothing passes every assertion it makes.
	if len(files) < 100 {
		t.Fatalf("the walk found %d files, which is too few to be the corpus", len(files))
	}

	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		// A binary is not a document, and matching path-shaped bytes inside one
		// reports a leak nobody can read. utf8.Valid is the same discriminator
		// the scrubber uses when a read fails to decode.
		if !utf8.Valid(body) {
			continue
		}
		if name, where, found := firstRealHome(string(body)); found {
			rel, _ := filepath.Rel(repoRoot, path)
			t.Errorf("%s names a real home directory (%s, owner %q). Under testdata/ or docs/ "+
				"that is scripts/scrub-fixtures.py's job; anywhere else, write one of %v instead",
				rel, where, name, sortedKeys(scrubbedUsers))
		}
	}
}

func TestNoInitFrameCarriesTheMachineItWasRecordedOn(t *testing.T) {
	var checked int
	for _, path := range corpusFiles(t) {
		// Every file, not just .jsonl. A findings note pastes whole frames into
		// a code fence, and an init frame in a .md is the same leak as one in a
		// fixture - scoping this to an extension left six keys sitting in
		// docs/superpowers/notes/ while this test was green. Lines that are not
		// JSON cost one failed Unmarshal each.
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !utf8.Valid(body) {
			continue
		}
		for i, line := range strings.Split(string(body), "\n") {
			var frame map[string]json.RawMessage
			if json.Unmarshal([]byte(line), &frame) != nil {
				continue // not every line is JSON, and that is not this test's business
			}
			if string(frame["subtype"]) != `"init"` {
				continue
			}
			checked++
			for _, key := range environmentKeys {
				if _, ok := frame[key]; ok {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s:%d is an init frame still carrying %q, which describes the "+
						"machine rather than the session. Run scripts/scrub-fixtures.py", rel, i+1, key)
				}
			}
		}
	}
	// The second floor. Deleting every init frame would make the loop above
	// vacuous, and a corpus with no init frames is a corpus that cannot prove
	// initFacts decodes one.
	if checked == 0 {
		t.Fatal("no init frame was checked: either the corpus lost them or the walk missed them")
	}
}

// slashAllowlist is every slash-command name a recorded init frame may
// advertise, unioned from internal/core/testdata/slash-allowlist.json: Claude
// Code's own built-ins, public plugins, Wake's own verbs, and the neutral
// placeholders scripts/scrub-fixtures.py writes over an operator's personal
// command names. The scrubber reads the same file, so the list the guard
// enforces and the list the fix produces are one artifact and cannot drift -
// which is the job TestTheScrubberAndThisGuardAgree does for DEAD_KEYS and KEEP,
// done here by sharing the file rather than by comparing two copies.
func slashAllowlist(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "slash-allowlist.json"))
	if err != nil {
		t.Fatalf("reading slash-allowlist.json: %v", err)
	}
	var a struct{ Builtin, Plugin, Wake, Neutral []string }
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("slash-allowlist.json: %v", err)
	}
	allow := map[string]bool{}
	for _, set := range [][]string{a.Builtin, a.Plugin, a.Wake, a.Neutral} {
		for _, c := range set {
			allow[c] = true
		}
	}
	// A floor: an emptied or moved file would let the loop below pass over
	// nothing, which is the one way a check like this reports clean while
	// enforcing nothing.
	if len(allow) < 100 {
		t.Fatalf("slash-allowlist.json holds only %d names, far short of the recorded set", len(allow))
	}
	return allow
}

// slash_commands is the init frame's installed-command set, and it is the most
// identifying field in the frame: it names an operator's own skills. It is the
// one machine field the scrub keeps rather than deletes - two guards decode it -
// so TestNoInitFrameCarriesTheMachineItWasRecordedOn, which deletes
// environmentKeys, cannot catch a personal command name here. This does: every
// entry must be on slash-allowlist.json, so an operator command that reaches a
// recording fails the suite instead of shipping. That is the exact gap the
// 2026-08-25 checklist note names - the leak that put 70 personal names in the
// corpus was silent because nothing held this field to a list.
// commandShaped tells a real slash-command token from the elisions and
// annotations a findings note leaves inside a pasted frame ("...ELIDED (133
// entries)..."). Every real command, plugin and neutral is lowercase kebab or
// colon; an elision starts with a dot or carries a space or paren, so it fails
// this and is not a command name to hold to the list. A personal-name leak is
// command-shaped, so it is still caught.
var commandShaped = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]*$`)

func TestNoInitFrameAdvertisesAnUnlistedCommand(t *testing.T) {
	allow := slashAllowlist(t)
	var checked int
	for _, path := range corpusFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if !utf8.Valid(body) {
			continue
		}
		for i, line := range strings.Split(string(body), "\n") {
			var frame struct {
				Subtype string   `json:"subtype"`
				Slash   []string `json:"slash_commands"`
			}
			if json.Unmarshal([]byte(line), &frame) != nil || frame.Subtype != "init" {
				continue
			}
			for _, c := range frame.Slash {
				if !commandShaped.MatchString(c) {
					continue // an elision or annotation in a pasted frame, not a command
				}
				checked++
				if !allow[c] {
					rel, _ := filepath.Rel(repoRoot, path)
					t.Errorf("%s:%d advertises %q, which is not a Claude built-in, a public "+
						"plugin, a Wake verb or a known placeholder - an operator's own command "+
						"name in the corpus. Fix it with scripts/scrub-fixtures.py, never by "+
						"adding it to slash-allowlist.json", rel, i+1, c)
				}
			}
		}
	}
	// The floor for this loop: a corpus with no init frame carrying
	// slash_commands makes every assertion above vacuous.
	if checked == 0 {
		t.Fatal("no slash_commands entry was checked: the corpus lost its init frames or the walk missed them")
	}
}

// The slash allowlist is only a single source of truth while both readers point
// at it. corpus_test.go reads it in slashAllowlist; this holds the scrubber to
// the same file, so a future edit that reverts scrub-fixtures.py to a hardcoded
// Python list - the drift TestTheScrubberAndThisGuardAgree exists to catch for
// DEAD_KEYS and KEEP - fails here instead of shipping a fix that no longer
// matches the guard.
func TestTheScrubberReadsTheSharedSlashAllowlist(t *testing.T) {
	body := readSource(t, filepath.Join(repoRoot, "scripts", "scrub-fixtures.py"))
	if !strings.Contains(string(body), "slash-allowlist.json") {
		t.Error("scrub-fixtures.py no longer names slash-allowlist.json: the scrubber and " +
			"TestNoInitFrameAdvertisesAnUnlistedCommand have stopped sharing one list")
	}
}

package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Phase 2 is being built on parallel branches that merge back one at a time,
// and one of those merges pushed a `CLAUDE.md` carrying `<<<<<<< HEAD` around
// two *identical* copies of the same table row. Nothing about the content
// signalled a problem — the sides did not differ, so there was nothing to read
// and disagree with — and no test read the file at all. Go's own compiler
// catches this in a `.go` file and nothing catches it anywhere else, which is
// exactly where it landed.
//
// This lives in `internal/core` beside the airlock guard because it is the same
// shape of check — a property of the whole tree, asserted from the one package
// that already walks it — not because conflict markers are anything to do with
// Claude's JSON.
//
// The markers are assembled at runtime rather than written out, because a test
// that spells `<<<<<<<` in its own source is a file this test would then have
// to make an exception for, and an exception is how the next one gets in.
func conflictMarkers() []string {
	return []string{
		strings.Repeat("<", 7) + " ",
		strings.Repeat("=", 7),
		strings.Repeat(">", 7) + " ",
	}
}

// textFiles is every tracked file worth reading for markers: source, docs,
// config, and the Makefile. Binary and recorded corpora are skipped — a
// fixture is bytes somebody else produced, and `testdata/stream/*.jsonl` would
// be read for `=======` inside recorded prose.
func textFiles(t *testing.T) []string {
	t.Helper()
	// Keyed on the path from the repo root, not on the base name. The airlock
	// walk skips any directory *called* `notes`, which was correct when the
	// only one was the gitignored `/notes/` — and silently started skipping the
	// tracked `docs/notes/` when the working notes moved there. This test's
	// first draft inherited that list and could not see a marker in the very
	// files most likely to carry one.
	skipDir := map[string]bool{
		".git": true, ".worktrees": true, "testdata": true,
		"bin": true, "dist": true, ".superpowers": true, "notes": true,
	}
	keepExt := map[string]bool{
		".go": true, ".md": true, ".mod": true, ".sum": true,
		".yml": true, ".yaml": true, ".json": true, ".toml": true,
		".sh": true, ".txt": true,
	}

	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			// Same rule as the airlock walk: a directory holding a `.git`
			// entry is somebody else's checkout. A parallel lane put one under
			// `.claude/worktrees/` and every hand-written list walked into it.
			if _, gerr := os.Stat(filepath.Join(path, ".git")); gerr == nil && rel != "." {
				return filepath.SkipDir
			}
			if skipDir[filepath.ToSlash(rel)] {
				return filepath.SkipDir
			}
			return nil
		}
		if keepExt[filepath.Ext(rel)] || filepath.Base(rel) == "Makefile" {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", repoRoot, err)
	}
	return files
}

func TestNoFileCarriesAnUnresolvedMergeConflict(t *testing.T) {
	files := textFiles(t)
	// A walk that silently matched nothing would make this test pass forever.
	// The floor is deliberately low: it only has to prove the walk reached the
	// tree, not to pin how many files the tree has.
	if len(files) < 20 {
		t.Fatalf("the walk found %d files, so it is not reaching the repository", len(files))
	}

	markers := conflictMarkers()
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for n, line := range strings.Split(string(body), "\n") {
			for _, marker := range markers {
				// Prefix, not contains: a line *discussing* a marker (this
				// project's notes do) indents or quotes it, and a real one is
				// always at column zero.
				if strings.HasPrefix(line, marker) {
					t.Errorf("%s:%d begins with a merge conflict marker: %q", rel, n+1, line)
				}
			}
		}
	}
}

package daemon

// One atomic write, and nothing else in this package may grow a second.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// atomicFile is the one file allowed to build a replace-a-file sequence.
const atomicFile = "atomicfile.go"

// replaceSteps are the calls that only mean one thing together: a file being
// replaced under a reader.
//
// os.Rename is the decisive one and os.CreateTemp is how the sequence starts.
// Either alone in another file is either a second copy of this or something
// that wants explaining, and both are worth a sentence from whoever writes it.
var replaceSteps = []string{"os.CreateTemp", "os.Rename"}

// pathChmod is the call that was the drift itself, and it is refused
// everywhere in this package including the file that owns the sequence.
//
// The three copies differed in exactly one detail: two chmod'd the **fd**
// (`tmp.Chmod`) before Close, the third chmod'd the **path** (`os.Chmod`) after
// it. Nothing observable separates them — the file ends up with the same mode
// either way, which is why the mutation restoring the path form survived a
// behavioural battery. The difference is a window: `os.Chmod` names a path, and
// between the Close and the Chmod that path is a name anything can take. So the
// property is about the *shape of the call* rather than about an output, and
// that makes it static, the same way the identity block's "appended whole" is.
const pathChmod = "os.Chmod"

// The replace-a-file sequence is spelled once.
//
// It was spelled three times - roster.go, parkbook.go and manager.go - and by
// the third it had drifted: that copy chmod'd the *path* after Close rather
// than the *fd* before it, which is a TOCTOU the other two do not have. Nothing
// was broken by it, which is the point: CLAUDE.md's no-parallel-implementations
// rule exists because the copies stop agreeing before anybody notices, and this
// is that rule made checkable in the package it happened in.
//
// Two floors. The scan has to find files, and it has to find the sequence in
// the file that is supposed to hold it - a check that matched nothing would
// report the strongest possible pass for the weakest reason.
func TestTheAtomicWriteIsSpelledOnceInThisPackage(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	scanned, inHome, fdChmods := 0, 0, 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		file, perr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			// The fd form, which is what the sequence must use. Counted rather
			// than only permitted, so "nothing sets the mode" cannot pass.
			if name == atomicFile && sel.Sel.Name == "Chmod" && pkg.Name != "os" {
				fdChmods++
			}
			spelled := pkg.Name + "." + sel.Sel.Name
			if spelled == pathChmod {
				t.Errorf("%s calls %s. A mode is set on the **descriptor** before Close, never on the path "+
					"after it: os.Chmod names a path, and between the Close and the Chmod that path is a "+
					"name anything can take. That is the one detail the three copies of this sequence "+
					"drifted on, and it is invisible to any test of the resulting mode", name, spelled)
			}
			for _, step := range replaceSteps {
				if spelled != step {
					continue
				}
				if name == atomicFile {
					inHome++
					continue
				}
				t.Errorf("%s calls %s. Replacing a file under a reader is one sequence - temp in the "+
					"destination's own directory, write, chmod the fd, close, rename - and it was "+
					"copied three times before it drifted. It lives in %s; call writeFileAtomically, "+
					"or say here why this one is different", name, spelled, atomicFile)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("the walk found no non-test files: the scan is broken and this check approves everything")
	}
	if fdChmods == 0 {
		t.Fatalf("%s never chmods the descriptor it created: either the mode is no longer being set at all, "+
			"or it moved to the path form this check forbids", atomicFile)
	}
	if inHome == 0 {
		t.Fatalf("%s spells none of %v: either the sequence moved, in which case this check has to follow "+
			"it, or it is gone", atomicFile, replaceSteps)
	}
}

// The file ends up with the bytes and the mode it was asked for, and a failed
// write leaves nothing behind.
//
// The mode is a literal here rather than the caller's constant, for the reason
// the manager's config test learned: an assertion that reads the value under
// test widens with it.
func TestAnAtomicWriteLeavesTheFileOrNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thing.json")

	if err := writeFileAtomically(path, "thing", []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatalf("writeFileAtomically: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(body) != `{"a":1}` {
		t.Errorf("wrote %q", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode is %v, want -rw-------: the mode is set on the descriptor, before anything can "+
			"open the file by name", got)
	}

	// Replacing it keeps the old contents until the rename, so a reader sees
	// one or the other and never half - which is the whole reason for the
	// temp file, and the reason the leftover must not survive a failure.
	if err := writeFileAtomically(path, "thing", []byte(`{"a":2}`), 0o600); err != nil {
		t.Fatalf("second write: %v", err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(left) != 1 {
		t.Errorf("the directory holds %d entries after two writes, want 1: a temp file left beside a "+
			"file the next daemon reads is debris it has to ignore", len(left))
	}

	// A directory that does not exist is the failure managerConfig turns into
	// a refused launch, so it has to be an error rather than a silent skip.
	if err := writeFileAtomically(filepath.Join(dir, "nope", "thing.json"), "thing", nil, 0o600); err == nil {
		t.Error("writing into a directory that does not exist succeeded")
	}
}

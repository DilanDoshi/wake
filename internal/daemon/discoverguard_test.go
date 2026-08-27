// Static guards over discovery, and both are about a *direction* rather than a
// value.
//
// discover.go's header makes two claims about the rest of the build, and rung 7
// of docs/notes/decisions.md is that a claim you do not read will outlive the
// fact. Both are read here:
//
//   - "the transcript's keys are spelled in this file and nowhere else in the
//     tree" - which is argv.go's ruling for the CLI flags, applied to the second
//     Claude format;
//   - "no value of slugOf can produce a wrong directory, because nothing calls
//     it that way round" - which is a statement about what the code *can*
//     express, and therefore static.

package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// transcriptKeys are the key names of claude's *on-disk transcript*, which is a
// different format from the stream internal/core's airlock owns.
//
// Derived from discover.go's own const block rather than written out here: a
// fourth key added there without a line here would otherwise be policed by
// nothing, which is the "hand-written list standing in for something the code
// already declares" failure this project has a named entry for.
func transcriptKeys(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "discover.go", nil, 0)
	if err != nil {
		t.Fatalf("parse discover.go: %v", err)
	}
	keys := map[string]string{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			name := vs.Names[0].Name
			if !strings.HasPrefix(name, "key") {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			keys[v] = name
		}
	}
	// The floor. A renamed const prefix, a moved block or a broken parse all
	// yield an empty map, which reads as "no leaks" - the strongest possible
	// pass for the weakest possible reason.
	if len(keys) < 3 {
		t.Fatalf("found %d transcript key consts in discover.go, want at least 3: the scan is broken, not the tree", len(keys))
	}
	return keys
}

// goFilesInTree lists every .go file `go list` can see, which is how argv.go's
// own guards avoid being fooled by a skip list that hides a package.
func goFilesInTree(t *testing.T) []string {
	t.Helper()
	root, err := exec.Command("go", "list", "-f", "{{.Dir}}", "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var out []string
	for _, dir := range strings.Fields(string(root)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				out = append(out, filepath.Join(dir, e.Name()))
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("walked %d go files, which is fewer than this tree has: the walk is broken", len(out))
	}
	return out
}

// The second airlock leak is confined to one file, and this is what confines
// it.
//
// It reads **string literals out of the AST**, not bytes, for the reason
// argv_test.go does: several files explain the transcript format in a comment,
// and prose cannot read a key out of a JSON object.
//
// **Non-test files, which is CLAUDE.md's rule verbatim** rather than a
// convenience. It is also what keeps the check worth having: `cwd` is a generic
// word, four test files in this package already use it as the name of a
// scratch directory, and a guard whose exemption list grows one fixture at a
// time is the airlock's own "the allowlist would become the file". The leak
// that matters is a *production* file learning to read a transcript, because
// that is the one a Codex port would have to find.
func TestTheTranscriptKeysAreSpelledOnlyInDiscover(t *testing.T) {
	keys := transcriptKeys(t)
	fset := token.NewFileSet()
	checked := 0
	for _, path := range goFilesInTree(t) {
		base := filepath.Base(path)
		if base == "discover.go" || strings.HasSuffix(base, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if name, policed := keys[v]; policed {
				t.Errorf("%s spells %q, which is a key of claude's on-disk transcript format and belongs to internal/daemon/discover.go (as %s): "+
					"reading a transcript anywhere else spreads the second airlock leak, and what everything outside asks for is a FoundSession",
					path, v, name)
			}
			return true
		})
	}
	if checked < 20 {
		t.Fatalf("checked %d files, which is fewer than this tree has: the scan is asserting nothing", checked)
	}
}

// slugOf may only ever be **compared**, never assigned into anything that
// becomes a path.
//
// This is the property that makes shipping without knowing claude's real slug
// function safe (discover.go's header, 2026-08-12 findings §2): a disagreement
// between Wake's function and claude's can only fail a comparison, which is a
// refusal, and can never construct a directory to run a session in.
//
// A field-set check would not close it. `dir := slugOf(x)` followed by a
// filepath.Join is exactly the mutation this exists to kill, and it reads only
// allowed things. So the constraint is on the **shape of every use**: every
// call to slugOf must be an operand of a comparison, and nothing else.
func TestSlugOfIsOnlyEverCompared(t *testing.T) {
	fset := token.NewFileSet()
	uses := 0
	for _, path := range goFilesInTree(t) {
		if strings.HasSuffix(path, "_test.go") {
			// The tests build a projects tree, which is the one legitimate
			// place a slug is a name rather than a comparison - a fixture has
			// to put a file somewhere. Production code has no such need.
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		// Every call to slugOf that sits directly under a comparison is fine;
		// collect those, then require that every call in the file is one of
		// them.
		ok := map[ast.Node]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			bin, isBin := n.(*ast.BinaryExpr)
			if !isBin || (bin.Op != token.EQL && bin.Op != token.NEQ) {
				return true
			}
			for _, side := range []ast.Expr{bin.X, bin.Y} {
				if isSlugCall(side) {
					ok[side] = true
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			if !isSlugCall(n) {
				return true
			}
			uses++
			if !ok[n] {
				t.Errorf("%s:%d uses slugOf somewhere other than as an operand of == or !=: "+
					"the slug function is lossy and claude's exact spelling of it is unrecorded, so Wake may only ever *check* a directory it was given and may never *build* one",
					filepath.Base(path), fset.Position(n.Pos()).Line)
			}
			return true
		})
	}
	// The floor: a renamed function or a broken walk yields zero uses, which
	// reads as "no violations".
	if uses == 0 {
		t.Fatal("no call to slugOf was found in any non-test file: the scan is broken, and a scan that finds nothing agrees with every mutation")
	}
}

func isSlugCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "slugOf"
}

// verifiedDir may only ever return a directory the transcript itself named.
//
// The rung above the table in discover_test.go: no fixture closes a value
// space, and the mutation that matters here does not add a wrong *case*, it
// invents a *value*. `return filepath.Join(projects, slug)` - reconstructing a
// path from the slug - passes every behavioural test that only ever asks
// whether the answer is right for directories the test itself created.
func TestVerifiedDirReturnsOnlyADirectoryItWasGiven(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "discover.go", nil, 0)
	if err != nil {
		t.Fatalf("parse discover.go: %v", err)
	}
	var fn *ast.FuncDecl
	ast.Inspect(f, func(n ast.Node) bool {
		if d, ok := n.(*ast.FuncDecl); ok && d.Name.Name == "verifiedDir" {
			fn = d
		}
		return true
	})
	if fn == nil {
		t.Fatal("verifiedDir was not found in discover.go: the scan is broken, not the tree")
	}
	returns := 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		returns++
		switch v := ret.Results[0].(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING && v.Value == `""` {
				return true
			}
		case *ast.Ident:
			// A plain local, which can only ever hold something assigned out of
			// the range over the cwds it was handed.
			return true
		}
		t.Errorf("verifiedDir:%d returns an expression that is neither \"\" nor a plain identifier: "+
			"the only directory it may name is one the transcript named, and anything derived from the slug is the wrong-directory failure this whole file exists to prevent",
			fset.Position(ret.Pos()).Line)
		return true
	})
	if returns < 2 {
		t.Fatalf("verifiedDir has %d single-value returns, want at least 2 (the refusal and the answer): the scan is broken", returns)
	}
}

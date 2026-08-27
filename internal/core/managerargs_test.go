package core

// The two flags that make a session the manager rather than an agent, and the
// one property that is a safety decision rather than a spelling.
//
// # Why --strict-mcp-config is not a second field
//
// `--mcp-config` **adds** servers to whatever the machine already has
// configured. Without `--strict-mcp-config` beside it the manager inherits
// every MCP server in the user's own configuration - on the machine this was
// written on that is Slack, Linear, firecrawl, playwright and more - so a
// process whose whole design is "two verbs an operator can undo by looking at
// the room" would silently hold a browser, a ticket tracker and a chat client.
//
// That is the same shape as the punished argv shapes identityArgs exists for:
// one flag emitted without its partner, accepted, exit 0, nothing on stderr,
// and a session that looks right. So the answer is the same answer - **the pair
// is one literal from one append**, which the static check below holds, and
// there is no Config field that can express one without the other.
//
// The behavioural half is a cross product rather than two examples because
// argvguard_test.go forbids the argv path from asking anything about a Config
// field except whether it is empty. That is what makes {empty, non-empty}
// sufficient here rather than lucky: a narrowing keyed on the *value* of
// MCPConfig is already a build failure over there, so what is left to check is
// that the emptiness test itself is total over the other fields - which is what
// the decorations dimension is for.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// managerFlags are the flags a manager session carries and an agent does not.
//
// Held to argv.go for the reason the identity flags are, and it is the same
// reason rather than an analogy: a second file spelling --mcp-config is a
// second place --strict-mcp-config can be forgotten, and forgetting it is
// silent.
var managerFlags = []string{"--mcp-config", "--strict-mcp-config", "--tools", "--append-system-prompt"}

// managerFlagCount holds that list to its size, the rule this project gives for
// any list that genuinely has to be hand-written. This one does: the domain is
// claude's own command line, which nothing in this repository declares.
const managerFlagCount = 4

// managerDomain is every kind of value the two manager fields can hold. Two
// kinds, because the argv path may only ask which one it is.
func managerDomain() []string { return []string{"", "/tmp/wake/mcp.json"} }

func promptDomain() []string { return []string{"", "You are Wake's manager."} }

// The MCP config reaches the command line with strict beside it, an ordinary
// agent gets neither, and neither can happen without the other.
func TestAnMCPConfigReachesTheCommandLineOnlyWithStrictBesideIt(t *testing.T) {
	checked, carried := 0, 0
	for _, cfgPath := range managerDomain() {
		for _, prompt := range promptDomain() {
			for _, d := range decorations() {
				// The decoration first, then the two fields under test: it
				// sets every *other* field, including these two, so applying it
				// second would overwrite the dimension being walked.
				cfg := d.apply(Config{SessionID: idForkChild})
				cfg.MCPConfig, cfg.AppendSystemPrompt = cfgPath, prompt
				args, err := NewSession(cfg).buildArgs()
				if err != nil {
					t.Fatalf("buildArgs(%s, mcp=%q): %v", d.name, cfgPath, err)
				}
				checked++
				hasConfig := slices.Contains(args, "--mcp-config")
				hasStrict := slices.Contains(args, "--strict-mcp-config")
				hasTools := slices.Contains(args, "--tools")
				if hasConfig != hasTools {
					t.Errorf("%s with MCPConfig=%q built --mcp-config=%v --tools=%v.\n"+
						"Without --tools the manager is an ordinary claude session holding Bash, Write and "+
						"Edit in auto, one hop from text an agent wrote to execution on this machine;\n"+
						"got: %v", d.name, cfgPath, hasConfig, hasTools, args)
				}
				if hasConfig != hasStrict {
					t.Errorf("%s with MCPConfig=%q built --mcp-config=%v --strict-mcp-config=%v.\n"+
						"A config without strict inherits every MCP server on the machine, so a manager "+
						"bounded to send and interrupt would silently hold whatever else is configured;\n"+
						"got: %v", d.name, cfgPath, hasConfig, hasStrict, args)
				}
				if cfgPath == "" {
					if hasConfig {
						t.Errorf("%s: an ordinary agent was given an MCP config. Every agent carrying the "+
							"manager's tools would let any of them message and interrupt any other, which is "+
							"a fleet that can deadlock itself with nobody having asked for it\ngot: %v", d.name, args)
					}
					continue
				}
				carried++
				if !containsSeq(args, []string{"--mcp-config", cfgPath, "--strict-mcp-config", "--tools", ""}) {
					t.Errorf("%s: argv = %v, want --mcp-config, its path, --strict-mcp-config and --tools \"\" as one run", d.name, args)
				}
			}
		}
	}
	if carried == 0 || checked == 0 {
		t.Fatalf("checked %d configs of which %d carried one: this test is asserting nothing", checked, carried)
	}
}

// The scope arrives as a system prompt, not as a turn.
//
// It is --append-system-prompt rather than a first message, and the difference
// is not cosmetic: a first message is a turn in the transcript that later turns
// can argue the model out of, and `/clear` drops it. A system prompt is not a
// turn. The manager reads text every agent in the fleet wrote, so the sentence
// that says that text is data rather than instruction has to be the one thing
// in its context that a conversation cannot move.
func TestAnAppendedSystemPromptReachesTheCommandLineAndAnEmptyOneDoesNot(t *testing.T) {
	checked, carried := 0, 0
	for _, prompt := range promptDomain() {
		for _, cfgPath := range managerDomain() {
			for _, d := range decorations() {
				// The decoration first, then the two fields under test: it
				// sets every *other* field, including these two, so applying it
				// second would overwrite the dimension being walked.
				cfg := d.apply(Config{SessionID: idForkChild})
				cfg.MCPConfig, cfg.AppendSystemPrompt = cfgPath, prompt
				args, err := NewSession(cfg).buildArgs()
				if err != nil {
					t.Fatalf("buildArgs(%s, prompt=%q): %v", d.name, prompt, err)
				}
				checked++
				if prompt == "" {
					if slices.Contains(args, "--append-system-prompt") {
						t.Errorf("%s: an agent with no scope was given --append-system-prompt with nothing "+
							"to say\ngot: %v", d.name, args)
					}
					continue
				}
				carried++
				if !containsSeq(args, []string{"--append-system-prompt", prompt}) {
					t.Errorf("%s: argv = %v, want --append-system-prompt and the scope after it", d.name, args)
				}
			}
		}
	}
	if carried == 0 || checked == 0 {
		t.Fatalf("checked %d prompts of which %d carried one: this test is asserting nothing", checked, carried)
	}
}

// The pair is one append, which is the static half of the property above.
//
// A cross product over Config cannot see the version that emits the two flags
// from two `if`s: both are emptiness tests on the same field, both are legal
// under argvguard's grammar, and both are true at the same time - so every cell
// passes while the argv has grown a second statement somebody can delete. The
// property is "these two literals are emitted together", the code declares it as
// one argument list, and an argument list is static.
//
// Same move as TestTheIdentityBlockIsAppendedWholeAndNeverEdited, for the same
// class of failure: a flag whose absence is accepted, exit 0, empty stderr.
func TestTheMCPFlagsAreEmittedFromOneAppendOrNotAtAll(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "argv.go", readSource(t, "argv.go"), 0)
	if err != nil {
		t.Fatalf("parse argv.go: %v", err)
	}
	parents := parentOf(file)

	calls := map[ast.Node][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		flag := ""
		for _, f := range []string{"--mcp-config", "--strict-mcp-config"} {
			if strings.Contains(lit.Value, f) {
				flag = f
			}
		}
		if flag == "" {
			return true
		}
		// The enclosing call, found by walking out: an argument may be wrapped.
		for at := ast.Node(lit); at != nil; at = parents[at] {
			call, isCall := at.(*ast.CallExpr)
			if !isCall {
				continue
			}
			calls[call] = append(calls[call], flag)
			return true
		}
		t.Errorf("argv.go names %s at %s outside any call: the two flags are emitted together or not at "+
			"all, and nothing here can say whether that holds", flag, fset.Position(lit.Pos()))
		return true
	})

	if len(calls) != 1 {
		t.Fatalf("argv.go emits the MCP flags from %d calls, want exactly 1. --mcp-config *adds* servers to "+
			"whatever the machine has configured, so the two are one literal from one append precisely so "+
			"that no statement can drop half of it - which is accepted, exit 0, empty stderr, and a manager "+
			"holding every MCP server the user has", len(calls))
	}
	for call, flags := range calls {
		fn, isIdent := call.(*ast.CallExpr).Fun.(*ast.Ident)
		if !isIdent || fn.Name != "append" {
			t.Errorf("the MCP flags are emitted from something other than append at %s", fset.Position(call.Pos()))
		}
		for _, want := range []string{"--mcp-config", "--strict-mcp-config"} {
			if !slices.Contains(flags, want) {
				t.Errorf("the one call emitting the MCP flags carries %v and not %q", flags, want)
			}
		}
	}
}

// The manager's flags are spelled in argv.go and nowhere else in the tree.
//
// The identity flags' own ruling, applied to the second pair that has a
// silently-punished shape. It reuses the same walk rather than growing a second
// one - goFiles skips .worktrees, and this repository is developed in worktrees
// under its own root - and reads string literals rather than bytes, so a comment
// explaining why a flag matters somewhere else is still writeable.
func TestTheManagerFlagsAreSpelledOnlyInArgv(t *testing.T) {
	if len(managerFlags) != managerFlagCount {
		t.Fatalf("managerFlags holds %d flags and managerFlagCount says %d: update both deliberately", len(managerFlags), managerFlagCount)
	}
	found := 0
	for _, rel := range nonTestGoFiles(t) {
		for _, lit := range stringsIn(t, filepath.Join(repoRoot, rel)) {
			if lit.fromTag {
				continue
			}
			for _, flag := range managerFlags {
				if !strings.Contains(lit.text, flag) {
					continue
				}
				if rel == argvFile {
					found++
					continue
				}
				t.Errorf("%s:%d spells %q. A second file spelling --mcp-config is a second place "+
					"--strict-mcp-config can be forgotten, and forgetting it is silent - so these belong "+
					"in %s, beside the pair rule that holds them together", rel, lit.line, flag, argvFile)
			}
		}
	}
	if found == 0 {
		t.Fatalf("no manager flag appears in %s at all: the scan is broken, or the block moved", argvFile)
	}
}

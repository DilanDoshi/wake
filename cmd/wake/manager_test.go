package main

// `wake manager`: the frame it writes, the verb it reaches, and the one thing
// this package and internal/daemon have to agree about.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The frame is a spawn carrying the manager role and no name.
//
// No name because there is nothing for a client to choose: the daemon owns the
// one manager name, refuses it to everything else, and a name on this frame
// would be a word this command asked for and did not get. The role is what
// makes it a manager at all - without it this is `wake new`, and the session
// that comes back is an ordinary agent under a pooled name, which nothing would
// report as a failure.
func TestTheManagerVerbAsksForTheManagerRoleAndNoName(t *testing.T) {
	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	frames, errs := rpc.ReadFrames(theirs)
	go func() {
		if err := requestSpawn(mine, idGammaCLI, "", rpc.RoleManager, spawnOpts{}); err != nil {
			t.Errorf("requestSpawn: %v", err)
		}
	}()

	f := <-frames
	if f.Kind != rpc.FrameSpawn {
		t.Fatalf("wrote a %q frame, want %q", f.Kind, rpc.FrameSpawn)
	}
	if f.Role != rpc.RoleManager {
		t.Errorf("the frame's Role is %q, want %q: without it the daemon names this an ordinary agent and "+
			"nothing reports that the manager was not started", f.Role, rpc.RoleManager)
	}
	if f.SessionID != idGammaCLI {
		t.Errorf("the frame's SessionID is %q, want the id this command minted %q", f.SessionID, idGammaCLI)
	}
	if f.Text != "" {
		t.Errorf("the frame asks to be called %q. The daemon owns the one manager name and refuses it to "+
			"everything else, so a name here is a word this command asked for and did not get", f.Text)
	}
	if !filepath.IsAbs(f.Dir) {
		t.Errorf("the frame carries Dir %q, which is not absolute: the daemon refuses a relative one, and "+
			"without any the manager runs in whatever directory forked the daemon", f.Dir)
	}

	_ = mine.Close()
	for range frames {
	}
	<-errs
}

// An ordinary spawn asks for no role, which is the other half of the same
// property: Role is the field that decides configuration, and every existing
// client sends it empty.
func TestAnOrdinarySpawnAsksForNoRole(t *testing.T) {
	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	frames, errs := rpc.ReadFrames(theirs)
	go func() {
		if err := requestSpawn(mine, idGammaCLI, "sydney", "", spawnOpts{}); err != nil {
			t.Errorf("requestSpawn: %v", err)
		}
	}()

	f := <-frames
	if f.Role != "" {
		t.Errorf("`wake new sydney` asked for role %q: an ordinary agent holding the manager's tools could "+
			"message and interrupt every other agent in the fleet", f.Role)
	}
	if f.Text != "sydney" {
		t.Errorf("the frame's Text is %q, want the name that was asked for", f.Text)
	}

	_ = mine.Close()
	for range frames {
	}
	<-errs
}

// The verb reaches its own command, and a refusal reaches the terminal.
//
// Both in one, because the second is what makes the first observable: a daemon
// that refuses the spawn turns a path that otherwise ends in a live manager
// into an error this package can read. `deferred.md` records the dispatch
// switch being silently wrong before, with no test asserting that any verb
// reached its own command at all.
func TestTheManagerVerbReachesItsOwnCommandAndReportsARefusal(t *testing.T) {
	const why = "a manager is already running"
	d := listenAs(t, &fakeDaemon{status: rpc.Status{Running: true}, spawnRefusal: why})
	t.Setenv("WAKE_SOCKET", d.socket)

	err := run([]string{cmdManager}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("`wake manager` against a daemon that refused the spawn succeeded")
	}
	if !strings.Contains(err.Error(), why) {
		t.Errorf("`wake manager` reported %v, want the daemon's own refusal: a refusal nobody reads is a "+
			"manager the operator believes is running", err)
	}
	if got := d.lastSpawn(); got.Role != rpc.RoleManager {
		t.Errorf("`wake manager` put a spawn with role %q on the wire, want %q", got.Role, rpc.RoleManager)
	}
}

// The verb is in the usage text, unlike `daemon` and `mcp`.
//
// It is a thing a person types, and the legend rule read at the shell: a
// feature nobody is told about is one nobody uses, and this one has no other
// door - the room refuses an unaddressed draft by naming this command.
func TestTheManagerVerbIsInTheUsageText(t *testing.T) {
	if !strings.Contains(usage, "wake "+cmdManager) {
		t.Errorf("the usage text does not mention `wake %s`:\n%s", cmdManager, usage)
	}
	for _, hidden := range []string{cmdDaemon, cmdMCP} {
		if strings.Contains(usage, "wake "+hidden) {
			t.Errorf("the usage text offers `wake %s`, which is not a user command: it speaks a protocol on "+
				"stdin and stdout, so a person who runs it gets a program that appears to hang", hidden)
		}
	}
}

// Every subcommand internal/daemon spells is a verb this command dispatches.
//
// The daemon produces two argv fragments naming this binary - `daemon`, which
// EnsureRunning forks, and `mcp`, which the manager's MCP config runs - and
// neither is checked against anything today. A mismatch is silent in the worst
// way available: `wake daemon` becomes "unknown command" inside a detached fork
// whose stderr is /dev/null, and `wake mcp` becomes a manager whose tools all
// fail with a usage message a model reads as a tool result.
//
// Both sides are derived rather than listed. That is rung 7 paid at the one
// place in this tree where two packages spell the same word for each other and
// nothing reads either.
func TestEverySubcommandTheDaemonSpellsIsAVerbThisCommandDispatches(t *testing.T) {
	spelled := map[string]string{}
	for name, word := range stringConstantsUnder(t, filepath.Join("..", "..", "internal", "daemon"), "") {
		if strings.HasSuffix(name, "Subcommand") {
			spelled[name] = word
		}
	}
	if len(spelled) == 0 {
		t.Fatal("internal/daemon declares no *Subcommand constant: the scan is broken, and a check that " +
			"finds no claims approves all of them")
	}

	verbs := stringConstantsUnder(t, ".", "cmd")
	known := map[string]bool{}
	for _, v := range verbs {
		known[v] = true
	}
	if len(known) == 0 {
		t.Fatal("cmd/wake declares no cmd* constant: the scan is broken")
	}

	for name, word := range spelled {
		if !known[word] {
			t.Errorf("internal/daemon's %s is %q and cmd/wake dispatches no such verb (it has %v).\n"+
				"The daemon forks this binary with that word and the failure is silent: a detached daemon's "+
				"stderr is /dev/null, and an MCP server that exits with a usage message reaches a model as a "+
				"tool result", name, word, verbs)
		}
	}
}

// shellVerb matches `wake <word>` wherever a sentence names one.
var shellVerb = regexp.MustCompile(`\bwake ([a-z][a-z-]*)`)

// quotedShellVerb is the same claim as it is written *inside a sentence*: in
// backticks, which is how every refusal in this tree offers a command to type.
//
// The looser shellVerb is right for the usage text, which is a formatted block
// with no quoting, and wrong for prose - `internal/daemon` legitimately writes
// "a wake has to run there" and "the wake binary", neither of which sends
// anybody to a shell. The backtick is the discriminator the codebase already
// uses, so requiring it is reading the convention rather than adding one.
var quotedShellVerb = regexp.MustCompile("`wake ([a-z][a-z-]*)")

// dispatchedVerbs is the verb set, read off this package's own cmd* constants.
//
// The floor is a Fatal rather than a skip: a scan that found no verbs would
// make every claim checked against it pass, which is the strongest possible
// result for the weakest possible reason.
func dispatchedVerbs(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, v := range stringConstantsUnder(t, ".", "cmd") {
		out[v] = true
	}
	if len(out) == 0 {
		t.Fatal("cmd/wake declares no cmd* constant: the scan is broken and every claim below holds over nothing")
	}
	return out
}

// requireEveryVerbNamedIsDispatched holds one sentence to the verb set.
//
// Both directions of the rung-7 failure land here: a sentence naming a verb
// that was renamed, and one naming a verb that never existed. It is the same
// check either way, because both are a claim about the build made by something
// that does not read it.
func requireEveryVerbNamedIsDispatched(t *testing.T, where, sentence string, pattern *regexp.Regexp) int {
	t.Helper()
	known := dispatchedVerbs(t)
	found := pattern.FindAllStringSubmatch(sentence, -1)
	for _, m := range found {
		if !known[m[1]] {
			t.Errorf("%s tells somebody to type `wake %s` and this command dispatches no such verb "+
				"(it has %v). A missing feature is not trusted and a lying one is - and a sentence like "+
				"this is advice given at the moment somebody has already failed to do what they wanted",
				where, m[1], known)
		}
	}
	return len(found)
}

// Every `wake <verb>` any surface names is a verb this command dispatches.
//
// Two surfaces, and the second is the one this exists for. `usage` is this
// package's own and would be caught by a reader eventually. `ui.NoAddressee` is
// **internal/ui's**, and it names a shell verb that package can neither call
// nor see - which is precisely the shape docs/notes/decisions.md calls rung 7:
// a claim about a part of the build the code making it never reads. The room
// refuses an unaddressed draft by telling somebody to type `wake manager`, and
// the day that verb is renamed the refusal becomes advice that produces
// "unknown command".
//
// Derived on both sides: the sentence is read off the exported constant, and
// the verb set off this package's own cmd* constants.
func TestEveryShellVerbASentenceNamesIsOneThisCommandDispatches(t *testing.T) {
	// ui.NoAddressee is no longer named here and is not lost: the scan below
	// finds its literal in internal/ui, by file and line, along with every
	// other sentence in that package. Naming one constant was what made this a
	// list of two.
	named := requireEveryVerbNamedIsDispatched(t, "cmd/wake's usage text", usage, shellVerb)
	if named == 0 {
		t.Fatal("the usage text names no `wake <verb>`: a check that matches nothing reports the strongest possible pass")
	}
	sentences := map[string]string{}
	// **Derived rather than listed**, which is rung 2 arriving inside the
	// rung-7 guard. This was a hand-written map of two, and session importing
	// added three new `wake <verb>` claims from internal/daemon that it could
	// not see - so the package that now names more shell verbs than any other
	// outside cmd/wake was the one surface with no cover. A sentence is a
	// string literal in a non-test file, wherever somebody puts it.
	for _, dir := range []string{"../../internal/daemon", "../../internal/ui"} {
		for where, sentence := range shellSentencesUnder(t, dir) {
			sentences[where] = sentence
		}
	}
	for where, sentence := range sentences {
		named += requireEveryVerbNamedIsDispatched(t, where, sentence, quotedShellVerb)
	}
	// The floor is over the **derived** half, because the usage text alone
	// would satisfy a bare count - which is exactly how this guard came to have
	// a blind spot. internal/daemon's import refusals and internal/ui's
	// NoAddressee are both in here; fewer than three sentences means the scan
	// is broken rather than the tree clean.
	if len(sentences) < 3 {
		t.Fatalf("the scan over internal/ found %d sentence(s) offering a `wake <verb>`, want at least 3: "+
			"a scan that finds nothing agrees with every mutation", len(sentences))
	}
}

// shellSentencesUnder finds every string literal in a package's non-test files
// that tells somebody to type a `wake` verb.
//
// Literals out of the AST rather than bytes, for TestTheIdentityFlagsAreSpelledOnlyInArgv's
// reason: several files discuss these verbs in a comment, and prose cannot send
// anybody to a shell.
func shellSentencesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil || !quotedShellVerb.MatchString(v) {
				return true
			}
			out[fmt.Sprintf("%s:%d", path, fset.Position(lit.Pos()).Line)] = v
			return true
		})
	}
	return out
}

// stringConstantsUnder is stringConstants over every non-test file in a
// package directory, so a constant can be found without naming the file it is
// declared in - which is the point here, since the whole property is that two
// packages agree about a word and neither reads the other.
func stringConstantsUnder(t *testing.T, dir, prefix string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		for k, v := range stringConstants(t, filepath.Join(dir, name), prefix) {
			out[k] = v
		}
	}
	return out
}

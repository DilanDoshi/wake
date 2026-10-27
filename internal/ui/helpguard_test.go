package ui

// What /wake-help lists is read off the bindings and the commands themselves,
// never typed beside them, so these guards hold it to them in both directions:
// a key, a key-capturing surface or a command added without a line in the view
// is a build failure. The manual's commands chapter is held to the same set.

import (
	"go/ast"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// helpEverything is every row the view can draw, with the shell section filled,
// folded to one string for a Contains.
func helpEverything() string {
	var b strings.Builder
	for _, s := range helpSections(helpSampleUsage) {
		for _, r := range s.rows {
			b.WriteString(r.lead + " " + r.text + "\n")
		}
	}
	return helpFolded(b.String())
}

func helpFolded(s string) string { return strings.Join(strings.Fields(s), " ") }

func sectionNamed(t *testing.T, title string) helpSection {
	t.Helper()
	for _, s := range helpSections(helpSampleUsage) {
		if s.title == title {
			return s
		}
	}
	t.Fatalf("no section %q in %v", title, helpTitles())
	return helpSection{}
}

func helpTitles() []string {
	var out []string
	for _, s := range helpSections(helpSampleUsage) {
		out = append(out, s.title)
	}
	return out
}

// Every key the legend names has a line saying what it does, and the view draws
// it: the legend is the canonical list App.key is held to, so this is the same
// bijection one step on.
func TestEveryKeyTheLegendBindsHasALineInTheHelp(t *testing.T) {
	keys := sectionNamed(t, keysSectionTitle)
	for _, e := range legendEntries {
		if strings.TrimSpace(e.help) == "" {
			t.Errorf("the legend binds %q (%s) and says nothing of it for /wake-help: write the line beside the entry", e.glyph, e.what)
			continue
		}
		if !slices.Contains(keys.rows, helpRow{lead: e.glyph, text: e.help}) {
			t.Errorf("the legend binds %q and the Keys section does not draw its line %q", e.glyph, e.help)
		}
	}
}

// The newline keys live on the composer's own binding rather than in the
// legend, and the help reads them from there.
func TestTheHelpNamesTheKeysThatPutANewlineInTheDraft(t *testing.T) {
	var found bool
	for _, r := range sectionNamed(t, keysSectionTitle).rows {
		found = found || (strings.Contains(r.lead, "⌥↵") && strings.Contains(r.lead, "⌃J"))
	}
	if !found {
		t.Errorf("no Keys row names ⌥↵ and ⌃J, the composer's newline binding: %v", sectionNamed(t, keysSectionTitle).rows)
	}
}

// interceptedBy is every `a.<name>Key(m)` call in one method - each is a surface
// that reads keys before the composer does.
func interceptedBy(t *testing.T, file, method string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	ast.Inspect(funcDeclIn(t, file, method).Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		recv, isIdent := sel.X.(*ast.Ident)
		arg, argIdent := call.Args[0].(*ast.Ident)
		if ok && isIdent && argIdent && recv.Name == "a" && arg.Name == "m" && strings.HasSuffix(sel.Sel.Name, "Key") {
			out[sel.Sel.Name] = true
		}
		return true
	})
	return out
}

// Both directions, the legend guard's own shape: a surface that takes keys with
// no rows in the help is a key nobody is told about, and a row naming a surface
// nothing reads any more is a line that lies.
func TestEveryKeyCapturingSurfaceHasRowsInTheHelpAndEveryRowIsAReader(t *testing.T) {
	found := interceptedBy(t, "keys.go", "key")
	maps.Copy(found, interceptedBy(t, "appupdate.go", "update"))
	if len(found) < 8 {
		t.Fatalf("found %d key-reading surfaces: the scan is broken and this test is asserting nothing", len(found))
	}
	described := map[string]bool{}
	for _, s := range helpSurfaces() {
		described[s.via] = true
		if !found[s.via] {
			t.Errorf("the help describes %q (%s) and neither App.key nor App.update calls it: a row about nothing", s.via, s.name)
		}
		if strings.TrimSpace(s.keys) == "" || strings.TrimSpace(s.name) == "" {
			t.Errorf("%q has a row with no name or no keys: %+v", s.via, s)
		}
	}
	for via := range found {
		if !described[via] {
			t.Errorf("App.key or App.update reads keys through %s and the help has no row for it: a surface nobody is told about", via)
		}
	}
}

var digitRange = regexp.MustCompile(`\[1-\d\]`)

// The line a surface draws is the line the help carries, read off the real
// producer at each state it has - not a copy typed beside it. A card's digit
// range is its question's own, so the help names the largest.
func TestTheHelpCarriesTheKeyLinesTheSurfacesDraw(t *testing.T) {
	have := helpEverything()
	check := func(name, drawn string) {
		t.Helper()
		if drawn = helpFolded(drawn); drawn == "" {
			t.Fatalf("%s drew no key line: the check is asserting nothing", name)
		}
		if !strings.Contains(have, drawn) {
			t.Errorf("%s draws %q and the help does not carry it", name, drawn)
		}
	}

	check("the board (rows)", boardKeyLineRows)
	check("the board (tiles)", boardKeyLineTiles)
	check("/status", statusHint)
	check("the completion menu", completionKeys)
	check("the save dialog", saveDialogKeys)
	for _, level := range []mcpLevel{mcpList, mcpDetail, mcpTools} {
		check("/mcp", MCPMenu{Level: level}.keyHint())
	}
	check("/resume in the room", ResumePicker{Multi: true}.keyHint())
	check("/resume in a conversation", ResumePicker{}.keyHint())

	view := WorkflowView{}
	runs := []workflowRunView{{Status: core.TaskRunning}}
	listed := view.listRows(runs, 80, 20)
	foot := ""
	for _, row := range listed { // the foot is the last row with words in it; blank rows pad below
		if strings.TrimSpace(stripANSI(row)) != "" {
			foot = stripANSI(row)
		}
	}
	check("/workflows list", foot)
	check("/workflows run", stripANSI(view.runKeyLine(workflowRunView{})))
	check("/workflows running run", stripANSI(view.runKeyLine(workflowRunView{Status: core.TaskRunning})))
	check("/workflows agent", stripANSI(agentKeyLine(false)))
	check("/workflows agent expanded", stripANSI(agentKeyLine(true)))

	cardLine := func(ev core.Event) string {
		cs := Cards{}.Add("s1", ev)
		top, _ := cs.Top()
		return digitRange.ReplaceAllString(cs.keyLine(top, 400, false), "[1-9]")
	}
	check("a permission card", cardLine(permissionAsk(t)))
	check("a plan card", cardLine(planAsk(t)))
	check("a question card", cardLine(firstAskOfShape(t, choiceFixture, ShapeQuestion)))
	check("a multi-select question card", cardLine(firstAskOfShape(t, multiFixture, ShapeQuestion)))
	check("a question's answer box", answerModeKeys)
	check("a deny reason", denyModeKeys)
	check("a question's review", reviewKeys)
}

func permissionAsk(t *testing.T) core.Event { return firstAskOfShape(t, permFixture, ShapePermission) }

func firstAskOfShape(t *testing.T, fixture string, shape CardShape) core.Event {
	t.Helper()
	for _, ev := range recordedAsks(t, fixture) {
		if cardFor(t, ev).Shape() == shape {
			return ev
		}
	}
	t.Fatalf("%s holds no ask of shape %v: this check would assert nothing", fixture, shape)
	return core.Event{}
}

// Every word Wake answers has one line saying what it does, and not one more: a
// description for a command that no longer exists is the lie this exists to
// prevent. The words are the router's three maps, read here the way
// TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising reads them.
func TestEveryCommandWakeOwnsHasExactlyOneHelpLine(t *testing.T) {
	owned := wakeOwned()
	for word := range bareOnlyCommands {
		owned[word] = struct{}{}
	}
	for word := range owned {
		line, ok := commandHelp[word]
		if !ok || strings.TrimSpace(line.what) == "" {
			t.Errorf("Wake answers %q and commandHelp says nothing of it: write the line beside the command in slashhelp.go", configureVerb(word))
		}
	}
	for word := range commandHelp {
		if _, ok := owned[word]; !ok {
			t.Errorf("commandHelp describes %q and Wake answers no such command", configureVerb(word))
		}
	}
	if len(owned) < 15 {
		t.Fatalf("only %d commands found: the scan is broken", len(owned))
	}
}

// The view draws every verb, tagged with how it is aimed and where it works,
// all of it read from the maps the router decides by.
func TestTheHelpDrawsEveryVerbWithItsTargetAndScope(t *testing.T) {
	rows := map[string]helpRow{}
	for _, r := range sectionNamed(t, commandsSectionTitle).rows {
		rows[strings.Fields(r.lead)[0]] = r
	}
	for _, verb := range wakeVerbs() {
		r, ok := rows[verb]
		if !ok {
			t.Errorf("the Commands section draws no row for %s", verb)
			continue
		}
		word := strings.TrimPrefix(verb, SlashPrefix)
		if _, aimed := roomTargetCommands[word]; aimed && !strings.Contains(r.lead, agentPrefix+"who") {
			t.Errorf("%s takes an @who (roomTargetCommands) and its row says %q", verb, r.lead)
		}
		if _, only := roomCommands[word]; only && !strings.Contains(r.text, helpRoomOnly) {
			t.Errorf("%s is the room's alone (roomCommands) and its row says %q", verb, r.text)
		}
		if _, bare := bareOnlyCommands[word]; bare && !strings.Contains(r.text, helpBareOnly) {
			t.Errorf("%s is claimed bare only (bareOnlyCommands) and its row says %q", verb, r.text)
		}
	}
}

func TestTheAddressingRowsAreSpelledFromTheRoutersOwnWords(t *testing.T) {
	leads := ""
	for _, r := range sectionNamed(t, addressingSectionTitle).rows {
		leads += r.lead + "\n"
	}
	for _, want := range []string{
		agentPrefix + "name", agentPrefix + "team", agentPrefix + core.BroadcastName,
		agentPrefix + mentionQuote, bangPrefix + "cmd", agentPrefix + "who " + bangPrefix + "cmd",
	} {
		if !strings.Contains(leads, want) {
			t.Errorf("the Addressing section has no row led by %q:\n%s", want, leads)
		}
	}
}

// slashAtALine is slashInAProse read line by line, so a command opening a line of
// the chapter's fenced list counts as named as one inside a sentence does.
var slashAtALine = regexp.MustCompile("(?m)(?:^|[ \x60(])(/[a-z][a-z-]*)(?:[^a-z/-]|$)")

// The manual's commands chapter stays hand-written and is checked against the
// same set, so a command shipped without a word there fails here too.
func TestTheCommandsChapterNamesEveryCommandWakeOwns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "user_manual", "06-commands.md"))
	if err != nil {
		t.Fatalf("reading the commands chapter: %v", err)
	}
	named := map[string]bool{}
	for _, m := range slashAtALine.FindAllStringSubmatch(string(raw), -1) {
		named[m[1]] = true
	}
	if len(named) < 15 {
		t.Fatalf("found %d commands in the chapter: the scan is broken", len(named))
	}
	for _, verb := range wakeVerbs() {
		if !named[verb] {
			t.Errorf("docs/user_manual/06-commands.md never names %s", verb)
		}
	}
}

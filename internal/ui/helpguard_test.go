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
	for _, s := range helpSections(shellRows(helpSampleUsage)) {
		for _, r := range s.rows {
			b.WriteString(r.lead + " " + r.text + "\n")
		}
	}
	return helpFolded(b.String())
}

func helpFolded(s string) string { return strings.Join(strings.Fields(s), " ") }

func sectionNamed(t *testing.T, title string) helpSection {
	t.Helper()
	for _, s := range helpSections(shellRows(helpSampleUsage)) {
		if s.title == title {
			return s
		}
	}
	t.Fatalf("no section %q in %v", title, helpTitles())
	return helpSection{}
}

func helpTitles() []string {
	var out []string
	for _, s := range helpSections(shellRows(helpSampleUsage)) {
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

// The scan finds a surface by the call `a.<x>Key(m)` and nothing else: a call
// spelled `next.fooKey(m)` or `a.fooKey(m, x)` would not be seen, which is a
// convention the tree keeps (keys.go, appupdate.go) and decisions.md states.
//
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
		agentPrefix + "name", agentPrefix + "team", agentPrefix + core.BroadcastName, agentPrefix + core.ManagerName,
		agentPrefix + mentionQuote, agentPrefix + "path", agentPrefix + subagentMention,
		bangPrefix + "cmd", agentPrefix + "who " + bangPrefix + "cmd",
	} {
		if !strings.Contains(leads, want) {
			t.Errorf("the Addressing section has no row led by %q:\n%s", want, leads)
		}
	}
}

// slashAtALine is slashInAProse read line by line, so a command opening a line of
// the chapter's fenced list counts as named as one inside a sentence does.
var slashAtALine = regexp.MustCompile("(?m)(?:^|[ \x60(])(/[a-z][a-z-]*)(?:[^a-z/-]|$)")

// A leading @name reaches one agent or the fleet by the mention mode, so the row
// says both, in the words ⌃T says them in.
func TestTheNameRowSaysWhatBothMentionModesDo(t *testing.T) {
	for _, r := range sectionNamed(t, addressingSectionTitle).rows {
		if r.lead == agentPrefix+"name" {
			if !strings.Contains(r.text, mentionDirectMeans) || !strings.Contains(r.text, mentionOpenMeans) {
				t.Errorf("the %sname row does not say both readings of the mention mode: %q", agentPrefix, r.text)
			}
			return
		}
	}
	t.Fatal("no row led by @name")
}

// The manual's commands chapter stays hand-written and its list of commands is
// checked against the same set, so a command shipped without a line there fails
// here too.
func TestTheCommandsChapterNamesEveryCommandWakeOwns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "user_manual", "06-commands.md"))
	if err != nil {
		t.Fatalf("reading the commands chapter: %v", err)
	}
	// The list: the first fenced block under the composer heading. A command named only
	// in prose, as /wake-help is in its own section, is not a line in it.
	_, afterHeading, found := strings.Cut(string(raw), "## `/…` — in the composer")
	_, rest, opened := strings.Cut(afterHeading, "```\n")
	list, _, closed := strings.Cut(rest, "```")
	if !found || !opened || !closed {
		t.Fatal("the commands chapter has no fenced list under its composer heading: the scan is broken")
	}
	named := map[string]bool{}
	for _, m := range slashAtALine.FindAllStringSubmatch(list, -1) {
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

// teaKeyWords is how each tea.Key a surface's handlers can name is written where
// the help says it, any one of which counts. A key with no entry is a failure, so
// this cannot be where a new key quietly avoids the check - legendKeyNames' shape.
var teaKeyWords = map[string][]string{
	"KeyUp": {"↑"}, "KeyDown": {"↓"}, "KeyLeft": {"←"}, "KeyRight": {"→"},
	"KeyTab": {"⇥"}, "KeyEnter": {"↵"}, "KeyEsc": {"esc"}, "KeySpace": {"space"}, "KeyBackspace": {"⌫"},
	"KeyPgUp": {"PgUp", "⇞"}, "KeyPgDown": {"PgDn", "⇟"}, "KeyHome": {"Home"}, "KeyEnd": {"End"},
	"KeyCtrlD": {"⌃D"}, "KeyCtrlY": {"⌃Y"}, "KeyCtrlB": {"⌃B"}, "KeyCtrlN": {"⌃N"}, "KeyCtrlP": {"⌃P"},
	"KeyCtrlC": {"⌃C"},
}

// teaKeysNotRead are the keys no row writes a word for: a printable key is
// "type to ..." on the line. ⌃C is not among them: a modal that binds it says
// what it does there, because the kill switch reads a ⌃C that appeared to do
// nothing as the start of ⌃C⌃C, and the workflow save dialog does exactly that.
var teaKeysNotRead = map[string]bool{"KeyRunes": true}

// typesInto are the keys a text box takes as text, per surface, which are not
// read as keys there.
var typesInto = map[string][]string{
	"resumePickerKey": {"KeySpace", "KeyBackspace"},
	"workflowKey":     {"KeySpace", "KeyBackspace"},
	"helpKey":         {"KeySpace"},
}

// surfaceHandlers is every method that reads a surface's keys, by the App method
// App.key and App.update call. A surface split across methods lists each.
var surfaceHandlers = map[string][][2]string{
	"boardKey":        {{"board.go", "boardKey"}},
	"pickerKey":       {{"picker.go", "pickerKey"}},
	"rewindKey":       {{"rewind.go", "rewindKey"}},
	"resumePickerKey": {{"resumepicker.go", "resumePickerKey"}},
	"helpKey":         {{"help.go", "helpKey"}},
	"completionKey":   {{"completion.go", "completionKey"}},
	"cardKey":         {{"cardkeys.go", "cardKey"}},
	"statusKey":       {{"statuscmd.go", "statusKey"}},
	"mcpMenuKey": {
		{"mcpmenu.go", "mcpMenuKey"}, {"mcpmenu.go", "mcpListKey"}, {"mcpmenu.go", "mcpDetailKey"}, {"mcpmenu.go", "mcpToolsKey"},
	},
	"workflowKey": {
		{"workflowview.go", "workflowKey"}, {"workflowview.go", "listKey"}, {"workflowview.go", "runKey"},
		{"workflowview.go", "agentKey"}, {"workflowsave.go", "saveKey"},
	},
}

// teaKeysIn is every tea.Key... a method names, in a case clause or a comparison.
func teaKeysIn(t *testing.T, file, method string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	ast.Inspect(funcDeclIn(t, file, method).Body, func(n ast.Node) bool {
		if name := teaKeyName(asExpr(n)); name != "" && name != "KeyMsg" && name != "KeyType" {
			out[name] = true
		}
		return true
	})
	return out
}

func asExpr(n ast.Node) ast.Expr {
	e, _ := n.(ast.Expr)
	return e
}

// Every key a surface's handlers bind is written in that surface's rows - not
// only a row for the surface. The board handled Home and End and drew PgUp/PgDn
// alone, /workflows binds → and ←, the cards ←→ and space: a reader-exists guard
// cannot see any of them. Printable keys (a letter, a digit) are not scanned, and
// a call spelled other than a.<x>Key(m) is not found by the guard above: both are
// stated limits, in decisions.md.
func TestEveryKeyASurfaceBindsIsNamedInItsRows(t *testing.T) {
	if len(surfaceHandlers) < 10 {
		t.Fatalf("%d surfaces listed: the table is broken", len(surfaceHandlers))
	}
	for via, handlers := range surfaceHandlers {
		var rows strings.Builder
		for _, s := range helpSurfaces() {
			if s.via == via {
				rows.WriteString(s.keys + " ")
			}
		}
		if rows.Len() == 0 {
			t.Errorf("%s has no rows in the help", via)
			continue
		}
		bound := map[string]bool{}
		for _, h := range handlers {
			maps.Copy(bound, teaKeysIn(t, h[0], h[1]))
		}
		if len(bound) < 1 {
			t.Fatalf("%s: found no keys: the scan is broken and this asserts nothing", via)
		}
		for name := range bound {
			if teaKeysNotRead[name] || slices.Contains(typesInto[via], name) {
				continue
			}
			words, known := teaKeyWords[name]
			if !known {
				t.Errorf("%s binds tea.%s and teaKeyWords does not say how the help writes it: add it there", via, name)
				continue
			}
			if !slices.ContainsFunc(words, func(w string) bool { return strings.Contains(rows.String(), w) }) {
				t.Errorf("%s binds tea.%s and its rows never write %v: %q", via, name, words, rows.String())
			}
		}
	}
}

// /new's flags are the parser's own table, so a flag added there reaches the help.
func TestTheNewCommandsHelpNamesEveryFlagItsParserTakes(t *testing.T) {
	line := commandHelp[newCommand].what
	for _, f := range newFlagTable {
		if !strings.Contains(line, f.name) {
			t.Errorf("the parser takes %s and the help line for %s says %q", f.name, newVerb, line)
		}
	}
	if len(newFlagTable) < 6 {
		t.Fatalf("%d flags in newFlagTable: the scan is broken", len(newFlagTable))
	}
}

// ⌃C parks from every view and menu but one, and the one is named: in the workflow
// save dialog it closes the dialog alone, so a reader who trusts "⌃C parks" is
// told where it does not.
func TestTheHelpNamesTheSaveDialogsCtrlC(t *testing.T) {
	var rows strings.Builder
	for _, s := range helpSurfaces() {
		if s.via == "workflowKey" {
			rows.WriteString(s.keys + " ")
		}
	}
	if !strings.Contains(rows.String(), "⌃C closes the view and parks") || !strings.Contains(rows.String(), "save dialog") {
		t.Errorf("the /workflows rows do not say what ⌃C does, or where it does not park: %q", rows.String())
	}
}

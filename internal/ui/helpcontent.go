package ui

// What /wake-help lists, and where each section is read from. Nothing here is a
// second copy of a binding or a command: a key is a legendEntries entry, a
// command is a word in slash.go's maps, a surface's key line is the constant or
// producer that surface draws, and the shell section is `wake help`'s own text.
// helpguard_test.go holds each of them to its source in both directions.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	keysSectionTitle       = "Keys"
	surfacesSectionTitle   = "While a menu or view is up"
	commandsSectionTitle   = "Commands"
	addressingSectionTitle = "Addressing"
	shellSectionTitle      = "At the shell"
)

// helpRow is one line of a section: what to press or type, and what it does. A
// sub row is a heading inside the section, drawn as it stands.
type helpRow struct {
	lead, text string
	sub        bool
}

type helpSection struct {
	title string
	rows  []helpRow
}

// helpSurface is one thing that reads keys before the composer does: via is the
// App method that reads them, which the guard matches to App.key and App.update.
type helpSurface struct{ via, name, keys string }

// helpSections is every section the view draws. The first four are the same for
// every window and built once; the shell section is the lines this App was
// handed, and absent without any.
func helpSections(shell []helpRow) []helpSection {
	out := slices.Clone(staticHelp())
	if len(shell) > 0 {
		out = append(out, helpSection{shellSectionTitle, shell})
	}
	return out
}

var staticHelp = sync.OnceValue(func() []helpSection {
	surfaces := helpSurfaces()
	rows := make([]helpRow, len(surfaces))
	for i, s := range surfaces {
		rows[i] = helpRow{lead: s.name, text: s.keys}
	}
	return []helpSection{
		{keysSectionTitle, keyRows()},
		{surfacesSectionTitle, rows},
		{commandsSectionTitle, commandRows()},
		{addressingSectionTitle, addressingRows()},
	}
})

// keyRows is every key the legend names, the composer's newline keys read off
// its own binding, and the one key that is read off the tty before Bubble Tea.
func keyRows() []helpRow {
	rows := make([]helpRow, 0, len(legendEntries)+2)
	for _, e := range legendEntries {
		rows = append(rows, helpRow{lead: e.glyph, text: e.help})
	}
	return append(rows,
		helpRow{lead: newlineGlyphs(), text: "a newline in the draft, rather than sending it"},
		// cmd/wake/killswitch.go reads it: no tea.Key case exists to derive it from.
		helpRow{lead: "⌃C⌃C", text: "emergency exit if Wake stops answering: it closes at once and every agent keeps running"},
	)
}

// newlineGlyphs spells the keys the composer's own binding takes.
func newlineGlyphs() string {
	var glyphs []string
	for _, k := range NewComposer().ta.KeyMap.InsertNewline.Keys() {
		glyphs = append(glyphs, keyGlyph(k))
	}
	return strings.Join(glyphs, " ")
}

var modifierGlyphs = []struct{ prefix, glyph string }{{"ctrl+", "⌃"}, {"alt+", "⌥"}, {"shift+", "⇧"}}

// keyGlyph is a bubbletea key name as the legend spells it: `ctrl+j` is ⌃J.
func keyGlyph(name string) string {
	mods := ""
	for stripped := true; stripped; {
		stripped = false
		for _, m := range modifierGlyphs {
			if rest, ok := strings.CutPrefix(name, m.prefix); ok {
				mods, name, stripped = mods+m.glyph, rest, true
			}
		}
	}
	if name == "enter" {
		return mods + "↵"
	}
	return mods + strings.ToUpper(name)
}

// helpSurfaces is every surface that reads keys before the composer does, with
// the key line it draws - or, for a picker that draws none, the line that sits
// beside its binding. via names the method that reads them.
func helpSurfaces() []helpSurface {
	join := func(keys []string) string { return strings.Join(keys, " · ") }
	asks := func(settle string) string { return settle + cardDot + cardConfirmHint + cardDot + cardInterruptHint }
	// The longest line a question draws: ⌃E is there only for an option with a description.
	question := func(move, choose string) string {
		return move + cardKeySep + choose + cardKeySep + cardDenyLabel + cardKeySep + cardExpandDetail + cardDot + cardDismissHint
	}
	return []helpSurface{
		{"boardKey", boardVerb + " rows", boardKeyLineRows},
		{"boardKey", boardVerb + " tiles", boardKeyLineTiles},
		{"boardKey", boardVerb + " scrolling", boardScrollKeys},
		{"workflowKey", workflowsVerb + " list", join(workflowListKeys)},
		{"workflowKey", workflowsVerb + " run", join(workflowRunKeys(false))},
		{"workflowKey", workflowsVerb + " run, running", join(workflowRunKeys(true))},
		{"workflowKey", workflowsVerb + " agent", join(agentKeys(false))},
		{"workflowKey", workflowsVerb + " agent, expanded", join(agentKeys(true))},
		{"workflowKey", workflowsVerb + " save", saveDialogKeys},
		{"workflowKey", workflowsVerb + " also", workflowAliasKeys},
		{"resumePickerKey", resumeVerb + " in the room", ResumePicker{Multi: true}.keyHint()},
		{"resumePickerKey", resumeVerb + " in a conversation", ResumePicker{}.keyHint()},
		{"resumePickerKey", resumeVerb + " also", helpParkKey},
		{"mcpMenuKey", mcpVerb + " servers", MCPMenu{Level: mcpList}.keyHint()},
		{"mcpMenuKey", mcpVerb + " one server", MCPMenu{Level: mcpDetail}.keyHint()},
		{"mcpMenuKey", mcpVerb + " tools", MCPMenu{Level: mcpTools}.keyHint()},
		{"mcpMenuKey", mcpVerb + " also", mcpAliasKeys},
		{"statusKey", statusVerb, statusHint},
		{"statusKey", statusVerb + " also", helpParkKey},
		{"cardKey", "permission ask", asks(cardAllowKeys)},
		{"cardKey", "plan ask", asks(cardPlanKeys)},
		{"cardKey", "question ask", question(cardMoveKeys, chooseKeys(cardMaxOptions))},
		{"cardKey", "question ask, several answers", question(cardMoveTickKeys, fmt.Sprintf(cardToggleFmt, cardMaxOptions))},
		{"cardKey", "question ask, also", cardStepKeys},
		{"cardKey", "question ask, several answers, also", cardTickKey},
		{"cardKey", "question ask, answer box", answerModeKeys},
		{"cardKey", "question ask, deny reason", denyModeKeys},
		{"cardKey", "question ask, review", reviewKeys},
		{"pickerKey", configureVerb(effortCommand) + " and " + configureVerb(modelCommand), pickerKeys},
		{"rewindKey", "rewind picker", rewindKeys},
		{"completionKey", "slash and " + agentPrefix + " menu", completionKeys + hintSep + completionAliases},
		{"helpKey", wakeHelpVerb, helpKeys},
		{"helpKey", wakeHelpVerb + " also", helpParkKey},
	}
}

// addressingRows are the forms a draft's first word can take, spelled from the
// router's own words: its prefixes, the broadcast and manager names, the quote.
func addressingRows() []helpRow {
	at := agentPrefix
	return []helpRow{
		{lead: at + "name", text: "address one agent; the mention mode (⌃T) decides how far it reaches: " + mentionDirectMeans + ", or " + mentionOpenMeans},
		{lead: at + "team", text: "send to every live member of a team (" + teamVerb + " groups them)"},
		{lead: at + core.BroadcastName, text: "send to every live agent; the target line says what that costs"},
		{lead: at + core.ManagerName, text: "send to the manager, the session that can see and run the fleet"},
		{lead: at + mentionQuote + "session name" + mentionQuote, text: "relay to one of the machine's other Claude sessions, through the manager"},
		{lead: at + "path", text: "a file in the agent's directory: a word that names no agent or team is a path, offered by the menu and passed to the agent as typed"},
		{lead: at + subagentMention + "<type>", text: "ask the agent to use one of its subagent types; the menu lists them"},
		{lead: at + "who <command>", text: "aim one command at one agent from the room, as " + at + "alex " + statusVerb + " does"},
		{lead: bangPrefix + "cmd", text: "run a shell line where this conversation runs; the output is drawn and goes to its agent (a bare one in the room reaches nobody)"},
		{lead: at + "who " + bangPrefix + "cmd", text: "in the room: run in that agent's directory and send the output to it; " + at + "team and " + at + core.BroadcastName + " are refused"},
	}
}

// usageRow splits a usage line into the words that are typed and what they do:
// the two columns `wake help` draws, a run of spaces apart.
var usageRow = regexp.MustCompile(`^\s+(\S.*?)\s{2,}(\S.*)$`)

// shellRows are `wake help`'s own lines: an unindented line is a heading of the
// group under it, an indented one a verb or a flag and its sentence, and an
// indented one with no sentence of its own the rest of the row above.
func shellRows(usage string) []helpRow {
	var rows []helpRow
	for _, line := range strings.Split(usage, "\n") {
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "" || trimmed == "usage:": // blank, or the title the section already wears
		case line[0] != ' ' && line[0] != '\t':
			rows = append(rows, helpRow{text: trimmed, sub: true})
		default:
			m := usageRow.FindStringSubmatch(line)
			switch {
			case m != nil:
				rows = append(rows, helpRow{lead: m[1], text: m[2]})
			case len(rows) > 0 && !rows[len(rows)-1].sub && rows[len(rows)-1].text != "":
				// A sentence that wrapped: it carries on the row above, whole.
				rows[len(rows)-1].text += " " + trimmed
			default:
				rows = append(rows, helpRow{lead: trimmed})
			}
		}
	}
	return rows
}

// helpFilter narrows sections to the rows holding every word of the query, in
// either column, and drops a section left with none. A heading is kept while a
// row of its group is, and a query that names the heading brings the whole group.
// An empty query is all.
func helpFilter(sections []helpSection, query string) []helpSection {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return sections
	}
	var out []helpSection
	for _, s := range sections {
		if rows := filteredRows(s.rows, words); len(rows) > 0 {
			out = append(out, helpSection{s.title, rows})
		}
	}
	return out
}

// filteredRows is one section's rows that match, group by group: the rows before
// the first heading, then each heading with the rows under it.
func filteredRows(rows []helpRow, words []string) []helpRow {
	var out []helpRow
	for len(rows) > 0 {
		group := rows[:1+countUntilHeading(rows[1:])]
		rows = rows[len(group):]
		head, body := helpRow{}, group
		if group[0].sub {
			head, body = group[0], group[1:]
		}
		var kept []helpRow
		for _, r := range body {
			if helpMatches(r, words) || (head.sub && helpMatches(head, words)) {
				kept = append(kept, r)
			}
		}
		switch {
		case len(kept) > 0 && head.sub:
			out = append(out, head)
			fallthrough
		case len(kept) > 0:
			out = append(out, kept...)
		}
	}
	return out
}

// countUntilHeading is how many rows run before the next heading.
func countUntilHeading(rows []helpRow) int {
	for i, r := range rows {
		if r.sub {
			return i
		}
	}
	return len(rows)
}

func helpMatches(r helpRow, words []string) bool {
	hay := strings.ToLower(r.lead + " " + r.text)
	return !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) })
}

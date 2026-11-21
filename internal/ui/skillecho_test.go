package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// A message that opens with a /command is the operator's own turn, the words they
// typed, and reaches the agent byte for byte. It used to be drawn as one orange
// row, cut with an ellipsis, so a skill called with a long argument lost the
// argument. It is an ordinary turn now with the command word in Claude's skill
// colour; Wake's own commands never reach a transcript, so none needs a row.

const longSkillCall = "/init-orch you are responsible for the historical contact ingest branch. " +
	"Own the plan and report back when the ingest is verified."

// painted is one drawn cell with the foreground and background in force when it
// was drawn ("" = none).
type painted struct {
	r      rune
	fg, bg string
}

// paintedCells reads every cell of out, blanks included, following SGR the way a
// terminal does, so a colour that a reset or a row break dropped shows as the
// wrong paint.
func paintedCells(out string) []painted {
	var cells []painted
	fg, bg := "", ""
	for i := 0; i < len(out); {
		if out[i] == 0x1b && i+1 < len(out) && out[i+1] == '[' {
			end := i + 2
			for end < len(out) && (out[end] < 0x40 || out[end] > 0x7e) {
				end++
			}
			if end < len(out) && out[end] == 'm' {
				fg, bg = paintAfter(fg, bg, strings.Split(out[i+2:end], ";"))
			}
			i = end + 1
			continue
		}
		r := []rune(out[i:])[0]
		i += len(string(r))
		if r != '\n' {
			cells = append(cells, painted{r, fg, bg})
		}
	}
	return cells
}

// paintOf is paintedCells without the blanks: the cells that carry the words.
func paintOf(out string) []painted {
	var words []painted
	for _, c := range paintedCells(out) {
		if c.r != ' ' {
			words = append(words, c)
		}
	}
	return words
}

// paintAfter applies one SGR's parameters to the colours in force.
func paintAfter(fg, bg string, params []string) (string, string) {
	for j := 0; j < len(params); j++ {
		switch params[j] {
		case "", "0":
			fg, bg = "", ""
		case "39":
			fg = ""
		case "49":
			bg = ""
		case "38", "48":
			if j+4 < len(params) && params[j+1] == "2" {
				rgb := strings.Join(params[j+2:j+5], ";")
				if params[j] == "38" {
					fg = rgb
				} else {
					bg = rgb
				}
			}
			j += 4
		}
	}
	return fg, bg
}

// skillRGB is Skill as paintOf reads it, derived from the style so the test is
// about whether the colour is applied and not about how lipgloss spells it.
func skillRGB(t *testing.T) string {
	t.Helper()
	return fgRGB(t, Skill)
}

func fgRGB(t *testing.T, c lipgloss.TerminalColor) string {
	t.Helper()
	cells := paintOf(lipgloss.NewStyle().Foreground(c).Render("x"))
	if len(cells) != 1 || cells[0].fg == "" {
		t.Fatalf("lipgloss drew no foreground for the colour at this profile: %q", lipgloss.NewStyle().Foreground(c).Render("x"))
	}
	return cells[0].fg
}

// bgRGB is a ground colour as paintedCells reads it.
func bgRGB(t *testing.T, c lipgloss.TerminalColor) string {
	t.Helper()
	cells := paintedCells(lipgloss.NewStyle().Background(c).Render("x"))
	if len(cells) != 1 || cells[0].bg == "" {
		t.Fatalf("lipgloss drew no background for the colour at this profile")
	}
	return cells[0].bg
}

// requireWord pins what a turn is: the cells of word wear colour and no cell
// after or before it does, so the word is tinted and the message is not.
func requireWord(t *testing.T, out, word, colour string) {
	t.Helper()
	cells := paintOf(out)
	var drawn strings.Builder
	for _, c := range cells {
		drawn.WriteRune(c.r)
	}
	idx := strings.Index(drawn.String(), squash(word))
	if idx < 0 {
		t.Fatalf("%q is not drawn in the turn:\n%q", word, ansi.Strip(out))
	}
	from := len([]rune(drawn.String()[:idx]))
	to := from + len([]rune(squash(word)))
	for i, c := range cells {
		inWord := i >= from && i < to
		switch {
		case inWord && c.fg != colour:
			t.Errorf("%q of %q is drawn in %q, want %q", string(c.r), word, c.fg, colour)
		case !inWord && c.fg == colour:
			t.Errorf("%q outside %q is drawn in its colour %q", string(c.r), word, colour)
		}
	}
}

func squash(s string) string { return strings.Join(strings.Fields(s), "") }

// requireWhole pins that the turn kept every word of what was typed - nothing cut,
// no ellipsis - inside the pane.
func requireWhole(t *testing.T, out, typed string, width int) {
	t.Helper()
	plain := ansi.Strip(out)
	if strings.Contains(plain, ellipsis) || strings.Contains(plain, "⌘") {
		t.Errorf("the turn still reads as a cut command row:\n%s", plain)
	}
	if !strings.Contains(squash(plain), squash(typed)) {
		t.Errorf("words of %q are lost from the turn:\n%s", typed, plain)
	}
	for i, row := range strings.Split(out, "\n") {
		if w := lipgloss.Width(row); w > width {
			t.Errorf("row %d is %d cells, wider than the pane's %d: %q", i, w, width, row)
		}
	}
	if strings.Contains(out, "\t") {
		t.Errorf("a tab reaches the terminal: %q", out)
	}
}

func TestASkillCallInAConversationIsAFullTurnWithTheWordTinted(t *testing.T) {
	trueColor(t)
	for _, width := range []int{80, 40, 24} {
		out := userBlock(core.Event{Kind: core.KindUserText, Text: longSkillCall}, width)
		if !strings.Contains(ansi.Strip(out), userLabel) {
			t.Errorf("width %d: a skill call is not headed as your turn (%q):\n%s", width, userLabel, ansi.Strip(out))
		}
		requireWhole(t, out, longSkillCall, width)
		requireWord(t, out, "/init-orch", skillRGB(t))
	}
}

// The token wraps with the text: a name longer than the pane breaks across rows
// and every cell of it keeps the colour, the argument after it none.
func TestATintedWordKeepsItsColourWhereTheTurnWrapsIt(t *testing.T) {
	trueColor(t)
	const word = "/a-very-long-skill-name-that-is-longer-than-the-pane"
	typed := word + " and then an argument"
	out := userBlock(core.Event{Kind: core.KindUserText, Text: typed}, 20)
	requireWhole(t, out, typed, 20)
	requireWord(t, out, word, skillRGB(t))
	if rows := strings.Count(out, "\n"); rows < 4 {
		t.Fatalf("precondition: the turn wraps onto several rows, got %d line breaks:\n%s", rows, ansi.Strip(out))
	}
}

func TestAClaudeBuiltInIsAnOrdinaryTurnToo(t *testing.T) {
	trueColor(t)
	for _, typed := range []string{"/compact keep the plan", "/context", "/model opus", "/clear", "/effort high",
		"/codex:rescue look at the flake", "/superpowers:brainstorming a thing", "/deploy_prod now"} {
		out := userBlock(core.Event{Kind: core.KindUserText, Text: typed}, 60)
		word, _, _ := strings.Cut(typed, " ")
		if !strings.Contains(ansi.Strip(out), userLabel) {
			t.Errorf("%q is not headed as your turn:\n%s", typed, ansi.Strip(out))
		}
		requireWhole(t, out, typed, 60)
		requireWord(t, out, word, skillRGB(t))
	}
}

func TestOnlyACommandWordIsTinted(t *testing.T) {
	trueColor(t)
	for _, typed := range []string{"/usr/local/bin is where it lives", "/foo.md is the file", "/123go", "//x", "/",
		"look at /foo", "please ship the fix", ""} {
		out := userBlock(core.Event{Kind: core.KindUserText, Text: typed}, 60)
		for _, c := range paintOf(out) {
			if c.fg == skillRGB(t) {
				t.Errorf("%q has a word in the skill colour: %q", typed, ansi.Strip(out))
				break
			}
		}
	}
}

// A line break after the word survives, as it does in any turn.
func TestABreakAfterTheWordIsKept(t *testing.T) {
	trueColor(t)
	out := userBlock(core.Event{Kind: core.KindUserText, Text: "/foo\nbar baz"}, 60)
	requireWord(t, out, "/foo", skillRGB(t))
	rows := strings.Split(ansi.Strip(out), "\n")
	if len(rows) < 3 || !strings.Contains(rows[1], "/foo") || strings.Contains(rows[1], "bar") || !strings.Contains(rows[2], "bar baz") {
		t.Errorf("the break after /foo was not kept:\n%s", ansi.Strip(out))
	}
}

// What was routed from the room is the operator's turn here too. The handle is
// not tinted in a conversation - it never was - but the command word behind it is.
func TestARoomRoutedSkillCallTintsTheWordNotTheHandleInAConversation(t *testing.T) {
	trueColor(t)
	typed := "@john " + longSkillCall
	out := userBlock(core.Event{Kind: core.KindUserText, Text: typed, FromRoom: true}, 60)
	if !strings.Contains(ansi.Strip(out), roomTurnLabel) {
		t.Errorf("a room-routed turn lost its %q head:\n%s", roomTurnLabel, ansi.Strip(out))
	}
	requireWhole(t, out, typed, 60)
	requireWord(t, out, "/init-orch", skillRGB(t))
	for _, c := range paintOf(out) {
		if c.fg == fgRGB(t, Mention) {
			t.Errorf("a handle is tinted in a conversation, which it never was:\n%q", out)
			break
		}
	}
}

func TestASkillCallInTheRoomIsAFullTurnWithTheWordTinted(t *testing.T) {
	trueColor(t)
	for _, width := range []int{80, 40, 24} {
		out := youSaid(longSkillCall, width)
		requireWhole(t, out, longSkillCall, width)
		requireWord(t, out, "/init-orch", skillRGB(t))
	}
}

// `@who /skill args` keeps the handle in the handle's colour and tints the word
// behind it, and `@who` alone still tints only the handle.
func TestASkillCallBehindAHandleInTheRoomTintsBoth(t *testing.T) {
	trueColor(t)
	typed := "@john " + longSkillCall
	for _, width := range []int{80, 40, 24} {
		out := youSaid(typed, width)
		requireWhole(t, out, typed, width)
		requireWord(t, out, "@john", fgRGB(t, Mention))
		requireWord(t, out, "/init-orch", skillRGB(t))
	}

	out := youSaid("@john ship /it", 60)
	requireWord(t, out, "@john", fgRGB(t, Mention))
	for _, c := range paintOf(out) {
		if c.fg == skillRGB(t) {
			t.Errorf("a slash that is not the command word is tinted: %q", ansi.Strip(out))
			break
		}
	}
}

// A reopened conversation and a board tile draw the typed turn the way the live
// one is drawn: the transcript hands back the same event and the tile is the same
// conversation drawn small.
func TestARestoredSkillCallAndABoardTileDrawLikeTheLiveOne(t *testing.T) {
	trueColor(t)
	// As claude records a skill call, not as it was typed: its name twice, then
	// the arguments (testdata/stream/loop-self-paced-end.jsonl, line 4).
	args := strings.TrimPrefix(longSkillCall, "/init-orch ")
	line := `{"type":"user","userType":"external","isSidechain":false,"sessionId":"11111111-1111-4111-8111-111111111111",` +
		`"uuid":"44444444-4444-4444-8444-444444444444","timestamp":"2026-09-01T00:00:00.000Z",` +
		`"message":{"role":"user","content":"<command-message>init-orch</command-message>\n<command-name>/init-orch</command-name>\n<command-args>` +
		args + `</command-args>"}}`
	evs, err := core.DecodeTranscriptLine([]byte(line))
	if err != nil || len(evs) != 1 {
		t.Fatalf("a recorded skill call restored as %d events (err %v), want 1", len(evs), err)
	}
	live := userBlock(core.Event{Kind: core.KindUserText, Text: longSkillCall}, 60)
	if got := userBlock(evs[0], 60); got != live {
		t.Errorf("the restored turn is not drawn as the live one:\nrestored %q\nlive     %q", got, live)
	}

	a := tiledBoardApp().ensureBoardDMs()
	a = a.foldBoard("s1", evs[0])
	_, tile := a.boardDMs["s1"].transcriptWindow(40, 12)
	requireWhole(t, tile, longSkillCall, 40)
	requireWord(t, tile, "/init-orch", skillRGB(t))
}

// Only a turn the operator typed is theirs to tint. A replayed frame and a prompt
// an agent handed a subagent keep their own heads, even when the text happens to
// open with a slash.
func TestAReplayedOrSubagentSlashStaysProse(t *testing.T) {
	trueColor(t)
	const w = 60
	replayed := userBlock(core.Event{Kind: core.KindUserText, Text: "/foo bar", Echoed: true}, w)
	if !strings.Contains(ansi.Strip(replayed), echoedLabel) {
		t.Errorf("a replayed frame lost its %q head:\n%s", echoedLabel, ansi.Strip(replayed))
	}
	handed := userBlock(core.Event{
		Kind:     core.KindUserText,
		Text:     "/foo bar",
		Subagent: &core.Subagent{Type: "reviewer"},
	}, w)
	if !strings.Contains(ansi.Strip(handed), promptLabel) {
		t.Errorf("a subagent prompt lost its %q head:\n%s", promptLabel, ansi.Strip(handed))
	}
	for name, out := range map[string]string{"replayed": replayed, "handed": handed} {
		for _, c := range paintOf(out) {
			if c.fg == skillRGB(t) && c.r == '/' {
				t.Errorf("a %s frame has its slash word tinted as the operator's own:\n%q", name, out)
				break
			}
		}
	}
}

// A drag over the turn copies what was typed, not the pane's wrapping: the rows
// still match back to the text with the word tinted inside them.
func TestACopyOfATintedTurnRejoinsWhatTheWrapBroke(t *testing.T) {
	trueColor(t)
	typed := "/a-very-long-skill-name-that-is-longer-than-the-pane " + longSkillCall
	for _, width := range []int{80, 20} {
		for name, rows := range map[string][]string{
			"conversation": strings.Split(userBlock(core.Event{Kind: core.KindUserText, Text: typed}, width), "\n"),
			"room":         strings.Split(youSaid(typed, width), "\n"),
		} {
			if got := typedRejoins(rows, typed); len(got) != len(rows) || got == nil {
				t.Errorf("%s at width %d: the rows do not match back to what was typed, so a copy keeps every wrap break:\n%s",
					name, width, ansi.Strip(strings.Join(rows, "\n")))
			}
		}
	}
}

// The tint must not change how the turn wraps. A span that carried a background
// made lipgloss read the whole first line as one word, so a skill call broke
// mid-word ("/init-" and "orch you are …") where plain text breaks at a space.
// At every width the turn reads exactly as the same words without a tint.
func TestATintedTurnWrapsExactlyAsPlainTextDoes(t *testing.T) {
	trueColor(t)
	for _, typed := range []string{
		longSkillCall,
		"@john " + longSkillCall,
		"@john  /init-orch\ttabbed and  doubled",
		"@john please ship the fix and tell me when the rollout is done",
	} {
		for w := minBlockWidth; w <= 140; w++ {
			plain := ansi.Strip(shadedOwn(typed, w))
			if got := ansi.Strip(youSaid(typed, w)); got != plain {
				t.Fatalf("width %d: the room wraps %q differently from plain text:\n%s\nplain:\n%s", w, typed, got, plain)
			}
			conv := ansi.Strip(userBlock(core.Event{Kind: core.KindUserText, Text: typed, FromRoom: strings.HasPrefix(typed, "@")}, w))
			if _, body, _ := strings.Cut(conv, "\n"); body != plain {
				t.Fatalf("width %d: a conversation wraps %q differently from plain text:\n%s\nplain:\n%s", w, typed, body, plain)
			}
		}
	}
}

// And the shaded ground stays one solid rectangle: every cell of every row under
// the head, the word's and the padding's, wears the turn's own ground.
func TestATintedTurnKeepsASolidGround(t *testing.T) {
	trueColor(t)
	own := bgRGB(t, Own)
	for _, typed := range []string{longSkillCall, "@john " + longSkillCall} {
		for _, w := range []int{80, 40, 24} {
			rooms := map[string][]string{
				"room": strings.Split(youSaid(typed, w), "\n"),
				"conversation": strings.Split(userBlock(core.Event{
					Kind: core.KindUserText, Text: typed, FromRoom: strings.HasPrefix(typed, "@"),
				}, w), "\n")[1:],
			}
			for name, rows := range rooms {
				for i, row := range rows {
					for col, c := range paintedCells(row) {
						if c.bg != own {
							t.Fatalf("%s at width %d: row %d cell %d (%q) has ground %q, want %q:\n%q", name, w, i, col, string(c.r), c.bg, own, row)
						}
					}
				}
			}
		}
	}
}

// What follows an @ is a path as often as a handle, and a path is no command:
// `@internal/ui` is one word, so the `/ui` in it is not a skill call.
func TestAPathBehindAnAtIsNotACommand(t *testing.T) {
	trueColor(t)
	for _, typed := range []string{"@internal/ui please review this", "@cmd/wake is where the binary lives", "@john/foo bar"} {
		for name, out := range map[string]string{
			"room":         youSaid(typed, 60),
			"conversation": userBlock(core.Event{Kind: core.KindUserText, Text: typed}, 60),
			"routed":       userBlock(core.Event{Kind: core.KindUserText, Text: typed, FromRoom: true}, 60),
		} {
			for _, c := range paintOf(out) {
				if c.fg == skillRGB(t) {
					t.Errorf("%s: a slash inside the word is drawn as a skill call in %q:\n%q", name, typed, ansi.Strip(out))
					break
				}
			}
		}
	}
}

// A conversation routes nothing: `@sydney /review x` typed in it reaches claude
// as that text, which opens with an @ and so calls nothing. Only a message the
// room routed had its handle taken off before it was sent.
func TestAConversationReadsAHandleAsTextNotARoute(t *testing.T) {
	trueColor(t)
	out := userBlock(core.Event{Kind: core.KindUserText, Text: "@sydney /review what do you think"}, 60)
	for _, c := range paintOf(out) {
		if c.fg == skillRGB(t) || c.fg == fgRGB(t, Mention) {
			t.Fatalf("a typed @handle and what follows it is tinted in a conversation:\n%q", ansi.Strip(out))
		}
	}

	out = userBlock(core.Event{Kind: core.KindUserText, Text: "@john ship it", FromRoom: true}, 60)
	for _, c := range paintOf(out) {
		if c.fg == skillRGB(t) || c.fg == fgRGB(t, Mention) {
			t.Fatalf("a routed handle with no command behind it is tinted in a conversation:\n%q", ansi.Strip(out))
		}
	}
}

// Wake's own commands are claimed before anything is sent, on both surfaces
// (submit asks slash first), so a typed one is never a turn in a transcript -
// which is why no orange row was needed for them. The one turn a Wake menu
// writes is the effort and model picker's, `/effort high`: claude's own
// command with its argument, sent to the agent as a typed one is.
func TestEveryWakeCommandIsClaimedBeforeAnythingIsSent(t *testing.T) {
	// A fresh conversation per draft: a refused command keeps its draft, and the
	// next one would be typed onto it.
	inConversation := func() App {
		a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
		return a.openDMWith("s1", a.agentName("s1"))
	}
	for word := range commands {
		for _, draft := range []string{configureVerb(word), configureVerb(word) + " x"} {
			if _, _, ok := inConversation().slash(draft); !ok {
				t.Errorf("%q is not claimed by Wake, so submit would send it to the agent", draft)
			}
		}
	}
	for _, word := range wakeWords() {
		if word == clearCommand {
			continue // claude's own in a conversation: sent, and drawn as the turn it is
		}
		drafts := []string{configureVerb(word)}
		if _, bareOnly := bareOnlyCommands[word]; !bareOnly {
			drafts = append(drafts, configureVerb(word)+" x") // with an argument /effort and /model are claude's
		}
		for _, draft := range drafts {
			b, _ := pressKey(inConversation().withDraft(draft), tea.KeyMsg{Type: tea.KeyEnter})
			for id := range b.dms {
				for _, ev := range dmEvents(b, id) {
					if ev.Kind == core.KindUserText {
						t.Errorf("%q left %q in %s's conversation", draft, ev.Text, id)
					}
				}
			}
		}
	}
}

// Whatever whitespace sits between the handle and the word, the word is the call.
func TestAWordIsTintedBehindAnyGapAfterTheHandle(t *testing.T) {
	trueColor(t)
	for _, gap := range []string{" ", "  ", "\t"} {
		typed := "@john" + gap + "/init-orch and the rest"
		requireWord(t, youSaid(typed, 60), "/init-orch", skillRGB(t))
		requireWord(t, userBlock(core.Event{Kind: core.KindUserText, Text: typed, FromRoom: true}, 60), "/init-orch", skillRGB(t))
	}
}

// A real handle is wider than a letter then letters and hyphens: names and teams
// take an underscore, and a team a leading digit. The router reads the whole
// word, so the call behind it is a call.
func TestAHandleTakesTheWholeNameTheRouterReads(t *testing.T) {
	trueColor(t)
	for _, handle := range []string{"@deploy_bot", "@2026q4", "@manager-ops"} {
		typed := handle + " /init-orch go"
		out := youSaid(typed, 60)
		requireWord(t, out, handle, fgRGB(t, Mention))
		requireWord(t, out, "/init-orch", skillRGB(t))
		requireWord(t, userBlock(core.Event{Kind: core.KindUserText, Text: typed, FromRoom: true}, 60), "/init-orch", skillRGB(t))
	}
}

// Nothing else in the turn takes a colour: a span that restored the foreground to
// the terminal's default, or to some other hue, would show on every word after
// the command, and requireWord only forbids the word's own colour elsewhere.
func TestEveryOtherCellOfATintedTurnIsPlainText(t *testing.T) {
	trueColor(t)
	text, skill, mention := fgRGB(t, Text), skillRGB(t), fgRGB(t, Mention)
	for _, typed := range []string{longSkillCall, "@john " + longSkillCall, "@john ship it", "/foo\nbar baz"} {
		for _, w := range []int{80, 40, 24} {
			rows := map[string][]string{
				"room": strings.Split(youSaid(typed, w), "\n"),
				"conversation": strings.Split(userBlock(core.Event{
					Kind: core.KindUserText, Text: typed, FromRoom: strings.HasPrefix(typed, "@"),
				}, w), "\n")[1:],
			}
			for name, body := range rows {
				for _, c := range paintOf(strings.Join(body, "\n")) {
					if c.fg != text && c.fg != skill && c.fg != mention {
						t.Fatalf("%s at width %d: %q in %q is drawn in %q, want the text's own colour:\n%q", name, w, string(c.r), typed, c.fg, strings.Join(body, "\n"))
					}
				}
			}
		}
	}
}

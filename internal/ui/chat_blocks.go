package ui

// The per-event half of the room: what one thing said in the group chat looks
// like. chat.go owns how those lines are laid out together.
//
// The room is a conversation, not a feed. Every block here is something
// addressed *to you* - an agent's own words, your own message, a marker that a
// turn produced none. What agents are *doing* lives in the right sidebar and
// never reaches this file.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

const (
	// roomInlineRows is where a reply stops being content and becomes a
	// pointer, counted in the rows it renders to at the room's *current* width.
	//
	// It is a surface boundary rather than a display nicety. The room is a hub
	// and a hub is bad at deep reading by design; making it good at both would
	// wreck it at the thing it is for. Past this, the room says who has
	// something long to say and how to go and read it.
	//
	// Rows, not characters, and this is the whole of the "variable sizing" the
	// room gained: a reply's cost is its rendered height, and a plan is headers,
	// bullets and code blocks with blank rows between - so a "short" 180-word
	// plan is ~20 rows of structure, not the ~4 its word count suggests.
	// Measuring the render at the pane's live width is what makes the same reply
	// show whole in a full-width room and collapse in a narrow column beside a
	// DM: the room shows more when it has the width for it and defers to the DM
	// when it does not, with no second knob.
	//
	// 24 is a judgment call - about two dozen rows - the way the old 400
	// characters was: it shows the 150-250-word plan/summary band at a
	// comfortable width and collapses it in a split, and the /context dumps that
	// run to hundreds of rows collapse at every width. The corpus tests
	// (chat_corpus_test.go) hold that *shape* across a range of caps rather than
	// deriving this exact number; TestTheDecisionFlipsAtExactlyTheRowCap is the
	// one place the value 24 itself is pinned.
	//
	// It is measured at the width and not the height, deliberately: coupling the
	// cap to the room's live height would make it move with the composer growing
	// and a card pinning - the chrome-height fragility this pane already fights -
	// so the trade is that on a wide-but-short terminal a reply at the cap can be
	// taller than the visible room. Bytes never enter it either: a double-width
	// CJK sign-off is a handful of rows whatever its byte length, so the old
	// byte-versus-character trap is gone rather than guarded.
	roomInlineRows = 24

	// roomCollapseLines is how much of a collapsed reply is kept: enough to
	// judge whether to open it, not enough to read it here.
	roomCollapseLines = 2

	// openDMHint is the affordance on a collapsed reply. ⌃D is spec §10's
	// "split right", which is exactly what opening a DM beside the room is.
	//
	// Nothing binds that key yet - App.key has no case for it and Task 10 owns
	// putting one there. It is the one thing in this file that describes a
	// build that does not exist, which is the failure CLAUDE.md names about the
	// hint line ("do not add a key to that legend until the key works"), and it
	// is tracked in docs/notes/deferred.md rather than left to be remembered.
	// The legend guard cannot see it: this is not a legendEntry.
	openDMHint = "⌃D open DM"

	// finishedMarker is what a turn that produced no words at all leaves
	// behind - 8 of 52 recorded turns.
	//
	// It is deliberately not a sentence. A finishing agent posts its *own*
	// closing prose, because a turn ending does not mean the work stopped: an
	// async subagent streams past its own result and 3 of 46 recorded results
	// are turns nobody asked for, so a Wake-authored "finished working on…"
	// would be false 3 times in 46. The agent's words are the agent's claim
	// and Wake only relays them; this marks the absence of a claim, which is
	// all Wake can honestly say. Same reason the permission-mode indicator was
	// removed.
	finishedMarker = "⤷ turn ended, nothing said"

	// roomSep separates a speaker from what they are working on.
	roomSep = " <> "

	// markerSep separates a speaker from a marker Wake wrote. It is not
	// roomSep, deliberately: that one means "and the branch it is on", and
	// spending it twice on one line draws the marker as a third half of the
	// agent's name - `sydney <> auth-fix <> ⤷ turn ended`.
	markerSep = " "

	// collapsedFormat is the pointer's last line: how much there is, and the
	// key that opens it. collapsedNoCount is the same line without the figure,
	// for a response that carried no token count - the figure is dropped rather
	// than shown as a zero.
	collapsedFormat  = "⤷ %s · " + openDMHint
	collapsedNoCount = "⤷ " + openDMHint

	// unnamedSpeaker heads a line from an agent this client has no name for
	// yet. Reachable: fan-out starts before a spawn is confirmed, so an event
	// can precede every report that would have named it.
	unnamedSpeaker = agentPrefix + "unnamed"
)

// renderRoomBlock is the one seam through which the Room renders an event. A
// retention test counts it because one hidden call here can put 20,000 Markdown
// events back through glamour outside the actual full-room pass.
var renderRoomBlock = roomBlock

// roomBlock renders one event for the room, or an empty block for one with no
// room representation. expanded draws an over-cap response in full rather than
// as a pointer - ⌃E and a click reach it through the Room's expand state; every
// other kind ignores it, since only an agent's reply collapses.
//
// It never returns a line wider than width. The room is one column of a
// three-region layout and lipgloss joins columns on their widest line, so an
// over-wide line here shoves both sidebars out of place.
func roomBlock(ev core.Event, a Agent, width int, expanded bool) block {
	w := max(width, minBlockWidth)
	switch ev.Kind {
	case core.KindAssistantText:
		return block{text: agentSaid(ev.Text, ev.OutputTokens, a, w, expanded)}
	case core.KindUserText:
		return block{text: youSaid(ev.Text, w)}
	case core.KindTurnEnd:
		return block{text: mutedLine(speaker(a)+markerSep+finishedMarker, w)}
	case core.KindPermissionRequest:
		// The room announces; it does not offer. The card is still the one
		// surface that answers - Cards.Undrawn keeps it to a single pane - and
		// this line carries no keys, so "one ask, one surface" is intact.
		//
		// It exists because the room had *neither*: the fold routed every ask
		// into Cards, and Cards.Undrawn excludes an agent whose conversation is
		// on screen, so opening the DM of a blocked agent left the group chat
		// with nothing at all. On the surface somebody supervising a fleet sits
		// on, an agent stopping is exactly what has to be visible.
		return block{text: warnLine(cardLead+speaker(a)+markerSep+askHeadline(ev), w)}
	default:
		// Cards are drawn by cards.go, which owns their pinning and their
		// keys; everything else the fold already dropped.
		// TestEveryKindTheRoomAdmitsIsDrawnHereOrExcusedWithAReason holds that
		// division, so a kind arriving here by accident is a failure rather
		// than a line nobody sees.
		return block{}
	}
}

// agentSaid is one agent's message: who, and what - whole if it renders short,
// a pointer if it is taller than the room shows inline.
//
// Rendered once, at this pane's width, and that one render is what both the
// height decision and the collapsed preview are taken from: a long message is
// exactly the case glamour's process-global mutex makes expensive, so it is
// never put through it twice.
func agentSaid(text string, count int, a Agent, width int, expanded bool) string {
	head := speakerStyle(a).MaxWidth(width).Render(speaker(a))
	body := strings.TrimSpace(text)
	rendered := render.Markdown(body, width)
	if expanded || renderedRows(rendered) <= roomInlineRows {
		return joinBlock(head, rendered)
	}
	return joinBlock(head, collapsed(rendered, tokenLabel(count), width, roomCollapseLines))
}

// roomCollapsible reports whether an event draws as a pointer at this width -
// i.e. whether there is anything for ⌃E or a click to expand. Only an agent's
// reply collapses; your own turn wraps whole and every marker is one line. It
// mirrors agentSaid's own boundary so the two cannot disagree about which lines
// are expandable, and it renders once, which is affordable on a user gesture
// and never on a frame.
func roomCollapsible(ev core.Event, width int) bool {
	if ev.Kind != core.KindAssistantText {
		return false
	}
	rendered := render.Markdown(strings.TrimSpace(ev.Text), max(width, minBlockWidth))
	return renderedRows(rendered) > roomInlineRows
}

// renderedRows is how many rows a rendered block occupies with its blank edges
// trimmed - the same trim joinBlock applies, so it counts the rows the reader
// actually sees. render.Markdown trims its own blank edges now (BUG-2), which
// is what makes the two agree: this used to count glamour's leading row and
// send a list one row nearer the collapse threshold than the reader saw.
func renderedRows(rendered string) int {
	r := strings.Trim(rendered, "\n")
	if r == "" {
		return 0
	}
	return strings.Count(r, "\n") + 1
}

// collapsed is a long piece of writing as a pointer: the opening lines, and
// how to reach the rest.
//
// It takes the *rendered* markdown rather than the source, so the caller that
// measured a block's height reuses that one render instead of paying glamour's
// process-global mutex a second time for the same long message. Showing the
// rendered opening is also what the reader wants - the first thing they would
// have seen, not the first thing the model typed - a reply opening with a
// fenced block shows the fence, which is itself the signal that this belongs in
// a DM.
//
// count is how much there is to read, in the reader's unit, and is the caller's
// to pass because only it still holds the source the render came from. keep is
// how many lines survive: a reply keeps two, because the room already told you
// who is talking and the question is only whether to go and read it; a plan
// card keeps more, because the reader is being asked to approve the thing rather
// than to decide whether to look at it. One function either way - a second copy
// of this in cards.go would be the parallel implementation this project forbids,
// and the pointer's wording is exactly what must not drift.
func collapsed(rendered, count string, width, keep int) string {
	lines := strings.Split(strings.Trim(rendered, "\n"), "\n")
	kept := lines[:min(len(lines), max(keep, 1))]
	line := collapsedNoCount
	if count != "" {
		line = fmt.Sprintf(collapsedFormat, count)
	}
	tail := ansi.Truncate(line, width, ellipsis)
	return strings.Join(append(kept, HintStyle.MaxWidth(width).Render(tail)), "\n")
}

// tokenLabel is how much a response ran to, in Claude's own output-token count -
// the unit the rest of Wake measures in. Empty for a response with no count, so
// the pointer drops the figure rather than showing a zero it cannot stand by.
func tokenLabel(n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return "1 token"
	}
	return fmt.Sprintf("%s tokens", thousands(n))
}

// words is how much there is to read, in the unit a person judges length in.
// The plan card still measures in words - a plan is not an assistant message
// and carries no token count of its own.
func words(text string) string {
	n := len(strings.Fields(text))
	if n == 1 {
		return "1 word"
	}
	return fmt.Sprintf("%s words", thousands(n))
}

// thousands groups a count so 1400 reads as 1,400 rather than as a number the
// eye has to parse.
func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// youSaid is the operator's own turn.
//
// Shaded rather than labelled. The DM heads it "› you" because a 1:1
// transcript has two speakers and the label is the whole attribution; the room
// has thirty, every other line already carries a name, and the one line
// without one is yours. OwnStyle is the same weight Claude Code gives a turn
// you typed.
//
// # It wraps, and it used to cut
//
// This flattened a draft to one row and truncated it with an ellipsis, on the
// argument that *"your own message is an instruction you have already read"*.
// That argument is wrong in the case that matters and the owner overruled it:
// the room is a narrow pane beside a conversation, so an ordinary sentence
// reached the cut, and what was lost was the half naming what you actually
// asked for. `@john can you make a…` is not a record of anything.
//
// The DM has always wrapped this - shadedOwn is OwnStyle.Width, and lipgloss
// reflows - so the two surfaces disagreed about the same message. They no
// longer do.
//
// Line breaks survive too, which they could not before ⌥↵ and ⌃J existed: a
// draft with a deliberate break in it is one somebody made on purpose, and
// collapsing it here would make the room the one place that break disappears.
func youSaid(text string, width int) string {
	body := strings.TrimSpace(text)
	if body == "" {
		return ""
	}
	// MaxWidth is deliberately not set beside Width. Width wraps to the pane;
	// MaxWidth would *cut* whatever the wrap could not place, which is the
	// behaviour this function is losing - and on an unbreakable token (a URL, a
	// path) the two disagree, with MaxWidth winning.
	return OwnStyle.Width(width).Render(colourMention(body))
}

// leadingMention is the @handle a room message opens with, which is the whole
// of what this colours: a mention anywhere else is a word, and finding those
// means guessing at a fleet that changes between keystrokes.
var leadingMention = regexp.MustCompile(`^@[A-Za-z][A-Za-z0-9-]*`)

// colourMention draws the address in Claude's blue and leaves the rest alone.
func colourMention(line string) string {
	m := leadingMention.FindString(line)
	if m == "" {
		return line
	}
	return MentionStyle.Render(m) + OwnStyle.Render(line[len(m):])
}

// collapseWhitespaceOneLine flattens a multi-line string to one row, for the
// surfaces that genuinely have one: a collapsed tool line, a notice.
func collapseWhitespaceOneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// speakerStyle is the colour an agent's name is drawn in when it heads a turn in
// the room: its own identity hue if /color gave it one, the shared Accent
// otherwise. Only the ordinary turn header takes it - the finished marker stays
// muted and an ask stays warn, because those are state rather than identity.
func speakerStyle(a Agent) lipgloss.Style {
	if style, ok := identityStyleFor(a); ok {
		return style
	}
	return AccentStyle
}

// speaker is who is talking: the name, and what they are on.
func speaker(a Agent) string {
	if a.Name == "" {
		return unnamedSpeaker
	}
	if a.Label == "" {
		return a.Name
	}
	return a.Name + roomSep + a.Label
}

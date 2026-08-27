package ui

// The per-event half of the DM: what one core.Event looks like as a block of
// terminal text. dm.go owns how those blocks are laid out together.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

const (
	// bodyIndent aligns a plain-text body with the two-column left margin
	// glamour gives rendered markdown, so a block's content always starts in
	// the same column and only labels and markers sit at the left edge.
	bodyIndent = 2

	// ellipsis marks text the DM had to cut.
	ellipsis = "…"

	// userLabel heads a turn Wake believes a human produced; echoedLabel heads
	// one the transcript replayed; promptLabel heads the one an agent handed
	// a subagent; roomTurnLabel heads one of your own routed from the room.
	// See userBlock for why the difference is only a label.
	userLabel     = "› you"
	echoedLabel   = "⟲ transcript"
	promptLabel   = "› prompt"
	roomTurnLabel = "› you · from the room"

	thinkingLabel   = "✻ thinking"
	permissionLabel = "⚠ permission request"
	rateLimitLabel  = "⚠ rate limit:"
	// No arrow: a KindSessionReset names no successor, so there is nothing
	// for one to point at. It used to point at new_conversation_id, which
	// was never the id the session continued under.
	sessionResetLabel = "⟲ session reset"
	compactedLabel    = "⟲ context compacted"
	deniedLabel       = "⊘ permission denied"

	// interruptedLabel replaces Claude's own abort wording, which arrives on a
	// frame indistinguishable from a user turn. It is muted rather than warned
	// about: a stopped turn is the operator getting what they asked for, not a
	// fault. One label for both recorded wordings - whether a tool was running
	// is already visible in the rejected tool result directly above it.
	interruptedLabel = "⊘ turn interrupted"

	// withdrawnLabel retires the ⚠ above it: the request is dead and the
	// answer this view was inviting would go nowhere. Muted for the same
	// reason interruptedLabel is - in every recording this is the operator's
	// own interrupt arriving, not a fault - and worded from the reader's side
	// of the airlock rather than the frame's.
	//
	// It says *permission request* where core.KindRequestWithdrawn says only
	// "a request", and the narrowing is this view's to make: a permission ask
	// is the one control request the DM ever draws, so within this transcript
	// that is what the reader is looking at. A withdrawal naming something
	// else would draw this line with no ⚠ above it, which is a spare notice
	// rather than a wrong one - the direction this file takes everywhere
	// (more output, never less), and never a deletion of anything drawn.
	withdrawnLabel = "⊘ permission request withdrawn"
)

// How a subagent's work is told apart from the agent's own.
//
// Two properties have to hold and neither is optional. A reader must be able
// to tell a subagent's line from the parent's - 26 of the 44 tool calls in
// the recorded corpus are a subagent's - and three concurrent streams must
// not read as one monologue, which subagent-parallel.jsonl produces by
// interleaving two subagents and their parent line by line.
//
// The gutter answers the first: every line a subagent produced carries it, so
// no line is ambiguous on its own. The header answers the second: it names
// the dispatch, so consecutive blocks from different subagents are two
// speakers rather than one voice changing subject. It repeats on every block
// for the same reason a chat shows a sender on every message - this view
// renders one event at a time and holds no memory of the last one, so there
// is nothing to compare against, and an unlabelled block in an interleaved
// stream belongs to nobody.
const (
	// subagentGutter prefixes every body line. Two columns, which is why the
	// body is rendered at a width reduced by exactly that.
	subagentGutter = "│ "

	// subagentLead opens the header line.
	subagentLead = "⤷ "

	// subagentSep separates the name from the tag.
	subagentSep = " · "

	// subagentUnnamed heads a subagent whose frame carried neither a task
	// description nor a type. All 80 recorded forwarded frames carry both,
	// but a permission ask carries neither, and §11 of the subagent findings
	// lists "whether subagent_type and task_description are always present"
	// as unverified for any type other than the one recorded.
	subagentUnnamed = "subagent"

	// How much of an id the header shows. Two constants, because subagentTag
	// reads two id spaces and they are not the same alphabet.
	//
	// The tag exists because the task description is not guaranteed unique:
	// two dispatches with the same description would be one voice again, and
	// the description is chosen by the model. The id is the only thing the
	// wire guarantees differs. Showing the whole of one is 27 columns of
	// noise in a view whose width is the scarce resource.
	//
	// A dispatch id's tail is base-59 - measured over the corpus's 44 toolu_
	// ids, where I, O and l never occur - so four characters is 12M and the
	// birthday bound over a heavy session's 200 dispatches is 0.0016. An
	// agent id is "a" plus 16 lowercase *hex* characters: four of those is
	// 65,536 and a bound of 0.30, thirty times the threshold, so it takes six
	// to buy the same safety. Equal discriminating power costs a different
	// number of columns because the alphabets are different sizes - and
	// dm_blocks_test.go derives both from the recording rather than from this
	// comment, which is the half that would otherwise rot.
	subagentDispatchTagLen = 4
	subagentAgentTagLen    = 6

	// What a dispatch receipt shows instead of its own content.
	//
	// The receipt for a completed foreground subagent repeats that
	// subagent's final message verbatim - byte-identical on the pair that
	// was compared - so drawing it prints the report twice. It is suppressed
	// rather than drawn, and the note says where the report went so the
	// suppression is legible rather than looking like a dropped frame.
	//
	// It used to say "its report is above", which was true while every
	// forwarded frame was drawn inline. Now the report is in the subagent's
	// own transcript and the note has to say where that is, or the one line
	// left of a dispatch reads as work that vanished.
	//
	// This holds only because Wake's own argv always passes
	// --forward-subagent-text, which is what puts the report in that
	// transcript in the first place. core.SubagentFinished carries that
	// argument and names who owns re-deciding it if the flag ever goes.
	subagentFinishedNote = "finished · ↵ to read it"
	subagentLaunchedNote = "launched in the background"
)

var (
	// warnStyle marks a blocked or degraded state. theme.go exports the Warn
	// colour but no style over it; this belongs there once that file is free
	// to change.
	warnStyle = lipgloss.NewStyle().Foreground(Warn)

	// noticeLabel is what a reader is shown for an event core.Notice picked
	// out. It is keyed on Wake's own vocabulary, not Claude's.
	//
	// It used to be keyed on the raw wire subtype, which is the half of the
	// airlock ruling that needed narrowing. As an allowlist of wire strings
	// this map is a category rather than an enumeration: it grows by one
	// entry per subtype with nobody reviewing the boundary, and the subagent
	// corpus alone lands 56 more task_* frames whose whole value is a status
	// line. Under the old shape the natural way to show them was seven more
	// keys here, and the sidebar would have been built out of Claude's
	// vocabulary inside internal/ui.
	//
	// Keyed on core.Notice the cost lands where it belongs: showing a new one
	// means adding a constant in protocol.go and a row here, so the decision
	// is made once, behind the airlock, in a file whose review is the point.
	// The passthrough the ruling protects is untouched - core.Event.Text
	// still carries the raw subtype, so an unmodelled one arrives as a system
	// event rather than degrading.
	noticeLabel = map[core.Notice]string{
		core.NoticeContextCompacted: compactedLabel,
		core.NoticeToolDenied:       deniedLabel,
		core.NoticeTurnInterrupted:  interruptedLabel,
	}
)

// renderEvent turns one event into a transcript block, or an empty one for an
// event with no visual form.
//
// The leading and trailing newlines go here. Glamour opens every document with
// a blank row, and left alone that lands on top of the blank row the transcript
// already puts between blocks - one wasted line per message, which over an
// hour-long conversation is a third of the pane. Trimming newlines only leaves
// glamour's two-column left margin intact.
//
// A tool result is joined to whatever precedes it in arrival order rather than
// matched to its call by id: Claude can emit several calls before their
// results, and re-ordering the transcript to pair them would misreport what
// the agent actually did when.
func (d DM) renderEvent(ev core.Event) block {
	b := block{
		text:   strings.Trim(d.eventBlock(ev), "\n"),
		joined: ev.Kind == core.KindToolResult,
	}
	// A subagent's call is deliberately unmarked: its block is drawn inside an
	// attribution gutter, so the ⏺ is not at the start of its line and neither
	// a recolour nor a click could address it without redrawing the whole
	// block. It folds and expands with the conversation like everything else.
	if ev.Tool == nil || ev.Subagent != nil {
		return b
	}
	switch ev.Kind {
	case core.KindToolUse:
		b.tool, b.head = ev.Tool.ID, true
	case core.KindToolResult:
		b.tool = ev.Tool.ID
	}
	return b
}

// eventBlock is what one event looks like, or "" for an event with no visual
// form. An event a subagent produced is drawn inside its attribution; every
// other one is drawn as itself.
func (d DM) eventBlock(ev core.Event) string {
	w := d.blockWidth()
	if ev.Subagent == nil {
		return d.kindBlock(ev, w)
	}
	if note := receiptNote(ev.Subagent.Result); note != "" {
		// A dispatch receipt is the agent reporting *about* a subagent, not
		// the subagent speaking, so it is one status line rather than a
		// headed block. Given a header it would introduce itself under a
		// different name from the frames it summarises - the receipt carries
		// the subagent's type where the forwarded frames carry its task - and
		// read as one more subagent rather than as one of them finishing.
		// The tag is what ties it to the stream above.
		return mutedLine(subagentLead+subagentTag(ev.Subagent)+subagentSep+note, w)
	}
	body := d.kindBlock(ev, w-lipgloss.Width(subagentGutter))
	if body == "" {
		// No content, so no header either: a lone "⤷ …" naming a subagent
		// that said nothing is a blank turn in the transcript.
		return ""
	}
	return joinBlock(mutedLine(subagentHeader(ev.Subagent), w), gutterLines(body))
}

// receiptNote is what a dispatch receipt shows instead of its own content,
// and "" for a receipt this decoder did not recognise.
//
// core.SubagentUnknown returns "" and therefore falls through to the ordinary
// headed block, content included. An unmodelled status must degrade toward
// more output, never less: both known values license hiding something, and a
// status nobody has recorded has not earned that.
func receiptNote(r core.SubagentResult) string {
	switch r {
	case core.SubagentFinished:
		return subagentFinishedNote
	case core.SubagentLaunched:
		return subagentLaunchedNote
	default:
		return ""
	}
}

// subagentHeader names who is speaking: what the subagent was asked to do,
// and a tag that stays distinct when two of them were asked the same thing.
func subagentHeader(s *core.Subagent) string {
	name := s.Task
	if name == "" {
		name = s.Type
	}
	if name == "" {
		name = subagentUnnamed
	}
	if tag := subagentTag(s); tag != "" {
		return subagentLead + name + subagentSep + tag
	}
	return subagentLead + name
}

// subagentTag is the tail of whichever id the frame carried.
//
// Dispatch first: it is on all 80 forwarded frames and is what separates two
// concurrent subagents. A permission ask carries no dispatch at all - the
// envelope names none - so it falls back to the subagent's own id, and an
// ask's tag therefore does not match that subagent's speech.
//
// That is a real limitation rather than a bug here, and core.Subagent's doc
// comment is where it is argued: the frame that joins the two id spaces is
// the dispatch receipt, which arrives *after* the ask. Joining forward - so
// that an ask could carry its dispatch's tag - needs task_started, which
// nothing in the tree decodes. What the ask does get, and did not before, is
// a mark saying a subagent asked.
func subagentTag(s *core.Subagent) string {
	if s.Dispatch != "" {
		return idTail(s.Dispatch, subagentDispatchTagLen)
	}
	return idTail(s.Agent, subagentAgentTagLen)
}

// idTail is the last n characters of an id, or the whole of a short one.
func idTail(id string, n int) string {
	r := []rune(id)
	if len(r) <= n {
		return id
	}
	return string(r[len(r)-n:])
}

// gutterLines marks every line of a block as a subagent's, so no line in an
// interleaved transcript is ambiguous on its own.
func gutterLines(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = subagentGutter + l
	}
	return strings.Join(out, "\n")
}

// kindBlock is the per-kind switch: what one event looks like at a given
// width, or "" for an event with no visual form.
func (d DM) kindBlock(ev core.Event, w int) string {
	switch ev.Kind {
	case core.KindAssistantText:
		return render.Markdown(ev.Text, w)
	case core.KindUserText:
		// A user frame the airlock recognised as something other than the user
		// speaking - Claude's abort marker, which carries nothing else to tell
		// it apart. The notice is drawn instead of the text, because the text
		// is Claude's English about Wake's own action.
		//
		// Falling back rather than returning what noticeBlock gave is the
		// whole of the care here. It renders "" for a notice with no label,
		// and on this one kind that would not be a missing decoration - it
		// would take the operator's own words out of their transcript, with
		// nothing anywhere saying so. The next content-derived notice added
		// without a row in noticeLabel is the way in.
		if block := noticeBlock(ev, w); block != "" {
			return block
		}
		return userBlock(ev, w)
	case core.KindThinking:
		return thinkingBlock(ev.Text, w)
	case core.KindToolUse:
		if ev.Tool == nil {
			return ""
		}
		return toolUseBlock(ev.Tool, d.bulletFor(ev.Tool.ID), w)
	case core.KindToolResult:
		if ev.Tool == nil {
			return toolResultBlock(ev, nil, d.expanded, w)
		}
		return toolResultBlock(ev, d.calls[ev.Tool.ID], d.expandedTool(ev.Tool.ID), w)
	case core.KindPermissionRequest:
		return permissionBlock(ev, w)
	case core.KindRequestWithdrawn:
		// The ⚠ above is dead. Drawn rather than dropped because this
		// transcript is append-only - there is no prompt to take down, only
		// a last word to correct - and because the alternative is what Wake
		// did until now: leave an ask on screen that nobody will ever
		// answer.
		return mutedLine(withdrawnLabel, w)
	case core.KindSessionReset:
		return mutedLine(sessionResetLabel, w)
	case core.KindRateLimit:
		return noticeBlock(ev, w)
	case core.KindSystem:
		// A dispatch ending is the one system frame that leaves a line: it is
		// a thing that happened, in an order, which is what a transcript is
		// for. Every other lifecycle frame is a fact about *now* and belongs
		// to the rows beside it. See taskline.go.
		if line := taskLine(ev.Task, w); line != "" {
			return line
		}
		return noticeBlock(ev, w)
	case core.KindTurnEnd:
		// A turn boundary carries the turn's final text, which the assistant
		// frame already delivered. The composer conveys that it is over.
		return ""
	default:
		// KindUnknown, and the two kinds nothing here draws yet:
		// KindControlReceipt and KindMessageState are both modelled and both
		// land here, so this is not only "a frame Wake does not model".
		// Naming any of them in the transcript would put wire vocabulary in
		// front of a reader.
		return ""
	}
}

// userBlock renders one turn from the user's side of the conversation.
//
// Both halves belong in a 1:1 view, so the text is always shown; only the
// label changes. Three cases, and the third is why this is a switch rather
// than the boolean it used to be: a forwarded user frame is the prompt an
// agent handed its subagent, not something a human typed. Six of the corpus's
// sixteen KindUserText events are exactly that, and every one of them used to
// be headed "› you".
//
// Echoed marks a frame the transcript replayed rather than one a human just
// typed - but which of those Claude's --replay-user-messages produces for
// Wake's own outgoing message has never been observed
// (docs/superpowers/notes/2026-08-08-stream-json-findings.md §12). Keeping the
// reliance cosmetic means a wrong guess mislabels a turn instead of hiding it.
//
// The DM does not de-duplicate. It draws exactly the events it is handed, so
// whoever feeds it must pick one source for the user's turn - the replayed
// frame or a local echo of what was sent, never both.
func userBlock(ev core.Event, width int) string {
	if strings.TrimSpace(ev.Text) == "" {
		return ""
	}
	switch {
	case ev.Subagent != nil:
		return joinBlock(mutedLine(promptLabel, width), render.Markdown(ev.Text, width))
	case ev.Echoed:
		return joinBlock(mutedLine(echoedLabel, width), render.Markdown(ev.Text, width))
	case ev.FromRoom:
		// Still your own words, so it keeps the accent and the shading rather
		// than being muted like a replay. Only the head moves - the mention it
		// carries is the *message*, and the label is what says the mention did
		// the routing instead of being text you typed at this composer.
		return joinBlock(accentLine(roomTurnLabel, width), shadedOwn(ev.Text, width))
	default:
		return joinBlock(accentLine(userLabel, width), shadedOwn(ev.Text, width))
	}
}

// shadedOwn draws the operator's own message on a shaded ground, the way
// Claude Code shades a turn you typed.
//
// Deliberately not markdown, and this is the one place in the DM that is not.
// Two reasons, and the second is why it is safe:
//
//   - It is what you typed. Rendering your own asterisks as bold shows you
//     something you did not write, and you are the one person in the
//     conversation who already knows what you meant.
//   - A background has to survive to the end of every line to read as a
//     rectangle, and glamour emits its own resets mid-string. Shading styled
//     text would break the ground wherever markdown ended a span. Plain text
//     has no resets to collide with, so the block is solid.
//
// lipgloss's Width does the whole job: it wraps, and it pads every line to
// exactly that many cells so the ground is a rectangle rather than a ragged
// edge. Measured against v1.1.0 - 90 CJK characters at width 40 come back as
// five lines of exactly 40 cells, so the padding is display-width aware and a
// wide-character message shades correctly.
//
// Below one column there is nothing to shade and Width(0) means "unbounded",
// which would draw the message on one endless line.
func shadedOwn(text string, width int) string {
	if width < 1 {
		return TextStyle.Render(text)
	}
	return OwnStyle.Width(width).Render(text)
}

// thinkingBlock shows what the agent was reasoning about, not just that it
// was. Thinking is one of the four things the spec promises comes back in a
// DM; a bare label would be the group chat's treatment.
//
// It is drawn as muted plain text rather than markdown so it recedes - a style
// wrapped around glamour's output would end at glamour's first SGR reset - and
// indented to bodyIndent so it lines up with the markdown around it.
func thinkingBlock(text string, width int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	body := HintStyle.Width(width).PaddingLeft(bodyIndent).Render(text)
	return joinBlock(mutedLine(thinkingLabel, width), body)
}

// permissionBlock shows that the agent is blocked asking to use a tool. Phase 1
// cannot answer it here, but a DM that simply goes quiet is worse than one that
// says why.
//
// Whether a subagent raised it is not decided here: eventBlock has already
// wrapped this block in that attribution when core.Event.Subagent is set, so
// an ask from a subagent no longer reads as the agent's own.
func permissionBlock(ev core.Event, width int) string {
	label := warnLine(permissionLabel, width)
	if ev.Tool == nil {
		return label
	}
	// An interactive ask is presented by its card - the questions, the
	// options, each one's consequence - so its tool call underneath is a third
	// account of one thing and the one that says least. The tool_use frame
	// above it already named the call; this used to name it again.
	//
	// Keyed on the ask kind rather than on the tool's name, for Card.Shape's
	// reason: any tool whose permission check asks for interaction can be one,
	// and both recorded names are words this package never spells.
	if ev.Ask == core.AskChoice || ev.Ask == core.AskApproval {
		return label
	}
	// Drawn in the conversation's own tool style with the unresolved bullet,
	// which is what an ask is: a call that has not run.
	//
	// The command is drawn **whichever tool this is**, and that is not a
	// nicety. An ask goes through the same core.toolCall as an invocation, so
	// a Bash ask carries a description in Title - and heading the block with
	// it alone would put "Run the test suite" in front of an operator being
	// asked to approve `rm -rf`. What is being approved is the command.
	return joinBlock(label,
		toolHeadline(ev.Tool, ToolRunningStyle, width),
		render.ToolCommand(ev.Tool.Command, toolStyle(ToolRunningStyle), width),
	)
}

// noticeBlock draws the events core.Notice picked out, and nothing else.
//
// A rate limit is the one that carries data alongside its label: the status
// string is what the reader needs and it is a value, not a wire word, so it
// is passed through. The benign status earns no notice at all and so never
// reaches here - drawing it is chrome.
func noticeBlock(ev core.Event, width int) string {
	if ev.Notice == core.NoticeRateLimited {
		return warnLine(rateLimitLabel+" "+ev.Text, width)
	}
	label, ok := noticeLabel[ev.Notice]
	if !ok {
		return ""
	}
	return mutedLine(label, width)
}

// joinBlock stacks the parts of one block, skipping the ones that rendered to
// nothing so a missing half never costs a blank row.
//
// Each part is stripped of leading and trailing blank rows for the same reason
// renderEvent strips them from the whole: a label has to sit directly on the
// text it introduces, and glamour opens every document it renders with a blank
// row that would otherwise land between the two.
func joinBlock(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.Trim(p, "\n"); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n")
}

func mutedLine(s string, width int) string  { return HintStyle.MaxWidth(width).Render(s) }
func accentLine(s string, width int) string { return AccentStyle.MaxWidth(width).Render(s) }
func warnLine(s string, width int) string   { return warnStyle.MaxWidth(width).Render(s) }

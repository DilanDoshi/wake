package ui

// A run of tool calls folded to one line, the way Claude Code shows a message's
// tool activity as `28 tool uses · 24 bash · …` rather than the calls
// themselves. toolblocks.go owns one call whole; this owns the fold over a run
// of them.
//
// # What a run is
//
// A maximal sequence of consecutive tool blocks - a KindToolUse and every
// KindToolResult and further use that follows it with nothing else between.
// Anything a reader would see as its own thing breaks the run: the agent's
// prose, its thinking, a user turn, a notice, and the last-read boundary. So a
// message's tools fold together and the next message's fold on their own,
// which is the per-message rollup the screenshots show.
//
// A run **starts at a use**, never at a result: a result whose use was folded
// into an earlier run has nowhere to belong and draws as itself. That is also
// what keeps the fold from ever having to un-draw a block it already drew - a
// use only ever grows its run, and a stray result is decided once.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
)

// mcpNamePrefix and mcpNameSep are how Claude spells an MCP tool -
// `mcp__linear-server__get_issue` - the one name shape a rollup groups by its
// server rather than by the tool. It is Claude's convention on a name core
// already decoded, not the wire envelope, so recognising it here breaks no
// airlock.
const (
	mcpNamePrefix = "mcp__"
	mcpNameSep    = "__"
)

// isToolBlock reports whether an event is one of the conversation's own tool
// blocks - a use or a result, never a subagent's, which draws in its own gutter
// and folds with the conversation rather than into a run.
func (d DM) isToolBlock(ev core.Event) bool {
	return ev.Subagent == nil && ev.Tool != nil && !d.exempt(ev) &&
		(ev.Kind == core.KindToolUse || ev.Kind == core.KindToolResult)
}

// isToolUse is the half of that a run may start on. A use's exemption is on the
// call itself, so this needs no conversation state.
func isToolUse(ev core.Event) bool {
	return ev.Subagent == nil && ev.Tool != nil && ev.Kind == core.KindToolUse && !foldExempt(ev.Tool)
}

// exempt reports whether an event is a fold-exempt tool call - a checklist, whose
// list is live status rather than activity. A use carries what marks it; a result
// carries none of its own (protocol.go builds it from just the id and the error
// flag), so it inherits its use's exemption through the stored call. Without that
// a checklist result could fold into an unrelated live run while its use was
// drawn apart - the two halves of one call on two surfaces.
func (d DM) exempt(ev core.Event) bool {
	if foldExempt(ev.Tool) {
		return true
	}
	if ev.Kind == core.KindToolResult {
		if use, ok := d.calls[ev.Tool.ID]; ok {
			return foldExempt(use)
		}
	}
	return false
}

// foldExempt marks a tool call the rollup keeps out of a run rather than folding
// into a count, because its list is live status, not the activity the fold hides.
// Two shapes carry a list: a legacy TodoWrite carries its whole list as
// `Todos`, and it still draws that list whole as its own block; a
// TaskCreate/TaskUpdate carries the op as `Checklist`, and it is the board pinned
// above the composer, drawing nothing in the transcript (eventBlock). Both break
// the run around them either way. A TaskCreate/TaskUpdate is exempt on the op
// alone - a restored one arrives with no snapshot - and its result carries no op,
// so it inherits this through the stored call (see exempt).
func foldExempt(tool *core.ToolCall) bool {
	return len(tool.Todos) > 0 || tool.Checklist != nil
}

// expandedRun reports whether a run is drawn as its per-call blocks: either the
// whole conversation is expanded (⌃E), or this run was opened by a click.
func (d DM) expandedRun(key string) bool { return d.expanded || d.runOpen[key] }

// storable is the events restored history may keep: the ones the live Append
// path would have stored. Append never stores an event that draws nothing and is
// not a tool block, so d.events holds only tool blocks and blocks with something
// to show - the invariant renderAll's structural run boundary depends on, so a
// blank block or a turn end read off disk cannot split a run the live path kept
// whole.
func (d DM) storable(events []core.Event) []core.Event {
	kept := make([]core.Event, 0, len(events))
	for _, ev := range events {
		if d.drawsSomething(ev) {
			kept = append(kept, ev)
		}
	}
	return kept
}

// drawsSomething reports whether an event contributes a block. It renders only
// when it must: a tool block and prose that plainly carries text are kept
// without a render, so a long history is not put through glamour twice on open;
// only an event with no text of its own - a turn end, a control frame, a
// card-only ask - is checked against the renderer, and those are few.
func (d DM) drawsSomething(ev core.Event) bool {
	if d.isToolBlock(ev) {
		return true
	}
	// A checklist op draws nothing but is state, not activity: storable keeps it
	// so a restore off disk can re-derive the board from d.events. See
	// DM.isChecklistOp and DM.foldChecklist.
	if d.isChecklistOp(ev) {
		return true
	}
	switch ev.Kind {
	case core.KindAssistantText, core.KindThinking, core.KindUserText:
		if strings.TrimSpace(ev.Text) != "" {
			return true
		}
	}
	return d.renderEvent(ev).text != ""
}

// toolCategory is the word a tool is counted under. An MCP tool counts under
// its server, so four calls to one server read as one figure; every other tool
// counts under its own lowercased name.
func toolCategory(name string) string {
	if rest, ok := strings.CutPrefix(name, mcpNamePrefix); ok {
		if server, _, ok := strings.Cut(rest, mcpNameSep); ok && server != "" {
			return server
		}
	}
	return strings.ToLower(name)
}

// rollupTally is a run's use count per category, the state a folded run's line
// is formatted from. It is maintained in O(1) as a run grows (countUse) so that
// appending the k-th tool event costs a constant rather than a walk back over
// the whole run - the difference between O(run) and O(run²) for a long batch on
// the one Update goroutine every session's events pass through. renderAll and
// Before rebuild it from a run's events (tallyOf); the live path never does.
type rollupTally map[string]int

// countUse returns t with one more use of name's category, copied on write so a
// DM handed an older copy keeps its own counts. Categories are few, so the copy
// is cheap; a fresh run passes a nil t. See drawFold.
func countUse(t rollupTally, name string) rollupTally {
	next := make(rollupTally, len(t)+1)
	for k, v := range t {
		next[k] = v
	}
	next[toolCategory(name)]++
	return next
}

// tallyOf counts a run's uses in one pass, for the paths that rebuild rather
// than maintain: a re-wrap, and Before recomputing the tail.
func tallyOf(events []core.Event) rollupTally {
	t := rollupTally{}
	for _, ev := range events {
		if isToolUse(ev) {
			t[toolCategory(ev.Tool.Name)]++
		}
	}
	return t
}

// rollupSummary is the line a folded run stands in for: the number of uses, then
// a breakdown by category, largest first. "" for a run with no uses to count -
// a results-only run, which does not fold.
func rollupSummary(t rollupTally) string {
	uses := 0
	cats := make([]string, 0, len(t))
	for cat, n := range t {
		uses += n
		cats = append(cats, cat)
	}
	if uses == 0 {
		return ""
	}
	// Largest category first, its name to break a tie, so the line is stable
	// across renders rather than reordering under the map's iteration.
	sort.Slice(cats, func(i, j int) bool {
		if t[cats[i]] != t[cats[j]] {
			return t[cats[i]] > t[cats[j]]
		}
		return cats[i] < cats[j]
	})
	parts := []string{plural(uses, "tool use")}
	for _, cat := range cats {
		parts = append(parts, strconv.Itoa(t[cat])+" "+cat)
	}
	return strings.Join(parts, " · ")
}

// plural is n and a noun with an "s" when n is not one. The categories are not
// pluralised - "1 read", "24 bash" - because they are labels rather than nouns;
// only the lead count is.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// rollupLine renders the dimmed summary a folded run stands in for, at the pane's
// width. drawFold rewrites this one line in place as the run grows.
func (d DM) rollupLine(t rollupTally) string {
	return render.ToolRollup(rollupSummary(t), expandKey, toolStyle(ToolRunningStyle), d.blockWidth())
}

// rollupBlock is the one line a folded run draws, marked with the run's key so a
// click can open it. renderAll builds the tally from the run's events; the live
// path keeps one incrementally instead - see drawFold.
func (d DM) rollupBlock(key string, events []core.Event) block {
	return block{text: d.rollupLine(tallyOf(events)), run: key}
}

// trailingRun is the events of the run at the end of the conversation, or nil
// when the last thing said was not a foldable run. It walks back over the
// consecutive tool blocks at the tail, stopping at a non-tool block or a
// last-read boundary, and returns from the first use in that tail.
//
// Bounded by the run's length, never the conversation's: it stops at the first
// thing that is not part of the run. See dm.go's Append rule.
func (d DM) trailingRun() []core.Event {
	n := d.events.len()
	if n == 0 || !d.isToolBlock(d.events.at(n-1)) {
		return nil
	}
	start := n - 1
	for start > 0 {
		if _, mk := d.markerBefore(start); mk {
			break
		}
		if !d.isToolBlock(d.events.at(start - 1)) {
			break
		}
		start--
	}
	for i := start; i < n; i++ {
		if isToolUse(d.events.at(i)) {
			return d.events.slice(i, n)
		}
	}
	return nil
}

// appendPerCall draws a run whole - every call as its own block - which is what
// an expanded run and a subagent's forwarded tools both show.
func (d DM) appendPerCall(blocks []block, run []core.Event) []block {
	for _, ev := range run {
		if b := d.renderEvent(ev); b.text != "" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// openRun toggles the one folded run drawn on a transcript line, and reports
// whether a line held one at all. A click on a rollup opens that run; every
// other click falls through to openTool, which opens one folded result.
func (d DM) openRun(line int) (DM, bool) {
	key := d.tr.runAt(line)
	if key == "" {
		return d, false
	}
	runOpen := make(map[string]bool, len(d.runOpen)+1)
	for k, v := range d.runOpen {
		runOpen[k] = v
	}
	runOpen[key] = !runOpen[key]
	d.runOpen = runOpen

	// Opening a run renumbers only the lines below it, so a reader scrolled above
	// the click keeps their place - the rule openTool states at length. runKey is
	// left untouched: this changes no events, so the trailing run is unchanged.
	following := d.tr.atBottom()
	d.tr = d.tr.replace(renderTranscript(d))
	if following {
		d.tr = d.tr.toBottom()
	}
	return d, true
}

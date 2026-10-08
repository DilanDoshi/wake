package ui

// A streamed answer's finished blocks are drawn formatted while the next one
// streams. Each is rendered once, by the same renderer the landed block goes
// through, and only for a pane that has seen the whole of the block it is
// previewing - a pane that joined it halfway cannot tell a code block's lines
// from prose, so it keeps showing that block as it always did.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const fmtW, fmtH = 60, 24

// chunkCounter swaps the markdown seam for one that counts, and puts it back.
func chunkCounter(t *testing.T) *int {
	t.Helper()
	n := new(int)
	orig := renderMarkdown
	renderMarkdown = func(src string, w int) string { *n++; return orig(src, w) }
	t.Cleanup(func() { renderMarkdown = orig })
	return n
}

// pieces cuts text into tokens of n bytes, the way a stream hands it over.
func pieces(text string, n int) []string {
	var out []string
	for i := 0; i < len(text); i += n {
		out = append(out, text[i:min(i+n, len(text))])
	}
	return out
}

func messageStarted(d DM) DM {
	return d.Append(core.Event{Kind: core.KindMessageStart, SessionID: "s1"})
}

// streamed is d after text arrives a few bytes at a time.
func streamed(d DM, text string) DM { return tokens(d, pieces(text, 5)...) }

// formatDM is a conversation that has seen a message begin, in an empty pane
// tall enough that the whole answer fits the preview.
func formatDM(h int) DM {
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	return messageStarted(d.SetSize(fmtW, h))
}

func startFrame(id string) rpc.Frame { return kindFrame(id, core.KindMessageStart, "") }

// fmtApp is the room with alex's conversation open beside it.
func fmtApp(t *testing.T) App {
	t.Helper()
	return newRoomApp(t).withSize(200, 40).withAgents("alex", "sydney").openDMWith("s1", "alex")
}

func (a App) streams(id, text string) App {
	for _, tok := range pieces(text, 5) {
		a = a.applyFrame(tokenFrame(id, tok))
	}
	return a
}

func leadOf(row string) int { return len(row) - len(strings.TrimLeft(row, " ")) }

// The point of the change: a long answer no longer streams as raw markdown for
// rows at a time and snaps to formatted when it lands.
func TestAFinishedParagraphIsFormattedWhileTheNextOneStreams(t *testing.T) {
	d := streamed(formatDM(fmtH), "The **first** paragraph is `done`.\n\nThe second is still **being writ")
	out := visible(d, fmtW, fmtH)

	for _, raw := range []string{"**first**", "`done`"} {
		if strings.Contains(out, raw) {
			t.Errorf("the finished paragraph is on screen with its markdown (%s):\n%s", raw, out)
		}
	}
	rows := strings.Split(out, "\n")
	done, open := lineIndex(out, "The first paragraph is done."), lineIndex(out, "The second is still **being writ")
	if done < 0 || open < 0 {
		t.Fatalf("the finished paragraph at row %d and the open one at row %d, want both on screen:\n%s", done, open, out)
	}
	if open <= done {
		t.Errorf("the open paragraph (row %d) is not under the finished one (row %d)", open, done)
	}
	if leadOf(rows[done]) != leadOf(rows[open]) {
		t.Errorf("the finished paragraph is drawn at column %d and the open one at %d: the left edge jumps when a block finishes\n%s", leadOf(rows[done]), leadOf(rows[open]), out)
	}
	if d.events.len() != 0 {
		t.Errorf("%d events stored: a preview is never a record", d.events.len())
	}
}

// Glamour is per finished block and never per token: the counter is the seam
// every markdown block a pane draws goes through.
func TestEachFinishedChunkIsRenderedOnce(t *testing.T) {
	renders := chunkCounter(t)
	d := formatDM(fmtH)
	d = streamed(d, "First block.\n\nSecond block.\n\nThird block is still being")
	if *renders != 2 {
		t.Fatalf("%d renders for two finished blocks, want 2", *renders)
	}

	// Per-token adds: nothing is rendered again.
	d = streamed(d, " written, token after token after token.")
	if *renders != 2 {
		t.Errorf("tokens into the open block rendered %d more times", *renders-2)
	}

	// A frame drawn with a menu up re-lays a copy of the pane every time.
	d.menu = "  a menu row"
	for range 5 {
		d.View(fmtW, fmtH)
	}
	if *renders != 2 {
		t.Errorf("drawing the pane with a menu up rendered finished blocks %d more times", *renders-2)
	}

	// A cap change re-slices the rows it has.
	d.menu = ""
	d = d.SetSize(fmtW, fmtH-8)
	d = d.SetSize(fmtW, fmtH)
	if *renders != 2 {
		t.Errorf("a cap change rendered finished blocks %d more times", *renders-2)
	}
}

// A preview is bounded to what a pane can draw, finished blocks included: the
// rows kept are the newest ones, and no older chunk is held that the view could
// never reach.
func TestThePreviewKeepsOnlyAPaneOfFinishedRows(t *testing.T) {
	d := formatDM(fmtH)
	var text strings.Builder
	for i := range 60 {
		fmt.Fprintf(&text, "Paragraph %02d says a few ordinary words about the work.\n\n", i)
	}
	d = streamed(d, text.String()+"Paragraph 60 is still being")

	rows := d.partial.rows()
	if limit := d.previewCap(true); rows == 0 || rows > limit {
		t.Fatalf("the preview draws %d rows, want 1 to %d", rows, limit)
	}
	out := visible(d, fmtW, fmtH)
	if !strings.Contains(out, "Paragraph 59 says") || !strings.Contains(out, "Paragraph 60 is still being") {
		t.Errorf("the newest rows are not the ones drawn:\n%s", out)
	}
	if strings.Contains(out, "Paragraph 10 says") {
		t.Errorf("an old paragraph is still on screen:\n%s", out)
	}

	// Held: enough rows to reach cap+slack, and not one chunk more than that.
	if len(d.partial.fin.chunks()) == 0 {
		t.Fatal("no finished block is held")
	}
	held := len(d.partial.fin.rows())
	want := d.partial.cap + previewSlack
	if held < want {
		t.Errorf("%d finished rows held, want at least cap+slack = %d", held, want)
	}
	if first := d.partial.fin.chunks()[0]; held-len(first.rows)-1 >= want {
		t.Errorf("%d finished rows held, and the oldest chunk (%d rows) is not needed to reach %d", held, len(first.rows), want)
	}
	if n := len(d.partial.fin.chunks()); n > want {
		t.Errorf("%d finished chunks held for a pane of %d rows", n, d.partial.cap)
	}
}

// A width change draws what a resize always does: the finished blocks again at
// the new width - once each - and the open block wrapped to it.
func TestAWidthChangeReWrapsFinishedChunksAndWrapsTheTail(t *testing.T) {
	renders := chunkCounter(t)
	long := "The **first** paragraph runs on for a good while so that it wraps differently at forty columns than at sixty."
	d := streamed(formatDM(fmtH), long+"\n\nAnd the second open paragraph is not finished either, it keeps on going and going")
	if *renders != 1 {
		t.Fatalf("%d renders for one finished block, want 1", *renders)
	}
	wide := d.partial.rows()

	*renders = 0
	d = d.SetSize(40, fmtH)
	if *renders != len(d.partial.fin.chunks()) || *renders != 1 {
		t.Errorf("a width change rendered %d times for %d held chunks, want 1 for the one", *renders, len(d.partial.fin.chunks()))
	}
	if narrow := d.partial.rows(); narrow <= wide {
		t.Errorf("%d rows at 60 columns and %d at 40: the preview was not re-wrapped", wide, narrow)
	}
	out := visible(d, 40, fmtH)
	if strings.Contains(out, "**first**") {
		t.Errorf("the finished paragraph came back as raw markdown:\n%s", out)
	}
	for _, row := range strings.Split(d.partial.view, "\n") {
		if w := ansi.StringWidth(row); w > 40 {
			t.Errorf("a preview row measures %d cells in a 40-column pane: %q", w, row)
		}
	}
	if !strings.Contains(out, "second open paragraph") {
		t.Errorf("the open block is gone after the resize:\n%s", out)
	}
}

// When the block lands, what it draws should be about what the preview was
// drawing: the rows may differ by one (a heading's or a list's spacing), not by a
// screenful.
func TestALandedBlockMatchesThePreviewsFinishedRows(t *testing.T) {
	answers := []string{
		"I found the bug: the retry header was dropped when the request body was replayed.\n\n" +
			"The fix is two parts:\n\n- keep the header on the replay\n- add a regression test for it\n\n" +
			"Tests pass and the diff is small.\n\nWant me to open a PR for it?",
		"## Summary\n\nThe parser now reads **three** formats.\n\n```go\nfunc Parse(s string) error {\n\treturn nil\n}\n```\n\n" +
			"1. run the tests\n2. check the output\n\nThat covers everything I changed in this pass.",
		"Short answer: yes.\n\nThe longer one is that the daemon keeps one `claude` per agent, so a park is a kill " +
			"and a wake is a `--resume`, and the transcript is what carries the conversation across.\n\nDone.",
	}
	for i, text := range answers {
		d := streamed(formatDM(60), text)
		preview := d.partial.rows()
		land := len(strings.Split(renderMarkdown(text, fmtW), "\n"))
		if delta := preview - land; delta < -1 || delta > 1 {
			t.Errorf("answer %d: the preview draws %d rows and the landed block %d\n%s", i, preview, land, d.partial.view)
		}
	}

	// And against what agents wrote: the recorded answers that have several blocks.
	var checked, worst int
	for _, text := range corpusReplies(t) {
		if len(text) > 2500 || strings.Count(text, "\n\n") < 2 {
			continue
		}
		d := streamed(formatDM(120), text)
		preview, land := d.partial.rows(), len(strings.Split(renderMarkdown(text, fmtW), "\n"))
		checked++
		worst = max(worst, max(preview-land, land-preview))
	}
	t.Logf("%d recorded answers streamed: the largest gap between preview and landed rows is %d", checked, worst)
	if checked < 5 {
		t.Fatalf("only %d recorded answers have blocks to compare", checked)
	}
	if worst > 1 {
		t.Errorf("a recorded answer's preview and landed block differ by %d rows, want at most 1", worst)
	}
}

// A pane that opens halfway through a block missed the lines that say what the
// rest of it is, so it shows that block raw to the end. Held for the case that
// leaves the DM looking synced when it is not: a pane closed with ⌃W still hears
// a message start and a landing, and tokens only while it is drawn.
func TestAPaneOpenedMidBlockPreviewsThatBlockRaw(t *testing.T) {
	renders := chunkCounter(t)
	a := fmtApp(t).applyFrame(startFrame("s1")).streams("s1", "first paragraph.\n\nsecond **para")
	if *renders == 0 {
		t.Fatal("a pane that saw the block begin formatted nothing: the control for everything below")
	}

	a = a.closeDM()
	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, "first paragraph.\n\nsecond paragraph."))
	a = a.applyFrame(startFrame("s1"))
	a = a.streams("s1", "dropped: nobody is looking")
	a = a.openDMWith("s1", "alex")

	*renders = 0
	a = a.streams("s1", "still code\n\nmore **code**\n\nlast words")
	if *renders != 0 {
		t.Errorf("a pane opened mid-block rendered %d finished chunks of a block it never saw begin", *renders)
	}
	out := shown(a)
	if !strings.Contains(out, "more **code**") {
		t.Errorf("the block is not drawn as the raw text it is:\n%s", out)
	}

	// The next block it sees begin is formatted again.
	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, "still code"))
	a = a.applyFrame(startFrame("s1"))
	before := *renders
	a = a.streams("s1", "a **fresh** paragraph.\n\nand the next")
	if *renders == before || strings.Contains(shown(a), "**fresh**") {
		t.Errorf("the next block was not formatted: the pane stayed raw after the block it joined had landed:\n%s", shown(a))
	}
}

// Leaving is what a displaced or pushed-off pane does, and it forgets the block.
func TestLeavingAConversationMakesItsNextBlockRawUntilAnotherBegins(t *testing.T) {
	renders := chunkCounter(t)
	d := streamed(formatDM(fmtH), "one.\n\ntwo")
	if *renders != 1 {
		t.Fatalf("%d renders before leaving, want 1", *renders)
	}
	d = d.Leave()
	*renders = 0
	d = streamed(d, "x **y**\n\nz is here\n\nand w")
	if *renders != 0 {
		t.Errorf("a conversation that was left rendered %d chunks of a block it did not see begin", *renders)
	}
	assertShows(t, d, fmtW, fmtH, "x **y**")
}

// A subagent's text landing between two of the agent's own tokens is not the end
// of the agent's block, so it cannot be a place to start reading one.
func TestASubagentsLandingMidBlockLeavesTheBlockRaw(t *testing.T) {
	renders := chunkCounter(t)
	d := streamed(formatDM(fmtH), "one.\n\ntwo")
	if *renders != 1 {
		t.Fatalf("%d renders for the block above the subagent, want 1: the control for what follows", *renders)
	}
	d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "sub says",
		Subagent: &core.Subagent{Dispatch: "d1", Task: "count lines"}})
	*renders = 0
	d = streamed(d, " and **more**.\n\nthree.\n\nfour")
	if *renders != 0 {
		t.Errorf("%d chunks rendered from the middle of a block after a subagent spoke", *renders)
	}
	assertShows(t, d, fmtW, fmtH, "and **more**.")
}

func TestAPaneThatNeverSawAMessageBeginStaysRaw(t *testing.T) {
	renders := chunkCounter(t)
	d := streamed(NewDM("s1", "alex").SetSize(fmtW, fmtH), "one **bold**.\n\ntwo.\n\nthree")
	if *renders != 0 {
		t.Errorf("%d renders for a preview nothing told it how to read", *renders)
	}
	assertShows(t, d, fmtW, fmtH, "one **bold**.")
}

// The inbox keeps the newest bytes of a fold a stalled draw loop left behind,
// so the pane is handed a block with its beginning gone. A code block's opener
// can be what went, and without it the lines after read as prose.
func TestAFoldTrimAcrossAFenceOpenerRendersNothingUntilLanding(t *testing.T) {
	renders := chunkCounter(t)
	a := fmtApp(t)
	a.in = newInbox()

	a.in.add(startFrame("s1"))
	a.in.add(tokenFrame("s1", "```go\nopener := true\n\n"))
	line := "code := inside(the, fence)\n\nmore := code()\n\n"
	for len(line)*3 < foldChars*2 {
		line += line
	}
	for range 4 {
		a.in.add(tokenFrame("s1", line))
	}
	m, _ := a.stream(streamMsg{batch: a.in.take(takeLimit), gen: a.gen})
	a = m.(App)

	if *renders != 0 {
		t.Errorf("%d chunks were rendered from a block whose opener the inbox dropped", *renders)
	}
	if out := shown(a); !strings.Contains(out, "more := code()") {
		t.Errorf("the code is not on screen as the raw text it is:\n%s", out)
	}
	a = a.streams("s1", "tokens after the trim\n\nare raw too\n\nas well")
	if *renders != 0 {
		t.Errorf("%d chunks rendered after the trim, before the block landed", *renders)
	}

	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, "```go\nopener := true\n```"))
	if out := shown(a); !strings.Contains(out, "opener := true") {
		t.Errorf("the landed block is not on screen:\n%s", out)
	}
	landed := *renders
	a = a.applyFrame(startFrame("s1")).streams("s1", "a fresh block.\n\nnext")
	if *renders-landed != 1 {
		t.Errorf("the block after the landing rendered %d finished chunks, want 1: the pane did not read it again", *renders-landed)
	}
}

// A tile has no business formatting a preview: it is a cell of a board, cheap to
// leave open, and it draws its tail through oneLine.
func TestABoardTilePreviewStaysRaw(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	a = a.ensureBoardDMs().foldBoard("s1", assistantBlock("an earlier block")) // sizes the preview, as a tile's history does
	renders := chunkCounter(t)
	if streamed(formatDM(fmtH), "one **bold**.\n\ntwo"); *renders == 0 {
		t.Fatal("a pane formatted nothing of the same tokens: the control for the tile below")
	}
	*renders = 0

	a = a.foldBoard("s1", core.Event{Kind: core.KindMessageStart, SessionID: "s1"})
	for _, tok := range pieces("one **bold**.\n\ntwo", 5) {
		a = a.foldBoard("s1", partialEv(tok))
	}
	if *renders != 0 {
		t.Errorf("a board tile rendered %d chunks of its preview", *renders)
	}
	if view := a.boardDMs["s1"].partial.sized(30).view; !strings.Contains(view, "one **bold**.") || strings.HasPrefix(view, "  ") {
		t.Errorf("the tile's preview is not the raw text from the left edge:\n%q", view)
	}
}

// A conversation nobody can see pays for nothing, finished blocks included.
func TestAnOffScreenPaneRendersNoChunk(t *testing.T) {
	a := fmtApp(t).openRight("s2", "sydney").hideDM(true) // sydney's transcript is kept and not drawn
	renders := chunkCounter(t)

	a = a.applyFrame(startFrame("s2")).streams("s2", "one.\n\ntwo.\n\nthree")
	if *renders != 0 {
		t.Errorf("an off-screen conversation rendered %d chunks", *renders)
	}
	a = a.applyFrame(startFrame("s1")).streams("s1", "one.\n\ntwo.\n\nthree")
	if *renders != 2 || a.dms["s1"].partial.view == "" {
		t.Errorf("the drawn conversation rendered %d chunks (preview %q), want its 2 and a preview", *renders, a.dms["s1"].partial.view)
	}
}

// The bound that keeps the work per token flat, now that a block is kept whole
// until it is read: the open block holds up to MaxChunk bytes, and past that the
// splitter freezes and the tail is cut as it always was.
func TestAFormattedPreviewIsBoundedToo(t *testing.T) {
	d := streamed(formatDM(20), "a finished block.\n\n")
	if len(d.partial.fin.chunks()) != 0 {
		t.Fatal("a block was finished before the next one began")
	}
	d = streamed(d, "x")
	if len(d.partial.fin.chunks()) != 1 {
		t.Fatalf("%d finished blocks held, want 1: this test is about a pane that reads blocks", len(d.partial.fin.chunks()))
	}
	token := "the quick brown fox jumps over the lazy dog. "
	for range 400 {
		d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: token})
		if limit := max(previewChars(fmtW, d.partial.cap), render.MaxChunk); len(d.partial.text) > limit {
			t.Fatalf("the open block holds %d bytes, want at most %d", len(d.partial.text), limit)
		}
	}
	if d.partial.rows() > d.previewCap(true) {
		t.Errorf("the preview draws %d rows, want at most %d", d.partial.rows(), d.previewCap(true))
	}

	// Lines of a paragraph that add up to just under the bound, then a long line:
	// the most a block can hold before the splitter gives up on it.
	d = formatDM(20)
	for range 63 {
		d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: strings.Repeat("w", 63) + "\n"})
	}
	for range 200 {
		d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: token})
		if len(d.partial.text) > 2*render.MaxChunk {
			t.Fatalf("the open block holds %d bytes, want at most twice the bound", len(d.partial.text))
		}
	}
}

// The source of a finished block is handed to the renderer exactly as a landed
// block's is: a trim on one side is a blank row, or here a paragraph for a block of
// code, on the other.
func TestAFinishedBlockIsRenderedFromItsUntrimmedSource(t *testing.T) {
	text := "    indented code opens the answer\n\nthen a paragraph that is still being"
	d := streamed(formatDM(fmtH), text)
	if len(d.partial.fin.chunks()) != 1 {
		t.Fatalf("%d finished blocks, want 1", len(d.partial.fin.chunks()))
	}
	whole := strings.Split(stripANSI(renderMarkdown(text, fmtW)), "\n")
	for i, row := range d.partial.fin.chunks()[0].rows {
		if got := stripANSI(row); got != whole[i] {
			t.Fatalf("row %d of the finished block is %q, the landed answer draws %q", i, got, whole[i])
		}
	}
}

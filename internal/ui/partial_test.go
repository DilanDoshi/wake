package ui

// The preview, and the four properties that make it affordable.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// tokens feeds a DM one partial event per delta, the way the wire does.
func tokens(d DM, words ...string) DM {
	for _, w := range words {
		d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: w})
	}
	return d
}

func TestThePreviewShowsTheTokensThatHaveArrived(t *testing.T) {
	d := tokens(NewDM("s1", "alex").SetSize(60, 20), "Fixed ", "the ", "retry ", "header")
	assertShows(t, d, 60, 20, "Fixed the retry header")
}

// The whole cost argument rests on this: a partial never becomes an event and
// never becomes a rendered line, so it cannot make Append superlinear, cannot
// invalidate the memoized transcript, and cannot be re-wrapped by a resize.
func TestAPreviewNeverEntersTheTranscript(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	before := d.tr.lines.len()

	d = tokens(d, "Fixed ", "the ", "retry ", "header")

	if d.events.len() != 0 {
		t.Errorf("%d events after four partials, want 0: a preview stored is every sentence held twice", d.events.len())
	}
	if got := d.tr.lines.len(); got != before {
		t.Errorf("the transcript grew from %d lines to %d: a partial that renders a line is a partial a width change has to re-wrap", before, got)
	}
}

// The completed block is the record and the preview is not, so the words are
// on screen exactly once at every moment - including the one moment both
// accounts of them exist.
func TestTheCompletedBlockReplacesThePreviewRatherThanDoublingIt(t *testing.T) {
	d := tokens(NewDM("s1", "alex").SetSize(60, 20), "Fixed ", "the ", "retry ", "header")
	d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "Fixed the retry header"})

	if n := strings.Count(visible(d, 60, 20), "Fixed the retry header"); n != 1 {
		t.Errorf("the sentence is on screen %d times, want 1: the preview is replaced by the block, never left under it", n)
	}
	if d.partial.text != "" {
		t.Errorf("the preview still holds %q after its block landed", d.partial.text)
	}
}

// A turn can end without ever producing the block its tokens were building -
// an interrupt is the recorded case, and CLAUDE.md's traps note that such a
// turn has no result text at all. Nothing else would ever clear it, so half a
// sentence would sit under the transcript until the agent next spoke.
func TestAnInterruptedTurnLeavesNoHalfSentenceUnderTheTranscript(t *testing.T) {
	d := tokens(NewDM("s1", "alex").SetSize(60, 20), "Fixed the retr")
	d = d.Append(core.Event{Kind: core.KindTurnEnd, SessionID: "s1"})

	assertHides(t, d, 60, 20, "Fixed the retr")
	if d.partial.text != "" {
		t.Errorf("the preview survived the turn that was writing it: %q", d.partial.text)
	}
}

// The bound is what makes the cost flat. Without it a 13,499-character block -
// the longest in the recorded corpus - is laid out in full on every token that
// arrives, and the work per delta grows with the answer. The pane keeps every
// row of the answer, but a token lays out only the open block it lands on, or the
// line once it is not read for blocks, and that is held to render.MaxChunk - the
// newest of it, since the newest tokens are the ones being read.
func TestATokenLaysOutOnlyTheBoundedTextItLandsOn(t *testing.T) {
	for _, reads := range []bool{false, true} {
		d := NewDM("s1", "alex").SetSize(60, 20)
		if reads {
			d = messageStarted(d)
		}
		for range 400 {
			d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: "the quick brown fox jumps over the lazy dog. "})
		}
		if got := len(d.partial.text); got > render.MaxChunk {
			t.Errorf("reads blocks = %v: a token lays out %d characters, want at most %d: an unbounded open text is an unbounded wrap on every token", reads, got, render.MaxChunk)
		}
		if !strings.HasSuffix(d.partial.text, "lazy dog. ") {
			t.Errorf("reads blocks = %v: the preview kept the wrong end of the block: %q", reads, lastRunes(d.partial.text, 40))
		}
	}
}

// The answer fills an otherwise-empty pane rather than sitting in a box while the
// rest of it stays blank, and the pane draws exactly its height with an answer of
// any length under a reader following it or scrolled back: a frame past its height
// scrolls the alt screen away on every draw.
func TestTheAnswerFillsThePaneAndThePaneKeepsItsHeight(t *testing.T) {
	const w, h = 60, 30
	long := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 200)

	empty := NewDM("s1", "alex")
	empty.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	empty = tokens(empty.SetSize(w, h), long)
	if got := strings.Count(visible(empty, w, h), "lazy dog."); got < h/2 {
		t.Errorf("into an empty %d-row pane the answer drew %d rows: it is stuck in a box while the pane sits blank", h, got)
	}

	full := NewDM("s1", "alex")
	full.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	full = full.SetSize(w, h)
	for range 100 {
		full = full.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "an earlier line of the conversation"})
	}
	for name, d := range map[string]DM{"empty": empty, "following": tokens(full, long), "scrolled back": tokens(full.ScrollUp(3), long)} {
		if got := lipgloss.Height(d.View(w, h)); got != h {
			t.Errorf("%s: the answer drew the pane %d rows tall, want %d", name, got, h)
		}
	}
}

// A pane too short to also hold a preview yields the preview to zero rows rather
// than drawing it and overflowing - a frame one row past its height scrolls the
// alt screen away on every draw. Regression for the tiny-working-pane case the
// code review caught: previewCap's floor forced a row that did not fit.
func TestATightPaneYieldsThePreviewRatherThanOverflowing(t *testing.T) {
	long := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 200)
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	d = d.SetSize(60, 40)  // establish the chrome at a comfortable size first
	floor := d.minHeight() // chrome plus one transcript row, no preview reserved
	for _, h := range []int{floor, floor + 1, floor + 2} {
		dd := tokens(d.SetSize(60, h), long)
		if got := lipgloss.Height(dd.View(60, h)); got != h {
			t.Fatalf("a %d-row working pane streaming a long reply drew %d rows: the preview overflowed a pane too tight to hold it", h, got)
		}
	}
}

// The open line is cut by byte, because the bound is on work and counting runes
// to find it would cost the length of the answer. So the cut lands mid-rune on any
// text that is not ASCII, and what must not happen is a replacement character on a
// row - or a row whose display width no longer matches the pane it was laid out for.
func TestCuttingTheOpenLineByByteNeverDrawsAHalfRune(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(40, 20)
	for range 400 {
		// No spaces, so the wrap has no break opportunity and every row is
		// exactly the pane's width - which is what makes a split rune visible.
		d = d.Append(core.Event{Kind: core.KindPartialText, SessionID: "s1", Text: "日本語のテキスト"})
	}
	if len(d.partial.text) < render.MaxChunk-len("日本語のテキスト") {
		t.Fatalf("the open line holds %d bytes: it was never cut, so this proves nothing", len(d.partial.text))
	}
	for _, l := range d.partial.rows.slice(0, d.partial.rows.len()) {
		if !utf8.ValidString(l) || strings.ContainsRune(l, utf8.RuneError) {
			t.Errorf("a preview row draws a broken rune: %q", l)
		}
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("a preview row measures %d cells in a 40-column pane: %q", w, l)
		}
	}
}

// The chrome argument from dm.go, one row further: a preview that appears
// mid-turn changes the pane's chrome without anybody resizing anything, and a
// frame one row too tall scrolls the alt screen away on every draw.
func TestThePaneStaysInBoundsWhileAPreviewIsDrawn(t *testing.T) {
	const w, h = 60, 20
	d := NewDM("s1", "alex").SetSize(w, h)
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	for _, n := range []int{0, 1, 40, 400} {
		d = tokens(d, strings.Repeat("token ", n))
		if got := lipgloss.Height(d.View(w, h)); got != h {
			t.Fatalf("with a %d-token preview the pane drew %d rows, want %d", n, got, h)
		}
	}
}

// The answer is the window's last rows, never chrome, so a preview leaves the
// pane's floor where it was, and at that floor the pane still draws its height.
func TestAPreviewLeavesThePanesFloorWhereItWas(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	bare := d.minHeight()

	d = tokens(d, strings.Repeat("token ", 200))
	if floor := d.minHeight(); floor != bare {
		t.Errorf("the floor is %d rows with a preview and %d without: the answer is being counted as chrome", floor, bare)
	}
	if got := lipgloss.Height(d.View(60, bare)); got != bare {
		t.Errorf("at its floor of %d rows the pane drew %d: a frame one row too tall scrolls the alt screen away on every draw", bare, got)
	}
}

// The preview is the agent's own words being written, so it belongs above the
// working line for the reason dm.go states about the working line itself: the
// newest thing said, then what is happening now, then where you type.
func TestThePreviewSitsBetweenTheTranscriptAndTheWorkingLine(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20)
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking, startedAt: clock()}
	d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "earlier turn"})
	d = tokens(d, "writing now")

	out := visible(d, 60, 20)
	said, preview := lineIndex(out, "earlier turn"), lineIndex(out, "writing now")
	if said < 0 || preview < 0 || said >= preview {
		t.Errorf("transcript at line %d and preview at line %d, want the preview below the transcript:\n%s", said, preview, out)
	}
}

// A width change re-wraps the preview, for the reason it re-wraps everything
// else: the rows it was laid out for no longer exist.
func TestAWidthChangeReWrapsThePreview(t *testing.T) {
	d := tokens(NewDM("s1", "alex").SetSize(120, 20), strings.Repeat("token ", 30))
	wide := d.partial.rows.len()
	narrow := d.SetSize(40, 20).partial.rows.len()
	if narrow <= wide {
		t.Errorf("the preview drew %d rows at 120 columns and %d at 40: it was not re-wrapped", wide, narrow)
	}
}

// A preview is per-token work, and per-token work for something nobody can see
// is the "work per frame that could be work per change" the first
// non-negotiable forbids, one surface over.
//
// It matters more than it looks because App.dms holds **every conversation ever
// opened** rather than the ones on screen - hideDM keeps the transcript so ⌃W is
// reversible - and every write to it copied the whole map of DM values. So an
// operator who has looked at all thirty agents was paying thirty large struct
// copies per token, measured at 106-123ms per fleet-second against 12.9ms with
// one conversation open. See BenchmarkStreamingFleetSecond's open= arms.
func TestOnlyAConversationOnScreenPaysForATokenStream(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "sydney")
	a = a.openDMWith("s1", "alex").openRight("s2", "sydney")
	a = a.hideDM(true) // s2's transcript is kept, and it is no longer drawn.

	for _, id := range []string{"s1", "s2"} {
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
			Kind: core.KindPartialText, SessionID: id, Text: "writing now",
		}})
	}

	if a.dms["s1"].partial.text == "" {
		t.Error("the drawn conversation holds no preview: the gate is refusing the pane somebody is reading")
	}
	if got := a.dms["s2"].partial.text; got != "" {
		t.Errorf("a conversation nobody is looking at accumulated %q: that is a map copy per token for a pane that is not on screen", got)
	}
}

// The gate is on the *accumulate* and never on the clear, and that asymmetry is
// the whole of what keeps a stale preview impossible. A conversation drawn
// while its agent was writing, then closed, then reopened, would otherwise come
// back showing a half-sentence from a block that finished long ago.
func TestClosingAPaneMidBlockLeavesNothingToComeBackTo(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "sydney")
	a = a.openDMWith("s1", "alex")

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindPartialText, SessionID: "s1", Text: "half a sent",
	}})
	if a.dms["s1"].partial.text == "" {
		t.Fatal("the fixture never accumulated a preview, so it cannot show one being cleared")
	}

	a = a.openDMWith("s2", "sydney") // s2 takes the pane s1 was in.
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1", Text: "half a sentence, finished",
	}})

	if got := a.dms["s1"].partial.text; got != "" {
		t.Errorf("a closed conversation still holds %q after its block landed: the clear must not be gated on being drawn, or reopening shows a sentence that finished long ago", got)
	}
}

// streamRateFixture is how fast a session speaks, measured over the recorded
// corpus by scripts/measure-stream-rate.py. It is a fixture rather than a
// figure in a comment because the whole cost argument for this feature is built
// on it: one output token is one partial frame, so the rate *is* what
// --include-partial-messages does to the frame rate.
const streamRateFixture = "testdata/stream-rate.json"

// The benchmark's constants are Claude's numbers, so they are held to the
// corpus rather than typed. Without this they are exactly what CLAUDE.md calls
// wrong by default - and worse than most, because they are the numbers that
// justify rejecting the obvious implementation.
//
// internal/ui may not read Claude's JSON, which is why the derivation is a
// script writing a fixture and not a test walking testdata/stream: the airlock
// is seven files in internal/core, and this package reads only the answer.
func TestTheStreamingConstantsStillDescribeTheCorpus(t *testing.T) {
	raw, err := os.ReadFile(streamRateFixture)
	if err != nil {
		t.Fatalf("reading %s: %v\nregenerate with scripts/measure-stream-rate.py", streamRateFixture, err)
	}
	var f struct {
		Turns  int `json:"turns"`
		Blocks int `json:"blocks"`
		Rate   struct {
			Median float64 `json:"median"`
			Max    float64 `json:"max"`
		} `json:"tokens_per_second"`
		Block struct {
			Mean int `json:"mean"`
			Max  int `json:"max"`
		} `json:"block_chars"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parsing %s: %v", streamRateFixture, err)
	}
	if f.Turns == 0 || f.Blocks == 0 {
		t.Fatalf("%s describes %d turns and %d blocks: the measurement is empty and this test is asserting nothing", streamRateFixture, f.Turns, f.Blocks)
	}

	// Truncated rather than rounded, because the constant is a whole number of
	// frames a second and a fractional one cannot be emitted.
	if got := int(f.Rate.Median); got != medianTokensPerSecond {
		t.Errorf("the corpus's median is %.1f tokens/s and medianTokensPerSecond says %d: regenerate with scripts/measure-stream-rate.py and re-read the ruling in decisions.md, which quotes it", f.Rate.Median, medianTokensPerSecond)
	}
	if f.Block.Mean != medianBlockChars {
		t.Errorf("the corpus's mean assistant block is %d characters and medianBlockChars says %d: the rejected granularity is superlinear in exactly this dimension, so the fleet arm is priced at the wrong block length", f.Block.Mean, medianBlockChars)
	}

	// The two figures the prose quotes and no constant carries. Checked so that
	// re-recording the corpus reddens the sentences as well as the constants.
	for _, c := range []struct {
		what string
		got  float64
		want float64
	}{
		{"the fastest recorded turn, quoted as 93.9 tokens/s", f.Rate.Max, 93.9},
		{"the longest recorded block, quoted as 13,499 characters", float64(f.Block.Max), 13499},
	} {
		if c.got != c.want {
			t.Errorf("%s is now %.1f: CLAUDE.md, decisions.md and internal/ui/partial.go all quote the old figure", c.what, c.got)
		}
	}
}

// lastRunes is the tail of s, for a failure message.
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

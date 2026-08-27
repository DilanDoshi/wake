// How a subagent's work is drawn, and the two properties that have to hold
// for the DM to be an honest account of what an agent did.
//
// Both are asserted against the real recording rather than against events
// this file made up. subagent-parallel.jsonl is two subagents and their
// parent interleaved line by line - it is the shape that produced the
// monologue, so it is the shape that has to prove the fix.

package ui

import (
	"bufio"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// The width the subagent tests render at. Wide enough that a header and a
// gutter-prefixed tool call both survive without truncation, which is what
// the assertions read.
const subagentTestWidth = 100

// --- property one: a subagent's line is tellable from the agent's own -------

func TestASubagentsBlockIsMarkedAndTheAgentsIsNot(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)

	own := d.eventBlock(core.Event{
		Kind: core.KindToolUse,
		Tool: &core.ToolCall{Name: "Bash", Display: "go test ./..."},
	})
	theirs := d.eventBlock(core.Event{
		Kind:     core.KindToolUse,
		Tool:     &core.ToolCall{Name: "Bash", Display: "go test ./..."},
		Subagent: &core.Subagent{Dispatch: "toolu_012Ft9BXrwnJVK3VrusxLKcJ", Type: "general-purpose", Task: "Count lines in alpha.txt"},
	})

	if own == theirs {
		t.Fatal("a subagent's tool call renders identically to the agent's own")
	}
	if strings.Contains(stripANSI(own), subagentGutter) {
		t.Errorf("the agent's own block carries the subagent gutter:\n%s", own)
	}
	for i, line := range strings.Split(stripANSI(theirs), "\n") {
		if i == 0 {
			if !strings.Contains(line, "Count lines in alpha.txt") {
				t.Errorf("header does not name the subagent: %q", line)
			}
			continue
		}
		if !strings.HasPrefix(line, subagentGutter) {
			t.Errorf("line %d of a subagent block is unmarked, so it is ambiguous on its own: %q", i, line)
		}
	}
}

// The header falls back through the fields the wire may omit, and never to
// nothing: an unnamed subagent is still a subagent, and a block with no
// header at all is back to being the agent's own.
func TestASubagentHeaderIsNeverEmpty(t *testing.T) {
	cases := map[string]struct {
		sub  core.Subagent
		want string
	}{
		"task and type":  {core.Subagent{Dispatch: "toolu_abcdWXYZ", Type: "general-purpose", Task: "Count lines"}, "Count lines"},
		"type only":      {core.Subagent{Dispatch: "toolu_abcdWXYZ", Type: "general-purpose"}, "general-purpose"},
		"neither":        {core.Subagent{Dispatch: "toolu_abcdWXYZ"}, subagentUnnamed},
		"agent id only":  {core.Subagent{Agent: "ab1b72d53680ae187"}, subagentUnnamed},
		"nothing at all": {core.Subagent{}, subagentUnnamed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := subagentHeader(&c.sub)
			if !strings.Contains(got, c.want) {
				t.Errorf("header = %q, want it to name %q", got, c.want)
			}
			if strings.TrimSpace(strings.TrimPrefix(got, subagentLead)) == "" {
				t.Errorf("header = %q, which names nobody", got)
			}
		})
	}
}

// The tag exists for the case the description cannot cover. A model can hand
// two subagents the same description, and without the id they would be one
// voice again.
func TestTwoSubagentsWithTheSameDescriptionStillDiffer(t *testing.T) {
	a := subagentHeader(&core.Subagent{Dispatch: "toolu_012Ft9BXrwnJVK3VrusxLKcJ", Task: "Count lines"})
	b := subagentHeader(&core.Subagent{Dispatch: "toolu_01SrsyL4cWGpk9TPFcHJmFWg", Task: "Count lines"})
	if a == b {
		t.Errorf("two dispatches with one description render the same header: %q", a)
	}
}

// A subagent that produced nothing renders nothing. A lone header naming a
// subagent that did not speak is a blank turn in the transcript.
func TestASubagentEventWithNoBodyDrawsNoHeader(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)
	got := d.eventBlock(core.Event{
		Kind:     core.KindThinking,
		Text:     "   ",
		Subagent: &core.Subagent{Dispatch: "toolu_a", Task: "Count lines"},
	})
	if got != "" {
		t.Errorf("block = %q, want empty", got)
	}
}

// The forwarded user frame is the prompt an agent handed its subagent, not
// something a human typed. Six of the corpus's sixteen user-text events are
// exactly this, and every one used to be headed "› you".
func TestASubagentsPromptIsNotAttributedToTheHuman(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)
	out := stripANSI(d.eventBlock(core.Event{
		Kind:     core.KindUserText,
		Text:     "Count the number of lines in alpha.txt",
		Subagent: &core.Subagent{Dispatch: "toolu_a", Task: "Count lines"},
	}))

	if strings.Contains(out, userLabel) {
		t.Errorf("a subagent's prompt is headed %q, which says a human typed it:\n%s", userLabel, out)
	}
	if !strings.Contains(out, promptLabel) {
		t.Errorf("a subagent's prompt is not labelled as one:\n%s", out)
	}
}

// I1 at the view. The operator approves an irreversible write; they have to
// be able to see who asked.
func TestASubagentsPermissionAskIsNotTheAgentsOwn(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)
	ask := core.Event{
		Kind:      core.KindPermissionRequest,
		RequestID: "r1",
		Tool:      &core.ToolCall{Name: "Write", Display: "/tmp/tally.txt"},
	}
	own := stripANSI(d.eventBlock(ask))

	ask.Subagent = &core.Subagent{Agent: "ab1b72d53680ae187"}
	theirs := stripANSI(d.eventBlock(ask))

	if own == theirs {
		t.Fatalf("a subagent's ask renders identically to the agent's own:\n%s", own)
	}
	for _, want := range []string{permissionLabel, "Write", subagentGutter} {
		if !strings.Contains(theirs, want) {
			t.Errorf("a subagent's ask is missing %q:\n%s", want, theirs)
		}
	}
}

// The ask the operator is looking at is dead, and the DM has to say so.
//
// This is the defect at the surface an operator actually sees. Wake dropped the
// withdrawal, so the ⚠ block stayed the last word in the conversation and the
// answer it invites goes nowhere: a well-formed allow for a withdrawn request
// produces no frame, no error and no tool run. The transcript is append-only, so
// "clearing" the prompt is drawing its retirement under it - and it has to be
// under it, because an operator reads down.
func TestAWithdrawnPermissionRequestIsRetiredUnderTheAskItKilled(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 24).
		Append(core.Event{
			Kind:      core.KindPermissionRequest,
			RequestID: "req-1",
			Tool:      &core.ToolCall{Name: "Write", Display: "notes.txt"},
		}).
		Append(core.Event{Kind: core.KindRequestWithdrawn, RequestID: "req-1"})

	out := stripANSI(d.View(60, 24))
	ask, withdrawn := lineIndex(out, permissionLabel), lineIndex(out, withdrawnLabel)
	if withdrawn < 0 {
		t.Fatalf("a withdrawn ask draws nothing, so the prompt above it is still the last word:\n%s", out)
	}
	if ask < 0 {
		t.Fatalf("the ask itself is missing, so this test is not about anything:\n%s", out)
	}
	if withdrawn < ask {
		t.Errorf("the withdrawal is drawn at line %d, above the ask at line %d:\n%s", withdrawn, ask, out)
	}
}

// C2's second half at the view. A completed dispatch's receipt repeats the
// subagent's final message verbatim; drawing it prints the report twice.
func TestADispatchReceiptDoesNotRedrawItsReport(t *testing.T) {
	const report = "All four files end with a trailing newline"
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)

	out := stripANSI(d.eventBlock(core.Event{
		Kind:     core.KindToolResult,
		Text:     report,
		Tool:     &core.ToolCall{ID: "toolu_d"},
		Subagent: &core.Subagent{Dispatch: "toolu_d", Agent: "a73", Type: "general-purpose", Result: core.SubagentFinished},
	}))

	if strings.Contains(out, report) {
		t.Errorf("the receipt redrew the report:\n%s", out)
	}
	if !strings.Contains(out, subagentFinishedNote) {
		t.Errorf("the receipt was suppressed without saying so, which reads as a dropped frame:\n%s", out)
	}
}

// An async dispatch's receipt is a launch acknowledgement whose own text
// tells the model not to quote it.
func TestAnAsyncDispatchReceiptSaysItLaunched(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)
	out := stripANSI(d.eventBlock(core.Event{
		Kind:     core.KindToolResult,
		Text:     "Async agent launched successfully. (internal metadata, never quote)",
		Subagent: &core.Subagent{Dispatch: "toolu_d", Agent: "a14", Result: core.SubagentLaunched},
	}))

	if strings.Contains(out, "internal metadata") {
		t.Errorf("the launch receipt's own metadata reached the reader:\n%s", out)
	}
	if !strings.Contains(out, subagentLaunchedNote) {
		t.Errorf("nothing says a subagent started:\n%s", out)
	}
}

// A status the decoder does not model must not inherit the licence to hide
// something. Both modelled values suppress a body; an unmodelled one shows it.
func TestAnUnmodelledDispatchStatusStillShowsItsContent(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 40)
	out := stripANSI(d.eventBlock(core.Event{
		Kind:     core.KindToolResult,
		Text:     "the subagent failed and this is why",
		Subagent: &core.Subagent{Dispatch: "toolu_d", Agent: "a1", Result: core.SubagentUnknown},
	}))

	if !strings.Contains(out, "the subagent failed") {
		t.Errorf("an unmodelled status was suppressed like a known one:\n%s", out)
	}
}

// --- property two: three concurrent streams are not one monologue ----------

// The whole of subagent-parallel.jsonl, decoded by the real airlock and drawn
// by the real DM. Nothing in this test is constructed: the interleaving, the
// two dispatches and the parent's own turns are what the recording contains.
//
// What it asserts is that every rendered line can be assigned to exactly one
// of the three streams by looking at that line and its header alone - which
// is the property a reader needs, because a reader has no access to the
// events behind the text.
func TestThreeConcurrentStreamsAreNotOneMonologue(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(subagentTestWidth, 200)

	speech := map[string]int{}   // stream tag -> blocks of that subagent's own work
	receipts := map[string]int{} // stream tag -> status lines about it
	order := []string{}          // subagent blocks in transcript order, by tag
	parentBlocks := 0

	for n, line := range fixtureLines(t, "subagent-parallel.jsonl") {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("line %d: %v", n+1, err)
		}
		for _, ev := range evs {
			text := stripANSI(d.eventBlock(ev))
			if text == "" {
				continue
			}
			rows := strings.Split(text, "\n")
			if ev.Subagent == nil {
				if strings.Contains(text, subagentGutter) || strings.HasPrefix(rows[0], subagentLead) {
					t.Errorf("line %d: the parent's own block is marked as a subagent's:\n%s", n+1, text)
				}
				parentBlocks++
				continue
			}

			// The stream's identity has to be on the block, not merely in the
			// event behind it: a reader has no access to the event.
			tag := subagentTag(ev.Subagent)
			if tag == "" {
				t.Fatalf("line %d: a subagent block has no tag to identify its stream", n+1)
			}
			if !strings.HasPrefix(rows[0], subagentLead) || !strings.Contains(rows[0], tag) {
				t.Errorf("line %d: first row does not identify stream %q: %q", n+1, tag, rows[0])
			}
			order = append(order, tag)

			if receiptNote(ev.Subagent.Result) != "" {
				receipts[tag]++
				if len(rows) != 1 {
					t.Errorf("line %d: a receipt drew %d rows, want the one status line:\n%s", n+1, len(rows), text)
				}
				continue
			}
			speech[tag]++
			for i, l := range rows[1:] {
				if !strings.HasPrefix(l, subagentGutter) {
					t.Errorf("line %d body row %d is unmarked, so it is ambiguous on its own: %q", n+1, i, l)
				}
			}
		}
	}

	// Three streams: the parent and exactly two subagents.
	if len(speech) != 2 {
		t.Fatalf("%d distinct subagent streams, want 2: %v", len(speech), speech)
	}
	if parentBlocks == 0 {
		t.Fatal("no parent blocks: the third stream is missing")
	}
	for tag, blocks := range speech {
		if blocks < 2 {
			t.Errorf("stream %s has %d block(s); one block per stream proves nothing about telling them apart", tag, blocks)
		}
		// The join: the receipt announcing a subagent finished carries the
		// same tag as the work it summarises, so a reader can see which of
		// the two finished. It comes from a different frame, correlated on
		// the dispatch id.
		if receipts[tag] == 0 {
			t.Errorf("stream %s has no receipt carrying its tag, so nothing says which subagent finished", tag)
		}
	}

	// And the recording really does interleave the two *subagents*. If every
	// block of one came before every block of the other, this test would pass
	// on a renderer that could not separate anything - a transcript already
	// sorted by speaker needs no per-line marking.
	//
	// order deliberately holds subagent tags only. The first version of this
	// included the parent's blocks, and the parent speaks at both ends of the
	// fixture - so the closing summary alone satisfied the check and the two
	// subagents could have been fully sorted without anything noticing.
	if !interleaved(order) {
		t.Errorf("the fixture no longer interleaves its subagents, so this test no longer proves the property: %v", order)
	}

	// And nothing but a subagent block is in it, which is the whole of the
	// fix: a helper cannot be misled by input it never receives.
	blocks := 0
	for _, n := range speech {
		blocks += n
	}
	for _, n := range receipts {
		blocks += n
	}
	if len(order) != blocks {
		t.Errorf("order holds %d entries against %d subagent blocks: something else got into it", len(order), blocks)
	}
	for i, tag := range order {
		if tag == "" {
			t.Errorf("order[%d] is empty: a block belonging to no stream is in the interleaving check", i)
		}
	}
}

// interleaved reports whether any stream's blocks are split by another
// stream's - i.e. whether reading top to bottom means switching speaker and
// coming back. It takes stream tags only: a stream nobody has to tell apart
// must not be what makes the answer true.
func interleaved(order []string) bool {
	last := map[string]int{}
	for i, tag := range order {
		if prev, seen := last[tag]; seen {
			for _, between := range order[prev+1 : i] {
				if between != tag {
					return true
				}
			}
		}
		last[tag] = i
	}
	return false
}

// The self-guard above is only worth having if it can fail, and the first
// version could not. These are the orders it has to separate.
func TestInterleavedOnlyReportsAStreamThatResumes(t *testing.T) {
	cases := map[string]struct {
		order []string
		want  bool
	}{
		"nothing":                      {nil, false},
		"one stream":                   {[]string{"a", "a", "a"}, false},
		"two streams already sorted":   {[]string{"a", "a", "a", "b", "b", "b"}, false},
		"three streams already sorted": {[]string{"a", "a", "b", "b", "c", "c"}, false},
		"alternating":                  {[]string{"a", "b", "a", "b"}, true},
		"one resumption is enough":     {[]string{"a", "a", "b", "a"}, true},
		"resumes across two others":    {[]string{"a", "b", "c", "a"}, true},
		// The exact sequence the parent's blocks used to smuggle through:
		// two subagents fully sorted, with the parent speaking either side.
		// Now that order holds tags only, the helper never sees those blocks
		// - and if it did, this is what it must say about them.
		"sorted subagents only": {[]string{"LKcJ", "LKcJ", "LKcJ", "mFWg", "mFWg", "mFWg"}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := interleaved(c.order); got != c.want {
				t.Errorf("interleaved(%v) = %v, want %v", c.order, got, c.want)
			}
		})
	}
}

// Whatever the header and gutter cost, the block still has to fit the width
// it was given. The body is rendered at a reduced width for exactly that
// reason, and nothing else in the suite notices if it is not.
func TestASubagentBlockNeverExceedsItsWidth(t *testing.T) {
	long := strings.Repeat("some/deep/path/", 20) + "file.go"
	events := map[string]core.Event{
		"a tool call": {
			Kind:     core.KindToolUse,
			Tool:     &core.ToolCall{Name: "Read", Display: long},
			Subagent: &core.Subagent{Dispatch: "toolu_012Ft9BXrwnJVK3VrusxLKcJ", Task: strings.Repeat("count ", 30)},
		},
		"prose": {
			Kind:     core.KindAssistantText,
			Text:     strings.Repeat("word ", 120),
			Subagent: &core.Subagent{Dispatch: "toolu_012Ft9BXrwnJVK3VrusxLKcJ", Task: "count"},
		},
		"a receipt": {
			Kind:     core.KindToolResult,
			Text:     "the report",
			Subagent: &core.Subagent{Dispatch: "toolu_012Ft9BXrwnJVK3VrusxLKcJ", Result: core.SubagentFinished},
		},
	}
	for name, ev := range events {
		for _, w := range []int{20, 40, 60, 90} {
			t.Run(fmt.Sprintf("%s at %d", name, w), func(t *testing.T) {
				d := NewDM("s1", "alex").SetSize(w, 40)
				for i, line := range strings.Split(stripANSI(d.eventBlock(ev)), "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Errorf("row %d is %d cells wide, want <= %d: %q", i, got, w, line)
					}
				}
			})
		}
	}
}

// The tag has to survive the fleet the spec names - 15-30 agents, each able
// to dispatch - because two subagents sharing a tag are one stream again,
// which is the failure this whole file exists to prevent.
//
// Both id spaces are checked, because subagentTag reads both: a dispatch id
// on forwarded frames, and the subagent's own id on a permission ask, which
// TestASubagentsPermissionAskIsNotTheAgentsOwn proves renders. They are not
// the same alphabet - toolu_ tails are base-59, agent ids are lowercase hex -
// so the same number of characters buys very different safety, and a guard
// that modelled only the first said 0.0016 about a path whose real risk is
// 0.30.
//
// The alphabets are measured from the recording rather than taken from the
// name "base62": the corpus's 44 toolu_ ids use 59 symbols, never I, O or l.
// Measuring is also the conservative direction - a smaller alphabet means a
// higher bound and a stricter test - and it is the same discipline
// notInTheCorpus exists to enforce, applied to a constant instead of a word.
func TestTheTagDiscriminatesInBothIdSpaces(t *testing.T) {
	const fleet = 200.0
	const maxCollisionRisk = 0.01

	cases := []struct {
		space    string
		alphabet string
		tagLen   int
		build    func(tail string) core.Subagent
	}{
		{
			space:    "dispatch",
			alphabet: measuredAlphabet(t, regexp.MustCompile(`"toolu_([A-Za-z0-9]+)"`)),
			tagLen:   subagentDispatchTagLen,
			build:    func(tail string) core.Subagent { return core.Subagent{Dispatch: "toolu_" + tail} },
		},
		{
			space:    "agent",
			alphabet: measuredAlphabet(t, regexp.MustCompile(`"agent(?:Id|_id)":"a([A-Za-z0-9]+)"`)),
			tagLen:   subagentAgentTagLen,
			build:    func(tail string) core.Subagent { return core.Subagent{Agent: "a" + tail} },
		},
	}

	for _, c := range cases {
		t.Run(c.space, func(t *testing.T) {
			if len(c.alphabet) < 8 {
				t.Fatalf("measured only %d symbols for the %s space: the scan is broken and the bound below is meaningless", len(c.alphabet), c.space)
			}

			// A deterministic sample, so the test cannot flake.
			rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic sample, not security
			seen := map[string]string{}
			for i := 0; i < int(fleet); i++ {
				tail := make([]byte, 22)
				for j := range tail {
					tail[j] = c.alphabet[rng.Intn(len(c.alphabet))]
				}
				sub := c.build(string(tail))
				tag := subagentTag(&sub)
				if len(tag) != c.tagLen {
					t.Fatalf("tag %q is %d characters, want %d", tag, len(tag), c.tagLen)
				}
				id := sub.Dispatch + sub.Agent
				if other, dup := seen[tag]; dup {
					t.Errorf("tag %q is shared by %s and %s: two subagents would read as one stream", tag, other, id)
				}
				seen[tag] = id
			}

			// One sample can pass on luck - three characters survives the
			// draw above for the dispatch space - so the claim is also
			// checked in closed form, over the alphabet just measured.
			space := math.Pow(float64(len(c.alphabet)), float64(c.tagLen))
			risk := (fleet * (fleet - 1)) / (2 * space)
			if risk > maxCollisionRisk {
				t.Errorf("a %d-character %s tag over an alphabet of %d collides with probability %.4f across %.0f dispatches, want below %.4f: two subagents would read as one stream",
					c.tagLen, c.space, len(c.alphabet), risk, fleet, maxCollisionRisk)
			}
		})
	}
}

// measuredAlphabet returns the distinct symbols the recorded corpus uses in
// the capture group of pattern, sorted. It is the corpus talking, not a
// comment: "base62" is documentation and the bytes say 59.
func measuredAlphabet(t *testing.T, pattern *regexp.Regexp) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "stream", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no fixtures matched: nothing to measure")
	}
	symbols := map[rune]bool{}
	samples := 0
	for _, path := range paths {
		for _, line := range fixtureLinesAt(t, path) {
			for _, m := range pattern.FindAllStringSubmatch(line, -1) {
				samples++
				for _, r := range m[1] {
					symbols[r] = true
				}
			}
		}
	}
	if samples == 0 {
		t.Fatalf("pattern %s matched nothing in the corpus: the measurement is empty", pattern)
	}
	out := make([]rune, 0, len(symbols))
	for r := range symbols {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return string(out)
}

// fixtureLines reads one recorded stream, which is how the tests above stay
// tied to what claude actually emitted rather than to what this package
// believes it emits.
func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	return fixtureLinesAt(t, filepath.Join("..", "..", "testdata", "stream", name))
}

func fixtureLinesAt(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var lines []string
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines = append(lines, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	if len(lines) == 0 {
		t.Fatalf("%s is empty", path)
	}
	return lines
}

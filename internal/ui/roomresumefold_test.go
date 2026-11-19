package ui

// The room folds an agent's repeated "↻ resumed" lines into one with a count
// (roomresumefold.go): the daemon really does send a `continue` per account
// flip to every agent the limit keeps stopping, and a screenful of the same
// dozen names is no help to somebody supervising the fleet.

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

var (
	foldSydney = Agent{ID: "s1", Name: "sydney"}
	foldJohn   = Agent{ID: "s2", Name: "john"}
)

// foldResume is the daemon's "continue" to a limited agent, or to one an
// unreachable API stopped.
func foldResume(a Agent, reconnect bool) core.Event {
	flags := byte(rpc.MessageResumed)
	if reconnect {
		flags |= rpc.MessageReconnected
	}
	return core.Event{Kind: core.KindUserText, SessionID: a.ID, Notice: core.NoticeAutoResumed, Text: "continue", MessageID: rpc.NewMessageSend(flags).ID()}
}

// foldMarker is the room's "turn ended, nothing said" for a turn: failed when
// the fleet's per-agent bit says the API failed it (turnoutcome.go).
func foldMarker(a Agent, failed bool) (core.Event, Agent) {
	a.failed = failed
	return core.Event{Kind: core.KindTurnEnd, SessionID: a.ID}, a
}

func foldProse(a Agent, text string) core.Event {
	return core.Event{Kind: core.KindAssistantText, SessionID: a.ID, Text: text}
}

func foldRoom() Room { return NewRoom().SetSize(120, 40) }

// foldedRows is every row the transcript holds, trimmed, blank rows left out.
func foldedRows(r Room) []string {
	var out []string
	for _, row := range r.tr.rows(r.tr.first(), r.tr.lines.len()) {
		if row = strings.TrimSpace(stripANSI(row)); row != "" {
			out = append(out, row)
		}
	}
	return out
}

func rowsWith(r Room, sub string) []string {
	var out []string
	for _, row := range foldedRows(r) {
		if strings.Contains(row, sub) {
			out = append(out, row)
		}
	}
	return out
}

func resumedFold(count int) string {
	return fmt.Sprintf("↻ sydney resumed after the usage limit ×%d", count)
}

// failedCycle is one limited turn: the daemon's resume, then the turn's own end.
func failedCycle(r Room, a Agent) Room {
	r = r.Append(foldResume(a, false), a)
	ev, by := foldMarker(a, true)
	return r.Append(ev, by)
}

func TestRepeatedResumesOfOneAgentFoldIntoOneCountedLine(t *testing.T) {
	r := foldRoom()
	for range 3 {
		r = failedCycle(r, foldSydney)
	}
	if got := rowsWith(r, "resumed after the usage limit"); len(got) != 1 || got[0] != resumedFold(3) {
		t.Errorf("three limited cycles should draw one %q, drew %q:\n%s", resumedFold(3), got, strings.Join(foldedRows(r), "\n"))
	}
	if got := rowsWith(r, "turn ended, nothing said"); len(got) != 1 {
		t.Errorf("the failed turns' marker should be drawn once under the folded line, drew %d:\n%s", len(got), strings.Join(foldedRows(r), "\n"))
	}
}

func TestAFirstResumeDrawsNoCount(t *testing.T) {
	r := failedCycle(foldRoom(), foldSydney)
	if got := rowsWith(r, "resumed after the usage limit"); len(got) != 1 || got[0] != "↻ sydney resumed after the usage limit" {
		t.Errorf("one resume should draw no count, drew %q", got)
	}
}

// What restore can show: the transcript has no turn ends, so a limited agent's
// resumes are back to back.
func TestBareRepeatedResumesFold(t *testing.T) {
	r := foldRoom()
	for range 4 {
		r = r.Append(foldResume(foldSydney, false), foldSydney)
	}
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != resumedFold(4) {
		t.Errorf("four bare resumes should draw one %q, drew %q", resumedFold(4), got)
	}
}

// An answered resume always starts a new line: the agent's own words end the run.
func TestAResumeTheAgentAnsweredStartsANewLine(t *testing.T) {
	r := foldRoom()
	r = failedCycle(r, foldSydney)
	r = r.Append(foldProse(foldSydney, "back at it"), foldSydney)
	r = failedCycle(r, foldSydney)
	if got := rowsWith(r, "resumed after"); len(got) != 2 || got[0] != "↻ sydney resumed after the usage limit" || got[1] != got[0] {
		t.Errorf("a resume after the agent spoke should be its own line, drew %q", got)
	}
}

// A resume that went through reads as one: a successful silent turn (tool-only
// work) ends with the same marker a failed one does, and must not be folded away.
func TestASuccessfulSilentTurnBreaksTheRunAndKeepsItsMarker(t *testing.T) {
	r := foldRoom()
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	ev, by := foldMarker(foldSydney, false) // the API answered: the bit is clear
	r = r.Append(ev, by)
	r = failedCycle(r, foldSydney) // limited again
	got := rowsWith(r, "resumed after")
	if len(got) != 2 {
		t.Fatalf("limit, resume, a successful silent turn, limit again should be two heads, drew %q:\n%s", got, strings.Join(foldedRows(r), "\n"))
	}
	for _, row := range got {
		if strings.Contains(row, "×") {
			t.Errorf("neither head should carry a count: %q", got)
		}
	}
	if n := len(rowsWith(r, "turn ended, nothing said")); n != 2 {
		t.Errorf("the successful turn's marker stays and the failed one's shows once: want 2 markers, got %d:\n%s", n, strings.Join(foldedRows(r), "\n"))
	}
}

// The line is replaced where it stands: another agent's words in between do
// not drag it down to the bottom.
func TestTheFoldedLineStaysWhereItFirstStood(t *testing.T) {
	r := foldRoom()
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldProse(foldJohn, "carrying on"), foldJohn)
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	rows := foldedRows(r)
	at := func(sub string) int {
		return slices.IndexFunc(rows, func(row string) bool { return strings.Contains(row, sub) })
	}
	if at(resumedFold(2)) < 0 || at(resumedFold(2)) > at("carrying on") {
		t.Errorf("the counted line should sit above john's words, where the first resume was:\n%s", strings.Join(rows, "\n"))
	}
}

func TestAnotherAgentsResumeIsItsOwnLine(t *testing.T) {
	r := foldRoom()
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldJohn, false), foldJohn)
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldJohn, false), foldJohn)
	if got := rowsWith(r, "resumed after"); len(got) != 2 || got[0] != resumedFold(2) || got[1] != "↻ john resumed after the usage limit ×2" {
		t.Errorf("each agent folds on its own, drew %q", got)
	}
}

// Keyed on (agent, usage limit vs connection): the two words are two facts.
func TestAConnectionResumeDoesNotFoldIntoALimitResume(t *testing.T) {
	r := foldRoom()
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldSydney, true), foldSydney)
	got := rowsWith(r, "resumed after")
	if len(got) != 2 || got[0] != "↻ sydney resumed after the usage limit" || got[1] != "↻ sydney resumed after the connection came back" {
		t.Errorf("a limit and a connection resume should each draw their own line, drew %q", got)
	}
}

// Anything said into the agent's thread ends the run: a message to it, and a
// broadcast every agent reads; one to somebody else does not.
func TestWhatIsSaidIntoTheThreadEndsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		line  roomLine
		folds bool
	}{
		{"a message to the agent", roomLine{ev: core.Event{Kind: core.KindUserText, Text: "status?"}, to: "s1"}, false},
		{"a broadcast", roomLine{ev: core.Event{Kind: core.KindUserText, Text: "everyone stop"}}, false},
		{"a message to another agent", roomLine{ev: core.Event{Kind: core.KindUserText, Text: "john, go"}, to: "s2"}, true},
		{"another agent's words", roomLine{ev: foldProse(foldJohn, "hello"), by: foldJohn}, true},
		{"the agent's own ask", roomLine{ev: core.Event{Kind: core.KindPermissionRequest, SessionID: "s1", RequestID: "r1"}, by: foldSydney}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := foldRoom().Append(foldResume(foldSydney, false), foldSydney)
			r = r.appendLine(tc.line)
			r = r.Append(foldResume(foldSydney, false), foldSydney)
			got := len(rowsWith(r, "resumed after"))
			if tc.folds && got != 1 || !tc.folds && got != 2 {
				t.Errorf("%s: resume lines drawn = %d, want %d:\n%s", tc.name, got, map[bool]int{true: 1, false: 2}[tc.folds], strings.Join(foldedRows(r), "\n"))
			}
		})
	}
}

// Folding rewrites one row of the same height, so nothing a reader holds moves.
func TestAFoldMovesNoRowAndNoReader(t *testing.T) {
	r := foldRoom()
	for i := range 60 {
		r = r.Append(foldProse(foldJohn, fmt.Sprintf("line %d", i)), foldJohn)
		if i == 5 {
			r = r.Append(foldResume(foldSydney, false), foldSydney)
		}
	}
	r = r.ScrollUp(30)
	before, scroll := r.tr.lines.len(), r.tr.scroll
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	if r.tr.lines.len() != before || r.tr.scroll != scroll {
		t.Errorf("a folded resume moved rows or the reader: lines %d -> %d, scroll %d -> %d", before, r.tr.lines.len(), scroll, r.tr.scroll)
	}
	if r.lineMoves != nil {
		t.Errorf("a folded resume reported line moves %v: no row moved", r.lineMoves)
	}
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != resumedFold(2) {
		t.Errorf("the head's row should read %q, drew %q", resumedFold(2), got)
	}
}

func TestACountedLineSurvivesAReWrap(t *testing.T) {
	r := foldRoom()
	for range 5 {
		r = r.Append(foldResume(foldSydney, false), foldSydney)
	}
	r = r.SetSize(70, 30)
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != resumedFold(5) {
		t.Errorf("a re-wrap should keep %q, drew %q", resumedFold(5), got)
	}
}

// The count is the new fact on the line, so a narrow pane cuts the words.
func TestTheCountSurvivesANarrowPane(t *testing.T) {
	for _, w := range []int{minBlockWidth, 24, 30, 40} {
		r := NewRoom().SetSize(w, 30)
		for range 12 {
			r = r.Append(foldResume(foldSydney, false), foldSydney)
		}
		rows := rowsWith(r, "×12")
		if len(rows) != 1 || !strings.HasSuffix(rows[0], "×12") {
			t.Errorf("width %d: the line should end in its count, drew %q:\n%s", w, rows, strings.Join(foldedRows(r), "\n"))
		}
	}
}

// A line the focus hides keeps its count for when it is shown again.
func TestANarrowedRoomShowsTheFoldedLineOfItsOwnAgent(t *testing.T) {
	r := foldRoom()
	for range 3 {
		r = failedCycle(r, foldSydney)
	}
	r = r.WithFocus("s1", "sydney")
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != resumedFold(3) {
		t.Errorf("narrowed to sydney the room should draw %q, drew %q", resumedFold(3), got)
	}
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.WithFocus("", "")
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != resumedFold(4) {
		t.Errorf("unfocused again the room should draw %q, drew %q", resumedFold(4), got)
	}
}

// A head the focus hides takes the count and shows it when it is drawn again.
func TestAHiddenHeadTakesTheCountForWhenItIsShown(t *testing.T) {
	r := foldRoom().Append(foldResume(foldSydney, false), foldSydney)
	head := r.said.at(r.said.first())
	head.rows = 0 // what a focus on another thread leaves it at
	r.said = r.said.replace(r.said.first(), head)
	rows := r.tr.lines.len()
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	if r.tr.lines.len() != rows {
		t.Errorf("a fold into a hidden head drew rows: %d -> %d", rows, r.tr.lines.len())
	}
	if got := r.counts[head.id]; got != 2 {
		t.Errorf("the hidden head should hold the count 2, holds %d", got)
	}
}

// Reclaiming the head leaves nothing to fold into: the next resume heads a line.
func TestAReclaimedHeadIsNotFoldedInto(t *testing.T) {
	r := foldRoom().Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.reclaimOldest(1)
	if len(r.counts) != 0 {
		t.Errorf("a reclaimed head's count should go with it, room still holds %v", r.counts)
	}
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != "↻ sydney resumed after the usage limit" {
		t.Errorf("after the head was reclaimed the next resume should draw a plain line, drew %q", got)
	}
}

func TestAClearedRoomFoldsNothingIntoWhatItDropped(t *testing.T) {
	r := foldRoom().Append(foldResume(foldSydney, false), foldSydney)
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	r = r.cleared(time.Unix(1, 0))
	if len(r.counts) != 0 {
		t.Errorf("a cleared room should hold no counts, holds %v", r.counts)
	}
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	if got := rowsWith(r, "resumed after"); len(got) != 1 || got[0] != "↻ sydney resumed after the usage limit" {
		t.Errorf("a resume after /clear should draw a plain line, drew %q", got)
	}
}

// Restore: the transcript's resumes come back folded the same way.
func TestRestoredResumesFoldToo(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)
	m := a.apply(oneReply("s1",
		resumeEvent("s1", long),
		resumeEvent("s1", long.Add(time.Minute)),
		resumeEvent("s1", long.Add(2*time.Minute)),
	))
	rows := rowsWith(m.room, "resumed after")
	if len(rows) != 1 || !strings.Contains(rows[0], "sydney resumed after the usage limit ×3") {
		t.Errorf("three restored resumes should draw one counted line, drew %q:\n%s", rows, shown(m))
	}
}

// A restore that merges a later batch re-derives the same line, not a second.
func TestARestoredLateBatchKeepsOneCountedLine(t *testing.T) {
	a := roomWithHistoryAsked(t)
	long := clock().Add(-time.Hour)
	m := a.apply(oneReply("s1", resumeEvent("s1", long), resumeEvent("s1", long.Add(time.Minute))))
	m = m.apply(oneReply("s2", resumeEvent("s2", long.Add(30*time.Second))))
	rows := rowsWith(m.room, "resumed after")
	if len(rows) != 2 {
		t.Fatalf("sydney's folded line and john's: want 2 lines, drew %q", rows)
	}
	if !strings.Contains(rows[0], "sydney resumed after the usage limit ×2") || strings.Contains(rows[1], "×") {
		t.Errorf("sydney should read ×2 and john plain, drew %q", rows)
	}
}

// A late history merge rebuilds the room under a live run, which keeps folding.
func TestALateHistoryMergeLeavesALiveRunOpen(t *testing.T) {
	r := foldRoom().Append(foldResume(foldSydney, false), foldSydney)
	restored := resumeEvent("s2", clock().Add(-time.Hour))
	r = r.Before([]roomLine{{ev: restored, by: foldJohn}})
	r = r.Append(foldResume(foldSydney, false), foldSydney)
	if got := rowsWith(r, "sydney resumed after"); len(got) != 1 || got[0] != resumedFold(2) {
		t.Errorf("after the merge a live resume should still fold into its run, drew %q:\n%s", got, strings.Join(foldedRows(r), "\n"))
	}
	if got := rowsWith(r, "john resumed after"); len(got) != 1 {
		t.Errorf("the restored resume should be drawn once, drew %q", got)
	}
}

// The live path end to end: the failed bit the fleet keeps stamps the marker.
func TestTheLiveRoomFoldsAFailedTurnsMarker(t *testing.T) {
	m := roomWithHistoryAsked(t)
	for range 3 {
		m = m.apply(resumeFrame("s1")).apply(usageLimitFrame("s1")).apply(turnEndFrame("s1"))
	}
	if got := rowsWith(m.room, "resumed after"); len(got) != 1 || got[0] != resumedFold(3) {
		t.Errorf("the live room should draw one %q, drew %q:\n%s", resumedFold(3), got, shown(m))
	}
	if n := len(rowsWith(m.room, "turn ended, nothing said")); n != 1 {
		t.Errorf("the marker of the failed turns should show once, showed %d:\n%s", n, shown(m))
	}
}

// And a resume that went through - the agent's tool call proves the API answered
// - is not folded with the next limit, and its silent turn keeps its marker.
func TestTheLiveRoomKeepsAResumeTheAPIAnswered(t *testing.T) {
	m := roomWithHistoryAsked(t)
	m = m.apply(resumeFrame("s1")).apply(toolUseFrame("s1")).apply(turnEndFrame("s1"))
	m = m.apply(resumeFrame("s1")).apply(usageLimitFrame("s1")).apply(turnEndFrame("s1"))
	got := rowsWith(m.room, "resumed after")
	if len(got) != 2 || strings.Contains(got[0]+got[1], "×") {
		t.Errorf("limit, a resume that worked, limit again should be two plain lines, drew %q:\n%s", got, shown(m))
	}
	if n := len(rowsWith(m.room, "turn ended, nothing said")); n != 2 {
		t.Errorf("both turns' markers should show, showed %d:\n%s", n, shown(m))
	}
}

func turnEndFrame(id string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{Kind: core.KindTurnEnd, SessionID: id}}
}

func toolUseFrame(id string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{Kind: core.KindToolUse, SessionID: id, Tool: &core.ToolCall{ID: "t1", Name: "Bash"}}}
}

// The conversation is the record of what the model read, so it still draws
// every `continue`; only the room folds.
func TestAConversationStillDrawsEveryResume(t *testing.T) {
	a := twoAgents(t)
	for range 3 {
		a = a.apply(resumeFrame("s1"))
	}
	if n := strings.Count(shown(a), autoResumedLabel); n != 3 {
		t.Errorf("the conversation should draw all three resumes, drew %d:\n%s", n, shown(a))
	}
}

// foldAlphabet is every kind of line that can sit near a run: each is a way a
// run is extended, ended or left alone.
func foldAlphabet() []roomLine {
	marker := func(a Agent, failed bool) roomLine {
		ev, by := foldMarker(a, failed)
		return roomLine{ev: ev, by: by}
	}
	return []roomLine{
		{ev: foldResume(foldSydney, false), by: foldSydney},
		{ev: foldResume(foldSydney, true), by: foldSydney},
		{ev: foldResume(foldJohn, false), by: foldJohn},
		marker(foldSydney, true),
		marker(foldSydney, false),
		marker(foldJohn, true),
		{ev: foldProse(foldSydney, "words"), by: foldSydney},
		{ev: foldProse(foldJohn, "words"), by: foldJohn},
		{ev: core.Event{Kind: core.KindUserText, Text: "everyone"}},
		{ev: core.Event{Kind: core.KindUserText, Text: "to sydney"}, to: "s1"},
		{ev: core.Event{Kind: core.KindUserText, Text: "to john"}, to: "s2"},
	}
}

// Whatever order lines arrive in, folding them one by one (the live room) and
// folding them as a batch (a restore) agree on which lines stay and what each
// counts, and the row the live room rewrote in place reads as the full render of
// the same lines.
func TestIncrementalAndBatchFoldsAgree(t *testing.T) {
	alphabet := foldAlphabet()
	rng := rand.New(rand.NewPCG(7, 11))
	for range 400 {
		var lines []roomLine
		r := foldRoom()
		for i := range 1 + rng.IntN(14) {
			l := alphabet[rng.IntN(len(alphabet))]
			r = r.appendLine(l)
			l.id = uint64(i + 1) // the batch's lines carry the ids a restore gives them
			lines = append(lines, stamped(l))
		}
		kept, counts := foldHist(slices.Clone(lines))
		held := r.said.slice(r.said.first(), r.said.len())
		if len(held) != len(kept) {
			t.Fatalf("live room kept %d lines, batch kept %d, sequence %s", len(held), len(kept), describeFold(lines))
		}
		for i := range kept {
			if held[i].ev.Kind != kept[i].ev.Kind || held[i].ev.SessionID != kept[i].ev.SessionID || r.counts[held[i].id] != counts[kept[i].id] {
				t.Fatalf("line %d: live %s count %d, batch %s count %d, sequence %s", i, held[i].ev.Kind, r.counts[held[i].id], kept[i].ev.Kind, counts[kept[i].id], describeFold(lines))
			}
		}
		live := foldedRows(r)
		if again := foldedRows(r.SetSize(r.width+1, r.height).SetSize(r.width, r.height)); !slices.Equal(live, again) {
			t.Fatalf("the rewritten rows differ from a full render:\n%s\nvs\n%s\nsequence %s", strings.Join(live, "\n"), strings.Join(again, "\n"), describeFold(lines))
		}
	}
}

func describeFold(lines []roomLine) string {
	var out []string
	for _, l := range lines {
		out = append(out, fmt.Sprintf("%s/%s/%s/to=%q", l.ev.Kind, l.ev.SessionID, l.ev.Notice, l.to))
	}
	return strings.Join(out, " ")
}

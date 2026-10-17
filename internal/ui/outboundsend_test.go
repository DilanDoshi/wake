package ui

// An agent's own SendMessage draws in its conversation as the peer message it
// is: `↪ <this agent> → <recipient>` and the words under it, folded and opened
// like an incoming one, instead of a bare `⏺ SendMessage` inside the tool count.
// Every pane that draws a conversation does it through DM.renderEvent, so these
// are DM-level: the grid's panes and the board's tiles are all this component.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

const sentWords = "rerun the migration tests and tell me what fails"

// sentResult is the tool result of sendCall's call, t1.
func sentResult(failed bool, text string) core.Event {
	return core.Event{Kind: core.KindToolResult, SessionID: "s1", Text: text,
		Tool: &core.ToolCall{ID: "t1", IsError: failed}}
}

func sentDM(events ...core.Event) DM {
	d := NewDM("s1", "alex").SetSize(80, 30)
	for _, ev := range events {
		d = d.Append(ev)
	}
	return d
}

func TestADMDrawsItsAgentsSendAsAnOutgoingPeerMessage(t *testing.T) {
	d := sentDM(sendCall("s1", "sydney", sentWords))
	assertShows(t, d, 80, 30, crossSessionLead+"alex"+crossSessionArrow+"sydney")
	assertShows(t, d, 80, 30, sentWords)
	assertHides(t, d, 80, 30, "SendMessage")
	assertHides(t, d, 80, 30, "tool use")
}

// Folded and opened like an incoming message: three rows, then a pointer, and a
// click opens it and folds it again.
func TestAnOutgoingMessageFoldsAtThreeRowsAndAClickOpensIt(t *testing.T) {
	long := strings.Join(repeatLines("sent", peerFoldRows+3), "\n")
	d := sentDM(sendCall("s1", "sydney", long))
	assertShows(t, d, 80, 30, "sent-02")
	assertHides(t, d, 80, 30, "sent-03")
	assertShows(t, d, 80, 30, "+3 lines")

	opened, hit := d.openTool(peerRow(t, d, "sent-00"))
	if !hit {
		t.Fatal("a click on a folded outgoing message reported no hit")
	}
	assertShows(t, opened, 80, 30, "sent-05")
	folded, hit := opened.openTool(peerRow(t, opened, "sent-00"))
	if !hit {
		t.Fatal("the opened outgoing message stopped being clickable")
	}
	assertHides(t, folded, 80, 30, "sent-05")

	short := sentDM(sendCall("s1", "sydney", sentWords))
	for line := short.tr.lines.first(); line < short.tr.lines.len(); line++ {
		if _, hit := short.openTool(line); hit {
			t.Fatalf("a click on line %d of a message that fits reported a hit", line)
		}
	}
}

// Two folded sends are two things to open: a click on one leaves the other
// folded, and a send never opens with an incoming message of the same words.
func TestTwoOutgoingMessagesOpenSeparately(t *testing.T) {
	first := sendCall("s1", "sydney", strings.Join(repeatLines("one", peerFoldRows+2), "\n"))
	second := sendCall("s1", "sydney", strings.Join(repeatLines("two", peerFoldRows+2), "\n"))
	d := sentDM(first, second)
	opened, hit := d.openTool(peerRow(t, d, "one-00"))
	if !hit {
		t.Fatal("a click on the first folded send reported no hit")
	}
	assertShows(t, opened, 80, 30, "one-04")
	assertHides(t, opened, 80, 30, "two-04")

	same := core.Event{Kind: core.KindCrossSession, SessionID: "s1", FromName: "sydney", Text: first.Tool.Send.Text}
	if peerKey(same) == peerKey(first) {
		t.Error("an incoming message and a send of the same words share one click key")
	}
}

// A delivered send needs no receipt under the message - the words are the
// record - but a failed one must still say why.
func TestASuccessfulSendDrawsNoReceiptAndAFailedOneDrawsItsError(t *testing.T) {
	const receipt = "“relay ping” → sydney (in that session's inbox, not yet read)"
	ok := sentDM(sendCall("s1", "sydney", sentWords), sentResult(false, receipt))
	assertShows(t, ok, 80, 30, sentWords)
	assertHides(t, ok, 80, 30, "inbox")

	failed := sentDM(sendCall("s1", "sydney", sentWords), sentResult(true, "no session is called sydney"))
	assertShows(t, failed, 80, 30, sentWords)
	assertShows(t, failed, 80, 30, "no session is called sydney")
}

// The message is its own block: the tool calls around it fold as they always
// did, and it is not hidden inside either count.
func TestASendBreaksAToolRunInsteadOfFoldingIntoIt(t *testing.T) {
	read := func(id string) core.Event {
		return core.Event{Kind: core.KindToolUse, SessionID: "s1", Tool: &core.ToolCall{ID: id, Name: "Read", Display: id + ".go"}}
	}
	d := sentDM(read("a"), sendCall("s1", "sydney", sentWords), read("b"))
	assertShows(t, d, 80, 30, sentWords)
	if got := strings.Count(visible(d, 80, 30), "1 tool use"); got != 2 {
		t.Errorf("the reads folded into %d rollups around the send, want two of their own", got)
	}
}

// A subagent's own send is drawn in its own view, inside its gutter, as the tool
// call it is: its name is not this conversation's, so `↪ alex → …` would say the
// wrong thing.
func TestASubagentsSendStaysAToolCall(t *testing.T) {
	ev := sendCall("s1", "sydney", sentWords)
	ev.Subagent = &core.Subagent{Dispatch: "d1"}
	d := sentDM()
	d.viewing = "d1"
	d = d.withSubTranscript("d1", []core.Event{ev})
	assertShows(t, d, 80, 30, "SendMessage")
	assertHides(t, d, 80, 30, crossSessionLead)
}

// A conversation read back off disk, and a board tile's own copy of it, draw the
// same block as the live one.
func TestARestoredConversationAndABoardTileDrawTheSendToo(t *testing.T) {
	restored := NewDM("s1", "alex").SetSize(80, 30).Before([]core.Event{sendCall("s1", "sydney", sentWords)})
	assertShows(t, restored, 80, 30, crossSessionLead+"alex"+crossSessionArrow+"sydney")
	assertShows(t, restored, 80, 30, sentWords)

	tile := NewDM("s1", "alex")
	tile.partial.raw = true // a board cell, as ensureBoardDMs builds one
	tile = tile.SetSize(60, 12).Append(sendCall("s1", "sydney", sentWords))
	assertShows(t, tile, 60, 12, crossSessionLead+"alex"+crossSessionArrow+"sydney")
	assertShows(t, tile, 60, 12, sentWords)
}

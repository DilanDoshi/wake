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
// record - but a failed one must still say why, marked or not: is_error is
// optional on the wire, so only the recorded delivery receipt for this recipient
// is hidden, and anything else degrades toward more output.
func TestASuccessfulSendDrawsNoReceiptAndAFailedOneDrawsItsError(t *testing.T) {
	const receipt = "“relay ping” → sydney (another Claude session on this machine; in that session's inbox, not yet read by its Claude)"
	ok := sentDM(sendCall("s1", "sydney", sentWords), sentResult(false, receipt))
	assertShows(t, ok, 80, 30, sentWords)
	assertHides(t, ok, 80, 30, "inbox")

	for name, result := range map[string]core.Event{
		"marked":                      sentResult(true, "no session is called sydney"),
		"unmarked":                    sentResult(false, "permission denied: SendMessage is not allowed"),
		"another recipient's receipt": sentResult(false, strings.Replace(receipt, "→ sydney", "→ planner", 1)),
	} {
		failed := sentDM(sendCall("s1", "sydney", sentWords), result)
		assertShows(t, failed, 80, 30, sentWords)
		assertShows(t, failed, 80, 30, strings.SplitN(result.Text, " (", 2)[0])
		_ = name
	}
}

// A subagent's own send keeps the result it always had, even one shaped like a
// delivery receipt: only the conversation's own sends are drawn this way.
func TestASubagentsSendResultIsStillDrawn(t *testing.T) {
	call := sendCall("s1", "sydney", sentWords)
	call.Subagent = &core.Subagent{Dispatch: "d1"}
	for _, text := range []string{"Message delivered to parent", "“relay” → sydney (in that session's inbox)"} {
		result := sentResult(false, text)
		result.Subagent = &core.Subagent{Dispatch: "d1"}
		d := sentDM()
		d.viewing = "d1"
		d = d.withSubTranscript("d1", []core.Event{call, result})
		assertShows(t, d, 80, 30, text)
	}
}

// A conversation made of nothing but sends still reclaims its oldest events: a
// send is no tool run to split, so it is a place to cut, or the stored events
// and the re-wrap that renders them grow without bound.
func TestAConversationOfOnlySendsStillReclaims(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30)
	for i := range dmRetentionEvents + 2*chunkSize {
		d = d.Append(sendCall("s1", "sydney", "message "+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))))
	}
	if !d.reclaimed() {
		t.Errorf("%d sends were kept: nothing was reclaimed", d.events.count())
	}
	if got := d.events.count(); got > dmRetentionEvents+chunkSize {
		t.Errorf("a conversation of sends holds %d events, want at most %d", got, dmRetentionEvents+chunkSize)
	}
}

// A send keeps the name it was sent under: a rename later changes what new
// sends say, never what an old one does when a click, ⌃E or a resize lays the
// transcript out again.
func TestAnOutgoingMessageKeepsTheNameItWasSentUnder(t *testing.T) {
	long := strings.Join(repeatLines("sent", peerFoldRows+2), "\n")
	d := sentDM(sendCall("s1", "sydney", long)).WithName("bob")
	opened, hit := d.openTool(peerRow(t, d, "sent-00"))
	if !hit {
		t.Fatal("a click on the folded send reported no hit")
	}
	assertShows(t, opened, 80, 30, crossSessionLead+"alex"+crossSessionArrow+"sydney")
	assertHides(t, opened, 80, 30, "bob"+crossSessionArrow)
	assertShows(t, opened.SetSize(60, 30), 60, 30, crossSessionLead+"alex"+crossSessionArrow+"sydney")

	later := d.Append(sendCall("s1", "planner", sentWords))
	assertShows(t, later, 80, 30, crossSessionLead+"bob"+crossSessionArrow+"planner")

	// Read back off disk, a send is stamped with the name the conversation has
	// when it arrives, and keeps it from then on.
	restored := NewDM("s1", "alex").SetSize(80, 30).Before([]core.Event{sendCall("s1", "sydney", long)}).WithName("bob")
	opened, hit = restored.openTool(peerRow(t, restored, "sent-00"))
	if !hit {
		t.Fatal("a click on the restored folded send reported no hit")
	}
	assertShows(t, opened, 80, 30, crossSessionLead+"alex"+crossSessionArrow+"sydney")
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

// A send by address is headed by who it reached, as the room heads it, and never
// by the socket path it was sent to.
func TestADMHeadsASendByAddressWithTheRecipientsName(t *testing.T) {
	d := sentDM(sentTo("s1", "uds:/tmp/cc-socks/40123.sock", sentWords, "sydney", "s2"))
	assertShows(t, d, 80, 30, crossSessionLead+"alex"+crossSessionArrow+"sydney")
	assertHides(t, d, 80, 30, "uds:")
}

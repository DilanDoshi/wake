package daemon

// The machine's other Claude sessions: a bare /list-agents asked of an idle
// agent as a probe, its reply kept from every client and restored history.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// listAgentsFixture is the recorded bare /list-agents: wf-gamma naming two idle
// peers, wf-beta and wf-alpha (2026-09-27-at-menu-findings.md §1).
const listAgentsFixture = "list-agents.jsonl"

// recordedPeers is what that recording's first listing names.
var recordedPeers = rpc.PeersFrame{
	Self: "wf-gamma",
	Peers: []core.Peer{
		{Name: "wf-beta", Dir: "/private/tmp/wake-rec/beta", State: "idle"},
		{Name: "wf-alpha", Dir: "/private/tmp/wake-rec/alpha", State: "idle"},
	},
}

// fakePeers answers a bare /list-agents with the recording's first listing turn -
// hooks, init, the listing and its num_turns:0 result - retagged to this
// session, and echoes anything else behind the recording's own init, so the
// daemon has seen list-agents advertised before it asks. With exitOnAsk it
// exits instead of answering, the agent that ends mid-ask.
func fakePeers(sid string, exitOnAsk bool) int {
	turn := retagSession(firstTurn(readFixture(listAgentsFixture)), sid)
	var initLine string
	for _, line := range turn {
		if strings.Contains(line, `"subtype":"init"`) {
			initLine = line
		}
	}
	for line := range stdinLines() {
		if strings.Contains(line, `"text":"/list-agents"`) {
			if exitOnAsk {
				return 0
			}
			emitLines(turn)
			continue
		}
		fmt.Println(initLine)
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	return 0
}

// firstTurn is a recording's lines up to and including its first result.
func firstTurn(lines []string) []string {
	for i, line := range lines {
		if strings.Contains(line, `"type":"result"`) {
			return lines[:i+1]
		}
	}
	return lines
}

// retagSession rewrites the recording's session id to sid, so the daemon reads
// the replay as this session's own.
func retagSession(lines []string, sid string) []string {
	var head struct {
		SessionID string `json:"session_id"`
	}
	if len(lines) == 0 || json.Unmarshal([]byte(lines[0]), &head) != nil || head.SessionID == "" {
		return lines
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, strings.ReplaceAll(line, head.SessionID, sid))
	}
	return out
}

// recordedListing is the recording's first listing turn, decoded.
func recordedListing(t *testing.T) []core.Event {
	t.Helper()
	var out []core.Event
	for _, line := range firstTurn(fixtureLinesFor(t, listAgentsFixture)) {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("decode %s: %v", listAgentsFixture, err)
		}
		out = append(out, evs...)
	}
	return out
}

// peersAgent is an idle agent whose last init advertised list-agents.
func peersAgent(id, name string) *agent {
	a := newAgent(id, name, "dev", "/repo/api", "", core.NewSession(core.Config{SessionID: id}), func() {})
	a.commands = []string{"model", listAgentsVerb}
	return a
}

func serverWith(t *testing.T, agents ...*agent) *server {
	t.Helper()
	s := newServer(tempSocket(t))
	for _, a := range agents {
		s.agents[a.id] = a
	}
	return s
}

// onlyQueued is the one stdin line queued for a, which must be the bare
// /list-agents probe; it then opens the probe's window the way apply does.
func onlyQueued(t *testing.T, a *agent) {
	t.Helper()
	if len(a.in) != 1 {
		t.Fatalf("%s has %d stdin lines queued, want exactly one /list-agents", a.name, len(a.in))
	}
	p := <-a.in
	if p.probe != peersProbe || p.frame.Kind != rpc.FrameSend || p.frame.Text != "/list-agents" {
		t.Fatalf("queued %+v (probe %d), want a bare /list-agents peers probe", p.frame, p.probe)
	}
	a.incProbe(p.probe)
}

// absorbListing feeds the recorded listing turn through fanOut's suppression
// step and fails on any listing frame that would reach a client.
func absorbListing(t *testing.T, s *server, a *agent, evs []core.Event) {
	t.Helper()
	for _, ev := range evs {
		listing := ev.Kind == core.KindAssistantText || ev.Kind == core.KindTurnEnd
		if got := s.absorbed(a, ev); got != listing {
			t.Fatalf("absorbed(%s %q) = %v, want %v", ev.Kind, ev.Text, got, listing)
		}
	}
}

// peersReplies drains c and returns every peers reply it was sent.
func peersReplies(t *testing.T, c *client) []rpc.PeersFrame {
	t.Helper()
	var out []rpc.PeersFrame
	for {
		select {
		case f := <-c.out:
			if f.Kind != rpc.FramePeersReply || f.Peers == nil {
				t.Fatalf("a client was sent %s, want only peers replies", f.Kind)
			}
			out = append(out, *f.Peers)
		default:
			return out
		}
	}
}

func samePeers(got, want rpc.PeersFrame) bool {
	got.AgeMS, want.AgeMS = 0, 0
	return reflect.DeepEqual(got, want)
}

// One /list-agents serves every client that asked while it was in flight, and
// each is answered once - asking twice is not a second answer.
func TestConcurrentAskersShareOneListAgentsAndEachIsAnsweredOnce(t *testing.T) {
	a := peersAgent(idAlpha, "sydney")
	s := serverWith(t, a)
	first, second := newClient(nil), newClient(nil)

	s.askPeers(first)
	s.askPeers(second)
	s.askPeers(first)
	if got := peersReplies(t, first); len(got) != 0 {
		t.Fatalf("answered before the agent replied: %+v", got)
	}
	onlyQueued(t, a)

	absorbListing(t, s, a, recordedListing(t))

	for i, c := range []*client{first, second} {
		got := peersReplies(t, c)
		if len(got) != 1 || !samePeers(got[0], recordedPeers) {
			t.Errorf("asker %d got %+v, want the recorded listing once", i, got)
		}
	}
	if a.pendingProbes[peersProbe] != 0 || len(a.in) != 0 {
		t.Errorf("the probe's window or queue did not close: pending %d, queued %d", a.pendingProbes[peersProbe], len(a.in))
	}
}

// With no agent that can answer now, the ask is answered at once rather than
// waited on, and nothing is written to any agent's stdin - not a working one,
// a blocked one, one running a tool, a parked one, one whose claude never
// advertised the command, or the manager.
func TestABusyFleetIsAnsweredAtOnceAndNoAgentIsAsked(t *testing.T) {
	working := peersAgent(idAlpha, "sydney")
	working.owed = true
	blocked := peersAgent(idBeta, "tokyo")
	blocked.pending = []ask{{id: "r1"}}
	tooling := peersAgent(testSessionID("d44d"), "lagos")
	tooling.tool = "Bash"
	parked := peersAgent(testSessionID("e55e"), "oslo")
	parked.parked, parked.ended = true, true
	unadvertised := peersAgent(testSessionID("f66f"), "lima")
	unadvertised.commands = nil
	manager := peersAgent(idGamma, core.ManagerName)
	fleet := []*agent{working, blocked, tooling, parked, unadvertised, manager}
	s := serverWith(t, fleet...)
	c := newClient(nil)

	s.askPeers(c)

	got := peersReplies(t, c)
	if len(got) != 1 || !samePeers(got[0], rpc.PeersFrame{}) {
		t.Fatalf("got %+v, want one empty answer at once", got)
	}
	for _, a := range fleet {
		if len(a.in) != 0 {
			t.Errorf("%s was sent /list-agents", a.name)
		}
	}
}

// A busy fleet is answered from the last listing, and says how old it is.
func TestABusyFleetIsAnsweredFromTheLastListingWithItsAge(t *testing.T) {
	a := peersAgent(idAlpha, "sydney")
	s := serverWith(t, a)
	c := newClient(nil)
	s.askPeers(c)
	onlyQueued(t, a)
	absorbListing(t, s, a, recordedListing(t))
	peersReplies(t, c)

	s.peers.at = s.peers.at.Add(-time.Minute)
	a.owed = true
	s.askPeers(c)

	got := peersReplies(t, c)
	if len(got) != 1 || !samePeers(got[0], recordedPeers) || got[0].AgeMS < time.Minute.Milliseconds() {
		t.Fatalf("got %+v, want the last listing, a minute old", got)
	}
	if len(a.in) != 0 {
		t.Error("a working agent was sent /list-agents")
	}
}

// A reply that opens like a listing but that this build cannot read is still
// the probe's own - suppressed, its end too - and answers empty rather than
// with rows it might have wrong, even over an older listing.
func TestAListingThisBuildCannotReadIsSuppressedAndAnswersEmpty(t *testing.T) {
	a := peersAgent(idAlpha, "sydney")
	s := serverWith(t, a)
	c := newClient(nil)
	s.askPeers(c)
	onlyQueued(t, a)
	absorbListing(t, s, a, recordedListing(t))
	peersReplies(t, c)

	s.askPeers(c)
	onlyQueued(t, a)
	garbled := "This session: sydney [abc123] (the name other sessions use to message it)\n\nA shape this build was never shown"
	if !s.absorbed(a, core.Event{Kind: core.KindAssistantText, Text: garbled}) {
		t.Fatal("an unreadable listing reached the conversation")
	}
	if !s.absorbed(a, core.Event{Kind: core.KindTurnEnd, Text: garbled, LocalCommand: true}) {
		t.Fatal("the unreadable listing's own end reached the conversation")
	}

	got := peersReplies(t, c)
	if len(got) != 1 || !samePeers(got[0], rpc.PeersFrame{}) {
		t.Fatalf("got %+v, want one empty answer", got)
	}
	if a.pendingProbes[peersProbe] != 0 {
		t.Errorf("the window did not close: %d", a.pendingProbes[peersProbe])
	}
}

// An ask whose agent ends before it answers is answered from the last listing,
// and the next ask goes to an agent that can still answer rather than joining
// one that never will.
func TestAnAskWhoseAgentEndsIsAnsweredAndTheNextIsAskedAfresh(t *testing.T) {
	gone := peersAgent(idAlpha, "sydney")
	s := serverWith(t, gone)
	c := newClient(nil)
	s.askPeers(c)
	onlyQueued(t, gone)

	s.peersGone(gone)
	if got := peersReplies(t, c); len(got) != 1 || !samePeers(got[0], rpc.PeersFrame{}) {
		t.Fatalf("got %+v, want one answer from the (empty) last listing", got)
	}

	next := peersAgent(idBeta, "tokyo")
	delete(s.agents, gone.id)
	s.agents[next.id] = next
	s.askPeers(c)
	onlyQueued(t, next)
}

// The window is per kind: an operator's own /list-agents on an agent with only
// a /model probe in flight is their answer, not the probe's.
func TestAnOperatorsOwnListAgentsReachesTheirConversation(t *testing.T) {
	a := peersAgent(idAlpha, "sydney")
	a.incProbe(modelProbe)
	s := serverWith(t, a)
	for _, ev := range recordedListing(t) {
		if s.absorbed(a, ev) {
			t.Fatalf("%s %q was suppressed with no /list-agents probe in flight", ev.Kind, ev.Text)
		}
	}
}

// The whole round trip over a real process: an idle agent's recorded listing
// answers the ask, and neither it nor its end reaches a client.
func TestAnIdleAgentListsTheMachinesSessionsAndNoClientSeesIt(t *testing.T) {
	replayingClaudeOnPath(t, "peers")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	// A real first turn, whose init advertises list-agents.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hello"})
	c.await("the first turn's end", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil && f.Event.Kind == core.KindTurnEnd
	})

	c.send(rpc.Frame{Kind: rpc.FramePeers})
	f := c.await("a peers reply", func(f rpc.Frame) bool { return f.Kind == rpc.FramePeersReply })
	if f.Peers == nil || !samePeers(*f.Peers, recordedPeers) {
		t.Fatalf("peers reply = %+v, want the recorded listing", f.Peers)
	}

	// A turn behind the probe, so its own end has had every chance to leak.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	c.awaitEvent(idAlpha, "after")
	for _, f := range c.seen {
		if f.Kind == rpc.FrameEvent && f.Event != nil && core.IsListAgentsReply(f.Event.Text) {
			t.Fatalf("the probe's listing reached a client as %s", f.Event.Kind)
		}
	}
}

// An agent whose session ends with the ask in flight still answers its asker -
// from the last listing, here none - rather than holding it for good.
func TestAnAgentThatEndsMidAskStillAnswersItsAsker(t *testing.T) {
	replayingClaudeOnPath(t, "peersgone")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hello"})
	c.await("the first turn's end", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil && f.Event.Kind == core.KindTurnEnd
	})

	c.send(rpc.Frame{Kind: rpc.FramePeers})
	f := c.await("a peers reply", func(f rpc.Frame) bool { return f.Kind == rpc.FramePeersReply })
	if f.Peers == nil || !samePeers(*f.Peers, rpc.PeersFrame{}) {
		t.Fatalf("peers reply = %+v, want the empty last listing", f.Peers)
	}
}

// A restored conversation shows neither the /list-agents Wake asked nor its
// reply, in either form the reply may take on disk, and keeps every real turn.
func TestHistoryDropsTheListAgentsProbe(t *testing.T) {
	listing := `This session: sydney [abc123] (the name other sessions use to message it)\n\nOther Claude sessions (1):\n  [idle]  ·  wf-beta  ·  /tmp/beta  ·  started 1m ago`
	plantTranscript(t, histID,
		userLine("run the tests"),
		assistantLine("the tests"),
		userLine("/list-agents"),
		assistantLine(listing),
		`{"type":"user","isSidechain":false,"message":{"role":"user","content":"<local-command-stdout>`+listing+`</local-command-stdout>"}}`,
		userLine("what next"),
		assistantLine("done"),
	)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var got []string
	for _, ev := range events {
		got = append(got, ev.Text)
	}
	if want := []string{"run the tests", "the tests", "what next", "done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("restored %q, want %q", got, want)
	}
}

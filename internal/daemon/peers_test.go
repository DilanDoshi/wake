package daemon

// The machine's other Claude sessions, asked of a bare one-shot claude: the
// fake one-shot is this test binary, reached through the same `claude` on PATH
// every fake agent is, and told apart by the --bare its argv carries.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// How the fake one-shot behaves, and where it reports.
const (
	fakeOneShotEnv      = "WAKE_FAKE_ONESHOT"         // one of the oneShot* modes; "" replays the listing
	fakeOneShotCountEnv = "WAKE_FAKE_ONESHOT_COUNT"   // a file each run appends one line to
	fakeOneShotGateEnv  = "WAKE_FAKE_ONESHOT_RELEASE" // a file a held run waits for
	fakeOneShotPIDEnv   = "WAKE_FAKE_ONESHOT_PIDFILE" // where a hung run writes its pid
)

const (
	oneShotEmpty   = "empty"   // the recorded no-peers listing
	oneShotGarbage = "garbage" // a result whose text is no listing
	oneShotFail    = "fail"    // the listing, then exit 1
	oneShotFlood   = "flood"   // the listing, then more than a listing's worth
	oneShotTurn    = "turn"    // the listing, from a result that ran a model turn
	oneShotHang    = "hang"    // never answer
	oneShotHeld    = "held"    // answer once the release file exists
	oneShotRenamed = "renamed" // the recorded listing holding a renamed peer's row
	oneShotOddRow  = "oddrow"  // the listing with one row of a shape never shown
)

// The recorded bare one-shot, its no-peers form and the line it was sent
// (2026-09-27-at-menu-findings.md §1a).
const (
	bareListingFixture = "list-agents-bare.jsonl"
	bareEmptyFixture   = "list-agents-bare-empty.jsonl"
	bareStdinFixture   = "../input/list-agents-bare.stdin.jsonl"
	bareRenamedFixture = "list-agents-bare-renamed.jsonl"
)

// recordedPeers is what the bare recording lists.
var recordedPeers = []core.Peer{
	{Name: "wf-beta", Dir: "/private/tmp/wake-rec/beta"},
	{Name: "wf-alpha", Dir: "/private/tmp/wake-rec/alpha"},
}

// fakeOneShot is the bare one-shot: it checks it was sent exactly the recorded
// line and then EOF, and answers as its mode says.
func fakeOneShot() int {
	if path := os.Getenv(fakeOneShotCountEnv); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(f, "run")
			_ = f.Close()
		}
	}
	if !sentTheRecordedLine() {
		return 2
	}
	switch os.Getenv(fakeOneShotEnv) {
	case oneShotEmpty:
		emitLines(readFixture(bareEmptyFixture))
	case oneShotGarbage:
		emitLines(readFixture(bareListingFixture)[:1])
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"num_turns":0,"result":"Sessions: two, probably."}`)
	case oneShotFail:
		emitLines(readFixture(bareListingFixture))
		return 1
	case oneShotFlood:
		emitLines(readFixture(bareListingFixture))
		fmt.Print(strings.Repeat("x", peersOutputBytes) + "\n")
	case oneShotTurn:
		lines := readFixture(bareListingFixture)
		last := len(lines) - 1
		lines[last] = strings.Replace(lines[last], `"num_turns":0`, `"num_turns":1`, 1)
		emitLines(lines)
	case oneShotHang:
		_ = os.WriteFile(os.Getenv(fakeOneShotPIDEnv), []byte(fmt.Sprint(os.Getpid())), 0o600)
		time.Sleep(lingerFor)
	case oneShotRenamed:
		emitLines(readFixture(bareRenamedFixture))
	case oneShotOddRow:
		for _, line := range readFixture(bareListingFixture) {
			fmt.Println(strings.ReplaceAll(line, "/private/tmp/wake-rec/alpha", "tmp/alpha"))
		}
	case oneShotHeld:
		for start := time.Now(); time.Since(start) < lingerFor; time.Sleep(10 * time.Millisecond) {
			if _, err := os.Stat(os.Getenv(fakeOneShotGateEnv)); err == nil {
				break
			}
		}
		emitLines(readFixture(bareListingFixture))
	default:
		emitLines(readFixture(bareListingFixture))
	}
	return 0
}

// sentTheRecordedLine reads stdin to EOF - so it proves stdin was closed - and
// reports whether it held exactly the recorded bare /list-agents.
func sentTheRecordedLine() bool {
	var got, want any
	in := strings.TrimSpace(readAll(os.Stdin))
	recorded := readFixture(bareStdinFixture)
	return len(recorded) == 1 && json.Unmarshal([]byte(in), &got) == nil &&
		json.Unmarshal([]byte(recorded[0]), &want) == nil && reflect.DeepEqual(got, want)
}

func readAll(f *os.File) string {
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}

// fakeAdvertises is an agent whose every turn opens with the bare recording's
// init - which advertises list-agents - and echoes what it was sent, so a
// /list-agents reaching its stdin would show.
func fakeAdvertises(sid string) int {
	initLine := readFixture(bareListingFixture)[0]
	var recorded struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal([]byte(initLine), &recorded) == nil && recorded.SessionID != "" {
		initLine = strings.ReplaceAll(initLine, recorded.SessionID, sid)
	}
	for line := range stdinLines() {
		fmt.Println(initLine)
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	return 0
}

// oneShotOnPath fakes claude for agents (script) and the one-shot (mode).
func oneShotOnPath(t *testing.T, script, mode string) {
	t.Helper()
	replayingClaudeOnPath(t, script)
	t.Setenv(fakeOneShotEnv, mode)
}

// runsCounted counts one-shot runs in a file of this test's own.
func runsCounted(t *testing.T) func() int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runs")
	t.Setenv(fakeOneShotCountEnv, path)
	return func() int {
		b, _ := os.ReadFile(path)
		return strings.Count(string(b), "run")
	}
}

// advertisingAgent spawns an agent and runs one turn, so its init has
// advertised list-agents.
func advertisingAgent(t *testing.T, c *testClient) {
	t.Helper()
	c.spawn(idAlpha, "sydney")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hello"})
	c.await("the first turn's end", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil && f.Event.Kind == core.KindTurnEnd
	})
}

// askPeersOf sends a FramePeers and returns the reply.
func askPeersOf(c *testClient) rpc.PeersFrame {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FramePeers})
	f := c.await("a peers reply", func(f rpc.Frame) bool { return f.Kind == rpc.FramePeersReply })
	if f.Peers == nil {
		c.t.Fatal("a peers reply carried no listing")
	}
	return *f.Peers
}

// peersRepliesSeen counts the peers replies c has read.
func peersRepliesSeen(c *testClient) int {
	n := 0
	for _, f := range c.seen {
		if f.Kind == rpc.FramePeersReply {
			n++
		}
	}
	return n
}

// The whole round trip: one run of the recorded one-shot lists the machine's
// sessions, and no agent's stdin ever carries the /list-agents.
func TestABareOneShotListsTheMachinesSessions(t *testing.T) {
	oneShotOnPath(t, "advertises", "")
	runs := runsCounted(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	advertisingAgent(t, c)

	if got := askPeersOf(c); !reflect.DeepEqual(got.Peers, recordedPeers) {
		t.Fatalf("peers = %+v, want the recorded %+v", got.Peers, recordedPeers)
	}
	if n := runs(); n != 1 {
		t.Errorf("%d one-shot runs, want 1", n)
	}

	// A turn behind the ask: anything written to the agent before it shows first.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	c.awaitEvent(idAlpha, "after")
	for _, f := range c.seen {
		if f.Kind == rpc.FrameEvent && f.Event != nil && strings.HasPrefix(f.Event.Text, "echo:") &&
			strings.Contains(f.Event.Text, "/list-agents") {
			t.Fatalf("an agent was sent /list-agents: %q", f.Event.Text)
		}
	}
}

// Every client that asks while a run is in flight shares it - one exec - and
// each is answered once, however often it asked.
func TestAsksDuringARunShareItAndEachIsAnsweredOnce(t *testing.T) {
	oneShotOnPath(t, "advertises", oneShotHeld)
	runs := runsCounted(t)
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv(fakeOneShotGateEnv, release)
	d := startDaemon(t)
	first, second := attach(t, d.socket), attach(t, d.socket)
	advertisingAgent(t, first)

	first.send(rpc.Frame{Kind: rpc.FramePeers})
	first.send(rpc.Frame{Kind: rpc.FramePeers})
	second.send(rpc.Frame{Kind: rpc.FramePeers})
	// Dispatch is serial per connection, so each status reply proves its
	// connection's asks were taken - all while the run is held.
	first.status()
	second.status()
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatalf("release the run: %v", err)
	}

	for i, c := range []*testClient{first, second} {
		c.await("a peers reply", func(f rpc.Frame) bool { return f.Kind == rpc.FramePeersReply })
		c.status() // anything sent beside the reply has arrived by now
		if n := peersRepliesSeen(c); n != 1 {
			t.Errorf("client %d was answered %d times, want once", i, n)
		}
	}
	if n := runs(); n != 1 {
		t.Errorf("%d one-shot runs for three asks during one, want 1", n)
	}
}

// With no agent whose claude advertised list-agents, nothing is run and the
// ask is answered empty at once - a held run would never answer.
func TestNoAdvertisedCommandRunsNothingAndAnswersAtOnce(t *testing.T) {
	oneShotOnPath(t, "", oneShotHeld)
	t.Setenv(fakeOneShotGateEnv, filepath.Join(t.TempDir(), "never"))
	runs := runsCounted(t)
	d := startDaemon(t)
	c := attach(t, d.socket)

	if got := askPeersOf(c); got.Peers != nil {
		t.Errorf("an empty fleet answered %+v", got.Peers)
	}
	c.spawn(idAlpha, "sydney") // "ready", and no init: nothing advertised
	c.awaitEvent(idAlpha, "ready")
	if got := askPeersOf(c); got.Peers != nil {
		t.Errorf("a fleet that advertised nothing answered %+v", got.Peers)
	}
	if n := runs(); n != 0 {
		t.Errorf("%d one-shot runs with list-agents advertised nowhere, want 0", n)
	}
}

// Every way a run can fail answers "no outside sessions", never a wrong row:
// the recorded empty listing, a text that is no listing, a non-zero exit or
// more output than a listing - even after a good listing - and no claude on
// PATH at all.
func TestAOneShotThatListsNobodyOrFailsAnswersEmpty(t *testing.T) {
	for _, mode := range []string{oneShotEmpty, oneShotGarbage, oneShotFail, oneShotFlood, "missing"} {
		t.Run(mode, func(t *testing.T) {
			oneShotOnPath(t, "", mode)
			if mode == "missing" {
				t.Setenv("PATH", t.TempDir())
			}
			s := newServer(tempSocket(t))
			// The empty listing is a good read; every other mode says why it failed.
			if peers, err := s.listPeers(t.Context()); peers != nil || (err == nil) != (mode == oneShotEmpty) {
				t.Errorf("listPeers = (%+v, %v), want none", peers, err)
			}
		})
	}
	// And the same harness does list the recording, so the cases above fail
	// for their own reason.
	oneShotOnPath(t, "", "")
	if peers, err := newServer(tempSocket(t)).listPeers(t.Context()); err != nil || !reflect.DeepEqual(peers, recordedPeers) {
		t.Fatalf("listPeers = (%+v, %v), want the recorded %+v", peers, err, recordedPeers)
	}
}

// A peer renamed after holding its name a while is listed with its former name
// in a column of its own (list-agents-bare-renamed.jsonl). That listing is still
// read, under the new name - it once refused the whole listing, so every
// conversation's menu lost every outside session.
func TestARenamedPeerStillListsTheMachinesSessions(t *testing.T) {
	oneShotOnPath(t, "", oneShotRenamed)
	want := []core.Peer{
		{Name: "wf beta", Dir: "/private/tmp/wake-rec/beta"},
		{Name: "wf-delta", Dir: "/private/tmp/wake-rec/alpha"},
	}
	if peers, err := newServer(tempSocket(t)).listPeers(t.Context()); err != nil || !reflect.DeepEqual(peers, want) {
		t.Fatalf("listPeers = (%+v, %v), want the recorded %+v", peers, err, want)
	}
}

// A row of a shape this build was never shown costs that one session, not the
// listing, and the log says how many were left out - never the row, which names
// the operator's sessions and directories.
func TestARowOfAnUnseenShapeIsLeftOutAndCounted(t *testing.T) {
	oneShotOnPath(t, "", oneShotOddRow)
	logged := lockedLog(t)

	peers, err := newServer(tempSocket(t)).listPeers(t.Context())
	if err != nil || !reflect.DeepEqual(peers, recordedPeers[:1]) {
		t.Fatalf("listPeers = (%+v, %v), want only %+v", peers, err, recordedPeers[:1])
	}
	if got := logged.String(); !strings.Contains(got, "left 1 of the machine's") || strings.Contains(got, "wake-rec") {
		t.Errorf("the log read %q, want the one row counted and not quoted", got)
	}
}

// A result that ran a model turn is not the recorded local command, whatever
// its text says - a claude that took /list-agents as a prompt - so it lists
// nobody, and this daemon runs no more one-shots: every later ask is answered
// empty at once, so any spend is bounded to that one turn.
func TestAOneShotThatRanAModelTurnListsNobodyAndIsNotRunAgain(t *testing.T) {
	oneShotOnPath(t, "advertises", oneShotTurn)
	runs := runsCounted(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	advertisingAgent(t, c)

	for i := range 2 {
		if got := askPeersOf(c); got.Peers != nil {
			t.Errorf("ask %d answered %+v from a model turn", i, got.Peers)
		}
	}
	if n := runs(); n != 1 {
		t.Errorf("%d one-shot runs, want 1: a one-shot that ran a model turn must not be run again", n)
	}
}

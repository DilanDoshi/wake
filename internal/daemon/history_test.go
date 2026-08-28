package daemon

// Reading a conversation back off claude's disk.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const histID = "11111111-2222-3333-4444-555555555555"

// plantTranscript writes lines as a transcript for id, under a projects
// directory this test owns, and points ProjectsDir at it.
//
// A projects directory already set is reused rather than replaced, so a test can
// plant transcripts for several ids under one tree - which is what a park book
// with more than one resumable record needs, since transcriptPath finds a file
// by name across every slug.
func plantTranscript(t *testing.T, id string, lines ...string) string {
	t.Helper()
	projects := os.Getenv("WAKE_PROJECTS")
	if projects == "" {
		projects = t.TempDir()
		t.Setenv("WAKE_PROJECTS", projects)
	}

	dir := filepath.Join(projects, "-Users-someone-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

func userLine(text string) string {
	return `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"` + text + `"}]}}`
}

func assistantLine(text string) string {
	return `{"type":"assistant","isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}`
}

// The conversation comes back, in order, with both sides of it.
func TestHistoryReadsTheConversationInOrder(t *testing.T) {
	plantTranscript(t, histID,
		userLine("what is left"),
		assistantLine("the tests"),
		userLine("run them"),
	)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("read %d events, want 3: %+v", len(events), events)
	}
	for i, want := range []string{"what is left", "the tests", "run them"} {
		if events[i].Text != want {
			t.Errorf("event %d is %q, want %q: a conversation read out of order is a conversation nobody can follow", i, events[i].Text, want)
		}
	}
}

// Every event carries the session it was read for.
//
// The on-disk key is `sessionId` where the stream's is `session_id`, so
// core.DecodeTranscriptLine reads neither and this is the one place that
// answers. An event with no session reaches App.observe as somebody else's.
func TestHistoryStampsTheSessionOnEveryEvent(t *testing.T) {
	plantTranscript(t, histID, userLine("hello"), assistantLine("hi"))

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events")
	}
	for i, ev := range events {
		if ev.SessionID != histID {
			t.Errorf("event %d carries session %q, want %q", i, ev.SessionID, histID)
		}
	}
}

// A session with no transcript is not an error.
//
// It is the ordinary state of a session that has never taken a turn, and of one
// started outside a directory claude tracks. The pane draws nothing, which is
// what it would have drawn anyway.
func TestHistoryOfASessionWithNoTranscriptIsEmptyAndNotAnError(t *testing.T) {
	plantTranscript(t, histID, userLine("hello"))

	const other = "99999999-8888-7777-6666-555555555555"
	events, err := History(other)
	if err != nil {
		t.Errorf("History of a session with no transcript failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("read %d events for a session with no transcript", len(events))
	}
}

// An id that is not a UUID reaches no filesystem at all.
//
// It is the same predicate maySpawn and the reaper apply, and it is what keeps
// a session id off the path: `../../etc/passwd` is not a session.
func TestHistoryRefusesAnIDThatIsNotOne(t *testing.T) {
	plantTranscript(t, histID, userLine("hello"))

	// A decoy where a traversal would land. Without it every probe resolves to
	// a path that does not exist *anyway*, so the test passes with the guard
	// deleted - which a review demonstrated. It asserted "no file was found"
	// where it claimed "no path was attempted".
	projects := os.Getenv("WAKE_PROJECTS")
	decoy := filepath.Join(projects, histID+".jsonl")
	if err := os.WriteFile(decoy, []byte(userLine("not this one")+"\n"), 0o600); err != nil {
		t.Fatalf("write decoy: %v", err)
	}

	for _, id := range []string{"", "notes", "../" + histID, histID + "/..", "-Users-someone-repo/" + histID} {
		if path, ok := transcriptPath(id); ok {
			t.Errorf("transcriptPath(%q) found %s: an id that is not a UUID must not reach a path", id, path)
		}
	}
}

// One unreadable line costs one turn, not the conversation.
func TestHistorySkipsALineItCannotDecode(t *testing.T) {
	plantTranscript(t, histID,
		userLine("before"),
		"{not json at all",
		assistantLine("after"),
	)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("read %d events around a malformed line, want 2: %+v", len(events), events)
	}
	if events[0].Text != "before" || events[1].Text != "after" {
		t.Errorf("the lines either side of a malformed one are %q and %q", events[0].Text, events[1].Text)
	}
}

// A long conversation comes back as its tail.
//
// The tail rather than the head: somebody reopening a conversation wants where
// it got to. The bound is a client bound as much as a wire one - every event
// handed over is rendered through glamour and re-wrapped on every resize.
func TestHistoryKeepsTheTailOfALongConversation(t *testing.T) {
	lines := make([]string, 0, historyEvents+50)
	for i := range historyEvents + 50 {
		lines = append(lines, userLine("line "+string(rune('a'+i%26))+strings.Repeat("x", i%3)))
	}
	// The last line is unmistakable, so "the tail" is checkable rather than
	// merely counted.
	lines = append(lines, assistantLine("the last thing said"))
	plantTranscript(t, histID, lines...)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) != historyEvents {
		t.Errorf("read %d events, want the bound of %d", len(events), historyEvents)
	}
	if last := events[len(events)-1]; last.Text != "the last thing said" {
		t.Errorf("the last event read is %q: a bound that kept the head would give back the beginning of a conversation somebody is returning to the end of", last.Text)
	}
}

// The records that exist only on disk never reach a conversation.
func TestHistoryDropsTheRecordsThatAreNotConversation(t *testing.T) {
	plantTranscript(t, histID,
		`{"type":"custom-title","customTitle":"spike"}`,
		`{"type":"queue-operation","operation":"enqueue"}`,
		userLine("the only turn"),
	)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) != 1 || events[0].Kind != core.KindUserText {
		t.Errorf("read %+v, want one user turn: a queue operation is a keystroke, not something anybody said", events)
	}
}

// A line longer than the scanner's bound costs that line and nothing else.
//
// The finding: bufio.Scanner *stops* on an over-long line, so the first draft
// returned an error and no history at all. A transcript carries whole file
// contents inside attachment records, so that is the ordinary case rather than
// an edge one.
func TestHistorySurvivesALineLongerThanTheBound(t *testing.T) {
	huge := `{"type":"user","isSidechain":false,"message":{"role":"user","content":[{"type":"text","text":"` +
		strings.Repeat("x", transcriptScanBytes+1024) + `"}]}}`
	plantTranscript(t, histID, userLine("before the big one"), huge, assistantLine("after it"))

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("read %d events around an over-long line, want the 2 either side: %+v", len(events), events)
	}
	if events[0].Text != "before the big one" || events[1].Text != "after it" {
		t.Errorf("the lines either side of an over-long one are %q and %q", events[0].Text, events[1].Text)
	}
}

// Nothing hands the raw line up.
//
// Raw is the undecoded bytes and nothing above internal/core may read it. It is
// also the whole of what makes an event large, and this is the one path that
// holds hundreds at once - and then puts them on a socket.
func TestHistoryDoesNotCarryTheUndecodedLine(t *testing.T) {
	plantTranscript(t, histID, userLine("hello"), assistantLine("hi"))

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	for i, ev := range events {
		if ev.Raw != nil {
			t.Errorf("event %d carries %d bytes of undecoded line", i, len(ev.Raw))
		}
	}
}

// A symlink named like a transcript is not one.
//
// os.Stat follows a final-component symlink, so a UUID-named link inside the
// projects directory would let a conversation be read from anywhere on the
// machine - and a FIFO would block the connection's goroutine on open.
func TestHistoryRefusesASymlinkedTranscript(t *testing.T) {
	plantTranscript(t, histID, userLine("the real one"))

	outside := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	if err := os.WriteFile(outside, []byte(userLine("somebody else's conversation")+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	const linked = "22222222-3333-4444-5555-666666666666"
	link := filepath.Join(os.Getenv("WAKE_PROJECTS"), "-Users-someone-repo", linked+".jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if path, ok := transcriptPath(linked); ok {
		t.Errorf("a symlink was accepted as a transcript: %s", path)
	}
	events, err := History(linked)
	if err != nil || len(events) != 0 {
		t.Errorf("History followed a symlink: %d events, err=%v", len(events), err)
	}
}

// sendHistory answers over a real socket, and it answers about the session it
// was asked about.
//
// The fourth vacuity a review found: no test named sendHistory and none drove
// FrameHistory through dispatch, so swallowing the read error, mis-addressing
// the reply, or answering with somebody else's conversation all passed. The
// only end-to-end coverage had one agent, where any id confusion is invisible.
func TestTheDaemonAnswersAHistoryFrameAboutTheSessionItNames(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	for id, text := range map[string]string{first: "the first conversation", second: "the second one"} {
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(userLine(text)+"\n"), 0o600); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}

	for id, want := range map[string]string{first: "the first conversation", second: "the second one"} {
		c.send(rpc.Frame{Kind: rpc.FrameHistory, SessionID: id})
		f := c.await("the history reply for "+id, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameHistoryReply && f.SessionID == id
		})
		if len(f.Events) != 1 {
			t.Fatalf("the reply for %s carries %d events, want 1", id, len(f.Events))
		}
		if f.Events[0].Text != want {
			t.Errorf("the reply for %s carries %q, want %q: two sessions and one answer is a conversation shown under the wrong name", id, f.Events[0].Text, want)
		}
		if f.Events[0].SessionID != id {
			t.Errorf("the reply's event is stamped %q, want the id the client asked with", f.Events[0].SessionID)
		}
	}
}

// A session with no transcript is answered, not ignored.
//
// The client is waiting, and a reply that never comes is a pane that stays
// empty for a reason nothing on any surface reports.
func TestTheDaemonAnswersEvenWithNoTranscript(t *testing.T) {
	fakeClaudeOnPath(t, "")
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	d := startDaemon(t)
	c := attach(t, d.socket)

	id := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameHistory, SessionID: id})
	f := c.await("the empty history reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameHistoryReply && f.SessionID == id
	})
	if len(f.Events) != 0 {
		t.Errorf("a session with no transcript was answered with %d events", len(f.Events))
	}
}

// The reply is bounded in bytes, not only in events.
//
// 400 events is not a size: one line can be a megabyte, so 400 of them is 400.
// A review measured 400 realistic file-reading turns encoding to a 39MB frame,
// past rpc's 16MB cap - which means the client's scanner refuses it, ReadFrames
// ends, and the socket ends with it. Not a pane missing its history: every
// session's events stopping.
func TestHistoryIsBoundedInBytesAsWellAsEvents(t *testing.T) {
	const each = 200 << 10
	lines := make([]string, 0, 60)
	for range 60 {
		lines = append(lines, userLine(strings.Repeat("x", each)))
	}
	plantTranscript(t, histID, lines...)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	total := 0
	for _, ev := range events {
		total += len(ev.Text)
	}
	if total > historyBytes+each {
		t.Errorf("a 12MB conversation came back as %d bytes in %d events, want at most about %d: a reply past rpc's frame cap ends the client's socket", total, len(events), historyBytes)
	}
	if len(events) == 0 {
		t.Error("the byte bound gave back nothing at all: the newest turn always fits")
	}
}

// A transcript that cannot be read is reported, not answered with silence.
func TestTheDaemonReportsATranscriptItCannotRead(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := uuid.NewString()
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(userLine("unreadable")+"\n"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}

	c.send(rpc.Frame{Kind: rpc.FrameHistory, SessionID: id})
	f := c.await("the refusal", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && f.SessionID == id
	})
	if !strings.Contains(f.Text, "transcript") {
		t.Errorf("the failure reads %q and does not say what could not be read", f.Text)
	}
}

// A rewound conversation comes back as its live branch, not the turns rewound
// away. The recorded fixture rewinds past a "remember 42" turn and its "7,42"
// answer; History must drop both and keep the branch written after the rewind.
// See core.ActiveBranch.
func TestHistoryDropsTheRewoundBranch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "rewind-tree.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	plantTranscript(t, histID, lines...)

	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var text strings.Builder
	for _, ev := range events {
		text.WriteString(ev.Text)
		text.WriteByte('\n')
	}
	got := text.String()
	if strings.Contains(got, "7,42") {
		t.Errorf("the rewound answer '7,42' came back on reopen:\n%s", got)
	}
	if strings.Contains(got, "Now also remember the number 42") {
		t.Errorf("the rewound question came back on reopen:\n%s", got)
	}
	if !strings.Contains(got, "List every number") {
		t.Errorf("the post-rewind question is missing from the reopened conversation:\n%s", got)
	}
}

// After a /clear, the transcript read is the one claude is writing now.
//
// /clear mints a new claude session id and leaves the old transcript under the
// old one, while Wake goes on addressing the agent by the id it spawned under.
// Reading by that id finds the *pre-clear* conversation and shows a cleared
// agent the context it no longer has.
func TestAClearedSessionReadsTheTranscriptClaudeIsWritingNow(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "s"))
	a := &agent{id: histID, name: "alex"}
	s.agents[histID] = a

	if got := s.transcriptID(histID); got != histID {
		t.Fatalf("before any event the transcript id is %q, want the agent's own", got)
	}

	const cleared = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	a.observe(core.Event{Kind: core.KindAssistantText, SessionID: cleared, Text: "after the clear"})
	if got := s.transcriptID(histID); got != cleared {
		t.Errorf("after an event carrying a new claude id the transcript id is %q, want %q: reading the old file shows a cleared agent the conversation it no longer has", got, cleared)
	}

	// The reset frame names the id that died and not its successor, so it only
	// forgets - the next event relearns.
	a.observe(core.Event{Kind: core.KindSessionReset, SessionID: cleared})
	if got := s.transcriptID(histID); got != histID {
		t.Errorf("after a reset the transcript id is %q, want the agent's own until the next event says otherwise", got)
	}
}

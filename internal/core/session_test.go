// Session tests drive a fake process, never a live model: a live model is
// slow, nondeterministic and billed per CI run. The fake is the standard Go
// helper-process pattern - the test binary re-execs itself with
// WAKE_WANT_HELPER=1 and impersonates `claude`, emitting canned stream-json.
//
// That makes this one of the files allowed to name Claude's frame types (with
// protocol.go, protocol_test.go, fixtures_test.go, encode_test.go and
// interrupt_test.go), and for the same narrow reason: something has to speak
// the wire to prove session.go never has to. session.go itself names no frame,
// key or subtype.
//
// Frames below are transcribed from testdata/stream/*.jsonl, abridged to the
// keys that carry meaning.

package core

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestHelperProcess is not a real test. It impersonates `claude`, emitting
// canned stream-json so session behavior can be tested without a live model.
//
// The frame order mirrors the fixtures: hook frames precede init, and init
// repeats per turn. init is a turn header, not a boot handshake - nothing
// may wait on it as a readiness signal. A real process spawned and never
// prompted emits hook frames and no init at all.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("WAKE_WANT_HELPER") != "1" {
		return
	}
	defer os.Exit(0)

	// Checked before the switch, and deliberately a different variable: the
	// orphan inherits WAKE_HELPER_SCRIPT=orphan from its parent, and
	// appending a replacement does not override it (Getenv returns the first
	// match). Reading the script first would fork bomb.
	if os.Getenv("WAKE_HELPER_LINGER") != "" {
		linger()
		return
	}

	// An in-group grandchild whenever a test wants to watch what becomes of
	// what the agent spawned. Started before the script, so it is running
	// before the session has seen a single frame, and its pid is recorded by
	// this process rather than by itself - a grandchild racing to write its
	// own pid against being killed is a flaky test, not a guard.
	if os.Getenv("WAKE_HELPER_PIDFILE") != "" {
		startOrphan(func(*exec.Cmd) {})
	}

	switch os.Getenv("WAKE_HELPER_SCRIPT") {
	case "orphan":
		spawnOrphanHoldingStderr()
	case "orphan-stdout":
		spawnOrphanHoldingStdout()
	case "idle":
		idleAfterATurn()
	case "eof-then-idle":
		eofThenIdle()
	case "deny":
		emitDeniedTurn()
	case "garbage":
		emitGarbageAmongGoodLines()
	case "bigline":
		emitOversizedLine()
	case "echo":
		echoEveryUserLine()
	case "replay":
		replayFixture()
	case "boom":
		// Exits 1 itself, so the deferred os.Exit(0) never runs.
		failOnStderr()
	case "interrupt-exit":
		// The recorded interrupt ending: exit 1, nothing on stderr. Lives in
		// interrupt_test.go with the rest of that story.
		interruptThenExitOne()
	case "exit-code":
		exitWithCode()
	default:
		emitTurns()
	}
}

// emitTurns replays the ordinary shape: hook frames, then a per-turn init,
// assistant text and result, repeated WAKE_HELPER_TURNS times.
func emitTurns() {
	turns := 1
	if n, err := strconv.Atoi(os.Getenv("WAKE_HELPER_TURNS")); err == nil && n > 0 {
		turns = n
	}
	for i := 0; i < turns; i++ {
		fmt.Println(`{"type":"system","subtype":"hook_started","hook_name":"SessionStart","session_id":"s1","uuid":"u1"}`)
		fmt.Println(`{"type":"system","subtype":"init","session_id":"s1","permissionMode":"default","uuid":"u2"}`)
		fmt.Printf(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"pong %d"}]}}`+"\n", i)
		fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"pong %d"}`+"\n", i)
	}
}

// emitDeniedTurn replays the recorded client-deny sequence: the ask, the
// refusal coming back as a failed tool result carrying the reason verbatim,
// and a turn that still ends successfully. Transcribed from
// testdata/stream/permission-deny-response.jsonl.
func emitDeniedTurn() {
	fmt.Println(`{"type":"system","subtype":"init","session_id":"s1","permissionMode":"default","uuid":"u1"}`)
	fmt.Println(`{"type":"control_request","request_id":"req-42","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"probe-note.txt","content":"ok"},"description":"probe-note.txt","tool_use_id":"toolu_1"}}`)
	fmt.Println(`{"type":"user","session_id":"s1","message":{"role":"user","content":[{"type":"tool_result","content":"spike: denied by probe","is_error":true,"tool_use_id":"toolu_1"}]},"tool_result_meta":[{"id":"toolu_1","non_execution_kind":"permission-rule"}],"tool_use_result":"Error: spike: denied by probe"}`)
	fmt.Println(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"That write was blocked."}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"num_turns":2,"session_id":"s1","result":"That write was blocked.","permission_denials":[{"tool_name":"Write","tool_use_id":"toolu_1"}]}`)
}

// emitGarbageAmongGoodLines puts an undecodable line between two good ones.
// A real process cannot emit this, but a truncated pipe or a future frame
// Wake mis-reads can produce the same effect, and the stream must survive it.
func emitGarbageAmongGoodLines() {
	fmt.Println(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"before"}]}}`)
	fmt.Println(`{"type":"assistant","session_id":"s1",`)
	fmt.Println(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"after"}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"after"}`)
}

// emitOversizedLine emits WAKE_HELPER_TURNS assistant frames whose text is
// WAKE_HELPER_BIGLINE bytes each, every one carrying a distinct first byte so
// a reader can tell which frame it is holding.
func emitOversizedLine() {
	// Said on stderr before the flood, so a session that ends on the scan
	// rather than on the process still has something to report about why.
	fmt.Fprintln(os.Stderr, mcpStartupComplaint)
	n, err := strconv.Atoi(os.Getenv("WAKE_HELPER_BIGLINE"))
	if err != nil || n <= 0 {
		n = 1
	}
	turns := 1
	if t, err := strconv.Atoi(os.Getenv("WAKE_HELPER_TURNS")); err == nil && t > 0 {
		turns = t
	}
	for i := 0; i < turns; i++ {
		text := strconv.Itoa(i) + strings.Repeat("x", n-1)
		fmt.Printf(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", text)
	}
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"big"}`)
}

// echoEveryUserLine mirrors the real process's turn loop: it stays alive
// across turns, answers each line written to stdin, and ends only when
// stdin closes. It echoes the line's exact bytes so a test can prove what
// arrived was one whole newline-delimited frame.
func echoEveryUserLine() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for sc.Scan() {
		fmt.Printf(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", sc.Text())
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"echoed"}`)
	}
}

// replayFixture impersonates an agent whose whole life is one recorded
// transcript: it prints every line of WAKE_HELPER_FIXTURE, WAKE_HELPER_CYCLES
// times over, and exits. Exiting is the point - stdout reaches EOF, the pump
// ends, finish reaps, and the events channel closes, which is a whole session
// lifecycle per spawn rather than one long-lived process.
//
// Used by the soak (soak_test.go, build tag `soak`), which needs many short
// real lifecycles rather than one long stream: a leak that bites on hour three
// accumulates per session as readily as per event. Nothing else in the file
// replays recorded bytes, so the other scripts stay hand-written frames.
//
// The path is absolute and arrives in this command's own environment, because
// twenty concurrent sessions replay twenty different files and os.Setenv is
// process-wide.
func replayFixture() {
	path := os.Getenv("WAKE_HELPER_FIXTURE")
	lines, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay:", err)
		os.Exit(1)
	}
	cycles := 1
	if n, err := strconv.Atoi(os.Getenv("WAKE_HELPER_CYCLES")); err == nil && n > 0 {
		cycles = n
	}
	// A fixture whose last line has no terminator would glue that line to the
	// first line of the next cycle and emit one frame that decodes as
	// garbage - a malformed line the soak would then report as a real finding
	// about Wake rather than about this helper.
	if len(lines) > 0 && lines[len(lines)-1] != '\n' {
		lines = append(lines, '\n')
	}
	// Buffered, and flushed once: a per-line write syscall would make this
	// script's own overhead the thing the soak measures.
	w := bufio.NewWriterSize(os.Stdout, 256*1024)
	for range cycles {
		if _, err := w.Write(lines); err != nil {
			// Wake closed the pipe. Nothing to report and nobody to report
			// it to; the session is already ending.
			return
		}
	}
	if err := w.Flush(); err != nil {
		return
	}
}

// orphanLifetime is how long the orphan holds its inherited stderr. It only
// has to outlast waitDelay by enough that a test timing out means the bound
// is gone, not that the machine was slow.
const orphanLifetime = 30 * time.Second

// mcpStartupComplaint is the kind of thing a real agent says on stderr while
// it still has something worth saying: not a startup rejection, just a
// subsystem failing. It is the only account of that failure anywhere, which
// is why losing it when the session ends on the wait bound is a real loss.
const mcpStartupComplaint = `MCP server "probe" failed to start: ENOENT`

// spawnOrphanHoldingStderr starts a grandchild that inherits this process's
// stderr and outlives it, then complains on stderr, runs an ordinary turn and
// exits. A stdio MCP server is the realistic version: the agent spawns it, the
// agent exits, the server keeps the descriptor.
//
// Its stdout is left nil, so it gets /dev/null and Wake's stdout pipe still
// reaches EOF. That is the point - the scan loop ends normally and the stall
// lands in cmd.Wait, where it is invisible.
//
// The orphan leaves this process's process group, so the group kill on the
// cancel path cannot be what ends this script. What is being tested here is
// the bound inside Wait, and it has to stay the only thing that can pass it.
func spawnOrphanHoldingStderr() {
	startOrphan(func(cmd *exec.Cmd) {
		cmd.Stderr = os.Stderr
		setProcessGroup(cmd)
	})
	fmt.Fprintln(os.Stderr, mcpStartupComplaint)
	emitTurns()
}

// spawnOrphanHoldingStdout is the harder half: the orphan holds the pipe Wake
// is *reading*, so the scan loop never sees EOF and never reaches cmd.Wait at
// all. No bound inside Wait can help. Only cancelling the context can.
//
// It leaves the process group for the same reason as above: a grandchild that
// dies with the group would end the scan by closing stdout, and closeOnCancel
// - the thing this script exists to test - would stop being what ends it.
func spawnOrphanHoldingStdout() {
	startOrphan(func(cmd *exec.Cmd) {
		cmd.Stdout = os.Stdout
		setProcessGroup(cmd)
	})
	emitTurns()
}

// idleAfterATurn runs one ordinary turn and then waits on stdin, which is what
// a real agent between turns looks like. Tests about how a session *ends* need
// one that has not ended on its own.
func idleAfterATurn() {
	emitTurns()
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// eofThenIdle runs a turn, closes its own stdout, and stays alive.
//
// That is the one steady state in which cancelling reaches nothing but
// cmd.Cancel: the scan has ended at EOF, so pump's kill branch is skipped
// (scanErr is nil) and closeOnCancel has already retired on scanDone, while
// finish sits in cmd.Wait on a process that has not exited. An agent that
// closes stdout and keeps working is unusual; an agent still running with
// nothing left to say is not, and this is the shortest way to that state.
func eofThenIdle() {
	emitTurns()
	_ = os.Stdout.Close()
	time.Sleep(orphanLifetime)
}

// startOrphan spawns a grandchild that outlives its parent. Unconfigured it
// stays in the agent's process group and holds none of Wake's pipes, so
// nothing about the session's I/O depends on it and the only question it
// asks is whether Wake reaped it.
func startOrphan(configure func(*exec.Cmd)) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
	cmd.Env = append(os.Environ(), "WAKE_WANT_HELPER=1", "WAKE_HELPER_LINGER=1")
	configure(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "orphan:", err)
		return
	}
	if path := os.Getenv("WAKE_HELPER_PIDFILE"); path != "" {
		_ = os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	}
}

// linger is the orphan: it writes nothing and holds whatever it inherited far
// longer than any bound Wake sets.
func linger() {
	time.Sleep(orphanLifetime)
}

// failOnStderr impersonates the two startup rejections the spike recorded: a
// missing --verbose, and a --session-id already in use. Both exit 1 with the
// reason on stderr and zero bytes on stdout, so nothing about the failure is
// visible on the JSON channel.
func failOnStderr() {
	fmt.Fprintln(os.Stderr, "Error: When using --print, --output-format=stream-json requires --verbose")
	os.Exit(1)
}

func fakeExec(ctx context.Context, name string, args ...string) *exec.Cmd {
	cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], cs...)
	cmd.Env = append(os.Environ(), "WAKE_WANT_HELPER=1")
	return cmd
}

func withFakeExec(t *testing.T) {
	t.Helper()
	orig := execCommand
	execCommand = fakeExec
	t.Cleanup(func() { execCommand = orig })
}

func startFake(t *testing.T, cfg Config) *Session {
	t.Helper()
	withFakeExec(t)
	s := NewSession(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

func drain(s *Session) []EventKind {
	var kinds []EventKind
	for ev := range s.Events() {
		kinds = append(kinds, ev.Kind)
	}
	return kinds
}

// drainEvents is drain when the test needs the events themselves.
func drainEvents(s *Session) []Event {
	var evs []Event
	for ev := range s.Events() {
		evs = append(evs, ev)
	}
	return evs
}

func TestSessionStreamsEventsUntilExit(t *testing.T) {
	s := startFake(t, Config{SessionID: "s1", Name: "alex", Dir: t.TempDir()})

	kinds := drain(s)
	want := []EventKind{KindSystem, KindSystem, KindAssistantText, KindTurnEnd}
	if len(kinds) != len(want) {
		t.Fatalf("got kinds %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("got kinds %v, want %v", kinds, want)
		}
	}
}

// The regression that matters: a result frame ends a turn, not the process.
// A session that tore itself down on the first one would kill live agents.
func TestSessionSurvivesMultipleTurnEnds(t *testing.T) {
	t.Setenv("WAKE_HELPER_TURNS", "3")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	kinds := drain(s)

	var ends, textAfterFirstEnd int
	seenEnd := false
	for _, k := range kinds {
		switch k {
		case KindTurnEnd:
			ends++
			seenEnd = true
		case KindAssistantText:
			if seenEnd {
				textAfterFirstEnd++
			}
		}
	}
	if ends != 3 {
		t.Fatalf("got %d turn ends, want 3\nkinds: %v", ends, kinds)
	}
	if textAfterFirstEnd != 2 {
		t.Fatalf("stream stopped at the first turn end: %d later texts, want 2\nkinds: %v", textAfterFirstEnd, kinds)
	}
	if kinds[len(kinds)-1] != KindTurnEnd {
		t.Fatalf("last kind = %q, want the final turn end", kinds[len(kinds)-1])
	}
}

// A denied tool is not a failed turn. Both denial paths end with
// subtype "success" and is_error false on the result frame, with the tool
// listed in result.permission_denials - so the turn end is an ordinary turn
// end. Anything that reads a denial as an errored turn misreports agent
// state, and at 20 agents that is the state that matters most.
func TestDeniedToolStillEndsTheTurnNormally(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "deny")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir(), PermissionMode: "manual"})

	var asked, refused bool
	var kinds []EventKind
	for ev := range s.Events() {
		kinds = append(kinds, ev.Kind)
		switch ev.Kind {
		case KindPermissionRequest:
			asked = true
			if ev.RequestID != "req-42" {
				t.Errorf("RequestID = %q, want req-42 - the only correlator there is", ev.RequestID)
			}
		case KindToolResult:
			if ev.Tool != nil && ev.Tool.IsError && strings.Contains(ev.Text, "denied by probe") {
				refused = true // the reason reached the model verbatim
			}
		case KindUnknown:
			t.Errorf("frame decoded as unknown: %q", ev.Text)
		}
	}

	if !asked {
		t.Fatalf("no permission request decoded\nkinds: %v", kinds)
	}
	if !refused {
		t.Fatalf("the deny reason never reached the model\nkinds: %v", kinds)
	}
	if kinds[len(kinds)-1] != KindTurnEnd {
		t.Fatalf("last kind = %q, want an ordinary turn end", kinds[len(kinds)-1])
	}
}

// A permission request arrives with no session_id on the wire - the only
// frame type in the recorded corpus that does not carry one - so the pipe it
// came out of is the only thing that knows which agent is blocked. Session is
// the only component that knows the pipe.
//
// The discrimination is in the two ids being different. The helper's frames
// say "s1" and the session was spawned as "wake-1", so an event carrying
// "wake-1" was stamped here and an event carrying "s1" was read off the
// frame. A test that spawned as "s1" would pass either way.
//
// Both halves matter and the second is the one with a trap behind it: /clear
// mints a new session id mid-process, and from that point the frames are the
// authority. A stamp that overwrote a frame's own id would re-label every
// post-clear event with the id the session was spawned under.
func TestPermissionRequestIsAttributedToTheSessionItArrivedOn(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "deny")
	s := startFake(t, Config{SessionID: "wake-1", Dir: t.TempDir(), PermissionMode: "manual"})

	var asked bool
	for _, ev := range drainEvents(s) {
		if ev.SessionID == "" {
			t.Errorf("%s event left core with no session attribution at all", ev.Kind)
		}
		if ev.Kind != KindPermissionRequest {
			// Everything else names its own session on the wire, and that
			// name wins: overwriting it would break /clear.
			if ev.SessionID != "s1" {
				t.Errorf("%s event = %q, want the id the frame carried (s1)", ev.Kind, ev.SessionID)
			}
			continue
		}
		asked = true
		if ev.SessionID != "wake-1" {
			t.Errorf("permission request = %q, want wake-1 - the highest-priority attention trigger is unroutable without it", ev.SessionID)
		}
	}
	if !asked {
		t.Fatal("no permission request decoded, so this test proved nothing")
	}
}

func TestSessionEventsChannelClosesOnExit(t *testing.T) {
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})
	drain(s)

	// A second drain must not block: the channel is closed, not merely empty.
	select {
	case _, open := <-s.Events():
		if open {
			t.Fatal("events channel still open after process exit")
		}
	case <-time.After(time.Second):
		t.Fatal("reading a closed events channel blocked")
	}
}

func TestSendBeforeStartIsAnError(t *testing.T) {
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Send("hello", nil); err == nil {
		t.Fatal("want error sending to an unstarted session, got nil")
	}
}

func TestPermissionAnswerBeforeStartIsAnError(t *testing.T) {
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.AllowTool("req-1", nil); err == nil {
		t.Fatal("want error allowing on an unstarted session, got nil")
	}
	if err := s.DenyTool("req-1", "no"); err == nil {
		t.Fatal("want error denying on an unstarted session, got nil")
	}
}

// nopWriteCloser lets a test read what the session wrote to stdin without
// a process in the way.
type nopWriteCloser struct{ buf *bytes.Buffer }

func (w nopWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w nopWriteCloser) Close() error                { return nil }

// blockingWriteCloser stands in for a child that has stopped draining stdin:
// the pipe buffer is full and the write does not return.
type blockingWriteCloser struct {
	entered chan struct{}
	release chan struct{}
}

func (w blockingWriteCloser) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

func (w blockingWriteCloser) Close() error { return nil }

func TestDenyToolWritesAControlResponseCorrelatedByRequestID(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	if err := s.DenyTool("req-42", "spike: denied by probe"); err != nil {
		t.Fatalf("DenyTool: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		`"type":"control_response"`,
		`"request_id":"req-42"`,
		`"behavior":"deny"`,
		`"message":"spike: denied by probe"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdin missing %s\ngot: %s", want, got)
		}
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("got %d newlines, want 1 - stdin frames are newline delimited", n)
	}
}

func TestAllowToolWritesAnAllowResponse(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	if err := s.AllowTool("req-42", nil); err != nil {
		t.Fatalf("AllowTool: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, `"behavior":"allow"`) {
		t.Errorf("stdin missing an allow behavior\ngot: %s", got)
	}
}

func TestBuildArgsCarriesIdentityAndMode(t *testing.T) {
	s := NewSession(Config{
		SessionID:      "uuid-1",
		Name:           "sydney",
		PermissionMode: "manual",
		Model:          "opus",
		Effort:         "max",
		MaxBudgetUSD:   "0.25",
		FallbackModel:  "sonnet,haiku",
	})
	args, err := s.buildArgs()
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	joined := " " + strings.Join(args, " ") + " "

	for _, want := range []string{
		" --print ", " --input-format stream-json ", " --output-format stream-json ",
		// Not optional: without --verbose the process exits 1, and without
		// --permission-prompt-tool stdio every ask is auto-denied unseen.
		" --verbose ", " --permission-prompt-tool stdio ",
		" --session-id uuid-1 ", " --name sydney ", " --permission-mode manual ",
		" --model opus ", " --effort max ", " --brief ",
		// Both documented "only works with --print", which is the first flag in
		// this same list. A chain is one argv word, separators included.
		" --max-budget-usd 0.25 ", " --fallback-model sonnet,haiku ",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\ngot: %s", want, joined)
		}
	}
}

// TestBuildArgsLeavesOffWhatNobodyChose is the other half of every optional flag
// in buildArgs, and it is the half that has never had a test.
//
// "" means "Wake chose nothing" for all five, and the flag is then absent so
// claude applies its own default. A build that emitted `--max-budget-usd ""`
// instead would cap every unbudgeted session at an amount claude has to parse,
// and nothing downstream would say so.
func TestBuildArgsLeavesOffWhatNobodyChose(t *testing.T) {
	s := NewSession(Config{SessionID: "uuid-1"})
	args, err := s.buildArgs()
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	joined := " " + strings.Join(args, " ") + " "

	for _, absent := range []string{
		"--name", "--model", "--effort", "--max-budget-usd", "--fallback-model",
		"--mcp-config", "--strict-mcp-config", "--tools", "--append-system-prompt",
	} {
		if strings.Contains(joined, " "+absent+" ") {
			t.Errorf("args carry %q for a Config that chose none: an unset field leaves the flag off entirely\ngot: %s", absent, joined)
		}
	}
	// And the floor, so a buildArgs that returned nothing would not pass the
	// loop above by emitting no flags at all.
	if !strings.Contains(joined, " --print ") {
		t.Fatal("args carry no --print, so this test asserted nothing about what was left off")
	}
}

// --- beyond the brief -------------------------------------------------------

// Send has to reach a live process, not just a buffer, and the process has
// to still be there for the turn after. The helper echoes the exact bytes it
// read back as assistant text, so this also proves the frame arrived whole
// and newline-delimited rather than split or concatenated.
//
// It is the only test that exercises the full supervision loop: spawn, send,
// read, send again, stop.
func TestSendReachesTheProcessAcrossTurnsAndStopEndsIt(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "echo")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	for _, text := range []string{"ping", "ping again"} {
		if err := s.Send(text, nil); err != nil {
			t.Fatalf("Send(%q): %v", text, err)
		}
		echoed := waitForKind(t, s, KindAssistantText)
		if !strings.Contains(echoed, `"text":"`+text+`"`) {
			t.Fatalf("process read %q, want a user frame carrying %q", echoed, text)
		}
		if strings.Contains(echoed, "\n") {
			t.Errorf("process read more than one line at once: %q", echoed)
		}
	}

	// Closing stdin is what ends a real process; the events channel closes
	// on the EOF that follows, not on either turn end above.
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-drainedAsync(s):
	case <-time.After(5 * time.Second):
		t.Fatal("events channel never closed after Stop")
	}
	if err := s.Stop(); err != nil {
		t.Errorf("second Stop: %v, want nil - stopping twice is not an error", err)
	}
	if err := s.Send("after stop", nil); err == nil {
		t.Error("want an error sending to a stopped session, got nil")
	}
}

// Nothing that outlives a claude process may stall the session that
// supervised it.
//
// Wake's stderr writer is not an *os.File, so os/exec runs a copying
// goroutine over a pipe, and cmd.Wait reads that pipe to EOF - which arrives
// only when the last descriptor for it closes, including any the agent handed
// to something it spawned. Unbounded, this is the worst state in the file: no
// events channel close, a leaked pump goroutine and a leaked consumer per
// attached client, and a session that looks healthy and idle while holding a
// live-cap slot.
func TestAnOrphanHoldingStderrCannotStallTheSession(t *testing.T) {
	base := runtime.NumGoroutine()
	t.Setenv("WAKE_HELPER_SCRIPT", "orphan")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	select {
	case <-drainedAsync(s):
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("events channel never closed: cmd.Wait is still reading a pipe an orphan holds open, and the pump goroutine has leaked with every consumer of Events()")
	}
	// The process itself exited cleanly; Wake gave up on its stderr. Saying
	// so beats reporting a healthy exit for a session whose output was cut.
	err := s.Err()
	if err == nil {
		t.Fatal("Err = nil, want a report that the session's output was held open past the bound")
	}
	// Ending on the bound costs the *trailing* bytes, not the ones already
	// captured. This is the one exit where stderr is the whole account of what
	// went wrong, so dropping it here inverts the reason it is captured.
	if !strings.Contains(err.Error(), mcpStartupComplaint) {
		t.Errorf("Err = %v, want the stderr the process did manage to write", err)
	}
	// A closed channel says the teardown ran; only this says nothing was left
	// behind running. The orphan still holds the descriptor for another 28
	// seconds, so anything still waiting on it would show up here.
	waitForGoroutines(t, base)
}

// The other half, and the one Task 6 will depend on: when the orphan holds
// the pipe Wake is *reading*, the scan loop never sees EOF and cmd.Wait is
// never even reached, so no bound inside Wait can help. Cancelling the
// context force-closes the pipes after the same delay, which is what makes
// cancellation an actual kill rather than a signal into the void.
func TestCancellingTheContextEndsASessionAnOrphanHoldsOpen(t *testing.T) {
	base := runtime.NumGoroutine()
	t.Setenv("WAKE_HELPER_SCRIPT", "orphan-stdout")
	withFakeExec(t)

	ctx, cancel := context.WithCancel(context.Background())
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The turn arrives, and then the stream simply stops: the process is gone
	// but its stdout pipe is not.
	if got := waitForKind(t, s, KindAssistantText); got != "pong 0" {
		t.Fatalf("assistant text = %q, want pong 0", got)
	}
	done := drainedAsync(s)
	select {
	case <-done:
		t.Fatal("events closed on its own - the orphan is not holding stdout, so this test proves nothing")
	case <-time.After(500 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("cancelling the context did not end the session: there is no way to kill a wedged agent")
	}
	if err := s.Err(); err == nil {
		t.Error("Err = nil, want the reason the stream ended early")
	}
	waitForGoroutines(t, base)
}

// The third route to the same bug, reached through the events channel rather
// than through either pipe.
//
// Events() is buffered and finite. A consumer that stops reading fills it, and
// the pump then parks on a *channel send* - not in Scan. closeOnCancel takes
// stdout away, which the pump is not reading, so cancellation reaches nothing:
// the scan never resumes, scanDone never closes, finish never runs, Events()
// never closes and Err() stays nil. Same shape as the other two: a session
// that looks healthy, holds a live-cap slot and cannot be killed.
//
// The test is deliberately consumer-free from Start to the assertion. A test
// that filled the buffer and then read from it would unpark the pump by
// itself and pass against the bug; the whole discrimination is in never
// touching the channel, and in proving the park with a stack rather than
// inferring it from a full buffer (which cannot be told apart from an idle
// scan).
func TestCancellingEndsASessionWhosePumpIsParkedOnTheEventsChannel(t *testing.T) {
	base := runtime.NumGoroutine()
	// Four events a turn, so this overruns the buffer several times over.
	t.Setenv("WAKE_HELPER_TURNS", strconv.Itoa(eventBuffer))
	withFakeExec(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	parked := waitForParkedSender(t)
	if n := len(s.Events()); n != eventBuffer {
		t.Fatalf("events buffered = %d, want the full %d - the pump parked somewhere this test does not mean", n, eventBuffer)
	}

	cancel()

	// Err is written by finish, which runs only after the pump has actually
	// exited, and reading it consumes nothing.
	waitForErr(t, s, parked)

	// Only now, and only to prove the channel was closed rather than left open
	// with a dead session behind it.
	select {
	case <-drainedAsync(s):
	case <-time.After(5 * time.Second):
		t.Fatal("events channel never closed, so every consumer ranging over it is still blocked")
	}
	waitForGoroutines(t, base)
}

// The call an operator makes when an agent is wedged must not be the call
// that a wedged agent blocks.
//
// A child that stops draining stdin fills the 64KB pipe buffer and then
// blocks the write. Holding the state mutex across that would take Stop, Err
// and the pump's own teardown down with it - turning a stalled session into
// an unkillable one.
func TestStopIsNotBlockedByAWedgedWrite(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	s := NewSession(Config{SessionID: "s1"})
	s.stdin = blockingWriteCloser{entered: entered, release: release}

	go func() { _ = s.Send("a message the agent will never read", nil) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Send never reached the writer")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop blocked behind an in-flight write - a wedged agent is exactly when an operator reaches for Stop")
	}

	// Reached only once Stop has returned, so the state mutex is provably
	// free rather than merely usually free.
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil", err)
	}
}

// Two frames must never interleave on stdin: half of one inside the other is
// a line claude cannot parse, and an unparseable line kills the process.
func TestConcurrentSendsDoNotInterleaveFrames(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "echo")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	const senders = 8
	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Send(strings.Repeat(strconv.Itoa(i), 4096), nil); err != nil {
				t.Errorf("Send: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// The helper echoes each line it read. Every one must still be a single
	// well-formed frame carrying one sender's text and nothing else.
	for i := 0; i < senders; i++ {
		line := waitForKind(t, s, KindAssistantText)
		if strings.Count(line, `"type":"user"`) != 1 {
			t.Fatalf("stdin frames interleaved: %.120s...", line)
		}
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)
}

// The fourth route to an unkillable session, and the only one that needs no
// grandchild and no stalled consumer: logging.
//
// log serializes on a package mutex and writes to whatever the process
// installed. A pump that called log.Printf directly would park inside that
// write - and then closeOnCancel takes away a stdout it is not reading, send's
// ctx case is never reached, and finish never runs. Wake's own stderr being a
// pipe nobody drains is the realistic way to get there, which is the same
// mistake that produced the first route in this file.
//
// So: a log output that blocks forever, a line the decoder rejects, and the
// session still has to end. Nothing here cancels anything - the point is that
// an ordinary session survives a wedged logger, not that it can be killed
// afterwards.
func TestASessionEndsEvenWhenLoggingIsWedged(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	// Cleanups run LIFO, and the order is load-bearing: whoever is stuck in
	// the write holds log's package mutex, so releasing has to come before
	// restoring the output or SetOutput deadlocks the cleanup. That is not
	// hypothetical - it is what this test does to itself when it fails.
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	t.Cleanup(func() { close(release) })
	log.SetOutput(blockingWriteCloser{entered: entered, release: release})

	t.Setenv("WAKE_HELPER_SCRIPT", "garbage")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	// Proves the write is genuinely stuck before anything is concluded from
	// the session ending: a logger that returned quickly would prove nothing.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing ever reached the blocked log writer, so this test never entered the state it is about")
	}

	select {
	case <-drainedAsync(s):
	case <-time.After(10 * time.Second):
		t.Fatal("the session never ended while its logger was blocked: the pump is parked inside log, holding a live-cap slot with no way to be killed")
	}
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil - a wedged logger is not a failed session", err)
	}
}

// A malformed line is skipped, never fatal: one bad frame must not cost a
// session its remaining output.
func TestMalformedLineIsSkippedNotFatal(t *testing.T) {
	// The event it emits is KindUnknown, which the DM renders as the empty
	// string - so without a log a decoder bug is invisible to everyone,
	// including whoever is trying to debug it.
	var logged safeBuffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	t.Setenv("WAKE_HELPER_SCRIPT", "garbage")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	var texts []string
	var unknown int
	for _, ev := range drainEvents(s) {
		if ev.Kind == KindUnknown {
			unknown++
			continue
		}
		if ev.Kind == KindAssistantText {
			texts = append(texts, ev.Text)
		}
	}
	if unknown != 1 {
		t.Errorf("got %d undecodable lines reported, want 1", unknown)
	}
	if len(texts) != 2 || texts[0] != "before" || texts[1] != "after" {
		t.Fatalf("assistant texts = %v, want [before after] - the stream stopped at the bad line", texts)
	}
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil - a bad line is not a failed session", err)
	}
	// Polled, because the pump hands diagnostics to a logger goroutine rather
	// than writing them itself - it must not block on log, so the line can
	// still be in flight when the session is already over.
	waitForLogged(t, &logged, "s1", "decode")
}

// bufio.Scanner's 64KB default does not fail loudly on a longer line - it
// truncates, and a truncated frame becomes a decode error instead of the
// tool result it was. The largest recorded frame is only ~15KB, so the
// corpus alone cannot catch this; the line here is one recorded frame past
// the default, which is still far short of what a real tool result can be.
func TestPumpCarriesALineLongerThanTheScannerDefault(t *testing.T) {
	size := bufio.MaxScanTokenSize + longestRecordedLine(t)
	t.Setenv("WAKE_HELPER_SCRIPT", "bigline")
	t.Setenv("WAKE_HELPER_BIGLINE", strconv.Itoa(size))
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	var got int
	for _, ev := range drainEvents(s) {
		if ev.Kind == KindAssistantText {
			got = len(ev.Text)
		}
		if ev.Kind == KindUnknown {
			t.Fatalf("the long line did not survive the scanner: %s", ev.Text)
		}
	}
	if got != size {
		t.Errorf("assistant text = %d bytes, want %d", got, size)
	}
}

// The scanner slides its buffer to the front to make room for the next
// token, overwriting where earlier ones sat. An event that kept a reference
// into that buffer instead of a copy is silently rewritten by a later line.
//
// The sizing is the whole test. Small frames all fit in one buffer fill, each
// token gets its own untouched region, and an aliasing bug is invisible - an
// earlier draft of this test used the ordinary helper and passed against a
// deliberately aliasing decoder. Half the buffer plus one byte means two
// frames cannot sit in it at once, which forces the slide, whatever
// initialLineBytes is set to later.
func TestEventRawSurvivesTheScannersReusedBuffer(t *testing.T) {
	const frames = 3
	t.Setenv("WAKE_HELPER_SCRIPT", "bigline")
	t.Setenv("WAKE_HELPER_BIGLINE", strconv.Itoa(initialLineBytes/2+1))
	t.Setenv("WAKE_HELPER_TURNS", strconv.Itoa(frames))
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	var checked int
	for _, ev := range drainEvents(s) {
		if ev.Kind != KindAssistantText {
			continue
		}
		if !bytes.Contains(ev.Raw, []byte(`"text":"`+ev.Text+`"`)) {
			t.Errorf("Raw for the frame starting %.8q is another line's bytes: %.40s...", ev.Text, ev.Raw)
		}
		checked++
	}
	if checked != frames {
		t.Fatalf("checked %d assistant frames, want %d", checked, frames)
	}
}

// A startup rejection - a missing --verbose, a session id already in use -
// exits 1 with the reason on stderr and nothing on stdout. Read only the
// JSON channel and a session like that is indistinguishable from one that
// ran and finished.
func TestAbnormalExitIsReportedWithItsStderr(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "boom")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	if kinds := drain(s); len(kinds) != 0 {
		t.Fatalf("got events %v, want none - the process wrote nothing to stdout", kinds)
	}
	err := s.Err()
	if err == nil {
		t.Fatal("Err = nil after a process exited 1 with an error on stderr")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("Err = %v, want the exit status", err)
	}
	if !strings.Contains(err.Error(), "requires --verbose") {
		t.Errorf("Err = %v, want the reason claude printed on stderr", err)
	}
	if !strings.Contains(err.Error(), "s1") {
		t.Errorf("Err = %v, want the session id - a fleet log needs to know which one died", err)
	}
}

// exitError has three things to say and they are not alternatives: which
// session, how it ended, and what it said on the way out. The wait-delay
// ending is the one where the third matters most - the process is gone and
// stderr is the only record of why - so that branch must not be the branch
// that returns early and drops it.
func TestExitErrorReportsStderrAlongsideEveryEnding(t *testing.T) {
	const tail = "Error: When using --print, --output-format=stream-json requires --verbose"

	for _, tc := range []struct {
		name    string
		waitErr error
		want    []string
	}{
		{
			name:    "wait delay",
			waitErr: fmt.Errorf("exec: WaitDelay expired before I/O complete: %w", exec.ErrWaitDelay),
			want:    []string{"s1", "held its output open", tail},
		},
		{
			name:    "ordinary failure",
			waitErr: errors.New("exit status 1"),
			want:    []string{"s1", "exit status 1", tail},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := exitError("s1", tc.waitErr, tail+"\n")
			if err == nil {
				t.Fatal("exitError = nil for a non-nil wait error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("exitError = %v, want it to carry %q", err, want)
				}
			}
			if !errors.Is(err, tc.waitErr) {
				t.Errorf("exitError = %v, want the wait error still unwrappable out of it", err)
			}
		})
	}

	if err := exitError("s1", nil, tail); err != nil {
		t.Errorf("exitError = %v for a clean exit, want nil", err)
	}
	if err := exitError("s1", errors.New("exit status 1"), "   \n  "); err == nil ||
		strings.Contains(err.Error(), ":  ") {
		t.Errorf("exitError = %v, want no empty stderr clause appended", err)
	}
}

// The stream can end before the process does - a cancel, or a line past
// maxLineBytes. The scan reason is the headline because it caused the kill,
// but it is not the whole account: the exit status and the stderr tail are
// what say *what the agent was doing* when it was cut off, and this branch
// fires on exactly the endings where nobody has anything better.
func TestScanStopErrorKeepsWhatTheProcessSaidAsWell(t *testing.T) {
	scanErr := errors.New("bufio.Scanner: token too long")
	const tail = `MCP server "probe" failed to start: ENOENT`

	full := scanStopError("s1", scanErr, errors.New("signal: killed"), tail+"\n")
	if full == nil {
		t.Fatal("scanStopError = nil")
	}
	for _, want := range []string{"s1", "stopped reading stdout", "token too long", "signal: killed", tail} {
		if !strings.Contains(full.Error(), want) {
			t.Errorf("scanStopError = %v, want it to carry %q", full, want)
		}
	}
	if !errors.Is(full, scanErr) {
		t.Error("the scan error must stay unwrappable - it is the headline a caller would test for")
	}

	// A process that outlived the stream has no exit status yet, and a quiet
	// one has no stderr. Neither may add an empty clause.
	bare := scanStopError("s1", scanErr, nil, "  \n ")
	if got := bare.Error(); strings.Contains(got, "exited") || strings.HasSuffix(got, ": ") {
		t.Errorf("scanStopError = %q, want no empty clauses", got)
	}
}

func TestErrIsNilAfterACleanExit(t *testing.T) {
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})
	drain(s)
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil after a clean exit", err)
	}
}

// A clean exit must be Err()==nil every single time, with no window where the
// close of Wake's read end races the scan into an ErrClosed the classifier
// would read as a stopped stream. cmd.StdoutPipe had that window - cmd.Wait
// closes its reader itself, untaggably, and can truncate unread frames - which
// is why Wake now owns the pipe and closes it only after awaitExit's Wait
// returns. This loop is the regression fence against reverting to StdoutPipe:
// it is green by construction with the owned pipe, and it is the concurrent
// spawn-drain-Wait lifecycle repeated under pressure. Run under -race, and
// -count multiplies it further; K is the fixed floor.
func TestACleanExitIsErrNilEveryTime(t *testing.T) {
	withFakeExec(t)
	const K = 100
	for i := 0; i < K; i++ {
		s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.Start(ctx); err != nil {
			cancel()
			t.Fatalf("iteration %d: Start: %v", i, err)
		}
		drain(s)
		if err := s.Err(); err != nil {
			cancel()
			t.Fatalf("iteration %d: Err = %v, want nil - a clean exit must never be classified as a stopped stream", i, err)
		}
		cancel()
	}
}

func TestStartTwiceIsAnError(t *testing.T) {
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})
	t.Cleanup(func() { drain(s) })

	if err := s.Start(context.Background()); err == nil {
		t.Fatal("want an error starting an already-started session, got nil")
	}
}

func TestSendWritesOneUserFrame(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	if err := s.Send("hello\nthere", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `"type":"user"`) {
		t.Errorf("stdin missing a user frame\ngot: %s", got)
	}
	// One line, with the embedded newline escaped: a raw newline here would
	// arrive as a second frame, and a frame claude cannot parse kills it.
	if n := strings.Count(got, "\n"); n != 1 {
		t.Errorf("got %d newlines, want 1\ngot: %s", n, got)
	}
	if !strings.Contains(got, `hello\nthere`) {
		t.Errorf("stdin lost the escaped newline\ngot: %s", got)
	}
}

// The tail is the whole mechanism behind Err's stderr half: a process that
// dies noisily must not be able to hold a session's worth of memory, and the
// end of stderr is where the reason for dying is.
func TestTailWriterKeepsTheEndAndBoundsWhatItHolds(t *testing.T) {
	w := &tailWriter{max: 8}

	for _, s := range []string{"abc", "de", "fghijkl"} {
		n, err := w.Write([]byte(s))
		if err != nil || n != len(s) {
			t.Fatalf("Write(%q) = %d, %v - a writer os/exec is copying into must consume everything", s, n, err)
		}
	}

	if got := w.String(); got != "efghijkl" {
		t.Errorf("String() = %q, want the last 8 bytes %q", got, "efghijkl")
	}
	if got := len(w.String()); got > 8 {
		t.Errorf("held %d bytes, want at most 8", got)
	}
}

func TestTailWriterKeepsShortOutputWhole(t *testing.T) {
	w := &tailWriter{max: stderrTailBytes}
	const reason = "Error: When using --print, --output-format=stream-json requires --verbose"

	if _, err := w.Write([]byte(reason)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := w.String(); got != reason {
		t.Errorf("String() = %q, want the reason untouched", got)
	}
}

// A claude process exports its own session identity to everything it
// spawns. Wake is routinely started from inside one, and a child that
// inherits those hands itself a foreign identity. Credentials are a
// different matter and must survive: scrubbing those would break every
// session on the machine.
func TestScrubbedEnvDropsNestedSessionIdentityAndKeepsCredentials(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"CLAUDECODE=1",
		"CLAUDE_CODE_SESSION_ID=parent-uuid",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"CLAUDE_CODE_EXECPATH=/somewhere/claude",
		"CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_PID=123",
		"CLAUDE_EFFORT=max",
		"CLAUDE_PLUGIN_DATA={}",
		"CLAUDE_CODE_OAUTH_TOKEN=secret",
		"ANTHROPIC_API_KEY=secret",
	}
	before := strings.Join(in, "\x00")

	got := strings.Join(scrubbedEnv(in), "\x00")

	for _, gone := range []string{"CLAUDECODE=", "CLAUDE_CODE_SESSION_ID=", "CLAUDE_CODE_ENTRYPOINT=",
		"CLAUDE_CODE_EXECPATH=", "CLAUDE_CODE_CHILD_SESSION=", "CLAUDE_PID=", "CLAUDE_EFFORT=", "CLAUDE_PLUGIN_DATA="} {
		if strings.Contains(got, gone) {
			t.Errorf("%s survived the scrub", gone)
		}
	}
	for _, kept := range []string{"PATH=/usr/bin", "CLAUDE_CODE_OAUTH_TOKEN=secret", "ANTHROPIC_API_KEY=secret"} {
		if !strings.Contains(got, kept) {
			t.Errorf("%s was scrubbed, and it must not be", kept)
		}
	}
	if strings.Join(in, "\x00") != before {
		t.Error("scrubbedEnv modified the environment it was given")
	}
}

// --- helpers ---------------------------------------------------------------

// waitForKind reads events until one of kind arrives, and returns its text.
func waitForKind(t *testing.T, s *Session, kind EventKind) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, open := <-s.Events():
			if !open {
				t.Fatalf("events channel closed before a %s arrived", kind)
			}
			if ev.Kind == kind {
				return ev.Text
			}
		case <-deadline:
			t.Fatalf("no %s event within 5s", kind)
		}
	}
}

// drainedAsync closes its channel once the session's events channel does.
func drainedAsync(s *Session) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		drain(s)
	}()
	return done
}

// safeBuffer is a log destination a test can read while another goroutine
// writes to it. Diagnostics leave the pump asynchronously now, so the write
// and the assertion are genuinely concurrent and a bare bytes.Buffer is a
// data race rather than a convenience.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForLogged polls until every want has been written, or fails with what
// was actually logged.
func waitForLogged(t *testing.T, logged *safeBuffer, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := logged.String()
		missing := ""
		for _, w := range want {
			if !strings.Contains(got, w) {
				missing = w
				break
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("nothing usable logged for an undecodable line: missing %q, got: %q", missing, got)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForParkedSender blocks until a session goroutine is parked in a channel
// send, and returns the stack that proves it.
//
// No assertion on the events channel can tell that park apart from a pump
// idling in Scan - both look like "the buffer is full and nothing is moving" -
// so the state the test is about is read out of the runtime instead of
// inferred. Failing here rather than proceeding is the point: cancelling a
// pump that never parked would prove nothing at all.
func waitForParkedSender(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if stack, ok := parkedSender(); ok {
			return stack
		}
		if time.Now().After(deadline) {
			t.Fatalf("no session goroutine ever parked sending to the events channel, so this test never reached the state it is about\n%s", allStacks())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// parkedSender returns the pump's stack if it is blocked handing an event to
// the consumer.
//
// Matched on the frame and not on the goroutine's state, because the fix
// changes the state: a bare channel send parks as "chan send", and the select
// that makes it cancellable parks as "select". Beneath emit there is nothing
// else that can block, so the frame is the exact signal under either shape -
// and matching the state alone would also catch closeOnCancel, which sits in
// a select of its own for the whole life of the session.
func parkedSender() (string, bool) {
	for _, g := range strings.Split(allStacks(), "\n\n") {
		header, _, _ := strings.Cut(g, "\n")
		blocked := strings.Contains(header, "chan send") || strings.Contains(header, "[select")
		if blocked && strings.Contains(g, "wake/internal/core.(*Session).emit(") {
			return g, true
		}
	}
	return "", false
}

func allStacks() string {
	buf := make([]byte, 1<<20)
	return string(buf[:runtime.Stack(buf, true)])
}

// waitForErr blocks until the session records why it ended, reading no events
// at all. Err is written by finish, which runs only once the pump has exited,
// so it reports the teardown without a consumer - and a consumer is exactly
// what must not appear here, since one would unpark the pump itself.
func waitForErr(t *testing.T, s *Session, parked string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if s.Err() != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cancelling the context did not end a session whose pump was parked on the events channel: a wedged reader makes a session unkillable\nparked at:\n%s\nstill:\n%s", parked, allStacks())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForGoroutines polls until the count is back to at most base, which
// upgrades "the events channel closed" into "nothing the session started is
// still running". Polled rather than sampled: os/exec's own copy goroutine
// and context watchdog unwind a moment after Wait returns.
func waitForGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= base {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			buf = buf[:runtime.Stack(buf, true)]
			t.Errorf("%d goroutines still running, want <= %d - something the session started never returned\n%s", n, base, buf)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// longestRecordedLine measures the corpus so a synthetic size stays tied to
// what the wire actually produces.
func longestRecordedLine(t *testing.T) int {
	t.Helper()
	longest := 0
	for _, path := range fixtureFiles(t) {
		for _, line := range fixtureLines(t, path) {
			if len(line) > longest {
				longest = len(line)
			}
		}
	}
	if longest == 0 {
		t.Fatal("no fixture lines measured")
	}
	return longest
}

// A session that was stopped before it started never starts.
//
// # The window this closes, and why it is core's rather than the daemon's
//
// internal/daemon's launch takes the fleet row **before** it starts the
// process, which is what stops two wakes of one id putting two claudes on one
// transcript. That reorder leaves an agent in s.agents with no process behind
// it for the width of an exec, and shutdown can snapshot it there: takeAgents
// hands it to stop(), which reaches Stop() on an unstarted session, sets
// stopped and returns nil - a graceful stop consumed and lost. Start then
// execs a claude that shutdown has already walked past. It never ends, because
// nothing closed its stdin; shutdown stalls the whole grace and SIGKILLs it;
// and kill() clears the park flag, so under ⌃Q that session is dropped from the
// park book - the one outcome the whole park design exists to prevent.
//
// The check belongs here because Start is the only lifecycle entry point that
// did not read stopped - Send does (see writer) and Stop is idempotent by
// design. A daemon-side flag would close this path and not the next caller's.
//
// The exec seam is counted rather than the error inspected, because `err != nil`
// is not the claim: a Start that got as far as the process and then failed on a
// pipe returns an error too. Zero calls is proof no process was built.
func TestStartRefusesASessionThatWasStoppedBeforeItStarted(t *testing.T) {
	reached := withCountingFakeExec(t)
	s := NewSession(Config{SessionID: "0a1b2c3d-0000-4000-8000-00000000feed"})

	if err := s.Stop(); err != nil {
		t.Fatalf("stopping a session that never started: %v", err)
	}

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("a session that had already been stopped started anyway. Whoever stopped it will " +
			"never close its stdin - Stop found none to close - so it runs until something SIGKILLs " +
			"it, and a fleet-wide shutdown waits out its whole grace first")
	}
	if !strings.Contains(err.Error(), "stopped") {
		t.Errorf("the refusal is %q and does not say the session was stopped: the caller's next move "+
			"is to put the row back, and it has to be able to tell this from a machine with no claude "+
			"on it", err)
	}
	if *reached != 0 {
		t.Errorf("execCommand was reached %d times, want 0. The refusal has to happen before there is "+
			"a process, because a process started here is one nothing will ever stop: the stop that "+
			"would have closed its stdin has already been consumed", *reached)
	}
}

// And stopping is still the only thing that refuses it - a session nobody
// stopped starts exactly as before.
//
// The floor the guard above needs: `return errors.New(...)` at the top of Start
// satisfies every assertion up there and breaks the product outright.
func TestStartStillWorksForASessionNobodyStopped(t *testing.T) {
	reached := withCountingFakeExec(t)
	s := NewSession(Config{SessionID: "0a1b2c3d-0000-4000-8000-00000000beef"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start on a session nobody stopped: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(); drain(s) })
	if *reached != 1 {
		t.Errorf("execCommand was reached %d times, want 1: the refusal above has widened past the "+
			"session it was written for", *reached)
	}
}

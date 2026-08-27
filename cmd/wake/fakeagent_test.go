// The agent the conversation tests talk to.
//
// # Why this is the test binary and not a shell script
//
// The two fakes already in this package are shell scripts, and for what they
// do that is right: detach_unix_test.go needs a process that says one thing and
// then holds stdin, and orphan_unix_test.go needs one that floods. Neither
// reads what Wake writes.
//
// These do, and reading is the whole point. A fake that greps its stdin for a
// substring cannot tell a well-formed frame from one carrying the same bytes at
// the wrong depth - which is exactly the defect a recording spike found in the
// permission path, where valid JSON with the right request_id produced no
// action at all and every unit test passed. So these decode structurally,
// through the same nesting the CLI reads, and that is not something to write in
// sed.
//
// The mechanism is the helper-process pattern internal/daemon/main_test.go
// documents: a directory on PATH holding a symlink named `claude` that points
// at this test binary, and TestMain intercepting the re-entry. What the daemon
// spawns is a genuine process with a genuine stdout pipe, a genuine process
// group and a genuine exit.
//
// That makes this file the one place in cmd/wake allowed to name Claude's JSON,
// for the reason internal/daemon's equivalent is: something has to speak the
// wire to prove nothing above it has to.

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The markers that turn this binary into an agent, and which script it runs.
const (
	fakeAgentEnv    = "WAKE_E2E_FAKE_CLAUDE"
	fakeAgentScript = "WAKE_E2E_FAKE_SCRIPT"
	// fakeTranscriptEnv opts a test in to the fake agent leaving a transcript on
	// disk for its session id, so a parked record is offered back. It is a
	// separate flag rather than keying on WAKE_PROJECTS because other tests set
	// that to plant real transcripts of their own, which this must not clobber.
	fakeTranscriptEnv = "WAKE_E2E_FAKE_TRANSCRIPT"
)

// scriptInterruptible is the agent that takes a turn slowly enough to be
// stopped. The empty script is the one that answers immediately.
const scriptInterruptible = "interruptible"

// scriptAsks blocks on a permission request instead of answering, which is the
// only state an answerable card exists in.
const scriptAsks = "asks"

// scriptPlans writes a task list and leaves it on screen, which is the only
// state a checklist block exists in. The tool is never called anywhere in
// testdata/stream - see notInTheCorpus - so this fake is the only thing in the
// tree that produces one end to end.
const scriptPlans = "plans"

// scriptModes answers a set_permission_mode control request the way 2.1.228
// does. The default echo fake drops every non-user line, so ⇧⇥ reaches it and
// nothing comes back - and the label is only allowed to move on a receipt, so a
// screen test of the mode needs an agent that sends one.
const scriptModes = "modes"

// scriptQuestions blocks on an ask that puts a *question* rather than a
// yes/no - the interactive shape, carrying requires_user_interaction and a
// questions payload. It is a different script from scriptAsks because the two
// are answered by different frames: a bare allow on a question runs the tool
// and tells the model nobody replied.
const scriptQuestions = "questions"

// scriptDispatches is the agent that runs a subagent: the lifecycle frames that
// put a row on screen, and the forwarded frames that fill that row's own
// transcript. It is the only thing in the tree that produces both end to end.
const scriptDispatches = "dispatches"

// scriptDispatchesLive is the same dispatch with no ending, which is the state
// the right sidebar exists to show: a subagent that is still running.
//
// A second script rather than a flag on the first, because the ending is what
// scriptDispatches was written to produce - the row's final usage and the line
// it leaves in the transcript are both asserted off it. A long-running subagent
// is genuinely a different recording, and it is the only one in which a sidebar
// row is stable enough for a screen test to wait for.
const scriptDispatchesLive = "dispatches-live"

// What scriptDispatches says about its subagent, so a test can find each part
// on screen: the row's own words, and the line only the subagent's transcript
// holds.
const (
	dispatchType  = "general-purpose"
	dispatchLabel = "Counting lines in alpha"

	// What task_progress reports, and it is deliberately *not* dispatchLabel.
	// A live status rewrites the description on every progress frame - all 9
	// recorded dispatches carrying both end on a different one - so a fake
	// that echoed its own dispatch description would let a transcript line
	// naming the wrong thing pass every screen test.
	dispatchDoing = "Reading beta.txt"
	dispatchSaid  = "twelve lines in alpha.txt"

	// dispatchToolUse is the parent's Agent tool_use id, which is what every
	// forwarded frame carries as parent_tool_use_id and what task_started
	// names as its tool_use_id. One value per dispatch, because on the wire it
	// is one - and a *different* value on each turn, because two turns are two
	// dispatches. Reusing them folded the second dispatch into the first row,
	// which let a test claiming "one line per dispatch" pass while proving
	// only that two ending frames are not deduplicated into one line.
	dispatchToolUse = "toolu_dispatch1"
	dispatchTaskID  = "a1b2c3d4e5f60718"
)

// askedTool is what scriptAsks says it wants to run, so a test can find the
// card by the words on it.
const askedTool = "Write"

// The words scriptQuestions puts, so a test can find each part of the card on
// screen: the chip, the question, its options, and the sample beside the one
// the cursor is on.
const (
	questionChip    = "Format"
	questionText    = "which output format"
	questionOptionA = "Markdown"
	questionOptionB = "CSV"
	questionSample  = "orders summary sample"
)

// heardPrefix is what the agent puts in front of anything it was told.
//
// It exists so a test can tell the agent's answer from the client's own echo of
// the same message. App.submit draws a sent message into the transcript
// locally, so asserting on the text somebody typed proves only that they typed
// it - this prefix is the part only the far side can produce.
const heardPrefix = "agent heard: "

// workingMarker is emitted when a turn starts and before it finishes, so a test
// can interrupt a turn that is provably in flight rather than racing one.
const workingMarker = "working on it"

// runFakeAgent is this binary pretending to be `claude`.
func runFakeAgent() int {
	// Or anything this process spawns becomes another agent. Nothing here
	// spawns, and the trap is cheap enough to close anyway.
	_ = os.Unsetenv(fakeAgentEnv)

	sid := agentArg(os.Args, "--session-id")
	// A session that runs leaves a transcript on disk, so a parked record for it
	// is offered back (parkedStatuses drops one with none). Behind an explicit
	// opt-in, so it never clobbers a transcript another test planted for its own
	// id under WAKE_PROJECTS.
	if os.Getenv(fakeTranscriptEnv) == "1" {
		if projects := os.Getenv("WAKE_PROJECTS"); projects != "" {
			_ = writeFakeTranscript(projects, sid)
		}
	}
	switch os.Getenv(fakeAgentScript) {
	case scriptInterruptible:
		return fakeAgentInterruptible(sid)
	case scriptAsks:
		return fakeAgentAsks(sid)
	case scriptQuestions:
		return fakeAgentQuestions(sid)
	case scriptModes:
		return fakeAgentModes(sid)
	case scriptPlans:
		return fakeAgentPlans(sid)
	case scriptDispatches:
		return fakeAgentDispatches(sid)
	case scriptDispatchesLive:
		return fakeAgentDispatchesLive(sid)
	}
	return fakeAgentEcho(sid)
}

// plantTranscript lays a transcript for id under a projects directory this test
// owns and points ProjectsDir at it, so a parked record for id is offered back:
// parkedStatuses drops a record with no transcript on disk (BUG-27). A projects
// directory already set is reused, so several ids can share one tree.
func plantTranscript(t *testing.T, id string) {
	t.Helper()
	projects := os.Getenv("WAKE_PROJECTS")
	if projects == "" {
		projects = t.TempDir()
		t.Setenv("WAKE_PROJECTS", projects)
	}
	if err := writeFakeTranscript(projects, id); err != nil {
		t.Fatalf("plant transcript for %s: %v", id, err)
	}
}

// writeFakeTranscript is plantTranscript's disk half, shared with the fake agent
// - which runs in a subprocess with no *testing.T and writes its own transcript
// so the session it stood in for is offered back after a park.
func writeFakeTranscript(projects, id string) error {
	dir := filepath.Join(projects, "-Users-someone-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte("{}\n"), 0o600)
}

// fakeAgentPlans writes one task list per turn: one item in flight, one still
// to do, one behind it. The shape is the shipped binary's own description of
// the tool, which is the only source there is - see notInTheCorpus.
func fakeAgentPlans(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)

	for line := range agentStdin() {
		if _, ok := userTextOf(line); !ok {
			continue
		}
		sayTodos(sid)
		sayText(sid, heardPrefix+"planned")
		sayResult(sid)
	}
	return 0
}

// sayTodos emits a TodoWrite tool call carrying a three-item list.
func sayTodos(sid string) {
	fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":`+
		`[{"type":"tool_use","id":"toolu_plan","name":"TodoWrite","input":{"todos":[`+
		`{"content":"encode the receipt","status":"in_progress","activeForm":"Encoding the receipt"},`+
		`{"content":"refuse the mode verb","status":"pending","activeForm":"Refusing the verb"},`+
		`{"content":"wire the daemon","status":"completed","activeForm":"Wiring the daemon"}`+
		`]}}]}}`+"\n", sid)
}

// fakeAgentDispatches runs one subagent per turn, in the order a real CLI
// emits it: the dispatch tool call, task_started, the subagent's own forwarded
// speech, task_progress, and the two frames that end it.
//
// The order is the corpus's rather than a guess -
// testdata/stream/subagent-task.jsonl is what it was transcribed from.
func fakeAgentDispatches(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)

	turn := 0
	for line := range agentStdin() {
		if _, ok := userTextOf(line); !ok {
			continue
		}
		tool, task := dispatchIDs(turn)
		turn++
		sayDispatch(sid, tool)
		sayTaskStarted(sid, tool, task)
		sayForwarded(sid, tool, dispatchSaid)
		sayTaskProgress(sid, tool, task)
		sayText(sid, heardPrefix+"dispatched")
		sayTaskEnded(sid, tool, task)
		sayResult(sid)
	}
	return 0
}

// fakeAgentDispatchesLive dispatches a subagent and never ends it, so the turn
// finishes with the dispatch still running.
//
// That is not a truncated recording: a subagent streams past its own result and
// past stdin closing, which is why Tasks.Observe retires a row only on a frame
// that says so. The turn ending is exactly where a real long dispatch is still
// going.
func fakeAgentDispatchesLive(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)

	turn := 0
	for line := range agentStdin() {
		if _, ok := userTextOf(line); !ok {
			continue
		}
		tool, task := dispatchIDs(turn)
		turn++
		sayDispatch(sid, tool)
		sayTaskStarted(sid, tool, task)
		sayTaskProgress(sid, tool, task)
		sayText(sid, heardPrefix+"dispatched")
		sayResult(sid)
	}
	return 0
}

// dispatchIDs are the two ids the nth dispatch of a session runs under. The
// first turn uses the constants a test can assert against; every later one is
// distinct, because a second turn is a second dispatch.
func dispatchIDs(turn int) (tool, task string) {
	if turn == 0 {
		return dispatchToolUse, dispatchTaskID
	}
	n := strconv.Itoa(turn)
	return dispatchToolUse + n, dispatchTaskID + n
}

// The parent's own tool call, whose id is the join to everything below it.
func sayDispatch(sid, tool string) {
	fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":`+
		`[{"type":"tool_use","id":%q,"name":"Agent","input":{"description":%q}}]}}`+"\n",
		sid, tool, dispatchLabel)
}

func sayTaskStarted(sid, tool, task string) {
	fmt.Printf(`{"type":"system","subtype":"task_started","task_id":%q,"tool_use_id":%q,`+
		`"description":%q,"subagent_type":%q,"task_type":"local_agent","session_id":%q}`+"\n",
		task, tool, dispatchLabel, dispatchType, sid)
}

func sayTaskProgress(sid, tool, task string) {
	fmt.Printf(`{"type":"system","subtype":"task_progress","task_id":%q,"tool_use_id":%q,`+
		`"description":%q,"subagent_type":%q,"usage":{"total_tokens":27000,"tool_uses":1,`+
		`"duration_ms":4100},"last_tool_name":"Read","session_id":%q}`+"\n",
		task, tool, dispatchDoing, dispatchType, sid)
}

func sayTaskEnded(sid, tool, task string) {
	fmt.Printf(`{"type":"system","subtype":"task_updated","task_id":%q,`+
		`"patch":{"status":"completed","end_time":1786272221002},"session_id":%q}`+"\n",
		task, sid)
	fmt.Printf(`{"type":"system","subtype":"task_notification","task_id":%q,"tool_use_id":%q,`+
		`"status":"completed","output_file":"/tmp/x.output","summary":%q,`+
		`"usage":{"total_tokens":29000,"tool_uses":1,"duration_ms":5000},"session_id":%q}`+"\n",
		task, tool, dispatchSaid, sid)
}

// A frame the subagent produced, carrying the three keys that attribute it.
func sayForwarded(sid, tool, text string) {
	fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":`+
		`[{"type":"text","text":%q}]},"parent_tool_use_id":%q,"subagent_type":%q,`+
		`"task_description":%q}`+"\n", sid, text, dispatchToolUse, dispatchType, dispatchLabel)
}

// fakeAgentAsks blocks on a permission request per turn and ends the turn once
// the answer arrives, which is what a real CLI does with --permission-prompt-tool.
func fakeAgentAsks(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)

	asked := 0
	for line := range agentStdin() {
		if _, ok := userTextOf(line); ok {
			asked++
			sayAsk(fmt.Sprintf("ask-%d", asked))
			continue
		}
		if answeredID(line) != "" {
			sayText(sid, heardPrefix+"answered")
			sayResult(sid)
		}
	}
	return 0
}

// fakeAgentQuestions blocks on a question instead of answering, which is the
// only state an answerable question card exists in - and, like a real one, it
// stays blocked until something answers rather than timing out.
func fakeAgentQuestions(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)

	asked := 0
	for line := range agentStdin() {
		if _, ok := userTextOf(line); ok {
			asked++
			sayQuestion(fmt.Sprintf("q-%d", asked))
			continue
		}
		if answeredID(line) != "" {
			sayText(sid, heardPrefix+"answered")
			sayResult(sid)
		}
	}
	return 0
}

// sayQuestion emits an interactive ask carrying one question: the chip, two
// options, and a sample for each.
//
// requires_user_interaction is what makes it a question rather than a
// permission - the tool's name is not, and nothing above the airlock reads one.
// The payload is the recorded shape reduced to one question, which is what
// keeps a screen assertion to a single card's worth of rows.
func sayQuestion(requestID string) {
	fmt.Printf(`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","requires_user_interaction":true,"tool_use_id":"toolu_fake","input":{"questions":[{"question":%q,"header":%q,"multiSelect":false,"options":[{"label":%q,"description":"a table plus a totals line","preview":%q},{"label":%q,"description":"machine readable rows","preview":"name,qty\nwidget,3"}]}]}}}`+"\n",
		requestID, questionText+"?", questionChip, questionOptionA, questionSample, questionOptionB)
}

// fakeAgentEcho answers every turn at once: one opening turn, then a turn per
// line on stdin, ending when stdin closes. That is how a real process behaves -
// it idles between turns and exits on EOF.
func fakeAgentEcho(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		sayText(sid, heardPrefix+text)
		sayResult(sid)
	}
	return 0
}

// fakeAgentModes is the echo agent plus the one control request this build
// sends: it answers a set_permission_mode with the mode it was asked for, which
// is what turns ⇧⇥ into a receipt the label may move on.
//
// Deliberately not folded into fakeAgentEcho: every screen test in this package
// runs on that one, and a control_response arriving where none did before is a
// frame each of them would have to be re-argued against.
func fakeAgentModes(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	for line := range agentStdin() {
		if id, mode, ok := modeRequested(line); ok {
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"mode":%q}}}`+"\n", id, mode)
			continue
		}
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		sayText(sid, heardPrefix+text)
		sayResult(sid)
	}
	return 0
}

// modeRequested reads the mode out of a set_permission_mode control request,
// one level down beside its subtype - the nesting the airlock's own findings
// record.
func modeRequested(line string) (id, mode string, ok bool) {
	var f struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
			Mode    string `json:"mode"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return "", "", false
	}
	if f.Type != "control_request" || f.Request.Subtype != "set_permission_mode" {
		return "", "", false
	}
	return f.RequestID, f.Request.Mode, true
}

// fakeAgentInterruptible starts a turn and does not finish it until it is
// stopped, then goes on working.
//
// Both halves are load-bearing. A turn that is still running is the only state
// an interrupt has anything to do, and a fake that completed the turn first
// would leave the test interrupting an idle session - a no-op that produces a
// receipt and proves nothing. And the process stays alive across the abort,
// because an interrupt ends a turn and not a session; anything that let Wake
// treat it as an ending would show up here as a conversation that went dead.
func fakeAgentInterruptible(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)

	running := false
	for line := range agentStdin() {
		if requestID, ok := interruptOf(line); ok {
			if !running {
				// An interrupt with no turn under it is a harmless no-op that
				// still gets a receipt. Reproduced rather than ignored so a test
				// that mistimed its keystroke fails on the missing abort instead
				// of passing on a coincidence.
				sayReceipt(requestID)
				continue
			}
			running = false
			// Claude's own account of the abort, on the shape the corpus
			// records: an ordinary user frame whose text is the only thing
			// identifying it. core resolves it to a notice; nothing above the
			// airlock may match on this string, which is why it is written here
			// and nowhere else in this package.
			sayUser(sid, "[Request interrupted by user]")
			sayReceipt(requestID)
			sayAbortedResult(sid)
			continue
		}
		text, ok := userTextOf(line)
		if !ok {
			continue
		}
		// The turn opens and deliberately does not close.
		running = true
		sayText(sid, workingMarker+": "+text)
		// Two messages of the turn, each reporting its own count more than once
		// and cumulatively. Wake takes the newest figure within a message and
		// sums across them, which is what the working line and the roster row
		// draw while the turn runs.
		for _, message := range fakeTurnMessages {
			sayMessageStart(sid)
			for _, n := range message {
				sayMessageDelta(sid, n)
			}
		}
	}
	return 0
}

// --- reading what Wake writes ----------------------------------------------

// userTextOf pulls the text out of a user turn, or reports that this line was
// not one.
func userTextOf(line string) (string, bool) {
	var f struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil || f.Type != "user" {
		return "", false
	}
	var b strings.Builder
	for _, c := range f.Message.Content {
		b.WriteString(c.Text)
	}
	return b.String(), true
}

// interruptOf reports whether this line is an interrupt, and the correlator its
// receipt has to carry.
//
// Decoded through the nesting rather than grepped: a control_request keeps its
// subtype under "request" and its id on the envelope, and a frame that put
// either anywhere else would abort nothing on a real CLI while still containing
// every byte a substring check looks for.
func interruptOf(line string) (string, bool) {
	var f struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return "", false
	}
	if f.Type != "control_request" || f.Request.Subtype != "interrupt" {
		return "", false
	}
	return f.RequestID, true
}

// agentStdin yields whole newline-delimited frames until stdin closes, which is
// the EOF that ends a real agent too.
//
// On its own goroutine, and that is not decoration. A fake that reads stdin in
// the foreground of whatever else it is doing stops reading the moment it is
// busy, and a previous controller test in this project passed for exactly that
// reason - its fake died when the pipe closed and the branch under test was
// never reached.
func agentStdin() <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			out <- sc.Text()
		}
	}()
	return out
}

// agentArg reads a flag's value out of the command line the daemon built. The
// session id is the one thing the fake has to agree with Wake about: it stamps
// every frame with it, so a daemon routing by anything else is caught here.
func agentArg(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// --- what the agent says ----------------------------------------------------

func sayText(sid, text string) {
	fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", sid, text)
}

// sayUser is a frame on the user's side of the conversation. Claude writes
// these for its own abort markers, which is the only reason this fake has one.
func sayUser(sid, text string) {
	fmt.Printf(`{"type":"user","session_id":%q,"message":{"role":"user","content":[{"type":"text","text":%q}]}}`+"\n", sid, text)
}

// fakeOutputTokens is what one turn of this fake reports having produced. A
// round number, so a screen test can assert on the abbreviation `↓ 1.2k`
// rather than on arithmetic.
const fakeOutputTokens = 1234

// sayResult ends a turn, carrying the usage block a real one carries.
//
// It carried none until 2026-08-15, which made Agent.Tokens zero everywhere in
// this suite and left the sidebar's token count and the working line's meta
// clause unreachable from a screen test. Every recorded result in
// testdata/stream carries `usage`, so this is the fake becoming faithful rather
// than a fixture bent to suit a test.
//
// The context fields stay zero and `modelUsage` stays absent on purpose:
// resultFacts reads the window from modelUsage, and inventing one here would
// put a context percentage on every conversation's status bar in every test in
// this package.
func sayResult(sid string) {
	fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"session_id":%q,"result":"done",`+
		`"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":%d}}`+"\n",
		sid, fakeOutputTokens)
}

// fakeTurnMessages is a turn's two messages, each as the *cumulative* counts
// its message_delta frames report - which is what a real one sends: "one or
// more message_delta events", their usage "cumulative", in the streaming docs'
// own words. So the turn's figure is 150 + 400 = 550, and a build that added
// the five numbers up would say 900.
//
// 550 is deliberately **not** fakeOutputTokens: a screen test can then tell the
// turn's own figure from the session total, and fails if a surface draws the
// wrong one.
var fakeTurnMessages = [][]int{{50, 100, 150}, {200, 400}}

// sayMessageDelta closes one message of a turn and states its output tokens.
//
// The shape a real `--include-partial-messages` run emits, which this tree has
// still never recorded - internal/core/wire.go names the source and says what
// it is worth. This exists so the *plumbing* is covered end to end: if the real
// envelope differs, this fake is what has to change with it, and the airlock's
// own tests are where that is written down.
func sayMessageDelta(sid string, tokens int) {
	fmt.Printf(`{"type":"stream_event","session_id":%q,"parent_tool_use_id":null,"uuid":"u-md","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":%d}}}`+"\n",
		sid, tokens)
}

// sayMessageStart opens one message of a turn.
//
// Its own usage is the shape a real one carries - output_tokens of 1, for a
// message that has written nothing yet - and it is here rather than omitted so
// a reader that added it to the turn would be caught by the arithmetic below.
func sayMessageStart(sid string) {
	fmt.Printf(`{"type":"stream_event","session_id":%q,"parent_tool_use_id":null,"uuid":"u-ms","event":{"type":"message_start","message":{"id":"msg_fake","role":"assistant","usage":{"input_tokens":25,"output_tokens":1}}}}`+"\n", sid)
}

// sayAbortedResult is how an interrupted turn ends, and it is not how a
// successful one does: subtype error_during_execution, is_error true, and no
// result key at all. Reproduced because Wake's exit guard is built on it.
func sayAbortedResult(sid string) {
	fmt.Printf(`{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":%q,"errors":[]}`+"\n", sid)
}

// sayAsk blocks the turn on a permission request. It names no session, which is
// what the recordings show - the correlator is the request id.
func sayAsk(requestID string) {
	fmt.Printf(`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":%q,"input":{"file_path":"/tmp/note.txt","content":"ok"},"tool_use_id":"toolu_fake"}}`+"\n",
		requestID, askedTool)
}

// answeredID reports which ask a control_response answers, or empty. Decoded
// through the nesting the CLI reads, for interruptOf's reason.
func answeredID(line string) string {
	var f struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil || f.Type != "control_response" {
		return ""
	}
	return f.Response.RequestID
}

// sayReceipt echoes an interrupt's correlator back. It is the only thing tying
// a receipt to the interrupt that caused it: the frame names no session and no
// subtype of its own.
func sayReceipt(requestID string) {
	fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`+"\n", requestID)
}

// --- putting it on PATH -----------------------------------------------------

// withScriptedAgent puts a `claude` on PATH that is this test binary, and
// selects which script it runs.
//
// core resolves the binary by name, so this is the whole of the substitution -
// and CLAUDE.md's rule against testing on a live model makes it mandatory
// rather than convenient.
func withScriptedAgent(t *testing.T, script string) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dir, "claude")); err != nil {
		t.Fatalf("symlink the fake agent: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeAgentEnv, "1")
	t.Setenv(fakeAgentScript, script)
}

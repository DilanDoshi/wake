// What the recorded subagent corpus proves, asserted over the whole of it.
//
// 383 recorded subagent lines carried exactly one assertion each before this
// - "not KindUnknown", from the glob in fixtures_test.go - and every one of
// them passed while the whole subagent dimension was being dropped on the
// floor. That is the failure this file exists to make impossible: nothing
// failed, so nothing announced what was lost.
//
// This file, fixtures_subagent_test.go, fixtures_helpers_test.go,
// protocol_test.go, encode_test.go and airlock_test.go are the airlock's own
// tests, so together they are the only files besides the airlock itself that
// may name Claude's frame types - and only ever to prove it decodes and
// encodes them. session_test.go is the one further exception, for the same
// narrow reason: its fake process has to speak the wire to prove session.go
// never does.

package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// --- the subagent corpus ----------------------------------------------------
//
// 383 recorded subagent lines carried exactly one assertion each before this
// - "not KindUnknown", via the glob above - and every one of them passed
// while the whole subagent dimension was being dropped. That is the shape of
// the failure this section exists to make impossible: nothing failed, so
// nothing announced what was lost.

// Corpus counts, measured over testdata/stream by reading the bytes. They are
// minimums, not equalities, so adding a fixture cannot redden the suite -
// but a fixture *deleted* or re-recorded without its subagent frames turns
// every assertion below into a no-op, and these are what catch that.
const (
	// Frames carrying a non-null parent_tool_use_id.
	minForwardedFrames = 80

	// Parent Agent tool_use blocks. Nine dispatches across seven files.
	minDispatches = 9

	// tool_result blocks whose content is an array rather than a string.
	minArrayToolResults = 10

	// can_use_tool frames carrying agent_id. One of the three asks.
	minSubagentAsks = 1
)

// Every forwarded frame is attributed, no parent frame is, and the
// attribution carries a name a human can read.
//
// The counting is what makes it more than a pin: a decoder that attributed
// nothing would satisfy the "no parent is attributed" half perfectly.
func TestEveryForwardedFrameIsAttributedToItsSubagent(t *testing.T) {
	forwarded, attributed, named := 0, 0, 0
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			parent, isForwarded := parentToolUseOf(t, line)
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			for _, ev := range evs {
				if !isForwarded {
					// A frame that was not forwarded may still be *about* a
					// subagent - a dispatch receipt and a permission ask both
					// are - and both name an Agent id. What must never happen
					// is a parent's own speech being filed as a subagent's,
					// and that is exactly the shape with a Dispatch and no
					// Agent. So Agent is the discriminator, not Result.
					if ev.Subagent != nil && ev.Subagent.Agent == "" {
						t.Errorf("%s:%d is not forwarded but its own content is attributed to a subagent: %+v", path, n+1, ev.Subagent)
					}
					continue
				}
				forwarded++
				if ev.Subagent == nil {
					t.Errorf("%s:%d is forwarded from %s but decoded with no Subagent: its work is filed as the agent's own", path, n+1, parent)
					continue
				}
				attributed++
				if ev.Subagent.Dispatch != parent {
					t.Errorf("%s:%d Dispatch = %q, want the frame's parent_tool_use_id %q", path, n+1, ev.Subagent.Dispatch, parent)
				}
				if ev.Subagent.Task != "" && ev.Subagent.Type != "" {
					named++
				}
			}
		}
	}
	if forwarded < minForwardedFrames {
		t.Errorf("%d forwarded events in the corpus, want at least %d - the fixtures this test reads are gone or changed", forwarded, minForwardedFrames)
	}
	if attributed != forwarded {
		t.Errorf("%d of %d forwarded events attributed", attributed, forwarded)
	}
	// The opaque toolu_ id separates two subagents; task_description is what
	// names one. All 80 recorded frames carry both.
	if named != forwarded {
		t.Errorf("%d of %d forwarded events carry both a type and a task description", named, forwarded)
	}
}

// Three concurrent streams must not read as one monologue, and this is the
// core half of that property: the events of subagent-parallel.jsonl separate
// into exactly three groups - the parent's and two subagents' - keyed on
// something every frame carries on its own.
func TestConcurrentSubagentsSeparateIntoDistinctStreams(t *testing.T) {
	const fixture = "subagent-parallel.jsonl"
	path := filepath.Join("..", "..", "testdata", "stream", fixture)

	dispatches := map[string]string{} // dispatch id -> task description
	parentEvents := 0
	for n, line := range fixtureLines(t, path) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s:%d failed to decode: %v", fixture, n+1, err)
		}
		for _, ev := range evs {
			if ev.Subagent == nil {
				parentEvents++
				continue
			}
			if ev.Subagent.Dispatch == "" {
				t.Errorf("%s:%d subagent event with no Dispatch: nothing separates it from the other subagent", fixture, n+1)
				continue
			}
			if task, seen := dispatches[ev.Subagent.Dispatch]; seen && ev.Subagent.Task != "" && task != ev.Subagent.Task {
				t.Errorf("%s:%d dispatch %s has two task descriptions: %q and %q", fixture, n+1, ev.Subagent.Dispatch, task, ev.Subagent.Task)
			} else if ev.Subagent.Task != "" {
				dispatches[ev.Subagent.Dispatch] = ev.Subagent.Task
			}
		}
	}

	if len(dispatches) != 2 {
		t.Fatalf("%s produced %d distinct dispatches, want the 2 recorded: %v", fixture, len(dispatches), dispatches)
	}
	if parentEvents == 0 {
		t.Errorf("%s produced no parent events: the third stream is missing", fixture)
	}
	// Two subagents whose descriptions were identical would be tellable
	// apart by id but not by eye, so the recording has to keep them distinct
	// for the DM's half of this property to mean anything.
	seen := map[string]bool{}
	for id, task := range dispatches {
		if seen[task] {
			t.Errorf("dispatch %s repeats the task description %q", id, task)
		}
		seen[task] = true
	}
}

// The dispatch receipt, over the whole corpus. Three properties at once: the
// receipt is recognised, its content arrives as prose rather than JSON, and
// Dispatch ties it to the forwarded frames it repeats - the join §7 of the
// findings note reported as needing a task_started lookup.
func TestDispatchReceiptsJoinTheForwardedStreamAndCarryProse(t *testing.T) {
	receipts, joined := 0, 0
	for _, path := range fixtureFiles(t) {
		forwardedIn := map[string]bool{}
		lines := fixtureLines(t, path)
		for _, line := range lines {
			if parent, ok := parentToolUseOf(t, line); ok {
				forwardedIn[parent] = true
			}
		}
		for n, line := range lines {
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			for _, ev := range evs {
				if ev.Subagent == nil || ev.Subagent.Result == "" {
					continue
				}
				receipts++
				if ev.Kind != KindToolResult {
					t.Errorf("%s:%d receipt decoded as %s, want %s", path, n+1, ev.Kind, KindToolResult)
				}
				if strings.HasPrefix(strings.TrimSpace(ev.Text), "[{") {
					t.Errorf("%s:%d receipt Text is a JSON array literal: %.60q", path, n+1, ev.Text)
				}
				if ev.Text == "" {
					t.Errorf("%s:%d receipt lost its content entirely", path, n+1)
				}
				if ev.Subagent.Agent == "" {
					t.Errorf("%s:%d receipt carries no agent id", path, n+1)
				}
				if forwardedIn[ev.Subagent.Dispatch] {
					joined++
				} else if ev.Subagent.Dispatch == "" {
					t.Errorf("%s:%d receipt names no dispatch, so nothing ties it to the speech it repeats", path, n+1)
				}
			}
		}
	}
	if receipts < minDispatches {
		t.Errorf("%d dispatch receipts in the corpus, want at least %d", receipts, minDispatches)
	}
	// Every recorded dispatch forwarded at least one frame, so every receipt
	// joins. That is the whole claim, and it is what lets a view suppress a
	// duplicate report without guessing which subagent's it was.
	if joined != receipts {
		t.Errorf("%d of %d receipts join a forwarded stream by Dispatch", joined, receipts)
	}
}

// No tool result anywhere in the corpus may reach a reader as JSON. This is
// the one that would have caught C2: it is a property over all 44, not a pin
// on the 10 that happen to be arrays today.
func TestNoToolResultReachesAReaderAsJSON(t *testing.T) {
	arrays, results := 0, 0
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			arrays += arrayToolResults(t, line)
			for _, ev := range evs {
				if ev.Kind != KindToolResult {
					continue
				}
				results++
				if body := strings.TrimSpace(ev.Text); strings.HasPrefix(body, "[{") || strings.HasPrefix(body, `{"`) {
					t.Errorf("%s:%d tool result Text is JSON: %.70q", path, n+1, ev.Text)
				}
			}
		}
	}
	if results == 0 {
		t.Fatal("no tool results in the corpus")
	}
	if arrays < minArrayToolResults {
		t.Errorf("%d array-content tool results, want at least the %d recorded - the shape this test guards is gone", arrays, minArrayToolResults)
	}
	// The guard has to count the shape, not a substring that happens to be
	// near it. `"content":[{` occurs 185 times in the corpus because it also
	// matches the *message's* content array, which every tool_result frame
	// has - so the first version of this counted 44 where it claimed 10 and
	// would have stayed green with every array fixture deleted.
	if arrays >= results {
		t.Errorf("counted %d array results against %d tool results: the count is matching every result, not the array ones", arrays, results)
	}
}

// arrayToolResults counts the tool_result blocks on one frame whose *own*
// content is an array rather than a string.
//
// It reads the bytes rather than asking the decoder, because the decoder is
// what this guards: a count derived from Event would agree with whatever the
// decoder did. A frame whose message or content is not the object-and-array
// shape yields 0, which is correct - it carries no tool_result block - and
// leaves the corpus-wide minimum to notice if that ever becomes wrong.
func arrayToolResults(t *testing.T, line string) int {
	t.Helper()
	var probe struct {
		Message struct {
			Content []struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return 0
	}
	n := 0
	for _, b := range probe.Message.Content {
		if b.Type == "tool_result" && isJSONArray(b.Content) {
			n++
		}
	}
	return n
}

// I1 over the corpus: exactly the asks that carry agent_id are attributed,
// and the two that do not are not.
func TestOnlyASubagentsPermissionAskIsAttributed(t *testing.T) {
	asks, attributed := 0, 0
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			if wireTypeOf(t, line) != "control_request" {
				continue
			}
			ev := onlyEvent(t, line, n+1)
			if ev.Kind != KindPermissionRequest {
				continue
			}
			asks++
			hasAgentID := strings.Contains(line, `"agent_id":"`)
			if got := ev.Subagent != nil; got != hasAgentID {
				t.Errorf("%s:%d Subagent present = %v, want %v (agent_id on the wire = %v)", path, n+1, got, hasAgentID, hasAgentID)
			}
			if hasAgentID {
				attributed++
				if ev.Subagent.Agent == "" {
					t.Errorf("%s:%d attributed ask carries no agent id", path, n+1)
				}
			}
		}
	}
	if asks < 3 {
		t.Errorf("%d can_use_tool frames in the corpus, want at least the 3 recorded", asks)
	}
	if attributed < minSubagentAsks {
		t.Errorf("%d subagent asks, want at least %d - the fixture carrying agent_id is gone", attributed, minSubagentAsks)
	}
}

// Display has to be resolved for the tool that made the map worth moving.
// Before this, a dispatch rendered as a bare "⏺ Agent" with no argument,
// because the renderer's map was keyed on the advertised name (Task) and the
// wire says Agent.
func TestEveryRecordedDispatchResolvesItsDescription(t *testing.T) {
	dispatches := 0
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			for _, ev := range evs {
				if ev.Kind != KindToolUse || ev.Tool == nil || ev.Tool.Name != "Agent" {
					continue
				}
				dispatches++
				if ev.Tool.Display == "" {
					t.Errorf("%s:%d dispatch resolved no Display: it renders as a bare tool name", path, n+1)
				}
			}
		}
	}
	if dispatches < minDispatches {
		t.Errorf("%d Agent tool calls in the corpus, want at least the %d recorded", dispatches, minDispatches)
	}
}

// The corpus fact the primaryArg map's comment rests on: the tool is named
// Agent on the wire and never Task, so a map keyed on Task would be dead.
func TestTheDispatchToolIsNeverNamedTaskOnTheWire(t *testing.T) {
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			if strings.Contains(line, `"name":"Task"`) {
				t.Errorf(`%s:%d names the tool "Task"; primaryArg's comment says no line in the corpus does`, path, n+1)
			}
		}
	}
}

// parentToolUseOf reads a frame's top-level parent_tool_use_id without
// decoding it, so a test can say what a frame *is* independently of what
// DecodeLine made of it. Present-and-null - which every parent frame carries
// - reads as absent, exactly as the decoder must treat it.
func parentToolUseOf(t *testing.T, line string) (string, bool) {
	t.Helper()
	var probe struct {
		Type   string  `json:"type"`
		Parent *string `json:"parent_tool_use_id"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		t.Fatalf("fixture line is not JSON: %v", err)
	}
	if probe.Type != "assistant" && probe.Type != "user" {
		return "", false
	}
	if probe.Parent == nil || *probe.Parent == "" {
		return "", false
	}
	return *probe.Parent, true
}

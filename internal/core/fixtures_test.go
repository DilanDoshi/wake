// The golden pass over every recorded stream-json line, and the pins tying
// specific recorded frames to specific kinds.
//
// Split out of one 938-line file when it passed this project's 800-line hard
// max - the same limit that split the airlock itself, and it applies to tests
// too. The subagent corpus went to fixtures_subagent_test.go and the
// fixture-reading helpers, which airlock_test.go also uses, to
// fixtures_helpers_test.go.
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
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// maxLineBytes is the contract Task 4's pump sizes its scanner from. It has
// to clear real recorded frames with room to spare, or a large tool result
// truncates into a decode error at exactly the worst moment.
func TestMaxLineBytesClearsTheLongestRecordedFrame(t *testing.T) {
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
	if maxLineBytes <= longest {
		t.Errorf("maxLineBytes = %d, but a recorded frame is already %d bytes", maxLineBytes, longest)
	}
}

// Golden test over every recorded fixture. Globbed, not listed, so a
// fixture added later is covered without editing this test.
func TestDecodeRecordedFixtures(t *testing.T) {
	files := fixtureFiles(t)
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var total, unknown int
			for n, line := range fixtureLines(t, path) {
				evs, err := DecodeLine([]byte(line))
				if err != nil {
					t.Fatalf("%s:%d failed to decode: %v\nline: %s", path, n+1, err, line)
				}
				for _, e := range evs {
					total++
					if e.Kind == KindUnknown {
						unknown++
						t.Errorf("%s:%d decoded as unknown (wire type %q) - the decoder needs a case for it", path, n+1, e.Text)
					}
				}
			}
			if total == 0 {
				t.Fatal("fixture produced no events")
			}
		})
	}
}

// Counting unknowns is not enough: a decoder that got every kind wrong
// would still pass it. These pin the frames the spike proved divergent to
// the kinds they must produce, selected by content so re-recording a
// fixture cannot quietly turn an assertion into a no-op.
func TestFixturesDecodeToTheExpectedKinds(t *testing.T) {
	cases := []struct {
		fixture string
		marker  string
		kind    EventKind
		check   func(t *testing.T, ev Event)
	}{
		{
			fixture: "permission.jsonl",
			marker:  `"type":"control_request"`,
			kind:    KindPermissionRequest,
			check: func(t *testing.T, ev Event) {
				if ev.RequestID == "" {
					t.Error("no RequestID: nothing can correlate the answer")
				}
				if ev.SessionID != "" {
					t.Errorf("SessionID = %q, want empty", ev.SessionID)
				}
				if ev.Tool == nil || ev.Tool.Name != "Write" {
					t.Errorf("tool = %+v, want Write", ev.Tool)
				}
			},
		},
		{
			fixture: "permission-denied.jsonl",
			marker:  `"subtype":"permission_denied"`,
			kind:    KindSystem,
			check: func(t *testing.T, ev Event) {
				// The regression: message is a string here, and a
				// struct-typed field loses the whole frame.
				if ev.Text != "permission_denied" {
					t.Errorf("Text = %q, want the subtype", ev.Text)
				}
			},
		},
		{
			fixture: "compaction.jsonl",
			marker:  `"content":"This session is being continued`,
			kind:    KindUserText,
			check: func(t *testing.T, ev Event) {
				if !strings.Contains(ev.Text, "This session is being continued") {
					t.Errorf("Text = %.60q, want the compaction summary", ev.Text)
				}
			},
		},
		{
			// The frame announces that an id died. It does not carry the
			// one replacing it - see TestSessionResetDoesNotNameItsSuccessor
			// for where the successor actually comes from.
			fixture: "slash-commands.jsonl",
			marker:  `"type":"conversation_reset"`,
			kind:    KindSessionReset,
			check: func(t *testing.T, ev Event) {
				if ev.SessionID != resetDeadSessionID {
					t.Errorf("SessionID = %q, want %q - the id that just ended", ev.SessionID, resetDeadSessionID)
				}
				if ev.Text != "" {
					t.Errorf("Text = %q, want empty: the only id on this frame is new_conversation_id, and that is not the successor", ev.Text)
				}
			},
		},
		{
			fixture: "basic-turn.jsonl",
			marker:  `"type":"rate_limit_event"`,
			kind:    KindRateLimit,
			check: func(t *testing.T, ev Event) {
				if ev.Text != "allowed" {
					t.Errorf("Text = %q, want the status", ev.Text)
				}
			},
		},
		{
			fixture: "tool-use.jsonl",
			marker:  `"type":"tool_result"`,
			kind:    KindToolResult,
			check: func(t *testing.T, ev Event) {
				if strings.HasPrefix(ev.Text, `"`) {
					t.Errorf("Text = %q, want the unquoted string content", ev.Text)
				}
			},
		},
		{
			fixture: "basic-turn.jsonl",
			marker:  `"type":"result"`,
			kind:    KindTurnEnd,
			check:   func(t *testing.T, ev Event) {},
		},
		{
			// A denied tool is not a failed turn: both denial paths close
			// with subtype "success" and is_error false, listing the tool
			// in result.permission_denials. Nothing may report a denial as
			// an errored turn.
			fixture: "permission-deny-response.jsonl",
			marker:  `"type":"result"`,
			kind:    KindTurnEnd,
			check: func(t *testing.T, ev Event) {
				if ev.Text == "" {
					t.Error("turn end lost its final text")
				}
			},
		},
		{
			fixture: "permission-deny-response.jsonl",
			marker:  `"non_execution_kind":"permission-rule"`,
			kind:    KindToolResult,
			check: func(t *testing.T, ev Event) {
				// A client deny reaches the model as a failed tool result
				// carrying the reason verbatim.
				if ev.Tool == nil || !ev.Tool.IsError {
					t.Errorf("tool = %+v, want is_error true", ev.Tool)
				}
				if ev.Text == "" {
					t.Error("deny reason did not survive to the model")
				}
			},
		},

		// --- the interrupt corpus ------------------------------------------
		//
		// Four lifecycle states and four receipt shapes, each pinned to the
		// one recorded line that carries it. Counting unknowns cannot tell
		// these apart: a decoder that mapped every lifecycle frame to
		// KindControlReceipt would score zero unknowns too.
		{
			// The message Wake queued, before anything happened to it.
			fixture: "interrupt-cancel-queued.jsonl",
			marker:  `"command_uuid":"c07ca05a-0add-414b-a0de-05ff3b59bdf8","state":"queued"`,
			kind:    KindMessageState,
			check: func(t *testing.T, ev Event) {
				if ev.Text != "queued" {
					t.Errorf("Text = %q, want the state %q", ev.Text, "queued")
				}
				if ev.MessageID != "c07ca05a-0add-414b-a0de-05ff3b59bdf8" {
					t.Errorf("MessageID = %q, want the command_uuid - without it nothing says which message", ev.MessageID)
				}
				if ev.SessionID == "" {
					t.Error("SessionID lost: a lifecycle frame carries one and the roster needs it")
				}
			},
		},
		{
			// cancel_queued:true destroying that same message. This is the
			// only frame type that reports a queued message being killed.
			fixture: "interrupt-cancel-queued.jsonl",
			marker:  `"command_uuid":"c07ca05a-0add-414b-a0de-05ff3b59bdf8","state":"cancelled"`,
			kind:    KindMessageState,
			check: func(t *testing.T, ev Event) {
				if ev.Text != "cancelled" {
					t.Errorf("Text = %q, want the state %q", ev.Text, "cancelled")
				}
				if ev.MessageID != "c07ca05a-0add-414b-a0de-05ff3b59bdf8" {
					t.Errorf("MessageID = %q, want the cancelled message", ev.MessageID)
				}
			},
		},
		{
			// The contrast: a plain interrupt, and the queued message runs
			// anyway. "started" is how Wake learns it was not spared.
			fixture: "interrupt-queued-survives.jsonl",
			marker:  `"command_uuid":"c011f484-e92c-4cd6-b262-93978c2851e5","state":"started"`,
			kind:    KindMessageState,
			check: func(t *testing.T, ev Event) {
				if ev.Text != "started" {
					t.Errorf("Text = %q, want the state %q", ev.Text, "started")
				}
				if ev.MessageID != "c011f484-e92c-4cd6-b262-93978c2851e5" {
					t.Errorf("MessageID = %q, want the surviving message", ev.MessageID)
				}
			},
		},
		{
			fixture: "interrupt-queued-survives.jsonl",
			marker:  `"command_uuid":"c011f484-e92c-4cd6-b262-93978c2851e5","state":"completed"`,
			kind:    KindMessageState,
			check: func(t *testing.T, ev Event) {
				if ev.Text != "completed" {
					t.Errorf("Text = %q, want the state %q", ev.Text, "completed")
				}
			},
		},
		{
			// The receipt for an interrupt Wake sent. request_id is nested at
			// .response.request_id and the frame carries no session_id at
			// all, so reading the top level loses the only correlator there
			// is - the same trap control_request sets, one level deeper.
			fixture: "interrupt-mid-tool.jsonl",
			marker:  `"request_id":"1fc6c7cc-7865-4242-a251-e76e78a7fc15"`,
			kind:    KindControlReceipt,
			check: func(t *testing.T, ev Event) {
				if ev.RequestID != "1fc6c7cc-7865-4242-a251-e76e78a7fc15" {
					t.Errorf("RequestID = %q, want the nested request_id - the receipt is unattributable without it", ev.RequestID)
				}
				if ev.SessionID != "" {
					t.Errorf("SessionID = %q, want empty - the frame carries none", ev.SessionID)
				}
				if ev.Text != "success" {
					t.Errorf("Text = %q, want the nested subtype", ev.Text)
				}
				if ev.Control == nil {
					t.Fatal("Control is nil: the receipt reported nothing")
				}
				if len(ev.Control.StillQueued) != 0 {
					t.Errorf("StillQueued = %v, want empty", ev.Control.StillQueued)
				}
				if ev.Control.Cancelled != nil {
					t.Errorf("Cancelled = %v, want nil - this request did not ask", ev.Control.Cancelled)
				}
			},
		},
		{
			// A plain interrupt with one message queued: the receipt names
			// the uuid that survived.
			fixture: "interrupt-queued-survives.jsonl",
			marker:  `"still_queued":["c011f484-e92c-4cd6-b262-93978c2851e5"]`,
			kind:    KindControlReceipt,
			check: func(t *testing.T, ev Event) {
				want := []string{"c011f484-e92c-4cd6-b262-93978c2851e5"}
				if !equalStrings(ev.Control.StillQueued, want) {
					t.Errorf("StillQueued = %v, want %v", ev.Control.StillQueued, want)
				}
				if ev.Control.Cancelled != nil {
					t.Errorf("Cancelled = %v, want nil on a plain interrupt", ev.Control.Cancelled)
				}
			},
		},
		{
			// cancel_queued:true. The running message also gets a "cancelled"
			// lifecycle frame, so this array is the only thing that says
			// which uuid the *request* destroyed.
			fixture: "interrupt-cancel-queued.jsonl",
			marker:  `"cancelled":["c07ca05a-0add-414b-a0de-05ff3b59bdf8"]`,
			kind:    KindControlReceipt,
			check: func(t *testing.T, ev Event) {
				want := []string{"c07ca05a-0add-414b-a0de-05ff3b59bdf8"}
				if !equalStrings(ev.Control.Cancelled, want) {
					t.Errorf("Cancelled = %v, want %v", ev.Control.Cancelled, want)
				}
				if len(ev.Control.StillQueued) != 0 {
					t.Errorf("StillQueued = %v, want empty", ev.Control.StillQueued)
				}
			},
		},
		{
			// cancel_queued:true against an empty queue. The key is present
			// and empty, which is not the same as absent: presence tracks
			// what Wake asked for, not what was there. Marshalling this with
			// omitempty would collapse the two.
			fixture: "interrupt-cancel-queued-empty.jsonl",
			marker:  `"still_queued":[],"cancelled":[]`,
			kind:    KindControlReceipt,
			check: func(t *testing.T, ev Event) {
				if ev.Control.Cancelled == nil {
					t.Error("Cancelled = nil, want present-and-empty: the key was on the wire")
				}
				if len(ev.Control.Cancelled) != 0 {
					t.Errorf("Cancelled = %v, want empty", ev.Control.Cancelled)
				}
			},
		},
		{
			// Claude's own abort marker, mid-generation. It arrives as an
			// ordinary user frame - no isSynthetic, no isReplay, the same six
			// keys a genuine user turn has - so nothing on the frame says it
			// is not the human speaking. Resolved here or it is drawn under
			// the user's own label, which is an operator being told they said
			// something they did not.
			fixture: "interrupt-mid-generation.jsonl",
			marker:  `"[Request interrupted by user]"`,
			kind:    KindUserText,
			check: func(t *testing.T, ev Event) {
				if ev.Notice != NoticeTurnInterrupted {
					t.Errorf("Notice = %q, want %q", ev.Notice, NoticeTurnInterrupted)
				}
				if ev.Echoed {
					t.Error("Echoed = true: the frame carries neither isReplay nor isSynthetic, which is exactly why the marker has to be resolved by its text")
				}
			},
		},
		{
			// The other literal, and the reason matching one string is not
			// enough. Same frame shape, different wording, and both are in
			// the 2.1.226 binary.
			fixture: "interrupt-mid-tool.jsonl",
			marker:  `"[Request interrupted by user for tool use]"`,
			kind:    KindUserText,
			check: func(t *testing.T, ev Event) {
				if ev.Notice != NoticeTurnInterrupted {
					t.Errorf("Notice = %q, want %q", ev.Notice, NoticeTurnInterrupted)
				}
			},
		},
		{
			// The withdrawal. An interrupt landing on an outstanding ask
			// kills it, and this frame is the only one that says so: the
			// receipt beneath it names the *interrupt's* request_id and
			// reports nothing about the ask (findings §3), and the aborted
			// result arrives four frames later.
			//
			// Two keys and no more - no session_id, no subtype, no reason -
			// so everything asserted here is an absence except the one field
			// that carries meaning.
			fixture: "interrupt-pending-basic.jsonl",
			marker:  `"type":"control_cancel_request"`,
			kind:    KindRequestWithdrawn,
			check: func(t *testing.T, ev Event) {
				if ev.RequestID == "" {
					t.Error("no RequestID: the frame names one dead request and carries nothing else, so without it the event reports nothing at all")
				}
				if ev.SessionID != "" {
					t.Errorf("SessionID = %q, want empty - the frame carries none, the same as the ask it cancels", ev.SessionID)
				}
				if ev.Text != "" {
					t.Errorf("Text = %q, want empty - there is no subtype on this frame to report", ev.Text)
				}
				if ev.Control != nil {
					t.Errorf("Control = %+v, want nil - a withdrawal is not a receipt and reports no queue", ev.Control)
				}
			},
		},
		{
			// The receipt for an interrupt sent without a request_id. It
			// comes back without one too and is therefore unattributable -
			// which is why Wake always sends one. It must still decode as a
			// receipt: dropping the frame would lose the ack entirely.
			fixture: "interrupt-no-request-id.jsonl",
			marker:  `"subtype":"success","response":{"still_queued":[]}`,
			kind:    KindControlReceipt,
			check: func(t *testing.T, ev Event) {
				if ev.RequestID != "" {
					t.Errorf("RequestID = %q, want empty - the recorded frame has no request_id key", ev.RequestID)
				}
				if ev.Text != "success" {
					t.Errorf("Text = %q, want the nested subtype", ev.Text)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.fixture+" "+c.marker, func(t *testing.T) {
			line, n := findFixtureLine(t, c.fixture, c.marker)
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", c.fixture, n, err)
			}
			for _, ev := range evs {
				if ev.Kind == c.kind {
					c.check(t, ev)
					return
				}
			}
			t.Fatalf("%s:%d produced no %s event: %+v", c.fixture, n, c.kind, evs)
		})
	}
}

// The two ids in the /clear recording. Named because three of the four
// assertions below are about telling them apart.
const (
	// resetDeadSessionID is the id the conversation_reset frame carries: the
	// one that just ended.
	resetDeadSessionID = "fc32ab1e-3bef-4583-8e9f-a2ec66949500"

	// resetSuccessorSessionID is the id every frame after the reset carries.
	// It appears nowhere in the reset frame.
	resetSuccessorSessionID = "6524c398-cff7-4716-8eff-1c1b3d704a31"
)

// A conversation_reset does NOT name the session that replaces it, and the
// field that looks like it does is a trap the recording disproves.
//
// new_conversation_id on slash-commands.jsonl:31 is b3144871..., which
// appears on that one line in all 1004 and never again. The id every later
// frame carries is 6524c398..., first seen on the very next line - a
// hook_started, four frames before the init that also carries it, so even
// "learn the new id from init" misattributes what is between.
//
// The consequence is not cosmetic. Spec §5 makes re-keying the registry on a
// session-id change a hard requirement and --resume <uuid> is how the pool
// wakes a parked session, so a pool that re-keys to the reset frame's
// payload re-keys to a conversation that never existed: --resume targets
// nothing and the real transcript is orphaned on disk under the id Wake
// threw away.
//
// The rule this pins instead: session_id is on every frame, so a change in
// Event.SessionID *is* the re-key signal. The reset only announces that one
// is coming.
//
// The assertion that has to be able to fail is Text == "": the decoder used
// to put new_conversation_id there, and the pin that guarded it checked only
// that Text was non-empty and different from SessionID - both of which the
// wrong value satisfies.
func TestSessionResetDoesNotNameItsSuccessor(t *testing.T) {
	const fixture = "slash-commands.jsonl"
	lines := fixtureLines(t, filepath.Join("..", "..", "testdata", "stream", fixture))
	_, n := findFixtureLine(t, fixture, `"type":"conversation_reset"`)
	if n >= len(lines) {
		t.Fatalf("%s:%d is the last line: nothing records what replaced the id", fixture, n)
	}

	reset := onlyEvent(t, lines[n-1], n)
	if reset.Kind != KindSessionReset {
		t.Fatalf("Kind = %q, want %q", reset.Kind, KindSessionReset)
	}
	if reset.SessionID != resetDeadSessionID {
		t.Errorf("SessionID = %q, want the id that just ended %q", reset.SessionID, resetDeadSessionID)
	}
	if reset.Text != "" {
		t.Errorf("Text = %q, want empty - the reset frame has no successor to report", reset.Text)
	}

	// The corpus fact the rule rests on: the successor is not in the frame
	// at all, so no amount of decoding could recover it from this line.
	if strings.Contains(string(reset.Raw), resetSuccessorSessionID) {
		t.Errorf("%s:%d contains %q: the premise of this test no longer holds", fixture, n, resetSuccessorSessionID)
	}

	// Where it does come from: the next frame, and every frame after it.
	next := onlyEvent(t, lines[n], n+1)
	if next.SessionID != resetSuccessorSessionID {
		t.Errorf("%s:%d SessionID = %q, want the successor %q", fixture, n+1, next.SessionID, resetSuccessorSessionID)
	}
}

// The pins above check one line each. This checks the mapping is a function
// over the whole corpus in both directions: every frame of these two types
// produces exactly one event of the paired kind, and no frame of any other
// type produces one.
//
// Pins alone cannot catch an over-broad case - a decoder that folded
// command_lifecycle into KindControlReceipt as well would satisfy every
// control-receipt pin and still score zero unknowns. Globbed and counted
// rather than hardcoded, so re-recording the corpus cannot turn it into a
// no-op and adding a fixture does not redden it.
func TestInterruptFrameTypesMapOneToOneOntoTheirKinds(t *testing.T) {
	// control_response forks into two kinds, discriminated by payload rather
	// than by wire type - see controlResponseEvent's Rewound branch. The
	// other two wire types here still own exactly one kind each.
	want := map[string][]EventKind{
		"command_lifecycle":      {KindMessageState},
		"control_response":       {KindControlReceipt, KindRewindReceipt},
		"control_cancel_request": {KindRequestWithdrawn},
	}
	owned := map[EventKind]string{}
	for wire, ks := range want {
		for _, k := range ks {
			owned[k] = wire
		}
	}

	seen := map[string]int{}
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			wire := wireTypeOf(t, line)
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			if ks, ok := want[wire]; ok {
				seen[wire]++
				if len(evs) != 1 || !slices.Contains(ks, evs[0].Kind) {
					t.Errorf("%s:%d is a %s frame but decoded to %+v, want one of %v", path, n+1, wire, kinds(evs), ks)
				}
				continue
			}
			for _, ev := range evs {
				if claimed, ok := owned[ev.Kind]; ok {
					t.Errorf("%s:%d is a %s frame but decoded to %s, which belongs to %s", path, n+1, wire, ev.Kind, claimed)
				}
			}
		}
	}

	// A pin over zero lines asserts nothing. The spike counted these.
	for wire, min := range map[string]int{"command_lifecycle": 17, "control_response": 14, "control_cancel_request": 2} {
		if seen[wire] < min {
			t.Errorf("%s: %d frames in the corpus, want at least the %d the spike recorded", wire, seen[wire], min)
		}
	}
}

// recordedWithdrawals is how many control_cancel_request frames the corpus
// carries: one in each of the two collision fixtures, and none anywhere else
// in 1004 lines. The third fixture of that spike is the control - the same
// argv and the same ask, held open 30.1 s with no interrupt - and its absence
// from this count is what says the interrupt is the cause.
const recordedWithdrawals = 2

// The withdrawal names an ask that is already on the wire, and a turn end
// still follows it. Both halves, over the corpus, in arrival order.
//
// This is the property the daemon's `pending` rests on, and **none of it is
// Wake's doing** - it is the CLI's ordering, inherited. Because the withdrawal
// precedes the turn end, `pending` cannot hang: a `result` always arrives to
// clear it. Because the ask is already dead when that `result` lands, `pending`
// cannot clear early either, so a blocked agent is never reported idle while it
// is still blocked. Wake would show both failures if the CLI emitted these two
// frames the other way round, and nothing in Wake would notice.
//
// So this is where a re-recording that broke the ordering has to be seen.
// Asserting it on the frames rather than in a comment is the difference between
// an inherited guarantee and an assumed one - and §7 of the findings note is
// explicit that three recordings of one scenario cannot prove the pairing is
// invariant, which is the other reason to check it against every fixture there
// is rather than the two that motivated it.
func TestAWithdrawalNamesAnEarlierAskAndATurnEndStillFollowsIt(t *testing.T) {
	withdrawals := 0
	for _, path := range fixtureFiles(t) {
		evs := decodeFixture(t, path)
		for i, ev := range evs {
			if ev.Kind != KindRequestWithdrawn {
				continue
			}
			withdrawals++
			if ev.RequestID == "" {
				t.Errorf("%s:%d withdraws a request it does not name: nothing can retire an ask on this", path, ev.line)
				continue
			}
			if !asksFor(evs[:i], ev.RequestID) {
				t.Errorf("%s:%d withdraws %q, which no earlier frame in this file asked for - the id is the only correlator there is, so a client would be retiring an ask it never saw",
					path, ev.line, ev.RequestID)
			}
			if !hasTurnEnd(evs[i+1:]) {
				t.Errorf("%s:%d withdraws an ask and the file ends without a turn end - this is the wedge: the daemon clears what a session owes on KindTurnEnd, so an agent whose ask died with no turn end reads blocked forever",
					path, ev.line)
			}
		}
	}
	if withdrawals != recordedWithdrawals {
		t.Errorf("found %d withdrawals in the corpus, want the %d recorded - if the corpus changed, this test's premise did too", withdrawals, recordedWithdrawals)
	}
}

// asksFor reports whether one of these events is the permission ask the given
// request id belongs to.
func asksFor(evs []decodedLine, requestID string) bool {
	for _, ev := range evs {
		if ev.Kind == KindPermissionRequest && ev.RequestID == requestID {
			return true
		}
	}
	return false
}

func hasTurnEnd(evs []decodedLine) bool {
	for _, ev := range evs {
		if ev.Kind == KindTurnEnd {
			return true
		}
	}
	return false
}

// recordedInterruptMarkers is how many abort markers the corpus carries: five
// [Request interrupted by user] and five [... for tool use], counted over all
// 1004 lines. Both literals matter and neither is rare.
//
// It was three "for tool use" against 919 lines until the permission-collision
// fixtures landed, and both of those carry one - even though **no tool ever ran
// in either**. The interrupt hit an ask that was still waiting to be allowed,
// and the CLI still writes the "for tool use" wording, so that literal does not
// mean a tool was executing. Nothing may read the choice of wording as
// evidence about what the agent was doing.
const recordedInterruptMarkers = 10

// The pins above check one line each, and one line cannot show the resolution
// is a *function of the text* rather than of the fixture it came from.
//
// Both directions, and the second is the one that has teeth. Every user-text
// event whose text is one of the two markers must carry the notice - a match
// on the shorter literal alone leaves three of these eight unresolved, and
// they are the mid-tool ones, the shape an operator produces most. And no
// other event may carry it: this notice is what a view draws instead of the
// speaker's own words, so a marker resolution that spread would silently
// delete somebody's message from their conversation.
func TestTheInterruptMarkersAreResolvedByTheirTextAndNothingElseIs(t *testing.T) {
	markers := 0
	for _, path := range fixtureFiles(t) {
		for n, line := range fixtureLines(t, path) {
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d failed to decode: %v", path, n+1, err)
			}
			for _, ev := range evs {
				isMarker := ev.Kind == KindUserText &&
					(ev.Text == "[Request interrupted by user]" || ev.Text == "[Request interrupted by user for tool use]")
				switch {
				case isMarker:
					markers++
					if ev.Notice != NoticeTurnInterrupted {
						t.Errorf("%s:%d %q decoded with Notice %q, want %q - it will be drawn as the user's own turn",
							path, n+1, ev.Text, ev.Notice, NoticeTurnInterrupted)
					}
				case ev.Notice == NoticeTurnInterrupted:
					t.Errorf("%s:%d carries %q but is a %s reading %.60q - the notice replaces the speaker's words, so it may only ever mean the marker",
						path, n+1, NoticeTurnInterrupted, ev.Kind, ev.Text)
				}
			}
		}
	}
	if markers != recordedInterruptMarkers {
		t.Errorf("found %d interrupt markers in the corpus, want the %d recorded - if the corpus changed, this test's premise did too", markers, recordedInterruptMarkers)
	}
}

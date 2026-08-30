package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// modeReceipt is what the daemon forwards when a set_permission_mode lands: a
// control receipt naming the mode the session ended up in. The label is not
// allowed to move on anything else.
func modeReceipt(sessionID, mode string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &core.Event{
		Kind:           core.KindControlReceipt,
		RequestID:      "r1",
		Text:           "success",
		PermissionMode: mode,
	}}
}

// initReporting is the second observable: every turn's init, carrying the mode
// the session is genuinely in.
func initReporting(sessionID, mode string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: sessionID, Event: &core.Event{
		Kind:           core.KindSystem,
		Text:           "init",
		PermissionMode: mode,
	}}
}

// roomWithPick is the room with three agents and the roster cursor resting on
// the first, which is the state ⇧⇥ needs: the target is the picked agent, and
// only while the activity sidebar is drawn.
//
// The width is a parameter rather than a constant because a second withSize does
// not take: a geometry change goes through one pending value and an 80ms settle,
// so the size a test wants has to be the size it starts at.
func roomWithPick(t *testing.T, width int) App {
	t.Helper()
	a := newRoomApp(t).withSize(width, 40).withAgents("sydney", "alex", "john")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftDown})
	if _, ok := a.pickedAgent(); !ok {
		t.Fatal("no agent is picked: this test would assert nothing")
	}
	return a
}

// modeRoomWidth is wide enough for the roster to be drawn, which is what makes an
// agent pickable at all. The legend's own tail is truncated at this width; only
// the test that reads the legend needs more.
const modeRoomWidth = 200

// TestTheCycleIsClosedAndEveryPositionIsAModeTheCLIAccepts walks the whole
// cycle back to where it started. A cycle with a position the CLI refuses would
// leave the label on a mode the session is not in, which is the defect this
// whole feature exists to delete.
func TestTheCycleIsClosedAndEveryPositionIsAModeTheCLIAccepts(t *testing.T) {
	accepted := map[string]bool{
		core.PermissionModePlan:        true,
		core.PermissionModeAuto:        true,
		core.PermissionModeDefault:     true,
		core.PermissionModeAcceptEdits: true,
	}

	start := spawnedMode
	seen := map[string]bool{}
	mode := start
	for range accepted {
		mode = nextMode(mode)
		if !accepted[mode] {
			t.Fatalf("the cycle reaches %q, which is not a mode the CLI accepts", mode)
		}
		if seen[mode] {
			t.Fatalf("the cycle repeats %q before visiting every position", mode)
		}
		seen[mode] = true
	}
	if mode != start {
		t.Errorf("the cycle ended on %q, want it closed back to %q", mode, start)
	}
}

// TestTheFirstPressTightensRatherThanLoosens is the safety property the old
// broken indicator failed in the unsafe direction: the label that appeared to
// *tighten* permissions was the one that did nothing. Every session spawns
// `auto`, so the first press an operator makes must not be the one that lets an
// agent do more.
//
// It is the *first* press and no longer the whole traversal, which is what
// adopting Claude Code's order cost: the second press, `default` →
// `acceptEdits`, loosens. That trade is argued in mode.go's header. The half
// worth keeping is this one - a key nobody has read the legend for.
func TestTheFirstPressTightensRatherThanLoosens(t *testing.T) {
	if got := nextMode(spawnedMode); got != core.PermissionModeDefault {
		t.Errorf("the first press moves %q to %q, want %q - from the mode agents spawn in, the first press must ask for *more* human involvement, not less",
			spawnedMode, got, core.PermissionModeDefault)
	}
}

// claudeModeCycle is the fixture: Claude Code's own `chat:cycleMode` switch,
// maintained by hand the way the palette is.
type claudeModeCycle struct {
	Version    string            `json:"claude_code_version"`
	Conditions map[string]bool   `json:"conditions"`
	Next       map[string]string `json:"next"`
	Unreach    []string          `json:"unreachable_in_wake"`
}

// TestTheCycleIsClaudeCodesOwnCycle holds nextMode to the switch rather than to
// a list somebody typed twice. ⇧⇥ is the one key Wake and Claude Code agree on,
// and the agreement is worth nothing if the two walk different orders.
//
// **bypassPermissions is excluded and that is the domain rather than a gap.**
// The CLI refuses it unless the process was launched
// --dangerously-skip-permissions (findings §7) and nothing in this tree passes
// that, so it is not a position ⇧⇥ can be pressed *from*. Asserting an answer
// for it would be a guard over a state that cannot arrive, which this project's
// own rule calls a defect. The exclusion is read off the fixture and checked
// below, so a second one cannot be added quietly.
func TestTheCycleIsClaudeCodesOwnCycle(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-mode-cycle.json"))
	if err != nil {
		t.Fatalf("read the recorded cycle: %v", err)
	}
	var rec claudeModeCycle
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode the recorded cycle: %v", err)
	}

	// The conditions the fixture was read under are Wake's, and a fixture
	// recorded under different ones answers a different question.
	if rec.Conditions["isBypassPermissionsModeAvailable"] {
		t.Fatalf("the fixture was recorded with bypassPermissions available; no Wake session is, because nothing here passes --dangerously-skip-permissions")
	}
	if !rec.Conditions["canCycleToAuto"] {
		t.Fatalf("the fixture was recorded with auto unavailable, and auto is the mode every Wake session spawns in")
	}
	if want := []string{"bypassPermissions"}; !slices.Equal(rec.Unreach, want) {
		t.Fatalf("the fixture excludes %v from the comparison, want exactly %v: an exclusion is how a real disagreement gets to look like agreement", rec.Unreach, want)
	}

	tested := 0
	for mode, want := range rec.Next {
		if slices.Contains(rec.Unreach, mode) {
			continue
		}
		if got := nextMode(mode); got != want {
			t.Errorf("nextMode(%q) = %q; claude %s cycles it to %q", mode, got, rec.Version, want)
		}
		tested++
	}
	if tested != len(rec.Next)-len(rec.Unreach) {
		t.Fatalf("compared %d modes of %d: the loop skipped one silently", tested, len(rec.Next)-len(rec.Unreach))
	}
	// And the floor: a fixture that lost its map would pass every assertion
	// above by testing nothing, which reads as the strongest possible result.
	if tested == 0 {
		t.Fatal("the fixture names no mode, so this test compared nothing")
	}
}

// TestDontAskIsNotACyclePositionAndExitsToDefault is the one place Wake was
// asked to widen the cycle further than Claude Code widens it, and did not.
//
// `dontAsk` is reachable - the CLI accepts it, and a permission suggestion can
// land a session in it - but Claude Code's switch does not cycle *into* it, and
// a blind key that reaches the least-asking mode is what mode.go's original
// ruling refused on its own. Two designs reaching the same answer is the
// argument. So it stays an exit: a session that got there some other way
// rejoins the cycle at `default` on the next press rather than being stuck.
//
// It costs no branch. `default` is position 0, so nextMode's existing
// off-the-cycle fallback *is* the switch's `case "dontAsk"` arm.
func TestDontAskIsNotACyclePositionAndExitsToDefault(t *testing.T) {
	if slices.Contains(modeCycle, core.PermissionModeDontAsk) {
		t.Fatalf("modeCycle holds %q: ⇧⇥ has no confirmation, and this is the mode that asks for none", core.PermissionModeDontAsk)
	}
	if got := nextMode(core.PermissionModeDontAsk); got != core.PermissionModeDefault {
		t.Errorf("nextMode(%q) = %q, want %q - a session that reached it another way has to be able to press its way out",
			core.PermissionModeDontAsk, got, core.PermissionModeDefault)
	}
}

// TestShiftTabAsksTheDaemonForThePickedAgentsMode pins the frame: the right
// kind, the right session, and a mode on it. FrameMode is refused without one.
func TestShiftTabAsksTheDaemonForThePickedAgentsMode(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	next, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	frames := sentFrames(t, next, cmd)

	if len(frames) != 1 {
		t.Fatalf("⇧⇥ wrote %d frames, want exactly 1: %+v", len(frames), frames)
	}
	if frames[0].Kind != rpc.FrameMode {
		t.Errorf("frame kind = %q, want %q", frames[0].Kind, rpc.FrameMode)
	}
	if frames[0].SessionID != picked.ID {
		t.Errorf("frame went to %q, want the picked agent %q", frames[0].SessionID, picked.ID)
	}
	if frames[0].Mode != nextMode(spawnedMode) {
		t.Errorf("frame asked for %q, want %q", frames[0].Mode, nextMode(spawnedMode))
	}
}

// TestTheLabelMovesOnTheReceiptAndNotOnTheKeystroke is the rule the design
// calls 2b, and it is the whole difference between this and what was deleted.
//
// The keystroke is an ask. The daemon's answer is the confirmation. A label
// that moved on the press would be right only when nothing went wrong - and
// `manual` normalizing to `default` guarantees it would be wrong on a real
// position rather than in principle.
func TestTheLabelMovesOnTheReceiptAndNotOnTheKeystroke(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	pressed, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := pressed.modeOf(picked.ID); got != spawnedMode {
		t.Errorf("the label moved to %q on the keypress; it must stay %q until the receipt says otherwise", got, spawnedMode)
	}

	settled := pressed.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))
	if got := settled.modeOf(picked.ID); got != core.PermissionModePlan {
		t.Errorf("after the receipt the label is %q, want %q", got, core.PermissionModePlan)
	}
}

// TestTheLabelFollowsTheReceiptWhenItDisagreesWithWhatWasAsked is findings §6
// driven through the UI. The receipt is the authority, so a receipt naming a
// mode nobody asked for still moves the label - that is what makes it a receipt
// rather than an acknowledgement.
func TestTheLabelFollowsTheReceiptWhenItDisagreesWithWhatWasAsked(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	pressed, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	settled := pressed.applyFrame(modeReceipt(picked.ID, core.PermissionModeDefault))

	if got := settled.modeOf(picked.ID); got != core.PermissionModeDefault {
		t.Errorf("label = %q, want the receipt's %q rather than anything derived from the key",
			got, core.PermissionModeDefault)
	}
}

// TestAnInitCorrectsABeliefThatWentStale is the second observable, and it is
// what makes "say it" safe rather than hopeful (design §2c).
//
// A mode set mid-session does not survive a park and wake: the woken process
// comes back in its spawn mode and no receipt says so. init does, on every
// turn. Path B is the same shape - a mode changed from inside a permission
// allow produces no receipt at all.
func TestAnInitCorrectsABeliefThatWentStale(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	believing := a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))
	if got := believing.modeOf(picked.ID); got != core.PermissionModePlan {
		t.Fatalf("label = %q, want %q before the correction", got, core.PermissionModePlan)
	}

	woken := believing.applyFrame(initReporting(picked.ID, core.PermissionModeAuto))
	if got := woken.modeOf(picked.ID); got != core.PermissionModeAuto {
		t.Errorf("label = %q after an init reporting %q: a belief that outlived the process it was about is the sentence this feature exists to delete",
			got, core.PermissionModeAuto)
	}
}

// TestOneAgentsModeIsNotEveryAgentsMode is design §4's first rule: there is no
// @all for this. Thirty agents' permission modes moved by one keystroke is the
// failure the per-agent card exists to prevent, arriving one level up.
func TestOneAgentsModeIsNotEveryAgentsMode(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	settled := a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))

	for _, other := range settled.fleet.Agents() {
		if other.ID == picked.ID {
			continue
		}
		if got := settled.modeOf(other.ID); got != spawnedMode {
			t.Errorf("%s is in %q after a receipt about %s: a mode is one agent's", other.Name, got, picked.Name)
		}
	}
}

// TestShiftTabRefusesAnAgentBlockedOnAnAsk is the ⌃C rule for the state
// findings §9 item 3 leaves unrecorded: what happens to an outstanding
// permission ask when the mode changes under it has no bytes behind it, and
// ⇧⇥ is a key an operator can press at any moment.
//
// So it refuses and says when it would work, rather than sending a frame whose
// effect on the card in front of the operator nobody has recorded. This is
// parkWouldDeny's shape one key over.
func TestShiftTabRefusesAnAgentBlockedOnAnAsk(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: picked.ID, Name: picked.Name, State: rpc.StateBlocked},
		},
	}})

	next, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	if cmd != nil {
		t.Errorf("⇧⇥ wrote %+v for an agent blocked on a permission ask; the effect on an outstanding ask is unrecorded",
			sentFrames(t, next, cmd))
	}
	if got := next.modeOf(picked.ID); got != spawnedMode {
		t.Errorf("label = %q, want it untouched at %q by a key that refused", got, spawnedMode)
	}
	if n, said := notice.Latest(); !said || !strings.Contains(n.Text, "permission request") {
		t.Errorf("the refusal said %q; a key that does nothing has to say when it would work", n.Text)
	}
}

// TestTheHintLineShowsTheModeOfTheAgentShiftTabWouldAct is design §4's last
// rule: the mode belongs on screen wherever it is in force. A label showing one
// agent's mode beside a key that acts on another is the lie in a new place.
// TestAWokenSessionSaysItsModeWentBackToTheSpawnMode is the design's §2c
// ruling, and the reason shipping without one would be worse than shipping
// nothing.
//
// A mode set mid-session is a property of the *process*, not of the session:
// --resume does not carry it, so a woken agent comes back in its spawn mode
// (findings §8). The owner's ruling is "say it" rather than persist it - the
// park book stays minimal - and a revert nobody is told about is the same
// silent lie one layer along.
//
// It fires only when the belief and the spawn mode disagree. A session that was
// never moved off `auto` comes back on `auto`, and a notice about that is a row
// spent on nothing.
func TestAWokenSessionSaysItsModeWentBackToTheSpawnMode(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	moved := a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))
	woken := moved.awaitingWake(picked.ID).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: picked.ID, Name: picked.Name, State: rpc.StateIdle}},
	}})

	if got := woken.modeOf(picked.ID); got != spawnedMode {
		t.Errorf("a woken session is believed to be in %q; --resume does not carry a mode, so it came back in %q",
			got, spawnedMode)
	}
	if n := notice.Count(fmt.Sprintf(modeRevertedFormat, agentPrefix, picked.Name, spawnedMode)); n != 1 {
		t.Errorf("a woken session reverted from %q to %q and said so %d times",
			core.PermissionModePlan, spawnedMode, n)
	}
}

// And it stays quiet for the session that never moved, which is most of them.
func TestAWokenSessionInTheSpawnModeSaysNothingAboutIt(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	woken := a.awaitingWake(picked.ID).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: picked.ID, Name: picked.Name, State: rpc.StateIdle}},
	}})

	if n := notice.Count(fmt.Sprintf(modeRevertedFormat, agentPrefix, picked.Name, spawnedMode)); n != 0 {
		t.Errorf("a session that was never moved off %q was told it reverted to it", spawnedMode)
	}
	_ = woken
}

// TestTwoPressesAdvanceTwoPositions is the half "wait for the receipt" gets
// wrong if it is the *only* rule.
//
// ⇧⇥ is a cycle key and cycle keys get mashed. Computing the next mode from the
// confirmed belief alone means two presses inside one round trip both read the
// same starting point and both ask for the same thing - the second press is
// swallowed, and the operator who wanted plan lands on default and cannot tell
// why.
//
// The fix does not weaken the receipt rule. The *label* still moves only when
// the daemon answers; what the second press reads is what this client already
// asked for and has not been answered about yet. Intent is the client's; truth
// is the receipt's.
func TestTwoPressesAdvanceTwoPositions(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	first, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	one := sentFrames(t, first, cmd)
	second, cmd := pressKey(first, tea.KeyMsg{Type: tea.KeyShiftTab})
	two := sentFrames(t, second, cmd)

	if len(one) != 1 || len(two) != 1 {
		t.Fatalf("wrote %d then %d frames, want one each", len(one), len(two))
	}
	wantFirst := nextMode(spawnedMode)
	if one[0].Mode != wantFirst {
		t.Fatalf("the first press asked for %q, want %q", one[0].Mode, wantFirst)
	}
	if two[0].Mode != nextMode(wantFirst) {
		t.Errorf("the second press asked for %q, want %q - it read the unanswered belief and asked for the same position twice, so one press did nothing",
			two[0].Mode, nextMode(wantFirst))
	}

	// And the label has still not moved, because nothing has answered.
	if got := second.modeOf(picked.ID); got != spawnedMode {
		t.Errorf("label = %q after two presses and no receipt, want %q", got, spawnedMode)
	}
}

// The receipt is what ends the run: once the daemon has answered, the next press
// cycles from what it said rather than from what was asked. That is what keeps a
// normalized answer - `manual` coming back `default` - from being cycled past.
func TestAReceiptEndsTheUnansweredRun(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	pressed, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyShiftTab})
	settled := pressed.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))

	next, cmd := pressKey(settled, tea.KeyMsg{Type: tea.KeyShiftTab})
	frames := sentFrames(t, next, cmd)
	if len(frames) != 1 {
		t.Fatalf("wrote %d frames, want 1", len(frames))
	}
	if want := nextMode(core.PermissionModePlan); frames[0].Mode != want {
		t.Errorf("after a receipt saying %q the next press asked for %q, want %q - it cycled from what it asked rather than from what it was told",
			core.PermissionModePlan, frames[0].Mode, want)
	}
}

// TestADroppedFrameForgetsTheModeRatherThanKeepingAStaleOne is the failure a
// belief built only from events has, and it is the one that fails in the unsafe
// direction.
//
// The daemon drops frames for a client whose queue is full (client.enqueue), and
// the receipt is a frame like any other. Lose one and this client goes on
// showing the mode the agent used to be in - and the dangerous half is which
// way: showing `plan` for an agent now running `auto` tells an operator their
// agent will ask before it edits, and it will not. That is deferred I7's own
// sentence, reached through the fix for it.
//
// So a reported gap forgets every belief instead of keeping one that may be
// stale. What is left is spawnedMode, which is a claim too - but it is the
// **loosest** of the three, so the residual error is "Wake says it acts freely
// and it is actually planning", which surprises nobody into losing a repository.
// The next turn's init repairs it exactly.
func TestADroppedFrameForgetsTheModeRatherThanKeepingAStaleOne(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	moved := a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))
	if got := moved.modeOf(picked.ID); got != core.PermissionModePlan {
		t.Fatalf("label = %q before the gap, want %q", got, core.PermissionModePlan)
	}

	m, _ := moved.Update(streamMsg{batch: batch{dropped: 3}, gen: moved.gen})
	if got := m.(App).modeOf(picked.ID); got != spawnedMode {
		t.Errorf("after %d dropped frames the label is still %q. One of them may have been the receipt that moved it, and a mode kept across a gap is a mode this window cannot vouch for",
			3, got)
	}
}

// The same hole, reached the other way: a reattach is a new connection, and
// everything that happened while this client was gone happened without it. The
// fleet report it comes back with carries no permission mode, so a belief that
// survived the disconnection is one nothing can confirm.
func TestAReattachForgetsTheModeItRememberedAcrossTheDisconnection(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	moved := a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))
	back, _ := moved.Update(reattachedMsg{})

	if got := back.(App).modeOf(picked.ID); got != spawnedMode {
		t.Errorf("a reattached window still believes %q. The connection that would have carried a change was not there to carry it", got)
	}
}

// TestARefusedControlRequestIsReported closes the gap between decoding a
// refusal and doing anything with it.
//
// A refused control_request comes back as a receipt with subtype "error" and the
// CLI's reason - an unknown mode, or bypassPermissions on a session not launched
// dangerously. The daemon's write *succeeded*, so no error frame is sent and
// nothing else on this path says a word. Without this the operator's key does
// nothing, the label does not move, and there is no account of either, which is
// the silence design §4 forbids.
func TestARefusedControlRequestIsReported(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()

	refused := rpc.Frame{Kind: rpc.FrameEvent, SessionID: picked.ID, Event: &core.Event{
		Kind:      core.KindControlReceipt,
		RequestID: "r9",
		Text:      "error",
		Control:   &core.ControlResult{Error: "Cannot set permission mode: must be one of acceptEdits, auto, bypassPermissions, default, dontAsk, plan"},
	}}
	after := a.applyFrame(refused)

	n, said := notice.Latest()
	if !said || !strings.Contains(n.Text, "must be one of") {
		t.Errorf("a refused control request reported %q; the CLI's own reason is the only account of why the key did nothing", n.Text)
	}
	if !strings.Contains(n.Text, picked.Name) {
		t.Errorf("the refusal is %q and does not name the agent: at 30 agents that is a sentence about none of them", n.Text)
	}
	if got := after.modeOf(picked.ID); got != spawnedMode {
		t.Errorf("label = %q after a refusal, want it untouched at %q - a refusal moved nothing", got, spawnedMode)
	}
}

// TestABlurredComposerNamesNoMode is the other half of "the value must match the
// key beside it".
//
// With both panes drawn, two legends are on screen. The room's names the mode
// ⇧⇥ would act on; a DM's would name its own agent's - and when the focus is in
// the room those are different agents, so one visible `permissions: …` describes
// something the key beside it will not touch.
//
// The pane without the keys says nothing about the mode. Every other glyph in a
// blurred legend already refers to the focused target, so the mode is the one
// entry that could be read as a claim about the pane it is drawn in.
func TestABlurredComposerNamesNoMode(t *testing.T) {
	blurred := NewComposer().Focused(false).WithMode(core.PermissionModePlan)
	if got := stripANSI(blurred.View(fullLegendWidth)); strings.Contains(got, core.PermissionModePlan) {
		t.Errorf("a blurred composer names a mode:\n%s", got)
	}
	focused := NewComposer().Focused(true).WithMode(core.PermissionModePlan)
	if got := stripANSI(focused.View(fullLegendWidth)); !strings.Contains(got, core.PermissionModePlan) {
		t.Errorf("the focused composer does not name its mode:\n%s", got)
	}
}

// The terminal is wide enough for the mode to survive the truncation: the
// legend is cut to the pane, and the tail is the first entry cut. A narrower
// window would make this test measure the truncation rather than the label.
func TestTheHintLineShowsTheModeOfTheAgentShiftTabWouldAct(t *testing.T) {
	a := roomWithPick(t, legendFitsAtTerminalWidth(t))
	picked, _ := a.pickedAgent()

	settled := a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))

	want := fmt.Sprintf(modeFormat, core.PermissionModePlan)
	if got := shown(settled); !strings.Contains(got, want) {
		t.Errorf("the screen does not say %q after the picked agent reached it:\n%s", want, got)
	}
}

// The mode is the agent's own word, and two surfaces draw it now.
//
// It arrives on a receipt or an init and nothing between the CLI and App.modes
// constrains it to the four the cycle walks - so a newline in it draws a legend
// row nobody counted, which is the pane-taller-than-its-box failure chromeHeight
// exists for, and a control sequence rewrites what the terminal has already
// drawn. Flattened once at the writer, so the bar and the legend agree by
// construction rather than by both remembering to.
//
// Measured against the same pane holding a benign mode rather than against a
// row count, so it stays true if either surface gains a row for its own reasons.
func TestAHostileModeAddsNoRowToTheBarOrTheLegend(t *testing.T) {
	for _, hostile := range []string{"plan\nauto", "plan\rauto", "plan\x1b[2Kauto", "plan\tauto"} {
		t.Run(hostile, func(t *testing.T) {
			benign := paneInMode(t, core.PermissionModePlan)
			evil := paneInMode(t, hostile)

			if got := evil.Composer().Mode(); strings.IndexFunc(got, unicode.IsControl) >= 0 {
				t.Errorf("the belief kept a control character: %q", got)
			}
			if got, want := lipgloss.Height(evil.bar), lipgloss.Height(benign.bar); got != want {
				t.Errorf("the bar drew %d rows against %d for a benign mode:\n%q", got, want, evil.bar)
			}
			got := lipgloss.Height(evil.Composer().View(fullLegendWidth))
			if want := lipgloss.Height(benign.Composer().View(fullLegendWidth)); got != want {
				t.Errorf("the legend drew %d rows against %d for a benign mode", got, want)
			}
		})
	}
}

// paneInMode is a conversation whose agent has reported mode, drawn the way
// App.dmPane draws it.
func paneInMode(t *testing.T, mode string) DM {
	t.Helper()
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	a = a.applyFrame(modeReceipt(picked.ID, mode))
	return a.dmFor(picked.ID).withBar(fullLegendWidth)
}

//go:build live && unix

// The one test in this tree that spends money.
//
// Everything else replays recorded JSONL, on CLAUDE.md's rule that a live model
// is slow, nondeterministic and costs money per CI run. That rule is about the
// *suite*: this file is behind a build tag, is excluded from `go test ./...` and
// from `make ci`, and runs only when somebody types `make live`. The rationale
// survives intact - nothing here ever runs on a gate.
//
// What it buys is the half no fixture can prove. A recording answers "does the
// airlock decode this frame"; it cannot answer "does a real model's markdown
// render without dropping a row", "do two agents working at once leave the room
// readable", or "does an AskUserQuestion answered with 1 actually reach the
// model". Those are the claims here.
//
// # The trap this file exists inside, and how every assertion avoids it
//
// **A room echoes what you typed.** The first version of this test asked an
// agent to "reply with exactly the word PONG" and then waited for "PONG" - which
// was already on screen, in the operator's own echoed draft, before any model
// had been reached. Every phase passed in under a second, the whole journey ran
// in 8.45s against agents that never left `idle`, and it reported success.
//
// So there are two rules here, and they are not style:
//
//  1. **An expected string may never appear in the prompt that asks for it.**
//     Every prompt below asks for a *transformation* - PONG backwards, six times
//     seven, the Greek letters - so the answer is a string this test never typed
//     and the echo cannot satisfy it.
//  2. **State claims are read off the awareness strip, not off content.**
//     `working`, `need you` and `idle` are internal/ui's own words for what an
//     agent is doing (stateLabel), so "both agents ran at once" and "an agent is
//     blocked on an ask" are structural rather than a guess about phrasing.
//
// # Every phase asserts *and* dumps
//
// A phase that can be decided mechanically is asserted with t.Errorf rather
// than t.Fatalf, and the journey continues either way. That is deliberate: this
// test is read as much as it is run, and stopping at the first failure throws
// away every frame after it - which is the evidence somebody came for. A hard
// stop is reserved for a broken setup, where later phases would report noise.
//
// # Cost
//
// Measured, not estimated: one trivial turn against this machine's default model
// billed **$0.21**, of which essentially all was 20,246 cache-creation tokens
// from the operator's SessionStart hooks - the output was six tokens. So spend
// is dominated by the *first* turn of each session and is nearly flat in what is
// asked afterwards, which is why this journey reuses two agents rather than
// spawning per phase.
//
// A spawned agent that is never addressed makes no API call at all, so the
// manager the room seats by default is free as long as nothing addresses it -
// which is why every message below names an agent. An unaddressed room draft
// goes to the manager and would be a turn nobody wanted.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hinshun/vt10x"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/daemon"
)

const (
	// liveTimeout bounds a wait on a real model. screenTimeout is 20s, tuned for
	// a fake that answers within a frame; a live turn that has to start a
	// process, run hooks and reach an API is a different order of magnitude.
	liveTimeout = 150 * time.Second

	// liveBudget is the ceiling handed to each agent `/new` starts here. A hard
	// rail rather than a hope: nothing in this file can spend past it, whatever
	// a model decides to do with a prompt.
	liveBudget = "0.50"

	// liveCols and liveRows are a realistic terminal rather than a generous one.
	// CLAUDE.md's own measurement is that the whole legend needs 352 columns, so
	// at 200 it truncates - which is the case an operator actually sees, and the
	// case docs/notes/bugs.md BUG-1 was reported from.
	liveCols, liveRows = 200, 50
)

// realClaude is where the actual binary lives, as against whatever `claude` on
// PATH resolves to today.
//
// On this machine PATH's `claude` is a cmux wrapper shim in a temp directory
// that strips itself out and re-execs the real one. Transparent in practice,
// but it makes the binary under test depend on which editor happens to be
// installed - so this resolves the real path and puts *that* on PATH, the same
// substitution withScriptedAgent makes in the other direction.
func realClaude(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join(os.Getenv("HOME"), ".local/bin/claude"),
		filepath.Join(os.Getenv("HOME"), ".claude/local/claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	t.Skip("no real claude binary found - this test spends money and needs one")
	return ""
}

// withRealAgent is withScriptedAgent's opposite: it pins `claude` to the real
// binary for the daemon this test forks.
//
// HOME is left alone. The credentials are in the macOS keychain and a sterile
// HOME cannot authenticate, so a live run is a real HOME run - which also means
// the operator's SessionStart hooks fire, and the cost note above is what they
// cost.
func withRealAgent(t *testing.T) {
	t.Helper()
	// The live journey is the one place that must run the real supervised path
	// this branch ships, so it clears the direct-launcher default TestMain sets
	// for the flaky pty suite. Every wake binary this test starts then runs each
	// agent under the supervisor against the real claude.
	t.Setenv(daemon.DirectAgentLauncherEnv, "")
	dir := t.TempDir()
	if err := os.Symlink(realClaude(t), filepath.Join(dir, "claude")); err != nil {
		t.Fatalf("link the real agent: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// awaitLive waits for text without killing the journey when it never arrives.
//
// s.await calls Fatalf on s.t, which is the parent here - one missing string
// would end every phase after it, and the frames those phases would have dumped
// are the point of running this at all.
func awaitLive(t *testing.T, s *screen, what, want string) bool {
	t.Helper()
	for deadline := time.Now().Add(liveTimeout); time.Now().Before(deadline); {
		if strings.Contains(s.text(), want) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("%s: %q never appeared within %s.\n%s", what, want, liveTimeout, s.dump())
	return false
}

// seen is awaitLive without the verdict, for a transient state that may have
// been and gone by the time it is asked about.
func seen(s *screen, want string, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); {
		if strings.Contains(s.text(), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// gone is seen's opposite: it waits for something to stop being on screen.
//
// Needed because the strip names every state at once - `1 need you · 2 idle` -
// so "has this agent come back" cannot be asked by waiting for a word that two
// uninvolved agents are already contributing.
func gone(s *screen, want string, within time.Duration) bool {
	for deadline := time.Now().Add(within); time.Now().Before(deadline); {
		if !strings.Contains(s.text(), want) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// openAgentAny is screen.openAgent without its assumption that the agent is
// idle.
//
// openAgent looks for the row "○ name", so it fails on exactly the agent this
// journey most wants to open - one that is working, or blocked on an ask. The
// glyph is the state and is not part of the identity, so this matches the
// roster's own row shape and ignores it.
func openAgentAny(t *testing.T, s *screen, name string) {
	t.Helper()
	for y, line := range s.lines() {
		if m := rosterRow.FindStringSubmatch(line); m != nil && m[1] == name {
			s.click(s.cols-3, y)
			s.settle()
			return
		}
	}
	t.Errorf("no roster row for %q in any state.\n%s", name, s.dump())
}

// focusRoom puts the keys back on the group chat.
//
// `/new` opens no pane and leaves the keys on whichever pane already had them
// (starts.go's draftMention), so this is not undoing a spawn - it is undoing
// phase 6 below, which opens the blocked agent's own conversation to answer
// its card and would otherwise leave every later phase typing into that pane
// instead of the room. Clicking the room's own composer is the shortest way
// back that does not depend on how many panes are open.
func focusRoom(t *testing.T, s *screen) {
	t.Helper()
	row := s.rowOf("group chat")
	if row < 0 {
		t.Fatalf("no room on screen to focus.\n%s", s.dump())
	}
	s.click(40, row+1)
	s.settle()
}

// liveSay types a room message and sends it. Always addressed: see the cost note.
func liveSay(s *screen, text string) {
	s.send(text + "\r")
}

// TestLiveJourney walks one fleet through every feature that needs a real model.
//
// One test rather than one per feature, because the expensive part is the
// session and the phases share it. Ordering is a dependency, not a preference:
// nothing can be asked of an agent that has not been named yet.
func TestLiveJourney(t *testing.T) {
	withRealAgent(t)
	s := startWake(t, liveCols, liveRows)

	// Setup is the one hard stop. A room that never drew, or a fleet with no
	// agent in it, makes every phase below report noise about a screen that was
	// never there.
	if !awaitLive(t, s, "setup", "group chat") {
		t.Fatal("the room never drew - nothing below would mean anything")
	}
	s.settle()
	t.Logf("PHASE 1 - the room, at %dx%d\n%s", liveCols, liveRows, s.dump())

	agents := agentsOnRoster(s)
	if len(agents) == 0 {
		t.Fatalf("first run spawned no agent.\n%s", s.dump())
	}
	first := agents[0]
	t.Logf("first-run agent: %q", first)

	// --- 2. a second agent, capped ------------------------------------------
	t.Run("spawn", func(t *testing.T) {
		liveSay(s, "/new --max-budget-usd "+liveBudget)
		for deadline := time.Now().Add(liveTimeout); time.Now().Before(deadline); {
			if len(agentsOnRoster(s)) > 1 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if got := agentsOnRoster(s); len(got) < 2 {
			t.Errorf("wanted a second agent, roster is %v.\n%s", got, s.dump())
		}
		// /new drafts a mention instead of opening a pane (starts.go's
		// draftMention), so the room is left holding "@<name> " - and esc
		// clears that in one press, settled before the next byte, or
		// bubbletea reads the two as one alt-modified key
		// (escprobe_test.go). Without it phase 3's liveSay below types its
		// own message onto the end of this one's leftover mention.
		s.send("\x1b")
		s.settle()
		t.Logf("PHASE 2 - two agents\n%s", s.dump())
	})

	agents = agentsOnRoster(s)
	second := first
	if len(agents) > 1 {
		second = agents[1]
	}

	// --- 3. a turn at all, and what the room does with a reply ---------------
	//
	// GNOP is never typed: the prompt asks for PONG reversed. That is the whole
	// discriminator between this passing and the echo satisfying it.
	t.Run("reply", func(t *testing.T) {
		focusRoom(t, s)
		liveSay(s, "@"+first+" Reply with the word PONG spelled backwards. Nothing else.")

		if !seen(s, "working", liveTimeout) {
			t.Errorf("%s never entered `working` - the turn never started.\n%s", first, s.dump())
		}
		awaitLive(t, s, "reply", "GNOP")
		s.settle()
		t.Logf("PHASE 3 - a reply in the room\n%s", s.dump())
	})

	// --- 4. a tool call, and the rollup line PR #68 added ---------------------
	//
	// 42 is never typed; 6 and 7 are. The agent spawns in `auto`, so this needs
	// no permission - what is under test is the rendering, not the ask.
	t.Run("tool", func(t *testing.T) {
		focusRoom(t, s)
		liveSay(s, "@"+first+" Run exactly this bash command, then reply with only its output: echo $((6*7))")
		awaitLive(t, s, "tool", "42")
		s.settle()
		t.Logf("PHASE 4 - a tool call and its rollup\n%s", s.dump())
		t.Logf("PHASE 4 styled\n%s", s.dumpStyled())
	})

	// --- 5. two agents working at once ---------------------------------------
	//
	// The claim is concurrency, so the evidence is the strip saying two are
	// working rather than two answers arriving eventually. 14 is never typed.
	//
	// **The turn is made slow on purpose, and that is what makes the claim
	// testable rather than lucky.** The first version asked for arithmetic and
	// polled for `2 working`: both agents answered correctly, but one finished
	// before the other was drawn as started, so the overlap the assertion is
	// about never existed to be sampled and the phase failed after its full
	// timeout - on a run where concurrency demonstrably worked. Sampling a
	// transient state is only sound if the state is guaranteed to last longer
	// than the poll, so the sleep buys a window neither the scheduler nor the
	// model's own speed can close. It costs wall time and no tokens.
	t.Run("concurrent", func(t *testing.T) {
		if second == first {
			t.Skip("only one agent - the second spawn failed")
		}
		focusRoom(t, s)
		liveSay(s, "@all Run exactly this bash command, then reply with only its output: sleep 6; echo $((7*2))")

		// A shorter bound than liveTimeout: with a six-second turn guaranteed on
		// both sides, an overlap that has not appeared inside a minute is a real
		// failure rather than a slow model, and waiting the full 150s only makes
		// the report slower.
		if !seen(s, "2 working", 60*time.Second) {
			t.Errorf("the strip never showed two agents working at once, on a turn built to keep both busy for six seconds.\n%s", s.dump())
		}
		t.Logf("PHASE 5 - mid-flight\n%s", s.dump())

		awaitLive(t, s, "concurrent", "14")
		s.settle()
		t.Logf("PHASE 5 - after both answered\n%s", s.dump())
	})

	// --- 6. the human in the loop --------------------------------------------
	//
	// The highest-value phase here. CLAUDE.md records that a bare allow is right
	// for a permission and *wrong* for a question: the answer rides in
	// updatedInput.answers, and without it the model is told nobody answered on a
	// turn that still ends success.
	//
	// `need you` is stateLabel[StateBlocked] - the fleet's own word for an agent
	// waiting on a human, and a string no prompt here contains. That is the proof
	// the ask reached a surface; the frames are the proof of what it looked like.
	t.Run("question", func(t *testing.T) {
		focusRoom(t, s)
		liveSay(s, "@"+first+" Use the AskUserQuestion tool to ask me to choose between exactly two drinks. Do nothing else afterwards.")

		if !awaitLive(t, s, "question", "need you") {
			return
		}
		s.settle()
		t.Logf("PHASE 6 - blocked, as the room reports it\n%s", s.dump())

		// The room announces and the pane offers, so the ask has to be opened
		// before a key can reach it. `need you` above is the room's half - a
		// line in its transcript carrying no keys - and it is deliberately what
		// this waits on, because it is the only surface that says a *fleet* has
		// somebody waiting. The card is in the agent's own conversation.
		openAgentAny(t, s, first)
		s.settle()
		t.Logf("PHASE 6 - the card, in its own agent's pane\n%s", s.dump())
		t.Logf("PHASE 6 styled\n%s", s.dumpStyled())

		// A digit per page, because a question ask is a wizard now: choosing an
		// option checks its tab and advances to the review, whose own options
		// are `1. Submit answers` and `2. Cancel`. So `1` then `1` - choose,
		// then submit - and no arm anywhere, because the review *is* the
		// confirmation the arm used to be.
		//
		// These presses have been wrong twice, in opposite directions, and both
		// are kept because they are the two ways a live test rots:
		//
		//   - `1` then ↵. The digit chose and the unarmed ↵ was chooseCursored,
		//     which writes no frame. The agent stayed blocked for the rest of
		//     the run and every later phase queued behind it.
		//   - `1` then `a` then ↵. Correct against arm-and-confirm, obsolete the
		//     moment the wizard landed: `a` is not a card key any more, so it
		//     went into the composer as a draft and the ↵ *sent* it to the agent.
		//
		// The card was self-documenting through both - its key line offered
		// `[a]nswer` exactly while that was the next press, and once a draft
		// existed it said `keys return when the draft is sent or cleared`. Read
		// the key line in the frames above before changing these presses.
		s.send("1")
		s.settle()
		t.Logf("PHASE 6 - option chosen, wizard advanced to its review\n%s", s.dump())
		s.send("1")
		s.settle()
		t.Logf("PHASE 6 - submitted\n%s", s.dump())

		// Not "idle": the strip reads `1 need you · 2 idle` while an agent is
		// blocked, so waiting for "idle" is satisfied by the agents that were
		// never asked anything. The claim is that *this* ask is gone.
		if !gone(s, "need you", liveTimeout) {
			t.Errorf("the ask was still outstanding after 1-a-↵.\n%s", s.dump())
		}
		s.settle()
		t.Logf("PHASE 6 - after settling\n%s", s.dump())
	})

	// --- 7. markdown, and the row bookkeeping under it ------------------------
	//
	// The reported symptom this exists for is the room "skipping lines". None of
	// alpha/beta/gamma is typed - the prompt asks for the Greek alphabet - so a
	// missing one is a dropped row rather than a missing echo.
	t.Run("markdown", func(t *testing.T) {
		focusRoom(t, s)
		liveSay(s, "@"+first+" Reply with a markdown bulleted list of the first three letters of the Greek alphabet, lowercase, one per line. No other text.")
		awaitLive(t, s, "markdown", "gamma")
		s.settle()

		drawn := s.text()
		for _, want := range []string{"alpha", "beta", "gamma"} {
			if !strings.Contains(drawn, want) {
				t.Errorf("the room lost %q out of a three-item list - a dropped row.\n%s", want, s.dump())
			}
		}
		t.Logf("PHASE 7 - markdown\n%s", s.dump())
		t.Logf("PHASE 7 styled\n%s", s.dumpStyled())
	})

	// --- 8. ⇧⇥, and where the mode is reported --------------------------------
	//
	// This phase was assertion-free for as long as BUG-1 and BUG-8 were open: the
	// mode reached no surface a conversation drew, and a test asserting the fixed
	// behaviour would have been red on arrival for a reason already written down.
	// Both are closed, so it asserts.
	//
	// The spawn mode is `auto` and one ⇧⇥ from there is `default` - the cycle is
	// Claude Code's own, read out of its binary. The label moves on the daemon's
	// receipt rather than on the keystroke, which is why this awaits rather than
	// settles.
	t.Run("mode", func(t *testing.T) {
		openAgentAny(t, s, first)
		if !awaitLive(t, s, "mode", "permissions: "+core.PermissionModeAuto) {
			return
		}
		t.Logf("PHASE 8 - a conversation, before ⇧⇥\n%s", s.dump())

		s.send("\x1b[Z") // CSI Z, backtab
		awaitLive(t, s, "mode", "permissions: "+core.PermissionModeDefault)
		s.settle()
		t.Logf("PHASE 8 - after one ⇧⇥\n%s", s.dump())
	})

	// --- 9. leaving -----------------------------------------------------------
	t.Run("park", func(t *testing.T) {
		s.send("\x11") // ⌃Q
		s.settle()
		t.Logf("PHASE 9 - after ⌃Q\n%s", s.dump())
	})
}

// dumpStyled is dump plus the colour, as runs rather than per cell.
//
// lines() reads Cell(x,y).Char and throws the rest of the glyph away, which is
// right for every assertion in this package - they are about layout, and the
// palette is already held to Claude Code's own by palette_test.go against a
// table matched against Claude Code.
//
// What that leaves unanswerable is whether the colours are *applied where they
// should be*: an extracted palette proves the values, and a rendered frame
// proves the placement. Only a reader comparing the two can say a heading came
// out in the accent and a quoted path did not.
//
// Runs rather than cells because a row of 200 per-cell triples is not something
// anybody reads. A run is `fg/bg` and the text that carries it; a row with one
// colour prints as one run, which is the common case and stays legible.
func (s *screen) dumpStyled() string {
	s.term.Lock()
	defer s.term.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "--- %dx%d styled ---\n", s.cols, s.rows)
	for y := range s.rows {
		var (
			row      strings.Builder
			run      strings.Builder
			fg, bg   vt10x.Color
			hasRun   bool
			anything bool
		)
		flush := func() {
			if !hasRun {
				return
			}
			if text := strings.TrimRight(run.String(), " "); text != "" {
				fmt.Fprintf(&row, "[%d/%d]%s", fg, bg, text)
				anything = true
			}
			run.Reset()
		}
		for x := range s.cols {
			c := s.term.Cell(x, y)
			if !hasRun || c.FG != fg || c.BG != bg {
				flush()
				fg, bg, hasRun = c.FG, c.BG, true
			}
			run.WriteRune(c.Char)
		}
		flush()
		if anything {
			fmt.Fprintf(&b, "%2d|%s\n", y, row.String())
		}
	}
	return b.String()
}

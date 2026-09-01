package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestContextLeftIsAPercentageOfTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name        string
		used, total int
		want        string
	}{
		{"a quarter used", 250_000, 1_000_000, "ctx:75%"},
		{"almost none used", 1_000, 1_000_000, "ctx:99%"},
		{"exactly full", 200_000, 200_000, "ctx:0%"},
		// Over the window before a compaction lands: clamped, not negative.
		{"over the window", 260_000, 200_000, "ctx:0%"},
		// Neither half is a claim on its own.
		{"no window", 5_000, 0, ""},
		{"no usage", 0, 200_000, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextLeft(tc.used, tc.total); got != tc.want {
				t.Errorf("contextLeft(%d, %d) = %q, want %q", tc.used, tc.total, got, tc.want)
			}
		})
	}
}

// Rounding down, so the bar never says there is room when there is none left.
func TestContextLeftRoundsTowardEmpty(t *testing.T) {
	if got := contextLeft(999_999, 1_000_000); got != "ctx:0%" {
		t.Errorf("one token short of full = %q, want ctx:0%%", got)
	}
}

func TestModelNameReadsLikeClaudes(t *testing.T) {
	for _, tc := range []struct {
		id     string
		window int
		want   string
	}{
		{"claude-opus-5", 1_000_000, "Opus 5 (1M context)"},
		{"claude-sonnet-5", 200_000, "Sonnet 5"},
		{"claude-haiku-4-5", 200_000, "Haiku 4.5"},
		// A model this build has not been told about is still named.
		{"claude-something-7", 200_000, "claude-something-7"},
		{"", 200_000, ""},
	} {
		if got := modelName(tc.id, tc.window); got != tc.want {
			t.Errorf("modelName(%q, %d) = %q, want %q", tc.id, tc.window, got, tc.want)
		}
	}
}

func TestShortPathUsesTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory on this machine")
	}
	if got, want := shortPath(filepath.Join(home, "Documents", "wake")), "~/Documents/wake"; got != want {
		t.Errorf("shortPath = %q, want %q", got, want)
	}
	if got := shortPath("/etc/hosts"); got != "/etc/hosts" {
		t.Errorf("a path outside home was rewritten to %q", got)
	}
	if got := shortPath(""); got != "" {
		t.Errorf("an unknown directory produced %q, want empty", got)
	}
}

// A fact nobody has reported is left out rather than drawn as a placeholder.
// Wake learns these one frame at a time, so a bar of "unknown  unknown" is the
// ordinary state for the first second of every session.
func TestTheBarDropsSegmentsItDoesNotKnow(t *testing.T) {
	bar := stripANSI(statusBar(Agent{Model: "claude-sonnet-5"}, "", 80))

	if !strings.Contains(bar, "Sonnet 5") {
		t.Errorf("bar %q lost the one fact it had", bar)
	}
	if strings.Contains(bar, ctxLabel) {
		t.Errorf("bar %q drew a context figure with no window reported", bar)
	}
	if strings.HasPrefix(bar, statusSep) || strings.Contains(bar, statusSep+statusSep) {
		t.Errorf("bar %q has a gap where a missing segment was", bar)
	}
}

// Effort is the level a /model probe confirmed (or the one Wake asked for until
// then), shown once known and dropped when neither exists - never guessed.
func TestTheBarShowsEffortWhenKnown(t *testing.T) {
	bar := stripANSI(statusBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5", Effort: "xhigh"}, modeAuto, 200))
	if !strings.Contains(bar, effortLabel+"xhigh") {
		t.Errorf("bar %q has no effort segment", bar)
	}

	bare := stripANSI(statusBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5"}, modeAuto, 200))
	if strings.Contains(bare, effortLabel) {
		t.Errorf("bar %q drew an effort with none known", bare)
	}
}

// The confirmed model wins over the init-frame id, so a /model change shows the
// moment the probe reads it back rather than at the next turn - the bug this
// fixes. Until the probe answers, the init id is the fallback.
func TestTheBarPrefersTheConfirmedModel(t *testing.T) {
	bar := stripANSI(statusBar(Agent{
		Cwd: "/tmp/repo", Model: "claude-opus-5", ConfirmedModel: "Sonnet 5 (1M context)",
	}, modeAuto, 200))
	if !strings.Contains(bar, "Sonnet 5 (1M context)") {
		t.Errorf("bar %q did not prefer the confirmed model", bar)
	}
	if strings.Contains(bar, "Opus 5") {
		t.Errorf("bar %q still drew the init-frame model over the confirmed one", bar)
	}

	fallback := stripANSI(statusBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5"}, modeAuto, 200))
	if !strings.Contains(fallback, "Opus 5") {
		t.Errorf("bar %q lost the init-frame model before a probe answered", fallback)
	}
}

// The PRs this session has opened, named as Claude Code would - one or several -
// and dropped rather than guessed when it has opened none.
func TestTheBarShowsThePRsThisSessionOpened(t *testing.T) {
	one := stripANSI(statusBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5", prs: &prSet{nums: []int{29}}}, "", 200))
	if !strings.Contains(one, "PR #29") {
		t.Errorf("bar %q has no PR segment", one)
	}

	many := stripANSI(statusBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5", prs: &prSet{nums: []int{29, 30}}}, "", 200))
	if !strings.Contains(many, "PR #29, #30") {
		t.Errorf("bar %q did not list both PRs", many)
	}

	none := stripANSI(statusBar(Agent{Cwd: "/tmp/repo", Model: "claude-opus-5"}, "", 200))
	if strings.Contains(none, "PR #") {
		t.Errorf("bar %q drew a PR segment for a session that opened none", none)
	}
}

// The report is the only route to a PR for a client that attached after it was
// opened, so a WithStatus fold has to land the numbers on the agent the bar
// draws - the same late-attach path Commands and Effort take.
func TestAReportCarriesThePRsOntoTheBar(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateWorking, Cwd: "/tmp/repo", PRs: []int{29, 30}},
	}})
	a, ok := f.Agent("s1")
	if !ok {
		t.Fatal("no agent after a report naming one")
	}
	if bar := stripANSI(statusBar(a, "", 200)); !strings.Contains(bar, "PR #29, #30") {
		t.Errorf("the report's PRs did not reach the bar: %q", bar)
	}
}

func TestTheBarIsEmptyWhenNothingIsKnown(t *testing.T) {
	if got := statusBar(Agent{}, "", 80); got != "" {
		t.Errorf("an unknown agent drew %q, want no bar at all", got)
	}
}

func TestTheBarIsBounded(t *testing.T) {
	a := Agent{
		Cwd:           "/very/long/path/that/keeps/going/and/going/for/quite/a/while/indeed",
		Model:         "claude-opus-5",
		ContextTokens: 250_000,
		ContextWindow: 1_000_000,
	}
	for _, width := range []int{10, 20, 40, 80} {
		if got := ansi.StringWidth(statusBar(a, modeAuto, width)); got > width {
			t.Errorf("width %d: bar is %d cells", width, got)
		}
	}
}

// Everything asked for that Wake can actually source, in one line.
func TestTheBarDrawsWhatItKnows(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, "ref: refs/heads/feat/fidelity")

	bar := stripANSI(statusBar(Agent{
		Cwd:           dir,
		Model:         "claude-opus-5",
		ContextTokens: 260_000,
		ContextWindow: 1_000_000,
	}, modeAuto, 200))

	for _, want := range []string{"feat/fidelity", "Opus 5 (1M context)", "ctx:74%", "permissions: " + modeAuto} {
		if !strings.Contains(bar, want) {
			t.Errorf("bar %q is missing %q", bar, want)
		}
	}
}

// countBars replaces the render seam with a counter, and puts it back.
func countBars(t *testing.T) *int {
	t.Helper()
	n, prev := 0, drawStatusBar
	drawStatusBar = func(a Agent, mode string, width int) string {
		n++
		return prev(a, mode, width)
	}
	t.Cleanup(func() { drawStatusBar = prev })
	return &n
}

// The bar is the one thing in this pane that reads the filesystem - gitref
// walks for a .git and reads HEAD - so it is drawn when what it says changes
// and not when the screen merely redraws.
//
// This is the guard for the first non-negotiable, and it is a cost test rather
// than an appearance one. The heartbeat redraws the whole frame 20 times a
// second for as long as *any* agent in the fleet is working, so a bar rendered
// per View is filesystem I/O per frame for a conversation that may itself be
// idle.
func TestTheStatusBarIsDrawnPerChangeAndNotPerFrame(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, "ref: refs/heads/main")

	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", Cwd: dir, Model: "claude-opus-5", ContextTokens: 10, ContextWindow: 100}
	d = d.SetSize(80, 24)

	bars := countBars(t)
	for range 20 {
		_ = d.View(80, 24)
	}
	if *bars != 0 {
		t.Errorf("twenty steady frames drew the status bar %d times, want 0", *bars)
	}
}

// It is still drawn when something it says has moved, or it would go stale.
func TestTheStatusBarIsRedrawnWhenItsFactsMove(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, "ref: refs/heads/main")

	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", Cwd: dir, Model: "claude-opus-5", ContextTokens: 10, ContextWindow: 100}
	d = d.SetSize(80, 24)

	for _, tc := range []struct {
		name  string
		apply func(DM) DM
	}{
		{"the context moved", func(d DM) DM { d.Agent.ContextTokens = 50; return d }},
		{"the model changed", func(d DM) DM { d.Agent.Model = "claude-sonnet-5"; return d }},
		// The probe reading a model back is what makes a /model instant; a bar
		// that did not redraw for it would move at the next turn, the bug again.
		{"the probe confirmed a model", func(d DM) DM { d.Agent.ConfirmedModel = "Sonnet 5"; return d }},
		{"the effort changed", func(d DM) DM { d.Agent.Effort = "low"; return d }},
		// A PR is scraped mid-turn with no other bar fact moving, so barKey has to
		// carry it or the segment never appears until something else redraws.
		{"a PR was opened", func(d DM) DM { d.Agent.prs = &prSet{nums: []int{29}}; return d }},
		{"the pane got narrower", func(d DM) DM { return d }},
		// A turn boundary is what re-reads the branch, so an operator who
		// checks out another one sees it without anything polling.
		{"a turn started", func(d DM) DM { d.Agent.State = "working"; return d }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bars := countBars(t)
			moved := tc.apply(d)
			width := 80
			if tc.name == "the pane got narrower" {
				width = 60
			}
			_ = moved.View(width, 24)
			if *bars == 0 {
				t.Error("the bar was not redrawn after one of its own facts changed")
			}
		})
	}
}

// The bar is one row, and this is the guard rather than a preference.
// chromeHeight budgets exactly one row whenever the bar is non-empty, so a
// segment carrying a newline draws two and the pane is a row taller than it was
// given - which scrolls the alt screen away on every frame at the ticker's rate.
//
// The facts are not all Wake's: the model is whatever a claude process reported
// on its init frame, and a directory may legally contain a newline.
func TestTheStatusBarIsAlwaysOneRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		agent Agent
		mode  string
	}{
		{"a model with a newline", Agent{Model: "evil\nsecond-row", ContextWindow: 200_000}, modeAuto},
		{"a directory with a newline", Agent{Cwd: "/tmp/one\ntwo"}, modeAuto},
		{"a carriage return", Agent{Model: "evil\rsecond"}, modeAuto},
		{"control characters", Agent{Model: "evil\x1b[2Jclear"}, modeAuto},
		{"a tab", Agent{Model: "one\ttwo"}, modeAuto},
		// The mode is an agent-authored fact too: it arrives on a receipt or
		// an init, so it is no more Wake's than the model is.
		{"a mode with a newline", Agent{Model: "claude-opus-5"}, "plan\nsecond-row"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bar := statusBar(tc.agent, tc.mode, 200)
			if got := lipgloss.Height(bar); got != 1 {
				t.Errorf("the bar drew %d rows: %q", got, stripANSI(bar))
			}
		})
	}
}

// And the pane still measures what it was given when a hostile fact arrives.
func TestThePaneStaysInBoundsWithAMultiLineFact(t *testing.T) {
	const w, h = 80, 24
	d := NewDM("s1", "alex").SetSize(w, h)
	d.Agent = Agent{ID: "s1", Model: "evil\nsecond-row", ContextTokens: 10, ContextWindow: 100}

	if got := lipgloss.Height(d.View(w, h)); got != h {
		t.Errorf("pane drew %d rows into %d", got, h)
	}
}

// modeAuto is the mode every session this build spawns in, spelled here rather
// than reached through spawnedMode so a test that changes with the spawn flag
// says so.
const modeAuto = "auto"

// --- docs/notes/bugs.md BUG-1 -------------------------------------------
//
// ⇧⇥ moved the belief and no surface a conversation pane draws said so: the
// mode's only home was the legend's tail, which is the first entry a narrow
// pane cuts and is withheld from a blurred composer outright. The bar is the
// answer because it asks a different question - the legend says what a key
// would do, the bar says what this session *is*.

func TestTheBarNamesThePermissionMode(t *testing.T) {
	bar := stripANSI(statusBar(Agent{Model: "claude-opus-5"}, "plan", 200))
	if want := "permissions: plan"; !strings.Contains(bar, want) {
		t.Errorf("bar %q is missing %q", bar, want)
	}
}

// The mode is a segment like any other, so it is dropped rather than guessed
// when the caller has none. Every real pane has one - modeOf falls back to the
// spawn mode - but statusBar does not invent it here.
func TestTheBarDropsAModeItWasNotGiven(t *testing.T) {
	if bar := stripANSI(statusBar(Agent{Model: "claude-opus-5"}, "", 200)); strings.Contains(bar, "permissions") {
		t.Errorf("bar %q named a mode it was not given", bar)
	}
	if got := statusBar(Agent{}, "", 80); got != "" {
		t.Errorf("an agent with no facts and no mode drew %q, want no bar at all", got)
	}
}

// And the mode is not a bar on its own. It is the one fact Wake always has an
// answer for, so admitting it among the segments would keep the row alive for a
// session nothing is known about - and that row comes out of the transcript in a
// pane already at minDMHeight. Nothing real lands here: a fleet report carries
// the spawn directory from the moment a session exists.
func TestTheModeIsNotAWholeBarOnItsOwn(t *testing.T) {
	if got := statusBar(Agent{}, modeAuto, 200); got != "" {
		t.Errorf("an agent with nothing but a mode drew %q, want no bar at all", stripANSI(got))
	}
}

// The pane's own mode, not the roster pick's: a conversation has exactly one
// agent, which is why the legend's ambiguity rule has nothing to protect
// against here.
func TestAConversationsBarNamesItsOwnMode(t *testing.T) {
	d := dmInMode(core.PermissionModePlan, true)
	if bar := stripANSI(d.bar); !strings.Contains(bar, "permissions: "+core.PermissionModePlan) {
		t.Errorf("the conversation's bar does not name its mode: %q", bar)
	}
}

// The ruling this entry turns on: a blurred pane keeps the mode in its *bar*.
// The bar is a statement about this pane's session, true wherever the keys are;
// the composer draws no mode at all now (TestTheComposerNamesNoMode), so the
// old "withheld from the blurred legend" half is the composer's rule to keep.
func TestABlurredConversationStillNamesItsModeInTheBar(t *testing.T) {
	d := dmInMode(core.PermissionModePlan, false)
	want := "permissions: " + core.PermissionModePlan

	if bar := stripANSI(d.bar); !strings.Contains(bar, want) {
		t.Errorf("a blurred conversation's bar dropped its mode: %q", bar)
	}
}

// The bar is drawn per change, so the mode has to be part of what "changed"
// means - or the one fact this entry exists for is the one that goes stale.
func TestTheBarIsRedrawnWhenTheModeMoves(t *testing.T) {
	d := dmInMode(core.PermissionModeDefault, true)

	bars := countBars(t)
	moved := d.WithComposer(d.Composer().WithMode(core.PermissionModePlan))
	_ = moved.View(fullLegendWidth, 24)
	if *bars == 0 {
		t.Error("the bar was not redrawn after the permission mode changed")
	}
}

// dmInMode is a sized conversation whose composer names mode, focused or not.
func dmInMode(mode string, focused bool) DM {
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", Model: "claude-opus-5", ContextTokens: 10, ContextWindow: 100}
	return d.WithComposer(d.Composer().Focused(focused).WithMode(mode)).SetSize(fullLegendWidth, 24)
}

// The mode is drawn whole or dropped, never cut. A right-cut `permissions: …`
// announces a value nobody can read - and it is what a real conversation pane
// produced at 90 columns with a long path above it.
func TestANarrowBarDropsTheModeRatherThanCuttingIt(t *testing.T) {
	a := Agent{Cwd: "/very/long/path/that/keeps/going/and/going/for/quite/a/while", Model: "claude-opus-5"}
	for width := 10; width <= 120; width++ {
		bar := stripANSI(statusBar(a, modeAuto, width))
		if strings.Contains(bar, "permissions") && !strings.Contains(bar, "permissions: "+modeAuto) {
			t.Fatalf("width %d cut the mode: %q", width, bar)
		}
	}
}

// The one line that threads a session's believed mode into its pane, asserted
// where it is cheap. Every other test here hands a composer its mode directly,
// which is the right unit for the bar's own rules and proves nothing about the
// wiring; only the pty test covered this, and a wiring regression should not
// have to wait for the slow suite.
func TestAPanesModeIsTheOneItsSessionIsBelievedToBeIn(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()
	a = a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))

	if got, want := a.dmFor(picked.ID).Composer().Mode(), a.modeOf(picked.ID); got != want {
		t.Errorf("the pane names %q and the client believes %q", got, want)
	}
	if got := a.modeOf(picked.ID); got != core.PermissionModePlan {
		t.Errorf("the receipt did not move the belief: %q", got)
	}
}

// docs/notes/bugs.md BUG-5, and it is asserted at App level because that is the
// only level that can see it. The DM-level guard above builds a DM directly,
// assigns d.Agent and calls SetSize - which caches barFrom *with that agent* -
// so the divergence never happens and it measures a path the product does not
// use. Measured on the unfixed tree: 20 bar draws over 20 App.View() calls.
//
// The bar reads the filesystem: gitref walks for a .git and reads HEAD. At the
// shimmer's 20Hz for as long as any agent in the fleet is working, that is four
// stats a frame per open conversation, which is the "work per frame that could
// be work per change" the first non-negotiable forbids.
func TestTheStatusBarIsDrawnPerChangeInARealApp(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	// One draw to settle whatever the open itself moved.
	_ = a.View()

	bars := countBars(t)
	for range 20 {
		_ = a.View()
	}
	if *bars != 0 {
		t.Errorf("twenty steady frames drew the status bar %d times, want 0", *bars)
	}
}

// And it is still drawn when something it says has moved, or the row goes stale
// - the half that stops the fix being "never draw it".
func TestTheStatusBarIsRedrawnInARealAppWhenItsFactsMove(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	_ = a.View()

	bars := countBars(t)
	picked, _ := a.pickedAgent()
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Sessions: []rpc.SessionStatus{{
		ID: picked.ID, Name: picked.Name, State: rpc.StateWorking, Cwd: t.TempDir(),
	}}}})
	_ = a.View()
	if *bars == 0 {
		t.Fatal("the bar was not redrawn after a report moved one of its own facts")
	}

	// And then it goes quiet again, which is the half that needs
	// App.refreshedAgents: the redraw above is correct, but without the report
	// folding the new facts into the *stored* conversation the pane would go on
	// re-rendering from the stale one on every frame after it - the bug again,
	// one report later. Removing refreshedAgents leaves the assertion above
	// green and this one red.
	settled := countBars(t)
	for range 20 {
		_ = a.View()
	}
	if *settled != 0 {
		t.Errorf("twenty frames after the report drew the bar %d more times, want 0", *settled)
	}
}

// The event path, which the report-path test above cannot see. A permission
// mode reaches App as a receipt rather than as a fleet report, and the bar
// names the mode - so ⇧⇥ reopened BUG-5 permanently: 20 draws over 20 frames
// after one receipt, for as long as the conversation stayed open.
//
// Found by an adversarial review of the first fix, which covered applyStatus
// and left this open.
func TestTheStatusBarGoesQuietAgainAfterAModeReceipt(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	_ = a.View()

	a = a.applyFrame(modeReceipt(picked.ID, core.PermissionModePlan))
	// The receipt itself may redraw - the mode it names is on the bar, so that
	// is the row moving rather than churn. What must not continue is the frames
	// after it.
	_ = a.View()

	bars := countBars(t)
	for range 20 {
		_ = a.View()
	}
	if *bars != 0 {
		t.Errorf("twenty frames after a mode receipt drew the bar %d times, want 0", *bars)
	}
}

// The third path, and the most frequent of the three: the model and the context
// figures reach the fleet on an init or a result, never on a fleet report, and
// the bar draws all three. So every turn boundary left the stored conversation
// behind and the pane re-rendered on every frame until something else corrected
// it. Found by a code review of the first two fixes.
func TestTheStatusBarGoesQuietAgainAfterATurnsFacts(t *testing.T) {
	a := roomWithPick(t, modeRoomWidth)
	picked, _ := a.pickedAgent()
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	_ = a.View()

	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: picked.ID, Event: &core.Event{
		Kind: core.KindSystem, Text: "init",
		Session: &core.SessionFacts{Model: "claude-opus-5", ContextTokens: 1000, ContextWindow: 200_000},
	}})
	_ = a.View()

	bars := countBars(t)
	for range 20 {
		_ = a.View()
	}
	if *bars != 0 {
		t.Errorf("twenty frames after a turn's facts drew the bar %d times, want 0", *bars)
	}
}

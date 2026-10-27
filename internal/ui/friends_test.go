package ui

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

var aReading = &rpc.SessionUsage{Percent: 47, Resets: usageResets}

// keptFriends records every choice the corner asked to keep.
type keptFriends struct {
	mu   sync.Mutex
	keys []string
}

func (k *keptFriends) save(key string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys = append(k.keys, key)
	return nil
}

func (k *keptFriends) got() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.keys)
}

// friendRoom is a 100x24 room with pick on the bar, drawn as Kitty images or
// half blocks, keeping its choices in kept.
func friendRoom(t *testing.T, kitty bool, pick string, kept *keptFriends) App {
	t.Helper()
	fresh(t)
	return newRoomApp(t).withSize(100, 24).WithFriends(Friends{Pick: pick, Kitty: kitty, Save: kept.save})
}

// friendIDs is the image every placeholder cell of rows names, in order.
func friendIDs(rows []string) []int {
	var out []int
	for _, r := range rows {
		for _, c := range placeholderCell.FindAllStringSubmatch(r, -1) {
			out = append(out, cellID(c))
		}
	}
	return out
}

// allOf is n copies of id.
func allOf(id, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = id
	}
	return out
}

var friendCells9x5 = friendCols * friendRows

func TestAFriendStandsOnTheRuleOverTheBar(t *testing.T) {
	a := friendRoom(t, true, "skip", &keptFriends{}).withUsageReport(aReading, "sydney", "john")
	rows := sidebarRows(a)
	block := rows[len(rows)-friendRows-2:]
	if got := friendIDs(block); !slices.Equal(got, allOf(friendImage(0, 0), friendCells9x5)) {
		t.Fatalf("the corner drew images %v, want Skip's first frame in all %d cells", got, friendCells9x5)
	}
	pad := strings.Repeat(" ", 23-friendCols)
	for i, r := range block[:friendRows-1] {
		if s := ansi.Strip(r); !strings.HasPrefix(s, pad) {
			t.Errorf("friend row %d starts %q, want the sidebar's width less the friend in blanks", i, s)
		}
	}
	if s := ansi.Strip(block[friendRows-1]); !strings.HasPrefix(s, strings.Repeat(sectionRule, 23-friendCols)) {
		t.Errorf("the friend's feet row is %q, want the rule up to its feet", s)
	}
	if text := usageBlock(a)[1:]; !strings.HasSuffix(text[0], " 47%") || !strings.HasPrefix(text[1], "session · ↻ 4:09pm") {
		t.Errorf("under the friend the sidebar drew %q, want the bar and its caption", text)
	}
	if got, want := a.rosterHeight(), a.paneHeight()-friendRows-2; got != want {
		t.Errorf("the roster keeps %d rows beside a friend on the bar, want %d", got, want)
	}
}

func TestAFriendSitsAloneInTheCornerBeforeTheFirstReading(t *testing.T) {
	a := friendRoom(t, true, "bo", &keptFriends{}).withUsageReport(nil, "sydney")
	rows := sidebarRows(a)
	if got := friendIDs(rows[len(rows)-friendRows:]); !slices.Equal(got, allOf(friendImage(2, 0), friendCells9x5)) {
		t.Fatalf("the corner drew images %v, want Bo's first frame", got)
	}
	for _, r := range rows {
		if strings.Contains(ansi.Strip(r), "session") {
			t.Errorf("a bar drew with no reading: %q", ansi.Strip(r))
		}
	}
	if got, want := a.rosterHeight(), a.paneHeight()-friendRows; got != want {
		t.Errorf("the roster keeps %d rows beside a lone friend, want %d", got, want)
	}
}

func TestNoneLeavesTheBarAndTheRosterAsTheyWere(t *testing.T) {
	a := friendRoom(t, true, friendNone, &keptFriends{})
	if got := a.withUsageReport(nil, "sydney").rosterHeight(); got != a.paneHeight() {
		t.Errorf("none with no reading leaves the roster %d rows, want all %d", got, a.paneHeight())
	}
	a = a.withUsageReport(aReading, "sydney")
	if got := a.rosterHeight(); got != a.paneHeight()-3 {
		t.Errorf("none with a reading leaves the roster %d rows, want %d", got, a.paneHeight()-3)
	}
	if ids := friendIDs(sidebarRows(a)); len(ids) != 0 {
		t.Errorf("none drew %d placeholder cells", len(ids))
	}
}

func TestATerminalWithoutKittyGraphicsDrawsTheHalfBlockSprite(t *testing.T) {
	trueColor(t)
	a := friendRoom(t, false, "ollie", &keptFriends{}).withUsageReport(aReading, "sydney")
	rows := sidebarRows(a)
	block := rows[len(rows)-friendRows-2 : len(rows)-2]
	sprite := halfBlockRows(ollieArt.blocks)
	for i, r := range block {
		if strings.ContainsRune(r, placeholderRune) {
			t.Fatalf("a half-block terminal drew a placeholder on row %d", i)
		}
		if !strings.HasSuffix(r, sprite[i]) {
			t.Errorf("friend row %d is %q, want it to end in the sprite's row %q", i, r, sprite[i])
		}
	}
	if !strings.ContainsAny(strings.Join(sprite, ""), upperHalf+lowerHalf) {
		t.Error("the sprite draws no half block")
	}
}

// A pane with room for the bar but not the friend beside a roster row keeps
// the bar, as a pane too short for the bar keeps the roster.
func TestAPaneTooShortForTheFriendKeepsTheBar(t *testing.T) {
	block := friendRows + 2
	a := friendRoom(t, true, "skip", &keptFriends{}).withUsageReport(aReading, "sydney")
	a = a.withSize(100, block+noticeHeight+stripHeight).applyGeometry()
	if got := a.rosterHeight(); got != block-3 {
		t.Errorf("a %d-row pane gave the roster %d rows, want %d beside the bar alone", a.paneHeight(), got, block-3)
	}
	if ids := friendIDs(sidebarRows(a)); len(ids) != 0 {
		t.Errorf("a pane too short for the friend drew %d of its cells", len(ids))
	}
	a = a.withSize(100, block+1+noticeHeight+stripHeight).applyGeometry()
	if got := a.rosterHeight(); got != 1 {
		t.Errorf("a %d-row pane gave the roster %d rows, want 1 above the friend and the bar", a.paneHeight(), got)
	}
}

func TestClicksLandOnTheRowDrawnThereBesideAFriend(t *testing.T) {
	names := rosterNames(30)
	for _, u := range []*rpc.SessionUsage{nil, aReading} {
		a := pick(friendRoom(t, true, "skip", &keptFriends{}).withUsageReport(u, names...), "s30")
		if len(a.usageDrawn()) < friendRows {
			t.Fatalf("with reading %v the corner drew %d rows, no friend among them", u, len(a.usageDrawn()))
		}
		everyClickLandsOnTheRowDrawnThere(t, a)
	}
}

// workingRoom is a friend room whose one agent has a turn running.
func workingRoom(t *testing.T) App {
	a := friendRoom(t, true, "skip", &keptFriends{})
	st := rpc.Status{Running: true, SessionUsage: aReading,
		Sessions: []rpc.SessionStatus{{ID: "s1", Name: "sydney", State: rpc.StateWorking}}}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
}

func atClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := clock
	clock = func() time.Time { return at }
	t.Cleanup(func() { clock = prev })
}

func TestAFriendAnimatesOffTheClockOnlyWhileAnAgentWorks(t *testing.T) {
	start := time.Unix(0, 0)
	for n := range 2 * friendFrames {
		atClock(t, start.Add(time.Duration(n)*friendFrameEvery))
		a := workingRoom(t)
		if got, want := friendIDs(sidebarRows(a)), allOf(friendImage(0, n%friendFrames), friendCells9x5); !slices.Equal(got, want) {
			t.Errorf("at %v a working fleet drew frame images %v, want frame %d", time.Duration(n)*friendFrameEvery, got[:1], n%friendFrames)
		}
	}
	atClock(t, start.Add(friendFrameEvery))
	idle := friendRoom(t, true, "skip", &keptFriends{}).withUsageReport(aReading, "sydney")
	if got := friendIDs(sidebarRows(idle)); !slices.Equal(got, allOf(friendImage(0, 0), friendCells9x5)) {
		t.Errorf("an idle fleet drew images %v, want the first frame whatever the clock", got[:1])
	}
	if _, cmd := idle.beat(); cmd != nil {
		t.Error("an idle fleet with a friend on the bar scheduled a heartbeat")
	}
}

func TestBareFriendsOpensOnTheKeptFriendAndTheCursorPreviews(t *testing.T) {
	kept := &keptFriends{}
	a := friendRoom(t, true, "skip", kept).withUsageReport(aReading, "sydney")
	before := sidebarRows(a)
	m, _ := typeAndSubmit(a, friendsVerb)
	a = m.(App)
	if p := a.picker; p.Word != friendsCommand || !slices.Equal(p.Options, []string{"skip", "ollie", "bo", friendNone}) || p.Cursor != 0 || p.Current != "skip" {
		t.Fatalf("/friends opened %+v, want the cast and none with the cursor on the kept Skip", p)
	}
	view := ansi.Strip(a.View())
	for _, want := range []string{friendsTitle, "Skip", "Ollie", "Bo", "none", "the tug that leaves a wake", friendsHint} {
		if !strings.Contains(view, want) {
			t.Errorf("the menu does not show %q", want)
		}
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := friendIDs(sidebarRows(a)); !slices.Equal(got, allOf(friendImage(1, 0), friendCells9x5)) {
		t.Errorf("with the cursor on Ollie the corner drew %v, want Ollie", got[:1])
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if ids := friendIDs(sidebarRows(a)); len(ids) != 0 || a.rosterHeight() != a.paneHeight()-3 {
		t.Errorf("with the cursor on none the corner drew %d cells and the roster %d rows", len(ids), a.rosterHeight())
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if a.picker.Open() || a.corner.pick != "skip" || !slices.Equal(sidebarRows(a), before) {
		t.Errorf("esc left the picker %v, the pick %q, and the corner changed: it must put Skip back as drawn", a.picker.Open(), a.corner.pick)
	}
	if got := kept.got(); len(got) != 0 {
		t.Errorf("esc kept %v", got)
	}
}

func TestEnterKeepsTheCursoredFriend(t *testing.T) {
	kept := &keptFriends{}
	a := friendRoom(t, true, "skip", kept).withUsageReport(aReading, "sydney")
	m, _ := typeAndSubmit(a, friendsVerb)
	a = m.(App)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	_ = runCmdQuietly(cmd)
	if a.picker.Open() || a.corner.pick != "bo" {
		t.Errorf("↵ on Bo left the picker open %v and the pick %q", a.picker.Open(), a.corner.pick)
	}
	if got := kept.got(); !slices.Equal(got, []string{"bo"}) {
		t.Errorf("↵ kept %v, want [bo] once", got)
	}
	if got := friendIDs(sidebarRows(a)); !slices.Equal(got, allOf(friendImage(2, 0), friendCells9x5)) {
		t.Errorf("after ↵ the corner drew %v, want Bo", got[:1])
	}
}

func TestTypingClosesTheFriendsMenuAndPutsTheKeptOneBack(t *testing.T) {
	kept := &keptFriends{}
	a := friendRoom(t, true, "ollie", kept).withUsageReport(aReading, "sydney")
	m, _ := typeAndSubmit(a, friendsVerb)
	a = m.(App)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	m, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	a = m.(App)
	if a.picker.Open() || a.drawnFriend() != "ollie" || len(kept.got()) != 0 {
		t.Errorf("typing left the picker open %v, the corner on %q and kept %v", a.picker.Open(), a.drawnFriend(), kept.got())
	}
}

func TestFriendsWithAWordSetsThatFriendAtOnce(t *testing.T) {
	kept := &keptFriends{}
	a := friendRoom(t, true, "skip", kept).withUsageReport(aReading, "sydney")
	m, cmd := typeAndSubmit(a, friendsVerb+" Bo")
	_ = runCmdQuietly(cmd)
	a = m.(App)
	if a.corner.pick != "bo" || a.picker.Open() || !slices.Equal(kept.got(), []string{"bo"}) {
		t.Errorf("/friends Bo left the pick %q, the picker open %v, and kept %v", a.corner.pick, a.picker.Open(), kept.got())
	}
	m, cmd = typeAndSubmit(a, friendsVerb+" Rex")
	_ = runCmdQuietly(cmd)
	a = m.(App)
	n, _ := notice.Latest()
	if a.corner.pick != "bo" || len(kept.got()) != 1 || !strings.Contains(n.String(), "Rex") || !strings.Contains(n.String(), friendsList()) {
		t.Errorf("/friends Rex left the pick %q, kept %v, and said %q; want nothing changed and the cast named", a.corner.pick, kept.got(), n.String())
	}
}

func TestAKeepThatFailsSaysSoAndKeepsTheFriendOnTheBar(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(100, 24).WithFriends(Friends{Pick: "skip", Save: func(string) error { return errors.New("read-only") }})
	m, cmd := typeAndSubmit(a, friendsVerb+" ollie")
	_ = runCmdQuietly(cmd)
	if n, _ := notice.Latest(); m.(App).corner.pick != "ollie" || !strings.Contains(n.String(), "read-only") {
		t.Errorf("a failed keep left the pick %q and said %q", m.(App).corner.pick, n.String())
	}
}

func TestAnUnknownOrMissingKeptFriendIsSkip(t *testing.T) {
	for _, kept := range []string{"", "rex", "SKIP"} {
		a := newRoomApp(t).withSize(100, 24).WithFriends(Friends{Pick: kept})
		if a.corner.pick != "skip" {
			t.Errorf("a kept %q drew %q, want Skip", kept, a.corner.pick)
		}
	}
	if a := newRoomApp(t).withSize(100, 24).WithFriends(Friends{Pick: "None"}); a.corner.pick != friendNone {
		t.Errorf("a kept None drew %q", a.corner.pick)
	}
}

func TestFriendsOpensOverAConversationsComposerToo(t *testing.T) {
	fresh(t)
	conn, _ := pipeClient(t)
	a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex").withSize(160, 30).WithFriends(Friends{Pick: "skip"})
	m, _ := typeAndSubmit(a, friendsVerb)
	a = m.(App)
	if !a.picker.Open() || a.picker.Word != friendsCommand || !strings.Contains(ansi.Strip(a.View()), friendsHint) {
		t.Errorf("/friends in a conversation opened %+v and drew no menu", a.picker)
	}
}

// syncBuffer is a terminal a Cmd's goroutine writes to.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// runAll runs cmd and everything a batch of it holds, each on its own
// goroutine as Bubble Tea would, returning without waiting for any.
func runAll(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		if batch, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range batch {
				runAll(c)
			}
		}
	}()
}

// awaitWritten waits for out to hold want.
func awaitWritten(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the terminal never got the friends' images; it got %d bytes", len(out.String()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheFriendsAreSentAtStartAndAfterAHandOverAndNeverInAFrame(t *testing.T) {
	seq := transmission(t)
	out := &syncBuffer{}
	a := friendRoom(t, true, "skip", &keptFriends{}).WithOutput(out).withUsageReport(aReading, "sydney")
	runAll(a.Init())
	awaitWritten(t, out, seq)
	if strings.Count(out.String(), seq) != 1 {
		t.Errorf("Init sent the images %d times, want once", strings.Count(out.String(), seq))
	}
	if strings.Contains(a.View(), "\x1b_G") {
		t.Error("a frame carries an image escape; only the transmission may")
	}
	for _, m := range []mcpSignedInMsg{{Session: "s1", Server: "x", Err: errors.New("cancelled")}, {Session: "s1", Server: "x"}} {
		out := &syncBuffer{}
		_, cmd := a.WithOutput(out).mcpSignedIn(m)
		runAll(cmd)
		awaitWritten(t, out, seq)
	}
	if cmd := friendRoom(t, false, "skip", &keptFriends{}).WithOutput(out).transmitFriends(); cmd != nil {
		t.Error("a terminal without Kitty graphics is sent the images")
	}
}

// A drag across the friend highlights what it crosses and leaves the friend's
// cells exactly as drawn - the selection's colour on them would name another
// image - copies none of them, and once cleared the corner is as it was.
func TestASelectionOverTheFriendLeavesItsCellsAndCopiesNone(t *testing.T) {
	trueColor(t)
	a := friendRoom(t, true, "skip", &keptFriends{}).withUsageReport(aReading, "sydney", "john")
	before := sidebarRows(a)
	top := a.paneHeight() - friendRows - 2
	a, _ = a.mouse(pressAt(rosterLeft(a), top-1))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: a.layout.Width - 1, Y: top + friendRows})
	if !a.sel.onScreen || a.sel.empty() {
		t.Fatalf("the drag made no screen selection: %+v", a.sel)
	}
	during := sidebarRows(a)
	if got := friendIDs(during); !slices.Equal(got, allOf(friendImage(0, 0), friendCells9x5)) {
		t.Errorf("under the highlight the friend's cells name %v, want Skip's image in all of them", got)
	}
	if during[top-1] == before[top-1] {
		t.Error("the row above the friend was not highlighted; the test drags across nothing")
	}
	if text := a.screenSelectedText(); strings.ContainsRune(text, placeholderRune) || strings.ContainsAny(text, string(placeDiacritics)) {
		t.Errorf("the copy carries the friend's placeholder runes: %q", text)
	}
	a = a.cleared()
	if !slices.Equal(sidebarRows(a), before) {
		t.Error("after the selection the corner is not drawn as it was")
	}
}

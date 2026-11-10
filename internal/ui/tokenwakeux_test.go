package ui

// BUG-67's bar: letting a token nothing on screen shows wait for the next wake
// changes nothing anybody sees. Two windows take the same scripted fleet: one is
// woken for every frame, as main's loop is under steady arrival, the other runs on
// the inbox that lets such a token wait. Both are drawn after every frame and must
// draw the same bytes. And at every one of those moments, for every session and
// one the fleet has never heard of, applied must leave the model exactly as
// observe would - its short path - and a token tokensUnseen names must change nothing drawn.

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// twins are the two windows. Each is built by its own call so they share nothing:
// a transcript writes some maps in place.
type twins struct {
	t        *testing.T
	every    App
	deferred App

	waited, unshown int // tokens the deferred window let wait; tokens tested against the frame
}

func newTwins(t *testing.T, build func() App, w, h int) *twins {
	t.Helper()
	fresh(t)
	holdClock(t, time.Date(2026, 11, 9, 18, 0, 0, 0, time.Local))
	tw := &twins{t: t, every: build(), deferred: build()}
	tw.deferred.in = newInbox()
	tw.msg("size", tea.WindowSizeMsg{Width: w, Height: h})
	return tw
}

func updated(a App, m tea.Msg) App {
	next, _ := a.Update(m)
	return next.(App)
}

// woken is the deferred window run the way listen runs it, until the doorbell is quiet.
func woken(a App) App {
	for rung(a.in) {
		a = updated(a, streamMsg{batch: a.in.take(takeLimit), gen: a.gen})
	}
	return a
}

func (tw *twins) frame(step string, f rpc.Frame) {
	tw.t.Helper()
	tw.every = updated(tw.every, streamMsg{batch: batch{frames: []rpc.Frame{f}}, gen: tw.every.gen})
	tw.deferred.in.add(f)
	if partialFrame(f) && len(tw.deferred.in.ready) == 0 {
		tw.waited++
	}
	tw.deferred = woken(tw.deferred)
	tw.same(step)
}

func (tw *twins) msg(step string, m tea.Msg) {
	tw.t.Helper()
	tw.every = updated(tw.every, m)
	tw.deferred = woken(updated(tw.deferred, m))
	tw.same(step)
}

// do changes both windows by hand, then nudges them so Update's seam - the
// inbox's shown set among it - sees the change.
func (tw *twins) do(step string, f func(App) App) {
	tw.t.Helper()
	tw.every, tw.deferred = f(tw.every), f(tw.deferred)
	tw.msg(step, nudge{})
}

func (tw *twins) keys(step, s string) {
	tw.t.Helper()
	for _, r := range s {
		tw.msg(step, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// same fails unless both windows draw the same frame; unless applied's answer to a
// token is observe's, field for field, for every session and one nobody reported;
// and unless a token tokensUnseen names leaves the frame as it is.
func (tw *twins) same(step string) {
	tw.t.Helper()
	want, got := tw.every.View(), tw.deferred.View()
	if want != got {
		tw.t.Fatalf("%s: the window that lets tokens wait draws a different frame:\n%s", step, frameDiff(want, got))
	}
	unseen := tw.every.tokensUnseen()
	for _, id := range append(slices.Clone(tw.every.fleet.order), "s-nobody-reported") {
		tok := core.Event{Kind: core.KindPartialText, SessionID: id, Text: "unseen "}
		full := tw.every.observe(id, tok)
		if path, same := sameModel(full, tw.every.applied([]rpc.Frame{tokenFrame(id, tok.Text)})); !same {
			tw.t.Fatalf("%s: applied's answer to a token for %s differs from observe's at %s", step, id, path)
		}
		if !slices.Contains(unseen, id) {
			continue
		}
		tw.unshown++
		if after := full.View(); after != want {
			tw.t.Fatalf("%s: a token for %s, which tokensUnseen names, changed the frame:\n%s", step, id, frameDiff(want, after))
		}
	}
}

// sameModel walks two models field by field and names the first that differs. A
// func is not compared: nothing a token does writes one.
func sameModel(x, y App) (string, bool) {
	return sameValue(reflect.ValueOf(x), reflect.ValueOf(y), "App", map[[2]uintptr]bool{})
}

func sameValue(x, y reflect.Value, path string, seen map[[2]uintptr]bool) (string, bool) {
	switch x.Kind() {
	case reflect.Func:
		return "", true
	case reflect.Pointer, reflect.Map:
		// Remembered by address: one object, or a cycle already being compared.
		if x.IsNil() || y.IsNil() {
			return path, x.IsNil() == y.IsNil()
		}
		key := [2]uintptr{x.Pointer(), y.Pointer()}
		if key[0] == key[1] || seen[key] {
			return "", true
		}
		seen[key] = true
	}
	switch x.Kind() {
	case reflect.Pointer, reflect.Interface:
		if x.IsNil() || y.IsNil() {
			return path, x.IsNil() == y.IsNil()
		}
		if x.Elem().Type() != y.Elem().Type() {
			return path, false
		}
		return sameValue(x.Elem(), y.Elem(), path, seen)
	case reflect.Struct:
		for i := range x.NumField() {
			if p, ok := sameValue(x.Field(i), y.Field(i), path+"."+x.Type().Field(i).Name, seen); !ok {
				return p, false
			}
		}
	case reflect.Slice, reflect.Array:
		if x.Len() != y.Len() {
			return path, false
		}
		for i := range x.Len() {
			if p, ok := sameValue(x.Index(i), y.Index(i), fmt.Sprintf("%s[%d]", path, i), seen); !ok {
				return p, false
			}
		}
	case reflect.Map:
		if x.Len() != y.Len() {
			return path, false
		}
		for _, k := range x.MapKeys() {
			v := y.MapIndex(k)
			if !v.IsValid() {
				return fmt.Sprintf("%s[%v]", path, k), false
			}
			if p, ok := sameValue(x.MapIndex(k), v, fmt.Sprintf("%s[%v]", path, k), seen); !ok {
				return p, false
			}
		}
	case reflect.Chan, reflect.UnsafePointer:
		return path, x.Pointer() == y.Pointer()
	case reflect.String:
		return path, x.String() == y.String()
	case reflect.Bool:
		return path, x.Bool() == y.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return path, x.Int() == y.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return path, x.Uint() == y.Uint()
	case reflect.Float32, reflect.Float64:
		return path, x.Float() == y.Float()
	case reflect.Complex64, reflect.Complex128:
		return path, x.Complex() == y.Complex()
	}
	return "", true
}

func frameDiff(want, got string) string {
	w, g := strings.Split(stripANSI(want), "\n"), strings.Split(stripANSI(got), "\n")
	var b strings.Builder
	for i := range max(len(w), len(g)) {
		var x, y string
		if i < len(w) {
			x = w[i]
		}
		if i < len(g) {
			y = g[i]
		}
		if x != y {
			fmt.Fprintf(&b, "row %d\n  want %q\n  got  %q\n", i, x, y)
		}
	}
	if b.Len() == 0 {
		return "(the rows match once styling is stripped: the difference is colour or style)"
	}
	return b.String()
}

// shows fails unless the frame draws each of want: a step the script means to
// reach, reached.
func (tw *twins) shows(step string, want ...string) {
	tw.t.Helper()
	frame := stripANSI(tw.every.View())
	for _, w := range want {
		if !strings.Contains(frame, w) {
			tw.t.Fatalf("%s: the frame does not draw %q, so the script did not reach what it tests:\n%s", step, w, frame)
		}
	}
}

// The fleet: alex's conversation open beside the room, the others off screen.
// bea and cy are the api team.
func uxFleet(states map[string]string) *rpc.Status {
	st := &rpc.Status{Running: true}
	for _, s := range []rpc.SessionStatus{
		{ID: "s1", Name: "alex"}, {ID: "s2", Name: "bea", Team: "api"}, {ID: "s3", Name: "cy", Team: "api"},
		{ID: "s4", Name: "dot"}, {ID: "s5", Name: "eve"},
	} {
		s.State = rpc.StateWorking
		if v, ok := states[s.ID]; ok {
			s.State = v
		}
		if s.State == rpc.StateBlocked {
			s.RequestIDs = []string{"r1"}
		}
		st.Sessions = append(st.Sessions, s)
	}
	return st
}

func statusFrame(st *rpc.Status) rpc.Frame { return rpc.Frame{Kind: rpc.FrameStatusPush, Status: st} }

func turnTokensFrame(id string, n int) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
		Kind: core.KindTurnTokens, SessionID: id, Session: &core.SessionFacts{TurnOutputTokens: n},
	}}
}

func retryOn(id string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
		Kind: core.KindSystem, SessionID: id, Notice: core.NoticeAPIRetrying,
		Retry: &core.APIRetry{Attempt: 3, Max: 10, Delay: 38 * time.Second},
	}}
}

// talk streams n words of id's answer, interleaved with a word from each of the
// others, so drawn and undrawn tokens arrive as a fleet's do.
func (tw *twins) talk(step string, n int, ids ...string) {
	tw.t.Helper()
	for i := range n {
		for _, id := range ids {
			tw.frame(fmt.Sprintf("%s: %s word %d", step, id, i), tokenFrame(id, fmt.Sprintf("%s%03d ", id, i)))
		}
	}
}

func TestLettingTokensWaitChangesNothingDrawn(t *testing.T) {
	tw := newTwins(t, func() App {
		return NewRoomApp(nil, Stream{}, uxFleet(map[string]string{"s2": rpc.StateIdle})).WithOpenDM("s1", "alex")
	}, 200, 45)

	// bea's turn starts while this window watches, so its end draws a done line.
	tw.frame("bea starts working", statusFrame(uxFleet(nil)))
	for _, id := range []string{"s1", "s2", "s3", "s4"} {
		tw.frame("message start "+id, startFrame(id))
	}
	tw.talk("the fleet streams", 6, "s1", "s2", "s3", "s4")
	tw.frame("bea's figure", turnTokensFrame("s2", 1240))
	tw.frame("cy's block lands", eventFrame("s3", "cy finished the schema pass."))
	tw.talk("streaming on", 3, "s2", "s1", "s3")

	// A narrowed room: a lone @name, then a lone @team.
	tw.do("the room takes the keys", func(a App) App { return a.refocus("") })
	tw.keys("narrowed to bea", "@bea ")
	tw.shows("narrowed to bea", "group chat › @bea")
	tw.talk("narrowed to bea", 3, "s2", "s3", "s1")
	tw.frame("bea's block lands in her thread", eventFrame("s2", "bea wrote the migration."))
	for range len("@bea ") {
		tw.msg("draft cleared", tea.KeyMsg{Type: tea.KeyBackspace})
	}
	tw.keys("narrowed to the api team", "@api ")
	tw.shows("narrowed to the api team", "group chat › @api")
	tw.talk("narrowed to the api team", 3, "s3", "s2", "s4")
	for range len("@api ") {
		tw.msg("draft cleared", tea.KeyMsg{Type: tea.KeyBackspace})
	}

	// A retry the next token clears, the token never preceded by a message start.
	tw.frame("dot's request is retried", retryOn("s4"))
	tw.shows("dot's request is retried", "⚠ dot")
	tw.frame("dot's API answers", tokenFrame("s4", "answered "))
	tw.shows("dot's API answered", "· dot")
	tw.talk("dot streams after the retry", 3, "s4", "s1")

	// An ask, and an API failure's notice.
	tw.frame("eve asks", statusFrame(uxFleet(map[string]string{"s5": rpc.StateBlocked})))
	tw.frame("eve's card", askFrame("s5", "r1"))
	tw.frame("cy's API fails", apiErrorFrame("s3", "API Error: 500 Internal server error"))
	tw.shows("eve asks", "⚠ eve wants Bash", "1 need you", "API Error: 500")
	tw.talk("streaming past the ask", 3, "s2", "s1")

	// bea goes idle with her tokens waiting unrung: the ✔ and her done line.
	tw.talk("bea's last words", 4, "s2")
	tw.frame("bea's block lands", eventFrame("s2", "bea's migration is in."))
	tw.frame("bea's turn ends", kindFrame("s2", core.KindTurnEnd, ""))
	tw.frame("bea reads idle", statusFrame(uxFleet(map[string]string{"s2": rpc.StateIdle, "s5": rpc.StateBlocked})))
	tw.shows("bea reads idle", "✔ bea", "✔ 1 done")
	tw.talk("the rest stream on", 2, "s3", "s1")
	tw.do("bea's conversation opens below alex's", func(a App) App { return a.refocus("s1").openBelow("s2", "bea") })
	tw.shows("bea's conversation opens below alex's", "done 6:00 PM", "@alex ─", "@bea ─")
	tw.talk("with bea open", 2, "s1", "s3", "s4")

	// alex's pane on a subagent, then on /workflows.
	tw.frame("alex dispatches", taskFrame("s1", started("a1", subDispatch, "Count lines", "general-purpose", core.TaskAgent)))
	tw.frame("the subagent speaks", taskFrame("s1", spoke(subDispatch, subSaid)))
	tw.do("alex's pane shows the subagent", func(a App) App {
		a.roster = Roster{Selected: "s1", SelectedTask: subDispatch}
		return a.refocus("s1").viewingPicked("s1")
	})
	tw.shows("alex's pane shows the subagent", subSaid)
	tw.talk("streaming under a subagent pane", 3, "s1", "s3", "s4")
	tw.do("back to alex's own transcript", func(a App) App {
		a.roster = Roster{Selected: "s1"}
		return a.viewingPicked("s1")
	})
	tw.keys("/workflows typed", "/workflows")
	tw.msg("/workflows opens", tea.KeyMsg{Type: tea.KeyEnter})
	tw.shows("/workflows opens", "@alex › workflows")
	tw.talk("streaming under /workflows", 3, "s1", "s3", "s4")
	tw.msg("/workflows closes", tea.KeyMsg{Type: tea.KeyEsc})

	// A resize mid-stream that keeps both panes on screen: the settle pending, then committed.
	tw.msg("the window narrows", tea.WindowSizeMsg{Width: 170, Height: 42})
	tw.talk("streaming mid-settle", 3, "s1", "s3", "s4")
	tw.msg("the settle commits", geometrySettledMsg{gen: tw.every.geoGen})
	tw.talk("streaming after the settle", 3, "s1", "s3", "s4")

	// The board, tiled: every tile's tokens are drawn. It comes up with nothing
	// waiting - a tile that comes on screen mid-answer is the next test's.
	tw.frame("a report wakes the loop", statusFrame(uxFleet(map[string]string{"s2": rpc.StateIdle, "s5": rpc.StateBlocked})))
	tw.do("the board goes up, tiled", func(a App) App {
		a.board = Board{Up: true, Tiled: true}
		return a
	})
	tw.talk("streaming under the board", 4, "s3", "s4", "s1")
	tw.shows("streaming under the board", "BOARD", "s3003", "s1003")
	tw.frame("dot's block lands", eventFrame("s4", "dot's sweep is done."))

	// The script has to have let tokens wait, and tested tokens against the frame.
	if tw.waited < 50 || tw.unshown < 200 {
		t.Fatalf("the deferred window let %d tokens wait and %d were tested against the frame: the script tests too little",
			tw.waited, tw.unshown)
	}
}

// A pane that comes on screen mid-answer is the one place the two windows may
// differ, and only one way: a token main refused while the pane was off screen,
// the deferred window hands the pane once it is drawn, because it was waiting
// still. So the pane draws every word main drew and some before them, ending in
// the newest, with no hole main does not have, and the landed block draws the
// same in both.
func TestAPaneComingOnScreenMidAnswerDrawsAtLeastWhatMainDid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		speaker string
		open    string // the conversation open beside the room from the start
		w, h    int
		off     func(*twins) // the speaker's pane goes off screen before its answer starts
		midway  func(*twins) // or once it has drawn the answer's first words
		on      func(*twins)
	}{
		{name: "opened with ⌃D", speaker: "s2", open: "s1", w: 200, h: 45, on: func(tw *twins) {
			tw.every.roster, tw.deferred.roster = Roster{Selected: "s2"}, Roster{Selected: "s2"}
			tw.onScreen(tea.KeyMsg{Type: tea.KeyCtrlD})
		}},
		{name: "a narrow window hides it and a wide one brings it back", speaker: "s1", open: "s1", w: 200, h: 45,
			off: func(tw *twins) {
				tw.do("the room takes the keys", func(a App) App { return a.refocus("") })
				tw.msg("the window narrows past two columns", tea.WindowSizeMsg{Width: dmTakeoverColumns - 10, Height: 45})
				tw.msg("the settle commits", geometrySettledMsg{gen: tw.every.geoGen})
			},
			on: func(tw *twins) {
				tw.onScreen(tea.WindowSizeMsg{Width: 200, Height: 45})
				tw.onScreen(geometrySettledMsg{gen: tw.every.geoGen})
			}},
		// Main glues the words after the hole onto the ones before it here (BUG-69);
		// the deferred window may fill some of it, never widen it.
		{name: "a narrow window hides it mid-answer and a wide one brings it back", speaker: "s1", open: "s1", w: 200, h: 45,
			midway: func(tw *twins) {
				tw.do("the room takes the keys", func(a App) App { return a.refocus("") })
				tw.msg("the window narrows past two columns", tea.WindowSizeMsg{Width: dmTakeoverColumns - 10, Height: 45})
				tw.msg("the settle commits", geometrySettledMsg{gen: tw.every.geoGen})
			},
			on: func(tw *twins) {
				tw.onScreen(tea.WindowSizeMsg{Width: 200, Height: 45})
				tw.onScreen(geometrySettledMsg{gen: tw.every.geoGen})
			}},
		{name: "the board comes up tiled", speaker: "s2", open: "s1", w: 200, h: 45, on: func(tw *twins) {
			tw.every.board, tw.deferred.board = Board{Up: true, Tiled: true}, Board{Up: true, Tiled: true}
			tw.onScreen(nudge{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tw := newTwins(t, func() App {
				return NewRoomApp(nil, Stream{}, uxFleet(nil)).WithOpenDM(tc.open, "alex")
			}, tc.w, tc.h)
			other := "s1"
			if tc.speaker == "s1" {
				other = "s3"
			}
			if tc.off != nil {
				tc.off(tw)
			}
			tw.frame("message start", startFrame(tc.speaker))
			n := 0
			say := func() rpc.Frame { n++; return tokenFrame(tc.speaker, fmt.Sprintf("%s%03d ", tc.speaker, n)) }
			if tc.midway != nil {
				for range 4 {
					tw.frame("on screen", say())
				}
				tc.midway(tw)
			}
			for range 6 {
				tw.frame("off screen, with the fleet waking the loop", say())
				tw.frame("the fleet", tokenFrame(other, "elsewhere "))
				tw.frame("the fleet", eventFrame("s4", "a block wakes the loop"))
			}
			for range 4 {
				tw.frame("off screen, waiting", say())
			}
			tc.on(tw)
			for i := range 6 {
				tw.both(say())
				tw.both(tokenFrame(other, "elsewhere "))
				if longer := tw.atLeastMain(t, tc.speaker, n); i == 0 && !longer {
					t.Fatal("the pane came on screen with no waiting token handed to it: the case tests nothing main does not")
				}
			}
			var block strings.Builder
			for i := 1; i <= n; i++ {
				fmt.Fprintf(&block, "%s%03d ", tc.speaker, i)
			}
			tw.frame("the block lands", eventFrame(tc.speaker, strings.TrimSpace(block.String())))
		})
	}
}

// onScreen sends m to both windows without comparing them: it brings a pane on
// screen, where they may differ.
func (tw *twins) onScreen(m tea.Msg) {
	tw.every = updated(tw.every, m)
	tw.deferred = woken(updated(tw.deferred, m))
}

// both sends f to both windows without comparing them.
func (tw *twins) both(f rpc.Frame) {
	tw.every = updated(tw.every, streamMsg{batch: batch{frames: []rpc.Frame{f}}, gen: tw.every.gen})
	tw.deferred.in.add(f)
	tw.deferred = woken(tw.deferred)
}

// atLeastMain fails unless the deferred window draws the speaker's words once each
// and in order, every word main draws, ending on the newest, n, with no hole main
// lacks: a word it skips, main skips too. It reports whether it draws more.
func (tw *twins) atLeastMain(t *testing.T, speaker string, n int) bool {
	t.Helper()
	words := func(a App) []int {
		var out []int
		for _, m := range regexpWords(speaker).FindAllStringSubmatch(stripANSI(a.View()), -1) {
			i, err := strconv.Atoi(m[1])
			if err != nil {
				t.Fatalf("%q is not a word number: %v", m[1], err)
			}
			out = append(out, i)
		}
		return out
	}
	d, e := words(tw.deferred), words(tw.every)
	for i := 1; i < len(d); i++ {
		if d[i] <= d[i-1] {
			t.Fatalf("the deferred window draws %v of %s's words: %d after %d", d, speaker, d[i], d[i-1])
		}
	}
	if len(d) == 0 || d[len(d)-1] != n {
		t.Fatalf("the deferred window draws %v of %s's words, want them ending in %d", d, speaker, n)
	}
	for _, w := range e {
		if !slices.Contains(d, w) {
			t.Fatalf("main draws %v of %s's words and the deferred window %v: it misses %d", e, speaker, d, w)
		}
	}
	for _, k := range holes(d) {
		if !slices.Contains(holes(e), k) {
			t.Fatalf("the deferred window draws %v of %s's words and main %v: a hole at %d main does not have", d, speaker, e, k)
		}
	}
	return len(d) > len(e)
}

// holes is every number missing between the first and last of an ascending run.
func holes(run []int) []int {
	var out []int
	for i := 1; i < len(run); i++ {
		for k := run[i-1] + 1; k < run[i]; k++ {
			out = append(out, k)
		}
	}
	return out
}

func regexpWords(speaker string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(speaker) + `(\d{3})`)
}

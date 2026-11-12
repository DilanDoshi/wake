package ui

// A token for a session whose tokens nothing on screen draws waits in its one
// slot for the next frame that wakes the loop, rather than waking it to draw a
// frame that is the same (BUG-67).

import (
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// rung reports whether the doorbell was rung, and clears it.
func rung(b *inbox) bool {
	select {
	case <-b.ready:
		return true
	default:
		return false
	}
}

func TestATokenForASessionNothingShowsRingsNothing(t *testing.T) {
	b := newInbox()
	b.setUnseen([]string{"s2"})
	b.add(tokenFrame("s2", "hidden "))
	if rung(b) {
		t.Fatal("a token for s2, whose tokens nothing on screen draws, woke the draw loop")
	}
	b.add(tokenFrame("s1", "shown "))
	if !rung(b) {
		t.Fatal("a token for s1, whose pane is drawn, did not wake the draw loop")
	}
}

func TestBeforeTheWindowSaysWhatItShowsEveryTokenRings(t *testing.T) {
	b := newInbox()
	b.add(tokenFrame("s2", "early "))
	if !rung(b) {
		t.Fatal("a token arriving before the window said what it shows did not wake the loop")
	}
}

func TestAWaitingTokenArrivesWithTheNextFrameAndInOrder(t *testing.T) {
	b := newInbox()
	b.setUnseen([]string{"s2"})
	b.add(tokenFrame("s2", "one "))
	b.add(tokenFrame("s2", "two "))
	b.add(eventFrame("s3", "a block"))
	if !rung(b) {
		t.Fatal("a block did not wake the loop")
	}
	got := b.take(takeLimit).frames
	if len(got) != 2 || got[0].SessionID != "s2" || got[0].Event.Text != "one two " || got[1].SessionID != "s3" {
		t.Fatalf("the batch is %+v, want s2's waiting tokens folded and then s3's block", got)
	}
}

func TestShowingASessionRingsForTheTokensWaitingForIt(t *testing.T) {
	b := newInbox()
	b.setUnseen([]string{"s2", "s3"})
	b.add(tokenFrame("s2", "waiting "))
	b.setUnseen([]string{"s3"})
	if !rung(b) {
		t.Fatal("s2 came on screen with tokens waiting for it and the loop was not woken to draw them")
	}
	if got := b.take(takeLimit).frames; len(got) != 1 || got[0].Event.Text != "waiting " {
		t.Fatalf("the batch is %+v, want s2's waiting token", got)
	}
}

func TestSayingTheSameUnseenSetAgainRingsNothing(t *testing.T) {
	b := newInbox()
	b.setUnseen([]string{"s2"})
	b.add(tokenFrame("s2", "waiting "))
	b.setUnseen([]string{"s2"})
	if rung(b) {
		t.Fatal("an unchanged unseen set woke the loop")
	}
}

// A session nothing shows, streaming for as long as anybody likes with nothing
// else waking the loop, costs one slot of at most foldChars - however long it talks.
func TestALongStreamNobodyIsShownHoldsOneSlotEach(t *testing.T) {
	b := newInbox()
	var all []string
	for i := range benchFleetSize {
		all = append(all, benchAgentID(i))
	}
	b.setUnseen(all)
	talk := func(tokens int) {
		for range tokens {
			for i := range benchFleetSize {
				b.add(tokenFrame(benchAgentID(i), "token "))
			}
		}
	}
	heap := func() uint64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	talk(1_000)
	before := heap()
	talk(10_000)
	after := heap()

	if rung(b) {
		t.Fatal("tokens nothing shows woke the loop")
	}
	if b.n != benchFleetSize {
		t.Fatalf("the ring holds %d frames for %d talking sessions, want one each", b.n, benchFleetSize)
	}
	for i := range b.n {
		if f := b.ring[(b.head+i)%len(b.ring)]; len(f.Event.Text) > foldChars || !f.Lost {
			t.Fatalf("slot %d holds %d bytes (Lost %v), want at most %d and marked Lost", i, len(f.Event.Text), f.Lost, foldChars)
		}
	}
	const slack = 1 << 20
	if after > before+slack {
		t.Fatalf("the heap grew from %d to %d bytes over ten times the stream, want it flat", before, after)
	}
}

// Under steady arrival the loop drains faster than the fleet talks, so every token
// that rings is a whole View of its own - BUG-67's 1,000 a second. Only the drawn
// conversation's tokens may ring; the others ride the frames that do.
func TestOnlyTheDrawnConversationsTokensWakeTheLoop(t *testing.T) {
	a := withInbox(t, streamingApp(t, 1))
	wakes := 0
	const rounds = medianTokensPerSecond
	for range rounds {
		for i := range benchFleetSize {
			a.in.add(tokenFrame(benchAgentID(i), "word "))
			a, wakes = drained(a, wakes)
		}
	}
	if wakes != rounds {
		t.Fatalf("a second of %d agents' tokens woke the loop %d times, want %d: one per token of the drawn conversation",
			benchFleetSize, wakes, rounds)
	}
	if text := a.dms[benchAgentID(0)].partial.text; strings.Count(text, "word") != rounds {
		t.Fatalf("the drawn conversation holds %q, want all %d of its tokens", text, rounds)
	}
}

// withInbox gives a model built without a connection an inbox, and lets one Update
// tell it what the window shows, as the first message of a real run does.
func withInbox(t *testing.T, a App) App {
	t.Helper()
	a.in = newInbox()
	m, _ := a.Update(tea.WindowSizeMsg{Width: a.layout.Width, Height: a.layout.Height})
	return m.(App)
}

// drained runs the loop the way listen does until the doorbell is quiet, counting
// the wakes.
func drained(a App, wakes int) (App, int) {
	for rung(a.in) {
		wakes++
		m, _ := a.Update(streamMsg{batch: a.in.take(takeLimit), gen: a.gen})
		a = m.(App)
	}
	return a, wakes
}

// The unseen set crosses from the draw loop to the pump. Raced here with nothing
// else between them - no glamour, no App - so the race lane sees only the inbox's
// own lock ordering the two.
func TestTheUnseenSetCrossesToThePumpUnderTheInboxsLock(t *testing.T) {
	b := newInbox()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 2_000 {
			b.add(tokenFrame(benchAgentID(i%benchFleetSize), "word "))
			if i%64 == 0 {
				b.take(takeLimit)
			}
		}
	}()
	for i := range 2_000 {
		b.setUnseen([]string{benchAgentID(i % benchFleetSize)})
	}
	<-done
}

// applied judges a run of waiting tokens once, so a frame folded in full between
// two of them has to be heard: here a retry makes the next token one that shows,
// because it clears the retry's line.
func TestATokenAfterAFrameThatShowsItsSessionIsFoldedInFull(t *testing.T) {
	a := streamingApp(t, 1)
	id := benchAgentID(3)
	a = a.applied([]rpc.Frame{tokenFrame(id, "before "), retryOn(id), tokenFrame(id, "answered ")})
	if _, standing := a.retrying[id]; standing {
		t.Fatal("a token after the retry, in the same batch, was judged by the run before it and left the retry standing")
	}
}

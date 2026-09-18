package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// The trigger is a pure function so the one decision that can close somebody's
// window by accident is testable without a terminal, a pty or a process.

// alignedCut holds back an unfinished escape sequence so a dropped chunk lands
// on a boundary. esc is the CSI opener spelled here so a case reads as bytes.
const esc = "\x1b"

func TestAlignedCutHoldsBackAnUnfinishedReport(t *testing.T) {
	report := esc + "[<64;10;5M"
	cases := []struct {
		name string
		buf  string
		want int
	}{
		{"a whole mouse report", report, len(report)},
		{"a whole report then a partial one", report + esc + "[<64;10;", len(report)},
		{"a partial report on its own", esc + "[<64;10;", 0},
		{"a CSI opening with nothing after the bracket", esc + "[", 0},
		{"a CSI with no final byte yet", esc + "[<64;10;5", 0},
		{"no escape at all is all runes", "hello", len("hello")},
		{"a lone trailing ESC waits", "ab" + esc, len("ab")},
		{"a whole report then a lone ESC", report + esc, len(report)},
		{"a whole X10 mouse report", esc + "[M\x20\x21\x22", len(esc + "[M\x20\x21\x22")},
		{"an X10 report missing a coordinate byte", esc + "[M\x20\x21", 0},
		{"a whole SS3 key", esc + "OP", len(esc + "OP")},
		{"an SS3 key missing its final byte", esc + "O", 0},
		{"an Alt+key is whole", esc + "x", len(esc + "x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := alignedCut([]byte(tc.buf)); got != tc.want {
				t.Fatalf("alignedCut(%q) = %d, want %d", tc.buf, got, tc.want)
			}
		})
	}
}

// The whole point, deterministically and without a terminal: drop one chunk from
// the middle of a scrolled fleet's mouse flood, and the survivors either side of
// the gap are still whole reports. The raw reads are shown to split under the same
// drop, so the test is proving a real difference rather than a tautology - the raw
// fragment is exactly the `<`, digits and `M` Bubble Tea typed into the composer.
func TestADroppedChunkSplitsARawReadButNotAnAlignedOne(t *testing.T) {
	report := esc + "[<64;10;5M"
	// Buffer-sized reads over an 11-byte report: readChunk is not a multiple of
	// 11, so the boundaries fall mid-report - a fast scroll's own reads.
	reads := splitReads(strings.Repeat(report, 4000), readChunk)
	drop := len(reads) / 2

	var raw []byte
	for i, r := range reads {
		if i != drop {
			raw = append(raw, r...)
		}
	}
	if wholeReports(raw, report) {
		t.Fatal("dropping a raw read did not split a report: the scenario is not adversarial, so an aligned pass would prove nothing")
	}

	var c chunker
	var aligned []byte
	for i, r := range reads {
		chunk := c.step(r, len(r) == readChunk)
		if i != drop {
			aligned = append(aligned, chunk...)
		}
	}
	// The stream is whole reports ending on a short read, so nothing is held back.
	if len(c.carry) != 0 {
		t.Fatalf("the chunker held %q after a boundary-ending stream", c.carry)
	}
	if !wholeReports(aligned, report) {
		t.Fatalf("a dropped aligned chunk split a report: survivors near the gap are %q", nearFirstFragment(aligned, report))
	}
}

// A mouse report split across two short reads - what a byte stream over SSH or
// tmux can deliver - must not become two droppable chunks. step holds the partial
// until the report completes, so the report is only ever one whole chunk and a
// drop can never expose half of it. This is the residual the full-read-only version
// left, that both reviews flagged.
func TestAMouseReportSplitAcrossShortReadsIsHeldUntilWhole(t *testing.T) {
	var c chunker
	if got := c.step([]byte(esc+"[<64;10;"), false); len(got) != 0 {
		t.Fatalf("the first half of a split report was forwarded as %q; it must be held whole", got)
	}
	got := c.step([]byte("5M"), false)
	if string(got) != esc+"[<64;10;5M" {
		t.Fatalf("the completed report was %q, want the whole %q", got, esc+"[<64;10;5M")
	}
	if len(c.carry) != 0 {
		t.Fatalf("carry was left holding %q after the report completed", c.carry)
	}
}

// A lone ESC is the one ambiguous carry. On a short read it is a real Escape
// keypress and must go now - holding it would strand ⎋, which interrupts a turn.
// On a full read more is coming, so it opens a sequence and is held.
func TestALoneEscGoesNowOnAShortReadAndWaitsOnAFullOne(t *testing.T) {
	var short chunker
	if got := short.step([]byte(esc), false); string(got) != esc {
		t.Fatalf("a lone ESC on a short read was %q, want it forwarded at once", got)
	}
	if len(short.carry) != 0 {
		t.Fatal("a lone ESC on a short read was held; ⎋ would be stranded")
	}
	var full chunker
	if got := full.step(append(bytes.Repeat([]byte("a"), readChunk-1), keyEsc), true); len(got) != readChunk-1 {
		t.Fatalf("a full read ending in ESC forwarded %d bytes, want the ESC held back", len(got))
	}
	if string(full.carry) != esc {
		t.Fatalf("a full read's trailing ESC was not held; carry is %q", full.carry)
	}
}

// carry never grows past a real sequence's worth, whatever a misbehaving source
// streams into an open ESC - the memory bound forwardQueue promises.
func TestCarryStaysBounded(t *testing.T) {
	var c chunker
	junk := append([]byte(esc+"["), bytes.Repeat([]byte("1"), readChunk)...) // an ESC then only params, never a final byte
	for range 8 {
		c.step(junk, true)
		if len(c.carry) > maxCarry {
			t.Fatalf("carry grew to %d bytes, past the %d bound", len(c.carry), maxCarry)
		}
	}
}

// splitReads cuts s into the reads a fixed-size buffer would return.
func splitReads(s string, size int) [][]byte {
	var reads [][]byte
	for b := []byte(s); len(b) > 0; {
		n := min(size, len(b))
		reads = append(reads, b[:n])
		b = b[n:]
	}
	return reads
}

// wholeReports reports whether b is exactly a run of report with nothing over -
// the property a split leaves broken. The reports are identical, so a prefix walk
// is exact.
func wholeReports(b []byte, report string) bool {
	r := []byte(report)
	for len(b) > 0 {
		if !bytes.HasPrefix(b, r) {
			return false
		}
		b = b[len(r):]
	}
	return true
}

// nearFirstFragment is the bytes around the first place b stops being whole
// reports, for a failure message.
func nearFirstFragment(b []byte, report string) []byte {
	r := []byte(report)
	for len(b) > len(r) && bytes.HasPrefix(b, r) {
		b = b[len(r):]
	}
	return b[:min(24, len(b))]
}

func TestTwoCtrlCsInTheWindowFires(t *testing.T) {
	at := time.Unix(0, 0)
	k, fired := killTrigger{}.saw([]byte{keyCtrlC, keyCtrlC}, at)
	if !fired {
		t.Fatalf("saw(⌃C ⌃C) did not fire, trigger now %+v", k)
	}
}

// ctrlQ is the byte the kill-switch deliberately does NOT watch any more. It is
// spelled raw rather than as a const, because keyCtrlQ was removed with the
// behaviour - see TestCtrlQNeverFiresTheEmergencyExit.
const ctrlQ = 0x11

// ⌃Q must never fire the emergency exit, at any repetition or spacing.
//
// This is the whole of the fix. ⌃Q now arms and confirms a real park in the
// TUI, and that park waits up to three seconds for the daemon's acknowledgement
// before the window closes. While ⌃Q was also an emergency chord here, a held
// key auto-repeating - or an impatient second tap during that visible delay -
// arrived as ⌃Q⌃Q in one read and fired this exit, which leaves the fleet
// untouched. So a healthy park was pre-empted into a bare exit and every agent
// was left running. Removing ⌃Q from the watched set is what closes that.
func TestCtrlQNeverFiresTheEmergencyExit(t *testing.T) {
	at := time.Unix(0, 0)
	cases := []struct {
		name  string
		bytes []byte
	}{
		{"two ⌃Q in one read", []byte{ctrlQ, ctrlQ}},
		{"a held ⌃Q auto-repeating", []byte{ctrlQ, ctrlQ, ctrlQ, ctrlQ, ctrlQ}},
		{"a single ⌃Q", []byte{ctrlQ}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, fired := (killTrigger{}).saw(tc.bytes, at); fired {
				t.Fatalf("saw(%v) fired the emergency exit; ⌃Q leaves the fleet untouched and must not "+
					"pre-empt the park - the escape hatch is ⌃C⌃C alone now", tc.bytes)
			}
		})
	}
	// And two ⌃Q spanning two reads inside the window must not fire either - the
	// slow double press is what a person does, and it is the case the emergency
	// used to catch.
	k, _ := killTrigger{}.saw([]byte{ctrlQ}, at)
	if _, fired := k.saw([]byte{ctrlQ}, at.Add(50*time.Millisecond)); fired {
		t.Fatal("two ⌃Q across two reads fired the emergency exit")
	}
	// A ⌃Q between two ⌃C disarms the emergency, exactly as any other key does.
	if _, fired := (killTrigger{}).saw([]byte{keyCtrlC, ctrlQ, keyCtrlC}, at); fired {
		t.Fatal("⌃C ⌃Q ⌃C fired; the ⌃Q in the middle must take the arm back like any other byte")
	}
}

// A fast double press shares one read and a slow one does not, and the trigger
// has to see both as the same thing. This is escprobe_test.go's finding about
// ⎋⎋ one layer down: a build that only looked at the first byte of a read works
// for slow presses and silently fails under a finger.
func TestOneReadAndTwoReadsAreTheSamePress(t *testing.T) {
	at := time.Unix(0, 0)
	if _, fired := (killTrigger{}).saw([]byte{keyCtrlC, keyCtrlC}, at); !fired {
		t.Fatal("two ⌃C sharing one read did not fire")
	}
	k, _ := killTrigger{}.saw([]byte{keyCtrlC}, at)
	if _, fired := k.saw([]byte{keyCtrlC}, at.Add(50*time.Millisecond)); !fired {
		t.Fatal("two ⌃C in two reads did not fire")
	}
}

func TestOnePressNeverFires(t *testing.T) {
	if _, fired := (killTrigger{}).saw([]byte{keyCtrlC}, time.Unix(0, 0)); fired {
		t.Fatal("a single ⌃C fired the emergency quit")
	}
}

// Anything between the two presses takes the arm back, which is what keeps ⌃C
// meaning park: parking two agents needs the roster cursor moved between them,
// and the arrow is the byte that disarms. ⌃Q is one such byte now that it is no
// longer watched - see TestCtrlQNeverFiresTheEmergencyExit.
func TestAnythingBetweenThePressesDisarms(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
	}{
		{"an arrow between two parks", []byte{keyCtrlC, 0x1b, '[', 'B', keyCtrlC}},
		{"a typed character", []byte{keyCtrlC, 'a', keyCtrlC}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, fired := (killTrigger{}).saw(tc.bytes, time.Unix(0, 0)); fired {
				t.Fatalf("saw(%v) fired; %s must not be an emergency quit", tc.bytes, tc.name)
			}
		})
	}
}

// The window is what separates a panic from two ordinary parks minutes apart.
func TestASlowSecondPressIsTwoParks(t *testing.T) {
	at := time.Unix(0, 0)
	k, fired := killTrigger{}.saw([]byte{keyCtrlC}, at)
	if fired {
		t.Fatal("the first press fired")
	}
	if _, fired = k.saw([]byte{keyCtrlC}, at.Add(killWindow+time.Millisecond)); fired {
		t.Fatalf("a second press %v after the first fired; the window is %v", killWindow+time.Millisecond, killWindow)
	}
}

// A press just inside the window still fires - the bound is the only thing
// separating the two cases, so both sides of it are asserted.
func TestAPressInsideTheWindowFires(t *testing.T) {
	at := time.Unix(0, 0)
	k, _ := killTrigger{}.saw([]byte{keyCtrlC}, at)
	if _, fired := k.saw([]byte{keyCtrlC}, at.Add(killWindow-time.Millisecond)); !fired {
		t.Fatal("a second press inside the window did not fire")
	}
}

// A third press is not a second quit. Once it has fired the process is on its
// way out; re-arming would be a second exit racing the first.
func TestTheTriggerIsSpentOnceItFires(t *testing.T) {
	at := time.Unix(0, 0)
	k, fired := killTrigger{}.saw([]byte{keyCtrlC, keyCtrlC}, at)
	if !fired {
		t.Fatal("did not fire")
	}
	if _, again := k.saw([]byte{keyCtrlC}, at); again {
		t.Fatal("a third press fired a second emergency quit")
	}
}

package main

import (
	"testing"
	"time"
)

// The trigger is a pure function so the one decision that can close somebody's
// window by accident is testable without a terminal, a pty or a process.

func TestTwoOfTheSameKeyInTheWindowFires(t *testing.T) {
	at := time.Unix(0, 0)
	cases := []struct {
		name  string
		bytes []byte
	}{
		{"two ctrl-c", []byte{keyCtrlC, keyCtrlC}},
		{"two ctrl-q", []byte{keyCtrlQ, keyCtrlQ}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, fired := killTrigger{}.saw(tc.bytes, at)
			if !fired {
				t.Fatalf("saw(%v) did not fire, trigger now %+v", tc.bytes, k)
			}
		})
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
	for _, b := range []byte{keyCtrlC, keyCtrlQ} {
		if _, fired := (killTrigger{}).saw([]byte{b}, time.Unix(0, 0)); fired {
			t.Fatalf("a single %#x fired the emergency quit", b)
		}
	}
}

// Anything between the two presses takes the arm back, which is what keeps ⌃C
// meaning park: parking two agents needs the roster cursor moved between them,
// and the arrow is the byte that disarms.
func TestAnythingBetweenThePressesDisarms(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
	}{
		{"an arrow between two parks", []byte{keyCtrlC, 0x1b, '[', 'B', keyCtrlC}},
		{"a typed character", []byte{keyCtrlC, 'a', keyCtrlC}},
		{"the other quit key", []byte{keyCtrlC, keyCtrlQ}},
		{"the other quit key, reversed", []byte{keyCtrlQ, keyCtrlC}},
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

package main

import (
	"testing"
	"time"
)

// The trigger is a pure function so the one decision that can close somebody's
// window by accident is testable without a terminal, a pty or a process.

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

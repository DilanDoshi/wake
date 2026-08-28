package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// /color carries the target and the colour, the same grammar /name and /task
// use: a bare colour is the conversation you are in, an @who is that agent. The
// value is validated by the daemon, so the client sends what was typed - "none"
// included, which the daemon reads as clear.
func TestColorCarriesTheTargetAndTheColour(t *testing.T) {
	for _, tc := range []struct {
		name, draft, session, text string
		room                       bool
	}{
		{name: "colour this conversation", draft: "/color green", session: "s1", text: "green"},
		{name: "colour another agent", draft: "/color @sydney violet", session: "s2", text: "violet"},
		{name: "colour from the room", draft: "/color @sydney red", session: "s2", text: "red", room: true},
		{name: "clear a colour", draft: "/color none", session: "s1", text: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			_, cmd := typeAndSubmit(a, tc.draft)
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)

			if f.Kind != rpc.FrameColor {
				t.Fatalf("%q wrote a %q frame, want %q", tc.draft, f.Kind, rpc.FrameColor)
			}
			if f.SessionID != tc.session {
				t.Errorf("%q was addressed to %q, want %q", tc.draft, f.SessionID, tc.session)
			}
			if f.Text != tc.text {
				t.Errorf("%q asked for %q, want %q", tc.draft, f.Text, tc.text)
			}
		})
	}
}

// /color guesses no target and no colour, /name's own rule: the room is not one
// conversation, and a bare /color there has nobody to colour.
func TestColorRefusesRatherThanGuess(t *testing.T) {
	for _, tc := range []struct {
		name, draft, says string
		room              bool
	}{
		{name: "no target in the room", draft: "/color green", says: noColorTarget, room: true},
		{name: "no colour", draft: "/color", says: colorUsage},
		{name: "a colour for nobody", draft: "/color @nobody green", says: noSuchAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			m, cmd := typeAndSubmit(a, tc.draft)
			if cmd != nil {
				t.Fatalf("%q was acted on anyway: %+v", tc.draft, sentFrames(t, m.(App), cmd))
			}
			if got := shown(m.(App)); !strings.Contains(got, tc.says) {
				t.Errorf("%q was refused without saying %q:\n%s", tc.draft, tc.says, got)
			}
		})
	}
}

// The three surfaces the owner named: an agent's turns in the room, its status
// bar, and its roster row all take its identity hue when it has one. Each is a
// style-picker asserted on its foreground rather than on rendered ANSI, which
// go test strips - palette_test.go's own approach.

func TestSpeakerStyleUsesTheIdentityHueWhenSet(t *testing.T) {
	if got := speakerStyle(Agent{Color: "green"}).GetForeground(); got != identityColors["green"] {
		t.Errorf("a coloured agent's room name-tag is %v, want the green hue", got)
	}
	if got := speakerStyle(Agent{}).GetForeground(); got != Accent {
		t.Errorf("an uncoloured agent's name-tag is %v, want the shared Accent", got)
	}
	if got := speakerStyle(Agent{Color: "chartreuse"}).GetForeground(); got != Accent {
		t.Errorf("an unknown colour drew %v rather than falling back to Accent", got)
	}
}

func TestStatusBarStyleUsesTheIdentityHueWhenSet(t *testing.T) {
	if got := barStyle(Agent{Color: "violet"}).GetForeground(); got != identityColors["violet"] {
		t.Errorf("a coloured agent's status bar is %v, want the violet hue", got)
	}
	if got := barStyle(Agent{}).GetForeground(); got != Muted {
		t.Errorf("an uncoloured status bar is %v, want the muted default", got)
	}
}

// The roster row takes the identity hue in its default arm only: the cursor's
// accent and a blocked agent's warn are state, and state wins over identity.
func TestRosterHeadStyleUsesTheIdentityHueButStateWins(t *testing.T) {
	a := Agent{ID: "sess-a", Color: "red", State: rpc.StateIdle}
	if got := (Roster{}).headStyle(a).GetForeground(); got != identityColors["red"] {
		t.Errorf("an idle coloured row is %v, want the red hue", got)
	}
	if got := (Roster{Selected: "sess-a"}).headStyle(a).GetForeground(); got != Accent {
		t.Errorf("the selected row is %v, want Accent: the cursor has to win over identity", got)
	}
	blocked := Agent{ID: "sess-a", Color: "red", State: rpc.StateBlocked}
	if got := (Roster{}).headStyle(blocked).GetForeground(); got == identityColors["red"] {
		t.Error("a blocked coloured row drew its identity hue; the warn colour has to win")
	}
}

// The bar reads the agent's colour now (barStyle), so a /color change has to be
// part of what "changed" means - or the status bar this feature is named for is
// the one of the three surfaces that never recolours on an idle agent, because
// withBar hands back the cached grey one. The picker test above proves barStyle
// returns the hue; this proves the hue reaches the pane through the cache.
// Adversarial review, 2026-08-27.
func TestTheBarIsRedrawnWhenTheColourMoves(t *testing.T) {
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", Model: "claude-opus-5", ContextTokens: 10, ContextWindow: 100}
	d = d.SetSize(fullLegendWidth, 24) // caches the bar with no colour

	bars := countBars(t)
	d.Agent.Color = "green"
	_ = d.View(fullLegendWidth, 24)
	if *bars == 0 {
		t.Error("the status bar was not redrawn after /color set the agent's hue: barKey omits the colour, " +
			"so an idle agent's bar stays grey until an unrelated fact moves")
	}
}

// The identity hues are Wake's own, and the set of names has to match the fence
// the daemon validates against exactly: a name the daemon stores but this
// package has no hue for draws nothing, and a hue with no name can never be
// chosen. So the map is held to rpc.ColorNames in both directions.
func TestIdentityColorsAreABijectionWithTheFence(t *testing.T) {
	for _, name := range rpc.ColorNames {
		if _, ok := identityColors[name]; !ok {
			t.Errorf("rpc.ColorNames has %q and theme.go has no hue for it: /color %s would store a colour "+
				"nothing can draw", name, name)
		}
	}
	for name := range identityColors {
		if !slices.Contains(rpc.ColorNames, name) {
			t.Errorf("theme.go has a hue for %q and rpc.ColorNames does not list it, so it can never be "+
				"chosen: the fence and the palette have drifted", name)
		}
	}
}

func TestIdentityStyleResolvesEveryNameAndNothingElse(t *testing.T) {
	for _, name := range rpc.ColorNames {
		if _, ok := identityStyle(name); !ok {
			t.Errorf("identityStyle(%q) found no style for a name in the set", name)
		}
	}
	// No colour and an unknown colour both resolve to nothing, so a caller draws
	// its default rather than a wrong hue.
	for _, none := range []string{"", "chartreuse"} {
		if _, ok := identityStyle(none); ok {
			t.Errorf("identityStyle(%q) returned a style; a caller would draw a hue for a session that has none", none)
		}
	}
}

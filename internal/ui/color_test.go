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

// The three surfaces the owner named the hue for: an agent's turns in the room
// (speakerStyle), the composer it types into (Composer.boxStyle/titleStyle), and
// its roster row (headStyle). The status bar deliberately does NOT take it - it
// recedes as chrome. Each style-picker is asserted on its foreground rather than
// on rendered ANSI, which go test strips - palette_test.go's own approach.

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

// The composer paints the hue into its border and its @name title, so the box
// you type into is the answer to "which agent is this". Uncoloured keeps the
// accent, and a blurred pane drops the hue with the accent - the border's whole
// job is "where do I type", which a blurred box is not the answer to.
func TestTheComposerTakesTheIdentityHueWhenSet(t *testing.T) {
	c := NewComposer().WithColor("blue")
	if got := c.boxStyle().GetBorderTopForeground(); got != identityColors["blue"] {
		t.Errorf("a coloured composer's border is %v, want the blue hue", got)
	}
	if got := c.titleStyle().GetForeground(); got != identityColors["blue"] {
		t.Errorf("a coloured composer's @name title is %v, want the blue hue", got)
	}

	plain := NewComposer()
	if got := plain.boxStyle().GetBorderTopForeground(); got != Accent {
		t.Errorf("an uncoloured composer's border is %v, want the shared Accent", got)
	}
	if got := plain.titleStyle().GetForeground(); got != Accent {
		t.Errorf("an uncoloured composer's title is %v, want the accent header style", got)
	}

	if got := c.Focused(false).boxStyle().GetBorderTopForeground(); got != Border {
		t.Errorf("a blurred coloured composer's border is %v, want the receding pane border", got)
	}
}

// The DM pane wires the hue into its composer, so a /color change moves the box
// the operator types into. Rendered rather than asserted on a picker, because
// the colour is applied at draw time and is not stored on the composer.
func TestTheDMComposerDrawsInTheAgentsHue(t *testing.T) {
	forceColour(t)
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", Name: "alex"}
	d = d.SetSize(fullLegendWidth, 24)

	plain := d.View(fullLegendWidth, 24)
	d.Agent.Color = "blue"
	if coloured := d.View(fullLegendWidth, 24); coloured == plain {
		t.Error("the DM composer did not change when its agent was given a colour: WithColor is not wired into the pane")
	}
}

// The status bar recedes as chrome and does not take the identity hue, unlike
// the three surfaces above. Two agents' bars that differ only by /color must
// render byte-for-byte the same - a hue in the ANSI would be the regression.
func TestTheStatusBarDoesNotTakeTheIdentityHue(t *testing.T) {
	forceColour(t)
	base := Agent{Cwd: t.TempDir(), Model: "claude-opus-5", ContextTokens: 10, ContextWindow: 100}
	plain := statusBar(base, modeAuto, 200)
	coloured := base
	coloured.Color = "violet"
	if got := statusBar(coloured, modeAuto, 200); got != plain {
		t.Errorf("a /color'd agent's status bar differs from an uncoloured one; the bar recedes and must not take the hue\n plain:    %q\n coloured: %q", plain, got)
	}
}

// The roster row keeps the identity hue under the cursor, showing the selection
// as bold rather than the accent that used to hide the colour. An uncoloured row
// still takes the accent, and a blocked agent's warn wins over identity.
func TestRosterHeadStyleKeepsTheIdentityHueUnderTheCursor(t *testing.T) {
	a := Agent{ID: "sess-a", Color: "red", State: rpc.StateIdle}
	if got := (Roster{}).headStyle(a).GetForeground(); got != identityColors["red"] {
		t.Errorf("an idle coloured row is %v, want the red hue", got)
	}

	sel := (Roster{Selected: "sess-a"}).headStyle(a)
	if got := sel.GetForeground(); got != identityColors["red"] {
		t.Errorf("the selected coloured row is %v, want the red hue to stay under the cursor", got)
	}
	if !sel.GetBold() {
		t.Error("the selected coloured row is not bold, so the cursor is invisible on a coloured row")
	}

	plain := Agent{ID: "sess-a", State: rpc.StateIdle}
	if got := (Roster{Selected: "sess-a"}).headStyle(plain).GetForeground(); got != Accent {
		t.Errorf("the selected uncoloured row is %v, want the Accent cursor", got)
	}

	blocked := Agent{ID: "sess-a", Color: "red", State: rpc.StateBlocked}
	if got := (Roster{}).headStyle(blocked).GetForeground(); got == identityColors["red"] {
		t.Error("a blocked coloured row drew its identity hue; the warn colour has to win")
	}

	// Blocked wins even under the cursor: a "needs you" that the selection paints
	// over with the identity hue is one nobody can see. The selection rides along
	// as bold rather than taking the colour.
	selBlocked := (Roster{Selected: "sess-a"}).headStyle(blocked)
	if got := selBlocked.GetForeground(); got == identityColors["red"] {
		t.Error("a selected blocked coloured row drew its identity hue; blocked has to win over the cursor")
	}
	if got := selBlocked.GetForeground(); got != Warn {
		t.Errorf("a selected blocked row is %v, want the warn colour", got)
	}
	if !selBlocked.GetBold() {
		t.Error("a selected blocked row is not bold, so the cursor is lost")
	}
}

// The bar recedes and ignores the identity hue, so barKey omits the colour and a
// /color change redraws nothing here. The inverse of what the bar-was-a-surface
// build asserted: then a colour move had to redraw the bar; now it must not.
func TestTheBarIgnoresColourChanges(t *testing.T) {
	d := NewDM("s1", "alex")
	d.Agent = Agent{ID: "s1", Model: "claude-opus-5", ContextTokens: 10, ContextWindow: 100}
	d = d.SetSize(fullLegendWidth, 24) // caches the bar

	bars := countBars(t)
	d.Agent.Color = "green"
	_ = d.View(fullLegendWidth, 24)
	if *bars != 0 {
		t.Error("the status bar redrew when only the colour changed: the bar recedes and must not carry the hue, " +
			"so colour belongs in no barKey")
	}
}

// @who /color in the room colours that agent, the same as /color @who from
// anywhere: the mention is the target. The room used to send a Wake target-
// command to the agent as a message - claude's own /color - so the hue never
// moved. /name and /task share the grammar and the bug, so all three are here.
func TestAMentionedTargetCommandInTheRoomAimsAtThatAgent(t *testing.T) {
	for _, tc := range []struct {
		name, draft, text string
		kind              string
	}{
		{name: "colour", draft: "@sydney /color green", text: "green", kind: rpc.FrameColor},
		{name: "rename", draft: "@sydney /name sid", text: "sid", kind: rpc.FrameRename},
		{name: "label", draft: "@sydney /task ui-fixes", text: "ui-fixes", kind: rpc.FrameLabel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40).showRoom()

			_, cmd := typeAndSubmit(a, tc.draft)
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)

			if f.Kind != tc.kind {
				t.Fatalf("%q wrote a %q frame, want %q", tc.draft, f.Kind, tc.kind)
			}
			if f.SessionID != "s2" {
				t.Errorf("%q was addressed to %q, want sydney (s2)", tc.draft, f.SessionID)
			}
			if f.Text != tc.text {
				t.Errorf("%q carried %q, want %q", tc.draft, f.Text, tc.text)
			}
		})
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

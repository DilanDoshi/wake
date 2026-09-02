package ui

// A slash command addressed to one agent in the room is a knob, not a message.
//
// pickerroute_test.go covers the *bare* configure form (`@alex /effort` opens a
// picker aimed at alex) and mention_test.go covers a plain message. This file is
// the third case: a slash command carrying an argument (`/model opus`,
// `/effort xhigh`) or one claude owns (`/clear`, `/compact`, a `.claude/commands`
// file), which fall through configure and mentionCommand to the ordinary send.
// Open mode widens a *message* and must not widen a *knob*, so every one of these
// reaches the one agent named with the mention stripped - exactly as it would in
// that agent's DM - whatever the mention mode is.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The report this fixes: `@sydney /model opus` in open mode broadcast
// `@sydney /model opus` to the whole fleet, so every agent read it as a query
// and none changed model. It must reach sydney alone, mention stripped.
func TestARoomSlashCommandWithAnArgumentReachesOneAgentInEitherMode(t *testing.T) {
	for _, mode := range []MentionMode{MentionDirect, MentionOpen} {
		room := func() App {
			a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
			a.mention = mode
			return a
		}
		sydney := idOfAgentNamed(t, room(), "sydney")

		// The floor: in open mode a plain message really does reach everybody, so
		// the assertion below is about the command and not about the fixture.
		if mode == MentionOpen {
			wide, cmd := pressKey(room().withDraft("@sydney ship it"), tea.KeyMsg{Type: tea.KeyEnter})
			if got := len(sentFrames(t, wide, cmd)); got != 3 {
				t.Fatalf("a message reached %d agents in open mode, want the whole fleet: this test "+
					"cannot show a command staying narrow in a mode that widens nothing", got)
			}
		}

		a, cmd := pressKey(room().withDraft("@sydney "+SlashPrefix+modelCommand+" opus"), tea.KeyMsg{Type: tea.KeyEnter})
		frames := sentFrames(t, a, cmd)
		if len(frames) != 1 {
			t.Fatalf("in %s mode @sydney /model opus sent %d frames, want 1: a command configures the "+
				"one agent named, never the fleet", mode, len(frames))
		}
		if frames[0].SessionID != sydney {
			t.Errorf("in %s mode @sydney /model opus reached %q, want sydney (%s)", mode, frames[0].SessionID, sydney)
		}
		if frames[0].Text != SlashPrefix+modelCommand+" opus" {
			t.Errorf("in %s mode @sydney /model opus sent %q, want %q with the mention stripped: a leading "+
				"@name makes claude read /model as prose, so the model never changes", mode, frames[0].Text, SlashPrefix+modelCommand+" opus")
		}
	}
}

// A command claude owns rides the same rule: `@sydney /clear` in open mode
// reaches sydney alone, not the fleet. This is the whole of "all the slash
// commands that work in a DM work in the room" - a passthrough command is one
// the router leaves a message, so without this it is the message open mode
// widens.
func TestARoomPassthroughSlashCommandReachesOneAgentInOpenMode(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	a.mention = MentionOpen
	sydney := idOfAgentNamed(t, a, "sydney")

	a, cmd := pressKey(a.withDraft("@sydney "+SlashPrefix+"clear"), tea.KeyMsg{Type: tea.KeyEnter})
	frames := sentFrames(t, a, cmd)
	if len(frames) != 1 {
		t.Fatalf("@sydney /clear in open mode sent %d frames, want 1: /clear is one session's, and clearing "+
			"the fleet off one keystroke is exactly what widening a knob would do", len(frames))
	}
	if frames[0].SessionID != sydney || frames[0].Text != SlashPrefix+"clear" {
		t.Errorf("@sydney /clear reached %q with %q, want sydney (%s) and %q", frames[0].SessionID, frames[0].Text, sydney, SlashPrefix+"clear")
	}
}

// The composer's promise is what enter sends, over a slash command in open mode.
//
// A slash draft is drawn `→ @sydney · direct` even while the mode is open,
// because it goes to sydney alone - and the number of turns it promises (one)
// is the number of frames it sends. §7's safety argument read onto the one draft
// this fix changed.
func TestOpenModeDrawsASlashCommandAsGoingToOneAgent(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", "john")
	a.mention = MentionOpen

	line := a.withDraft("@sydney "+SlashPrefix+modelCommand+" opus").room.Composer().Target()
	if line != "→ @sydney · direct" {
		t.Errorf("a slash command in open mode drew %q, want → @sydney · direct: it reaches sydney alone, "+
			"so the line that promises where ↵ goes must say so", line)
	}
}

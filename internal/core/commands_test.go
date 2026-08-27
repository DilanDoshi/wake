package core

import "testing"

// An init frame's advertised slash commands cross the airlock, read off a real
// recording.
//
// The fixture rather than a hand-built frame, for the MCP roster's reason: the
// shape is Claude's, and a frame this file wrote would only prove this file
// agrees with itself. The corpus carries an operator's own `.claude/commands`
// files in the same list, which is what makes it worth reading at all.
func TestAnInitFramesSlashCommandsCrossTheAirlock(t *testing.T) {
	line := firstInitLine(t, "../../testdata/stream/basic-turn.jsonl", "slash_commands")

	ev := onlyEvent(t, line, 0)
	if ev.Session == nil {
		t.Fatal("a recorded init frame produced no session facts at all")
	}
	if len(ev.Session.SlashCommands) == 0 {
		t.Fatal("the init frame advertises slash commands and none of them crossed the airlock: a " +
			"completion menu has nothing to offer, and the absence looks exactly like a session with none")
	}
	for _, name := range ev.Session.SlashCommands {
		if name == "" {
			t.Error("a command crossed with no name, which is a row a menu would draw blank")
		}
	}
}

// A session advertising nothing reports nothing, and that is not an error.
//
// nil rather than an empty slice, for the MCP roster's reason: "advertises none"
// and "no init has arrived" must not be two values that render the same.
func TestASessionAdvertisingNoCommandsCarriesNone(t *testing.T) {
	ev := onlyEvent0(t, `{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-6"}`)
	if ev.Session == nil {
		t.Fatal("an init frame with a model produced no facts")
	}
	if ev.Session.SlashCommands != nil {
		t.Errorf("a session advertising no commands carries %q, want nil", ev.Session.SlashCommands)
	}
}

// The list rides on init and on nothing else.
//
// A result frame names no commands, so a consumer folding one must not blank
// what an init established - the same trap Model and the context window are
// guarded against, one field over.
func TestOnlyAnInitFrameCarriesTheAdvertisedCommands(t *testing.T) {
	ev := onlyEvent0(t, `{"type":"result","subtype":"success","session_id":"s1","result":"done",`+
		`"usage":{"input_tokens":10},"modelUsage":{"claude-opus-4-6":{"contextWindow":200000}}}`)
	if ev.Session == nil {
		t.Fatal("a result frame carrying usage produced no facts")
	}
	if ev.Session.SlashCommands != nil {
		t.Errorf("a result frame carries %q as an advertised command set: only init advertises one, "+
			"and a consumer folding this would empty the menu once per turn", ev.Session.SlashCommands)
	}
}

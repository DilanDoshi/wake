package ui

// What each command Wake owns does, one line apiece, for /wake-help.
//
// Keyed by the same word constants slash.go's maps are, and held to them in both
// directions by TestEveryCommandWakeOwnsHasExactlyOneHelpLine: a command with no
// line here, or a line for a command that is gone, is a build failure. Which
// commands exist, who they take and where they work is read off those maps, not
// written here - this file holds only the argument sketch and the sentence.

import (
	"strings"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// helpRoomOnly and helpBareOnly tag the two kinds of command that work in less
	// than every form: roomCommands is the room's alone, bareOnlyCommands is Wake's
	// only without an argument and reaches the agent with one.
	helpRoomOnly = "room only"
	helpBareOnly = "bare only; with an argument it reaches the agent"
)

// commandHelpLine is one command's argument sketch and what it does.
type commandHelpLine struct{ args, what string }

var commandHelp = map[string]commandHelpLine{
	resumeCommand:          {"[<name>|" + resumeAll + "]", "bring a parked session back; bare opens a picker"},
	newCommand:             {"[<name>] [" + dirKeyword + " <dir>] [flags]", "start an agent without leaving the room; flags: " + strings.Join(newFlagNames(), " ")},
	nameCommand:            {"<new-name>", "rename a session"},
	taskCommand:            {"<what it is on>", "set the label beside a session's name"},
	colorCommand:           {"<colour>", "an identity hue (" + strings.Join(rpc.ColorNames, " ") + "), or " + rpc.ColorNone + " to clear"},
	teamCommand:            {"<name>", "group a session under a team - " + agentPrefix + "<name> then reaches them all - or " + rpc.TeamNone + " to clear"},
	quitCommand:            {"", "end one session for good and drop its row (irreversible)"},
	adoptCommand:           {"[<id> ...]", "adopt a claude session this machine already has; bare lists them in the room"},
	mcpCommand:             {"", "an agent's MCP servers: status, sign in, reconnect, turn on or off, tools"},
	managerCommand:         {"", "the switch: start the manager, wake a parked one, or park a running one"},
	managerStopCommand:     {"", "end the manager for good; a fresh " + managerVerb + " starts a new one"},
	boardCommand:           {"", "the fleet as one row per agent; ⇥ tiles it into a live wall"},
	loginCommand:           {"", "the auth panel: sign in, or check whether this machine is"},
	groupchatFilterCommand: {"[on|off]", "whether a lone " + agentPrefix + "name narrows the room by default; bare reports"},
	reauthCommand:          {"", "recover sessions an expired shared login knocked out"},
	workflowsCommand:       {"", "a Workflow run's phases and agents: filter, stop or save one"},
	statusCommand:          {"", "an agent's own facts: state, ids, model, context, effort, team, builds; read-only"},
	wakeNameCommand:        {"[<name>]", "rename this fleet; bare says its name"},
	wakeHelpCommand:        {"[<filter>]", "every key and command Wake has, narrowed as you type"},
	friendsCommand:         {"[" + strings.Join(friendOptions(), "|") + "]", "the friend on the usage bar; bare opens a picker that previews in the corner"},
	clearCommand:           {"", "empty the group chat in this window; no agent is touched"},
	effortCommand:          {"", "pick a reasoning level"},
	modelCommand:           {"", "pick a model"},
}

// newFlagNames are the spawn flags /new takes, read off the table its parser
// reads, so a flag added there reaches the help.
func newFlagNames() []string {
	names := make([]string, len(newFlagTable))
	for i, f := range newFlagTable {
		names[i] = f.name
	}
	return names
}

// commandRows is every command as a help row: the verb and its argument sketch,
// with [@who] in front for the commands a mention can aim, and the sentence with
// the tag for a command that does not work in every form.
func commandRows() []helpRow {
	rows := make([]helpRow, 0, len(commandHelp))
	for _, word := range wakeWords() {
		line := commandHelp[word]
		lead, what := configureVerb(word), line.what
		args := line.args
		if _, aimed := roomTargetCommands[word]; aimed {
			args = strings.TrimSpace("[" + agentPrefix + "who] " + args)
		}
		if args != "" {
			lead += " " + args
		}
		if _, only := roomCommands[word]; only {
			what += " (" + helpRoomOnly + ")"
		}
		if _, bare := bareOnlyCommands[word]; bare {
			what += " (" + helpBareOnly + ")"
		}
		rows = append(rows, helpRow{lead: lead, text: what})
	}
	return rows
}

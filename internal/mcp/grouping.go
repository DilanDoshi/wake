package mcp

// The grouping tools: set_team and set_color, split from tools.go for the
// file-size hard max. They let a manager group the fleet the way an operator
// does at a keystroke - assign a team, set an identity hue.
//
// # Why these two act and rename/label do not
//
// A team and a colour are the operator's grouping of the fleet - and the
// manager's now too, on the owner's 2026-09-21 override of "send, interrupt and
// spawn, and nothing else". A manager that coordinates the fleet groups it: it
// is who "put test-x and test-y in a testing team" is addressed to. The two are
// undoable by looking - the change is a roster section and a name-tag hue the
// operator sees and can retype - and neither carries an injection vector: a team
// is fenced to a mention token and a colour to seven words, both by the daemon's
// own rpc.NormalizeTeam / rpc.NormalizeColor. Rename and label stay refused:
// rename moves where the operator's own @name routing lands, and a label is the
// column the operator scans, so a model authoring one writes the chrome the
// operator reads as their own. The full argument is in cmd/wake/mcpguard_test.go.
//
// # Why the value is passed raw
//
// spawn_agent's rule one field over: the daemon owns the fence. rpc.NormalizeTeam
// folds and refuses the shape, teamSession refuses a reserved routing word, and
// rpc.NormalizeColor refuses a hue outside its set - all on the far side of the
// socket, where a parked session is refused too. A tool that re-checked here
// would be a second copy of a rule that already runs where the value is stored,
// and act's read-back surfaces the daemon's refusal to the manager rather than
// reporting a grouping that did not happen.

import (
	"context"
	"fmt"
	"strings"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// colorArg is what set_color sets. A colour name rather than an id, beside
// teamArg (in sendteam.go), which the two grouping tools name their value with.
const colorArg = "color"

// setTeam groups one agent under a team, creating the team if no agent wears the
// tag yet. The manager still addresses the agent by id; the team is a name.
func setTeam() Tool {
	schema := agentIDSchema()
	schema["properties"].(map[string]any)[teamArg] = map[string]any{
		"type": "string",
		"description": "The team to group the agent under. A name no agent wears yet makes the team; an existing one adds to it. " +
			"Use \"none\" to remove the agent from its team. Letters, digits, dash and underscore.",
	}
	schema["required"] = []string{agentIDArg, teamArg}

	return Tool{
		Name: "set_team",
		Description: "Group one agent under a team, addressed by the id from list_agents. A team is a grouping of the fleet: " +
			"it heads a roster section and @team reaches its members (send_to_team). A name no agent wears yet makes the team; " +
			"\"none\" removes the agent from its team. The operator sees the change in the roster.",
		Schema: schema,
		Call: func(ctx context.Context, f Fleet, args map[string]any) (string, error) {
			id, err := agentID(args)
			if err != nil {
				return "", err
			}
			team, ok := args[teamArg].(string)
			if !ok || strings.TrimSpace(team) == "" {
				return "", fmt.Errorf("%s is required: name the team to group this agent under, or %q to remove it from one", teamArg, rpc.TeamNone)
			}
			who, err := requireLive(ctx, f, id)
			if err != nil {
				return "", err
			}
			if err := f.SetTeam(ctx, id, team); err != nil {
				return "", err
			}
			if folded := strings.ToLower(strings.TrimSpace(team)); folded == rpc.TeamNone {
				return "Removed " + who + " from its team.", nil
			}
			return "Grouped " + who + " under team " + oneLine(strings.ToLower(strings.TrimSpace(team)), agentLineMax) + ".", nil
		},
	}
}

// setColor sets one agent's identity hue: the colour its name-tag, status bar and
// roster row are drawn in. One of rpc.ColorNames, or "none" to clear.
func setColor() Tool {
	schema := agentIDSchema()
	schema["properties"].(map[string]any)[colorArg] = map[string]any{
		"type": "string",
		"description": "The colour to draw the agent in: one of " + strings.Join(rpc.ColorNames, ", ") + ". Use \"none\" to clear it.",
	}
	schema["required"] = []string{agentIDArg, colorArg}

	return Tool{
		Name: "set_color",
		Description: "Set one agent's identity colour, addressed by the id from list_agents - the hue its name-tag and roster row are drawn in, so the operator can tell agents apart by more than name. One of " +
			strings.Join(rpc.ColorNames, ", ") + ", or \"none\" to clear. The operator sees the change in the roster and the room.",
		Schema: schema,
		Call: func(ctx context.Context, f Fleet, args map[string]any) (string, error) {
			id, err := agentID(args)
			if err != nil {
				return "", err
			}
			color, ok := args[colorArg].(string)
			if !ok || strings.TrimSpace(color) == "" {
				return "", fmt.Errorf("%s is required: name one of %s, or %q to clear", colorArg, strings.Join(rpc.ColorNames, " "), rpc.ColorNone)
			}
			who, err := requireLive(ctx, f, id)
			if err != nil {
				return "", err
			}
			if err := f.SetColor(ctx, id, color); err != nil {
				return "", err
			}
			if folded := strings.ToLower(strings.TrimSpace(color)); folded == rpc.ColorNone {
				return "Cleared " + who + "'s colour.", nil
			}
			return "Set " + who + " to " + oneLine(strings.ToLower(strings.TrimSpace(color)), agentLineMax) + ".", nil
		},
	}
}

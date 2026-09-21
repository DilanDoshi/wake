package mcp

// The team fan-out send: send_to_team, split from tools.go for the file-size
// hard max. It is send_to_agent for a group, and the whole of what it adds is
// resolving one team name to its live members before the same per-member send.

import (
	"context"
	"fmt"
	"strings"
)

// teamArg is what send_to_team addresses. A name rather than an id, unlike
// send_to_agent: a team is the operator's grouping and list_agents shows the
// name, and the daemon resolves it to member ids on this side of the socket.
const teamArg = "team"

// sendToTeam starts a turn on every live member of a team.
//
// It is send_to_agent for a group - N sends, each of which the manager could
// already make one at a time, and each of which the operator sees in the room -
// so it stays inside the "undoable by looking" envelope Tools() draws the send
// verbs to. It writes rpc.FrameSend per member, the frame already allowed the
// manager (mcpguard_test.go), so it adds no new reach onto the wire, only a
// convenience over doing it by hand. A team with no live member sends nothing.
func sendToTeam() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			teamArg: map[string]any{
				"type":        "string",
				"description": "The team name shown in list_agents. The message goes to every live member of it.",
			},
			messageArg: map[string]any{
				"type":        "string",
				"description": "What to say to each member. It is delivered exactly as written.",
			},
		},
		"required": []string{teamArg, messageArg},
	}
	return Tool{
		Name:        "send_to_team",
		Description: "Send a message to every live member of a team, which starts a turn on each. Address the team by the name from list_agents. This costs what N turns cost and the operator sees each one in the room, so it is not a way to check on a team - agent_status is. A team with no live member sends nothing.",
		Schema:      schema,
		Call: func(ctx context.Context, f Fleet, args map[string]any) (string, error) {
			team, _ := args[teamArg].(string)
			team = strings.ToLower(strings.TrimSpace(team))
			if team == "" {
				return "", fmt.Errorf("%s is required: name the team from list_agents", teamArg)
			}
			text, ok := args[messageArg].(string)
			if !ok || strings.TrimSpace(text) == "" {
				return "", fmt.Errorf("%s is required and must not be blank: agents handed nothing still spend a turn answering about nothing", messageArg)
			}
			st, err := f.List(ctx)
			if err != nil {
				return "", err
			}
			var ids []string
			for _, s := range liveSessions(st) {
				if s.Team == team {
					ids = append(ids, s.ID)
				}
			}
			if len(ids) == 0 {
				return "No live agent is in team " + oneLine(team, agentLineMax) + ", so nothing was sent.", nil
			}
			for _, id := range ids {
				if err := f.Send(ctx, id, text); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("Sent to %d in team %s. Each is a turn now; agent_status says when one has finished.", len(ids), oneLine(team, agentLineMax)), nil
		},
	}
}

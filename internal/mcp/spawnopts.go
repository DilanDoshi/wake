package mcp

// What spawn_agent's optional arguments choose for a new agent, read off a
// model's call. This is the manager's counterpart of cmd/wake's spawnflags.go:
// it refuses the malformed before a spawn, with a sentence a model can act on,
// and the daemon's configRefusal stays the last word on every value.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

// effortLegal is what an effort refusal says the levels are, derived from the
// one list --effort takes so a level added there reaches this sentence too.
var effortLegal = "one of " + strings.Join(core.EffortLevels, ", ")

// modelExamples are the aliases a model is shown, without core.ModelDefault:
// that word means no flag at all, so it is not a model to choose.
var modelExamples = strings.Join(slices.DeleteFunc(slices.Clone(core.ModelAliases),
	func(m string) bool { return m == core.ModelDefault }), ", ")

// spawnSchema is spawn_agent's arguments as a model is shown them, beside the
// reader below so the two cannot drift.
func spawnSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			dirArg: map[string]any{
				"type":        "string",
				"description": "Where the agent runs. Exactly a directory from list_agents.",
			},
			nameArg: map[string]any{
				"type":        "string",
				"description": "Optional display name for the new agent, e.g. \"x\": letters, digits, - and _. Omit it to have one assigned. A name a live agent already has, or one that reads as the operator or the system (operator, system, admin, ...), is refused - address the agent by the id this returns, never by name.",
			},
			effortArg: map[string]any{
				"type":        "string",
				"enum":        slices.Clone(core.EffortLevels),
				"description": "Optional reasoning effort, weakest first. Set it only when the operator asked for one; omit it and the agent thinks at the operator's own default.",
			},
			modelArg: map[string]any{
				"type":        "string",
				"description": "Optional model, e.g. " + modelExamples + ", or a full model ID. Set it only when the operator asked for one; omit it and the agent runs on the operator's own default model.",
			},
		},
		"required": []string{dirArg},
	}
}

// spawnOpts reads the name, effort and model a call chose.
//
// Absent is "" for each, "Wake chose nothing". A present value that is not a
// string is refused rather than coerced to "": a model that asked for something
// and silently got the default believes in an agent that does not exist.
// "default" as a model is read as absent, which is what the word means.
func spawnOpts(args map[string]any) (SpawnOpts, error) {
	name, err := optionalString(args, nameArg, `like "x", or left out to have one assigned`)
	if err != nil {
		return SpawnOpts{}, err
	}
	effort, err := optionalString(args, effortArg, effortLegal+", or left out for the operator's default")
	if err != nil {
		return SpawnOpts{}, err
	}
	if effort != "" && !core.ValidEffort(effort) {
		return SpawnOpts{}, fmt.Errorf("%s %q is not a level --effort takes: use %s, or leave it out for the operator's default", effortArg, effort, effortLegal)
	}
	model, err := optionalString(args, modelArg, `like "opus", or left out for the operator's default`)
	if err != nil {
		return SpawnOpts{}, err
	}
	if model == core.ModelDefault {
		model = ""
	}
	return SpawnOpts{Name: name, Effort: effort, Model: model}, nil
}

// optionalString reads one optional string argument; legal finishes the
// refusal of a present non-string.
func optionalString(args map[string]any, key, legal string) (string, error) {
	raw, present := args[key]
	if !present {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string %s", key, legal)
	}
	return s, nil
}

// chosen is what a spawn's answer adds for an effort or a model the call chose,
// or "" when it chose neither, so that answer stays the one it always was.
// agent_status never reports a model, so this is the manager's record of it.
func (o SpawnOpts) chosen() string {
	var s string
	if o.Effort != "" {
		s += " at effort " + o.Effort
	}
	if o.Model != "" {
		s += " on model " + oneLine(o.Model, toolArgMax)
	}
	return s
}

package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The report carries the model now, so a client that never witnessed an agent's
// init - a late attach, a board tile - still names it. See rpc.SessionStatus.Model.
func TestWithStatusFoldsTheModelFromTheReport(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateWorking, Model: "claude-opus-4-8"},
	}})
	if got, _ := f.Agent("s1"); got.Model != "claude-opus-4-8" {
		t.Errorf("Agent.Model = %q, want claude-opus-4-8", got.Model)
	}
}

// The event stream set the model; a later report assembled before the daemon saw
// an init carries an empty Model and must not erase it - the guard every fact on
// this fold keeps (Model, the context window, the MCP count).
func TestAnEmptyReportModelDoesNotBlankAKnownOne(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Model: "claude-opus-4-8"}}, "s1")
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", State: rpc.StateWorking}}})
	if got, _ := f.Agent("s1"); got.Model != "claude-opus-4-8" {
		t.Errorf("Agent.Model = %q, want the event-stream value kept", got.Model)
	}
}

package main

// Bare `wake` held from the side an example cannot reach: which model it runs.
// Split off openroom_test.go by subject, the way forkguard_test.go is split off
// fork_test.go.

import (
	"go/ast"
	"testing"
)

// Bare `wake` runs the room's model, and that wire is the half no test in this
// package can watch.
//
// # Why this is not a "calls the right function" check for its own sake
//
// Because the wrong one **compiles and very nearly works**. WithOpenDM("")
// returns the model untouched, so `converse(socket, rpc.SessionStatus{}, …)`
// here would open the same room with the same seed and differ in one invisible
// thing: its dialer would be redial on an empty session id, which asks
// liveSession about an id nothing holds and refuses every reattach. The symptom
// is a hang-up that never comes back - after a window drag, on the one path
// that has no session to reattach to - and it is not reachable from any test
// that does not own a terminal.
func TestTheRoomBareWakeOpensIsTheOneWithTheRoomsWayBack(t *testing.T) {
	// `attach` still compiles here and differs in a model rather than in an
	// error: it spawns an agent and opens its conversation beside the room.
	front := funcDecl(t, "openroom.go", "openRoom")
	if callsFunctionNamed(front, "attach") {
		t.Error("openRoom calls attach, so bare `wake` spawns an agent and opens its conversation. " +
			"Bare `wake` is a request about the fleet, and the manager is the only session it starts")
	}
	if !callsFunctionNamed(front, "converseRoom") {
		t.Error("openRoom does not run the room's model. `converse` builds one with a DM " +
			"and a session dialer, and bare `wake` has neither a target nor an id to reattach to")
	}

	room := funcDecl(t, "attach.go", "converseRoom")
	if got := argumentTo(t, room, "converseModel", 1); !callsFunction(got, "conversationRoom") {
		t.Errorf("converseRoom runs %s. The model is where the two flows differ and the exit line is "+
			"where they must not: converseModel exists so ⌃Q's sentence is written once",
			exprText(got))
	}
}

// callsFunctionNamed reports whether a function calls another by that name.
func callsFunctionNamed(fn *ast.FuncDecl, name string) bool {
	called := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && exprText(call.Fun) == name {
			called = true
		}
		return !called
	})
	return called
}

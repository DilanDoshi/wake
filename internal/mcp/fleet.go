// Package mcp is the MCP server Wake exposes to the manager session.
//
// # Why this is not internal/core/mcp.go
//
// The spec's package list puts it there and this is a deliberate departure.
// internal/core is the airlock plus session supervision and it sits *below*
// the daemon: a query surface over daemon state placed there would either
// invert that dependency or put a JSON-RPC server in the four-file package
// whose whole review value is being small and about Claude's JSON.
//
// # Why it is written against an interface
//
// So this package never imports internal/daemon. The server is a client of the
// fleet, not a part of it, and the seam is what lets every tool be tested
// against a fake with no socket, no daemon and no claude process - which is
// the same rule the rest of this tree follows.
//
// # Why there is no clock in here
//
// "Wake must be cheap to leave open" is a non-negotiable, and for a query
// surface it has one exact meaning: an answer is produced when a model asks
// and at no other time. Nothing here caches a fleet snapshot on a ticker,
// polls for a change, or sleeps between retries - a `wake mcp` process sits
// beside thirty claude processes and is idle for almost all of its life.
// TestNothingInThisPackageKeepsTime derives that from the source rather than
// trusting this paragraph.
package mcp

import (
	"context"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// Fleet is everything the tools need from the daemon.
//
// Ids in and ids out, never names. internal/daemon/names.go rules that nothing
// is addressed by name on the wire and a test enforces it, because the reaper
// proves a process group by finding a session UUID in its argv - so a name
// arriving from a *model* is the last thing that may become an address. A tool
// takes an id the model got from list_agents; that is one extra tool call and
// the property survives.
//
// All three methods are declared here although the reading half calls only
// List. One seam or none: a second interface added later for the acting tools
// would mean two fakes, two sets of tests and two places for the id rule to be
// enforced differently.
type Fleet interface {
	// List is the whole fleet report, fetched when a model asks and never
	// held. rpc.SessionStatus carries Dir, Tool and ToolArg precisely so a
	// freshly started `wake mcp` can answer list_agents with no history of
	// its own.
	List(ctx context.Context) (rpc.Status, error)

	// Send starts a turn on one agent, addressed by id.
	Send(ctx context.Context, id, text string) error

	// Interrupt stops the turn an agent is running without ending the
	// session - the esc that shipped, which is what "pause" means.
	Interrupt(ctx context.Context, id string) error

	// Spawn starts one agent in a directory and returns the id it was given.
	// The id is minted by the caller, so it is knowable before the daemon has
	// confirmed anything - which is what lets the tool answer with something
	// the manager can address.
	Spawn(ctx context.Context, dir string) (string, error)
}

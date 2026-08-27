package main

// That every flag the parser accepts survives the last hop to the socket.
//
// The chain from an argv word to a running session is long - parse, validate,
// spawnOpts, rpc.Frame, daemon, core.Config, argv - and the hop tested here is
// the one with no natural failure. Every other link either refuses a value or
// carries it; this one copies field by field, so a flag added to the table and
// not to the frame is parsed, validated, reported legal in the usage text, and
// then silently dropped. Nothing downstream can tell that apart from an
// operator who did not ask for it.

import (
	"net"
	"reflect"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// TestEverySpawnOptReachesTheSpawnFrame is derived from spawnOpts' own fields
// rather than from a list, which is what makes it cover the sixth flag on the
// day it is added.
//
// It works by giving every field a value that could not appear by accident and
// then looking for that value on the frame requestSpawn writes. Values rather
// than field names, because the two structs do not agree on naming - Worktree
// against Worktree, but MaxBudgetUSD against MaxBudgetUSD and Effort against
// Effort - and a name-matching test would be asserting a convention rather than
// the transfer.
//
// It marks a `[]string` as well as a string, because --add-dir is repeatable.
// A field of any *third* kind is still a hard failure rather than a skip: the
// whole property is that no field goes unmarked, and a kind this does not know
// how to mark is one it would silently pass over.
func TestEverySpawnOptReachesTheSpawnFrame(t *testing.T) {
	var opts spawnOpts
	v := reflect.ValueOf(&opts).Elem()
	marks := make(map[string]string, v.NumField())
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		mark := "mark-" + name
		switch f := v.Field(i); {
		case f.Kind() == reflect.String:
			f.SetString(mark)
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String:
			f.Set(reflect.ValueOf([]string{mark}))
		default:
			t.Fatalf("spawnOpts.%s is a %s: this test marks strings and string slices, so a field of another kind is one it silently skips", name, f.Kind())
		}
		marks[name] = mark
	}
	if len(marks) == 0 {
		t.Fatal("spawnOpts has no fields, so this test asserted nothing")
	}

	frame := frameFromSpawn(t, opts)

	// Every mark has to be findable somewhere on the frame. Which field it
	// landed in is not this test's business - rpc.Frame's own comments own that
	// - but a mark that is nowhere is a flag that reached no socket.
	present := markedStrings(reflect.ValueOf(frame))
	for name, mark := range marks {
		if !present[mark] {
			t.Errorf("spawnOpts.%s does not reach the spawn frame: the flag is parsed, validated and named in the usage text, and then dropped before the socket", name)
		}
	}
}

// markedStrings is every string a struct carries, one level into its string
// slices - which is exactly as deep as a mark can be hidden.
func markedStrings(v reflect.Value) map[string]bool {
	out := map[string]bool{}
	for i := range v.NumField() {
		switch f := v.Field(i); {
		case f.Kind() == reflect.String:
			out[f.String()] = true
		case f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String:
			for j := range f.Len() {
				out[f.Index(j).String()] = true
			}
		}
	}
	return out
}

// frameFromSpawn is the frame requestSpawn writes for one spawnOpts, read back
// off a real socket pair.
//
// A real pipe rather than a seam in requestSpawn, because the seam is what is
// under test: a function that returned a frame for a test to inspect and wrote a
// different one to the wire would pass.
func frameFromSpawn(t *testing.T, opts spawnOpts) rpc.Frame {
	t.Helper()
	mine, theirs := net.Pipe()
	defer func() { _ = mine.Close() }()
	defer func() { _ = theirs.Close() }()

	done := make(chan error, 1)
	go func() { done <- requestSpawn(mine, "session-1", "alex", "", opts) }()

	frames, errs := rpc.ReadFrames(theirs)
	frame, ok := <-frames
	if !ok {
		t.Fatalf("no spawn frame was written: %v", <-errs)
	}
	if err := <-done; err != nil {
		t.Fatalf("requestSpawn: %v", err)
	}
	return frame
}

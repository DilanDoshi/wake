// The four ending verbs and the status report, as they appear on the wire.
//
// The verbs are separate kinds for the same reason FrameAllow and FrameDeny
// are: an unrecognized kind does nothing, while one kind with a mode field
// needs a default, and every default here is destructive. A stop that arrived
// as a kill takes an agent down mid-Edit.

package rpc

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// frameKinds is every frame kind this package declares, by constant name.
//
// Hand-written, and TestNoFrameKindIsMissingFromTheDistinctnessMap is what
// keeps it honest: the distinctness check below cannot fail for a kind that is
// not listed here, so without that guard adding a constant whose value
// collided with an existing one would be green. Two tests over one map rather
// than a map per test, so a kind cannot be in one and not the other.
var frameKinds = map[string]string{
	"FrameEvent":            FrameEvent,
	"FrameHistory":          FrameHistory,
	"FrameHistoryReply":     FrameHistoryReply,
	"FrameRoomHistory":      FrameRoomHistory,
	"FrameRoomHistoryReply": FrameRoomHistoryReply,
	"FrameSend":             FrameSend,
	"FrameSpawn":            FrameSpawn,
	"FrameFork":             FrameFork,
	"FrameImport":           FrameImport,
	"FrameHello":            FrameHello,
	"FrameError":            FrameError,
	"FrameAllow":            FrameAllow,
	"FrameAnswer":           FrameAnswer,
	"FrameDeny":             FrameDeny,
	"FrameInterrupt":        FrameInterrupt,
	"FrameStop":             FrameStop,
	"FramePark":             FramePark,
	"FrameWake":             FrameWake,
	"FrameKill":             FrameKill,
	"FrameQuit":             FrameQuit,
	"FrameParkAll":          FrameParkAll,
	"FrameStatus":           FrameStatus,
	"FrameStatusReply":      FrameStatusReply,
	"FrameStatusPush":       FrameStatusPush,
	"FrameRename":           FrameRename,
	"FrameLabel":            FrameLabel,
	"FrameMode":             FrameMode,
}

// The verbs must not collide with each other or with the existing kinds. A
// duplicated constant would silently route one verb to another's handler -
// and the pairs that would hurt are stop/kill, quit/stop, and now
// interrupt/stop, which differ by whether an agent survives.
func TestEveryFrameKindIsDistinct(t *testing.T) {
	kinds := frameKinds
	seen := make(map[string]string, len(kinds))
	for name, kind := range kinds {
		if kind == "" {
			t.Errorf("%s is the empty string, which is what an absent kind decodes to", name)
		}
		if other, dup := seen[kind]; dup {
			t.Errorf("%s and %s are both %q", name, other, kind)
		}
		seen[kind] = name
	}
}

// The distinctness check above reads a hand-written map, so it cannot fail for
// a kind nobody added to it - which makes "add a constant, forget the map" a
// silent way to reintroduce exactly the collision it exists to catch. This
// reads the declarations instead.
//
// It is the guard that stops that test from being one more of the shape this
// project keeps finding: a check whose subject can walk out from under it.
func TestNoFrameKindIsMissingFromTheDistinctnessMap(t *testing.T) {
	declared := frameKindConstants(t, "wire.go", "lifecycle.go")
	if len(declared) < len(frameKinds) {
		t.Fatalf("found %d Frame* constants across the package, but the map holds %d: the scan is broken and this test is asserting nothing", len(declared), len(frameKinds))
	}
	for name, value := range declared {
		got, ok := frameKinds[name]
		if !ok {
			t.Errorf("%s = %q is declared but is in no distinctness map: a kind nobody listed cannot collide with anything, which is how a duplicate ships", name, value)
			continue
		}
		if got != value {
			t.Errorf("the map has %s = %q, the declaration says %q", name, got, value)
		}
	}
}

// frameKindConstants reads every `Frame… = "…"` constant declared in the named
// files of this package.
func frameKindConstants(t *testing.T, files ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				name, value, ok := stringConst(spec)
				if ok && strings.HasPrefix(name, "Frame") {
					out[name] = value
				}
			}
		}
	}
	return out
}

// stringConst pulls one `Name = "value"` out of a const spec.
func stringConst(spec ast.Spec) (string, string, bool) {
	vs, ok := spec.(*ast.ValueSpec)
	if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
		return "", "", false
	}
	lit, ok := vs.Values[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", "", false
	}
	return vs.Names[0].Name, value, true
}

// A status reply carries the whole fleet, and every field has to survive the
// round trip: `wake status` exists so a background process the user cannot
// see is still accountable, and a field that silently drops makes it lie.
//
// The first row is filled from the declaration rather than written out here.
// A hand-written row pins the fields whoever wrote it remembered and enforces
// nothing about the next one added - the shape docs/notes/decisions.md names
// first, and this test was an instance of it: three fields (Dir, Tool,
// ToolArg) joined SessionStatus and it went on passing without them. The
// failure it now covers is not hypothetical either, and it is silent in both
// directions: two fields sharing a json name make encoding/json drop *both*,
// with no error anywhere.
func TestStatusReplyRoundTripsThroughTheWire(t *testing.T) {
	filled := filledSessionStatus(t)
	want := Frame{
		Kind: FrameStatusReply,
		Status: &Status{
			Running: true,
			PID:     4242,
			Socket:  "/tmp/wake/daemon.sock",
			Sessions: []SessionStatus{
				filled,
				{ID: "s2", Name: "alex", State: StateBlocked, RequestIDs: []string{"req-7"}},
				{ID: "s3", State: StateSilent, QuietMS: 600_000},
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, want); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got := readOne(t, &buf)

	// Field by field before the whole-frame comparison, so a failure names the
	// field that dropped instead of printing two reports to diff by eye.
	if got.Status == nil || len(got.Status.Sessions) == 0 {
		t.Fatalf("no sessions came back: %+v", got)
	}
	back := reflect.ValueOf(got.Status.Sessions[0])
	sent := reflect.ValueOf(filled)
	for i := range sent.NumField() {
		f := sent.Type().Field(i)
		if !reflect.DeepEqual(back.Field(i).Interface(), sent.Field(i).Interface()) {
			t.Errorf("SessionStatus.%s (json tag %q) did not survive the wire: sent %v, read back %v",
				f.Name, f.Tag.Get("json"), sent.Field(i).Interface(), back.Field(i).Interface())
		}
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status did not survive the wire:\n got %+v\nwant %+v", got.Status, want.Status)
	}
}

// filledSessionStatus gives every field SessionStatus declares a distinct
// non-zero value, so the round trip above covers the struct as it is rather
// than as somebody once listed it.
//
// A kind it cannot fill is a build failure rather than a skip. Skipping is how
// the check would decay back into a hand-written list: a field of some new type
// would cross the wire with nothing looking at it, and the test would still be
// green and still be named after covering everything.
func filledSessionStatus(t *testing.T) SessionStatus {
	t.Helper()

	var st SessionStatus
	v := reflect.ValueOf(&st).Elem()

	// Scan integrity, sitting just under the twelve fields the struct carries
	// today. Without it a struct gutted to nothing would round-trip perfectly
	// and this would report success over an empty loop.
	const minFields = 9
	if v.NumField() < minFields {
		t.Fatalf("SessionStatus declares %d fields, want at least %d - the scan is broken or the report has shrunk, and either way this test is asserting nothing", v.NumField(), minFields)
	}

	for i := range v.NumField() {
		f := v.Type().Field(i)
		switch v.Field(i).Kind() {
		case reflect.String:
			// Distinct per field, so a pair that swapped places is a failure
			// rather than two equal strings agreeing with each other.
			v.Field(i).SetString("value of " + f.Name)
		case reflect.Int, reflect.Int64:
			v.Field(i).SetInt(int64(i) + 1)
		case reflect.Slice:
			// []string fields today (RequestIDs, Commands). Distinct per field for
			// the same reason the string case is, so a mis-wired field is a failure.
			if v.Field(i).Type().Elem().Kind() != reflect.String {
				t.Fatalf("SessionStatus.%s is a slice of %s and this filler only knows []string: teach it that element kind", f.Name, v.Field(i).Type().Elem().Kind())
			}
			v.Field(i).Set(reflect.ValueOf([]string{"value of " + f.Name}))
		default:
			t.Fatalf("SessionStatus.%s is a %s and this filler cannot populate it: teach it that kind, because a field it leaves zero crosses the wire with nothing checking it", f.Name, v.Field(i).Kind())
		}
	}
	return st
}

// An ordinary event frame must not grow a status object. At 15-30 sessions
// every event carries the envelope, and a nil payload has to stay absent
// rather than serialize as null on every frame.
func TestStatusIsAbsentFromAnEventFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameEvent, SessionID: "s1"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if got := buf.String(); strings.Contains(got, "status") {
		t.Errorf("event frame carries a status key: %s", got)
	}
}

// readOne decodes exactly one frame and insists the stream held only that.
func readOne(t *testing.T, r *bytes.Buffer) Frame {
	t.Helper()

	frames, errs := ReadFrames(r)
	f, open := <-frames
	if !open {
		t.Fatal("no frame decoded")
	}
	if extra, open := <-frames; open {
		t.Fatalf("a second frame appeared: %+v", extra)
	}
	if err := <-errs; err != nil {
		t.Fatalf("ReadFrames: %v", err)
	}
	return f
}

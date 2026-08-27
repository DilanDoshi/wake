// The agent whose permission answer has a consequence.
//
// # Why this is a second file of fakes
//
// main_test.go holds the fakes that prove the daemon's *lifecycle*: an agent
// that idles, one that wedges, one that floods, one that never reads. Its
// header says it is the one place in this package allowed to name Claude's
// JSON, and this file is the second - the set is those two, split by subject at
// the point main_test.go left the project's size band rather than after it had
// passed the hard max. Nothing else in internal/daemon may name the wire.
//
// The subject here is the one thing those fakes cannot express. fakeAsk proves
// an answer *arrives*: it greps the line for a behavior and echoes which one it
// saw. That is not the same as the tool running, and the difference is a defect
// this project actually had - a recording spike found core.EncodeAllow emitting
// well-formed JSON, correct envelope, right request_id, that a real `claude`
// read and answered with "The user did not answer the questions", ending the
// turn subtype "success". Every unit test passed. A grep for
// `"behavior":"allow"` passes against those bytes too, because every byte is
// present; what was wrong was where they sat.
//
// So this fake does the two things a grep cannot. It decodes the answer
// structurally, through the nesting the CLI actually reads, so a verdict
// written at the wrong depth is invisible to it rather than merely differently
// spelled. And an allow makes it *do* something outside Wake entirely, so a
// test can assert on the far side's own state rather than on anything Wake said
// about itself.

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// answeredPermission is the shape encode.go writes, read back the way the CLI
// reads it: subtype and request_id one level down under "response", and the
// verdict one level below that.
//
// Deliberately not strings.Contains. A frame with `behavior` on the envelope,
// or nested once instead of twice, contains the same substring and answers
// nothing - which is exactly the failure that was live in this codebase and
// that every shape-of-the-bytes assertion passed through.
type answeredPermission struct {
	Response struct {
		RequestID string `json:"request_id"`
		Response  struct {
			Behavior string `json:"behavior"`
			Message  string `json:"message"`
		} `json:"response"`
	} `json:"response"`
}

// toolRanPath and toolDeniedPath are where this agent records what happened,
// keyed by session id.
//
// By session id because one environment is inherited by every agent the daemon
// spawns, so a single configured path would have two concurrently asking
// sessions writing the same file - and the cross-contamination test would then
// be unable to tell "the right agent ran it" from "the wrong one did".
func toolRanPath(dir, sid string) string    { return filepath.Join(dir, sid+".ran") }
func toolDeniedPath(dir, sid string) string { return filepath.Join(dir, sid+".denied") }

// toolRanMarker is what the tool writes when it actually runs. Its content is
// arbitrary; its existence is the whole assertion.
const toolRanMarker = "the tool ran"

// fakeTool asks permission for a Write and then behaves like one.
//
// An allow runs the tool. A deny records the reason it was given, verbatim and
// unexamined, which is the only way a test can see what core.EncodeDeny
// actually put in front of the model: that text is echoed back as the tool
// result, so this file standing in for the model has to keep it rather than
// judge it.
func fakeTool(sid string) int {
	dir := os.Getenv(fakeToolDirEnv)
	emitText(sid, "ready")
	fmt.Printf(`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":%q,"content":"ok"},"tool_use_id":"toolu_1"}}`+"\n",
		askRequestID, toolRanPath(dir, sid))

	for line := range stdinLines() {
		var ans answeredPermission
		if err := json.Unmarshal([]byte(line), &ans); err != nil {
			continue
		}
		// The ask this agent is actually blocked on. A real CLI ignores a
		// control_response naming a request that does not exist, and so does
		// this - which is what makes an answer misrouted to the wrong session
		// silent here rather than helpfully absorbed.
		if ans.Response.RequestID != askRequestID {
			continue
		}
		switch ans.Response.Response.Behavior {
		case "allow":
			writeToolRecord(toolRanPath(dir, sid), toolRanMarker)
			emitText(sid, "tool ran")
		case "deny":
			writeToolRecord(toolDeniedPath(dir, sid), ans.Response.Response.Message)
			emitText(sid, "tool refused")
		default:
			// Anything else is a frame this agent has no verdict from, and a
			// blocked process stays blocked. Continuing rather than guessing is
			// what lets a test observe an answer that never arrived.
			continue
		}
		emitResult(sid)
	}
	return 0
}

// writeToolRecord is the side effect. A failure is reported on stderr, which
// core captures and the daemon surfaces, rather than swallowed into a test
// that would then merely see no file and blame the wrong layer.
func writeToolRecord(path, body string) {
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "tool:", err)
	}
}

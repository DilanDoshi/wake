package core

// The directory a session is running in, which is not the one it was spawned
// in for as long as EnterWorktree exists.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnInitFrameCarriesTheDirectoryTheSessionIsIn(t *testing.T) {
	ev := onlyEvent0(t, `{"type":"system","subtype":"init","session_id":"s1","cwd":"/repos/wake/.wake/worktrees/sydney","model":"claude-sonnet-5"}`)

	if ev.Session == nil {
		t.Fatal("an init frame produced no session facts at all")
	}
	if got := ev.Session.Dir; got != "/repos/wake/.wake/worktrees/sydney" {
		t.Errorf("init.cwd decoded to %q, want the directory the frame named", got)
	}
}

// The facts are merged field by field by every consumer, so a frame that names
// no directory has to carry the empty string rather than a guess. A result
// frame naming one would overwrite a live belief once per turn - the trap
// Model and the context window are already guarded against.
func TestAFrameThatNamesNoDirectoryCarriesNone(t *testing.T) {
	evs := decodeLineT(t, `{"type":"result","subtype":"success","session_id":"s1","model":"claude-sonnet-5","modelUsage":{"claude-sonnet-5":{"contextWindow":1000000}}}`)

	for _, ev := range evs {
		if ev.Session != nil && ev.Session.Dir != "" {
			t.Errorf("a result frame carrying no cwd produced Dir=%q, which would overwrite a live directory once a turn", ev.Session.Dir)
		}
	}
}

// An init with a cwd and no model still carries the directory. The gate used to
// be the model alone, which is fine for every frame anybody has recorded and is
// the wrong domain: this decodes what the wire *says*, and a frame naming one
// of the two is not a frame naming neither.
func TestAnInitWithADirectoryAndNoModelStillCarriesIt(t *testing.T) {
	ev := onlyEvent0(t, `{"type":"system","subtype":"init","session_id":"s1","cwd":"/repos/api"}`)

	if ev.Session == nil || ev.Session.Dir != "/repos/api" {
		t.Errorf("an init naming a directory and no model produced %+v, want the directory", ev.Session)
	}
}

// Derived from the corpus rather than asserted: every recorded init frame
// carries a cwd, which is what makes this a field to read rather than one to
// hope for. If a future recording drops it, this fails and says so.
func TestEveryRecordedInitFrameNamesItsDirectory(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "stream", "*.jsonl"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no recorded streams to read: %v", err)
	}
	seen := 0
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			if !strings.Contains(line, `"subtype":"init"`) {
				continue
			}
			var f struct {
				Type    string `json:"type"`
				Subtype string `json:"subtype"`
				Cwd     string `json:"cwd"`
			}
			if json.Unmarshal([]byte(line), &f) != nil || f.Type != "system" || f.Subtype != "init" {
				continue
			}
			seen++
			if f.Cwd == "" {
				t.Errorf("%s has an init frame with no cwd, so the directory cannot be read back from the stream", filepath.Base(p))
			}
		}
	}
	if seen == 0 {
		t.Fatal("no init frames in the corpus: this test is asserting nothing")
	}
}

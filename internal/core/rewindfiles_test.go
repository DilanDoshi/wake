package core

// Restoring a session's files: the rewind_files request Wake writes, the
// receipts claude answers with (testdata/stream/rewind-files*.jsonl), and the
// session's labelling of each receipt with what was asked.
// docs/superpowers/notes/2026-10-02-file-rewind-findings.md.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const rewindFilesInput = "../../testdata/input/rewind-files.stdin.jsonl"

// Every rewind_files line the driver wrote that claude took as meant, encoded
// again from its own ids. The camelCase dryRun line is the trap: claude
// ignored the key and restored for real, so it must be what Wake never writes.
func TestEncodeRewindFilesMatchesTheRecordedRequests(t *testing.T) {
	f, err := os.Open(rewindFilesInput)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	checked := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		var line struct {
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype       string `json:"subtype"`
				UserMessageID string `json:"user_message_id"`
				DryRun        bool   `json:"dry_run"`
			} `json:"request"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil || line.Request.Subtype != "rewind_files" {
			continue
		}
		got, err := EncodeRewindFiles(line.RequestID, line.Request.UserMessageID, line.Request.DryRun)
		if err != nil {
			t.Fatalf("EncodeRewindFiles: %v", err)
		}
		got = bytes.TrimSuffix(got, []byte("\n"))
		if strings.Contains(sc.Text(), `"dryRun"`) {
			if bytes.Equal(got, sc.Bytes()) {
				t.Errorf("Wake wrote the dryRun spelling claude ignores: %s", got)
			}
			continue
		}
		if !bytes.Equal(got, sc.Bytes()) {
			t.Errorf("encoded %s\nrecorded %s", got, sc.Bytes())
		}
		checked++
	}
	if checked < 8 {
		t.Fatalf("checked %d recorded requests, want the fixture's 8", checked)
	}
}

func TestEncodeRewindFilesRefusesABlankIDOrMessage(t *testing.T) {
	for _, c := range []struct{ id, target string }{{"", "u"}, {"r", ""}} {
		if _, err := EncodeRewindFiles(c.id, c.target, true); !errors.Is(err, ErrNotWritten) {
			t.Errorf("EncodeRewindFiles(%q, %q) err = %v, want ErrNotWritten", c.id, c.target, err)
		}
	}
}

// The four receipts the airlock can name on its own, known by canRewind's
// presence. Which request each answers - and whether it was a preview - is
// the session's to say; see the labelling tests below.
func TestDecodeFilesRewindReceipts(t *testing.T) {
	for _, c := range []struct {
		name, marker string
		want         FilesRewind
	}{
		{"preview with files", "probe-5c216df1", FilesRewind{Restorable: true,
			Files: []string{"/private/tmp/rwf-6zwblejt/a.txt", "/private/tmp/rwf-6zwblejt/b.txt"}, Deletions: 2}},
		{"preview, nothing to undo", "probe-98b43fa4", FilesRewind{Restorable: true}},
		{"restored", "probe-89968866", FilesRewind{Restorable: true}},
		{"preview refused", "probe-07d46284", FilesRewind{Error: "No file checkpoint found for this message."}},
	} {
		line, n := lineContaining(t, "testdata/stream/rewind-files.jsonl", c.marker)
		ev := onlyEvent(t, line, n)
		if ev.Kind != KindFilesRewindReceipt || ev.Files == nil {
			t.Fatalf("%s: kind %q files %+v, want a files rewind receipt", c.name, ev.Kind, ev.Files)
		}
		got := *ev.Files
		if got.Restorable != c.want.Restorable || got.Error != c.want.Error || got.Deletions != c.want.Deletions ||
			strings.Join(got.Files, ",") != strings.Join(c.want.Files, ",") {
			t.Errorf("%s: decoded %+v, want %+v", c.name, got, c.want)
		}
		if ev.RequestID != c.marker {
			t.Errorf("%s: request id %q, want %q", c.name, ev.RequestID, c.marker)
		}
	}
}

func TestTheSessionLabelsAPreviewWithItsTarget(t *testing.T) {
	s, buf := mcpSession(t)
	if err := s.RewindFiles("ask-1", FilesRewind{Target: "U3", Preview: true}); err != nil {
		t.Fatal(err)
	}
	if sent, req := sentRequest(t, buf); sent != "ask-1" || req["subtype"] != "rewind_files" ||
		req["user_message_id"] != "U3" || req["dry_run"] != true {
		t.Fatalf("RewindFiles wrote %q %v", sent, req)
	}
	reply := `{"type":"control_response","response":{"subtype":"success","request_id":"ask-1","response":{"canRewind":true,"filesChanged":["/p/b.txt"],"insertions":0,"deletions":1}}}`
	ev := s.attribute(onlyEvent(t, reply, 0))
	if ev.Kind != KindFilesRewindReceipt || ev.Files == nil || ev.Files.Target != "U3" || !ev.Files.Preview ||
		len(ev.Files.Files) != 1 || ev.Files.Deletions != 1 {
		t.Fatalf("labelled %q %+v, want the preview of U3 with its one file", ev.Kind, ev.Files)
	}
}

// A refused restore is a bare error receipt, the shape a mode refusal has
// (rewind-files.jsonl line 58): only the id says it answers a restore.
func TestARefusedRestoreIsLabelledByTheAsk(t *testing.T) {
	s, _ := mcpSession(t)
	if err := s.RewindFiles("probe-32e3d84a", FilesRewind{Target: "U9", Both: true}); err != nil {
		t.Fatal(err)
	}
	line, n := lineContaining(t, "testdata/stream/rewind-files.jsonl", "probe-32e3d84a")
	if bare := onlyEvent(t, line, n); bare.Kind != KindControlReceipt {
		t.Fatalf("the airlock named a bare refusal %q on its own; the session's label is what says", bare.Kind)
	}
	ev := s.attribute(onlyEvent(t, line, n))
	if ev.Kind != KindFilesRewindReceipt || ev.Files == nil || ev.Files.Target != "U9" || ev.Files.Preview || !ev.Files.Both ||
		ev.Files.Restorable || ev.Files.Error != "No file checkpoint found for this message." || ev.Control != nil {
		t.Fatalf("labelled %q files %+v control %+v, want a refused restore of U9, the code half of both", ev.Kind, ev.Files, ev.Control)
	}
}

func TestAnUnwrittenRewindFilesIsNotRemembered(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	if err := s.RewindFiles("r", FilesRewind{Target: "U1", Preview: true}); err == nil {
		t.Fatal("a session that never started accepted a write")
	}
	if n := s.pendingAsks(); n != 0 {
		t.Errorf("%d asks remembered after a failed write", n)
	}
}

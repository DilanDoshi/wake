//go:build drift

// The corpus guards in injected_test.go, run over this machine's own
// transcripts. A recording is a photograph of one claude version; the
// operator's ~/.claude/projects is every version since, so claude changing
// what it writes shows here first - the subagent hand-back restored as the
// operator's turn for two weeks before anyone saw it (BUG-41).
//
// Not a gate: it reads data no other machine has. `make drift` runs it. It
// prints counts and the first words of a line with the home directory cut,
// never a whole line, and writes nothing.

package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// driftHead is how much of a line the audit shows: enough to name the kind.
const driftHead = 48

func driftRoot(t *testing.T) string {
	if dir := os.Getenv("WAKE_PROJECTS"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	return filepath.Join(home, ".claude", "projects")
}

// driftTally counts findings by a short, redacted key.
type driftTally map[string]int

func (d driftTally) report(t *testing.T, what string) {
	if len(d) == 0 {
		return
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return d[keys[i]] > d[keys[j]] })
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "\n  %6d  %s", d[k], k)
	}
	t.Errorf("%s:%s", what, b.String())
}

func redactedHead(text string) string {
	// The home directory, and its slug in claude's project directory names.
	if home, _ := os.UserHomeDir(); home != "" {
		text = strings.NewReplacer(home, "~", strings.ReplaceAll(home, "/", "-"), "~").Replace(text)
	}
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > driftHead {
		text = string(r[:driftHead]) + "…"
	}
	return text
}

// TestDrift reads every top-level transcript and fails on what the recorded
// corpus would have caught: claude's own line restored as the operator's turn,
// or a mark nothing has ruled on.
func TestDrift(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(driftRoot(t), "*", "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no transcripts under %s (err=%v): this audit would assert nothing", driftRoot(t), err)
	}
	misread, origins, sources, undecodable := driftTally{}, driftTally{}, driftTally{}, driftTally{}
	newest, lines := "", 0
	for _, f := range files {
		driftFile(t, f, func(line []byte) {
			lines++
			var v struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(line, &v) == nil && newerVersion(v.Version, newest) {
				newest = v.Version
			}
			m, ok := marksOf(line)
			if !ok {
				return
			}
			if _, ruled := ruledOrigins[m.Origin.Kind]; m.Origin.Kind != "" && !ruled {
				origins[m.Origin.Kind]++
			}
			if _, ruled := ruledPromptSources[m.PromptSource]; m.PromptSource != "" && !ruled {
				sources[m.PromptSource]++
			}
			evs, err := DecodeTranscriptLine(line)
			if err != nil {
				undecodable[redactedHead(err.Error())]++
			}
			for _, ev := range evs {
				if m.injected() && operatorTurn(ev) {
					misread[fmt.Sprintf("origin=%q meta=%v source=%q  %s", m.Origin.Kind, m.Meta, m.PromptSource, redactedHead(ev.Text))]++
				}
			}
		})
	}
	t.Logf("read %d lines in %d transcripts; newest claude %s, corpus newest %s", lines, len(files), newest, corpusNewest(t))
	misread.report(t, "claude's own lines that restore as the operator's turn")
	origins.report(t, "origin kinds nothing has ruled on (injected_test.go's ruledOrigins)")
	sources.report(t, "promptSource values nothing has ruled on (ruledPromptSources)")
	undecodable.report(t, "user lines DecodeTranscriptLine refuses")
}

func driftFile(t *testing.T, path string, each func([]byte)) {
	fh, err := os.Open(path)
	if err != nil {
		t.Logf("skip %s: %v", filepath.Base(path), err)
		return
	}
	defer func() { _ = fh.Close() }()
	br := bufio.NewReaderSize(fh, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && len(line) <= maxLineBytes {
			each(line)
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Logf("skip the rest of %s: %v", filepath.Base(path), err)
			return
		}
	}
}

// corpusNewest is the newest claude version any recorded init frame names.
func corpusNewest(t *testing.T) string {
	newest := ""
	for _, f := range fixtureFiles(t) {
		for _, line := range fixtureLines(t, f) {
			var v struct {
				Version string `json:"claude_code_version"`
			}
			if json.Unmarshal([]byte(line), &v) == nil && newerVersion(v.Version, newest) {
				newest = v.Version
			}
		}
	}
	return newest
}

// newerVersion compares dotted versions numerically, so 2.1.288 is newer than 2.1.29.
func newerVersion(a, b string) bool {
	if a == "" {
		return false
	}
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			_, _ = fmt.Sscan(pa[i], &x)
		}
		if i < len(pb) {
			_, _ = fmt.Sscan(pb[i], &y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

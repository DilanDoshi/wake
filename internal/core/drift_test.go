//go:build drift

// The corpus guards in injected_test.go, run over this machine's own
// transcripts. A recording is a photograph of one claude version; the
// operator's ~/.claude/projects is every version since, so claude changing
// what it writes shows here first - the subagent hand-back restored as the
// operator's turn for two weeks before anyone saw it (BUG-42).
//
// Not a gate: it reads data no other machine has. `make drift` runs it. It
// prints counts and the first words of a line with the home directory cut,
// and writes nothing. Those words are still the operator's own text: never
// paste the output into anything public.

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

// driftAudit is one pass over the transcripts.
type driftAudit struct {
	misread, origins, sources, retyped, undecodable driftTally
	marks                                           map[string]int
	newest                                          string
	lines, userLines, skipped                       int
}

// TestDrift reads every top-level transcript and fails on what the recorded
// corpus would have caught: claude's own line restored as the operator's turn,
// or a mark nothing has ruled on.
func TestDrift(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(driftRoot(t), "*", "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no transcripts under %s (err=%v): this audit would assert nothing", driftRoot(t), err)
	}
	a := driftAudit{misread: driftTally{}, origins: driftTally{}, sources: driftTally{}, retyped: driftTally{}, undecodable: driftTally{}, marks: map[string]int{}}
	for _, f := range files {
		a.skipped += driftLines(t, f, a.line)
	}
	if a.marks["any"] == 0 {
		t.Fatalf("read %d lines (%d user) in %d transcripts and none was claude's own: this audit asserted nothing", a.lines, a.userLines, len(files))
	}
	t.Logf("read %d lines (%d user, %d too long to read) in %d transcripts; claude's own lines by mark %v; newest claude %s, corpus newest %s",
		a.lines, a.userLines, a.skipped, len(files), a.marks, a.newest, corpusNewest(t))
	a.misread.report(t, "claude's own lines that restore as the operator's turn")
	a.origins.report(t, "origin kinds nothing has ruled on (injected_test.go's ruledOrigins)")
	a.sources.report(t, "promptSource values nothing has ruled on (ruledPromptSources)")
	a.retyped.report(t, "user lines whose marks changed type")
	a.undecodable.report(t, "user lines DecodeTranscriptLine refuses")
}

func (a *driftAudit) line(line []byte) {
	a.lines++
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(line, &v) == nil && newerVersion(v.Version, a.newest) {
		a.newest = v.Version
	}
	m, ok, err := marksOf(line)
	if err != nil {
		a.retyped[redactedHead(err.Error())]++
	}
	if !ok {
		return
	}
	a.userLines++
	if _, ruled := ruledOrigins[m.Origin.Kind]; m.Origin.Kind != "" && !ruled {
		a.origins[redactedHead(m.Origin.Kind)]++
	}
	if _, ruled := ruledPromptSources[m.PromptSource]; m.PromptSource != "" && !ruled {
		a.sources[redactedHead(m.PromptSource)]++
	}
	evs, err := DecodeTranscriptLine(line)
	if err != nil {
		a.undecodable[redactedHead(err.Error())]++
	}
	if !m.injected() {
		return
	}
	a.marks["any"]++
	for mark, on := range m.present() {
		if on {
			a.marks[mark]++
		}
	}
	for _, ev := range evs {
		if operatorTurn(ev) {
			a.misread[redactedHead(fmt.Sprintf("origin=%s meta=%v source=%s", m.Origin.Kind, m.Meta, m.PromptSource))+"  "+redactedHead(ev.Text)]++
		}
	}
}

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

// driftLines hands each line of path to each and returns how many it skipped
// for being longer than maxLineBytes - read in pieces and dropped, so one huge
// attachment record costs no more memory than the bound.
func driftLines(t *testing.T, path string, each func([]byte)) (skipped int) {
	fh, err := os.Open(path)
	if err != nil {
		t.Errorf("open %s: %v", filepath.Base(path), err)
		return 0
	}
	defer func() { _ = fh.Close() }()
	br := bufio.NewReaderSize(fh, 1<<20)
	for {
		var line []byte
		long := false
		for {
			chunk, more, err := br.ReadLine()
			if !long && len(line)+len(chunk) <= maxLineBytes {
				line = append(line, chunk...)
			} else {
				long, line = true, nil
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					t.Errorf("read %s: %v", filepath.Base(path), err)
				}
				return skipped
			}
			if !more {
				break
			}
		}
		if long {
			skipped++
		} else if len(line) > 0 {
			each(line)
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

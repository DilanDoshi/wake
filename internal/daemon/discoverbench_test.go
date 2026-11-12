package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A machine's worth of transcripts, generated: lines shaped like claude's (a cwd
// beside a message that carries most of the bytes), a prompt and a title near the
// end, and a few files large enough that the directory they prove is deep in.
// None of it is a real transcript.
const (
	benchFiles    = 240
	benchBigEvery = 20
	benchSmall    = 128 << 10
	benchBig      = 6 << 20
)

func benchLine(cwd, id string, i int) string {
	return fmt.Sprintf(`{"parentUuid":"p%d","isSidechain":false,"userType":"external","cwd":%q,"sessionId":%q,`+
		`"version":"2.1.322","gitBranch":"main","type":"assistant","message":{"role":"assistant","content":[`+
		`{"type":"text","text":%q},{"type":"tool_use","id":"t%d","name":"Bash","input":{"command":"ls","description":"d"}}],`+
		`"usage":{"input_tokens":1,"output_tokens":2}},"uuid":"u%d","timestamp":"2026-11-11T00:00:00.000Z"}`,
		i, cwd, id, strings.Repeat("w", 3000), i, i)
}

func writeBenchTranscript(tb testing.TB, path, cwd, id string, size int) {
	tb.Helper()
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		b.WriteString(benchLine(cwd, id, i))
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, `{"type":"last-prompt","lastPrompt":"the newest thing asked","sessionId":%q}`+"\n", id)
	fmt.Fprintf(&b, `{"type":"custom-title","customTitle":"bench-%s","sessionId":%q}`+"\n", id[:8], id)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		tb.Fatal(err)
	}
}

// benchCorpus writes the tree and returns its root and its size in bytes.
func benchCorpus(tb testing.TB) (string, int64) {
	tb.Helper()
	projects := tb.TempDir()
	var total int64
	for i := 0; i < benchFiles; i++ {
		cwd := tb.TempDir()
		slugDir := filepath.Join(projects, slugOf(cwd))
		if err := os.MkdirAll(slugDir, 0o755); err != nil {
			tb.Fatal(err)
		}
		size := benchSmall
		if i%benchBigEvery == 0 {
			size = benchBig
		}
		id := fmt.Sprintf("%08x-0000-4000-8000-000000000000", i+1)
		writeBenchTranscript(tb, filepath.Join(slugDir, id+".jsonl"), cwd, id, size)
		total += int64(size)
	}
	return projects, total
}

// The benchmarks pin the two numbers the picker is felt through: a first open,
// and the open after it. `go test -bench Discover ./internal/daemon`.

func BenchmarkDiscoverDecodingWalk(b *testing.B) {
	projects, total := benchCorpus(b)
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := discoverDecoded(projects); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiscoverCold(b *testing.B) {
	projects, total := benchCorpus(b)
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessionScans.reset()
		if _, err := discover(projects); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiscoverWarm(b *testing.B) {
	projects, total := benchCorpus(b)
	if _, err := discover(projects); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := discover(projects); err != nil {
			b.Fatal(err)
		}
	}
}

// One transcript grows between opens, as a session being worked in does.
func BenchmarkDiscoverWarmOneTranscriptGrew(b *testing.B) {
	projects, total := benchCorpus(b)
	if _, err := discover(projects); err != nil {
		b.Fatal(err)
	}
	entries, _ := os.ReadDir(projects)
	jobs := transcriptFiles(projects, entries)
	grown := jobs[1].path // a small one: the cost is the walk plus one re-read
	b.SetBytes(total)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := os.OpenFile(grown, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = f.WriteString(`{"type":"last-prompt","lastPrompt":"grew"}` + "\n")
		_ = f.Close()
		if _, err := discover(projects); err != nil {
			b.Fatal(err)
		}
	}
}

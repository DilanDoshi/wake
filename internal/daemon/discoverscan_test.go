package daemon

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The top-level scanner replaced a json.Unmarshal of every transcript line, so
// every test here holds it to that decoder: same fields from every line the
// decoder reads, and never a line lost or a field mis-read. The one direction it
// may differ in is documented on scanTopLevel (a malformed token inside a value
// it skips), and nothing below asserts that direction.

// scanLine is the scanner's account of one line, in decodeLine's shape.
func scanLine(raw []byte) (fields [fieldCount]string, present [fieldCount]bool, ok bool) {
	vals := make([][]byte, fieldCount)
	if len(raw) == 0 || !scanTopLevel(raw, transcriptFields[:], vals) {
		return fields, present, false
	}
	for k := range vals {
		fields[k], present[k] = decodeString(vals[k])
	}
	return fields, present, true
}

// agrees is the contract: wherever the decoder reads a line, the scanner finds
// the same four fields. A bare `null` is the one line the decoder reads that
// holds no fields, so the scanner may drop it: nothing is lost either way.
func agrees(t *testing.T, raw []byte) {
	t.Helper()
	wantF, wantP, wantOK := decodeLine(raw)
	if !wantOK {
		return
	}
	gotF, gotP, gotOK := scanLine(raw)
	if !gotOK && wantP == [fieldCount]bool{} {
		return
	}
	if !gotOK || gotF != wantF || gotP != wantP {
		t.Fatalf("the decoder reads %q as %q/%v but the scanner reads it as ok=%v %q/%v",
			raw, wantF, wantP, gotOK, gotF, gotP)
	}
}

// Lines the decoder reads, written by hand, one per shape the scanner could get
// wrong: where a key sits, how a value is spelled, what surrounds the object.
var readableLines = []string{
	`{"cwd":"/a","lastPrompt":"p","customTitle":"t","aiTitle":"g"}`,
	`{}`,
	` {} `,
	"\t{ \"cwd\" : \"/a\" , \"x\" : 1 }\t",
	`{"x":1,"cwd":"/a"}`,
	// A wanted key repeated: the last one wins, as in a map.
	`{"cwd":"/a","cwd":"/b","lastPrompt":"1","lastPrompt":"2"}`,
	`{"customTitle":"old","customTitle":"","aiTitle":"x"}`,
	// A wanted key inside a nested value is not the top level's.
	`{"message":{"cwd":"/nested","content":[{"cwd":"/deeper","customTitle":"n"}]},"cwd":"/top"}`,
	`{"a":[{"cwd":"/nested"}],"b":{"lastPrompt":"nested"}}`,
	// Braces, brackets, commas, colons and quotes inside strings are not structure.
	`{"message":{"content":"}{\"cwd\":\"/forged\"}]["},"cwd":"/real"}`,
	`{"s":"\\","cwd":"/after-backslash"}`,
	`{"s":"\\\\\"","cwd":"/after-escaped-quote"}`,
	`{"s":"a\"b","cwd":"/c"}`,
	// Escapes in a value the scanner hands to the decoder, and in a key.
	`{"customTitle":"a\"b\\c\u00e9\ud83d\ude00\n\t\/","cwd":"/\u00e9"}`,
	`{"c\u0077d":"/spelled-with-an-escape"}`,
	`{"lastPrompt":"line\nbreak and \u2028 sep"}`,
	// Blank and non-string values read as absent, every one of them.
	`{"cwd":"","lastPrompt":"   ","customTitle":null,"aiTitle":1}`,
	`{"cwd":{"a":1},"lastPrompt":[1],"customTitle":true,"aiTitle":false}`,
	`{"cwd":-1.5e10,"lastPrompt":0,"customTitle":1E-3}`,
	`{"cwd":" \t\u00a0 ","lastPrompt":"\u3000"}`,
	// Literals and numbers between keys.
	`{"a":true,"b":false,"c":null,"d":-0.5,"e":1e5,"cwd":"/x"}`,
	// Deep nesting and arrays of arrays.
	`{"a":[[[[{"b":[{"c":{"d":[]}}]}]]]],"cwd":"/deep"}`,
	`{"a":{},"b":[],"c":"","cwd":"/empties"}`,
	// Invalid UTF-8 is replaced by the decoder, so both read the same words.
	"{\"customTitle\":\"bad\xff\xfebytes\",\"cwd\":\"/u\"}",
	`{"customTitle":"\ud800 lone surrogate"}`,
	// A line that is no object but reads as one holding nothing.
	`null`, ` null `,
	// A key that merely contains a wanted one.
	`{"xcwd":"/no","cwdx":"/no","Cwd":"/no","cwd ":"/no"}`,
}

func TestTheScannerReadsEveryLineTheDecoderReadsTheSameWay(t *testing.T) {
	for _, line := range readableLines {
		t.Run(line, func(t *testing.T) {
			if _, _, ok := decodeLine([]byte(line)); !ok {
				t.Fatalf("this case is meant to be a line the decoder reads, and it does not")
			}
			agrees(t, []byte(line))
		})
	}
}

// Lines the decoder drops whole. The scanner must drop them too wherever the
// breakage is structure: it cannot see a bad token inside a value it skips (see
// scanTopLevel), and none of these is that.
var unreadableLines = []string{
	``, ` `, `{`, `}`, `{"cwd"`, `{"cwd":`, `{"cwd":"/a"`, `{"cwd":"/a`, `{"cwd":"/a",`,
	`{"cwd":"/a",}`, `{,}`, `{"cwd" "/a"}`, `{"cwd":}`, `{"cwd":,"a":1}`, `{cwd:"/a"}`,
	`{"cwd":"/a"} x`, `{"cwd":"/a"}{"cwd":"/b"}`, `{"cwd":"/a"}}`, "\xef\xbb\xbf{\"cwd\":\"/a\"}",
	`[]`, `[{"cwd":"/a"}]`, `"cwd"`, `true`, `12`,
	`{"a":[}`, `{"a":{]}`, `{"a":[1,2}`, `{"a":{"b":1]}`, `{"a":[{"b":1}}`,
	`{"a":"unterminated,"cwd":"/a"}`, `{"a":x,"cwd":"/a"}`, `{"a":tru}`, `{"a":nul}`,
	`{"a":1,"cwd":"/a"`, `{"a":{"b":{"c":1}}`,
	`{"c\qwd":"/a"}`,
}

func TestTheScannerDropsAStructurallyBrokenLineWholeAsTheDecoderDoes(t *testing.T) {
	for _, line := range unreadableLines {
		t.Run(line, func(t *testing.T) {
			if _, _, ok := decodeLine([]byte(line)); ok {
				t.Fatalf("this case is meant to be a line the decoder drops, and it reads it")
			}
			if _, _, ok := scanLine([]byte(line)); ok {
				t.Errorf("the scanner read %q, which the decoder drops", line)
			}
		})
	}
}

// A torn write only ever cuts a line short, so every strict prefix of a readable
// line must be dropped - the one failure a live transcript can actually have.
func TestTheScannerDropsEveryTornPrefixOfALine(t *testing.T) {
	for _, line := range readableLines {
		for n := 0; n < len(line); n++ {
			prefix := []byte(line[:n])
			if _, _, ok := decodeLine(prefix); ok {
				continue // a prefix that is itself a whole object (leading spaces)
			}
			if _, _, ok := scanLine(prefix); ok {
				t.Fatalf("the scanner read the torn line %q (cut from %q), which the decoder drops", prefix, line)
			}
		}
	}
}

// jsonGen writes random valid objects: a shape no hand-written table covers, to
// find the line the scanner and the decoder disagree on.
type jsonGen struct{ r *rand.Rand }

var stringPieces = []string{
	"a", "b", "/", " ", "x y", `\"`, `\\`, `\/`, `\n`, `\t`, `\u00e9`, `\ud83d\ude00`, `\u0000`,
	"é", "😀", "{", "}", "[", "]", ",", ":", `{\"cwd\":\"/forged\"}`, `\\\"`,
}

func (g jsonGen) ws() string { return []string{"", "", " ", "\t", "  "}[g.r.Intn(5)] }

func (g jsonGen) str() string {
	var b strings.Builder
	b.WriteByte('"')
	for i, n := 0, g.r.Intn(6); i < n; i++ {
		b.WriteString(stringPieces[g.r.Intn(len(stringPieces))])
	}
	b.WriteByte('"')
	return b.String()
}

func (g jsonGen) scalar() string {
	switch g.r.Intn(8) {
	case 0:
		return "true"
	case 1:
		return "false"
	case 2:
		return "null"
	case 3:
		return []string{"0", "-1", "1.5e10", "1E-3", "-0.25"}[g.r.Intn(5)]
	default:
		return g.str()
	}
}

func (g jsonGen) value(depth int) string {
	if depth == 0 || g.r.Intn(3) == 0 {
		return g.scalar()
	}
	if g.r.Intn(2) == 0 {
		return g.array(depth - 1)
	}
	return g.object(depth-1, false)
}

func (g jsonGen) array(depth int) string {
	var b strings.Builder
	b.WriteString("[" + g.ws())
	for i, n := 0, g.r.Intn(4); i < n; i++ {
		if i > 0 {
			b.WriteString(g.ws() + "," + g.ws())
		}
		b.WriteString(g.value(depth))
	}
	b.WriteString(g.ws() + "]")
	return b.String()
}

func (g jsonGen) key(top bool) string {
	if top && g.r.Intn(2) == 0 {
		k := transcriptFields[g.r.Intn(fieldCount)]
		if g.r.Intn(6) == 0 {
			// The same key, one letter written as an escape.
			return `"` + k[:1] + fmt.Sprintf(`\u%04x`, k[1]) + k[2:] + `"`
		}
		return `"` + k + `"`
	}
	return []string{`"a"`, `"b"`, `"message"`, `"cwdx"`, `"xcwd"`, g.str()}[g.r.Intn(6)]
}

func (g jsonGen) object(depth int, top bool) string {
	var b strings.Builder
	b.WriteString("{" + g.ws())
	for i, n := 0, g.r.Intn(7); i < n; i++ {
		if i > 0 {
			b.WriteString(g.ws() + "," + g.ws())
		}
		b.WriteString(g.key(top) + g.ws() + ":" + g.ws())
		// A wanted key mostly carries a string, as in a real transcript.
		if top && g.r.Intn(3) != 0 {
			b.WriteString(g.str())
		} else {
			b.WriteString(g.value(depth))
		}
	}
	b.WriteString(g.ws() + "}")
	return g.ws() + b.String() + g.ws()
}

func TestTheScannerAgreesWithTheDecoderOnRandomObjectsTheirTornPrefixesAndMutations(t *testing.T) {
	g := jsonGen{rand.New(rand.NewSource(20261111))}
	for i := 0; i < 3000; i++ {
		line := []byte(g.object(3, true))
		if _, _, ok := decodeLine(line); !ok {
			t.Fatalf("the generator wrote a line the decoder rejects: %q", line)
		}
		agrees(t, line)
		// Every strict prefix of an object is a torn write; sampled on long ones.
		step := 1 + len(line)/200
		for n := 0; n < len(line); n += step {
			if _, _, ok := decodeLine(line[:n]); ok {
				continue
			}
			if _, _, ok := scanLine(line[:n]); ok {
				t.Fatalf("the scanner read the torn line %q, which the decoder drops", line[:n])
			}
		}
		// One byte changed: wherever the decoder still reads it, so must the scanner.
		for m := 0; m < 8; m++ {
			mutated := append([]byte(nil), line...)
			mutated[g.r.Intn(len(mutated))] = `"{}[],:\ x1`[g.r.Intn(11)]
			agrees(t, mutated)
		}
	}
}

func FuzzScanTopLevel(f *testing.F) {
	for _, line := range readableLines {
		f.Add([]byte(line))
	}
	for _, line := range unreadableLines {
		f.Add([]byte(line))
	}
	f.Fuzz(func(t *testing.T, raw []byte) { agrees(t, raw) })
}

// transcriptOf writes a transcript made of exactly these lines, whatever they
// hold, and returns its path.
func transcriptOf(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// sameRead holds readTranscript to the decoding reader on one file.
func sameRead(t *testing.T, path string) {
	t.Helper()
	want := readTranscriptDecoded(path)
	got, err := readTranscript(path)
	if err != nil {
		t.Fatalf("readTranscript: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("readTranscript read %+v, the decoding reader read %+v", got, want)
	}
}

func TestReadingATranscriptGivesWhatTheDecodingReaderGave(t *testing.T) {
	big := strings.Repeat("z", transcriptScanBytes+10)
	cases := map[string][]string{
		"every readable line, in order":  readableLines,
		"every unreadable line":          unreadableLines,
		"a line past the cap is skipped": {`{"cwd":"/before"}`, `{"cwd":"/huge","pad":"` + big + `"}`, `{"cwd":"/after","customTitle":"kept"}`},
		"a title then a generated title": {`{"customTitle":"named"}`, `{"aiTitle":"generated"}`},
		"only a generated title":         {`{"aiTitle":"generated"}`, `{"cwd":"/a"}`},
		"no preview falls back to title": {`{"customTitle":"named","cwd":"/a"}`},
		"a prompt per turn, newest wins": {`{"lastPrompt":"one"}`, `{"cwd":"/a"}`, `{"lastPrompt":"two"}`},
		"CRLF line endings":              {"{\"cwd\":\"/a\"}\r", "{\"lastPrompt\":\"p\"}\r"},
		"a torn tail after good lines":   {`{"cwd":"/a","lastPrompt":"p"}`, `{"cwd":"/b","lastPrompt":"torn`},
		"blank lines and non-objects":    {``, `   `, `[1,2]`, `"cwd"`, `{"cwd":"/a"}`},
		"two cwds in one transcript":     {`{"cwd":"/a"}`, `{"cwd":"/b"}`, `{"cwd":"/a"}`},
		"a prompt long enough to bound":  {`{"lastPrompt":"` + strings.Repeat("é", 200) + `"}`},
		"control characters in a title":  {`{"customTitle":"a\u001b[31mred\u0007\n"}`},
		"an empty transcript":            {},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) { sameRead(t, transcriptOf(t, lines...)) })
	}
}

func TestReadingACwdFarPastAnyHeadBoundStillFindsIt(t *testing.T) {
	pad := `{"type":"assistant","text":"` + strings.Repeat("z", 4096) + `"}`
	lines := make([]string, 0, 700)
	for len(lines)*len(pad) < 3<<20 {
		lines = append(lines, pad)
	}
	lines = append(lines, `{"cwd":"/deep"}`)
	p := transcriptOf(t, lines...)
	sameRead(t, p)
	if got, _ := readTranscript(p); !reflect.DeepEqual(got.cwds, []string{"/deep"}) {
		t.Fatalf("the cwd three megabytes in was read as %q", got.cwds)
	}
}

// What the scanner buys: a line is no longer decoded, so reading a transcript
// costs no allocation per field of every message it holds. A decoder spends a
// map, a copy per key and the value of every key on each line; the bound here is
// a few allocations a line, and is far under that.
func TestReadingATranscriptDoesNotDecodeEveryLine(t *testing.T) {
	const lines = 200
	row := `{"parentUuid":"u","isSidechain":false,"userType":"external","cwd":"/a","sessionId":"s","version":"2.1.322",` +
		`"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + strings.Repeat("w", 2000) +
		`"},{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls","description":"d"}}],"usage":{"input_tokens":1,"output_tokens":2}},` +
		`"uuid":"v","timestamp":"2026-11-11T00:00:00.000Z"}`
	rows := make([]string, lines)
	for i := range rows {
		rows[i] = row
	}
	p := transcriptOf(t, rows...)
	allocs := testing.AllocsPerRun(5, func() { _, _ = readTranscript(p) })
	if limit := float64(lines * 3); allocs > limit {
		t.Fatalf("reading %d lines took %.0f allocations (limit %.0f): a line is being decoded rather than scanned", lines, allocs, limit)
	}
}

// Whole discoveries agree too: the rows, their order, and every field of each.
func TestDiscoveringATreeGivesWhatTheDecodingWalkGave(t *testing.T) {
	projects := t.TempDir()
	real := t.TempDir()
	other := t.TempDir()
	ids := []string{
		"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444",
	}
	writeTranscript(t, projects, slugOf(real), ids[0], real)
	writeTranscript(t, projects, slugOf(real), ids[1], other, "/gone/elsewhere")
	p := writeTranscript(t, projects, slugOf(other), ids[2], other)
	if err := os.WriteFile(p, []byte(`{"cwd":"`+other+`","customTitle":"named \"it\"","lastPrompt":"go"}`+"\n"+`{"cwd":"`+other+`","lastPrompt":"torn`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, slugOf(real), "notes.jsonl"), []byte(`{"cwd":"/x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, projects, "-no-such-dir", ids[3], "/no/such/dir")

	want, err := discoverDecoded(projects)
	if err != nil {
		t.Fatal(err)
	}
	got, err := discover(projects)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 4 || !reflect.DeepEqual(want, got) {
		t.Fatalf("discover gave\n%+v\nthe decoding walk gave\n%+v", got, want)
	}
}

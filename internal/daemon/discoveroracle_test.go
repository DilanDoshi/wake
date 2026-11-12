package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
)

// The reader and the walk this package used before discovery read a line with a
// top-level scanner instead of decoding it, kept as the oracle every new
// discovery path is held to: same rows, same order, same fields.
//
// It is test code on purpose. The production reader is the scanner; this is the
// definition of what "the same answer" means, and a second copy in production
// would be the parallel implementation this tree forbids.

// decodeStringKey is the old decodeString: one top-level key out of a line
// decoded into a map, any shape but a non-blank string treated as absent.
func decodeStringKey(line map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := line[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}

// decodeLine is the oracle's account of one line: the four wanted fields as the
// decoder reads them, and whether the line was read at all.
func decodeLine(raw []byte) (fields [fieldCount]string, present [fieldCount]bool, ok bool) {
	var line map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &line) != nil {
		return fields, present, false
	}
	for k, key := range transcriptFields {
		fields[k], present[k] = decodeStringKey(line, key)
	}
	return fields, present, true
}

// readTranscriptDecoded is readTranscript as it was: every line decoded into a
// map.
func readTranscriptDecoded(path string) transcriptRead {
	f, err := os.Open(path)
	if err != nil {
		return transcriptRead{}
	}
	defer func() { _ = f.Close() }()
	var cwds []string
	var preview, title, name string
	seen := map[string]bool{}
	var generated string
	br := bufio.NewReaderSize(f, 64*1024)
	for {
		raw, err := readTranscriptLine(br)
		if fields, present, ok := decodeLine(raw); ok {
			if c, has := fields[fieldCwd], present[fieldCwd]; has && !seen[c] {
				seen[c] = true
				cwds = append(cwds, c)
			}
			if present[fieldLastPrompt] {
				preview = fields[fieldLastPrompt]
			}
			if present[fieldCustomTitle] {
				title = fields[fieldCustomTitle]
			}
			if present[fieldAITitle] {
				generated = fields[fieldAITitle]
			}
		}
		if err != nil {
			break
		}
	}
	name = title
	if title == "" {
		title = generated
	}
	if preview == "" {
		preview = title
	}
	return transcriptRead{
		cwds: cwds, preview: oneLine(preview, previewBytes), title: oneLine(title, previewBytes), recorded: name,
	}
}

// foundDecoded is one transcript's row as the decoding walk made it.
func foundDecoded(j transcriptFile) (FoundSession, bool) {
	info, isReg := regularTranscript(j.path)
	if !isReg {
		return FoundSession{}, false
	}
	read := readTranscriptDecoded(j.path)
	return FoundSession{
		ID: j.id, Dir: verifiedDir(j.slug, read.cwds), Slug: j.slug, Path: j.path,
		Modified: info.ModTime(), Preview: read.preview, Title: read.title, Name: read.recorded,
	}, true
}

// newestFirstDecoded is the order the picker was always given: newest first, and
// by id where two share a timestamp.
func newestFirstDecoded(rows []FoundSession) {
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].Modified.Equal(rows[j].Modified) {
			return rows[i].Modified.After(rows[j].Modified)
		}
		return rows[i].ID < rows[j].ID
	})
}

// discoverDecoded is discover as it was, read one transcript after another with
// the decoding reader: the rows and the order every other path must reproduce.
func discoverDecoded(projects string) ([]FoundSession, error) {
	entries, err := os.ReadDir(projects)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []FoundSession
	for _, j := range transcriptFiles(projects, entries) {
		if row, ok := foundDecoded(j); ok {
			out = append(out, row)
		}
	}
	newestFirstDecoded(out)
	return out, nil
}

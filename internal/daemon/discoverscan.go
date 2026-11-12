package daemon

import (
	"bytes"
	"encoding/json"
)

// scanTopLevel finds the wanted keys of one transcript line - one JSON object -
// without decoding the rest of it.
//
// A transcript line carries a whole message, and some carry a whole file, so
// decoding each into a map copied every byte of every key of ten gigabytes to
// read four short strings (measured at 90% of a discovery's CPU). This walks the
// line once instead, skipping every value it does not want at memchr speed, and
// leaves each wanted value as raw bytes for decodeString.
//
// vals[k] is the raw value of keys[k] at the object's top level - the last one
// when a key repeats, as in a decoded map - or nil. It reports whether the line
// is one object whose structure closes: a line cut short by a write in progress,
// or no object at all, is not read, as a decoder would not read it.
//
// # Where it is looser than a decoder, on purpose
//
// It checks structure - quotes, colons, commas, matching brackets, nothing after
// the closing brace - and not the grammar inside a value it skips, so a bad
// escape, a raw control character or a malformed number in an unwanted key is
// read where a decoder drops the line. Claude writes each line with one
// serialiser call and a write cut short only ever leaves structure unclosed, so
// nothing a live transcript does reaches that gap. The tests hold the other
// direction without exception: every line a decoder reads is read, with the same
// fields (discoverscan_test.go, and `make drift` against a real corpus).
func scanTopLevel(line []byte, keys []string, vals [][]byte) bool {
	clear(vals)
	i := skipSpace(line, 0)
	if i == len(line) || line[i] != '{' {
		return false
	}
	i = skipSpace(line, i+1)
	if i < len(line) && line[i] == '}' {
		return skipSpace(line, i+1) == len(line)
	}
	for {
		keyEnd := endOfString(line, i)
		if keyEnd < 0 {
			return false
		}
		k, ok := keyIndex(line[i:keyEnd], keys)
		if !ok {
			return false
		}
		i = skipSpace(line, keyEnd)
		if i == len(line) || line[i] != ':' {
			return false
		}
		i = skipSpace(line, i+1)
		end := skipValue(line, i)
		if end < 0 {
			return false
		}
		if k >= 0 {
			vals[k] = line[i:end]
		}
		i = skipSpace(line, end)
		if i == len(line) {
			return false
		}
		switch line[i] {
		case ',':
			i = skipSpace(line, i+1)
		case '}':
			return skipSpace(line, i+1) == len(line)
		default:
			return false
		}
	}
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// endOfString is the index past the string that opens at b[i], or -1 when b[i]
// is no quote or the string never closes. A quote closes it unless an odd run of
// backslashes sits in front of it.
func endOfString(b []byte, i int) int {
	if i >= len(b) || b[i] != '"' {
		return -1
	}
	i++
	for {
		j := bytes.IndexByte(b[i:], '"')
		if j < 0 {
			return -1
		}
		quote := i + j
		slashes := 0
		for p := quote - 1; p >= i && b[p] == '\\'; p-- {
			slashes++
		}
		if slashes%2 == 0 {
			return quote + 1
		}
		i = quote + 1
	}
}

// keyIndex is which of keys the quoted key names, or -1 for none, and whether
// the key is well formed. A key written with an escape means what it decodes to,
// so one is decoded before it is compared.
func keyIndex(quoted []byte, keys []string) (int, bool) {
	name := quoted[1 : len(quoted)-1]
	if bytes.IndexByte(name, '\\') >= 0 {
		var decoded string
		if json.Unmarshal(quoted, &decoded) != nil {
			return -1, false
		}
		name = []byte(decoded)
	}
	for k, key := range keys {
		if string(name) == key {
			return k, true
		}
	}
	return -1, true
}

// skipValue is the index past the value that starts at b[i], or -1 when there is
// none or it never closes.
func skipValue(b []byte, i int) int {
	if i >= len(b) {
		return -1
	}
	switch b[i] {
	case '"':
		return endOfString(b, i)
	case '{', '[':
		return skipContainer(b, i)
	}
	return skipLiteral(b, i)
}

// skipContainer is the index past the object or array that opens at b[i]. Every
// opener must meet its own closer; strings, which are most of the bytes, are
// skipped whole.
func skipContainer(b []byte, i int) int {
	var space [32]byte
	open := space[:0]
	for i < len(b) {
		switch c := b[i]; c {
		case '"':
			if i = endOfString(b, i); i < 0 {
				return -1
			}
			continue
		case '{':
			open = append(open, '}')
		case '[':
			open = append(open, ']')
		case '}', ']':
			if len(open) == 0 || open[len(open)-1] != c {
				return -1
			}
			if open = open[:len(open)-1]; len(open) == 0 {
				return i + 1
			}
		}
		i++
	}
	return -1
}

// skipLiteral is the index past a number, `true`, `false` or `null`, or -1 for
// any other word. A number's own grammar is not checked (see scanTopLevel).
func skipLiteral(b []byte, i int) int {
	end := i
	for end < len(b) && isLiteralByte(b[end]) {
		end++
	}
	switch word := string(b[i:end]); word {
	case "true", "false", "null":
		return end
	case "":
		return -1
	}
	if c := b[i]; c != '-' && (c < '0' || c > '9') {
		return -1
	}
	return end
}

func isLiteralByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '+' || c == '.'
}

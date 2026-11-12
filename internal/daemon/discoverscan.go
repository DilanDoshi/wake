package daemon

import (
	"bytes"
	"encoding/json"
)

// scanTopLevel finds the wanted keys of one transcript line - one JSON object -
// without decoding the rest of it: decoding every line into a map was 90% of a
// discovery's CPU. vals[k] is the raw value of keys[k] at the top level (the last
// one when a key repeats) or nil, and it reports whether the line is one object
// whose structure closes, so a line cut short is dropped as a decoder would drop it.
//
// It checks structure, not grammar: brackets everywhere, and colons and commas at
// the top level. So a bad escape, control character or number in a key or value
// it skips, nesting past a decoder's depth limit, or a wanted value that will not
// decode is read here where a decoder drops the whole line. A write cut short only
// ever leaves structure unclosed. The tests hold the other direction without
// exception: every line a decoder reads is read, with the same fields.
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

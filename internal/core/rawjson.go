package core

// Two questions about a json.RawMessage that need nothing of Claude's format:
// what shape it is, and what string it holds. The airlock decodes with them,
// but they name no key, no tag and no wire word, so they are not the airlock's
// to hold - they moved out of protocol.go and wire.go when merging /mcp and
// workflows filled all four airlock files to the 800-line hard max, and the
// airlock is a fixed four (airlock_test.go's TestTheAirlockIsFourFilesInInternalCore).

import (
	"bytes"
	"encoding/json"
)

// jsonString unquotes a JSON string, falling back to the raw bytes so a
// shape we have not seen still reaches a human instead of vanishing.
//
// The fallback is for shapes that are genuinely unrecorded. It used to catch
// a tool_result's array content as well, which was not unrecorded at all -
// 10 of the 44 recorded results carry it - and printed a JSON literal in the
// transcript. toolResultText handles that shape properly now, and this is
// left to cover what is still unknown.
func jsonString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// firstJSONByte is the cheapest way to tell a JSON string from an array or
// object without a second full unmarshal.
func firstJSONByte(raw json.RawMessage) byte {
	t := bytes.TrimLeft(raw, " \t\r\n")
	if len(t) == 0 {
		return 0
	}
	return t[0]
}

func isJSONObject(raw json.RawMessage) bool { return firstJSONByte(raw) == '{' }
func isJSONArray(raw json.RawMessage) bool  { return firstJSONByte(raw) == '[' }

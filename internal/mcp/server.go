package mcp

// The stdio JSON-RPC loop. One request per line in, one response per line out,
// which is the same newline-delimited framing internal/rpc uses and for the
// same reason: the peer ships with this binary.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// protocolVersion is what this server reports. It is echoed rather than
// negotiated: there is one client and it ships with this binary.
const protocolVersion = "2025-06-18"

const serverName = "wake"

// version is what serverInfo reports. One string, because the client ships
// with this binary and there is no negotiation to get wrong.
const version = "0.2.0"

// JSON-RPC error codes, from the specification.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
)

// maxLine bounds one request. A tool call carrying a broadcast's text is the
// largest thing that arrives, and bufio.Scanner's 64KB default fails by
// *truncating*, which would be silent corruption rather than an error.
const maxLine = 4 * 1024 * 1024

// The methods this server answers. Spelled as constants because the dispatch
// below and any future client in this tree have to agree on them exactly, and
// a typo in a switch case is a method that silently does not exist.
const (
	methodInitialize = "initialize"
	methodToolsList  = "tools/list"
	methodToolsCall  = "tools/call"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads requests until in is exhausted.
//
// A malformed line is an error response, not the end of the connection -
// unlike internal/rpc, and the difference is who the peer is. There, a decode
// failure means a stream already desynced between two Wake processes. Here the
// peer is a model's tool runner, and one bad line is a bad line: ending the
// server would take away every tool the manager has for the rest of its life,
// which is a far worse failure than answering one request with an error.
//
// A *write* failure is the other end of that judgement and does end the loop:
// if the pipe back to the model is gone there is nobody left to answer, and
// reading on would be a process spinning through a conversation nobody hears.
//
// ctx bounds the tool calls, not the read. There is no wait to abandon here -
// the loop is parked in a read on stdin, and stdin closing is how a tool
// runner says it is finished. A goroutine watching ctx so that Serve could
// return a few milliseconds earlier at shutdown would be a goroutine per
// server for no behaviour anybody can observe.
func Serve(ctx context.Context, in io.Reader, out io.Writer, f Fleet) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	enc := json.NewEncoder(out)
	// The same ruling as the daemon's wire, and for the same reason: Go
	// escapes <, > and & into six bytes each to protect JSON embedded in a
	// script tag, and this is a pipe to a local process. See
	// docs/notes/decisions.md - measured 1.87x on bracket-dense payloads,
	// which a fleet report full of file paths and diffs is.
	enc.SetEscapeHTML(false)

	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			if werr := enc.Encode(errorResponse(nil, codeParse, err.Error())); werr != nil {
				return werr
			}
			continue
		}
		// A notification carries no id and is never answered. Answering one
		// puts a response on the wire the client has nothing to correlate it
		// with, which desynchronises its own bookkeeping.
		if len(req.ID) == 0 {
			continue
		}
		if err := enc.Encode(answer(ctx, req, f)); err != nil {
			return err
		}
	}
	return sc.Err()
}

func answer(ctx context.Context, req request, f Fleet) response {
	switch req.Method {
	case methodInitialize:
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocolVersion,
			// An empty tools object is the capability. A client that is not
			// told the server has tools never calls tools/list, so every tool
			// below this line is unreachable and nothing else fails.
			"capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo":   map[string]any{"name": serverName, "version": version},
		}}
	case methodToolsList:
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDescriptors()}}
	case methodToolsCall:
		return callTool(ctx, req, f)
	default:
		return errorResponse(req.ID, codeMethodNotFound, fmt.Sprintf("no method %q", req.Method))
	}
}

// errorResponse is a JSON-RPC error. A nil id marshals as null, which is what
// the specification says to answer a line that could not be parsed with -
// there is no id to echo, and inventing one would correlate against a request
// the client never sent.
func errorResponse(id json.RawMessage, code int, msg string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

// callTool runs one tool and returns its result as MCP content.
//
// A tool's own failure is reported as *content* with isError set, not as a
// JSON-RPC error. The distinction is who the failure belongs to: a JSON-RPC
// error means the request was malformed and the model can do nothing with it,
// while "no live agent has that id, call list_agents" is an instruction the
// model can act on - and it only reaches the model at all through content.
func callTool(ctx context.Context, req request, f Fleet) response {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, codeInvalidRequest, err.Error())
	}
	for _, t := range Tools() {
		if t.Name != params.Name {
			continue
		}
		text, err := t.Call(ctx, f, params.Arguments)
		if err != nil {
			text = err.Error()
		}
		return response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
			"isError": err != nil,
		}}
	}
	return errorResponse(req.ID, codeMethodNotFound, fmt.Sprintf("no tool %q", params.Name))
}

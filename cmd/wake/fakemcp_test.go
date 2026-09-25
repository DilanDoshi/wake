package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
)

// scriptMCP answers the three MCP control requests in the shapes
// testdata/stream/mcp-control.jsonl recorded: two user-scope servers, one
// connected and one that needs signing in until a reconnect succeeds.
const scriptMCP = "mcp"

func fakeAgentMCP(sid string) int {
	sayText(sid, "ready")
	sayResult(sid)
	signedIn := false
	for line := range agentStdin() {
		var f struct {
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype    string `json:"subtype"`
				ServerName string `json:"serverName"`
			} `json:"request"`
		}
		if json.Unmarshal([]byte(line), &f) != nil {
			continue
		}
		switch f.Request.Subtype {
		case "mcp_status":
			higgs := `{"name":"higgsfield","status":"needs-auth","scope":"user","config":{"type":"http","url":"https://mcp.higgsfield.ai/mcp"}}`
			if signedIn {
				higgs = `{"name":"higgsfield","status":"connected","scope":"user","config":{"type":"http","url":"https://mcp.higgsfield.ai/mcp"},"tools":[{"name":"generate","annotations":{}}]}`
			}
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"mcpServers":[`+
				`{"name":"firecrawl","status":"connected","scope":"user","config":{"type":"stdio","command":"npx","args":["-y","firecrawl-mcp"]},"tools":[{"name":"firecrawl_scrape","annotations":{}}]},%s]}}}`+"\n", f.RequestID, higgs)
		case "mcp_reconnect":
			signedIn = true
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q}}`+"\n", f.RequestID)
		case "mcp_toggle":
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q}}`+"\n", f.RequestID)
		}
	}
	return 0
}

// fakeClaudeMCP is `claude mcp login <server>`: it refuses a stdin that is not a
// terminal, as the real one does, then reads one line and prints it back - so a
// screen test can see the typed keys reached the child rather than Wake.
func fakeClaudeMCP(args []string) int {
	if len(args) < 2 || args[0] != "login" {
		return 2
	}
	fmt.Printf("Starting authentication for %q…\r\n", args[1])
	if !term.IsTerminal(os.Stdin.Fd()) {
		fmt.Println("stdin isn't a terminal, so authentication can't be completed here.")
		return 1
	}
	fmt.Print("Paste the redirect URL here: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return 1
	}
	fmt.Printf("got:%s\r\nAuthentication successful.\r\n", line[:len(line)-1])
	return 0
}

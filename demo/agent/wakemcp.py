"""A minimal MCP client, so the scripted manager really does reach the fleet.

The manager is spawned with `--mcp-config <path>`, and behind that path is
`wake mcp` — a real stdio JSON-RPC server backed by the daemon socket. A fake
manager that only *drew* `send_to_agent` tool calls would be a lie on camera:
the room would show it dispatching work while every roster row stayed idle.
So this speaks the same wire the real manager does, and the fan-out is real.

Only what the demo needs: initialize, then tools/call.
"""

import json
import re
import subprocess

# list_agents names each row by its full session uuid. (`wake status` prints a
# short prefix instead — different surface, different id, and matching the
# wrong one silently finds nobody.)
_ID = re.compile(r"^[0-9a-f]{8}-[0-9a-f-]+$")


class Server:
    def __init__(self, config_path):
        self.proc = None
        with open(config_path) as fh:
            cfg = json.load(fh)
        servers = cfg.get("mcpServers", {})
        if not servers:
            return
        spec = next(iter(servers.values()))
        self.proc = subprocess.Popen(
            [spec["command"], *spec.get("args", [])],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            bufsize=1,
        )
        self._id = 0
        self._rpc(
            "initialize",
            {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "wake-demo", "version": "0"},
            },
        )

    def _rpc(self, method, params):
        if not self.proc:
            return None
        self._id += 1
        self.proc.stdin.write(
            json.dumps(
                {
                    "jsonrpc": "2.0",
                    "id": self._id,
                    "method": method,
                    "params": params,
                }
            )
            + "\n"
        )
        self.proc.stdin.flush()
        while True:
            line = self.proc.stdout.readline()
            if not line:
                return None
            try:
                msg = json.loads(line)
            except ValueError:
                continue
            if msg.get("id") == self._id:
                return msg.get("result")

    def call(self, tool, args=None):
        """Returns the tool's text, which is what a model would read."""
        res = self._rpc("tools/call", {"name": tool, "arguments": args or {}})
        if not res:
            return ""
        return "".join(
            c.get("text", "") for c in res.get("content", []) if isinstance(c, dict)
        )

    def close(self):
        if self.proc:
            try:
                self.proc.stdin.close()
                self.proc.wait(timeout=2)
            except Exception:
                self.proc.kill()


def agents_matching(listing, needle, exclude=()):
    """Pick ids out of list_agents' text by what each agent is working on.

    The listing is one agent per line beginning with its id. Matching on the
    rest of the line is what "the agents working on the api" means — the
    manager has no other handle on it.
    """
    out = []
    for line in listing.splitlines():
        parts = line.split()
        # The id column is short hex, not a dashed uuid — and the listing is
        # framed with a prose note, so every line that does not start with an
        # id is one to skip.
        if len(parts) < 2 or not _ID.match(parts[0]):
            continue
        rest = line[len(parts[0]) :]
        if needle.lower() in rest.lower() and not any(x in rest for x in exclude):
            out.append((parts[0], parts[1]))
    return out

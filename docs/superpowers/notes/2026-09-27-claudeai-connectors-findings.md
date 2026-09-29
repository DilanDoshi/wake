# claude.ai connectors in a headless session — findings, 2026-09-27

Probed against Claude Code 2.1.281, headless (`--print --input-format stream-json
--output-format stream-json --verbose --permission-prompt-tool stdio`), no model turn except one
Haiku `Reply with just: ok` used to compare frame sets.

## What loads them

- **Without an `initialize` control request a headless session loads no claude.ai connector** —
  not before a turn, not after one, not with `ENABLE_CLAUDEAI_MCP_SERVERS=true`. That was the
  2026-09-23 finding, and the one thing it had not tried is the handshake every Agent SDK host opens
  a session with.
- **With `{"subtype":"initialize"}` as the first control request, every connector the account has
  appears** in `mcp_status`, `scope: "claudeai"`, `config.type: "claudeai-proxy"`, named
  `claude.ai <service>`. `testdata/input/initialize.stdin.jsonl` is the request (sterile HOME);
  `testdata/stream/initialize.jsonl` its reply.
- **The handshake changes nothing else about a turn**: one Haiku turn produced the same frame kinds
  with and without it, plus the handshake's own `control_response`. The reply is an environment dump
  (commands, agents, models, account, flags) — `scripts/scrub-fixtures.py` strips its `commands` and
  `agents`, and `corpus_test.go` refuses one that still carries them.
- **`--strict-mcp-config` still excludes them**: with it and an empty `--mcp-config`, a session that
  sent the handshake lists no servers at all, so the manager does not reach the operator's connectors.

## What connects them

- **A loaded connector reads `needs-auth`, even one signed in on claude.ai** (where `claude mcp list`
  reports it connected), and stays so — still `needs-auth` after sixty seconds.
- **One `mcp_reconnect` connects a signed-in connector** (Drive: `needs-auth` → `connected`, 8 tools)
  and **refuses one that is not**, at once, `Server status: needs-auth` (Slack).
  `testdata/stream/mcp-connectors.jsonl` holds both.

## Telling a connector by its name

- `mcp_status` carries a server's `scope`; `init`'s `mcp_servers` carries only its name and status.
- `claude mcp add "claude.ai Fake" …` is refused (names may hold only letters, numbers, hyphens and
  underscores), but the same name through `--mcp-config` loads, with scope `dynamic` — so only a
  hand-written config can give an ordinary server a connector's name.

## The fixture is a scrubbed subset, and why

A sterile HOME has no claude.ai login, so it has no connectors: `mcp-connectors.jsonl` was recorded
under the owner's login and then cut down by hand rather than by the scrubber alone:

- only the `control_response` frames of the status and reconnect asks are kept — the handshake's
  reply is dropped (its sterile twin is `initialize.jsonl`), as are the hook frames;
- only `scope: "claudeai"` rows are kept — the owner's own configured servers are dropped;
- each connector's `config.id` (`mcpsrv_…`, per account) is replaced by one same-length placeholder.
  The proxy `url` is a generic `api.anthropic.com` endpoint with no account part and is kept.

Then `scripts/scrub-fixtures.py` ran over it as over every fixture.

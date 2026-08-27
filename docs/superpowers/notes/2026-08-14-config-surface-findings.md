# What Claude Code's settings commands do in stream-json

**Recorded 2026-08-14 against Claude Code 2.1.232.** Fixtures:
`testdata/stream/bare-mcp.jsonl`, `testdata/stream/bare-config.jsonl`.

Recorded because the banner's MCP row was written on an *inference* — that `/mcp` would be inert
headless, because `/effort` and `/model` are (2026-08-13 bare-command findings). The inference was
wrong in an interesting direction, and the question behind it turned out to be much larger than one
row: **how much of Claude Code's configuration is reachable from a headless session at all.**

The invocation is the one that file used, with the binary named explicitly — `claude` on PATH here is
a cmux shim a non-interactive shell cannot see, and a shim is not what a Wake agent runs:

```sh
printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"/mcp"}]}}' \
  | ~/.local/share/claude/versions/2.1.232 \
      --print --input-format stream-json --output-format stream-json --verbose
```

## §1 The results

| command | `num_turns` | cost | what comes back |
|---|---|---|---|
| `/config` | 0 | $0 | **the whole settings surface, and it is a setter** |
| `/mcp` | 0 | $0 | a server summary, read-only |
| `/context` | 0 | $0 | the full context report, per category and per MCP tool |
| `/usage` | 0 | $0 | subscription, session and weekly limits |
| `/list-agents` | 0 | $0 | the other Claude sessions on this machine |
| `/recap` | 0 | $0 | a recap, or "nothing to recap yet" |
| `/agents` | 0 | $0 | *"The /agents wizard has been removed"* |
| `/insights` | **1** | **$2.62** | a generated report — **this one costs real money** |

**`num_turns=0` at zero cost is the load-bearing half**, exactly as it was for the bare forms: the
command was handled by the CLI without a model turn. So these are not merely harmless from a fleet
of thirty sessions, they are free.

`/insights` is the exception and is the reason this table exists rather than a rule of thumb. It
spends a turn and $2.62 of somebody's money. **Nothing may invoke a slash command speculatively.**

## §2 `/config` is a settings surface, and it already works through Wake

```
Usage: /config key=value [key=value ...]
```

Thirty-six settable keys, recorded in `bare-config.jsonl`. Among them: `model`, `permissionMode`,
`outputStyle`, `thinking`, `autoCompact`, `checkpoints`, `editor`, `theme`, `verbose`,
`workflowSizeGuideline`, `worktreeBaseRef`.

**Wake needs no feature for this.** `internal/ui/slash.go` resolves a closed set Wake owns and passes
everything else to the agent byte for byte, and `config` is not in that set — so `/config
model=opus` typed into a conversation already reaches claude and already works. What was missing was
not a mechanism but the knowledge that the mechanism was there.

That is worth stating plainly because the opposite conclusion was available and wrong: the obvious
reading of "Wake should be able to do Claude Code's settings" is that Wake needs a settings feature.
It does not. It needs its passthrough to keep working, which is a rule this project already treats
as load-bearing.

## §3 `/mcp` works and cannot fix anything, which is why the banner does not name it

```
6 MCP server(s): 2 connected, 2 connecting, 2 not connected, 0 disabled.
Use `/mcp` in the terminal for details.
```

Two things follow, and they point opposite ways:

- the earlier inference was **wrong** — `/mcp` is not inert, it answers;
- the banner's decision not to say `· run /mcp` was **right anyway**, and now for a recorded reason
  instead of an inferred one. The command is read-only from here and says so itself: authenticating
  needs a terminal. Advertising it would send somebody to a command that reports the same counts the
  banner is already showing them and then tells them to go somewhere else.

**Its status vocabulary is not the init frame's.** This summary says *connected / connecting / not
connected / disabled*; `init.mcp_servers` says *connected / pending / needs-auth*. Six servers appear
in both. Nothing should assume the two sets map onto each other — the banner counts `needs-auth` off
the init frame, which is the one Wake decodes.

## §4 What was recorded and not committed

`/usage`, `/context`, `/list-agents`, `/recap` and `/insights` were recorded and their fixtures are
**deliberately absent**: they carry account telemetry — subscription percentages, request counts, the
operator's own MCP tool inventory and the paths of other live sessions. The table above is the
finding; the bytes are not worth publishing to establish it. `/mcp` and `/config` are committed
because their output is about the *product* rather than the account, and because the two claims
anything downstream rests on are theirs.

## §5 What this licenses

- The banner may keep stating the MCP fact without naming a command. §3.
- `/config key=value` may be documented as working from a conversation. §2, and it needs no code.
- Nothing may be invoked speculatively, and a survey like this one is run deliberately and read
  once. §1, and the $2.62 is why.

# Bounding what a session can reach — what `--allowed-tools` actually does

Recorded 2026-08-12 against **Claude Code 2.1.228**, macOS 15 (arm64), model `claude-sonnet-5`.

This is the recording spike Phase 4 item 2·0 asks for: *"what the flag accepts, whether MCP tools
are nameable in it, what `init.tools` reports back, what a refused call looks like."* All of it was
unrecorded.

**The headline is that the fix 2·0 proposes does not work.** `--allowed-tools` is a permission
allow-list, not a tool bound, and in `--permission-mode auto` — which is the mode the manager runs
in — it is a **complete no-op**. Adding it to `argv.go` would leave `Bash`, `Write`, `Edit` and
`Task` exactly where they are while `CLAUDE.md`, `managerScope` and the scope doc all said the
bound existed. That is the same class of failure 2·0 was written to close, arriving through the fix
for it. §3 is the evidence.

**What does work is `--tools`**, a third flag neither 2·0 nor `CLAUDE.md`'s CLI table mentions. With
`--tools ""` the session's tool list is **exactly the MCP tools and nothing else** — every built-in
gone, `--mcp-config` untouched. §4.

Every **wire shape** here was observed on the real stdout of a real `claude` process. Two classes of
claim are not stdout observations and are labelled inline where they appear: text quoted from
`claude --help` at 2.1.228, and one string observed in Claude Code (§8). Anything neither observed
nor backed by a committed fixture is in §10.

Fixtures, all in `testdata/stream/`:

| File | Lines | Flags under test | What it proves |
|---|---|---|---|
| `tools-baseline.jsonl` | 12 | none (control) | `init.tools` with nothing bounded: 35, including both MCP tools |
| `tools-allowed.jsonl` | 43 | `--allowed-tools` in `auto` | **the no-op**: 35 tools, and `Write` runs while off the list |
| `tools-allowed-manual.jsonl` | 38 | `--allowed-tools` in `manual` | what the flag is actually for: a listed tool skips the ask, an unlisted one raises it |
| `tools-restricted.jsonl` | 33 | `--tools "Read,mcp__…"` | built-ins bounded to 1; **the unnamed MCP tool survives anyway** |
| `tools-none.jsonl` | 26 | `--tools ""` | 2 tools, both MCP, zero built-ins — the shape Wake wants |
| `tools-disallowed.jsonl` | 48 | `--disallowed-tools` | removes an MCP tool by name; and the model offers `Bash` as the way round it |
| `tools-unknown-name.jsonl` | 12 | `--tools "Read,NoSuchTool,…"` | an unknown name is **silently ignored**: exit 0, empty stderr |

`tools-allowed.jsonl` and `tools-allowed-manual.jsonl` are an A/B pair: same flag, same tool asked
for, differing only in `--permission-mode`.

---

## 1. The three flags, and that there are three

From `claude --help` at 2.1.228, verbatim (**help text, not a stdout observation**):

```
  --allowedTools, --allowed-tools <tools...>
      Comma or space-separated list of tool names to allow (e.g. "Bash(git *)
      Edit")

  --disallowedTools, --disallowed-tools <tools...>
      Comma or space-separated list of tool names to deny (e.g. "Bash(git *)
      Edit")

  --tools <tools...>                    Specify the list of available tools from
                                        the built-in set. Use "" to disable all
                                        tools, "default" to use all tools, or
                                        specify tool names (e.g.
                                        "Bash,Edit,Read").
```

Three flags, and they are not three spellings of one idea. The first two are **permission** verbs —
they decide whether a call is *approved*. The third is a **membership** verb — it decides whether
the tool exists for this session at all. `CLAUDE.md`'s CLI table has none of them and Phase 4 2·0
names only the first, which is the one that cannot do the job.

So the answer to *"what does the flag accept"* is: a comma **or** space separated list, with an
optional argument pattern in parentheses (`Bash(git *)`). Wake passes exactly one string, so the
comma form is the one that matters. §8 is what happens to a name that is not real.

## 2. The control: what an unbounded session holds

`tools-baseline.jsonl:5`, spawned with `--mcp-config` + `--strict-mcp-config` and no tool flag —
the `tools` value in full, wrapped for width and otherwise unaltered:

```
"tools":["Task","AskUserQuestion","Bash","CronCreate","CronDelete","CronList","DesignSync","Edit",
"EnterPlanMode","EnterWorktree","ExitPlanMode","ExitWorktree","ListAgents","Monitor","NotebookEdit",
"PushNotification","Read","RemoteTrigger","ReportFindings","ScheduleWakeup","SendMessage","Skill",
"TaskCreate","TaskGet","TaskList","TaskOutput","TaskStop","TaskUpdate","ToolSearch","WebFetch",
"WebSearch","Workflow","Write","mcp__spikefleet__list_agents","mcp__spikefleet__send_to_agent"]
```

35 tools: 33 built-ins and the two from the spike's MCP server. `Bash`, `Write`, `Edit` and `Task`
are all there, which is Phase 4 2·0's whole complaint, now with a byte behind it.

Two things to read off this line rather than infer:

- **An MCP tool appears in `init.tools` under `mcp__<server>__<tool>`.** That is the name every
  flag below has to be given, and it is the name `internal/mcp`'s tools would carry.
- `mcp_servers` is `[{"name":"spikefleet","status":"connected"}]` — a separate field, and it says
  the server connected rather than which of its tools survived a filter. It is **not** the
  observable for a bound.

## 3. `--allowed-tools` does not bound anything, and in `auto` it does nothing at all

`tools-allowed.jsonl` was spawned `--allowed-tools "Read,mcp__spikefleet__list_agents"` with
`--permission-mode auto`. It was then asked to write a file with `Write`, which is not on that list.

- `:5` and `:30` — `init.tools` is **35**. Identical to the control. `Write` and `Bash` are both
  still in it.
- `:9` — the model calls `Write`.
- `:23` — `tool_result`: `"File created successfully at: …/work/spike-b.txt"`.
- The file was on disk afterwards.
- **Zero `can_use_tool` frames in the whole recording.**

It then called `mcp__spikefleet__send_to_agent`, also not on the list, and that succeeded too
(`:36`, `:37`).

So in `auto`, `--allowed-tools` changes **nothing observable**: not the tool list, not whether a
call is made, not whether it runs, and it raises no frame. A manager spawned this way holds `Bash`
and `Write` exactly as it does today.

**What the flag is for** is the A/B half. `tools-allowed-manual.jsonl` is the same flag —
`--allowed-tools "Read"` — with `--permission-mode manual`:

- `:5`, `:16` — `init.tools` is still **35**. The flag does not remove a tool in *either* mode.
- `:7` — `Read` is called, and **no `can_use_tool` is raised for it**. It is on the list, so it is
  pre-approved.
- `:18` — `Write` is called, and `:23` raises an ordinary `can_use_tool` for it.

That is an auto-approve list. It is exactly the right instrument for *"stop asking me about
`Read`"* and exactly the wrong one for *"this session may not run `Bash`"*. In a mode that never
asks, an auto-approve list has nothing to do.

**This is the finding that matters for Phase 4.** 2·0's proposed shape — `--allowed-tools` as a
third element of the `--mcp-config` append, keyed on the same emptiness test — would compile, ship,
satisfy a static guard that the flags are emitted together, and bound nothing whatsoever. The
project's own standard for this is `identityArgs`' header: a shape that is *accepted and silently
ignored* is worse than one that fails, because it produces a plausible-looking result. This is that
shape at the tool layer.

## 4. `--tools` is the instrument, and `""` is the shape Wake wants

`tools-none.jsonl`, spawned `--tools "" --mcp-config <path> --strict-mcp-config`, `auto`:

- `:5` and `:17` — `init.tools` is `["mcp__spikefleet__list_agents","mcp__spikefleet__send_to_agent"]`.
  **Two tools. Zero built-ins.**
- `:10`, asked to write a file, the model's own words:

  > I don't have a Write tool available in this session — my only tools are `list_agents` and
  > `send_to_agent` from the spikefleet MCP server — so `spike-c.txt` was not created.

- No file was created.
- `:19` — the MCP tool is called and works.

That is precisely the manager Phase 4 2·0 describes wanting: it can operate the fleet through
`internal/mcp` and it cannot touch the machine. And it composes with what `argv.go` already emits —
`--mcp-config` and `--strict-mcp-config` are untouched, and the MCP server still connects.

`tools-restricted.jsonl` is the same flag with a non-empty list,
`--tools "Read,mcp__spikefleet__list_agents"`:

- `:5`, `:16`, `:25` — `init.tools` is `["Read","mcp__spikefleet__list_agents","mcp__spikefleet__send_to_agent"]`.
- `:9`, asked to write: *"I can't create the file: the Write tool isn't available in this session"*.

**Note what is in that list that was not asked for.** `mcp__spikefleet__send_to_agent` was **not**
named and is present anyway — and at `:26` the model called it successfully. So:

> **`--tools` bounds the built-in set only. MCP tools pass through it untouched, named or not.**

Which is what the help text says — *"from the built-in set"* — and it is the sentence somebody will
skim past. Naming an MCP tool there is accepted, does nothing, and is indistinguishable from
working: the tool you named is present, so the flag looks like it took. It only shows up as the
tool you *didn't* name also being present.

For Wake this is benign in the direction that matters — Wake wants **all** of its MCP tools and
none of the built-ins, which is exactly `--tools ""`. It is a trap for any later change that tries
to bound the MCP surface with the same flag.

## 5. `--disallowed-tools` does reach MCP tools — and demonstrates why it is the wrong shape

`tools-disallowed.jsonl`, spawned `--disallowed-tools "mcp__spikefleet__send_to_agent,Write"`,
`auto`:

- `:5` and `:28` — `init.tools` is **33**: both `Write` and `mcp__spikefleet__send_to_agent` are
  gone, and `mcp__spikefleet__list_agents` remains.
- `:22` — *"The spikefleet MCP server exposes only `list_agents` — there is no `send_to_agent` tool
  available, so nothing was called."*

So a deny-list **does** remove an MCP tool by name, which `--tools` cannot. That is the only thing
it is better at, and it is not worth having, because of what the model said next. Asked to write a
file with `Write` removed, `:43`:

> The Write tool is not available in this session (it isn't in my tool list and `ToolSearch` finds
> no deferred `Write`), so no file was created — **say the word and I'll create `spike-e.txt` with
> Bash instead.**

An unprompted offer to route around the bound with a tool the deny-list did not name. That is the
allow-list-versus-deny-list argument making itself: a deny-list bounds the things somebody thought
of, and `Bash` is a general-purpose escape from any of them. `internal/mcp`'s `liveSessions` was
rewritten from a deny-list to an allow-list for this reason and `stateguard_test.go` holds it
there; the same ruling applies here, and this fixture is the evidence for it.

## 6. What `init.tools` reports, and the one caveat on reading it

`init.tools` is the **effective** list — it reflects `--tools` and `--disallowed-tools`, and does
not reflect `--allowed-tools`. So it is a real observable for a membership bound and no observable
at all for a permission one.

**The caveat: `init` is per-turn, and `init.tools` can grow between turns.** `mode-set.jsonl:5`
reports 84 tools and `:31` reports 164 — the added ones are `ListMcpResourcesTool` and the
machine's own MCP servers' tools (Slack, Linear, …), resolving after the first turn. That is the
existing *"`result` and `system/init` are per-turn, not per-process"* trap arriving on a new field.

In every bounded recording here the count is **identical on every `init` in the file** (3/3/3,
35/35, 2/2, 33/33), because those sessions had one MCP server and it connected before the first
turn. A checker that reads `init.tools` once and treats it as the session's tool set is right for
those and wrong in general.

## 7. There is no refusal frame, and that is the answer to the fourth question

2·0 asks *"what a refused call looks like on the wire."* **It does not look like anything.**

Across `tools-restricted`, `tools-none` and `tools-disallowed` — three sessions asked to use a tool
that had been bounded away — there is not one `control_request`, not one `is_error` `tool_result`,
not one `system` frame, and nothing on stderr. The tool is simply absent from the model's list, so
the model never calls it and says so in prose instead.

That has a direct consequence for Wake: **a tool bound is not verifiable from the stream.** There
is no frame to assert on and no failure to detect. The only observable is `init.tools`, which says
what the set *is* rather than that something was refused. So the bound has to be asserted where it
is *built* — statically, over the argv — exactly as `TestTheMCPFlagsAreEmittedFromOneAppendOrNotAtAll`
already does for `--mcp-config`/`--strict-mcp-config`. A runtime check is not available at any
price.

The one shape that *does* produce a frame is §3's: `--allowed-tools` in an asking mode, where an
unlisted tool raises an ordinary `can_use_tool` indistinguishable from any other ask.

## 8. An unknown tool name is accepted and silently ignored

`tools-unknown-name.jsonl`, spawned `--tools "Read,NoSuchTool,mcp__nope__missing"`:

- **exit 0, empty stderr.** No warning anywhere.
- `:5` — `init.tools` is `["Read","mcp__spikefleet__list_agents","mcp__spikefleet__send_to_agent"]`.
  Both bogus names are gone without comment.

So the CLI does not validate the list. For `--tools` the failure direction is safe — a typo removes
a tool rather than granting one — but it is silent in both directions, and *"the flag is spelled
correctly"* is therefore not something the CLI will ever tell Wake. If `internal/daemon/manager.go`
grows a tool list, the spelling of every entry is Wake's own to check, and the check has to be
against a list Wake maintains rather than against anything the process reports.

This is the same failure shape as `--fork-session` with no `--resume` (`argv.go`'s header): exit 0,
empty stderr, a plausible-looking session.

## 9. Incidental: `--strict-mcp-config` corroborated from the other side

Not what this spike was for, and it fell out of it. The `mode-*` recordings pass **no**
`--mcp-config` at all. Their first `init.tools` holds **84** tools against the bounded recordings'
35, and the extra ones are this machine's own MCP servers — `mcp__claude_ai_Slack__*`,
`mcp__linear-server__*`, `mcp__firecrawl__*`, `mcp__playwright__*`.

`CLAUDE.md` says a manager without `--strict-mcp-config` *"inherits every MCP server in the user's
own configuration — on the machine this was written on that is Slack, Linear, firecrawl and
playwright"*. That was reasoning from the flag's documentation. It is now a recorded count on this
machine, and the named four are the named four.

## 10. What this does **not** settle

Nothing below has a byte behind it. Per this project's rule, none of it may be designed against.

1. **Whether `--tools ""` survives a `--resume`.** Every recording here is a fresh session. The
   mode spike found that a *mid-session* mode change does not survive a wake
   (`2026-08-12-permission-mode-findings.md` §6); a spawn **flag** is a different mechanism and is
   re-passed by `buildArgs` on every launch, so there is reason to expect it holds — and that is an
   expectation, not a recording. **A manager that came back from `⌃Q` unbounded is the failure this
   would produce**, and it is the one thing worth recording before 2·0 ships.
2. **Whether the argument-pattern form (`Bash(git *)`) works, and in which flag.** Only the bare
   name form was recorded. The help text shows the pattern for `--allowed-tools`/`--disallowed-tools`
   and not for `--tools`.
3. **`--tools "default"`.** Documented, never run here.
4. **Interaction between the three flags.** No recording passes more than one. In particular
   `--tools ""` plus `--allowed-tools` is untested, and so is whether `--disallowed-tools` can
   remove something `--tools` already excluded.
5. **Whether a bounded session can reach a tool through `Skill`, `Task` or `ToolSearch`.** `--tools ""`
   removed all three along with everything else, so the question never arose; a *non-empty* `--tools`
   that keeps `Task` may or may not hand a subagent the full set. This is the one that would make a
   partial bound worthless, and it is unrecorded.
6. **`init.tools` before the first turn.** The field is only ever read here off an `init` that
   already has a turn behind it.
7. **What a bounded *subagent* reports.** No recording uses `Task`.

## 11. What this means for Phase 4 item 2·0

Stated as a recommendation, not a decision — the item belongs to the owner.

- **`--allowed-tools` should be struck from 2·0.** It cannot close the gap, and shipping it would
  re-assert a bound that does not exist, which is the specific failure 2·0 exists to fix.
- **The flag is `--tools`, and the value is `""`.** It is a one-literal append keyed on the same
  `Config.MCPConfig != ""` emptiness test 2·0 already proposes, so the shape of the fix survives
  intact — only the flag changes. No value of that field could then express
  MCP-tools-without-a-bound, exactly as none can express `--mcp-config` without
  `--strict-mcp-config`.
- **It has to be asserted statically** (§7). There is no runtime evidence a bound is in force, so
  the guard is over `argv.go`, beside the one that already holds the MCP pair.
- **`CLAUDE.md`'s CLI table needs all three flags**, with `--tools` marked as the membership one.
  The table currently names none, and the next reader will reach for `--allowed-tools` for the same
  reason 2·0 did.
- **Item 1 of §10 should be recorded before the implementation lands**, because a manager that
  loses its bound on a wake fails silently and looks correct.

## 12. Method

Recorded with a throwaway Go driver in a scratch directory outside the repo (deleted with the
scratch; it is not committed), following the method in
`2026-08-09-resume-fork-findings.md` §11. It spawns
`/Users/dev/.local/share/claude/versions/2.1.228` directly — bypassing the cmux shim first on
`PATH`, as §1 of the stream-json note requires — tees stdout to the fixture byte-for-byte through an
`io.TeeReader`, auto-allows any `can_use_tool` by echoing `request.input` back inside the `allow`
frame `encode.go` writes, and drives scripted turns off `result` frames. It scrubs the same eight
`nestedSessionEnv` variables `internal/core/process.go` scrubs, and its argv is a transcription of
`buildArgs` rather than a hand-written list, with each recording's flags appended where `buildArgs`
appends `--mcp-config`.

The MCP server behind `mcp__spikefleet__*` is a ~100-line stdio JSON-RPC server in the same scratch
directory, speaking the shapes `internal/mcp/server.go` speaks (protocol `2025-06-18`, a `tools`
capability, `tools/list`, `tools/call`). It exposes **two** tools on purpose: a bound is only
observable if one thing can be named and another left out. It is not `wake mcp`, which would need a
daemon and could reach a real fleet.

`cwd` for every run was a scratch directory holding three trivial files (`a.txt`, `b.txt`,
`notes.md`) plus whatever the recordings created, so a `Write` had somewhere real to land and the
fixtures carry no Wake source.

Fixture integrity, checked over all 212 lines of this spike's seven files before commit: every line
parses as JSON; every `uuid` present is RFC 4122 **version 4**; exactly **one** distinct
`session_id` per file; seven distinct pids in `init.messaging_socket_path`, confirming seven
separate processes; `init.apiKeySource` is `"none"` on every one; and no credential-shaped strings
(`sk-ant-`, `ghp_`, `AKIA`, `xox…`, bearer tokens) appear anywhere. The fixtures do contain this
machine's absolute paths and the user's installed skill/plugin/MCP inventory, which is expected for
a recording and matches every fixture already in the corpus.

Which scenario produced which fixture:

| Fixture | Tool flags | Mode | Turns |
|---|---|---|---|
| `tools-baseline` | none | `manual` | reply `READY` |
| `tools-restricted` | `--tools "Read,mcp__spikefleet__list_agents"` | `auto` | `Write`; call `list_agents`; call `send_to_agent` |
| `tools-allowed` | `--allowed-tools "Read,mcp__spikefleet__list_agents"` | `auto` | `Write`; call `send_to_agent` |
| `tools-allowed-manual` | `--allowed-tools "Read"` | `manual` | `Read a.txt`; `Write` |
| `tools-none` | `--tools ""` | `auto` | `Write`; call `list_agents` |
| `tools-disallowed` | `--disallowed-tools "mcp__spikefleet__send_to_agent,Write"` | `auto` | call `send_to_agent`; `Write` |
| `tools-unknown-name` | `--tools "Read,NoSuchTool,mcp__nope__missing"` | `auto` | reply `READY` |

All seven also carry `--mcp-config <spike>/mcp.json --strict-mcp-config`.

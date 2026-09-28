# @-menu findings — peers, outside sessions, rename, subagents

Recorded 2026-09-27 against **2.1.283**, with Wake's own argv, to ground the `@`-menu work before building it.
The behaviour of Claude Code's own `@` typeahead is from its public docs (cross-session messaging, subagents).

| Fixture | What it is |
|---|---|
| `testdata/stream/list-agents.jsonl` | `wf-gamma` sends `/list-agents`, `/rename wf-delta`, `/list-agents` with two idle peers (`wf-alpha`, `wf-beta`) |
| `testdata/stream/list-agents-empty.jsonl` | `/list-agents` with no peers |
| `testdata/stream/list-agents-bare.jsonl` | a `--bare --no-session-persistence` one-shot `/list-agents` with two idle peers |
| `testdata/stream/list-agents-bare-empty.jsonl` | the same one-shot with no peers |

## Provenance

Both recorded under a **sterile `HOME`**. That is possible here, unlike a model turn, because every command
recorded is local: it needs no login (`apiKeySource: none`). A sterile-`HOME` session **sees only peers sharing
that `HOME`**, so the listing holds neutral names and `/tmp` paths and nothing of the recording machine.

## 1. `/list-agents` is a local command, and it lists every reachable session

A headless session answers a bare `/list-agents` with **no model turn** — `num_turns: 0`, `total_cost_usd: 0`,
the listing in the `result` text — the same shape as a bare `/model`. The undocumented alias `/peers` answers
the same; only `list-agents` is advertised in `init.slash_commands`. The text is:

- a first line `This session: <name> [<short-id>] (the name other sessions use to message it)`;
- then `Other Claude sessions (<n>):` and one row per session, `[<state>]  ·  <name>  ·  <cwd>  ·  started <age>`,
  with `<state>` recorded as `idle` (and `busy` observed under a real `HOME`);
- or, with nobody else, `No subagents, teammates or other Claude sessions — …`.

Per the docs, the listing is every session that binds an inbox socket on this machine: interactive sessions in
other terminals, background sessions, and `claude -p` sessions (so Wake's own agents list each other), plus
Remote Control and cloud sessions while this session is connected to Remote Control. It is human text, not a
schema — a parser must be tolerant and degrade to "no outside sessions", never to a wrong row.

### 1a. Asking a live agent costs that agent context; a bare one-shot costs nothing

A `/list-agents` sent to a live session **persists** in its transcript: a meta caveat line, the
`<command-name>` user line, and a `system`/`local_command` entry holding the listing (with `commandRun`).
A follow-up model turn in the same session quoted the listing back, so **the listing enters the model's context
on the next turn**. Probing an idle agent would add the whole machine listing to some agent's context on every
menu opening.

`claude --print --bare --no-session-persistence --input-format stream-json --output-format stream-json
--verbose` answers the same bare `/list-agents` in about 0.7s, with `num_turns: 0`, `$0`, no hook frames, no
MCP servers and no transcript. Every stream frame names its command: the assistant frame carries
`local_command_run: {command: "list-agents"}`, and the result carries `local_command: "list_agents"`. A bare
session registers no inbox, so the listing **omits the `This session:` line** and the one-shot is not listed.
The empty form is `No subagents, teammates or other Claude sessions.`
(`testdata/stream/list-agents-bare.jsonl`, `list-agents-bare-empty.jsonl`). Peers register at startup, before
their first turn.

## 2. `/rename` is local and takes effect at once

`/rename wf-delta` replies `Session renamed to: wf-delta` with `num_turns: 0` and `$0`, and the next
`/list-agents` names the session `wf-delta`. So Wake can keep claude's peer name in step with its own `/name`
for free. **`--name` wins on `--resume`**, even over an earlier `/rename`: a session named `wf-one`, resumed with
`--name wf-two`, lists as `wf-two`; renamed to `wf-three` and resumed with `--name wf-four`, it lists as `wf-four`;
resumed with no `--name`, it gets a generated name rather than the stored one (sterile `HOME`, 2.1.283, not
committed as a fixture). So every Wake relaunch starts claude's name in step. Two sessions launched with the same `--name` were not conclusive (the listing showed one); the docs
say a colliding name is renamed to a variant for interactive sessions.

## 3. `init.agents` names the subagent types, and `@agent-<name>` resolves headless

The `init` frame carries `agents` — under a sterile `HOME` only the built-ins (`claude`, `Explore`,
`general-purpose`, `Plan`, `statusline-setup`); under a real `HOME` also the operator's own, which is why the
scrubber strips the key. Typed as plain text in a headless session, `@agent-general-purpose <task>` produced an
`Agent` tool call with `subagent_type: "general-purpose"` and a `local_agent` task — the same effect as Claude
Code's typeahead (which inserts `@"<name> (agent)"`; the docs say the typed `@agent-<name>` form resolves on
submit). That run was under the real `HOME` and is observed here, not committed.

## 4. How a peer mention is delivered (docs)

In Claude Code, picking a session from the `@` typeahead inserts `@<name>`, and Claude writes the message and
sends it with `SendMessage` — the operator's text is not delivered verbatim. Every Wake agent has `SendMessage`
and `ListAgents` in `init.tools`. Typed as plain text, Claude finds the target itself (a `ListAgents` call if it
needs one). The receiver's side reaches Wake's room as a cross-session envelope (`↪ sender → recipient`).

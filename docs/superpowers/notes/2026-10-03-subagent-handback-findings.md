# A subagent's hand-back, and the lines claude injects as `isMeta`

**Probed 2026-10-03 against Claude Code 2.1.288**, headless, with Wake's replay flags, under the
operator's login (credentials live in the keychain) in a scratch project directory. Three probes;
only the one in `--permission-mode auto` produced a hand-back. The committed fixtures are minimal and
sterile, authored from the observed shapes (the cross-session findings' precedent):
`testdata/stream/subagent-handback.jsonl`, `testdata/transcript/subagent-handback.jsonl`,
`testdata/transcript/injected-meta.jsonl`.

## 1. What a hand-back is

Since 2.1.271 claude gives a subagent the `SubagentHandback` tool **in auto mode only**
(tools-reference: "Provided only in auto mode, to subagents that the Agent tool runs locally other
than forks"). Wake spawns every agent in `auto`, so its agents' subagents have it. A subagent that
reports through it reaches its parent as a user line whose string content is a one-line preamble, an
`<agent-message from="<agent id>">…</agent-message>` envelope holding the report under a
`[Subagent hand-back]` header, and harness guidance after it. Claude enqueues the old
`<task-notification>` beside it; the probe's transcript holds both.

A subagent that ends without calling the tool still ends as a `<task-notification>`, so which of
the two a parent receives is the subagent model's choice: on this machine, since 2.1.277, 579
background-agent endings arrived as hand-backs and 273 as notifications, with no split by subagent
type, model or foreground/background. The same envelope without the `[Subagent hand-back]` header
carries a subagent's `SendMessage` to its parent.

## 2. Where it lands

- **Live:** replayed on stdout as a `user` frame with `isReplay` and `isSynthetic` (both, like a
  cross-session message) and an `origin` object (`kind:"peer"`, `handback:true`, `senderTaskId`,
  `name`). The turn it starts ends in a `result` carrying the same `origin`.
- **On disk:** the same line with `isMeta:true`, `promptSource:"system"`, `origin.kind:"peer"`, and no
  replay flags.

Before this fix, live it folded to an Echoed user turn, which the DM and the room drop. On disk it
decoded as the operator's own turn: `› you` in a reopened conversation, a rewind target, and a
recallable prompt. `origin.kind` cannot tell it apart, because a real cross-session message is
`peer` too; the envelope can. Core now drops it on both wires: live by `isAgentMessage` on a frame
claude marks `isSynthetic`, on disk by the `isMeta` fail-safe below. A typed turn quoting the
envelope carries neither, and stays typed (a hand-started claude writes typed turns as strings).

## 3. `isMeta` is claude's mark on a line it injected

An audit of every `user` line in this machine's headless transcripts found the hand-back was one of
several injected kinds the decoder returned as the operator's turn, all `isMeta:true`: a skill's
body (`Base directory for this skill: …`, array content), image notes (`[Image: …]`), `/loop`'s
skill text, Stop-hook notices, `[Cross-session idle notice]` and continuation nudges. None of the
kinds Wake decodes on purpose is unclaimed `isMeta`. The compaction summary, the interrupt marker,
`<local-command-stdout>` and `!` lines carry no `isMeta`, and a cross-session message, which does,
is claimed by `crossSession` first. So `decodeTranscript` drops every plain user turn from an
`isMeta` line. The ruling is in `docs/notes/decisions.md` (2026-10-03).

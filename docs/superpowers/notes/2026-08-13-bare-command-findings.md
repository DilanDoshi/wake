# What the bare `/effort` and `/model` do in stream-json

**Recorded 2026-08-13 against Claude Code 2.1.232.** Fixtures:
`testdata/stream/bare-effort.jsonl`, `testdata/stream/bare-model.jsonl`.

Recorded because `internal/ui/slash.go`'s fence may only admit a word claude advertises **in a form
claude is recorded doing nothing with**, and a claim that a form is inert is not one this project
takes on trust. Both words are in the `init.slash_commands` corpus.

## The invocation

From a scratch directory, one turn each, with the line shape `core.EncodeUserMessage` emits:

```sh
printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"/effort"}]}}' \
  | claude --print --input-format stream-json --output-format stream-json --verbose \
  > bare-effort.jsonl
```

## §1 Both bare forms are inert, and the receipt says so three ways

Each recording is nine frames: three `hook_started`, three `hook_response`, one `system/init`, one
`assistant`, one `result`. The `result` carries:

```
subtype="success"  is_error=false  num_turns=0  total_cost_usd=0
```

**`num_turns=0` and a zero cost are the load-bearing halves.** They say the command was handled by
the CLI without a model turn at all — so the bare form is not merely harmless, it is not a
conversation with the agent in any sense. Nothing in either recording changes the session: the
`init` frame is the only thing that reports state, it precedes the reply, and no second one follows.

This is the evidence `bareOnlyCommands` names. It settles the fence question for both words.

## §2 `/effort` prints its usage and nothing else

```
Usage: /effort <low|medium|high|xhigh|max|ultracode|auto>
```

Seven levels, against the five `--effort` takes (`claude --help`: "Effort level for the current
session (low, medium, high, xhigh, max)"). That divergence is why `core.EffortCommands` and
`core.EffortLevels` are separate constants and why `daemon.argvEffort` exists.

The comment in `internal/daemon/effort.go` that said this form *"opens claude's own picker, and whose
outcome Wake cannot see"* was **wrong** and is corrected. In stream-json there is no picker to open.

## §3 `/model` prints the list of available models — which nothing else does

```
Current model: Opus 5 (1M context) (effort: xhigh)
Usage: /model <name>. Available: sonnet, opus, haiku, fable, best, sonnet[1m], opus[1m], fable[1m],
opusplan, default, or a full model ID.
```

**This falsifies the design's §6.1.** The spec ruled that no source could enumerate the models — the
`init` frame names the one in use and no others, and `--help` gives only an `e.g.` — so the picker
would offer a hand-picked sample of aliases. It does not have to. The bare command's own reply is
the enumeration, it is claude's own list, and it is now a recording this repository can assert
against, exactly as the palette is.

So `core.ModelAliases` is derived from **this fixture** rather than from `--help`. The `--help`
examples remain a second, weaker source and are still checked as a subset, which is what would catch
one list moving without the other.

The typed escape survives the change and is claude's own wording for it: *"or a full model ID"*. The
list is a snapshot of one binary on one machine, so a model shipped tomorrow must still be reachable
without a Wake release.

## §4 A session's effort is observable after all — by asking

`(effort: xhigh)` in the reply above is the first known way to read a session's reasoning level back.
`docs/notes/deferred.md` has said, correctly until today, that *"effort is on no frame Wake receives
at all"*, and `internal/daemon/effort.go` is built on it: the daemon watches the command go past
because nothing ever reports the result.

That premise is now narrower than it looked. Effort is on no frame Wake receives **unasked** — but a
bare `/model` returns it, and §1 says that costs `num_turns=0` and `$0`.

**Nothing in this branch acts on it, deliberately.** Wake claims the bare `/model` for its own
picker, so it never sends one and never sees this reply; and reading a level out of English prose an
assistant wrote is a parser over model output, which is a different class of thing from reading a
JSON field. But the sentence "effort can never be confirmed" is no longer true as an absolute, and
the design should stop resting on it as one. `deferred.md` carries the costed entry.

## §5 What was not established

- Whether the model list is stable across accounts, subscription tiers, or `--settings`. One machine,
  one account. The list is a snapshot and the typed escape is what makes an out-of-date one harmless.
- Whether `/effort` bare behaves the same in an *interactive* claude. Irrelevant to Wake — every
  agent is headless — but the difference is the whole reason this note exists.
- What a bare `/model` does when the session was started with `--model`. Not recorded; the fixture's
  session took the machine's default (`claude-opus-5[1m]`).

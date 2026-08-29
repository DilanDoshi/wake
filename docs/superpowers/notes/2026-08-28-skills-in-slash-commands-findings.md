# Skills ride in `init.slash_commands`, and the `skills` array is not the one to read

**2026-08-28, claude 2.1.251, macOS.** One probe, to answer a single question: when an operator has
Claude Code **skills** configured, how does a headless session advertise them — in the `skills`
array, in `slash_commands`, or both? The completion menu offers `slash_commands` and decodes nothing
else, so the answer decides whether skills can appear in it at all.

No frames are pasted below — the init frame is an environment dump. Field names, counts, and one
injected name are all that is cited.

## The probe

A throwaway `HOME` holding a single dummy skill and nothing else:

```sh
D=$(mktemp -d)
mkdir -p "$D/.claude/skills/demo-skill"
printf -- '---\nname: demo-skill\ndescription: demo\n---\nBody.\n' > "$D/.claude/skills/demo-skill/SKILL.md"
printf '%s\n' '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"/model"}]}}' \
  | HOME=$D claude --print --input-format stream-json --output-format stream-json --verbose
```

A bare `/model` rather than a real prompt: the CLI answers it without a model turn, so the `result`
frame reports `num_turns: 0` and the probe cost $0 — the same trick `bare-model.jsonl` records. The
init frame is emitted before the turn is handled, which is the frame under study.

**Caveat on sterility.** This machine's `claude` is reached through a cmux wrapper that injects its
own skill set regardless of `HOME`, so the probe's `slash_commands` and `skills` both carried names
that are not `demo-skill`. That is why no fixture is committed here: a clean capture needs the wrapper
out of the loop. The overlap finding below does not depend on it — it is read off `demo-skill`, the
one name this probe put there itself.

## What the init frame carries

The complete top-level key set matched the 2026-08-08 recording (`stream-json-findings.md`), with
`skills` present: `agents`, `mcp_servers`, `memory_paths`, `model`, `permissionMode`, `plugins`,
`skills`, `slash_commands`, `terminal_slash_commands`, `tools`, and the rest.

Two arrays matter:

- **`skills`** — an array of plain strings, one per skill name. 17 entries in the probe, `demo-skill`
  among them.
- **`slash_commands`** — the array Wake already decodes (`wire.go` → `SessionFacts.SlashCommands`).
  45 entries, `demo-skill` **also among them**.

`demo-skill` appears in **both**. And every one of the 17 `skills` entries was also in the 45
`slash_commands` — the `skills` array was a **subset**. So `slash_commands` is the superset: it holds
the operator's `.claude/commands`, and their skills, in one list.

## The two conclusions Wake takes from this

1. **The completion menu already has the skills.** They are in `slash_commands`, which is decoded end
   to end. Surfacing them was never a wire problem — it was the menu ordering, where Wake's own twelve
   verbs filled the bound before any advertised name (fixed 2026-08-28; see `decisions.md` and
   `internal/ui/completion.go`'s `commandMenu`).

2. **Do not decode the `skills` array into the menu.** It is redundant with `slash_commands` today,
   and reading it instead of (or as well as) `slash_commands` is a hazard rather than a safeguard:
   `slash_commands` is the list of what a leading `/` *invokes*, while the `skills` array is a catalog
   that can in principle include model-invoked-only skills. Offering one of those as a `/command`
   would be a completion that resolves to a message the CLI does nothing with — the lying feature the
   legend and menu rules exist to prevent. Trust the `/`-invocable list.

## What is not verified, and what would falsify the design

One capture, one version, and through a wrapper. If a later Claude moves user-invocable skills *out*
of `slash_commands` and into the `skills` array alone, the completion menu would stop offering them
with the whole suite still green — decoding `slash_commands` would simply not see them. Re-run the
probe above (ideally with the wrapper bypassed, for a committable fixture) before assuming skills are
still in `slash_commands`, the same way every CLI-surface claim in this project is re-verified rather
than trusted across versions.

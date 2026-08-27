# Image input over stream-json — findings

**Recorded 2026-08-15 against `claude` 2.1.233.** One live turn, `claude-haiku-4-5`, `$0.0302`.
Fixtures: `testdata/input/image-block.stdin.jsonl` (what Wake would write) and
`testdata/stream/image-block.jsonl` (what came back).

The input line lives in `testdata/input/` and not beside the output streams on purpose:
`TestDecodeRecordedFixtures` walks `testdata/stream/*.jsonl` and requires every line to decode, and
an input line is a third kind of artifact - neither an output stream nor the on-disk transcript.

## The question

Wake runs every agent as `--print --input-format stream-json --output-format stream-json`. Claude
Code's TUI pastes an image with `⌃V` (`chat:imagePaste`). Wake has no image path at all —
`internal/ui/clipboard.go` is write-only. Before anyone scopes image paste: **can the transport
carry one?**

## The answer: yes, and it is recorded

The image was a 292×57 PNG containing the words `VELVET OTTER`, chosen so that a correct reply
could not be produced from context. The reply was exactly `VELVET OTTER`, `subtype: "success"`,
`num_turns: 1`, empty stderr, exit 0. The model read pixels.

### What Wake would write

One line on stdin:

```json
{"type":"user","message":{"role":"user","content":[
  {"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo..."}},
  {"type":"text","text":"Reply with exactly the two words written in this image, nothing else."}]}}
```

**`encode.go` already emits `content` as an array** (`outUserMessage.Content []outTextBlock`), so the
delta is a polymorphic block type and nothing else on the write path.

### Three rules

- **Images first, text last.** Claude Code derives the prompt string from `content.at(-1)`, so a
  message whose last block is an image leaves the prompt null and routes differently.
- **Never send an empty `content` array.** With no attachments it is silently dropped.
- **Media types are `image/png`, `image/jpeg`, `image/gif`, `image/webp`**, sniffed from magic
  bytes rather than trusted from the field.

### Do not pre-resize

Claude Code budgets and downscales the image itself — a per-model box (2000×2000, 5MB base64) and a
hard per-image budget after compression. Wake should hand over raw base64 and let it. The budgeting
runs **only** for `source.type: "base64"`; a `url` source passes through unbudgeted.

## What is recorded, and what is not

**Recorded here:** the success path end to end — the envelope above, accepted, with a reply that
proves vision, and the whole output stream as a fixture.

**Worked out against Claude Code and NOT recorded** — the failure shapes. They are the reason this
file exists rather than only the fixture, and the first one is the dangerous one:

| Trigger | What happens |
|---|---|
| **Undecodable or over-large image** | **Silently degrades to a text block** — `[Image could not be processed: …]`. The turn proceeds and ends `success`. Wake sees nothing unusual on stdout. |
| Unparseable JSON line | stderr carries **the whole line**, then exit 1. With a multi-MB base64 line that is megabytes into Wake's stderr reader. |
| `message.role` not `"user"` | stderr `Expected message role 'user'`, exit 1 |
| Malformed block (`{"type":"image"}` with no `source`) | `result` / `error_during_execution`, `is_error: true`, `num_turns: 0`, exit 1 |
| Accumulated images over the request limit | API 413 |

Row one is the trap worth designing around: **a bad image is not an error, it is a text block the
model reads.** There is no frame that says so.

## A finding the fixture produced immediately

Dropping the input line into `testdata/stream/` failed `TestDecodeRecordedFixtures` with
*"decoded as unknown (wire type `image`)"*. That is not only a misplaced file: **`internal/core`'s
decoder has no case for an `image` content block.** It does not matter for a session Wake drives
today, because Wake never writes one - but `DecodeTranscriptLine` reads conversations off claude's
disk, and a session somebody pasted an image into before Wake imported it carries exactly this
block. It decodes as unknown and renders as nothing.

Recorded rather than fixed: it is one arm in the airlock and a rendering decision above it, and
neither belongs in a findings branch.

## `--file` is not this

`--file <file_id>:<path>` downloads a file resource from Anthropic at startup using the OAuth
bearer token. It needs a server-side id that only exists because something was uploaded, and it puts
nothing in a message. It is not the image path.

## Recommendation

The transport question is closed. The remaining work is **not** the wire — it is that
`internal/ui/clipboard.go` is write-only by design (three layers of *copy*), reading image data off
the macOS pasteboard needs more than `pbpaste`, and a transcript has no way to draw an image
placeholder. That is a genuine feature and it is outside spec §17's "in" list, so it is deferred —
but whoever picks it up now inherits a fixture instead of a spike.

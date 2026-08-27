# The streamed answer, recorded — live-testing §15's recording task

**One turn against claude 2.1.238, 2026-08-21.** `testdata/stream/partial-turn.jsonl` (39 frames)
with `testdata/input/partial-turn.stdin.jsonl` as the line Wake would write. The prompt asks for a
~300-word markdown answer so there are content blocks worth streaming. This closes the airlock's
one unrecorded inbound shape: the five `notInTheCorpus` excuses for the token stream are deleted,
and `TestTheVocabularyDescribesTheRecordedCorpus` now holds `stream_event`,
`content_block_delta`, `text_delta`, `message_delta` and `message_start` to bytes.

Cite the fixture and the line, never paste a frame — this note names indices only.

## The four claims §15 asked to check, each confirmed

1. **The envelope is exactly what was read out of the 2.1.233 bundle.** Every `stream_event`
   frame carries `{event, parent_tool_use_id, session_id, type, uuid}`, and `ttft_ms` appears on
   exactly one of the 24 — the `message_start` — which is the bundle's own `ttft_ms?` optionality
   confirmed rather than tightened. `wireStreamEvent`'s inference was right; what was "stronger
   than documentation and weaker than a frame" is now a frame.
2. **A text delta is `event.type == "content_block_delta"` with `event.delta.type ==
   "text_delta"` → `event.delta.text`.** Two other delta types stream beside it —
   `thinking_delta` and `signature_delta`, for the thinking block — and `partialEvent`'s
   yield-no-event fallback is the right behaviour for both: a thinking block is not the answer.
3. **The completed `assistant` frame still arrives, and its text is byte-identical to the
   concatenated deltas** (1,998 chars compared equal). The design's load-bearing claim — the
   partial is a preview the block replaces — holds. Ordering nuance worth knowing: the completed
   frame for each content block arrives after *that block's* deltas but **before** the turn's
   `message_delta`/`message_stop`, so a fold keyed on "assistant frame ends the stream" would be
   wrong; Wake keys on nothing of the sort.
4. **`message_delta.usage.output_tokens` exists and reconciles**: the one `message_delta` says
   788 and the `result` frame's own `output_tokens` is 788. The sum-of-newest-per-message fold is
   right for the single-message case. `usage` also carries keys the airlock does not read
   (`output_tokens_details.thinking_tokens`, `iterations`); none are decoded and none need to be.

## One genuine first — and a correction this note shipped with and takes back

The first cut of this section called `rate_limit_event` and `system/thinking_tokens` shapes "the
corpus had never seen", and both claims were false — the review pass counted ~58 and 44 existing
fixtures carrying them, and both *decode*: `rate_limit_event` to `KindRateLimit` (drawn by
`dm_blocks.go`), `thinking_tokens` to `KindSystem`. The lesson is this project's oldest: check the
corpus before calling anything new.

What this recording genuinely carries first: **`rate_limit_info.status: "allowed_warning"`** — the
sole non-`"allowed"` status in the corpus. Two consequences, neither acted on here:
`vocabulary.go`'s comment that `allowed` is "the only status ever recorded" is now stale (fixed in
this change), and `rateLimitNotice` maps `allowed_warning` to `NoticeRateLimited` — a *warning*
drawn as *rate-limited*, a ruling made when no such status existed and now testable against real
bytes. Whether a warning deserves a softer sentence is a product call left open.

## Provenance caveats

- **Recorded under the real `HOME`, then scrubbed** — the sterile-HOME procedure could not
  authenticate (credentials live in the macOS keychain; extracting them into a throwaway HOME was
  not acceptable), so this recording leaned on `scripts/scrub-fixtures.py` instead, which passes
  `--check` over the result. Consequence: the fixture carries six `hook_started`/`hook_response`
  frames — all `SessionStart:startup`, preceding the turn — where sterile recordings have none.
  They decode (Wake passes `--include-hook-events`) and are left in deliberately: a machine with
  hooks is a machine Wake runs on.
- **2.1.238**, newer than the corpus's 2.1.226–2.1.232 range; the init frame names it.
- One turn, one message, no tool calls here. The multi-message case (a turn with tool use between
  messages, where the sum-of-newest fold earns its keep) **was recorded the same day** —
  `debug-runtime.jsonl`, three messages summing exactly to the result's figure; see
  `2026-08-21-runtime-command-findings.md` §6. §15's remaining unchecked boxes — watching the
  number climb on screen, the interrupt mid-answer, fifteen agents at once — are judgment and load
  items only a human at a terminal can do, and stay on the list.

# Multi-select questions: what the wire carries and how Claude Code answers one

Recorded 2026-10-18 against **Claude Code 2.1.302**, model haiku, cwd `/tmp/msq-proj`. This closes
§9's first item in `2026-08-09-question-findings.md`, which forbade building a multi-select UI
without a recording.

## Provenance

- **Headless**, with Wake's own argv (every visibility flag, `--permission-prompt-tool stdio`,
  `--permission-mode auto`) plus `--safe-mode`, under the real `HOME`, for the partial-messages
  note's reason (the credentials live in the keychain). Scrubbed by `scripts/scrub-fixtures.py`.
  `testdata/stream/question-multiselect.jsonl`, with the lines Wake's stand-in wrote in
  `testdata/input/question-multiselect.stdin.jsonl`.
- **Interactive**, the same question put to a throwaway `claude --safe-mode` session in a VHS
  terminal, to see Claude Code's own question screen and read its answer back from that session's
  transcript. Nothing from it is committed: it is the behaviour Wake matches, maintained by hand.

## The ask

A multi-select question is the ordinary `AskUserQuestion` ask with the question's `multiSelect`
flag set true (`question-multiselect.jsonl:28`). Nothing else about the ask differs: the same
`can_use_tool`, the same `requires_user_interaction`, and the same options.

## The answer

There is still one string per question. A multi-select answer is the chosen labels joined by
`", "`. The headless session accepted `Mushrooms, Cheese, anchovies` in `updatedInput.answers`
(the stdin fixture's second line) and read it back to the model verbatim in the tool result
(`question-multiselect.jsonl:30`). The model's reply quotes it whole.

Claude Code's own screen produced exactly that string, which is why Wake sends it:

- **The order ticked, not the order listed.** Ticking option 2 and then option 1 sent
  `Mushrooms, Cheese`.
- **The typed words last.** Text typed in the "Type something" row came after the ticks, whenever it
  was typed.

## Claude Code's question screen, as observed

- Every option row carries a box, `[ ]` or `[✔]`, after its number. The cursor marker stays beside
  it.
- `↵`, the option's digit and space all toggle a box. None of them moves the cursor, and none moves
  on to the next question.
- The "Type something" row is a box too. Typing there ticks it.
- A row under the options walks on. It reads **Next** under a question that is not the last and
  **Submit** under the last one, which leads to the review. `↵` on it walks on even with nothing
  ticked.
- The tab strip marks a question `☒` once anything in it is ticked.
- The review lists the joined answer under its question, as the answer will be sent.

## Also seen: 2.1.302 replays the answer on stdout

The `control_response` Wake writes to answer the ask comes back on stdout
(`question-multiselect.jsonl:29`). No earlier recording carried one; the 2026-08-09 corpus found
none in 462 lines. It decodes as a control receipt with no error, no mode and no recall, so nothing
in Wake acts on it (`TestTheAnswerReplayedOnStdoutIsAnInertReceipt`). It is also why `allow` and
`updatedInput` left the airlock's unrecorded list.

## Not established

- **A label that itself contains `", "`** is indistinguishable on the wire from two choices.
  Claude Code's own encoding has the same ambiguity, and Wake sends what Claude Code sends.
- **More than one multi-select question in one ask**, or a multi-select question beside a
  single-select one, was seen on screen but not answered on the wire. The answer map is keyed per
  question, so nothing suggests the shape changes.

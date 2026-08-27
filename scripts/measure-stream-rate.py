#!/usr/bin/env python3
"""Measure how fast a session speaks, from the recorded corpus.

Token-level streaming costs one frame per output token, so what
`--include-partial-messages` does to the frame rate is a function of how fast a
session generates. That rate is Claude's, not Wake's, and it is read out of the
recordings rather than guessed - the reason CLAUDE.md gives about numbers
nothing asserts, and the same shape as extract-claude-flags.py.

    python3 scripts/measure-stream-rate.py internal/ui/testdata/stream-rate.json

`internal/ui/partial_bench_test.go` builds its fleet-scale arm from these
figures and `TestTheStreamingConstantsStillDescribeTheCorpus` holds the
constants to this file, so a re-recorded corpus moves the benchmark rather than
leaving it quoting a number that was true once.

Two things are counted and they answer different questions:

  * `tokens_per_second` is the rate, from `result` frames' own `output_tokens`
    and `duration_api_ms`. Turns under `MIN_TOKENS` are dropped: a two-token
    turn's rate is dominated by connection setup and says nothing about
    generation. It is the *frame* rate one working agent produces.
  * `block_chars` is how long an assistant text block is. It is what decides
    the rejected granularity's cost, because re-rendering per token is
    superlinear in exactly that dimension - so the maximum matters as much as
    the mean and both are reported.

This reads Claude's JSON, which is why it is a script and not a Go test:
internal/ui may not, and the airlock is four files in internal/core.
"""

import json
import pathlib
import statistics
import sys

# Below this a turn's reported rate is setup rather than generation.
MIN_TOKENS = 50

CORPUS = pathlib.Path("testdata/stream")


def frames(path):
    """Yield the decodable JSON objects in one recording."""
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            yield json.loads(line)
        except json.JSONDecodeError:
            continue


def rates_and_blocks(root):
    """Return (output tokens/second per turn, assistant text block lengths)."""
    rates, blocks = [], []
    for path in sorted(root.glob("*.jsonl")):
        for f in frames(path):
            if f.get("type") == "result":
                out = (f.get("usage") or {}).get("output_tokens")
                ms = f.get("duration_api_ms")
                if out and ms and out >= MIN_TOKENS:
                    rates.append(out / (ms / 1000.0))
            elif f.get("type") == "assistant":
                content = (f.get("message") or {}).get("content")
                if not isinstance(content, list):
                    continue
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "text":
                        text = block.get("text") or ""
                        if text.strip():
                            blocks.append(len(text))
    return rates, blocks


def percentile(sorted_values, q):
    return sorted_values[int(q * (len(sorted_values) - 1))]


def main():
    if len(sys.argv) != 2:
        sys.exit(f"usage: {sys.argv[0]} <output.json>")
    root = pathlib.Path(__file__).resolve().parent.parent / CORPUS
    rates, blocks = rates_and_blocks(root)
    if not rates or not blocks:
        sys.exit(f"no turns found under {root}: the corpus moved or the shapes changed")

    rates.sort()
    blocks.sort()
    out = {
        "_source": f"{CORPUS} at {len(rates)} turns of >={MIN_TOKENS} output tokens, "
        f"{len(blocks)} assistant text blocks",
        "_script": "scripts/measure-stream-rate.py",
        "turns": len(rates),
        "tokens_per_second": {
            "min": round(rates[0], 1),
            "median": round(statistics.median(rates), 1),
            "p90": round(percentile(rates, 0.90), 1),
            "max": round(rates[-1], 1),
        },
        "blocks": len(blocks),
        "block_chars": {
            "median": blocks[len(blocks) // 2],
            "mean": round(statistics.mean(blocks)),
            "max": blocks[-1],
        },
    }
    pathlib.Path(sys.argv[1]).write_text(
        json.dumps(out, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(out, indent=2))


if __name__ == "__main__":
    main()

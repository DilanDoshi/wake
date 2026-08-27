#!/usr/bin/env python3
"""Extract the effort and model vocabularies from an installed Claude Code.

Wake passes `--effort` and `--model`, and both sets are Claude's rather than
ours. They are read out of `claude --help` instead of being hand-written, for
the reason CLAUDE.md gives about numbers nothing asserts.

    python3 scripts/extract-claude-flags.py internal/core/testdata/claude-flags.json

The two flags are not equally knowable, and the output says which is which:

  * `--effort` prints its set closed - "(low, medium, high, xhigh, max)" - so
    `effort_levels` is exact, and internal/core's test holds core.EffortLevels
    equal to it.
  * `--model` prints examples - "e.g. 'fable', 'opus', or 'sonnet'" - and an
    e.g. closes nothing. So the key is `model_alias_examples`, and the test
    checks only that core.ModelAliases contains them.

    It is the *weaker* of two sources and deliberately kept anyway. The
    authority for the model list is the bare `/model` command's own reply,
    which enumerates ("Available: sonnet, opus, …, or a full model ID") and is
    recorded in testdata/stream/bare-model.jsonl. One list moving without the
    other is what a single source cannot catch.

`/effort` (the slash command) takes two levels `--effort` does not, ultracode
and auto. That is not in --help and is not extracted here; it is recorded in
core.EffortCommands against the usage line the command itself prints.

Regenerate when the version in the fixture moves. The test that reads the
fixture names this script in its failure message.
"""

import json
import pathlib
import re
import subprocess
import sys
import tempfile

# The parenthesised set on the --effort line, and the quoted names in the
# --model line's first "e.g.". Both are matched against a de-wrapped block:
# --help wraps a description across several indented lines, so nothing can be
# read a line at a time.
EFFORT_SET = re.compile(r"\(([^)]*)\)")
MODEL_EXAMPLES = re.compile(r"e\.g\.\s*([^)]*)")
QUOTED = re.compile(r"'([^']+)'")


def help_text():
    """Return `claude --help`, or exit saying why not.

    Captured through a file rather than a pipe, and that is not a style
    preference: at 2.1.232 `claude --help` writes 242 lines to a file and 20 to
    a pipe. Every flag this script reads is past the cut, so capture_output=True
    - the obvious spelling - sees a help text with no --effort in it and this
    script would report the flag as removed.
    """
    with tempfile.TemporaryFile("w+") as sink:
        try:
            done = subprocess.run(
                ["claude", "--help"], stdout=sink, stderr=subprocess.PIPE, text=True
            )
        except FileNotFoundError:
            sys.exit("no `claude` on PATH; this script reads the installed binary")
        if done.returncode != 0:
            sys.exit(
                f"claude --help exited {done.returncode}: {(done.stderr or '').strip()}"
            )
        sink.seek(0)
        return sink.read()


def version():
    done = subprocess.run(
        ["claude", "--version"], capture_output=True, text=True, errors="replace"
    )
    # "2.1.232 (Claude Code)" - the number is the half worth recording.
    return done.stdout.strip().split()[0] if done.stdout.strip() else "unknown"


def option_block(text, flag):
    """Return one option's whole description with its wrapping removed.

    An option starts at two spaces and a dash and runs until the next one, so
    the block is everything between - joined, with runs of whitespace
    collapsed, which is what makes a wrapped set matchable.
    """
    lines = text.splitlines()
    start = next(
        (i for i, line in enumerate(lines) if line.strip().startswith(flag)), None
    )
    if start is None:
        sys.exit(f"no {flag} in claude --help; the flag was renamed or removed")
    block = [lines[start]]
    for line in lines[start + 1 :]:
        if re.match(r"^\s{2,}-", line):
            break
        block.append(line)
    return re.sub(r"\s+", " ", " ".join(block)).strip()


def effort_levels(text):
    block = option_block(text, "--effort")
    found = EFFORT_SET.search(block)
    if not found:
        sys.exit(f"--effort prints no parenthesised set any more: {block!r}")
    return [level.strip() for level in found.group(1).split(",") if level.strip()]


def model_alias_examples(text):
    block = option_block(text, "--model")
    found = MODEL_EXAMPLES.search(block)
    if not found:
        sys.exit(f"--model names no examples any more: {block!r}")
    return QUOTED.findall(found.group(1))


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    out = pathlib.Path(sys.argv[1])

    text = help_text()
    payload = {
        "_script": "scripts/extract-claude-flags.py",
        "claude_code_version": version(),
        "effort_levels": effort_levels(text),
        "model_alias_examples": model_alias_examples(text),
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")
    print(
        f"{out}: {len(payload['effort_levels'])} effort levels, "
        f"{len(payload['model_alias_examples'])} model examples "
        f"from {payload['claude_code_version']}"
    )


if __name__ == "__main__":
    main()

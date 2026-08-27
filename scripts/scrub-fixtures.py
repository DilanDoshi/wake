#!/usr/bin/env python3
"""Take the recording machine back out of the corpus.

A recording is a photograph of the machine that took it. Claude's `system/init`
frame is an environment dump - skills, plugins and their paths, agents, memory
directories, the socket - and it is the *first line of every recording*, so
nobody ever chose to commit one. They arrived ahead of the frame each fixture
was actually recorded for.

    python3 scripts/scrub-fixtures.py            # rewrite in place
    python3 scripts/scrub-fixtures.py --check    # report, change nothing

`internal/core/corpus_test.go` is the guard this satisfies. Run this rather than
fixing a failure by hand: the point of a script is that the 80th file is treated
like the first.

# Three operations, because there are three shapes

**Environment keys are deleted** from init frames, in every file type. A
findings note pastes whole frames into a code fence, so an init frame in a `.md`
is the same leak as one in a `.jsonl`; every line is tried as JSON regardless of
extension, and the lines that are not JSON pass through untouched.

**Home paths are rewritten**, in both spellings. Claude slugifies a project
directory by replacing separators with dashes, so `/Users/someone/Documents/wake`
is also present as `-Users-someone-Documents-wake`; a substitution written for
the first form leaves the second sitting in the same file.

**Bare owner names are rewritten too**, and this is the one a path-shaped scrub
misses entirely. The corpus carries committed `ls -l` output inside tool
results, where the username is the *owner column* - a bare word with no prefix
to anchor on. The first version of this script left it in nine fixtures with the
guard passing. So pass one collects the owner names from the paths that do have
a prefix, and pass two replaces both shapes.

The rewrite is a *substitution*, not a blanking, and that is load-bearing:
`initFacts` returns nil when model and cwd are both empty, and the fleet and
discovery tests tell sessions apart by directory. Replacing only the owner's
name keeps every path distinct and every structure intact.

# What is kept, and why each one is a decision

`cwd` and `mcp_servers` are decoded by `initFacts`. `mcp_servers` also lists
public MCP servers rather than anything private, so it is a cheap keep.

`tools` and `slash_commands` are decoded by nothing and still cannot go; both
were learned by going red. `TestTheVocabularyDescribesTheRecordedCorpus` needs
`tools`, the only place `Task`, `Edit`, `WebFetch` and `WebSearch` appear in the
corpus. `TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising` needs
`slash_commands`, the 133 recorded words that make its overlap check exact - the
guard that caught `/rename` before Wake claimed it.

**"Read by nothing" has to mean read by no *test* either**, not merely absent
from `wire.go`. This list was wrong twice on exactly that distinction.

# The open trade

`skills` is deleted here and **that currently buys nothing**: every one of its
entries is also in `slash_commands`, which stays. Deleting one while keeping the
other removes a key, not an exposure. Whether an operator's own command names
should ship at all is a decision above this script. Until it is made, nothing here pretends they are
gone.
"""

import argparse
import json
import pathlib
import re
import sys

# Trees a recording can land in. docs/ is here because the findings notes paste
# frames inline, which is how the leak outran testdata/ the first time.
CORPUS_DIRS = ["testdata", "docs"]

# Init-frame keys describing the machine rather than the session, read by no
# decoder and no test. Keep in step with environmentKeys in corpus_test.go.
DEAD_KEYS = [
    "agents",
    "memory_paths",
    "messaging_socket_path",
    "plugins",
    "skills",
]

# A home directory with its owner attached. Two patterns rather than one, and
# the difference is a single `-` in the name class, because the two spellings
# terminate a name differently:
#
#   * a **path** ends the name at `/`, so a hyphen is part of it -
#     a name like `some-one` is one person, not `some`;
#   * a **slug** ends it at `-`, so a hyphen is the terminator -
#     `-Users-someone-Documents-wake` is `someone`, and a class that
#     swallowed hyphens would take `someone-Documents-wake` as the name and
#     destroy the path.
#
# Sharing one pattern between them silently half-redacts every hyphenated
# username: only the head is replaced and the tail survives, still naming them.
HOME_PATH = re.compile(r"(/)(Users|home)(/)([A-Za-z0-9._-]+)")
HOME_SLUG = re.compile(r"(-)(Users|home)(-)([A-Za-z0-9._]+)")
HOME_PATTERNS = (HOME_PATH, HOME_SLUG)
PLACEHOLDER = "dev"

# Names already agreed to be nobody. Left alone rather than rewritten: they are
# hand-written in prose and Go tests, the guard accepts them, and churning them
# would make this script fight its own documentation. Keep in step with
# scrubbedUsers in internal/core/corpus_test.go.
KEEP = {"dev", "someone", "somebody", "runner", "some-one"}

# macOS per-user temp directory. The hash is stable per account per machine, so
# it correlates even though it names nobody - `getconf DARWIN_USER_TEMP_DIR`
# prints exactly the value the corpus carried.
VARFOLDERS = re.compile(r"/var/folders/[A-Za-z0-9_]{2}/[A-Za-z0-9_]{20,}")
VARFOLDERS_PLACEHOLDER = "/var/folders/ab/scrubbedscrubbedscrubbed0000gn"


def owners(texts):
    """Return the home-directory owner names appearing across the corpus.

    Derived from the paths rather than taken as an argument, which is what keeps
    this script from having to be told whose machine it is.
    """
    return {
        m[4]
        for text in texts
        for pattern in HOME_PATTERNS
        for m in pattern.finditer(text)
        if m[4] not in KEEP
    }


def rewrite_paths(text):
    """Return text with every real home directory's owner replaced."""

    def sub(m):
        sep_a, prefix, sep_b, name = m.groups()
        return f"{sep_a}{prefix}{sep_b}{name if name in KEEP else PLACEHOLDER}"

    for pattern in HOME_PATTERNS:
        text = pattern.sub(sub, text)
    return VARFOLDERS.sub(VARFOLDERS_PLACEHOLDER, text)


def scrub_text(text, names, slash_mapping):
    """Delete environment keys from init frames, then rewrite what identifies."""
    out = []
    for line in text.split("\n"):
        try:
            frame = json.loads(line)
        except (json.JSONDecodeError, TypeError):
            out.append(line)  # most of a document is not JSON, and that is fine
            continue
        if isinstance(frame, dict) and frame.get("subtype") == "init":
            for key in DEAD_KEYS:
                frame.pop(key, None)
            cmds = frame.get("slash_commands")
            if isinstance(cmds, list) and slash_mapping:
                frame["slash_commands"] = [slash_mapping.get(c, c) for c in cmds]
            # separators=... keeps the compact spelling the corpus is written
            # in, so the diff is the removal rather than a reformat of 80 files.
            line = json.dumps(frame, separators=(",", ":"), ensure_ascii=False)
        out.append(line)

    text = rewrite_paths("\n".join(out))
    for name in sorted(names):
        text = re.sub(rf"\b{re.escape(name)}\b", PLACEHOLDER, text)
    return text


def corpus_paths(root):
    return [
        p for d in CORPUS_DIRS for p in sorted((root / d).rglob("*")) if p.is_file()
    ]


# The slash_commands allowlist is shared with internal/core/corpus_test.go's
# TestNoInitFrameAdvertisesAnUnlistedCommand. builtin + plugin + wake are the
# names that legitimately appear on a recorded init frame; neutral is the
# placeholders this script has written before. Reading the one file is what keeps
# the guard's list and this script's in step - the job the DEAD_KEYS/KEEP
# agree-test does by comparison, done here by a single source.
SLASH_ALLOWLIST = "internal/core/testdata/slash-allowlist.json"

# A real command token, as opposed to the "...ELIDED (133 entries)..." a findings
# note leaves inside a pasted frame. Matches corpus_test.go's commandShaped.
COMMAND = re.compile(r"^[a-z0-9][a-z0-9_.:-]*$")

# Phonetic stems, so a placeholder names nobody - the alphabet the existing
# corpus was already neutralised into.
NATO = ["alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
        "india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
        "quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey",
        "xray", "yankee", "zulu"]


def load_allowlist(root):
    """Return (doc, keep, neutral) from the shared allowlist JSON."""
    doc = json.loads((root / SLASH_ALLOWLIST).read_text())
    keep = set(doc["builtin"]) | set(doc["plugin"]) | set(doc["wake"])
    return doc, keep, set(doc["neutral"])


def slash_names(text):
    """Yield every command-shaped slash_commands entry on an init frame."""
    for line in text.split("\n"):
        try:
            frame = json.loads(line)
        except (json.JSONDecodeError, TypeError):
            continue
        if isinstance(frame, dict) and frame.get("subtype") == "init":
            for c in frame.get("slash_commands") or []:
                if isinstance(c, str) and COMMAND.match(c):
                    yield c


def build_slash_mapping(personal, taken):
    """Map each off-allowlist command to a fresh neutral, preserving new-/plugin
    shape and colliding with nothing already in use - so the same operator name
    becomes the same placeholder in every fixture."""
    mapping, used = {}, set(taken)
    for original in sorted(personal):
        prefix = "new-" if original.startswith("new-") else ""
        plugin = ":" in original[len(prefix):]
        i = 0
        while True:
            stem = NATO[i % len(NATO)] + ("" if i < len(NATO) else str(i // len(NATO)))
            cand = prefix + ("scrub:" + stem if plugin else stem)
            if cand not in used:
                break
            i += 1
        mapping[original] = cand
        used.add(cand)
    return mapping


def read(path):
    """Return a file's text, or None if it is not text at all."""
    try:
        return path.read_text()
    except UnicodeDecodeError:
        return None


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--check", action="store_true", help="report, change nothing")
    ap.add_argument("--root", default=".", help="repository root")
    args = ap.parse_args()

    root = pathlib.Path(args.root)
    texts = {}
    for path in corpus_paths(root):
        body = read(path)
        if body is not None:
            texts[path] = body

    names = owners(texts.values())
    if names:
        print(f"owner names found: {', '.join(sorted(names))}")

    doc, keep, neutral = load_allowlist(root)
    personal = {c for body in texts.values() for c in slash_names(body)
                if c not in keep and c not in neutral}
    slash_mapping = build_slash_mapping(personal, keep | neutral) if personal else {}
    if personal:
        print(f"operator commands neutralised: {', '.join(sorted(personal))}")

    changed = []
    for path, old in texts.items():
        new = scrub_text(old, names, slash_mapping)
        if new == old:
            continue
        changed.append(path.relative_to(root))
        if not args.check:
            path.write_text(new)

    # Register the placeholders just written, so the guard - which reads the same
    # file - passes on the corpus this run produced. Never in --check: a gate does
    # not edit the thing it checks.
    if slash_mapping and not args.check:
        doc["neutral"] = sorted(set(doc["neutral"]) | set(slash_mapping.values()))
        (root / SLASH_ALLOWLIST).write_text(json.dumps(doc, indent=2) + "\n")

    verb = "would change" if args.check else "scrubbed"
    for rel in changed:
        print(f"  {verb} {rel}")
    print(f"{len(changed)} files {verb}")
    # --check is a gate: a non-empty result means the corpus is not clean.
    return 1 if (args.check and changed) else 0


if __name__ == "__main__":
    sys.exit(main())

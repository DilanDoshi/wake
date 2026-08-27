# Session discovery — what is on disk, and what it proves

Measured 2026-08-12 against **Claude Code 2.1.228**, macOS 15 (arm64), on the owner's own machine:
**83 project directories, 428 session transcripts.** This is the recording session importing was
waiting on, and it is a *filesystem* recording rather than a stdout one — the question is what
`~/.claude/projects/` says, so there is nothing on any stream to commit.

Nothing here was run against a live LLM and no `wake` verb was run at all. Every number below comes
from one script, `scripts/measure-discovery.py`, which reads the tree and one `ps -Aww`. Re-run it
before trusting a number; this file records what it said on the day.

```
project dirs                          83
transcripts                           428
  filename not <uuid>.jsonl           0
  zero bytes                          0
  no cwd on any line                  1
  lines that are not JSON             0
first cwd slugs to its own dir        ok 369  MISMATCH 58
verified dirs per transcript          {0: 96, 1: 331}
  matched a cwd, dir now deleted      93
non-[A-Za-z0-9-] chars in cwd values  {'/': 1039346, ' ': 174257, '.': 61875, '_': 407}
non-[A-Za-z0-9-] chars in slug dirs   {}
live interactive claude processes      bare 4  --session-id 10  --resume 2
```

---

## 1. The layout, confirmed rather than assumed

`~/.claude/projects/<slug>/<session-id>.jsonl`, one file per session, named for the session UUID.
**428 of 428 filenames are a canonical lowercase v4-shaped UUID with a `.jsonl` suffix** — no
exceptions, no temporary names, no lockfiles. So the id is free from the filename and needs no
parse of the contents.

`v1_goals.md` said *"115 project directories, 444 session transcripts"* when the feature was
proposed. It is 83 and 428 today on the same machine. Both were counted, and the drift is the point:
**this directory is not append-only** — directories disappear when their project does. Nothing may
treat a transcript as a permanent record.

---

## 2. The slug is a *lossy* function of the directory, so it cannot be inverted

The slug replaces path separators with `-`. It replaces more than that, and the corpus says which:

```
non-[A-Za-z0-9-] chars in cwd values  {'/': …, ' ': …, '.': …, '_': …}
non-[A-Za-z0-9-] chars in slug dirs   {}
```

Every one of `/`, ` `, `.` and `_` appears in real working directories on this machine, and **not
one of them survives into a slug** — 83 of 83 slug names are `[A-Za-z0-9-]` only. Three worked
examples, each a real pair on this disk:

```
/Users/dev/Documents/Project Pufferfish            →  -Users-dev-Documents-Project-Pufferfish
    (a space)
/Users/…/delta-agent/.claude/worktrees/dev-1421      →  …-delta-agent--claude-worktrees-dev-1421
    (a dot, giving the doubled dash)
/private/tmp/claude-501/-Users-dev-Documents/…     →  -private-tmp-claude-501--Users-dev-Documents-…
    (a directory whose real name already contains dashes)
```

**So `-` is the image of at least five different things** — `/`, `.`, ` `, `_`, and a literal `-`.
The mapping is many-to-one and there is no inverse. `-Users-dev-Documents-Project-Pufferfish`
is equally consistent with `/Users/dev/Documents/Project Pufferfish`,
`/Users/dev/Documents/Project/Pufferfish` and `/Users/dev/Documents/Project.Pufferfish`.

**This is the "get it wrong and you import into the wrong place" case**, and it is not hypothetical
on this machine: `Project Pufferfish` with a space is where most of the owner's work lives.

### What the corpus cannot distinguish, and why that is safe here

Two candidate slug functions — *"replace `[/._ ]`"* and *"replace everything outside
`[A-Za-z0-9-]`"* — agree on **all 401 distinct (dir, cwd) pairs**, because no real directory here
contains any other special character. So the corpus **does not establish which one claude uses**,
and this note does not claim to know.

It does not have to, and that is a property of the direction the function is used in. Wake never
*builds* a directory from a slug. It takes a directory the transcript itself names, slugs it, and
**compares**. If Wake's slug function is broader than claude's, or narrower, the two disagree on
some character and the comparison fails — which is a **refusal**, never a wrong directory. An
unknown that can only produce false negatives is one this project is allowed to ship against.

---

## 3. The transcript names its own `cwd`, and it is **not** the directory the session was started in

Every `user`, `assistant` and `attachment` frame carries a top-level `cwd`. **427 of 428
transcripts carry at least one.** That looks like the answer to §2 and it is not:

```
first cwd slugs to its own dir        ok 369  MISMATCH 58
```

**58 transcripts have a first `cwd` that does not slug to the directory holding them.** They go both
ways, which is what rules out an off-by-one:

```
dir  -Users-…-delta-agent                              cwd  /Users/…/delta-agent/.claude/worktrees/dev-1919
dir  -Users-…-delta-agent--claude-worktrees-dev-1421   cwd  /Users/…/delta-agent
```

A transcript is also not single-valued: distinct `cwd` counts per file run **1 to 51**, and only 223
of 428 hold exactly one. Subagents, `--worktree` and a `cd` inside a turn all move it.

**So `cwd` is a property of a message, not of the session**, and reading the first one as "where to
resume" is wrong for 58 of 428 sessions here — 13.6%. That is the trap this section exists to
record.

---

## 4. What discovery *can* prove: a verified directory, by comparison

The rule that survives §2 and §3 is a **verification**, and it uses both facts against each other:

> A directory is proven for a transcript when it is a `cwd` **the transcript itself names**, its
> slug **equals the directory name the transcript was found in**, and it **still exists on disk**.
> If there is not exactly one such directory, discovery names none.

Measured over the whole corpus:

```
verified dirs per transcript          {0: 96, 1: 331}
```

**331 of 428 are proven; 97 are not; and not one is ambiguous.** The ambiguity arm has never fired
here, which is a reason to keep it rather than to drop it — it is what stands between a lossy
function and a confident wrong answer, and its cost is one comparison.

Of the 97 without a directory, **93 are transcripts whose directory has since been deleted** —
scratch trees from this project's own recording spikes. The remaining 4 are the `--worktree` shape
from §3, where no `cwd` in the file slugs to the containing directory at all. Both are the same
verdict for the operator: **there is a transcript and Wake will not say where to run it**, so it is
listed and refused rather than resumed into `$PWD`.

---

## 5. What discovery **cannot** prove: that anything is closed

This is the half that decides the feature, and the measurement is on the process table:

```
live interactive claude processes      bare 4  --session-id 10  --resume 2
```

Ten of the sixteen live `claude` processes carry `--session-id <uuid>` (they were started by cmux,
which assigns one). Two carry `--resume`. **Four carry neither**: their whole command line is the
word `claude`, because a person typed it and the CLI minted the id itself.

`daemon.resumeSafe` asks `idsInUse`, which matches `core.SessionArgvMarkers` — `--session-id <id>`
and `--resume <id>`. **It is structurally blind to the bare shape**, and the bare shape is exactly
what the feature was asked for: *"a bunch of sessions open in terminals scattered."*

So, restating 2026-08-09 findings §8.4 with the numbers attached:

- a **transcript is not evidence about a process**. Recency is a hint and nothing more;
- `resumeSafe` catches what it can catch — 12 of 16 live processes here — and **a pass from it is
  not proof the session is closed**;
- there is no error, no frame and no diagnostic if the guess is wrong: two live processes on one id
  branch the transcript in place, last-writer-wins, undetectably (§5 of that note).

**Discovery must therefore not imply importability, and an import must not be a `--resume`.**

---

## 6. What that leaves, and it is the primitive the corpus already named

2026-08-09 findings §5 ends with the sentence this whole note converges on:

> **And it makes the fork the safe primitive for importing, not just for forking.** `--resume …
> --fork-session --session-id <new>` against a session someone still has open costs the original
> nothing (§7): the parent file is not touched.

and 2026-08-10 §5 measured that against a **live** parent, by sha256 at both generations: the
parent's transcript is **byte-identical** across a fork taken while it was running.

So an import is a fork of a transcript Wake did not write:

1. it **cannot** branch the session somebody still has open, whatever `resumeSafe` could or could
   not see — a fork does not write to the parent's file;
2. the new session's id is **minted by Wake**, so `maySpawn`, the reaper's argv proof and
   `SessionArgvMarkers` are all unchanged;
3. it runs in the **verified** directory from §4, never `$PWD`;
4. `resumeSafe` is still asked, and still refuses — it is a *narrowing* that catches the 12-of-16
   case, not the thing the safety rests on.

**What is given up is the id.** `--resume` would have kept it (§2 of the 2026-08-09 note), and an
imported session therefore does **not** keep its original identity: it is a new Wake session
carrying that conversation. That is a real cost and it is the right one — the alternative is
keeping an id by writing to a file another process may be writing to, with nothing anywhere able to
say it happened.

---

## 7. What this did **not** establish

- **Which slug function claude actually uses.** §2: two candidates agree on every path in this
  corpus. Only a directory containing `+`, `@`, `~`, `#`, a non-ASCII character or a newline would
  separate them, and none exists here. Wake's use is comparison-only, so a disagreement is a
  refusal — but nobody should read this note as knowing the function.
- **Whether the slug is derived from the cwd at all**, as opposed to from something correlated with
  it. 401 pairs are consistent with it; that is evidence, not a specification.
- **What a *partial* transcript looks like.** No zero-byte and no non-JSON line was found in 428
  files, so the torn-write case was **not observed** and is not designed around. A file being
  appended to *right now* by a live session was not sampled either.
- **Whether a live `claude` holds its `.jsonl` open**, which would make `lsof` a liveness check that
  reaches the bare shape §5 is blind to. Not measured. It is the one thing that would turn the
  liveness question from "cannot" into "can", and it is the obvious next spike.
- **Forking a transcript no Wake daemon has ever held.** Every recorded fork in this project's
  corpus took a parent this daemon started. An import forks a stranger's transcript, and while §4's
  directory is verified and §6's flag shape is the recorded one, *that composition* has not been
  run against a live `claude`.
- **A transcript being written while it is forked.** 2026-08-10 §12 already lists this; §5 here only
  adds that the writer may be a process Wake cannot see.
- **`customTitle` / `agentName` as a preview source.** Both appear on this corpus (a `custom-title`
  and an `agent-name` frame lead several files) and neither was surveyed for coverage.
- **Anything about a machine that is not this one.** One user, one OS, one plugin inventory.

---

## 8. Method

`scripts/measure-discovery.py`, committed beside this note. It walks `~/.claude/projects`, parses
every line of every `.jsonl`, and shells out to one `ps -Aww -o command=`. It writes nothing,
starts nothing, and reads no `WAKE_SOCKET`. The per-transcript numbers are over all 428 files; the
character census is over every `cwd` string, so its counts are occurrences rather than paths.

The process census classifies a command line whose argv[0] basename is `claude`, excluding
claude's own `bg-pty-host`, `bg-spare` and `daemon` helpers, which are not sessions.

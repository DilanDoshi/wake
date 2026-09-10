# Making releases — notes and gotchas

Companion to [`docs/RELEASING.md`](../RELEASING.md), which is the how-to (tag →
`goreleaser release --clean`). This file records things that have *bitten* a
release, so they don't bite the next one.

## The `@name` changelog → phantom "contributors" gotcha

**Symptom.** The GitHub Release page shows contributor avatars for accounts that
aren't real people. On v0.1.0 they read `@who`, `@agent`, and `@name`.

**Cause.** GoReleaser builds the release notes from raw commit *subjects*, and
Wake's commit messages legitimately use its own `@name`-routing syntax — e.g.
"route `@agent` /command", "`@who` /rename", "the room `@name` view filter".
GitHub auto-links any `@word` in release notes as a **user mention** and renders
it with an avatar, so your own syntax turns into phantom contributors. It is
purely cosmetic — the commits are all authored by you; nothing is actually
mis-attributed.

**Per-release fix.** Backtick-wrap the mentions in the notes so GitHub won't
linkify them (they *are* syntax, so code-formatting is also more correct):

```sh
gh release view vX.Y.Z --json body --jq '.body' > /tmp/notes.md
python3 - /tmp/notes.md <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
s = re.sub(r'(?<!`)@(who|agent|name|all)\b', r'`@\1`', s)
open(p, 'w').write(s)
PY
gh release edit vX.Y.Z --notes-file /tmp/notes.md
```

Then hard-refresh the release page; the phantom contributors disappear. Applied
to v0.1.0 — 14 mentions neutralised.

**Permanent options (not yet done).** Pick one when it's worth it:

- Add a post-release step that runs the substitution above on the generated
  notes automatically.
- Switch `.goreleaser.yaml`'s changelog to GitHub's PR-based format
  (`changelog.use: github`). It works from the *second* tag onward — it needs a
  previous tag to diff against, which is why v0.1.0 fell back to the git-based
  changelog. It lists PR titles rather than raw commit subjects, so it is less
  prone to spawning mentions (though a PR *title* containing `@name` could still
  do it).

## `go install` and the reflow `replace` (context for future config changes)

`go install github.com/DilanDoshi/wake/cmd/wake@<version>` refuses any module
whose `go.mod` carries a `replace` directive. As long as `go.mod` has
`replace github.com/muesli/reflow => ./third_party/reflow` (the vendored
word-wrap fix), the `go install` channel does not work — only the GitHub Release
binaries and `git clone && go build` do. Removing that blocker is tracked
separately (the wrap-move: wake wraps via `x/ansi` so glamour can use upstream
reflow). Until it lands, the README should not lead with `go install`.

<!-- add future release gotchas below -->

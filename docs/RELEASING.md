# Releasing Wake

Releases are **manual**. Nothing publishes automatically — you run one command when *you* decide a
release should go out. There is no CI release trigger; the timing is entirely yours.

## Cut a release

1. Land everything you want in the release on `main`, and confirm the gate:

   ```sh
   make ci        # must exit 0 — this is the only gate
   ```

2. Bump the checked-in banner default to the release number, so a plain `go
   build`/`go install` reports it. Update `Version` in `internal/version/version.go`
   and the `bannerVersion` constant in `internal/ui/menuposition_test.go` (they
   must match — the screen tests assert the banner draws `v<Version>`), then
   re-run `make ci`. A release build stamps the version from the tag via
   `-ldflags -X`, but the checked-in default is what every non-release build
   shows, so keep it in step with the tag you are about to cut.

3. Tag the commit (semver):

   ```sh
   git tag v0.1.2
   git push origin v0.1.2
   ```

4. Cut it:

   ```sh
   GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
   ```

   This publishes with `.goreleaser.yaml`'s default changelog — every commit since the last tag, one
   line each, machine-authored. **Replace it before calling the release done** — see the format below.

5. Write the real release notes and replace the auto-generated changelog:

   ```sh
   gh release edit v0.1.2 --notes-file /path/to/notes.md
   ```

That builds `wake` for **macOS and Linux, amd64 and arm64**, stamps the version from the tag, and
publishes a **GitHub Release** with the binaries and a `checksums.txt`. The version the banner
reports comes from the tag, via `-ldflags -X …/internal/version.Version` (this is why `Version` is a
`var`, not a `const` — a `const` can't be stamped).

## Release notes format

`v0.1.1` and `v0.1.2` are the template — hand-written, organized by what a user of Wake cares about,
never the raw commit list. `v0.1.0` and `v0.1.3` shipped with goreleaser's default changelog because
step 5 above was skipped; they are what *not* to repeat, not a second acceptable shape.

The shape, in order:

1. **One summary line** naming the headline change(s) in plain language — no heading, right under the
   title. `v0.1.2`'s: "This release brings Claude Code's native `/goal` and `/loop` lifecycles into
   Wake, animates `/compact`, and lands a set of board, composer, and terminal-setup improvements —
   plus a fix that makes `go install` work without a fork." A patch release with nothing to opt into
   says so (`v0.1.1`: "a patch release... No new flags and nothing to opt into: it installs and runs
   exactly like v0.1.0.").
2. **`## New features`** — one `### Feature name (#PR)` subsection per notable feature, each a short
   prose paragraph on what it does and why, not a changelog line. Omit this section entirely on a
   patch release with no new features (`v0.1.1` has none).
3. **`## Improvements`** — a bullet list, each `- **Short name** (#PR) — description.` for smaller,
   non-headline additions.
4. **`## Fixes`** — same bullet shape as Improvements: `- **Short name** (#PR).` then a paragraph.
   Name the user-visible symptom and the mechanism, the way the PR title and body would, not just
   "fixed a bug."
5. **`## Install`** — the `go install github.com/DilanDoshi/wake/cmd/wake@<tag or latest>` command,
   plus a line pointing at the prebuilt binaries below and `checksums.txt`.
6. **`**Full changelog:**`** — one line, `https://github.com/DilanDoshi/wake/compare/<prev>...<tag>`.

Every named change gets its PR number in parentheses — a reader can always jump to the diff. Skip a
section that has nothing in it rather than writing "None" under a heading.

## Before you tag

- `make ci` is green. It runs the suite twice (with and without `-race`), lint, the coverage floors,
  and a cross-compile. Run it from a normal checkout under your home directory — not `/tmp` and not a
  very long temp path, because the screen tests render the working directory and assume a sane path.
- Follow semver for the tag.
- The checked-in banner default (`internal/version.Version`, and its `bannerVersion` test twin) matches the
  tag — that is step 2 above. A tag ahead of the default means every non-release build under-reports.

## After you cut

- **Install from the published artifact on a machine that has never built Wake, and run it.** A
  release nobody has installed from is a release nobody has tested. `go install
  github.com/DilanDoshi/wake/cmd/wake@latest` also works once the Go module proxy has indexed the tag.

## Dry runs (safe — publish nothing)

```sh
goreleaser check                          # validate .goreleaser.yaml
goreleaser build --snapshot --clean       # build every platform locally, publish nothing
```

## Homebrew tap (not wired yet)

`brew install …/wake` is a fast-follow. It needs a `homebrew-tap` repository and a `homebrew_casks`
block in `.goreleaser.yaml` (GoReleaser deprecated the formula `brews` block in favour of casks for
prebuilt binaries). Note: macOS Gatekeeper will quarantine the **unsigned** binary until it is signed
and notarised, so plan for signing before leaning on the tap as the primary channel.

## Config

`.goreleaser.yaml` lives at the repo root. It is set up once and reused for every release — you
should not have to touch it again unless you add a channel (the tap) or a platform.

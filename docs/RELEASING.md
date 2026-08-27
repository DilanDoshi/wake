# Releasing Wake

Releases are **manual**. Nothing publishes automatically — you run one command when *you* decide a
release should go out. There is no CI release trigger; the timing is entirely yours.

## Cut a release

1. Land everything you want in the release on `main`, and confirm the gate:

   ```sh
   make ci        # must exit 0 — this is the only gate
   ```

2. Tag the commit (semver):

   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```

3. Cut it:

   ```sh
   GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
   ```

That builds `wake` for **macOS and Linux, amd64 and arm64**, stamps the version from the tag, and
publishes a **GitHub Release** with the binaries and a `checksums.txt`. The version the banner
reports comes from the tag, via `-ldflags -X …/internal/ui.Version` (this is why `Version` is a
`var`, not a `const` — a `const` can't be stamped).

## Before you tag

- `make ci` is green. It runs the suite twice (with and without `-race`), lint, the coverage floors,
  and a cross-compile. Run it from a normal checkout under your home directory — not `/tmp` and not a
  very long temp path, because the screen tests render the working directory and assume a sane path.
- Follow semver for the tag.

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

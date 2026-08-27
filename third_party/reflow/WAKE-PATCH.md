# muesli/reflow v0.3.0, with one branch changed

Upstream: https://github.com/muesli/reflow (MIT, see `LICENSE`).
Vendored verbatim except for `reflow.png` and `.github/`, which were dropped, and the patch below.

## The patch

`wordwrap/wordwrap.go`, the breakpoint branch of `Write`:

```go
} else if inGroup(w.Breakpoints, c) {
    w.addSpace()
    w.addWord()
    if w.lineLen > 0 && w.lineLen+1 > w.Limit {   // added
        w.addNewLine()                             // added
    }                                              // added
    w.lineLen++                                    // added
    _, _ = w.buf.WriteRune(c)
}
```

`Breakpoints` defaults to `{'-'}`. A breakpoint rune goes straight to the
buffer instead of being held in `w.word`, so it misses both things the
ordinary-character branch below does: it is never counted against `lineLen`,
and it is never checked against the limit. The line is therefore measured short
and then written past the limit anyway - asked for 88 cells, a sentence with
five hyphens comes back 92 wide.

**Both halves are needed.** Counting alone fixes single hyphens and leaves runs
of two: `--resume` still overran by one cell, which is the spelling this project
uses constantly. Measured by `internal/render.TestProseWrapsGreedily` over its
corpus - upstream strands 8,693 lines, counting-only strands 254, both together
strand none.

## Why Wake carries a fork for it

`glamour` wraps a paragraph **twice** at the same width - once here
(`ansi/paragraph.go`), then again over the whole document block
(`ansi/blockelement.go`, which uses `x/ansi.Wordwrap` and is correct). The
second pass re-breaks the over-long line the first pass emitted, and the tail
word is stranded on a line of its own with room to spare above it:

```
  I'll work ticket DEV-3035 end-to-end following the /complete-linear-ticket skill. First,
  let
  me load the Linear tools and fetch the ticket.
```

Nothing in glamour's API reaches this. `WithWordWrap` sets the width both
passes use, `options.WordWrap` has exactly two readers and both are
`blockstack.Width()`, and none of the other fifteen options disable the first
pass. So there is no width or style that avoids it - one hyphen is enough,
which here means flags (`--resume`), dates and ticket ids. glamour v1.0.0 is
the latest release and reflow's is from 2021.

The alternative was `WithWordWrap(0)`, which turns off glamour's wrapping and
padding and leaves Wake to wrap. That also gives up the width-aware layout
glamour does for lists, tables and blockquotes, which is a great deal more
surface than one branch.

## What guards it

`internal/render.TestProseWrapsGreedily`. Upstream's own suite does **not** -
it has two breakpoint cases and neither discriminates: both produce identical
output patched and unpatched. This directory is a separate module, so
`go test ./...` and `make ci` do not descend into it either.

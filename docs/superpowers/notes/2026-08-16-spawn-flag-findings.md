# `--add-dir` and the debug flags, measured

**2026-08-16, claude 2.1.233, macOS.** Four probes, all under a throwaway `HOME` so nothing on this
machine reached a recording, and all with stdin closed immediately — a session that is spawned and
never prompted exits 0 without a model turn, so every one of these cost $0.

The probe shape, which is Wake's own argv minus the flags under test:

```sh
D=$(mktemp -d); cd "$D"
printf "" | HOME=$D claude --print --input-format stream-json --output-format stream-json --verbose <flags> > out.json 2> err.txt
```

No frames are pasted below. `--help`'s own wording for the three flags is quoted, and everything else
is a count.

## 1. `--debug <filter>` alone writes nothing anybody can read

`--debug api`: **exit 0, 0 bytes on stderr**, and `out.json` byte-for-byte the length of the same
spawn without the flag (585 bytes, two `system` frames). Bare `--debug` with no filter: identical.

This is the finding the build is designed around. `--help` says `-d, --debug [filter]` — *"Enable
debug mode with optional category filtering (e.g., \"api,hooks\" or \"!1p,!file\")"* — and in an
interactive session that output goes to the terminal. In `--print --output-format stream-json`, which
is the only mode a Wake agent ever runs in, it goes nowhere observable: not stdout, which would
corrupt the airlock, and not stderr, which `internal/core/process.go` keeps a bounded tail of.

So Wake refuses a filter with no `--debug-file` beside it rather than emitting one. The alternative —
emitting it — is a flag an operator turned on and no log anywhere, which is the "lying feature" this
project's legend rule exists to prevent. `internal/core/debug.go` holds the argument;
`internal/daemon/spawnconfig.go`'s `debugPairing` holds the refusal, and `cmd/wake` and `internal/ui`
each refuse it before a socket is dialled.

## 2. `--debug-file <path>` writes a real log, and does enable debug mode

`--debug-file $D/dbg.log` with no `--debug`: exit 0, 0 bytes on stderr, and **17,053 bytes** at that
path from a session that took no turn at all. Lines are `<ISO timestamp> [DEBUG] <message>`. `--help`
says *"Write debug logs to a specific file path (implicitly enables debug mode)"*, and the file
appearing without `--debug` is that sentence being true.

The parent directory existed in every probe, so **whether `--debug-file` creates its own parents is
not recorded**. `internal/daemon/debuglog.go` creates the directory itself rather than finding out.

## 3. `--add-dir` accumulates in both spellings, and they are indistinguishable

Two directories created outside the session's own tree, then the same session spawned twice — once
`--add-dir $E/alpha1 --add-dir $E/beta2` (repeated) and once `--add-dir $E/alpha1 $E/beta2`
(variadic, which is what `--help`'s `--add-dir <directories...>` documents) — with `--debug-file` on
both so there was something to read.

Both logs mention `alpha1` **7 times** and `beta2` **7 times**. The two spellings are equivalent as
far as anything observable here is concerned.

Wake emits the **repeated** form, and the reason is local rather than about claude: the variadic form
has to ask "is this the first directory", and `internal/core/argvguard_test.go` refuses a question
about a Config field's value on the argv path. The repeated form needs no question at all.

## 4. What this does not settle

- **Whether a running session can be given a directory, or logging, without a respawn.** The recorded
  corpus in `testdata/stream` advertises `debug` on its `init` frames and does not carry `add-dir`,
  and a corpus's `slash_commands` merges claude's own commands with the operator's `.claude/commands`
  files — so it cannot tell those apart. This is what `docs/notes/deferred.md`'s 2026-08-16 entry
  leaves open, and it is what decides whether either flag should survive a park.
- **What an added directory does for a session with no built-in tools.** The manager is spawned with
  `--tools ""`, so `wake manager --add-dir` widens the reach of a session that has no file tool to
  reach with. `--help` also lists `--add-dir` under "CLAUDE.md dirs", so it may still affect what
  memory that session loads; nothing here measured it.
- **Whether two sessions given the same log name interleave or clobber.** `--debug-file` truncates,
  and Wake refuses no duplicate.

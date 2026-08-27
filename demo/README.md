# The demo film

A ~3 minute film of Wake, and a landing page. Every frame is the **real `wake`
binary** in a real pty driven by [VHS] — the daemon, the room, the rendering
and the protocol are all genuine. The only scripted part is what the models
say.

    ./setup.sh          # build the workspace: Harbor, the PATH shim, a scratch HOME
    vhs tapes/03-broadcast.tape   # record one beat
    ./build.sh          # caption, stitch, master out

## Why the agents are fake

Recording against live Claude would be slow, nondeterministic, billed per take,
and — for something posted publicly — a way to leak whatever happened to be on
the machine. So `agent/claude` is a Python stand-in on a shim `PATH`, which is
all it takes: `internal/core` resolves `claude` on `PATH` and nothing else.

It speaks genuine stream-json. **The frame shapes are copied from
`testdata/stream/`** — init, assistant blocks, tool calls, task lifecycle,
permission asks, interrupts, per-turn results — so what Wake decodes on camera
is the real wire. It also journals to `$HOME/.claude/projects/…` the way claude
does, because Wake keeps no transcripts of its own and a fake that skipped that
left an empty room on every reattach.

Python rather than Go on purpose: `internal/core/argv_test.go` and
`airlock_test.go` walk every non-test `.go` file in the tree and fail the build
on one that spells `--session-id` or Claude's wire vocabulary outside the
airlock. Those guards are right; a demo prop is not a reason to widen them.

## The manager really does reach the fleet

`agent/wakemcp.py` is a small MCP client. The scripted manager is spawned with
`--mcp-config` like any other, and behind that path is `wake mcp` — a real
stdio server backed by the daemon socket. So "tell the agents working on the
api to…" is genuinely `list_agents`, a filter, and a `send_to_agent` per match,
and the roster rows that light up are lit by the tool calls above them. A
manager that only *drew* those calls would be a lie on camera.

## Layout

    agent/claude        the scripted `claude`, on a shim PATH
    agent/wakemcp.py    MCP client, so the manager's fan-out is real
    scenarios/*.json    one per cast member; `_pool.json` is the fallback
    harbor/             the fictional product, copied into a real git repo
    tapes/*.tape        one per beat
    captions.py         title cards and caption bands, in Wake's own palette
    setup.sh            builds .work/ (and generates .work/stage.tape)
    build.sh            trims, captions, stitches, masters

`setup.sh` generates the staging tape rather than shipping one, because
`/new <name> in <dir>` resolves a relative directory against the *session's*
directory — so `in web` from an agent started in `api/` means `api/web`, and a
refused command keeps its draft for the next line to concatenate onto.

## The film is silent

Deliberately. It ships as a silent master with captions carrying the argument,
which is how most people watch video in a feed. To add a track:

    ffmpeg -i wake-demo.mp4 -i track.m4a -c:v copy -shortest -c:a aac wake-demo-scored.mp4

[VHS]: https://github.com/charmbracelet/vhs

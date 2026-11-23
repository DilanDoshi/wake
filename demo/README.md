# The demo film

A ~3 minute film of Wake, and a landing page. Every frame is the **real `wake`
binary** in a real pty driven by [VHS] — the daemon, the room, the rendering
and the protocol are all genuine. The only scripted part is what the models
say.

    ./setup.sh          # build the workspace: Harbor, the PATH shim, a scratch HOME
    ./record.sh tapes/03-broadcast.tape   # record one beat
    WANT_AGENTS=22 ./record.sh tapes/33-jump.tape   # a beat on the big fleet
    ./build.sh          # caption, stitch, master out
    python3 -m unittest discover -s .     # the relay's and the page's tests

`record.sh` counts the fleet a take staged and then **ends it**: the daemon is detached and outlives
`vhs`, so a bare `vhs tapes/…` leaves it, and its agents, running until `wake stop`.

Every take mints its own socket under `/tmp/wake-take.*`, and every tape is filmed at 3200x1760,
font 21 (`setup.sh`'s `head.tape`). Larger loses frames on a busy machine and the take plays fast, so
check a take's length against its script before keeping it.

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
    gestures/*.json     one per clip that needs the mouse: ctrl+<key> bound to clicks, drags, wheel
    harbor/             the fictional product, copied into a real git repo
    tapes/*.tape        one per beat; `_*.tape` are sourced helpers, never recorded
    shiftkeys.py        runs `wake` in a pty that turns Ctrl+G/Ctrl+K into ⇧↓/⇧↑, which VHS
                        cannot send and the roster walks on, and the keys a clip's
                        gestures/ file binds into the mouse reports a terminal sends
    test_shiftkeys.py   the relay's tests: the bytes, the pacing, the refusals, one real pty
    test_page.py        the site's tests: what the front page leads with, that every clip has a
                        tape that ends after a Show and a gesture file that matches it, that a cut
                        never runs past its recording, that no sentence a review proved false
                        has come back
    test_fleet.py       the big fleet's table in setup.sh against its scenarios
    test_agent.py       the stand-in `claude`, run as Wake runs it (an autostarted ask is answerable)
    captions.py         title cards and caption bands, in Wake's own palette
    setup.sh            builds .work/ (and generates .work/stage.tape)
    build.sh            trims, captions, stitches, masters
    page.py             the landing site (index, features, releases, and the robots.txt,
                        sitemap.xml and favicon a search engine reads): `page.py <wake-landing>`
    pagecopy.py         its words — one entry per beat, the build notes, each page's title
                        and description

## The mouse

VHS cannot send a click, a drag or the wheel, so a take that needs one runs `wake` under
`shiftkeys.py` with `WAKE_GESTURES` set to its clip's name (`export WAKE_GESTURES=34-drag` before
the stage is sourced, which is where `wake` first starts). `gestures/34-drag.json` binds
`ctrl+<letter>` to steps at screen cells counted from 0 — `click`, `press`, `move`, `release`,
`glide` (from, to, steps, over), `wheel_up`, `wheel_down`, `wait` — and the tape types that key.
The relay writes the same SGR reports a terminal does, so the binary sees a real mouse. Find a
cell by recording with an `Output x.txt` (VHS writes the screen as text) and reading it; the
terminal is 62 rows tall at this size, the sidebar's rule in column 214 and an agent's name from
column 219. Ctrl+G, K, C, H, I, J, M, W, Q and Esc cannot be bound: the relay, the staging or the
tape needs them. A cell is only as good as the roster it was read from: idle agents swap rows
between stagings, so a gesture grabs an agent that is blocked or working, which keep theirs.

## The big fleet

`setup.sh` also generates `stage-fleet.tape`: 21 agents and the manager in five teams, three of
them blocked on an ask and six mid-tool for the length of a take (`scenarios/<name>.json`; the
rest take a variant of `_pool.json`), which is what the agent finder, the sidebar's search bar,
dragging an agent out of it and its team boxes are filmed against. A take on it is recorded with
`WANT_AGENTS=22`, or `record.sh`'s count of 7 passes a half-staged fleet.

A tape must end after a `Show`: VHS runs a tape that never calls it a second time, and the second
pass types the whole staging into the room that the first one left running.

`setup.sh` generates the staging tape rather than shipping one, because
`/new <name> in <dir>` resolves a relative directory against the *session's*
directory — so `in web` from an agent started in `api/` means `api/web`, and a
refused command keeps its draft for the next line to concatenate onto.

## The film is silent

Deliberately. It ships as a silent master with captions carrying the argument,
which is how most people watch video in a feed. To add a track:

    ffmpeg -i wake-demo.mp4 -i track.m4a -c:v copy -shortest -c:a aac wake-demo-scored.mp4

[VHS]: https://github.com/charmbracelet/vhs

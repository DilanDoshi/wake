#!/usr/bin/env python3
"""Title cards and lower-third captions for the demo film.

Rendered as PNGs and composited by ffmpeg rather than drawn with `drawtext`,
which cannot do tracking, a rounded key chip, or a scrim that fades. The
palette is Wake's own, read off `internal/ui/theme.go`, so the captions belong
to the same piece of software as the terminal behind them.
"""

import os
import sys

from PIL import Image, ImageDraw, ImageFont

W, H = 2400, 1320

GROUND = (20, 19, 19)
ACCENT = (215, 119, 87)  # theme.Accent  — claude orange
TEXT = (255, 255, 255)
MUTED = (153, 153, 153)

FONTS = os.path.expanduser("~/Library/Fonts")
BOLD = os.path.join(FONTS, "JetBrainsMono-Bold.ttf")
REG = os.path.join(FONTS, "JetBrainsMono-Regular.ttf")
LIGHT = os.path.join(FONTS, "JetBrainsMono-Light.ttf")

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".work", "cards")


def font(path, size):
    return ImageFont.truetype(path, size)


def tracked(draw, xy, text, f, fill, track=0):
    """Draw with letter-spacing. PIL has no tracking, and a wordmark without
    it reads as a terminal string rather than a name."""
    x, y = xy
    for ch in text:
        draw.text((x, y), ch, font=f, fill=fill)
        x += draw.textlength(ch, font=f) + track
    return x


def tracked_width(draw, text, f, track=0):
    return sum(draw.textlength(c, font=f) for c in text) + track * (len(text) - 1)


# --- title and end cards -----------------------------------------------------


def card(path, lines, sub=None, footer=None):
    """A full-frame card on Wake's own ground."""
    img = Image.new("RGB", (W, H), GROUND)
    d = ImageDraw.Draw(img)

    mark = font(BOLD, 132)
    body = font(LIGHT, 44)
    small = font(REG, 32)

    track = 18
    y = H // 2 - 190

    for i, line in enumerate(lines):
        f = mark if i == 0 else body
        t = track if i == 0 else 2
        w = tracked_width(d, line, f, t)
        tracked(d, ((W - w) / 2, y), line, f, TEXT if i == 0 else MUTED, t)
        y += 170 if i == 0 else 62

    if sub:
        y += 26
        for line in sub:
            w = tracked_width(d, line, body, 2)
            tracked(d, ((W - w) / 2, y), line, body, TEXT, 2)
            y += 62

    if footer:
        w = tracked_width(d, footer, small, 4)
        tracked(d, ((W - w) / 2, H - 190), footer, small, ACCENT, 4)

    img.save(path)


# --- lower-third captions ----------------------------------------------------


BAND = 176


def caption(path, lines, chip=None, top=False):
    """A caption in an opaque band, not a scrim over the terminal.

    A gradient was tried and is unreadable: the transcript fills the pane, so
    terminal text shows straight through the caption and neither reads. The
    opaque band is also what retires the stale `! labelling @alex…` notice —
    it covers the legend, awareness strip and notice rows, which is exactly
    where a staging artifact lands.

    `top` puts the band over the header instead, for the one beat where the
    bottom of the screen is the subject: ⌃O swaps `↵ send` to `↵ detach` in
    the legend, and covering that would cover what is being demonstrated.
    """
    img = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    y0 = 0 if top else H - BAND
    d.rectangle([0, y0, W, y0 + BAND], fill=(*GROUND, 255))
    rule = y0 + BAND - 3 if top else y0
    d.rectangle([0, rule, W, rule + 3], fill=(*ACCENT, 110))

    head = font(REG, 44)
    sub = font(LIGHT, 38)
    y = y0 + 40

    for i, line in enumerate(lines):
        f = head if i == 0 else sub
        tracked(d, (120, y), line, f, TEXT if i == 0 else MUTED, 1)
        y += 62

    if chip:
        cf = font(BOLD, 40)
        pad_x, pad_y = 26, 15
        tw = d.textlength(chip, font=cf)
        cw, ch = tw + pad_x * 2, 40 + pad_y * 2
        cx, cy = W - cw - 120, y0 + (BAND - ch) // 2
        d.rounded_rectangle([cx, cy, cx + cw, cy + ch], radius=12,
                            fill=(*ACCENT, 46), outline=(*ACCENT, 200), width=3)
        d.text((cx + pad_x, cy + pad_y - 3), chip, font=cf, fill=ACCENT)

    img.save(path)


# --- the film's text ---------------------------------------------------------

CAPTIONS = {
    "hook": (["Six Claude Code sessions.", "One room."], None),
    "start": (["`wake` opens the room.", "Every session lives in it."], None),
    "broadcast": (
        [
            "Type once. Every agent hears it.",
            "They stay quiet unless they have something to say.",
        ],
        "@all",
    ),
    "mention": (
        ["`@name` when you mean one of them.", "The room still sees the answer."],
        "@name",
    ),
    "blocked": (
        ["The roster ranks by who needs you.", "One key goes to whoever is blocked."],
        "^X",
    ),
    "answer": (
        ["Answered in the pane that asked —", "never in a modal over everything."],
        None,
    ),
    "dm": (
        ["Open one and you're in Claude Code.", "Same commands. Same rendering."],
        "^D",
    ),
    "grid": (
        ["Columns, each split once.", "Not a pane tree. Not a multiplexer."],
        "^Y  ^B",
    ),
    "fork": (
        [
            "Branch a conversation mid-thought.",
            "Two agents, one history, separate from here.",
        ],
        "^F",
    ),
    "manager": (
        [
            "Every room seats a manager.",
            "It can see the fleet, and send, interrupt and spawn.",
        ],
        "@manager",
    ),
    "fanout": (
        [
            '"Tell the agents working on the api…"',
            "It finds them and sends to each one.",
        ],
        None,
    ),
    "board": (
        ["The whole fleet, one row each.", "What every agent is doing, in one screen."],
        "/board",
    ),
    "leaving": (
        ["Leave, and they keep working.", "Come back to exactly what you left."],
        "^O",
    ),
}


def main():
    os.makedirs(OUT, exist_ok=True)

    card(
        os.path.join(OUT, "title.png"),
        ["Wake"],
        sub=["A terminal for running a fleet", "of Claude Code sessions."],
    )

    handle = sys.argv[1] if len(sys.argv) > 1 else "github.com/DilanDoshi/wake"
    card(
        os.path.join(OUT, "end.png"),
        ["Wake"],
        sub=["Run a fleet of Claude Code sessions", "like a team, not tabs."],
        footer=handle,
    )

    for name, (lines, chip) in CAPTIONS.items():
        caption(os.path.join(OUT, "cap-%s.png" % name), lines, chip,
                top=name == "leaving")

    print("cards written to", OUT)


if __name__ == "__main__":
    main()

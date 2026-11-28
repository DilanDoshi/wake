"""The landing site's icon: Wake's banner ring in the banner's accent, read from the Go source at each build.

An unreadable source is an error, never a fallback; test_favicon.py holds the result to a golden.
"""

import os
import re
import struct
import zlib
from collections import namedtuple

# pixels are the lit (x, y); width and height are the sprite box the banner declares.
Ring = namedtuple("Ring", "pixels width height")

# Half-block glyphs: which of a cell's two pixel rows (top, bottom) each one lights.
GLYPHS = {
    " ": (False, False),
    "▀": (True, False),
    "▄": (False, True),
    "█": (True, True),
}

# The ring plus a unit of margin: 32 and 48 px are whole pixels to its unit.
ICON_GRID = 8
# Two units of margin; iOS rounds the corners and fills transparency black.
APPLE_GRID = 10
APPLE_SIZE = 180
GROUND = "#faf8f6"  # the site's light --ground (test_page.py holds the two equal)

BANNER = os.path.join("internal", "ui", "banner.go")
THEME = os.path.join("internal", "ui", "theme.go")


def code(src: str) -> str:
    """Go source without its // comments, so a comment can neither be a declaration nor cut one short."""
    return re.sub(r"//[^\n]*", "", src)


def sprite_rows(banner_src: str) -> list:
    body = re.search(
        r"(?m)^[ \t]*spriteRows\s*=\s*\[\]string\{([^}]*)\}", code(banner_src)
    )
    if not body:
        raise ValueError("banner.go declares no spriteRows")
    if re.sub(r'"[^"]*"|,|\s', "", body.group(1)):
        raise ValueError(
            "banner.go's spriteRows is not a plain list of string literals"
        )
    rows = re.findall(r'"([^"]*)"', body.group(1))
    if not rows or len({len(r) for r in rows}) != 1:
        raise ValueError(
            "banner.go's spriteRows are missing or not all one width: %r" % rows
        )
    return rows


def lit_pixels(row: str, glyph_row: int):
    """The pixels one glyph row lights; a glyph row is two pixel rows."""
    for x, glyph in enumerate(row):
        if glyph not in GLYPHS:
            raise ValueError(
                "banner.go's sprite has a glyph this cannot draw: %r" % glyph
            )
        for half, on in enumerate(GLYPHS[glyph]):
            if on:
                yield (x, glyph_row * 2 + half)


def read_ring(banner_src: str) -> Ring:
    """The sprite the banner draws."""
    rows = sprite_rows(banner_src)
    pixels = frozenset(p for i, row in enumerate(rows) for p in lit_pixels(row, i))
    if not pixels:
        raise ValueError("banner.go's sprite lights no pixel")
    return Ring(pixels, len(rows[0]), 2 * len(rows))


def require_accent_sprite(banner_src: str) -> None:
    """The icon is coloured by theme.go's Accent, so the sprite must still be painted in it."""
    pattern = (
        r"(?m)^[ \t]*spriteStyle\s*=\s*lipgloss\.NewStyle\(\)\.Foreground\(Accent\)"
    )
    if not re.search(pattern, code(banner_src)):
        raise ValueError("banner.go no longer paints its sprite in Accent")


def accent(theme_src: str) -> str:
    """theme.go's Accent, one colour on both grounds."""
    hexc = r'"(#[0-9a-fA-F]{6})"'
    pattern = (
        r"(?m)^[ \t]*Accent\s*=\s*lipgloss\.AdaptiveColor\{Light:\s*%s,\s*Dark:\s*%s\}"
        % (hexc, hexc)
    )
    m = re.search(pattern, code(theme_src))
    if not m:
        raise ValueError("theme.go declares no Accent")
    if m.group(1).lower() != m.group(2).lower():
        raise ValueError("theme.go's Accent differs by ground: %s, %s" % m.groups())
    return m.group(1).lower()


def placed(ring: Ring, grid: int) -> frozenset:
    """The ring centred, by its declared box, on a grid-by-grid field of units."""
    if grid < max(ring.width, ring.height):
        raise ValueError(
            "a %dx%d ring does not fit a %d unit grid" % (ring.width, ring.height, grid)
        )
    dx, dy = (grid - ring.width) // 2, (grid - ring.height) // 2
    return frozenset((x + dx, y + dy) for x, y in ring.pixels)


def runs(pixels: frozenset) -> list:
    """Horizontal runs (x, y, width), so the path is a rect per run, not per pixel."""
    out = []
    for y in sorted({y for _, y in pixels}):
        xs = sorted(x for x, py in pixels if py == y)
        start = prev = xs[0]
        for x in xs[1:]:
            if x != prev + 1:
                out.append((start, y, prev - start + 1))
                start = x
            prev = x
        out.append((start, y, prev - start + 1))
    return out


def svg(ring: Ring, colour: str) -> str:
    d = "".join(
        "M%d %dh%dv1h-%dz" % (x, y, w, w) for x, y, w in runs(placed(ring, ICON_GRID))
    )
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">\n'
        '<path fill="%s" shape-rendering="crispEdges" d="%s"/>\n'
        "</svg>\n" % (ICON_GRID, ICON_GRID, colour, d)
    )


def rgba(hex_colour: str) -> bytes:
    return bytes(int(hex_colour[i : i + 2], 16) for i in (1, 3, 5)) + b"\xff"


def chunk(kind: bytes, body: bytes) -> bytes:
    return (
        struct.pack(">I", len(body))
        + kind
        + body
        + struct.pack(">I", zlib.crc32(kind + body))
    )


def png(ring: Ring, colour: str, grid: int, size: int, ground: str = "") -> bytes:
    """An 8-bit RGBA PNG of the ring, a whole number of pixels to the unit; no ground is transparent."""
    if size % grid:
        raise ValueError(
            "%d px is not a whole number of pixels to %d units" % (size, grid)
        )
    unit, on = size // grid, placed(ring, grid)
    off = rgba(ground) if ground else b"\x00\x00\x00\x00"
    ink = rgba(colour)
    raw = b"".join(
        b"\x00"
        + b"".join(ink if (x // unit, y // unit) in on else off for x in range(size))
        for y in range(size)
    )
    header = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


def files(root: str) -> dict:
    """Every icon the site carries, by file name, built from the repo checkout at root."""
    with open(os.path.join(root, BANNER), encoding="utf-8") as fh:
        banner_src = fh.read()
    with open(os.path.join(root, THEME), encoding="utf-8") as fh:
        theme_src = fh.read()
    require_accent_sprite(banner_src)
    ring, colour = read_ring(banner_src), accent(theme_src)
    return {
        "favicon.svg": svg(ring, colour).encode("utf-8"),
        "favicon-32.png": png(ring, colour, ICON_GRID, 32),
        "favicon-48.png": png(ring, colour, ICON_GRID, 48),
        "apple-touch-icon.png": png(ring, colour, APPLE_GRID, APPLE_SIZE, GROUND),
    }

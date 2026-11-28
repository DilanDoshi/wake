"""Tests for the site's icon (favicon.py): the banner's ring and colour, held to a hand-written golden.

    python3 -m unittest discover -s demo

Run by hand: no demo test is part of `make ci`, so a banner change reaches this golden only there.
"""

import os
import re
import struct
import sys
import tempfile
import unittest
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
sys.path.insert(0, HERE)

import favicon  # noqa: E402

# The banner's ring as its half-block glyphs draw it; # is a lit pixel.
GOLDEN_RING = (
    ".####.",
    "#....#",
    "#.##.#",
    "#....#",
    "#....#",
    ".####.",
)
ACCENT = "#d77757"
GROUND = "#faf8f6"  # the site's light --ground, behind the ring on the apple icon

FILES = {"favicon.svg", "favicon-32.png", "favicon-48.png", "apple-touch-icon.png"}


def lit(art):
    """The set of (x, y) a block of # and . draws."""
    return {(x, y) for y, row in enumerate(art) for x, c in enumerate(row) if c == "#"}


def read(*parts):
    with open(os.path.join(ROOT, *parts), encoding="utf-8") as fh:
        return fh.read()


def banner():
    return read("internal", "ui", "banner.go")


def sprite(*rows, tail=""):
    """A banner.go fragment declaring these glyph rows."""
    body = "".join('\t\t"%s",\n' % r for r in rows)
    return "spriteRows = []string{\n%s\t}\n%s" % (body, tail)


def rgba(hex_colour, alpha=255):
    return tuple(int(hex_colour[i : i + 2], 16) for i in (1, 3, 5)) + (alpha,)


def svg_pixels(svg):
    """The pixels an SVG draws, read back with no help from favicon.py.

    Accepts only the shape the builder promises (an svg holding one path of unit-high
    rects) so a stray element or a different path command is a failure, not a pass.
    """
    assert re.findall(r"<(/?\w+)", svg) == [
        "svg",
        "path",
        "/svg",
    ], "unexpected elements"
    d = re.search(r' d="([^"]*)"', svg).group(1)
    out = set()
    for command in filter(None, d.split("z")):
        m = re.fullmatch(r"M(\d+) (\d+)h(\d+)v1h-(\d+)", command)
        assert m and m.group(3) == m.group(4), "unexpected path command: %r" % command
        x, y, width = int(m.group(1)), int(m.group(2)), int(m.group(3))
        out |= {(x + i, y) for i in range(width)}
    return out


def decode_png(data):
    """(width, height, rows of RGBA tuples) of an 8-bit RGBA, unfiltered, uninterlaced PNG."""
    assert data[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG"
    pos, idat, size, last = 8, b"", None, None
    while pos < len(data):
        (length,) = struct.unpack(">I", data[pos : pos + 4])
        kind, body = data[pos + 4 : pos + 8], data[pos + 8 : pos + 8 + length]
        (crc,) = struct.unpack(">I", data[pos + 8 + length : pos + 12 + length])
        assert crc == zlib.crc32(kind + body), "bad CRC on " + kind.decode()
        if kind == b"IHDR":
            w, h, depth, colour, compression, filtering, interlace = struct.unpack(
                ">IIBBBBB", body
            )
            assert (depth, colour, compression, filtering, interlace) == (8, 6, 0, 0, 0)
            size = (w, h)
        elif kind == b"IDAT":
            idat += body
        pos, last = pos + 12 + length, kind
    assert last == b"IEND" and pos == len(data), "the file does not end with IEND"
    w, h = size
    raw = zlib.decompress(idat)
    stride = 1 + w * 4
    assert (
        len(raw) == h * stride
    ), "the pixel data is not exactly the rows the header names"
    rows = []
    for y in range(h):
        line = raw[y * stride : (y + 1) * stride]
        assert line[0] == 0, "filter byte is not None"
        rows.append([tuple(line[1 + x * 4 : 5 + x * 4]) for x in range(w)])
    return w, h, rows


class TheRing(unittest.TestCase):
    def test_it_is_the_ring_the_banner_draws(self):
        ring = favicon.read_ring(banner())
        self.assertEqual(set(ring.pixels), lit(GOLDEN_RING))
        self.assertEqual((ring.width, ring.height), (6, 6))

    def test_it_follows_whatever_the_banner_declares(self):
        ring = favicon.read_ring(sprite("██", "▀▄"))
        # █ is two pixels in its column, ▀ the top one and ▄ the bottom one.
        self.assertEqual(
            set(ring.pixels), {(0, 0), (1, 0), (0, 1), (1, 1), (0, 2), (1, 3)}
        )
        self.assertEqual((ring.width, ring.height), (2, 4))

    def test_a_banner_without_a_sprite_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.read_ring("package ui\n")

    def test_a_ragged_sprite_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.read_ring(sprite("██", "█"))

    def test_a_glyph_it_cannot_draw_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.read_ring(sprite("█x"))

    def test_a_sprite_with_nothing_lit_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.read_ring(sprite("  ", "  "))

    def test_a_brace_in_a_comment_inside_the_sprite_does_not_cut_it_short(self):
        src = 'spriteRows = []string{\n\t\t"██", // top }\n\t\t"▀▄", // {x}\n\t}\n'
        self.assertEqual(favicon.read_ring(src), favicon.read_ring(sprite("██", "▀▄")))

    def test_a_comment_that_mentions_the_sprite_is_not_the_sprite(self):
        src = '// spriteRows = []string{"▄▄"}\n' + sprite("██")
        self.assertEqual(favicon.read_ring(src), favicon.read_ring(sprite("██")))

    def test_a_similarly_named_declaration_is_not_the_sprite(self):
        src = 'altspriteRows = []string{\n\t\t"▄▄",\n\t}\n' + sprite("██")
        self.assertEqual(favicon.read_ring(src), favicon.read_ring(sprite("██")))

    def test_a_quoted_word_in_a_comment_is_not_a_row(self):
        src = 'spriteRows = []string{\n\t\t// "xx" is not a row\n\t\t"██",\n\t}\n'
        self.assertEqual(favicon.read_ring(src), favicon.read_ring(sprite("██")))

    def test_rows_built_by_concatenation_are_refused(self):
        with self.assertRaises(ValueError):
            favicon.read_ring('spriteRows = []string{\n\t\t"█" + "█",\n\t}\n')

    def test_a_sprite_too_big_for_the_grid_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.placed(favicon.read_ring(sprite("█" * 9)), 8)

    def test_it_is_centred_by_the_box_it_declares_not_the_pixels_it_lights(self):
        ring = favicon.read_ring(
            sprite("█  ")
        )  # one lit column in a box 3 wide and 2 high
        # Centred on 8 units that is (8 - 3) // 2 = 2 across; the lit-pixel box would say 3.
        self.assertEqual(
            set(favicon.placed(ring, 8)), {(2, 3), (2, 4)}
        )  # and (8 - 2) // 2 = 3 down


class TheColour(unittest.TestCase):
    def test_it_is_the_themes_accent(self):
        self.assertEqual(favicon.accent(read("internal", "ui", "theme.go")), ACCENT)

    def test_it_follows_whatever_the_theme_declares(self):
        src = 'Accent = lipgloss.AdaptiveColor{Light: "#112233", Dark: "#112233"}'
        self.assertEqual(favicon.accent(src), "#112233")

    def test_an_accent_that_differs_by_ground_is_refused(self):
        src = 'Accent = lipgloss.AdaptiveColor{Light: "#112233", Dark: "#445566"}'
        with self.assertRaises(ValueError):
            favicon.accent(src)

    def test_a_theme_without_an_accent_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.accent("package ui\n")

    def test_a_similarly_named_colour_is_not_the_accent(self):
        real = 'Accent = lipgloss.AdaptiveColor{Light: "#112233", Dark: "#112233"}'
        wrong = (
            'SubAccent = lipgloss.AdaptiveColor{Light: "#111111", Dark: "#111111"}\n'
        )
        self.assertEqual(favicon.accent(wrong + real), "#112233")

    def test_a_commented_out_accent_is_not_the_accent(self):
        real = 'Accent = lipgloss.AdaptiveColor{Light: "#112233", Dark: "#112233"}'
        old = '// Accent = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#000000"}\n'
        self.assertEqual(favicon.accent(old + real), "#112233")

    def test_the_banner_still_paints_its_sprite_in_that_accent(self):
        favicon.require_accent_sprite(banner())  # does not raise

    def test_a_sprite_painted_in_another_colour_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.require_accent_sprite(
                "spriteStyle = lipgloss.NewStyle().Foreground(Warn)\n"
            )

    def test_a_banner_with_no_sprite_style_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.require_accent_sprite("package ui\n")


class TheSvg(unittest.TestCase):
    def setUp(self):
        self.svg = favicon.svg(favicon.read_ring(banner()), ACCENT)

    def test_it_draws_the_ring_one_unit_in_on_an_eight_unit_grid(self):
        self.assertIn('viewBox="0 0 8 8"', self.svg)
        self.assertEqual(
            svg_pixels(self.svg), {(x + 1, y + 1) for x, y in lit(GOLDEN_RING)}
        )

    def test_it_is_orange_crisp_and_has_no_tile(self):
        self.assertIn('fill="%s"' % ACCENT, self.svg)
        self.assertIn('shape-rendering="crispEdges"', self.svg)
        self.assertNotIn("<rect", self.svg)


class ThePngs(unittest.TestCase):
    def setUp(self):
        self.files = favicon.files(ROOT)

    def test_the_site_gets_exactly_these_four_files(self):
        self.assertEqual(set(self.files), FILES)

    def test_the_favicons_are_the_ring_on_nothing(self):
        for name, size in (("favicon-32.png", 32), ("favicon-48.png", 48)):
            with self.subTest(name):
                w, h, rows = decode_png(self.files[name])
                self.assertEqual((w, h), (size, size))
                unit = size // 8
                for y in range(size):
                    for x in range(size):
                        on = (x // unit - 1, y // unit - 1) in lit(GOLDEN_RING)
                        want = rgba(ACCENT) if on else (0, 0, 0, 0)
                        self.assertEqual(rows[y][x], want, "pixel %d,%d" % (x, y))

    def test_the_apple_icon_is_the_ring_on_the_ground_with_no_transparency(self):
        w, h, rows = decode_png(self.files["apple-touch-icon.png"])
        self.assertEqual((w, h), (180, 180))
        unit = 180 // 10  # the ring is six units on a ten-unit ground: 36 px of margin
        for y in range(h):
            for x in range(w):
                on = (x // unit - 2, y // unit - 2) in lit(GOLDEN_RING)
                want = rgba(ACCENT) if on else rgba(GROUND)
                self.assertEqual(rows[y][x], want, "pixel %d,%d" % (x, y))

    def test_two_builds_in_one_process_agree(self):
        # Not a claim about other machines: another zlib may deflate the same pixels to other bytes.
        self.assertEqual(favicon.files(ROOT), self.files)

    def test_the_svg_file_is_the_svg(self):
        ring = favicon.read_ring(banner())
        self.assertEqual(
            self.files["favicon.svg"], favicon.svg(ring, ACCENT).encode("utf-8")
        )

    def test_a_size_that_is_not_whole_pixels_to_the_unit_is_refused(self):
        with self.assertRaises(ValueError):
            favicon.png(favicon.read_ring(banner()), ACCENT, 8, 36)


class TheFiles(unittest.TestCase):
    def checkout(self, banner_edit=lambda src: src):
        """A scratch checkout holding only the two Go files, the banner's edited."""
        root = tempfile.TemporaryDirectory()
        self.addCleanup(root.cleanup)
        os.makedirs(os.path.join(root.name, "internal", "ui"))
        for name, edit in (("banner.go", banner_edit), ("theme.go", lambda src: src)):
            with open(
                os.path.join(root.name, "internal", "ui", name), "w", encoding="utf-8"
            ) as fh:
                fh.write(edit(read("internal", "ui", name)))
        return root.name

    def test_an_unedited_checkout_builds(self):
        self.assertEqual(set(favicon.files(self.checkout())), FILES)

    def test_a_banner_that_stops_painting_in_the_accent_stops_the_build(self):
        root = self.checkout(
            lambda src: src.replace("Foreground(Accent)", "Foreground(Warn)")
        )
        with self.assertRaises(ValueError):
            favicon.files(root)

    def test_a_checkout_with_no_banner_is_an_error_not_a_fallback(self):
        with tempfile.TemporaryDirectory() as empty:
            with self.assertRaises(OSError):
                favicon.files(empty)


if __name__ == "__main__":
    unittest.main()

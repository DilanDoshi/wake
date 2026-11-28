#!/usr/bin/env python3
"""Builds the landing site: the beat clips beside the claim each proves.

    python3 page.py /path/to/wake-landing

writes index.html (the story), features.html (every beat and the build notes),
releases.html (the GitHub releases, rendered at build time), media/ (one
mp4 and one poster per beat), what a search engine reads first (robots.txt,
sitemap.xml) and the icons a browser asks for (favicon.svg and its PNGs, built
by favicon.py) into that checkout. --pages-only does all of it but the media.
The site is served under a CSP that allows same-origin media and no external
hosts but fonts, so clips are files beside the pages and the release notes are
baked in rather than fetched. Edit this file and pagecopy.py, never the output.
"""

import html
import json
import os
import subprocess
import sys
from typing import Callable

import favicon
from pagecopy import (
    ALSO,
    FRIENDS,
    FRIENDS_HEADING,
    FRIENDS_PROSE,
    META,
    POSTERS,
    SHOTS,
    Friend,
    Shot,
)

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
OUT = os.path.join(HERE, ".work", "out")
REPO = "DilanDoshi/wake"
GITHUB = "https://github.com/" + REPO
LATEST = GITHUB + "/releases/latest"
# Where the site is served: canonical links, the sitemap and the share image
# are absolute. Change it here when the site moves to its own domain.
SITE = "https://wake-landing-rouge.vercel.app"
INSTALL = (
    "curl -fsSL https://raw.githubusercontent.com/%s/main/scripts/install.sh | sh"
    % REPO
)
# goreleaser's default changelog, the shape RELEASING.md says not to repeat:
# shown folded, since it is a commit list rather than notes.
AUTO_CHANGELOG = "## Changelog"


# A recording may fall this many seconds short of a cut: VHS rounds a take's length.
CUT_TOLERANCE = 0.25


def run(args: list[str]) -> None:
    subprocess.run(args, check=True)


def poster_at(clip: str, dur: float) -> float:
    """Where in the cut clip its poster frame is taken: the frame pagecopy names for
    it, else three fifths in. A poster is all a visitor sees until the clip plays."""
    return POSTERS.get(clip, dur * 0.6)


def recorded_length(clip: str) -> float:
    """A recording's length in seconds, as ffprobe reads it."""
    out = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-show_entries",
            "format=duration",
            "-of",
            "csv=p=0",
            os.path.join(OUT, clip + ".mp4"),
        ],
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    return float(out)


def short_cuts(length_of: Callable[[str], float]) -> list[str]:
    """The cuts a recording cannot cover: ffmpeg would write a short clip and no
    poster frame, or leave an old poster in place, and the page would not say."""
    cuts = [(s.clip, s.start, s.dur) for s in SHOTS]
    cuts += [(f.clip, f.start, f.dur) for f in FRIENDS]
    problems = []
    for clip, start, dur in cuts:
        try:
            have = length_of(clip)
        except (FileNotFoundError, subprocess.CalledProcessError):
            problems.append(
                f"{clip}: no recording at {os.path.join(OUT, clip + '.mp4')}"
            )
            continue
        if start + dur > have + CUT_TOLERANCE:
            problems.append(
                f"{clip}: the cut wants {start + dur:.1f}s ({start} + {dur}) "
                f"but the recording is {have:.1f}s"
            )
    return problems


def encode(clip: str, start: float, dur: float, media: str, crop: str = "") -> int:
    """One clip and its poster frame, at the size it was filmed: a smaller copy
    is what a retina screen then blurs. Terminal video is mostly static, so it
    survives a low bitrate far better than camera footage would. crop, ffmpeg's
    w:h:x:y, cuts the friends' band out of the frame."""
    src = os.path.join(OUT, clip + ".mp4")
    dst = os.path.join(media, clip + ".mp4")
    run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-ss",
            str(start),
            "-t",
            str(dur),
            "-i",
            src,
            *(["-vf", "crop=" + crop] if crop else []),
            "-an",
            "-c:v",
            "libx264",
            "-crf",
            "28",
            "-preset",
            "slow",
            "-pix_fmt",
            "yuv420p",
            "-movflags",
            "+faststart",
            "-y",
            dst,
        ]
    )
    poster = os.path.join(media, clip + ".jpg")
    run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-ss",
            str(poster_at(clip, dur)),
            "-i",
            dst,
            "-frames:v",
            "1",
            # Only shown until the clip plays, and fetched with the page.
            "-vf",
            "scale=1920:-2",
            "-q:v",
            "5",
            "-y",
            poster,
        ]
    )
    return os.path.getsize(dst) + os.path.getsize(poster)


def shot_section(shot: Shot) -> str:
    return f"""
    <section id="{shot.clip}">
      <div class="attr"><span class="glyph">{shot.glyph}</span>{shot.who} <span class="lbl">&lt;&gt; {shot.label}</span></div>
      <h2>{shot.heading}</h2>
      <p>{shot.prose}</p>
      <figure>
        <div class="screen">
          <video muted loop playsinline preload="none" poster="media/{shot.clip}.jpg">
            <source src="media/{shot.clip}.mp4" type="video/mp4">
          </video>
        </div>
        <figcaption>Recorded from the running binary — real daemon, real room, real protocol. Click to enlarge.</figcaption>
      </figure>
    </section>"""


def friend_figure(friend: Friend) -> str:
    width, height = friend.crop.split(":")[:2]
    return f"""
      <figure>
        <div class="screen">
          <video muted loop playsinline preload="none" poster="media/{friend.clip}.jpg" style="aspect-ratio:{width}/{height}">
            <source src="media/{friend.clip}.mp4" type="video/mp4">
          </video>
        </div>
        <figcaption>{friend.caption}</figcaption>
      </figure>"""


def friends_section() -> str:
    clips = "".join(friend_figure(f) for f in FRIENDS)
    return f"""
    <section id="friends">
      <div class="attr"><span class="glyph">·</span>friends <span class="lbl">&lt;&gt; on the usage bar</span></div>
      <h2>{FRIENDS_HEADING}</h2>
      <p>{FRIENDS_PROSE}</p>{clips}
    </section>"""


def note(title: str, prose: str) -> str:
    return f"""
      <div class="note">
        <h3>{title}</h3>
        <p>{prose}</p>
      </div>"""


def nav(active: str) -> str:
    def link(href: str, label: str) -> str:
        cur = ' aria-current="page"' if href == active else ""
        return f'<a href="{href}"{cur}>{label}</a>'

    return f"""
<nav class="top"><div class="wrap">
  <a class="brand" href="index.html">WAKE</a>
  {link("features.html", "Features")}
  {link("releases.html", "Releases")}
  <a href="{GITHUB}">GitHub</a>
  <a class="dl" href="{LATEST}">Download</a>
</div></nav>"""


def url_of(active: str) -> str:
    """The URL Vercel's cleanUrls serves a page at, which is its canonical one."""
    return SITE + (
        "/" if active == "index.html" else "/" + active.removesuffix(".html")
    )


def software_ld() -> str:
    """The front page as schema.org's SoftwareApplication."""
    data = {
        "@context": "https://schema.org",
        "@type": "SoftwareApplication",
        "name": "Wake",
        "description": META["index.html"][1],
        "url": url_of("index.html"),
        "downloadUrl": LATEST,
        "applicationCategory": "DeveloperApplication",
        "operatingSystem": "macOS, Linux",
        "license": "https://www.apache.org/licenses/LICENSE-2.0",
        "offers": {"@type": "Offer", "price": "0", "priceCurrency": "USD"},
        "sameAs": [GITHUB],
    }
    return '<script type="application/ld+json">%s</script>' % json.dumps(data)


def page(active: str, body: str, head: str = "") -> str:
    title, description = (html.escape(s) for s in META[active])
    url = url_of(active)
    return f"""<!DOCTYPE html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title}</title>
<meta name="description" content="{description}">
<link rel="canonical" href="{url}">
<link rel="icon" href="favicon.svg" type="image/svg+xml" sizes="any">
<link rel="icon" href="favicon-32.png" type="image/png" sizes="32x32">
<link rel="icon" href="favicon-48.png" type="image/png" sizes="48x48">
<link rel="apple-touch-icon" href="apple-touch-icon.png">
<meta property="og:type" content="website">
<meta property="og:site_name" content="Wake">
<meta property="og:title" content="{title}">
<meta property="og:description" content="{description}">
<meta property="og:url" content="{url}">
<meta property="og:image" content="{SITE}/media/{SHOTS[0].clip}.jpg">
<meta property="og:image:alt" content="Wake's ⌃F finder over the room: a few letters of a name and any agent in the fleet is one key away">
<meta name="twitter:card" content="summary_large_image">{head}
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Newsreader:ital,opsz,wght@0,6..72,300..600;1,6..72,300..500&family=JetBrains+Mono:wght@400;500;700&display=swap">
<style>{CSS}</style>
{nav(active)}
{body}
<footer class="wrap">
  <p class="mono" style="font-size:14px;color:var(--muted)"><a href="{GITHUB}">github.com/{REPO}</a></p>
  <p class="mono" style="font-size:12.5px;color:var(--muted);margin-top:14px">
    Wake is not affiliated with, endorsed by, or sponsored by Anthropic. Claude and
    Claude&nbsp;Code are trademarks of Anthropic.
  </p>
</footer>
<script>{JS}</script>
"""


def hero() -> str:
    return f"""
<header class="hero">
  <div class="wrap">
    <h1 class="mark">Wake</h1>
    <p class="thesis">A terminal for running a fleet of Claude&nbsp;Code sessions.</p>
    <p class="sub">Fifty agents is not fifty terminal tabs. Wake turns the fleet into a room:
      one group chat as the primary surface, <code>@name</code> to reach one, <code>@team</code> to
      reach a group, team channels that show one team's messages, a manager that acts on the fleet
      for you, a roster that ranks agents by whether they need you and marks the ones that are done,
      <code>⌃F</code> to find any agent on the roster in a few letters of its name, drag-to-place panes, and any
      agent openable as a full conversation at Claude&nbsp;Code fidelity. Wake V1 is out.</p>
    <div class="meta">
      <a class="pill" href="{LATEST}"><b>Download</b> — latest release&nbsp;→</a>
      <span class="pill req"><b>requires Claude&nbsp;Code</b> — installed and signed in</span>
      <span class="pill"><b>Go</b> · Bubble&nbsp;Tea</span>
      <span class="pill">no screen-scraping — <b>structured JSON only</b></span>
      <span class="pill">stream-json over a <b>daemon socket</b></span>
    </div>
  </div>
</header>"""


def subhead(eyebrow: str, title: str, sub: str, extra: str = "") -> str:
    return f"""
<header class="subhead">
  <div class="wrap">
    <div class="attr"><span class="glyph">·</span>{eyebrow}</div>
    <h1 class="title">{title}</h1>
    <p class="sub">{sub}</p>{extra}
  </div>
</header>"""


def more_section() -> str:
    count = len(SHOTS) + len(FRIENDS)
    return f"""
  <section>
    <div class="attr"><span class="glyph">·</span>there's more <span class="lbl">&lt;&gt; {count} recordings in all</span></div>
    <h2>Everything else Wake does.</h2>
    <div class="cta">
      <a class="card" href="features.html"><h3>Features →</h3>
        <p>Every recording, including narrowing the room, done marks, questions, <code>!cmd</code>,
           <code>/wake-help</code> and workflows — and the smaller things that landed with no clip
           of their own.</p></a>
      <a class="card" href="releases.html"><h3>Releases →</h3>
        <p>What changed in each version, newest first, straight from the release notes.</p></a>
    </div>
  </section>"""


def requirements_section() -> str:
    return f"""
  <section>
    <div class="attr"><span class="glyph">·</span>what you need <span class="lbl">&lt;&gt; before the first wake</span></div>
    <h2>Wake runs on your Claude&nbsp;Code.</h2>
    <p>Wake is not a model and not a service. Every agent is your own <code>claude</code> running
      headless, spawned and supervised by Wake — so Claude&nbsp;Code has to work from your terminal
      before Wake can do anything, and every turn bills to whatever plan or API key Claude&nbsp;Code
      is already using.</p>
    <div class="notes">{
        note(
            "Claude Code, signed in",
            "Installed, on your <code>PATH</code>, and authenticated — a Claude subscription or an "
            "API key that Claude&nbsp;Code accepts. Wake adds no model access of its own. "
            "<code>/login</code> inside the room shows the auth status it found.",
        )
    }{
        note(
            "Install",
            f'<code>{INSTALL}</code> installs the <a href="{LATEST}">latest release</a> for macOS or '
            "Linux and puts <code>wake</code> on your <code>PATH</code>; <code>wake upgrade</code> keeps "
            "it current. Run <code>wake</code> in a project: a daemon starts and the room opens "
            "with the manager alone — ask it for agents, or start one with <code>/new</code>.",
        )
    }{
        note(
            "Any terminal",
            "Wake is not a multiplexer and asks nothing of the terminal but a tty. "
            "<code>wake setup-terminal</code> teaches Ghostty, Kitty and Alacritty to send "
            "<code>⇧↵</code> as a newline and <code>⌘←</code>/<code>⌘→</code> as line start and "
            "end, which they cannot do unaided.",
        )
    }
    </div>
  </section>"""


def also_section() -> str:
    notes = "".join(note(t, p) for t, p in ALSO)
    return f"""
  <section>
    <div class="attr"><span class="glyph">·</span>also in the build</div>
    <h2>The rest of what landed.</h2>
    <div class="notes dense">{notes}
    </div>
  </section>"""


def built_section() -> str:
    return f"""
  <section>
    <div class="attr"><span class="glyph">·</span>how it's built <span class="lbl">&lt;&gt; the parts that matter</span></div>
    <h2>The constraints are the design.</h2>
    <div class="notes">{
        note(
            "Never screen-scraped",
            "Every agent is a headless <code>claude</code> in stream-json mode with a Wake-assigned "
            "session id. All state arrives as structured JSON on stdout — nothing is read off a "
            "rendered screen.",
        )
    }{
        note(
            "One airlock",
            "Exactly eight files are allowed to know Claude's JSON. Everything above them sees Wake's "
            "own event type, and a test holds the file set so another cannot be added quietly.",
        )
    }{
        note(
            "Cheap to leave open",
            "No work per frame that could be work per change, and no polling. A streamed answer is a "
            "preview, never a record — re-rendering each token measured 65× the cost.",
        )
    }{
        note(
            "Wake owns almost no state",
            "Claude persists the transcripts; Wake reads one back when a conversation opens. It keeps "
            "only a roster, a park book, groups, layout and the friend you picked — so it can crash "
            "and lose nothing.",
        )
    }
    </div>
    <div class="disclosure">
      <p>Every frame here is the real binary in a real terminal — the daemon, the room, the
        rendering and the wire protocol are all genuine, and the git branch in the status bar is a
        real branch. What is scripted is <em>what the models say</em>: the recordings drive a
        stand-in <code>claude</code> so takes are deterministic, cost nothing, and cannot leak
        anything from the machine that recorded them.</p>
    </div>
  </section>"""


def fetch_releases() -> list[dict]:
    """Every published release, notes rendered to HTML by GitHub itself."""
    raw = subprocess.run(
        [
            "gh",
            "api",
            "-H",
            "Accept: application/vnd.github.full+json",
            "repos/%s/releases" % REPO,
            "--paginate",
        ],
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    return [r for r in json.loads(raw) if not r["draft"]]


def release_article(rel: dict, latest: bool) -> str:
    tag = html.escape(rel["tag_name"])
    date = rel["published_at"][:10]
    badge = ' <span class="badge">latest</span>' if latest else ""
    body = rel["body_html"]
    if (rel.get("body") or "").lstrip().startswith(AUTO_CHANGELOG):
        body = f"<details><summary>Commit list</summary>{body}</details>"
    return f"""
  <article class="release" id="{tag}">
    <div class="attr"><span class="glyph">·</span>{date}</div>
    <h2><a href="{html.escape(rel["html_url"])}">{tag}</a>{badge}</h2>
    <div class="rel-body">{body}</div>
  </article>"""


def write(path: str, text: str) -> None:
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text)


def write_bytes(path: str, data: bytes) -> None:
    with open(path, "wb") as fh:
        fh.write(data)


def write_releases(dest: str, rels: list[dict]) -> None:
    # GitHub's own "latest": the newest that is not a prerelease, as the Download link resolves.
    latest = next((r["tag_name"] for r in rels if not r["prerelease"]), None)
    articles = "".join(release_article(r, r["tag_name"] == latest) for r in rels)
    write(
        os.path.join(dest, "releases.html"),
        page(
            "releases.html",
            subhead(
                'releases <span class="lbl">&lt;&gt; newest first</span>',
                "What changed, version by version.",
                "Install the newest with the one-line installer, or run "
                "<code>wake upgrade</code> on an existing install.",
                f'<pre class="install"><code>{INSTALL}</code></pre>',
            )
            + f'<main class="wrap">{articles}</main>',
        ),
    )


def index_page() -> str:
    home = "".join(shot_section(s) for s in SHOTS if s.home) + friends_section()
    return page(
        "index.html",
        hero()
        + f'<main class="wrap">{home}{more_section()}{requirements_section()}{built_section()}</main>',
        software_ld(),
    )


def features_page() -> str:
    every = "".join(shot_section(s) for s in SHOTS)
    return page(
        "features.html",
        subhead(
            'features <span class="lbl">&lt;&gt; every recording</span>',
            "Everything Wake does, recorded.",
            "Each clip is the real <code>wake</code> binary in a real terminal, driving a "
            "scripted fleet. Click one to watch it full screen.",
        )
        + f'<main class="wrap">{every}{friends_section()}{also_section()}</main>',
    )


def write_crawl_files(dest: str, icons: dict) -> None:
    """What a crawler reads before any page, and the icons a browser asks for."""
    urls = "".join(f"  <url><loc>{url_of(p)}</loc></url>\n" for p in META)
    write(
        os.path.join(dest, "sitemap.xml"),
        '<?xml version="1.0" encoding="UTF-8"?>\n'
        '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n'
        f"{urls}</urlset>\n",
    )
    write(
        os.path.join(dest, "robots.txt"),
        f"User-agent: *\nAllow: /\n\nSitemap: {SITE}/sitemap.xml\n",
    )
    for name, data in icons.items():
        write_bytes(os.path.join(dest, name), data)


def encode_media(dest: str) -> int:
    """Every clip and poster into dest/media; the bytes written."""
    problems = short_cuts(recorded_length)
    if problems:
        sys.exit("recordings that do not cover their cuts:\n  " + "\n  ".join(problems))

    media = os.path.join(dest, "media")
    os.makedirs(media, exist_ok=True)
    total = 0
    for shot in SHOTS:
        size = encode(shot.clip, shot.start, shot.dur, media)
        total += size
        print("  %-18s %7.1f KB" % (shot.clip, size / 1024))
    for friend in FRIENDS:
        size = encode(friend.clip, friend.start, friend.dur, media, friend.crop)
        total += size
        print("  %-18s %7.1f KB" % (friend.clip, size / 1024))
    return total


USAGE = "usage: page.py <wake-landing checkout> [--releases-only | --pages-only]"


def parse_args(args: list) -> tuple:
    """(checkout, releases_only, pages_only), or an exit that says what was wrong."""
    flags = [a for a in args if a.startswith("--")]
    paths = [a for a in args if not a.startswith("--")]
    unknown = [a for a in flags if a not in ("--releases-only", "--pages-only")]
    if unknown:
        sys.exit("unknown option %s\n%s" % (unknown[0], USAGE))
    if "--releases-only" in flags and "--pages-only" in flags:
        sys.exit(
            "--pages-only and --releases-only are exclusive: one rewrites every page, the other releases.html alone"
        )
    if len(paths) != 1:
        sys.exit(USAGE)
    return paths[0], "--releases-only" in flags, "--pages-only" in flags


def build_icons() -> dict:
    try:
        return favicon.files(ROOT)
    except (OSError, ValueError) as err:
        sys.exit("cannot build the site's icon: %s" % err)


def main() -> None:
    # --releases-only: a release touches only releases.html, and the clips are not
    # re-encoded from whatever recordings this machine holds (docs/RELEASING.md).
    # --pages-only: every page and icon, for a change in a page's head or words
    # and not in a clip; the clips its pages link to must already be there.
    dest, releases_only, pages_only = parse_args(sys.argv[1:])
    if pages_only and not os.path.isdir(os.path.join(dest, "media")):
        sys.exit(
            "%s/media not found: --pages-only leaves the clips alone, so they must be there"
            % dest
        )
    # First, so an icon source that cannot be read, or a gh that is missing or
    # signed out, fails before any page is written.
    icons = {} if releases_only else build_icons()
    rels = fetch_releases()
    if releases_only:
        write_releases(dest, rels)
        print("releases: %s/releases.html — %d releases" % (dest, len(rels)))
        return

    total = 0 if pages_only else encode_media(dest)

    write(os.path.join(dest, "index.html"), index_page())
    write(os.path.join(dest, "features.html"), features_page())
    write_releases(dest, rels)
    write_crawl_files(dest, icons)

    if pages_only:
        print("\npages only: %s — media untouched, %d releases" % (dest, len(rels)))
        return
    print(
        "\nsite: %s — %d beats, %.1f MB of media, %d releases"
        % (dest, len(SHOTS) + len(FRIENDS), total / 1e6, len(rels))
    )


CSS = """
:root{
  --ground:#faf8f6; --surface:#fffdfc; --ink:#1a1614; --muted:#6f655f;
  --rule:#e8dfd9; --accent:#c2603f; --accent-soft:#f0d9cf; --shadow:24px 24px 0 -12px #efe6e0;
}
@media (prefers-color-scheme:dark){
  :root:not([data-theme="light"]){
    --ground:#141313; --surface:#1b1918; --ink:#f2efec; --muted:#9a918b;
    --rule:#2e2a28; --accent:#d77757; --accent-soft:#3a2620; --shadow:24px 24px 0 -12px #201d1b;
  }
}
:root[data-theme="dark"]{
  --ground:#141313; --surface:#1b1918; --ink:#f2efec; --muted:#9a918b;
  --rule:#2e2a28; --accent:#d77757; --accent-soft:#3a2620; --shadow:24px 24px 0 -12px #201d1b;
}

*{box-sizing:border-box}
html{scroll-padding-top:72px}
body{
  margin:0; background:var(--ground); color:var(--ink);
  font-family:Newsreader,Georgia,"Times New Roman",serif;
  font-size:19px; line-height:1.62; -webkit-font-smoothing:antialiased;
}
.mono{font-family:"JetBrains Mono",ui-monospace,SFMono-Regular,Menlo,monospace}
code{font-family:"JetBrains Mono",ui-monospace,Menlo,monospace;font-size:.86em;
  background:var(--accent-soft); color:var(--accent); padding:.1em .35em; border-radius:3px}

.wrap{max-width:1080px;margin:0 auto;padding:0 32px}

/* Nav ------------------------------------------------------------------- */
nav.top{position:sticky;top:0;z-index:5;border-bottom:1px solid var(--rule);
  background:color-mix(in srgb,var(--ground) 90%,transparent);backdrop-filter:blur(8px)}
nav.top .wrap{display:flex;align-items:center;gap:24px;height:58px;
  font-family:"JetBrains Mono",monospace;font-size:13px;letter-spacing:.04em}
nav.top a{color:var(--muted);text-decoration:none}
nav.top a:hover,nav.top a[aria-current]{color:var(--ink)}
nav.top .brand{color:var(--ink);font-weight:700;letter-spacing:.22em;margin-right:auto}
nav.top .dl{color:var(--ground);background:var(--accent);padding:6px 14px;border-radius:999px}
nav.top .dl:hover{color:var(--ground);filter:brightness(1.08)}

/* Hero ------------------------------------------------------------------ */
header.hero{padding:96px 0 56px;border-bottom:1px solid var(--rule)}
header.subhead{padding:80px 0 44px;border-bottom:1px solid var(--rule)}
.mark{font-family:"JetBrains Mono",monospace;font-weight:700;font-size:clamp(52px,8vw,86px);
  letter-spacing:.22em;margin:0 0 4px;text-transform:none}
.title{font-size:clamp(34px,4.6vw,54px);line-height:1.15;margin:0;font-weight:500;
  text-wrap:balance;max-width:22ch}
.thesis{font-size:clamp(22px,2.6vw,30px);line-height:1.34;max-width:22ch;margin:18px 0 0;
  text-wrap:balance;font-weight:400}
.sub{color:var(--muted);max-width:62ch;margin:22px 0 0}
.meta{display:flex;flex-wrap:wrap;gap:10px;margin-top:30px}
.pill{font-family:"JetBrains Mono",monospace;font-size:12.5px;letter-spacing:.06em;
  border:1px solid var(--rule);border-radius:999px;padding:6px 13px;color:var(--muted)}
.pill b{color:var(--accent);font-weight:500}
.pill.req{border-color:var(--accent);color:var(--ink)}
a.pill{text-decoration:none;background:var(--accent);border-color:var(--accent);color:var(--ground)}
a.pill b{color:inherit}

/* Sections -------------------------------------------------------------- */
section{padding:76px 0;border-bottom:1px solid var(--rule)}
.attr{font-family:"JetBrains Mono",monospace;font-size:13.5px;letter-spacing:.04em;
  color:var(--accent);margin-bottom:18px}
.attr .glyph{opacity:.85;margin-right:.5em}
.attr .lbl{color:var(--muted)}
h2{font-size:clamp(28px,3.4vw,40px);line-height:1.2;margin:0 0 16px;font-weight:500;
  text-wrap:balance;max-width:20ch}
section p{max-width:64ch;margin:0 0 30px;color:var(--ink)}

figure{margin:0}
.screen{position:relative;border:1px solid var(--rule);border-radius:10px;overflow:hidden;
  background:#141313;box-shadow:var(--shadow);
  /* Out of the reading column: prose wants ~65 characters, a 230-column
     terminal wants every pixel there is. Two measures, two widths. */
  width:min(1760px,94vw);margin-left:50%;transform:translateX(-50%);cursor:zoom-in}
.screen:fullscreen{width:100vw;border:0;border-radius:0;transform:none;margin:0;
  display:flex;align-items:center;background:#141313}
.screen:fullscreen video{max-height:100vh;object-fit:contain}
.screen video{display:block;width:100%;height:auto;aspect-ratio:2400/1320}
figcaption{max-width:64ch;font-family:"JetBrains Mono",monospace;font-size:12px;color:var(--muted);
  margin-top:12px;letter-spacing:.03em}

/* Cards ----------------------------------------------------------------- */
.cta{display:grid;grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:24px;margin-top:28px}
.cta .card{display:block;border:1px solid var(--rule);border-radius:10px;padding:26px 28px;
  background:var(--surface);color:var(--ink);text-decoration:none;transition:border-color .15s,transform .15s}
.cta .card:hover,.cta .card:focus-visible{border-color:var(--accent);transform:translateY(-2px)}
.cta .card h3{font-family:"JetBrains Mono",monospace;font-size:14px;letter-spacing:.06em;
  color:var(--accent);margin:0 0 10px;font-weight:500}
.cta .card p{margin:0;font-size:17px;color:var(--muted)}

/* Build notes ----------------------------------------------------------- */
.notes{display:grid;grid-template-columns:repeat(auto-fit,minmax(268px,1fr));gap:34px;margin-top:34px}
.note h3{font-family:"JetBrains Mono",monospace;font-size:13px;letter-spacing:.05em;
  text-transform:uppercase;color:var(--accent);margin:0 0 10px;font-weight:500}
.note p{font-size:17px;color:var(--muted);margin:0;max-width:44ch}
.notes.dense{gap:26px 30px;margin-top:30px}
.notes.dense .note h3{text-transform:none;font-size:13.5px;margin-bottom:6px}
.notes.dense .note p{font-size:16px}
.note code{font-size:.8em}

.disclosure{border-left:2px solid var(--accent);padding:4px 0 4px 20px;margin-top:44px}
.disclosure p{font-size:17px;color:var(--muted);max-width:66ch;margin:0}

/* Releases -------------------------------------------------------------- */
pre.install{margin:24px 0 0;max-width:100%;width:max-content;overflow-x:auto;padding:14px 18px;
  border:1px solid var(--rule);border-radius:8px;background:var(--surface);font-size:14px}
pre.install code{background:none;color:var(--ink);padding:0;font-size:inherit}
.release{padding:56px 0;border-bottom:1px solid var(--rule)}
.release h2 a{color:var(--ink);text-decoration:none;font-family:"JetBrains Mono",monospace;letter-spacing:.04em}
.release h2 a:hover{color:var(--accent)}
.badge{font-family:"JetBrains Mono",monospace;font-size:12px;letter-spacing:.08em;vertical-align:middle;
  margin-left:14px;padding:3px 10px;border-radius:999px;background:var(--accent-soft);color:var(--accent)}
.rel-body{max-width:72ch}
.rel-body h2{font-size:22px;margin:34px 0 10px;max-width:none}
.rel-body h3{font-size:19px;font-weight:600;margin:24px 0 6px}
.rel-body p,.rel-body li{font-size:17.5px}
.rel-body ul{padding-left:1.2em}
.rel-body li{margin:7px 0}
.rel-body a{color:var(--accent);text-decoration:none;border-bottom:1px solid var(--accent-soft)}
.rel-body pre{background:var(--surface);border:1px solid var(--rule);border-radius:6px;
  padding:14px 16px;overflow-x:auto;font-size:14px}
.rel-body pre code,.rel-body pre{color:var(--ink)}
.rel-body pre code{background:none;padding:0}
.rel-body .anchor,.rel-body .octicon{display:none}
.rel-body .markdown-heading{display:block}
.rel-body details summary{cursor:pointer;color:var(--muted);font-family:"JetBrains Mono",monospace;font-size:13px}

footer{padding:64px 0 96px}
footer p{max-width:66ch}
footer a,.note a{color:var(--accent);text-decoration:none;border-bottom:1px solid var(--accent-soft)}
footer a:hover,footer a:focus-visible,.note a:hover,.note a:focus-visible{border-bottom-color:var(--accent)}
:focus-visible{outline:2px solid var(--accent);outline-offset:3px}

@media (max-width:640px){ body{font-size:17.5px} section{padding:56px 0} .wrap{padding:0 20px}
  nav.top .wrap{gap:14px;font-size:12px} }
@media (prefers-reduced-motion:reduce){ .screen video{outline:none} .cta .card{transition:none} }
"""

JS = """
// Play a clip only while it is on screen: a page of terminal recordings all
// decoding at once is real work on a laptop. preload="none" means a clip is not
// fetched at all until it is first scrolled to.
const vids = [...document.querySelectorAll('video')];
const reduce = matchMedia('(prefers-reduced-motion: reduce)').matches;
if (!reduce && 'IntersectionObserver' in window) {
  const io = new IntersectionObserver(es => es.forEach(e => {
    if (e.isIntersecting) { e.target.play().catch(() => {}); }
    else { e.target.pause(); }
  }), { threshold: 0.25 });
  vids.forEach(v => io.observe(v));
} else {
  vids.forEach(v => { v.controls = true; });
}

document.querySelectorAll('.screen').forEach(el => {
  el.addEventListener('click', () => {
    if (document.fullscreenElement) document.exitFullscreen();
    else if (el.requestFullscreen) el.requestFullscreen().catch(() => {});
  });
});
"""


if __name__ == "__main__":
    main()

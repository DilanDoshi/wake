#!/usr/bin/env python3
"""Builds the landing page: the beat clips, embedded, beside the claim each proves.

Artifacts are served under a strict CSP with no external hosts, so every clip
is a data: URI. That is the whole reason this is a generator rather than a
hand-written file — and the reason the clips are re-encoded small first.
"""

import base64
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, ".work", "out")
TMP = os.path.join(HERE, ".work", "web")
PAGE = os.path.join(HERE, "wake-demo-page.html")

# (clip, start, seconds, glyph, who, label, heading, prose)
#
# The glyph/name/label triple is the room's own grammar for attributing a line,
# which is what the page is laid out in.
SHOTS = [
    (
        "03-broadcast",
        9.5,
        13,
        "●",
        "you",
        "the room",
        "One message. Six agents.",
        "<code>@all</code> reaches every agent in the room at once. They pick up "
        "their own piece of the work and answer in one thread — and they stay "
        "quiet unless they have something to say, which is the only reason a room "
        "of thirty is readable at all. An unaddressed message goes to the manager "
        "instead, deliberately: a manager told to report on the fleet by a message "
        "it also received has been given the same job twice.",
    ),
    (
        "05-blocked",
        0,
        13,
        "▲",
        "omar",
        "web · 429 client state",
        "The one that needs you, found for you.",
        "Agents rank themselves by whether they need you. When one blocks on a "
        "permission, it goes to the top of the roster and turns amber without "
        "anyone watching for it — and <code>⌃X</code> jumps straight to whoever is "
        "blocked. The ask is answered <em>in the pane that raised it</em>, with the "
        "turn that led to it still on screen, never in a modal thrown over "
        "everything.",
    ),
    (
        "07-grid",
        2.5,
        12,
        "○",
        "priya",
        "cli · --rate-limit flag",
        "Open one and you're in Claude Code.",
        "A conversation is the real thing: the same slash commands (the ones that "
        "session advertised, not a guess), the same rendering, the same status bar "
        "with its branch and context left. Panes are columns, each splittable once "
        "— bounded on purpose. Wake is not trying to become a multiplexer; the "
        "group chat is the product and the panes are substrate.",
    ),
    (
        "09-manager",
        13,
        15,
        "◐",
        "manager",
        "feat/rate-limit",
        "Tell the agents working on the api to…",
        "Every room seats a manager, and it can do exactly three things: send, "
        "interrupt, spawn. There is no bulk-send primitive — so “tell the agents "
        "working on the api” is the manager listing the fleet, deciding which rows "
        "match, and sending to each one. Those are real tool calls against a real "
        "server, which is why the roster rows light up underneath them.",
    ),
    (
        "10-board",
        2.5,
        11,
        "▪",
        "the fleet",
        "one row each",
        "Thirty agents don't fit in panes.",
        "<code>/board</code> is the overview: one row per agent carrying its state "
        "and its own last line, and nothing else. No transcripts — a tiled grid of "
        "thirty conversations is unreadable by arithmetic. It is for triage, so it "
        "carries the triage verbs: jump to one, park one, leave.",
    ),
    (
        "11-leaving",
        1.5,
        17,
        "·",
        "you",
        "leaving",
        "Close the terminal. They keep working.",
        "<code>⌃O</code> arms the detach and <code>↵</code> confirms it — a "
        "different key, because a same-key confirm fires on exactly the reflex the "
        "arm exists to catch. Then the client is gone and the fleet is not: "
        "<code>wake status</code> from a bare shell still lists every agent, "
        "including the one still blocked. Run <code>wake</code> again and the room "
        "comes back, re-derived from Claude's own transcripts rather than from "
        "anything Wake kept.",
    ),
]


def encode(clip, start, dur):
    """One clip, small enough to inline. Terminal video is mostly static, so it
    survives a low bitrate far better than camera footage would."""
    os.makedirs(TMP, exist_ok=True)
    dst = os.path.join(TMP, "%s.mp4" % clip)
    subprocess.run(
        [
            "ffmpeg",
            "-v",
            "error",
            "-ss",
            str(start),
            "-t",
            str(dur),
            "-i",
            os.path.join(OUT, "%s.mp4" % clip),
            "-vf",
            "scale=2400:-2",
            "-an",
            "-c:v",
            "libx264",
            "-crf",
            "25",
            "-preset",
            "slow",
            "-pix_fmt",
            "yuv420p",
            "-movflags",
            "+faststart",
            "-y",
            dst,
        ],
        check=True,
    )
    with open(dst, "rb") as fh:
        return base64.b64encode(fh.read()).decode(), os.path.getsize(dst)


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
body{
  margin:0; background:var(--ground); color:var(--ink);
  font-family:Newsreader,Georgia,"Times New Roman",serif;
  font-size:19px; line-height:1.62; -webkit-font-smoothing:antialiased;
}
.mono{font-family:"JetBrains Mono",ui-monospace,SFMono-Regular,Menlo,monospace}
code{font-family:"JetBrains Mono",ui-monospace,Menlo,monospace;font-size:.86em;
  background:var(--accent-soft); color:var(--accent); padding:.1em .35em; border-radius:3px}

.wrap{max-width:1080px;margin:0 auto;padding:0 32px}

/* Hero ------------------------------------------------------------------ */
header{padding:104px 0 56px;border-bottom:1px solid var(--rule)}
.mark{font-family:"JetBrains Mono",monospace;font-weight:700;font-size:clamp(52px,8vw,86px);
  letter-spacing:.22em;margin:0 0 4px;text-transform:none}
.thesis{font-size:clamp(22px,2.6vw,30px);line-height:1.34;max-width:22ch;margin:18px 0 0;
  text-wrap:balance;font-weight:400}
.sub{color:var(--muted);max-width:62ch;margin:22px 0 0}
.meta{display:flex;flex-wrap:wrap;gap:10px;margin-top:30px}
.pill{font-family:"JetBrains Mono",monospace;font-size:12.5px;letter-spacing:.06em;
  border:1px solid var(--rule);border-radius:999px;padding:6px 13px;color:var(--muted)}
.pill b{color:var(--accent);font-weight:500}

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
.screen video{display:block;width:100%;height:auto}
figcaption{max-width:64ch;font-family:"JetBrains Mono",monospace;font-size:12px;color:var(--muted);
  margin-top:12px;letter-spacing:.03em}

/* Build notes ----------------------------------------------------------- */
.notes{display:grid;grid-template-columns:repeat(auto-fit,minmax(268px,1fr));gap:34px;margin-top:34px}
.note h3{font-family:"JetBrains Mono",monospace;font-size:13px;letter-spacing:.05em;
  text-transform:uppercase;color:var(--accent);margin:0 0 10px;font-weight:500}
.note p{font-size:17px;color:var(--muted);margin:0;max-width:44ch}

.disclosure{border-left:2px solid var(--accent);padding:4px 0 4px 20px;margin-top:44px}
.disclosure p{font-size:17px;color:var(--muted);max-width:66ch;margin:0}

footer{padding:64px 0 96px}
footer a{color:var(--accent);text-decoration:none;border-bottom:1px solid var(--accent-soft)}
footer a:hover,footer a:focus-visible{border-bottom-color:var(--accent)}
:focus-visible{outline:2px solid var(--accent);outline-offset:3px}

@media (max-width:640px){ body{font-size:17.5px} section{padding:56px 0} .wrap{padding:0 20px} }
@media (prefers-reduced-motion:reduce){ .screen video{outline:none} }
"""

JS = """
// Play a clip only while it is on screen. Six terminal recordings all decoding
// at once is real work on a laptop, and nothing below the fold is being read.
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


def main():
    handle = sys.argv[1] if len(sys.argv) > 1 else "github.com/DilanDoshi/wake"
    total = 0
    blocks = []

    for clip, start, dur, glyph, who, label, head, prose in SHOTS:
        b64, size = encode(clip, start, dur)
        total += size
        print("  %-14s %6.1f KB" % (clip, size / 1024))
        blocks.append(f"""
    <section>
      <div class="attr"><span class="glyph">{glyph}</span>{who} <span class="lbl">&lt;&gt; {label}</span></div>
      <h2>{head}</h2>
      <p>{prose}</p>
      <figure>
        <div class="screen">
          <video muted loop playsinline preload="metadata"
                 src="data:video/mp4;base64,{b64}"></video>
        </div>
        <figcaption>Recorded from the running binary — real daemon, real room, real protocol. Click to enlarge.</figcaption>
      </figure>
    </section>""")

    html = f"""<title>Wake</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Newsreader:ital,opsz,wght@0,6..72,300..600;1,6..72,300..500&family=JetBrains+Mono:wght@400;500;700&display=swap">
<style>{CSS}</style>

<header>
  <div class="wrap">
    <h1 class="mark">Wake</h1>
    <p class="thesis">A terminal for running a fleet of Claude&nbsp;Code sessions.</p>
    <p class="sub">Fifteen to thirty agents is not fifteen to thirty terminal tabs. Wake turns the
      fleet into a room: one group chat as the primary surface, <code>@name</code> to reach one,
      a roster that ranks agents by whether they need you, and any agent openable as a full
      conversation at Claude&nbsp;Code fidelity.</p>
    <div class="meta">
      <span class="pill"><b>Go</b> · Bubble&nbsp;Tea</span>
      <span class="pill">no screen-scraping — <b>structured JSON only</b></span>
      <span class="pill">stream-json over a <b>daemon socket</b></span>
      <span class="pill">6 surfaces, <b>one keyboard</b></span>
    </div>
  </div>
</header>

<main class="wrap">{"".join(blocks)}

  <section>
    <div class="attr"><span class="glyph">·</span>how it's built <span class="lbl">&lt;&gt; the parts that matter</span></div>
    <h2>The constraints are the design.</h2>
    <div class="notes">
      <div class="note">
        <h3>Never screen-scraped</h3>
        <p>Every agent is a headless <code>claude</code> in stream-json mode with a Wake-assigned
           session id. All state arrives as structured JSON on stdout — nothing is read off a
           rendered screen.</p>
      </div>
      <div class="note">
        <h3>One airlock</h3>
        <p>Exactly four files are allowed to know Claude's JSON. Everything above them sees Wake's
           own event type, and a test holds the file set so a fifth cannot be added quietly.</p>
      </div>
      <div class="note">
        <h3>Cheap to leave open</h3>
        <p>No work per frame that could be work per change, and no polling. A streamed answer is a
           preview, never a record — re-rendering each token measured 65× the cost.</p>
      </div>
      <div class="note">
        <h3>Wake owns almost no state</h3>
        <p>Claude persists the transcripts; Wake reads one back when a conversation opens. It keeps
           only a roster, a park book, groups and layout — so it can crash and lose nothing.</p>
      </div>
    </div>

    <div class="disclosure">
      <p>Every frame here is the real binary in a real terminal — the daemon, the room, the
        rendering and the wire protocol are all genuine, and the git branch in the status bar is a
        real branch. What is scripted is <em>what the models say</em>: the recordings drive a
        stand-in <code>claude</code> so takes are deterministic, cost nothing, and cannot leak
        anything from the machine that recorded them.</p>
    </div>
  </section>
</main>

<footer class="wrap">
  <p class="mono" style="font-size:14px;color:var(--muted)">
    <a href="https://{handle}">{handle}</a>
  </p>
</footer>
<script>{JS}</script>
"""
    with open(PAGE, "w") as fh:
        fh.write(html)

    print("\npage: %s" % PAGE)
    print("  video %.1f MB · page %.1f MB" % (total / 1e6, os.path.getsize(PAGE) / 1e6))


if __name__ == "__main__":
    main()

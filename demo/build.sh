#!/usr/bin/env bash
# Assembles the beat recordings into the film.
#
# Each shot is one recorded beat, trimmed, captioned, normalised to 1920x1080
# and written to an intermediate; the intermediates are concatenated. Doing it
# per shot rather than in one filter graph keeps a re-cut of one beat cheap —
# the others do not re-encode.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="$here/.work/out"
cards="$here/.work/cards"
seg="$here/.work/seg"
final="$here/wake-demo.mp4"

mkdir -p "$seg"
rm -f "$seg"/*.mp4 "$seg/list.txt"

GROUND=0x141313
FPS=30

# A still, held. Used for the title and end cards.
still() { # still <png> <seconds> <output>
  # -t goes on the OUTPUT. As an input option in front of a looped still it
  # does not bound a filter_complex graph, and the encode runs forever.
  ffmpeg -v error -loop 1 -i "$1" \
    -filter_complex "[0:v]scale=1920:1056,pad=1920:1080:0:12:color=$GROUND,setsar=1,format=yuv420p[v]" \
    -map "[v]" -t "$2" -r $FPS -c:v libx264 -crf 18 -y "$3"
}

# One beat: trim, hold a caption over part of it, normalise.
#
# The caption is an overlay rather than drawtext because it is a rendered band
# with tracking and a key chip; `enable` is what decides the window it is up
# for, so a beat can run before its caption arrives.
shot() { # shot <clip> <start> <dur> <caption|-> <cap-in> <output>
  local clip="$out/$1.mp4" start="$2" dur="$3" cap="$4" capin="$5" dst="$6"
  # Clamp to what the clip actually holds. A re-take is a slightly different
  # length every time, and asking for more than exists silently yields a short
  # shot — which is how the film once came out half its intended length.
  local have
  have=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$clip")
  dur=$(python3 -c "print(round(min($dur, $have - $start - 0.05), 2))")
  if [ "$cap" = "-" ]; then
    ffmpeg -v error -ss "$start" -t "$dur" -i "$clip" \
      -filter_complex "[0:v]scale=1920:1056,pad=1920:1080:0:12:color=$GROUND,setsar=1,format=yuv420p[v]" \
      -map "[v]" -r $FPS -c:v libx264 -crf 18 -y "$dst"
  else
    ffmpeg -v error -ss "$start" -t "$dur" -i "$clip" -loop 1 -i "$cards/cap-$cap.png" \
      -filter_complex "[0:v][1:v]overlay=0:0:enable='gte(t,$capin)'[o];[o]scale=1920:1056,pad=1920:1080:0:12:color=$GROUND,setsar=1,format=yuv420p[v]" \
      -map "[v]" -t "$dur" -r $FPS -c:v libx264 -crf 18 -y "$dst"
  fi
}

add() { echo "file '$1'" >> "$seg/list.txt"; }

# The counter must not run in a subshell: `f=$(next)` increments a copy, so
# every shot writes 01.mp4 and the film becomes the last one, repeated.
n=0
next() { n=$((n+1)); f=$(printf "%s/%02d.mp4" "$seg" "$n"); }

echo "→ title"
next; still "$cards/title.png" 4.5 "$f"; add "$f"

# The hook: a fleet already in motion, unexplained for the first seconds.
echo "→ hook"
next; shot 01-hook 0 8.8 hook 2.6 "$f"; add "$f"

echo "→ start"
next; shot 02-start 0 9 start 3.2 "$f"; add "$f"

# The broadcast is the centre of the film, so it gets the longest hold.
echo "→ broadcast"
next; shot 03-broadcast 0 23 broadcast 9.5 "$f"; add "$f"

echo "→ mention"
next; shot 04-mention 0 9 mention 3 "$f"; add "$f"

# Triage: ⌃X to the blocked agent, then the card being answered.
echo "→ blocked"
next; shot 05-blocked 0 7 blocked 0.8 "$f"; add "$f"
next; shot 05-blocked 7 7 answer 0.5 "$f"; add "$f"

echo "→ conversation"
next; shot 06-dm 0 9.4 dm 2.5 "$f"; add "$f"

echo "→ grid"
next; shot 07-grid 0 14.2 grid 2.5 "$f"; add "$f"

echo "→ fork"
next; shot 08-fork 0 10 fork 2.5 "$f"; add "$f"

# The manager: a rollup, then the fan-out — two claims, so two captions.
echo "→ manager"
next; shot 09-manager 0 13 manager 1.5 "$f"; add "$f"
next; shot 09-manager 13 15 fanout 1.0 "$f"; add "$f"

echo "→ board"
next; shot 10-board 0 12.4 board 2.5 "$f"; add "$f"

# Leaving. Its caption sits in a top band, because the legend at the bottom
# swapping `↵ send` to `↵ detach` is the thing being demonstrated.
echo "→ leaving"
next; shot 11-leaving 0 20 leaving 1.5 "$f"; add "$f"

echo "→ end card"
next; still "$cards/end.png" 5.5 "$f"; add "$f"

echo "→ concat"
total=0
while read -r _ path; do
  path="${path%\'}"; path="${path#\'}"
  d=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$path")
  total=$(python3 -c "print($total + $d)")
done < "$seg/list.txt"
fadeout=$(python3 -c "print(round($total - 1.2, 2))")

ffmpeg -v error -f concat -safe 0 -i "$seg/list.txt" \
  -vf "fade=t=in:st=0:d=0.6,fade=t=out:st=$fadeout:d=1.2" \
  -r $FPS -c:v libx264 -crf 19 -pix_fmt yuv420p -movflags +faststart -y "$final"

dur=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$final")
printf "\n%s\n  %s  %.0fs  %s\n" "film:" "$final" "$dur" \
  "$(ffprobe -v error -show_entries stream=width,height -of csv=p=0:s=x "$final" | head -1)"

# A README hero loop: the broadcast beat alone, no captions, as a GIF.
echo "→ hero gif"
ffmpeg -v error -ss 9 -t 12 -i "$out/03-broadcast.mp4" \
  -vf "fps=12,scale=1200:-1:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer" \
  -y "$here/wake-demo.gif"
ls -lh "$here/wake-demo.gif" | awk '{print "  " $9 "  " $5}'

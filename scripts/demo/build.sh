#!/bin/bash
# Builds the HTT Scheduling demo video:
#   1. neural TTS narration per scene (edge-tts; falls back to macOS `say`)
#   2. per-scene mp4: screen recording + logo watermark, padded/trimmed to the
#      narration length
#   3. branded title cards (rendered by render-cards.mjs)
#   4. concat -> HTT-Scheduling-Demo.mp4 at the project root
#
# Usage: ./scripts/demo/build.sh    (run scripts/demo/record.mjs first)

set -euo pipefail
cd "$(dirname "$0")"

OUT=out
EDGE_VOICE=en-US-AndrewNeural
EDGE_RATE=-4%
VOICE=Samantha   # fallback if the edge-tts venv is missing
RATE=168
VENV_TTS="$PWD/.venv/bin/edge-tts"
mkdir -p "$OUT/audio" "$OUT/parts"

dur() { ffprobe -v error -show_entries format=duration -of csv=p=0 "$1"; }

# Narration via Microsoft's neural voices when available (no key needed);
# falls back to macOS `say` otherwise. Output is always 48k stereo wav.
tts() { # $1 = out wav, $2 = text
  local out=$1 text=$2
  if [ -x "$VENV_TTS" ]; then
    "$VENV_TTS" --voice "$EDGE_VOICE" --rate="${EDGE_RATE}" \
      --text "$text" --write-media "${out%.wav}.mp3" >/dev/null
    ffmpeg -y -loglevel error -i "${out%.wav}.mp3" -ar 48000 -ac 2 -sample_fmt s16 "$out"
    rm -f "${out%.wav}.mp3"
  else
    say -v "$VOICE" -r "$RATE" -o "$out" --data-format=LEI16@48000 "$text"
  fi
  [ -s "$out" ] || { echo "TTS failed: $out" >&2; exit 1; }
}

min_of() {
  case "$1" in
    01) echo 19 ;;
    02) echo 24 ;;
    03) echo 27 ;;
    04) echo 22 ;;
    05) echo 30 ;;
    *)  echo 0  ;;
  esac
}

# ---------- 1. narration ----------
echo "== narration =="
while IFS='|' read -r nn text; do
  [ -z "${nn:-}" ] && continue
  echo "  TTS $nn"
  tts "$OUT/audio/$nn.wav" "$text"
done < narration.txt

# ---------- 2. screen scenes ----------
make_scene() { # $1 = scene number, $2 = webm
  local nn=$1 src=$2
  local d t m
  d=$(dur "$OUT/audio/$nn.wav")
  m=$(min_of "$nn")
  t=$(awk -v d="$d" -v m="$m" 'BEGIN { t = 2.4 + d; if (t < m) t = m; printf "%.2f", t }')
  echo "  scene $nn: vo=${d}s target=${t}s"
  LOGO="$OUT/shots/logo-badge.png"
  ffmpeg -y -hide_banner -loglevel error \
    -i "$src" -loop 1 -framerate 30 -i "$LOGO" -i "$OUT/audio/$nn.wav" \
  -filter_complex "[0:v]fps=30,scale=1440:900:force_original_aspect_ratio=decrease,pad=1440:900:(ow-iw)/2:(oh-ih)/2,setsar=1,tpad=stop_mode=clone:stop_duration=40[base];[1:v]scale=84:84,format=rgba,colorchannelmixer=aa=0.85[wm];[base][wm]overlay=x=W-w-26:y=26[v];[2:a]aformat=sample_rates=48000:channel_layouts=stereo,adelay=1000|1000,apad[a]" \
  -map "[v]" -map "[a]" -t "$t" \
  -c:v libx264 -preset medium -crf 20 -pix_fmt yuv420p \
  -c:a aac -b:a 160k -ar 48000 \
  "$OUT/parts/$nn.mp4"
}

# ---------- 3. title cards ----------
make_card() { # $1 = scene number, $2 = pre-rendered card png
  local nn=$1 bg=$2
  local d t
  d=$(dur "$OUT/audio/$nn.wav")
  t=$(awk -v d="$d" 'BEGIN { printf "%.2f", 2.2 + d }')
  echo "  card $nn: vo=${d}s target=${t}s"
  ffmpeg -y -hide_banner -loglevel error \
    -loop 1 -framerate 30 -i "$bg" -i "$OUT/audio/$nn.wav" \
    -filter_complex "[0:v]scale=1440:900,setsar=1,fps=30[v];[1:a]aformat=sample_rates=48000:channel_layouts=stereo,adelay=800|800,apad[a]" \
    -map "[v]" -map "[a]" -t "$t" \
    -c:v libx264 -preset medium -crf 20 -pix_fmt yuv420p \
    -c:a aac -b:a 160k -ar 48000 \
    "$OUT/parts/$nn.mp4"
}

echo "== brand assets =="
node render-cards.mjs

echo "== assemble scenes =="
make_scene 01 "$OUT/video/01-login.webm"
make_scene 02 "$OUT/video/02-student.webm"
make_scene 03 "$OUT/video/03-manager.webm"
make_scene 04 "$OUT/video/04-scheduler.webm"
make_scene 05 "$OUT/video/05-admin.webm"

echo "== title cards =="
make_card 00 "$OUT/shots/card-title.png"
make_card 06 "$OUT/shots/card-outro.png"

# ---------- 4. concat ----------
echo "== concat =="
: > "$OUT/list.txt"
for f in "$OUT"/parts/*.mp4; do echo "file '$PWD/$f'" >> "$OUT/list.txt"; done

FINAL="../../HTT-Scheduling-Demo.mp4"
ffmpeg -y -hide_banner -loglevel error \
  -f concat -safe 0 -i "$OUT/list.txt" \
  -c:v libx264 -profile:v main -level:v 4.0 -preset medium -crf 20 -pix_fmt yuv420p \
  -colorspace bt709 -color_primaries bt709 -color_trc bt709 \
  -c:a aac -b:a 160k -movflags +faststart \
  "$FINAL"

echo "== done =="
ffprobe -v error -show_entries format=duration,size -of default=noprint_wrappers=1 "$FINAL"

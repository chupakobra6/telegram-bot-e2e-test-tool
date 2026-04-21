#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${TG_E2E_FIXTURE_DIR:-$ROOT_DIR/artifacts/fixtures}"

mkdir -p "$OUT_DIR"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg is required to generate audio fixtures" >&2
  exit 1
fi

go run ./cmd/fixturegen --output "$OUT_DIR/e2e-photo.png"

ffmpeg -y -f lavfi -i sine=frequency=880:duration=1 -c:a libopus "$OUT_DIR/e2e-voice.ogg" >/dev/null 2>&1
ffmpeg -y -f lavfi -i sine=frequency=660:duration=1 -c:a libmp3lame "$OUT_DIR/e2e-audio.mp3" >/dev/null 2>&1

echo "generated fixtures in $OUT_DIR"

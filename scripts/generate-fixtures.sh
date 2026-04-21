#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${TG_E2E_FIXTURE_DIR:-$ROOT_DIR/artifacts/fixtures}"
TMP_DIR="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$OUT_DIR"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg is required to generate audio fixtures" >&2
  exit 1
fi

go run ./cmd/fixturegen --output "$OUT_DIR/e2e-photo.png" --preset package
go run ./cmd/fixturegen --output "$OUT_DIR/e2e-receipt.png" --preset receipt
go run ./cmd/fixturegen --output "$OUT_DIR/e2e-blank-photo.png" --preset blank

if command -v say >/dev/null 2>&1 && say -v '?' 2>/dev/null | grep -q '^Milena'; then
  say -v Milena -o "$TMP_DIR/e2e-voice.aiff" "сметана завтра"
  say -v Milena -o "$TMP_DIR/e2e-audio.aiff" "йогурт послезавтра"
  ffmpeg -y -i "$TMP_DIR/e2e-voice.aiff" -c:a libopus "$OUT_DIR/e2e-voice.ogg" >/dev/null 2>&1
  ffmpeg -y -i "$TMP_DIR/e2e-audio.aiff" -c:a libmp3lame "$OUT_DIR/e2e-audio.mp3" >/dev/null 2>&1
elif [[ -f "$OUT_DIR/e2e-voice.ogg" && -f "$OUT_DIR/e2e-audio.mp3" ]]; then
  echo "reusing existing speech fixtures in $OUT_DIR" >&2
elif [[ "${TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES:-}" == "1" ]]; then
  ffmpeg -y -f lavfi -i sine=frequency=880:duration=1 -c:a libopus "$OUT_DIR/e2e-voice.ogg" >/dev/null 2>&1
  ffmpeg -y -f lavfi -i sine=frequency=660:duration=1 -c:a libmp3lame "$OUT_DIR/e2e-audio.mp3" >/dev/null 2>&1
  echo "warning: generated synthetic non-speech audio fixtures because TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES=1" >&2
else
  echo "speech fixtures require macOS say+Milena or preexisting e2e-voice.ogg/e2e-audio.mp3 in $OUT_DIR" >&2
  echo "refusing to generate misleading sine-wave fixtures; set TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES=1 only if you explicitly want non-speech placeholders" >&2
  exit 1
fi
cat >"$OUT_DIR/e2e-document.txt" <<'EOF'
unsupported payload fixture
EOF

echo "generated fixtures in $OUT_DIR"

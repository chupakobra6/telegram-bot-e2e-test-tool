#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -z "${CHAT:-}" ]]; then
  echo "CHAT=@your_bot_username is required" >&2
  exit 2
fi

scenarios=(
  "examples/suite/01-start-pin-service.jsonl"
  "examples/suite/02-dashboard-navigation-edit.jsonl"
  "examples/suite/03-text-draft-confirm.jsonl"
  "examples/suite/04-photo-processing-and-draft.jsonl"
  "examples/suite/05-voice-processing.jsonl"
  "examples/suite/06-audio-processing.jsonl"
)

required_fixtures=(
  "artifacts/fixtures/e2e-photo.png"
  "artifacts/fixtures/e2e-voice.ogg"
  "artifacts/fixtures/e2e-audio.mp3"
)

for scenario in "${scenarios[@]}"; do
  if [[ ! -f "$scenario" ]]; then
    echo "suite scenario not found: $scenario" >&2
    exit 1
  fi
  echo "==> running $scenario"
done

for fixture in "${required_fixtures[@]}"; do
  if [[ ! -f "$fixture" ]]; then
    echo "required fixture missing: $fixture" >&2
    echo "run 'make fixtures' first" >&2
    exit 1
  fi
done

./scripts/run-scenario.sh "${scenarios[@]}"

echo "suite completed"

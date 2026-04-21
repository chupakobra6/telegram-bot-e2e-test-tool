#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

scenarios=(
  "examples/suite/01-start-pin-service.jsonl"
  "examples/suite/02-dashboard-navigation-edit.jsonl"
  "examples/suite/03-text-draft-confirm.jsonl"
  "examples/suite/04-photo-processing-and-draft.jsonl"
  "examples/suite/05-voice-processing.jsonl"
  "examples/suite/06-audio-processing.jsonl"
)

for scenario in "${scenarios[@]}"; do
  echo "==> running $scenario"
done

./scripts/run-scenario.sh "${scenarios[@]}"

echo "suite completed"

#!/usr/bin/env bash
set -euo pipefail

CALLER_PWD="$(pwd -P)"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$#" -lt 1 ]]; then
  echo "usage: scripts/run-scenario.sh <scenario.jsonl> [scenario.jsonl ...]" >&2
  exit 2
fi

resolve_path() {
  local input="$1"
  case "$input" in
    /*) printf '%s\n' "$input" ;;
    *) printf '%s\n' "$CALLER_PWD/$input" ;;
  esac
}

scenario_paths=()
for scenario in "$@"; do
  resolved="$(resolve_path "$scenario")"
  if [[ ! -f "$resolved" ]]; then
    echo "scenario file not found: $scenario (resolved: $resolved)" >&2
    exit 1
  fi
  scenario_paths+=("$resolved")
done

cd "$ROOT_DIR"
go run ./cmd/tg-e2e-tool run-scenario "${scenario_paths[@]}"

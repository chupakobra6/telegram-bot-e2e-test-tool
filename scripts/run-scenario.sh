#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ "$#" -lt 1 ]]; then
  echo "usage: scripts/run-scenario.sh <scenario.jsonl> [scenario.jsonl ...]" >&2
  exit 2
fi

go run ./cmd/tg-e2e-tool run-scenario "$@"

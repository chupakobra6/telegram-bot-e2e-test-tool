#!/usr/bin/env bash
set -euo pipefail

CALLER_PWD="$(pwd -P)"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

CASES_FILE="${CASES:-}"
WAIT_TIMEOUT_MS="${WAIT_TIMEOUT_MS:-12000}"
CANCEL_BUTTON_TEXT="${CANCEL_BUTTON_TEXT:-↩️ Отмена}"

if [[ -z "${CHAT:-}" ]]; then
  echo "CHAT=@your_bot_username is required" >&2
  exit 2
fi

if [[ -z "$CASES_FILE" ]]; then
  echo "CASES=/absolute/path/to/cases.txt is required" >&2
  exit 2
fi

case "$CASES_FILE" in
  /*) ;;
  *) CASES_FILE="$CALLER_PWD/$CASES_FILE" ;;
esac

if [[ ! -f "$CASES_FILE" ]]; then
  echo "cases file not found: $CASES_FILE" >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

case_index=0
while IFS= read -r phrase || [[ -n "$phrase" ]]; do
  [[ -z "${phrase// }" || "$phrase" == \#* ]] && continue
  case_index=$((case_index + 1))
  scenario_path="$tmpdir/case-$(printf '%02d' "$case_index").jsonl"
  echo "==> text case $case_index: $phrase"
  go run ./cmd/scenario-helper render-text-case \
    --output "$scenario_path" \
    --text "$phrase" \
    --cancel-button "$CANCEL_BUTTON_TEXT" \
    --wait-timeout-ms "$WAIT_TIMEOUT_MS"
  CHAT="$CHAT" ./scripts/run-scenario.sh "$scenario_path"
done < "$CASES_FILE"

echo "text matrix completed"

#!/usr/bin/env bash
set -euo pipefail

CALLER_PWD="$(pwd -P)"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTROL_URL="${CONTROL_URL:-${SHELFY_DEV_CONTROL_URL:-http://127.0.0.1:8081}}"
RUN_PREFIX="${RUN_PREFIX:-$(LC_ALL=C od -An -N5 -tx1 /dev/urandom | tr -d ' \n')}"

RESET_BEFORE=1
CLEAR_TIME=0

usage() {
  cat >&2 <<'EOF'
usage: scripts/run-block.sh [--no-reset] [--clear-time] <scenario.jsonl|scenario.jsonl.tmpl> [...]

Environment:
  CHAT                     Telegram bot username, passed through to the tool
  RUN_PREFIX               Optional explicit prefix reused across multiple invocations
  CONTROL_URL              Dev control base URL (default: http://127.0.0.1:8081)
  SHELFY_DEV_CONTROL_URL   Legacy alias for CONTROL_URL during migration
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

resolve_path() {
  local input="$1"
  case "$input" in
    /*) printf '%s\n' "$input" ;;
    *) printf '%s\n' "$CALLER_PWD/$input" ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-reset)
      RESET_BEFORE=0
      shift
      ;;
    --clear-time)
      CLEAR_TIME=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      usage
      echo "unknown flag: $1" >&2
      exit 2
      ;;
    *)
      break
      ;;
  esac
done

if [[ $# -lt 1 ]]; then
  usage
  exit 2
fi

if [[ -z "${CHAT:-}" ]]; then
  echo "CHAT is required" >&2
  exit 2
fi

require_cmd curl
require_cmd envsubst

TMP_PARENT="${TMPDIR:-/tmp}"
TMP_DIR="$(mktemp -d "$TMP_PARENT/tg-e2e-run-block.XXXXXX")"
cleanup() {
  if [[ $CLEAR_TIME -eq 1 ]]; then
    curl -fsS -X POST "$CONTROL_URL/control/time/clear" >/dev/null || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

if [[ $RESET_BEFORE -eq 1 ]]; then
  curl -fsS -X POST "$CONTROL_URL/control/e2e/reset" >/dev/null
fi

rendered=()
for scenario in "$@"; do
  resolved="$(resolve_path "$scenario")"
  if [[ ! -f "$resolved" ]]; then
    echo "scenario file not found: $scenario (resolved: $resolved)" >&2
    exit 1
  fi
  if [[ "$resolved" == *.tmpl ]]; then
    target="$TMP_DIR/$(basename "${resolved%.tmpl}")"
    envsubst '${RUN_PREFIX}' <"$resolved" >"$target"
    rendered+=("$target")
  else
    rendered+=("$resolved")
  fi
done

echo "RUN_PREFIX=$RUN_PREFIX" >&2
CHAT="$CHAT" "$ROOT_DIR/scripts/run-scenario.sh" "${rendered[@]}"

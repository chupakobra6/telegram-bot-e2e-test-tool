# Setup

## 1. Prepare the environment

```bash
cd telegram-bot-e2e-test-tool
cp .env.example .env
```

Fill in:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

The CLI auto-loads `.env` from the current working directory, so you do not need to `source .env` manually before `make ...` or `go run ...`.

[`.env.example`](../.env.example) already contains example value formats, including:

- app hash
- phone number
- proxy URL
- absolute paths for optional overrides

The bundled repository scenarios use explicit `select_chat` placeholders instead of hardcoding a real bot username. `make run-scenario`, `make run-suite`, and `make rate-sweep` can materialize that placeholder from:

- `CHAT=@your_bot_username`

## 2. Check the effective config

```bash
make doctor
```

`doctor` shows:

- whether Telegram credentials are set
- where the session file lives
- where the runtime lock file lives
- whether the session file exists
- whether Telegram still considers the current session authorized
- effective pacing/cache defaults
- which proxy variables were picked up

## 3. Create an MTProto session

```bash
make login
```

The tool will ask for a code from Telegram, and for a password if the account uses 2FA.

## 4. Start interactive mode

```bash
make interactive
```

Example command:

```json
{"id":"select","action":"select_chat","chat":"@your_bot_username"}
{"id":"start","action":"send_text","text":"/start"}
```

## 5. Run a single scenario

```bash
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl CHAT=@your_bot_username
```

If you want a clean home dashboard before a targeted scenario, prepend the helper scenario instead of baking `/start` into every flow:

```bash
CHAT=@your_bot_username ./scripts/run-scenario.sh \
  examples/helpers/00-home-ready.jsonl \
  examples/suite/03-text-draft-confirm.jsonl
```

`scripts/run-scenario.sh` accepts absolute paths and paths relative to the directory you launched it from, so sibling product repos can call it without first `cd`-ing into the tool root.

For stateful blocks with reset/template rendering, use the tool-owned runner instead of a product-local wrapper:

```bash
CHAT=@your_bot_username CONTROL_URL=http://127.0.0.1:8081 ./scripts/run-block.sh \
  /absolute/path/to/00-home-ready.jsonl \
  /absolute/path/to/11-timed-digest-setup.jsonl.tmpl
```

`scripts/run-block.sh` handles reset, `RUN_PREFIX`, `.jsonl.tmpl` rendering, optional `--clear-time`, and caller-relative path resolution. `CONTROL_URL` is the preferred control endpoint variable; `SHELFY_DEV_CONTROL_URL` remains a temporary compatibility alias.

After `run-scenario` completes, the tool refreshes:

- `artifacts/transcripts/last-run-artifacts.json`
- `artifacts/transcripts/last-run-summary.json`
- `artifacts/transcripts/last-run-summary.txt`
- `artifacts/transcripts/last-failure.json`
- `artifacts/transcripts/last-failure.txt` when the latest run failed

Read the artifacts in this order:

1. `last-run-summary.txt`
2. `last-failure.txt` if present
3. scenario-level `.compact.txt`
4. raw `.txt` / `.json` only as a last step

`click_button` is pinned-first by default: the tool will try the current pinned interactive screen before scanning visible history, even when that pinned message is outside the visible window. `message_offset` remains the explicit way to target older visible messages on purpose, for example when probing stale dashboards.

External product repos can also reference tool-owned fixtures directly inside their scenarios with paths like:

```json
{"id":"photo","action":"send_photo","path":"@fixtures/e2e-photo.png"}
```

That placeholder resolves to `artifacts/fixtures/` in the tool repo after `make fixtures`.

If another repo keeps only a plain text matrix of phrases, keep the execution layer here:

```bash
make run-text-matrix \
  CHAT=@your_bot_username \
  CASES=/absolute/path/to/date-cases.txt
```

`CASES` may also be relative to the caller's working directory.

Optional overrides:

- `CANCEL_BUTTON_TEXT=↩️ Отмена`
- `WAIT_TIMEOUT_MS=12000`

## 6. Run the full v1 suite

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

`make fixtures` generates the PNG photo fixtures through the repo's own `fixturegen`, so photo and receipt OCR scenarios do not depend on macOS-only `qlmanage`.
On non-macOS machines, it reuses existing speech fixtures if they are already present under `artifacts/fixtures/`. It refuses to silently generate fake sine-wave "speech" unless you explicitly opt in with `TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES=1`.

`make run-suite` keeps one MTProto session alive across the full suite instead of reconnecting between files.
It also fails fast when `CHAT` is missing or the expected media fixtures are absent.
Do not start another runtime command in parallel with the same Telegram account while the suite is running.
The suite is intentionally ordered and stateful: it exercises `/start` once, then reuses that home dashboard in later scenarios.

## 7. Probe the safe action-spacing boundary

```bash
make rate-sweep CHAT=@your_bot_username
```

This uses built-in benchmark scenarios and performs a binary search over `action_spacing` without turning tuning into permanent `.env` settings.
For the bundled Shelfy benchmark scenarios, `rate-sweep` automatically executes `examples/bench/00-shelfy-home-warmup.jsonl` before every probe on the built-in stable pacing. That resets the chat to a known home baseline without making repeated `/start` resets part of the candidate measurement.

For custom benchmarks, you can add the same kind of reset hook explicitly:

```bash
go run ./cmd/tg-e2e-tool rate-sweep \
  --chat @your_bot_username \
  --prepare-scenario examples/bench/00-shelfy-home-warmup.jsonl \
  path/to/custom-bench.jsonl
```

The read side stays fixed at the built-in hybrid values:

- `sync_interval=1600ms`
- `rpc_spacing=700ms`
- `pinned_cache_ttl=45000ms`

The default search range is:

- `min_action_ms=1800`
- `max_action_ms=3000`
- `resolution_ms=100`

The same built-in pacing also applies to interactive mode, so repeated live commands already obey the same action/RPC spacing and `FLOOD_WAIT` backoff logic.

The visible history window is auto-selected the same way. You do not need to tune it in `.env` for normal usage.

## Paths and overrides

By default, the tool uses:

- session: `.sessions/user.json`
- transcripts: `artifacts/transcripts`

You usually do not need to override them in `.env`. `TG_E2E_SESSION_PATH` and `TG_E2E_TRANSCRIPT_DIR` are kept only for non-standard setups.

## Proxy support

- `HTTP_PROXY` / `HTTPS_PROXY` work through `HTTP CONNECT`
- `NO_PROXY` is respected
- `ALL_PROXY` can be used for `SOCKS5`

If you run commands through `make`, `.env` is loaded automatically.

## Parallel-run safety

`login`, `interactive`, `run-scenario`, and `rate-sweep` take a runtime lock next to the session file.

This is intentional. One MTProto test account and one tested chat can only produce meaningful results when a single local process controls them at a time.

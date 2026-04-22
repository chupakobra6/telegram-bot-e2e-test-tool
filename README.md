# Telegram Bot E2E Test Tool

A real-user E2E testing tool for Telegram bots over MTProto.

It logs in as a regular Telegram user, sends messages to a bot, clicks inline buttons, and captures the current chat state the way a user sees it.

It is designed for cases where Bot API-level tests are not enough and you want a user-like view of the chat:

- real MTProto user session, not a bot-side shortcut
- one JSONL command format for both interactive debugging and saved scenarios
- visible-history snapshots, pinned state, service messages, and transcript artifacts for investigation
- one active runtime per Telegram session; do not run scenarios in parallel with the same test account

## Highlights

- one JSONL action format for interactive runs and saved scenarios
- real-user MTProto behavior instead of bot-side shortcuts
- visible `ChatState` snapshots, diffs, service messages, and transcript artifacts
- built-in suite, text-matrix runner, and rate-sweep tooling
- designed to be reused from product repos without moving orchestration into them

## What v1 supports

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `send_document`
- `select_chat`
- `click_button`
- `wait`
- `dump_state`
- interactive JSONL mode over stdin/stdout
- JSONL scenarios using the same commands as interactive mode
- transcript artifacts in raw and compact form, plus rolling run triage summaries

## Why it works this way

- `MTProto`, not Bot API  
  The tool should behave like a normal user, not like a bot or a testing backdoor.
- `JSONL` as the single protocol  
  Interactive mode and the scenario runner use the exact same command format.
- One command-stream executor for both modes  
  Interactive mode feeds the executor from `stdin`, while `run-scenario` feeds it from JSONL files with only a thin relative-path resolution layer for media files.
- `ChatState` as a snapshot of visible history  
  After each action, the tool does not guess based on bot internals. It reads recent chat history, pinned state, and service messages.
- One MTProto session for sequential scenario runs  
  When you pass multiple scenario files, the tool keeps one authorized session alive instead of reconnecting between them.
- One active runtime lock per session  
  `login`, `interactive`, `run-scenario`, and `rate-sweep` acquire a file lock next to the MTProto session. This prevents two local processes from racing the same Telegram account and corrupting each other's scenarios.
- Built-in default paths  
  For normal local usage, you do not need to configure session or transcript paths in `.env`. Those env vars remain available only as advanced overrides.
- Conservative MTProto pacing by default  
  The tool intentionally spaces outgoing actions and sync RPCs to reduce the chance of `FLOOD_WAIT` and protect the test account during longer suites.

The v1 execution loop is simple:

1. log in with a dedicated Telegram user account
2. send a JSON command
3. perform the action in Telegram as a normal user
4. resync recent chat history and pinned state
5. return a fresh `state_snapshot` or `state_update`

## Quick start

```bash
cp .env.example .env
make doctor
make login
make interactive
```

The CLI auto-loads `.env` from the current working directory and falls back to the tool repo root, so you do not need `source .env` or `set -a ...`.
`make doctor` now also performs a live Telegram auth check when the app credentials and session file are present, so it can tell you whether the local session file is actually still authorized.

If you want the full suite:

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

`make fixtures` now generates photo and receipt PNG fixtures through the repo's own `fixturegen`, so those OCR scenarios no longer depend on macOS-only `qlmanage`. It also refuses to silently synthesize fake sine-wave "speech" on machines without macOS `say`. If real speech fixtures already exist under `artifacts/fixtures/`, it reuses them; otherwise it fails loudly unless you explicitly opt in with `TG_E2E_ALLOW_SYNTHETIC_AUDIO_FIXTURES=1`.

Useful next reads:

- [docs/setup.md](./docs/setup.md)
- [docs/troubleshooting.md](./docs/troubleshooting.md)

## Interactive workflow

Interactive mode is a JSONL protocol over stdin/stdout. Each input line is one command, and each output line is one event.

When interactive mode starts, it emits an `info` event that tells you to select a chat first and reminds you that built-in pacing is already active.

Start by choosing the bot chat you want to inspect:

```json
{"id":"select","action":"select_chat","chat":"@your_bot_username"}
```

Then send a normal user action:

```json
{"id":"start","action":"send_text","text":"/start"}
```

After that you will typically wait for the visible chat state to change:

```json
{"id":"wait1","action":"wait","timeout_ms":8000}
```

Main output event types:

- `info`
- `ack`
- `state_update`
- `state_snapshot`
- `error`
- `timeout`

Each snapshot/update contains the current visible messages, inline buttons, pinned summary, sync time, and a diff relative to the previous snapshot when available.

## Scenario format

Scenarios are `JSONL` files. One line is one command.

Empty lines and lines starting with `#` are ignored.

Supported actions:

- `select_chat`
- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `send_document`
- `click_button`
- `wait`
- `dump_state`

Common fields:

- `id` — optional, but strongly recommended
- `action` — required
- `chat` — required for `select_chat`; optional for later commands once a current chat is already active
- `timeout_ms` — mainly used for `wait`

Per-action fields:

- `select_chat`: `chat`
- `send_text`: `text`
- `send_photo`: `path`, optional `caption`
- `send_voice`: `path`
- `send_audio`: `path`
- `send_document`: `path`, optional `caption`
- `click_button`: `button_text`, optional `message_offset`
- `wait`: optional `timeout_ms`
- `dump_state`: no extra fields

`click_button` resolves against the current pinned interactive screen first by default, even if that pinned message is not part of the visible history window. If the pinned screen does not contain the requested button, the tool falls back to visible interactive messages while ignoring exact visible duplicates of the current pinned screen. Use `message_offset` only when you intentionally want to click the same label in an older visible message, for example to probe stale-dashboard behavior.

Relative media paths in a scenario file are resolved relative to the scenario file location, not the current working directory.
Paths starting with `@fixtures/` resolve to the tool-owned fixture directory `artifacts/fixtures/`. This is useful when scenarios live in another repo but should still consume media fixtures generated by the test tool itself.

`run-scenario` can take more than one JSONL file. They are executed sequentially in one MTProto session, which is faster and avoids reconnect churn during longer runs.
After every `run-scenario` invocation, the tool refreshes these rolling artifacts under `artifacts/transcripts/`:

- per-scenario raw transcripts:
  - `<label>.json`
  - `<label>.txt`
- per-scenario compact transcripts:
  - `<label>.compact.json`
  - `<label>.compact.txt`
- rolling triage artifacts:
  - `last-run-artifacts.json`
  - `last-run-summary.json`
  - `last-run-summary.txt`
  - `last-failure.json`
  - `last-failure.txt`

The compact artifacts are intentionally lossy: they normalize common dashboard/draft states, strip decorative emoji noise, collapse repeated whitespace, omit callback payloads, and keep only the shortest useful state/button summaries.

For repository scenarios, the recommended first step is `select_chat`. This keeps the scenario format identical to interactive mode and avoids hidden target selection.

Do not run multiple `login`, `interactive`, `run-scenario`, `run-block`, `run-text-matrix`, or `rate-sweep` processes in parallel with the same Telegram account. The tool now blocks this with a runtime lock because parallel runs can interleave messages, clicks, waits, and benchmark probes in one chat and make results meaningless.

Triage order after a run:

1. open `artifacts/transcripts/last-run-summary.txt`
2. if failed, open `artifacts/transcripts/last-failure.txt`
3. only then open raw scenario transcripts if the compact artifacts are not enough

## Main commands

```bash
make help
make setup
make test
make doctor
make login
make interactive
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl CHAT=@your_bot_username
make run-text-matrix CHAT=@your_bot_username CASES=/absolute/path/to/cases.txt
make fixtures
make run-suite CHAT=@your_bot_username
make rate-sweep CHAT=@your_bot_username
make clean
```

Direct CLI usage works the same way because `.env` is auto-loaded:

```bash
go run ./cmd/tg-e2e-tool doctor
go run ./cmd/tg-e2e-tool interactive
CHAT=@your_bot_username go run ./cmd/tg-e2e-tool run-scenario examples/suite/03-text-draft-confirm.jsonl
go run ./cmd/tg-e2e-tool rate-sweep --chat @your_bot_username
```

## Built-in v1 suite

The ready-to-run suite in `examples/suite/` covers the full public surface of the tool against a real bot over MTProto.

Coverage includes:

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`
- target selection by `@username`
- explicit `select_chat` flow shared with interactive mode
- pinned summary
- service messages from chat history
- diff handling for added, changed, removed, and re-pinned visible state

Run it with:

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

`make run-suite` now reuses one MTProto session across the whole suite instead of reconnecting between scenario files.
It also stays strictly sequential. Do not start another live run against the same test account until the current one exits.
The suite runner now fails fast if `CHAT` is missing or the expected media fixtures have not been generated yet.
The suite is intentionally stateful and ordered: `01-start-pin-service` establishes the home dashboard once, and later scenarios reuse that state instead of sending `/start` again.

Or run a single scenario:

```bash
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl CHAT=@your_bot_username
```

If you want a fresh home dashboard before a targeted scenario, compose it with the helper fragment instead of copying `/start` into every test:

```bash
CHAT=@your_bot_username \
go run ./cmd/tg-e2e-tool run-scenario \
  examples/helpers/00-home-ready.jsonl \
  examples/suite/03-text-draft-confirm.jsonl
```

Or run several scenarios in one session:

```bash
CHAT=@your_bot_username \
go run ./cmd/tg-e2e-tool run-scenario \
  examples/suite/01-start-pin-service.jsonl \
  examples/suite/02-dashboard-navigation-edit.jsonl
```

`tg-e2e-tool run-scenario` accepts either absolute scenario paths or paths relative to the directory you invoked it from.
When another repo calls the tool through `make -C`, prefer absolute paths such as `$PWD/...`; `make -C` changes the working directory before the CLI starts.

For stateful multi-scenario blocks, keep the orchestration here instead of re-implementing it in product repos:

```bash
CHAT=@your_bot_username CONTROL_URL=http://127.0.0.1:8081 \
go run ./cmd/tg-e2e-tool run-block \
  /absolute/path/to/00-home-ready.jsonl \
  /absolute/path/to/11-timed-digest-setup.jsonl.tmpl
```

`tg-e2e-tool run-block` owns:

- optional `POST /control/e2e/reset` before the block
- `.jsonl.tmpl` rendering through `RUN_PREFIX`
- optional `POST /control/time/clear` on exit via `--clear-time`
- scenario resolution from the CLI working directory; for `make -C` from another repo, pass absolute paths

Prefer `CONTROL_URL`. `SHELFY_DEV_CONTROL_URL` is still accepted as a temporary alias during the migration away from product-owned wrappers.

If another product repo keeps only scenario files or a plain text case list, keep the orchestration here and point the tool at those external files:

```bash
make run-text-matrix \
  CHAT=@your_bot_username \
  CASES=/absolute/path/to/date-cases.txt
```

This renders one temporary JSONL scenario per line and runs it through the same `run-scenario` path. The default cleanup button is `↩️ Отмена`; override it with `CANCEL_BUTTON_TEXT=...` if another bot uses a different transient-close button.
`CASES` may also be relative to the directory you launched `tg-e2e-tool run-text-matrix` from.
If you call it through `make -C` from another repo, prefer an absolute path like `$PWD/...`.

Because v1 intentionally has no assert DSL, validation is done through:

- `artifacts/transcripts/*.json`
- `artifacts/transcripts/*.txt`
- current `ChatState` snapshots

Healthy-run criteria:

- no `error`
- no `timeout`
- `wait` returns a real visible-state change
- `dump_state` shows current messages, pinned summary, and buttons
- `click_button` resolves against the intended visible bot message

## Scenario design guidelines

For real Telegram E2E, faster and safer suites come more from better scenario design than from shaving every pacing millisecond.

- test one behavior per scenario fragment
- avoid embedding `/start` into every scenario; use one ordered setup fragment when a fresh home state is enough
- keep `select_chat` explicit so targeted runs stay readable and interactive mode matches saved scenarios
- if a scenario creates transient state such as a draft, either close it locally or run it late in the suite
- use repeated clicks only when repeated interaction is itself the behavior under test

## Configuration

Minimum `.env` values:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

Bundled repository scenarios intentionally keep an explicit `select_chat` step with a generic placeholder instead of hardcoding a specific bot username. `make run-scenario`, `make run-suite`, and `make rate-sweep` can materialize that placeholder from:

- `CHAT=@your_bot_username`

The MTProto pacing knobs are intentionally kept out of normal `.env` setup.
The tool uses built-in safe defaults for:

- visible history window
- sync interval
- outgoing action spacing
- generic MTProto RPC spacing
- pinned-state cache TTL

If you want to probe the fastest safe action spacing, use `make rate-sweep` instead of turning tuning values into persistent local config.

The visible history window is also auto-selected in code. It is intentionally not exposed as normal `.env` tuning, because this tool works best when recent-state coverage and sync payload size are balanced by one built-in value instead of per-machine guesswork.

[`.env.example`](./.env.example) already shows example value formats for:

- `TG_E2E_APP_HASH` as a 32-character hex string
- `TG_E2E_PHONE` in international format
- `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY`
- absolute paths for rare override cases

Optional local path overrides in `.env` are intentionally not needed for most users:

- `TG_E2E_SESSION_PATH`
- `TG_E2E_TRANSCRIPT_DIR`

The tool already uses sensible defaults:

- session: `.sessions/user.json`
- transcripts: `artifacts/transcripts`
- runtime lock: `.sessions/runtime.lock`

## Proxy support

- `HTTP_PROXY` / `HTTPS_PROXY` are supported via `HTTP CONNECT`
- `NO_PROXY` is respected
- `ALL_PROXY` can be used for `SOCKS5`

## Rate sweep

If you want to search for the fastest still-safe MTProto action spacing, run:

```bash
make rate-sweep CHAT=@your_bot_username
```

This uses three benchmark scenario types:

- read-heavy
- write-heavy callback navigation without persistent settings changes
- mixed draft creation followed by cancel instead of save

For the bundled Shelfy benchmark scenarios, `rate-sweep` automatically runs `examples/bench/00-shelfy-home-warmup.jsonl` before every probe on the built-in stable pacing. That keeps each probe on a fresh home baseline without letting repeated `/start` resets dominate the measured candidate pacing.

If you provide your own benchmark scenarios, you can do the same explicitly:

```bash
go run ./cmd/tg-e2e-tool rate-sweep \
  --chat @your_bot_username \
  --prepare-scenario examples/bench/00-shelfy-home-warmup.jsonl \
  path/to/custom-bench.jsonl
```

Instead of comparing a few named profiles, the tool now runs a binary search over `action_spacing` and keeps the read side fixed at the built-in hybrid values:

- `sync_interval=1600ms`
- `rpc_spacing=700ms`
- `pinned_cache_ttl=45000ms`

The search range defaults to:

- `min_action_ms=1800`
- `max_action_ms=3000`
- `resolution_ms=100`

Run sweeps sequentially only. They intentionally keep one live MTProto session and one Telegram account for the whole sweep; running two sweeps in parallel defeats the measurement and increases the chance of `FLOOD_WAIT`.

The summary is written under `artifacts/rate-sweep/`.

## Documentation

- [docs/setup.md](./docs/setup.md)
- [docs/troubleshooting.md](./docs/troubleshooting.md)

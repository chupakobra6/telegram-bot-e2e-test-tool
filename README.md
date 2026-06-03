# Telegram Bot E2E Test Tool

Real-user Telegram bot E2E testing over MTProto.

This tool signs in as a normal Telegram user, sends messages and media to a bot, clicks inline buttons, and records the chat the way a user sees it. It exists for cases where bot-side tests are not enough and you need the real pinned dashboard, transient drafts, service messages, and visible history.

## Highlights

- MTProto user session, not Bot API shortcuts
- one JSONL command format for interactive mode and saved scenarios
- visible `ChatState` snapshots, diffs, pinned summary, and transcript artifacts
- built-in Shelfy suite, text-matrix runner, and rate-sweep tooling
- one runtime lock per Telegram session to prevent parallel-account corruption

## Quick start

```bash
cp .env.example .env
make setup
make doctor
make login
make interactive
```

The CLI auto-loads `.env` from the current working directory and falls back to the repo root. You do not need to `source .env` manually.

For the built-in Shelfy suite:

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

`make run-suite` assumes a live local Shelfy dev stack with the non-production control API reachable at `http://127.0.0.1:8081`.

Useful next reads:

- [docs/setup.md](./docs/setup.md)
- [docs/troubleshooting.md](./docs/troubleshooting.md)

## Main commands

```bash
make help
make setup
make test
make doctor
make login
make chats CHAT_GROUPS=1 CHAT_FILTER=TrackMate CHAT_TOPICS=1 CHAT_ADMINS=1
make interactive
make run-scenario SCENARIO=examples/suite/03-text-fast-path-complete.jsonl CHAT=@your_bot_username
make run-text-matrix CHAT=@your_bot_username CASES=/absolute/path/to/cases.txt
make fixtures
make run-suite CHAT=@your_bot_username
make rate-sweep CHAT=@your_bot_username
make clean
```

`make clean` removes generated content under `artifacts/` and the default `.sessions/runtime.lock`, but keeps the saved MTProto session at `.sessions/user.json`.

## JSONL protocol

Supported actions:

- `select_chat`
- `send_text`
- `edit_text`
- `send_photo`
- `send_photo_group`
- `send_voice`
- `send_audio`
- `send_document`
- `click_button`
- `delete_visible_messages`
- `wait`
- `dump_state`
- `assert_visible_text`
- `assert_not_visible_text`

Interactive mode and saved scenarios use the same command contract and the same execution path.

For Telegram forum groups, commands can target a topic by passing the numeric topic starter ID as either `message_thread_id` or its short alias `topic`. A `select_chat` with a topic keeps that topic selected for following commands until another chat or topic is selected. Scenarios without a topic continue to target the normal chat history.

Use `tg-e2e-tool chats --groups --filter TrackMate --topics --admins` to find the MTProto target id, forum topic ids, and bot admin topic-management rights for scenario files. The `target=` value is the chat value accepted by `select_chat`; `topic_id=` is the value accepted by `message_thread_id`/`topic`.

For noisy chats with repeated inline buttons, `click_button` can be scoped with `message_text`. The button is clicked only on a visible message whose text contains that fragment.
`edit_text` edits a visible outgoing user message. Scope it with `message_text`
when several recent outgoing messages are present; `message_offset` selects the
newest matching message by default.
`assert_visible_text` and `assert_not_visible_text` check the current visible
snapshot and fail the scenario when the expected text state is not true.

For reusable test forum groups, `delete_visible_messages` removes the currently visible non-service messages from the selected chat/topic. It keeps the topic starter, pinned message, and Telegram service messages, so cleanup scenarios can preserve topic structure and pinned intro/control messages. Use `max_batches` to keep walking older history windows; the default is 8 batches. If a product intentionally leaves its intro unpinned, pass `keep_oldest: true` to keep the earliest ordinary visible message too.

```json
{"id":"select","action":"select_chat","chat":"@your_forum_group","message_thread_id":12345}
{"id":"start","action":"send_text","text":"/start"}
{"id":"fix_typo","action":"edit_text","message_text":"/start","text":"/start now"}
{"id":"open_report","action":"click_button","button_text":"🏁 Report","message_text":"Task title"}
{"id":"wait","action":"wait","timeout_ms":15000}
{"id":"assert_updated","action":"assert_visible_text","text":"Updated result"}
{"id":"cleanup","action":"delete_visible_messages","max_batches":8}
```

## Bundled Shelfy suite

The built-in suite in [`examples/suite/`](./examples/suite/) is kept in sync with current Shelfy product flows. It currently covers:

- `/start` idempotence and `/dashboard` recovery
- dashboard navigation and settings actions
- text fast-path draft creation and confirm
- incomplete draft name editing
- repeated date editing with invalid and valid input
- unsupported document upload
- unsupported photo fail-closed handling
- product close, discard, and delete flows
- voice upload and cleanup back to home
- audio upload and cleanup back to home
- blocked save on incomplete drafts and draft cancellation
- timed digest setup, observation, close, and reconciliation cleanup
- dashboard pagination and back-to-origin navigation
- rapid same-user interaction ordering around settings and draft editing

Run the full suite with:

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

Run one focused scenario with:

```bash
make run-scenario SCENARIO=examples/suite/05-edit-date-valid-invalid.jsonl CHAT=@your_bot_username
```

If you want a fresh home dashboard before a targeted scenario, prepend the helper fragment:

```bash
CHAT=@your_bot_username \
go run ./cmd/tg-e2e-tool run-scenario \
  examples/helpers/00-home-ready.jsonl \
  examples/suite/05-edit-date-valid-invalid.jsonl
```

## Fixtures and transcripts

`make fixtures` generates:

- `artifacts/fixtures/e2e-photo.png`
- `artifacts/fixtures/e2e-receipt.png`
- `artifacts/fixtures/e2e-blank-photo.png`
- `artifacts/fixtures/e2e-voice.ogg`
- `artifacts/fixtures/e2e-audio.mp3`
- `artifacts/fixtures/e2e-document.txt`

After every `run-scenario` or `run-suite`, the tool refreshes rolling transcript artifacts under `artifacts/transcripts/`:

- `last-run-artifacts.json`
- `last-run-summary.json`
- `last-run-summary.txt`
- `last-failure.json`
- `last-failure.txt`

Recommended triage order:

1. open `artifacts/transcripts/last-run-summary.txt`
2. if failed, open `artifacts/transcripts/last-failure.txt`
3. only then open per-scenario compact or raw transcripts

## Repository layout

- [`cmd/tg-e2e-tool`](./cmd/tg-e2e-tool) — main CLI
- [`cmd/fixturegen`](./cmd/fixturegen) — standalone PNG fixture generator
- [`examples/suite`](./examples/suite) — current bundled Shelfy suite
- [`examples/helpers`](./examples/helpers) — reusable helper fragments
- [`examples/bench`](./examples/bench) — benchmark and rate-sweep scenarios
- [`internal/protocol`](./internal/protocol) — JSONL command/event contract
- [`internal/engine`](./internal/engine) — action execution path
- [`internal/state`](./internal/state) — visible chat snapshot/diff model
- [`internal/mtproto`](./internal/mtproto) — Telegram transport and sync behavior

## Notes

- The tool is intentionally user-like: no Bot API shortcuts and no test-only bot backdoors.
- The bundled scenarios keep an explicit `select_chat` step and use the placeholder `@your_bot_username`; `run-scenario`, `run-suite`, and `rate-sweep` materialize it from `CHAT=@your_bot_username`.
- Relative media paths resolve relative to the scenario file. Paths starting with `@fixtures/` resolve to the tool-owned `artifacts/fixtures/` directory.
- `send_photo_group` sends one Telegram album/media group with 2-10 photo paths and one caption applied to every item:
  `{"id":"album","action":"send_photo_group","paths":["@fixtures/e2e-photo.png","@fixtures/e2e-receipt.png"],"caption":"общая подпись"}`
- `delete_visible_messages` uses the MTProto user account. In supergroups, that account needs enough rights to delete bot messages if the cleanup should remove both test-user messages and bot replies.
- Do not run `login`, `interactive`, `run-scenario`, `run-block`, `run-text-matrix`, or `rate-sweep` in parallel with the same Telegram account.

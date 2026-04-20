# Telegram Bot E2E Test Tool

A real-user E2E testing tool for Telegram bots over MTProto.

It logs in as a regular Telegram user, sends messages to a bot, clicks inline buttons, and captures the current chat state the way a user sees it.

## What v1 supports

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`
- interactive JSONL mode over stdin/stdout
- JSONL scenarios using the same commands as interactive mode
- transcript artifacts in JSON and text format

## Key design decisions

- `MTProto`, not Bot API  
  The tool should behave like a normal user, not like a bot or a testing backdoor.
- `JSONL` as the single protocol  
  Interactive mode and the scenario runner use the exact same command format.
- `ChatState` as a snapshot of visible history  
  After each action, the tool does not guess based on bot internals. It reads recent chat history, pinned state, and service messages.
- Built-in default paths  
  For normal local usage, you do not need to configure session or transcript paths in `.env`. Those env vars remain available only as advanced overrides.

## Quick start

```bash
cp .env.example .env
make doctor
make login
make interactive
```

If you want the full suite:

```bash
make fixtures
make run-suite
```

## Main commands

```bash
make help
make setup
make test
make doctor
make login
make interactive
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl
make fixtures
make run-suite
make clean
```

## What you actually need in `.env`

Minimum:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

You will usually also want:

- `TG_E2E_DEFAULT_CHAT=@your_bot_username`

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

## Proxy support

- `HTTP_PROXY` / `HTTPS_PROXY` are supported via `HTTP CONNECT`
- `NO_PROXY` is respected
- `ALL_PROXY` can be used for `SOCKS5`

## Example interactive command

```json
{"id":"start","action":"send_text","chat":"@your_bot_username","text":"/start"}
```

Example next step:

```json
{"id":"wait1","action":"wait","timeout_ms":8000}
```

## Where to look next

- [docs/setup.md](./docs/setup.md)
- [docs/overview.md](./docs/overview.md)
- [docs/scenario-format.md](./docs/scenario-format.md)
- [docs/interactive-mode.md](./docs/interactive-mode.md)
- [docs/scenario-suite.md](./docs/scenario-suite.md)
- [docs/troubleshooting.md](./docs/troubleshooting.md)

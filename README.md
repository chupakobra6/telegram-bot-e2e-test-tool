# Telegram Bot E2E Test Tool

Telegram Bot E2E Test Tool is a real-user Telegram testing utility built on MTProto.

## What it does

- logs in as a normal Telegram user
- sends text, photo, voice, and audio messages to a bot
- clicks inline buttons visible in chat
- syncs the latest visible chat history and pinned message
- writes JSON and text transcripts for later debugging

## Core model

- transport: MTProto via `gotd`
- execution protocol: JSONL commands over stdin/stdout
- scenario files: JSONL files with the same commands as interactive mode
- state model: latest visible chat snapshot plus a diff against the previous snapshot

## Commands

```bash
make setup
make test
make print-config
make login
make interactive
make run-scenario
```

## Quick start

1. Copy `.env.example` to `.env` and fill in the Telegram app credentials and test account variables.
2. Run `make login` once to create the MTProto user session.
3. Use `make interactive` to drive the bot with JSONL commands.
4. Use `make run-scenario` to execute a JSONL scenario file.

Before running commands in a plain shell:

```bash
set -a
source .env
set +a
```

## Example interactive command

```json
{"id":"start","action":"send_text","chat":"@your_bot_username","text":"/start"}
```

Each command is echoed back through JSONL events such as `ack`, `state_update`, `state_snapshot`, `error`, and `timeout`.

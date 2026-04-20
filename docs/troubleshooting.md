# Troubleshooting

## `telegram session is not authorized`

Run this first:

```bash
make login
```

Runtime commands require an existing MTProto session file.

## `TG_E2E_APP_ID` or `TG_E2E_APP_HASH` is required

Telegram app credentials are not set in `.env`.

Check:

```bash
make doctor
```

## `chat is required`

The command has no `chat` field, no current chat has been selected yet, and `TG_E2E_DEFAULT_CHAT` is not set.

## `button ... not found in visible messages`

There is no matching inline button in the latest relevant bot message of the current snapshot.

Run:

```json
{"id":"dump","action":"dump_state"}
```

Then inspect which messages and buttons the tool actually sees right now.

## `wait timeout`

The visible chat state did not change before the timeout expired.

This usually means one of three things:

- the bot did not respond
- the timeout is too small
- the expected change did not appear inside the visible history window

## Where transcripts are stored

By default:

- `artifacts/transcripts/*.json`
- `artifacts/transcripts/*.txt`

If the path was overridden, `make doctor` will show the effective location.

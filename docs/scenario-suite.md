# v1 Scenario Suite

This scenario suite covers the full public surface of `telegram-bot-e2e-test-tool` v1 against a real bot over MTProto.

## What it covers

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`
- target selection by `@username`
- default chat fallback through `TG_E2E_DEFAULT_CHAT`
- `pinned` summary
- service messages from chat history
- diff handling for:
  - adding a new message
  - removing messages
  - changing an existing message
  - changing the pinned message

## Scenarios

- `examples/suite/01-start-pin-service.jsonl`
  Covers `/start`, pinned state, and the `message pinned` service message.
- `examples/suite/02-dashboard-navigation-edit.jsonl`
  Covers dashboard navigation and editing the same pinned message.
- `examples/suite/03-text-draft-confirm.jsonl`
  Covers the text flow, draft card, confirmation, and transient message cleanup.
- `examples/suite/04-photo-processing-and-draft.jsonl`
  Covers `send_photo`, the initial processing message, and the later draft update.
- `examples/suite/05-voice-processing.jsonl`
  Covers `send_voice` and the bot's first visible reaction.
- `examples/suite/06-audio-processing.jsonl`
  Covers `send_audio` and the bot's first visible reaction.

## How to run it

First generate fixtures:

```bash
make fixtures
```

Then run the full suite:

```bash
make run-suite
```

Or run a single scenario:

```bash
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl
```

## What to inspect in the result

Because `v1` has no built-in asserts, validation is done through the transcript and `ChatState`.

Check:

- `artifacts/transcripts/*.json`
- `artifacts/transcripts/*.txt`

Healthy-run criteria:

- there is no `error` and no `timeout`
- `wait` returns the expected `state_update`, not an empty replay of an old snapshot
- `dump_state` shows current messages, pinned summary, and buttons
- service messages are present in history when Telegram exposed them
- `click_button` resolves against the latest relevant bot message, not a random stale button

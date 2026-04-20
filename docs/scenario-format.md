# Scenario Format

Scenarios are newline-delimited JSON files. Each line is one command object.

Blank lines and lines starting with `#` are ignored, which makes it safe to add short comments between steps.

## Why JSONL

- the scenario runner uses the exact same protocol as interactive mode
- there is no separate scenario DSL to maintain
- every line is directly replayable through stdin

## Supported actions

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`

## Common fields

- `id` optional but recommended
- `action` required
- `chat` optional; falls back to the current chat or `TG_E2E_DEFAULT_CHAT`
- `timeout_ms` optional; mainly used by `wait`

## Action fields

- `send_text`: `text`
- `send_photo`: `path`, optional `caption`
- `send_voice`: `path`
- `send_audio`: `path`
- `click_button`: `button_text`
- `wait`: optional `timeout_ms`
- `dump_state`: no extra fields

## Example

```json
{"id":"start","action":"send_text","text":"/start"}
{"id":"wait-dashboard","action":"wait","timeout_ms":5000}
{"id":"confirm","action":"click_button","button_text":"✅ Сохранить"}
```

The runner stops on the first transport, runtime, or timeout error and then writes transcript artifacts.

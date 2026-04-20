# Scenario Format

Scenarios are `JSONL` files. One line is one command.

Empty lines and lines starting with `#` are ignored.

## Why JSONL

- scenarios use the same format as interactive mode
- there is no separate DSL to maintain
- any line can be pasted directly into interactive mode stdin

## Supported actions

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`

## Common fields

- `id` — optional, but strongly recommended
- `action` — required
- `chat` — optional if a current chat already exists or `TG_E2E_DEFAULT_CHAT` is set
- `timeout_ms` — mainly used for `wait`

## Per-action fields

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
{"id":"confirm","action":"click_button","button_text":"Confirm"}
```

The runner stops on the first transport, runtime, or timeout error and saves transcript artifacts.

See [docs/scenario-suite.md](./scenario-suite.md) for the full ready-to-run suite.

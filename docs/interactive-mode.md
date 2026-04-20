# Interactive Mode

Interactive mode is a JSONL protocol over stdin/stdout.

## Input

Each incoming line is a JSON command.

Example:

```json
{"id":"start","action":"send_text","chat":"@your_bot_username","text":"/start"}
```

## Output

Each outgoing line is a JSON event.

Main event types:

- `ack`
- `state_update`
- `state_snapshot`
- `error`
- `timeout`

## What `state_update` and `state_snapshot` contain

- recent visible chat messages
- inline buttons
- pinned summary
- sync timestamp
- diff relative to the previous snapshot, if any

## Typical flow

1. send `/start`
2. wait for a change
3. click a visible button
4. call `dump_state` if needed

Because this is JSONL, an agent can drive the tool directly without a separate REPL wrapper.

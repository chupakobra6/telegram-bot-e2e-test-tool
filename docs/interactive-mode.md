# Interactive Mode

Interactive mode is JSONL over stdin/stdout.

## Input

Each input line is a JSON command:

```json
{"id":"start","action":"send_text","chat":"@your_bot_username","text":"/start"}
```

## Output

Each output line is a JSON event:

- `ack`
- `state_update`
- `state_snapshot`
- `error`
- `timeout`

`state_update` and `state_snapshot` include the current normalized chat view:

- latest visible messages
- inline buttons
- pinned message summary
- sync timestamp
- diff from the previous snapshot when available

## Typical flow

1. send `/start`
2. wait for the dashboard to appear
3. click a visible button
4. dump the state if manual inspection is needed

Because the protocol is JSONL, an agent can drive the tool directly without parsing a human shell.

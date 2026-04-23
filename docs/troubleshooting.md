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

The command has no `chat` field and no current chat has been selected yet.

Send `select_chat` first or pass `chat` explicitly on the command that needs it.

## `another tg-e2e-tool runtime is already active for this session`

Another local `login`, `interactive`, `run-scenario`, `run-block`, `run-text-matrix`, or `rate-sweep` process is already holding the runtime lock for this MTProto session.

This is expected protection, not a random failure. Do not run live Telegram scenarios in parallel with the same test account.

Wait for the other process to exit, then retry.

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

## `required fixture missing`

The built-in suite now expects the bundled local fixtures to exist:

- photo
- document
- voice
- audio

Run:

```bash
make fixtures
```

Then retry the scenario or suite.

## `run-suite requires reachable Shelfy control API`

The full built-in Shelfy suite now includes timed digest and reset-driven flows.

Check that your local Shelfy dev stack is running and that the non-production control API is reachable:

```bash
curl -X POST http://127.0.0.1:8081/control/time/clear
```

If you use a different endpoint, set `CONTROL_URL`.

## `FLOOD_WAIT`

The tool already uses conservative defaults for:

- sync polling
- outgoing action spacing
- generic MTProto RPC spacing
- pinned-state caching

If you still hit `FLOOD_WAIT`, do not start by turning pacing into permanent `.env` config.

Use:

```bash
make rate-sweep CHAT=@your_bot_username
```

Then inspect which `action_spacing` candidate fails first.

If you still need a slower account-safe profile for local debugging, prefer changing the sweep or runner invocation rather than keeping permanent tuning values in `.env`.

## Where transcripts are stored

By default:

- `artifacts/transcripts/*.json`
- `artifacts/transcripts/*.txt`
- `artifacts/transcripts/*.compact.json`
- `artifacts/transcripts/*.compact.txt`
- `artifacts/transcripts/last-run-summary.txt`
- `artifacts/transcripts/last-failure.txt`

If the path was overridden, `make doctor` will show the effective location.

Prefer the rolling triage files first:

1. `last-run-summary.txt`
2. `last-failure.txt`
3. only then the raw transcript pair for the failing scenario

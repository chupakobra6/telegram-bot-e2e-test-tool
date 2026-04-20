# Troubleshooting

## `telegram session is not authorized`

Run `make login` first. The runtime commands require an existing MTProto session file.

## `TG_E2E_APP_ID` or `TG_E2E_APP_HASH` is required

Your Telegram app credentials are missing or empty. Export them before starting the tool.

## `chat is required`

The command did not include `chat`, there is no current chat from a previous command, and `TG_E2E_DEFAULT_CHAT` is unset.

## `button ... not found in visible messages`

The latest synced chat snapshot does not currently show a matching inline button. Run `dump_state` and inspect the visible messages before clicking again.

## `wait timeout`

The visible chat state did not change before the timeout expired. This usually means:

- the bot did not answer
- the bot answered outside the latest synced history window
- the timeout is too small for the scenario

## Where are transcripts stored?

By default in `artifacts/transcripts/` as both JSON and text files.

# Setup

## 1. Prepare the environment

```bash
cd telegram-bot-e2e-test-tool
cp .env.example .env
```

Fill in:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

[`.env.example`](../.env.example) already contains example value formats, including:

- app hash
- phone number
- proxy URL
- absolute paths for optional overrides

You will usually also want:

- `TG_E2E_DEFAULT_CHAT=@your_bot_username`

## 2. Check the effective config

```bash
make doctor
```

`doctor` shows:

- whether Telegram credentials are set
- which default chat will be used
- where the session file lives
- whether the session file exists
- which proxy variables were picked up

## 3. Create an MTProto session

```bash
make login
```

The tool will ask for a code from Telegram, and for a password if the account uses 2FA.

## 4. Start interactive mode

```bash
make interactive
```

Example command:

```json
{"id":"start","action":"send_text","text":"/start"}
```

## 5. Run a single scenario

```bash
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl
```

## 6. Run the full v1 suite

```bash
make fixtures
make run-suite
```

## Paths and overrides

By default, the tool uses:

- session: `.sessions/user.json`
- transcripts: `artifacts/transcripts`

You usually do not need to override them in `.env`. `TG_E2E_SESSION_PATH` and `TG_E2E_TRANSCRIPT_DIR` are kept only for non-standard setups.

## Proxy support

- `HTTP_PROXY` / `HTTPS_PROXY` work through `HTTP CONNECT`
- `NO_PROXY` is respected
- `ALL_PROXY` can be used for `SOCKS5`

If you run commands through `make`, `.env` is loaded automatically.

# Setup

## 1. Install dependencies

```bash
cd telegram-bot-e2e-test-tool
make setup
```

## 2. Create Telegram app credentials

Create `api_id` and `api_hash` at `https://my.telegram.org`.

## 3. Use a dedicated test account

Use a separate Telegram user account for E2E testing. This tool logs in as that user through MTProto.

## 4. Configure environment

Start from the example file:

```bash
cp .env.example .env
set -a
source .env
set +a
```

Required:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

Optional:

- `TG_E2E_PASSWORD` for 2FA accounts
- `TG_E2E_SESSION_PATH` default: `.sessions/user.json`
- `TG_E2E_TRANSCRIPT_DIR` default: `artifacts/transcripts`
- `TG_E2E_DEFAULT_CHAT` default chat target such as `@shelfy_bot`
- `TG_E2E_HISTORY_LIMIT` default: `50`
- `TG_E2E_SYNC_INTERVAL_MS` default: `1200`

## 5. Create the MTProto session

```bash
make login
```

The tool will prompt for the login code and then persist the session file.

## 6. Run the tool

Interactive:

```bash
make interactive
```

Scenario:

```bash
make run-scenario
```

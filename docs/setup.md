# Setup

## 1. Подготовить окружение

```bash
cd telegram-bot-e2e-test-tool
cp .env.example .env
```

Заполнить:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

Обычно еще стоит задать:

- `TG_E2E_DEFAULT_CHAT=@your_bot_username`

## 2. Проверить effective config

```bash
make doctor
```

`doctor` показывает:

- заданы ли Telegram credentials
- какой default chat будет использоваться
- где лежит session file
- существует ли session file
- какие proxy-переменные подхвачены

## 3. Создать MTProto session

```bash
make login
```

Tool попросит код из Telegram, а если у аккаунта включен 2FA — пароль.

## 4. Запустить interactive mode

```bash
make interactive
```

Пример команды:

```json
{"id":"start","action":"send_text","text":"/start"}
```

## 5. Запустить один сценарий

```bash
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl
```

## 6. Запустить весь v1 suite

```bash
make fixtures
make run-suite
```

## Пути и overrides

По умолчанию tool сам использует:

- session: `.sessions/user.json`
- transcripts: `artifacts/transcripts`

Переопределять их через `.env` обычно не нужно. Поля `TG_E2E_SESSION_PATH` и `TG_E2E_TRANSCRIPT_DIR` оставлены только для нестандартных случаев.

## Прокси

- `HTTP_PROXY` / `HTTPS_PROXY` работают через `HTTP CONNECT`
- `NO_PROXY` учитывается
- `ALL_PROXY` можно использовать для `SOCKS5`

Если ты запускаешь команды через `make`, `.env` подхватывается автоматически.

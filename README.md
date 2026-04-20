# Telegram Bot E2E Test Tool

Инструмент для real-user E2E тестирования Telegram-ботов через MTProto.

Он логинится как обычный пользователь Telegram, отправляет сообщения в бота, нажимает inline-кнопки и снимает текущее состояние чата так, как его видит пользователь.

## Что умеет v1

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`
- интерактивный JSONL-режим через stdin/stdout
- JSONL-сценарии теми же командами, что и interactive mode
- transcript artifacts в JSON и text

## Ключевые решения

- `MTProto`, а не Bot API  
  Tool должен вести себя как обычный пользователь, а не как бот или тестовая ручка.
- `JSONL` как единый протокол  
  Interactive mode и scenario runner используют один и тот же формат команд.
- `ChatState` как снимок видимой истории  
  После действий tool не гадает по внутренним событиям бота, а читает последние сообщения чата, pinned state и service messages.
- Пути по умолчанию зашиты в tool  
  Для локального использования не нужно настраивать session/transcript paths через `.env`. Эти env-поля оставлены только как advanced override.

## Быстрый старт

```bash
cp .env.example .env
make doctor
make login
make interactive
```

Если нужен полный suite:

```bash
make fixtures
make run-suite
```

## Основные команды

```bash
make help
make setup
make test
make doctor
make login
make interactive
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl
make fixtures
make run-suite
make clean
```

## Что действительно нужно в `.env`

Минимум:

- `TG_E2E_APP_ID`
- `TG_E2E_APP_HASH`
- `TG_E2E_PHONE`

Практически удобно еще задать:

- `TG_E2E_DEFAULT_CHAT=@your_bot_username`

Необязательные локальные пути в `.env` специально не нужны почти никому:

- `TG_E2E_SESSION_PATH`
- `TG_E2E_TRANSCRIPT_DIR`

Tool и так использует нормальные defaults:

- session: `.sessions/user.json`
- transcripts: `artifacts/transcripts`

## Прокси

- `HTTP_PROXY` / `HTTPS_PROXY` поддерживаются через `HTTP CONNECT`
- `NO_PROXY` учитывается
- `ALL_PROXY` можно использовать для `SOCKS5`

## Пример interactive команды

```json
{"id":"start","action":"send_text","chat":"@your_bot_username","text":"/start"}
```

Пример следующего шага:

```json
{"id":"wait1","action":"wait","timeout_ms":8000}
```

## Куда смотреть дальше

- [docs/setup.md](./docs/setup.md)
- [docs/overview.md](./docs/overview.md)
- [docs/scenario-format.md](./docs/scenario-format.md)
- [docs/interactive-mode.md](./docs/interactive-mode.md)
- [docs/scenario-suite.md](./docs/scenario-suite.md)
- [docs/troubleshooting.md](./docs/troubleshooting.md)

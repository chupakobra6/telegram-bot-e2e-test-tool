# Набор Сценариев v1

Этот набор сценариев покрывает весь публичный функционал `telegram-bot-e2e-test-tool` v1 на реальном боте через MTProto.

## Что покрывается

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`
- target по `@username`
- default chat fallback через `TG_E2E_DEFAULT_CHAT`
- `pinned` summary
- service messages из истории чата
- diff на:
  - добавление нового сообщения
  - удаление сообщений
  - изменение уже существующего сообщения
  - смену pinned message

## Сценарии

- `examples/suite/01-start-pin-service.jsonl`
  Проверяет `/start`, pinned state и service message `message pinned`.
- `examples/suite/02-dashboard-navigation-edit.jsonl`
  Проверяет click по dashboard и edit одного pinned message.
- `examples/suite/03-text-draft-confirm.jsonl`
  Проверяет text flow, draft card, confirm и cleanup transient сообщений.
- `examples/suite/04-photo-processing-and-draft.jsonl`
  Проверяет `send_photo`, первое processing-сообщение и поздний draft update.
- `examples/suite/05-voice-processing.jsonl`
  Проверяет `send_voice` и первую реакцию бота.
- `examples/suite/06-audio-processing.jsonl`
  Проверяет `send_audio` и первую реакцию бота.

## Как запускать

Сначала подготовить фикстуры:

```bash
make fixtures
```

Потом прогнать весь suite:

```bash
make run-suite
```

Или отдельный сценарий:

```bash
make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl
```

## Что смотреть в результате

Поскольку в `v1` нет встроенных asserts, проверка идет по transcript и `ChatState`.

Смотри:

- `artifacts/transcripts/*.json`
- `artifacts/transcripts/*.txt`

Критерии нормальной работы:

- нет `error` и `timeout`
- после `wait` приходит ожидаемый `state_update`, а не пустой повтор старого snapshot
- `dump_state` показывает актуальные сообщения, pinned summary и кнопки
- service messages реально присутствуют в истории, если Telegram их показал
- `click_button` работает по последнему релевантному bot-message, а не по случайной старой кнопке

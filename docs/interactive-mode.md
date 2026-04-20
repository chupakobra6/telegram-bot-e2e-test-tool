# Interactive Mode

Interactive mode — это JSONL-протокол через stdin/stdout.

## Вход

Каждая входящая строка — JSON-команда.

Пример:

```json
{"id":"start","action":"send_text","chat":"@your_bot_username","text":"/start"}
```

## Выход

Каждая исходящая строка — JSON-event.

Основные типы:

- `ack`
- `state_update`
- `state_snapshot`
- `error`
- `timeout`

## Что содержат `state_update` и `state_snapshot`

- последние видимые сообщения чата
- inline-кнопки
- pinned summary
- timestamp синка
- diff относительно прошлого snapshot, если он есть

## Типичный цикл

1. отправить `/start`
2. дождаться `wait`
3. нажать видимую кнопку
4. при необходимости сделать `dump_state`

Поскольку это JSONL, агент может управлять инструментом напрямую без отдельной REPL-обвязки.

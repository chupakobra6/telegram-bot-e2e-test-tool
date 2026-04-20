# Troubleshooting

## `telegram session is not authorized`

Сначала выполни:

```bash
make login
```

Runtime-команды требуют уже существующий MTProto session file.

## `TG_E2E_APP_ID` или `TG_E2E_APP_HASH` is required

В `.env` не заданы Telegram app credentials.

Проверь:

```bash
make doctor
```

## `chat is required`

У команды нет поля `chat`, current chat еще не выбран, и `TG_E2E_DEFAULT_CHAT` не задан.

## `button ... not found in visible messages`

В текущем snapshot нет подходящей inline-кнопки в последнем релевантном bot-message.

Сделай:

```json
{"id":"dump","action":"dump_state"}
```

И посмотри, какие сообщения и кнопки сейчас реально видит tool.

## `wait timeout`

Видимое состояние чата не изменилось до истечения timeout.

Обычно это значит одно из трех:

- бот не ответил
- timeout слишком маленький
- нужное изменение не попало в видимое окно истории

## Где лежат транскрипты

По умолчанию:

- `artifacts/transcripts/*.json`
- `artifacts/transcripts/*.txt`

Если путь был переопределен, `make doctor` покажет effective location.

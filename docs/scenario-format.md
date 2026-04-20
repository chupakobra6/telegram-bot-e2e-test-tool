# Формат Сценариев

Сценарии — это `JSONL` файлы. Одна строка — одна команда.

Пустые строки и строки, начинающиеся с `#`, игнорируются.

## Почему JSONL

- сценарии используют тот же формат, что и interactive mode
- нет отдельного DSL, который надо поддерживать отдельно
- любую строку можно буквально вставить в stdin interactive mode

## Поддерживаемые actions

- `send_text`
- `send_photo`
- `send_voice`
- `send_audio`
- `click_button`
- `wait`
- `dump_state`

## Общие поля

- `id` — опционально, но очень желательно
- `action` — обязательно
- `chat` — опционально, если уже есть current chat или задан `TG_E2E_DEFAULT_CHAT`
- `timeout_ms` — в основном для `wait`

## Поля по action

- `send_text`: `text`
- `send_photo`: `path`, опционально `caption`
- `send_voice`: `path`
- `send_audio`: `path`
- `click_button`: `button_text`
- `wait`: опционально `timeout_ms`
- `dump_state`: без дополнительных полей

## Пример

```json
{"id":"start","action":"send_text","text":"/start"}
{"id":"wait-dashboard","action":"wait","timeout_ms":5000}
{"id":"confirm","action":"click_button","button_text":"✅ Сохранить"}
```

Runner останавливается на первой transport/runtime/timeout ошибке и сохраняет transcript artifacts.

Готовый полный suite смотри в [docs/scenario-suite.md](./scenario-suite.md).

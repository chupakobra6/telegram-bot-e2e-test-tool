# Обзор

`telegram-bot-e2e-test-tool` управляет Telegram-ботом как обычный пользователь через MTProto.

## Scope v1

- только private chat с ботом
- JSONL interactive mode через stdin/stdout
- JSONL scenario runner теми же командами
- `ChatState` из последних сообщений истории
- pinned summary
- service messages в видимой истории
- transcript artifacts для автодебага

## Как это работает

1. Tool логинится отдельным Telegram-аккаунтом.
2. Принимает JSON-команду.
3. Выполняет пользовательское действие в Telegram.
4. Снимает новый snapshot истории чата и pinned state.
5. Возвращает `state_snapshot` или `state_update`.

## Почему так

- нет отдельного test DSL поверх интерактивных команд
- нет test-only ручек, которых нет у обычного пользователя
- нет зависимости от Bot API
- проще дебажить сценарии по одному формату команд и одному формату transcript

# Telegram Bot E2E Test Tool

Сквозное тестирование Telegram-ботов через пользовательский сеанс MTProto.

Инструмент входит в Telegram как обычный пользователь, отправляет боту сообщения и медиа, нажимает встроенные кнопки и сохраняет видимое состояние чата. Поддерживает приватные чаты, группы и темы форумов. Он нужен, когда серверных тестов бота недостаточно: требуется увидеть настоящее закреплённое сообщение, временные черновики, служебные сообщения и историю чата.

## Возможности

- пользовательские действия через MTProto-сеанс;
- единый формат JSONL-команд для интерактивного режима и сохранённых сценариев;
- снимки видимого `ChatState`, различия, состояние закреплённого сообщения и протоколы выполнения;
- встроенный набор сценариев Shelfy, запуск текстовых наборов и подбор интервала между запросами;
- блокировка на Telegram-сеанс для ограничения одновременного доступа к аккаунту.

## Быстрый запуск

```bash
cp .env.example .env
make setup
make doctor
make login
make interactive
```

CLI автоматически загружает `.env` из рабочего каталога, а при его отсутствии — из корня репозитория. Выполнять `source .env` вручную не нужно.

Для встроенного набора сценариев Shelfy:

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

Для `make run-suite` требуется запущенное локальное тестовое окружение Shelfy с control API по адресу `http://127.0.0.1:8081`.

Подробности:

- [Настройка](./docs/setup.md)
- [Разбор неполадок](./docs/troubleshooting.md)

## Основные команды

```bash
make help
make setup
make test
make doctor
make login
make chats CHAT_GROUPS=1 CHAT_FILTER=TrackMate CHAT_TOPICS=1 CHAT_ADMINS=1
make interactive
make run-scenario SCENARIO=examples/suite/03-text-fast-path-complete.jsonl CHAT=@your_bot_username
make run-text-matrix CHAT=@your_bot_username CASES=/absolute/path/to/cases.txt
make fixtures
make run-suite CHAT=@your_bot_username
make rate-sweep CHAT=@your_bot_username
make clean
```

`make clean` удаляет сгенерированные файлы в `artifacts/` и стандартный `.sessions/runtime.lock`, сохраняя MTProto-сеанс в `.sessions/user.json`. Выполняй очистку после остановки запусков.

## Протокол JSONL

Поддерживаемые действия:

- `select_chat`
- `send_text`
- `send_bot_note`
- `edit_text`
- `send_photo`
- `send_photo_group`
- `send_voice`
- `send_audio`
- `send_document`
- `click_button`
- `delete_visible_messages`
- `wait`
- `sleep`
- `dump_state`
- `assert_visible_text`
- `assert_not_visible_text`

Интерактивный режим и сохранённые сценарии используют один контракт команд и один путь исполнения.

В группах-форумах команду можно направить в тему: передай числовой ID начального сообщения темы в `message_thread_id` или его короткий вариант `topic`. После `select_chat` тема сохраняется для последующих команд до выбора другого чата или темы. Сценарии без темы работают с обычной историей чата.

Команда `tg-e2e-tool chats --groups --filter TrackMate --topics --admins` показывает MTProto-идентификатор чата, ID тем и права бота-администратора на управление темами. Значение `target=` передаётся в поле чата команды `select_chat`, а `topic_id=` — в `message_thread_id`/`topic`.

Если в чате повторяются одинаковые кнопки, ограничь `click_button` полем `message_text`: кнопка нажимается только в видимом сообщении, содержащем этот фрагмент.

`edit_text` редактирует видимое исходящее сообщение пользователя. При нескольких сообщениях задай `message_text`; `message_offset` по умолчанию выбирает самое новое совпадение.

`send_bot_note` отправляет видимое пояснение через Bot API. Так бот не обрабатывает аннотацию демонстрации как пользовательский ввод из MTProto-сеанса. Нужны `TG_E2E_BOT_TOKEN` и `TG_E2E_BOT_CHAT_ID`.

`sleep` делает паузу на `timeout_ms`, не ожидая изменения чата. `assert_visible_text` и `assert_not_visible_text` проверяют текст в текущем видимом снимке и завершают сценарий ошибкой, если ожидаемое условие не выполнено.

При повторном использовании тестовой группы-форума `delete_visible_messages` удаляет видимые неслужебные сообщения выбранного чата или темы. Начальное сообщение темы, закреплённое сообщение и служебные сообщения Telegram сохраняются — вместе со структурой темы и закреплёнными вводными или управляющими сообщениями. `max_batches` задаёт число проходов по более старым окнам истории; по умолчанию — 8. Если вводное сообщение продукта намеренно не закреплено, передай `keep_oldest: true`, чтобы сохранить также самое раннее обычное видимое сообщение. Поле `message_text` ограничивает удаление сообщениями с указанным фрагментом; это удобно для временных пояснений к завершённому шагу демонстрации.

```json
{"id":"select","action":"select_chat","chat":"@your_forum_group","message_thread_id":12345}
{"id":"start","action":"send_text","text":"/start"}
{"id":"fix_typo","action":"edit_text","message_text":"/start","text":"/start now"}
{"id":"open_report","action":"click_button","button_text":"🏁 Report","message_text":"Task title"}
{"id":"wait","action":"wait","timeout_ms":15000}
{"id":"assert_updated","action":"assert_visible_text","text":"Updated result"}
{"id":"cleanup","action":"delete_visible_messages","max_batches":8}
{"id":"cleanup_note","action":"delete_visible_messages","message_text":"1/6 Demo note","max_batches":2}
```

## Встроенный набор сценариев Shelfy

Встроенный набор [`examples/suite/`](./examples/suite/) сопровождает пользовательские сценарии Shelfy:

- повторный `/start` и восстановление через `/dashboard`;
- навигация по главной панели и настройкам;
- быстрое создание черновика из текста и подтверждение;
- изменение названия неполного черновика;
- повторное изменение даты, ошибочный и допустимый ввод;
- загрузка неподдерживаемого документа;
- отклонение неподдерживаемой фотографии;
- закрытие продукта как съеденного или выброшенного и удаление;
- загрузка голосового сообщения, очистка и возврат к главной панели;
- загрузка аудиофайла, очистка и возврат к главной панели;
- отказ сохранения неполного черновика и отмена черновика;
- подготовка утренней сводки в управляемое время, наблюдение, закрытие продукта и очистка сводки;
- постраничный список и возврат к исходному месту навигации;
- быстрые последовательные действия одного пользователя в настройках и при редактировании черновика.

Набор выполняет действия и сохраняет наблюдаемое состояние. Текущие сценарии не проверяют автоматически все перечисленные свойства: например, наблюдение и очистка сводки ограничены снимками. Успешное исполнение команд само по себе не подтверждает каждый продуктовый результат; проверяй соответствующий снимок и действующие утверждения сценария.

Весь набор:

```bash
make fixtures
make run-suite CHAT=@your_bot_username
```

Один сценарий:

```bash
make run-scenario SCENARIO=examples/suite/05-edit-date-valid-invalid.jsonl CHAT=@your_bot_username
```

Чтобы перед отдельным сценарием вернуть главную панель в исходное состояние, добавь вспомогательный фрагмент:

```bash
CHAT=@your_bot_username \
go run ./cmd/tg-e2e-tool run-scenario \
  examples/helpers/00-home-ready.jsonl \
  examples/suite/05-edit-date-valid-invalid.jsonl
```

## Тестовые файлы и протоколы выполнения

`make fixtures` создаёт:

- `artifacts/fixtures/e2e-photo.png`
- `artifacts/fixtures/e2e-receipt.png`
- `artifacts/fixtures/e2e-blank-photo.png`
- `artifacts/fixtures/e2e-voice.ogg`
- `artifacts/fixtures/e2e-audio.mp3`
- `artifacts/fixtures/e2e-document.txt`

`run-scenario` и `run-suite` сохраняют протоколы и обновляют файлы последнего запуска в `artifacts/transcripts/`:

- `last-run-artifacts.json`
- `last-run-summary.json`
- `last-run-summary.txt`
- `last-failure.json`
- `last-failure.txt`

Порядок разбора результата:

1. Открой `artifacts/transcripts/last-run-summary.txt`.
2. При ошибке открой `artifacts/transcripts/last-failure.txt`.
3. Для подробностей прочитай краткий или исходный протокол нужного сценария.

Сверяй итог с кодом завершения команды и сообщениями об ошибках сохранения. Текущая сводка не отражает все отказы подготовки, разбора сценария и записи артефактов; отсутствие ошибки в ней не всегда означает полный успешный запуск.

## Устройство репозитория

- [`cmd/tg-e2e-tool`](./cmd/tg-e2e-tool) — основной CLI;
- [`cmd/fixturegen`](./cmd/fixturegen) — отдельный генератор тестовых PNG;
- [`examples/suite`](./examples/suite) — встроенный набор сценариев Shelfy;
- [`examples/helpers`](./examples/helpers) — повторно используемые вспомогательные фрагменты;
- [`examples/bench`](./examples/bench) — измерительные сценарии и подбор интервала между запросами;
- [`internal/protocol`](./internal/protocol) — контракт JSONL-команд и событий;
- [`internal/engine`](./internal/engine) — исполнение действий;
- [`internal/state`](./internal/state) — модель видимого снимка чата и различий;
- [`internal/mtproto`](./internal/mtproto) — Telegram-транспорт и обновление состояния.

## Условия работы

- Действия проверяемого пользователя выполняются через MTProto, а отдельный `send_bot_note` использует Bot API для пояснений к демонстрации. [Блоки с подготовкой состояния](./docs/setup.md#10-run-a-stateful-block) и [встроенный набор Shelfy](./docs/setup.md#6-run-the-built-in-shelfy-suite) используют control API тестового окружения для подготовки/сброса состояния и запуска заданий по времени; продуктовый результат наблюдается через Telegram.
- Во встроенных сценариях есть явный шаг `select_chat` с заполнителем `@your_bot_username`; `run-scenario`, `run-suite` и `rate-sweep` подставляют в него значение из `CHAT=@your_bot_username`.
- Относительные пути медиа отсчитываются от файла сценария. Префикс `@fixtures/` указывает на каталог инструмента `artifacts/fixtures/`.
- `send_photo_group` отправляет один Telegram-альбом: от 2 до 10 путей фотографий и общую подпись, применяемую к каждому элементу:
  `{"id":"album","action":"send_photo_group","paths":["@fixtures/e2e-photo.png","@fixtures/e2e-receipt.png"],"caption":"общая подпись"}`
- `delete_visible_messages` использует MTProto-аккаунт пользователя. Для удаления и пользовательских сообщений, и ответов бота в супергруппе этому аккаунту нужны права на удаление сообщений бота.
- Запускай `login`, `interactive`, `run-scenario`, `run-block`, `run-text-matrix` и `rate-sweep` последовательно для одного Telegram-аккаунта.

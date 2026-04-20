# Overview

`telegram-bot-e2e-test-tool` controls a Telegram bot like a regular user over MTProto.

## Scope v1

- private chat with a bot only
- JSONL interactive mode over stdin/stdout
- JSONL scenario runner using the same commands
- `ChatState` built from recent chat history
- pinned summary
- service messages in visible history
- transcript artifacts for auto-debugging

## How it works

1. The tool logs in with a separate Telegram account.
2. It accepts a JSON command.
3. It performs a user-like action in Telegram.
4. It captures a fresh chat-history snapshot and pinned state.
5. It returns `state_snapshot` or `state_update`.

## Why it works this way

- there is no separate test DSL on top of interactive commands
- there are no test-only hooks that a regular user would not have
- there is no dependency on Bot API
- scenarios are easier to debug when there is one command format and one transcript format

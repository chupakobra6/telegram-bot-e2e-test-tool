# Overview

Telegram Bot E2E Test Tool drives Telegram bots as a normal Telegram user over MTProto.

## v1 scope

- private chat with a bot
- JSONL interactive mode over stdin/stdout
- JSONL scenario runner using the exact same command format
- visible chat snapshots built from recent history syncs
- transcript artifacts for agent debugging

## How it works

1. A test user account authenticates through MTProto.
2. A command is received in JSON form.
3. The tool executes the matching Telegram user action.
4. The tool syncs the latest visible chat history and pinned message.
5. The tool emits a new JSON event containing the current snapshot or a diff.

## Design rules

- no Bot API transport
- no test-only actions that a normal user cannot perform
- no separate YAML DSL
- no built-in assert language in v1
- scenario files and interactive mode share one action format

# AGENTS.md

## Project overview
- This repository contains a real-user Telegram bot E2E testing tool built on MTProto.
- v1 is intentionally scoped to private bot chats.
- Keep changes minimal and targeted unless a structural change is clearly required.
- Preserve the current architecture unless the task genuinely needs a new boundary.

## How to work
- Start non-trivial tasks with a short plan before editing code.
- For implementation work, explain which files will change and why.
- If the task starts drifting, restate the plan and continue from the updated scope.

## Source of truth
- Follow nearby code patterns first.
- Treat these files as the canonical entrypoints for behavior:
  - `cmd/tg-e2e-tool/main.go` for CLI commands and transcript save flow
  - `internal/protocol/protocol.go` for the JSONL command/event contract
  - `internal/engine/engine.go` for action execution semantics
  - `internal/state/state.go` for `ChatState` and snapshot/diff behavior
  - `internal/mtproto/client.go` for Telegram transport and chat sync behavior
- Treat `docs/*.md` and `examples/suite/*.jsonl` as the canonical user-facing contract for setup and supported flows.

## Project constraints
- Keep the tool user-like:
  - prefer MTProto user actions;
  - do not add Bot API shortcuts;
  - do not add test-only hooks that a normal Telegram user would not have unless explicitly requested.
- The scenario format is JSONL, not YAML.
- Interactive mode and scenario mode must keep using the same command format and the same execution path.
- Default local paths should stay sane in code; do not make path env vars mandatory unless there is a strong reason.

## Commands
- Install/update dependencies: `make setup`
- Run tests: `make test`
- Show effective config: `make doctor`
- Print compact config: `make print-config`
- Create MTProto session: `make login`
- Run interactive mode: `make interactive`
- Run one scenario: `make run-scenario SCENARIO=examples/suite/03-text-draft-confirm.jsonl`
- Generate local media fixtures: `make fixtures`
- Run the full v1 suite: `make run-suite`
- Clean generated artifacts and the default runtime lock: `make clean`
- `make clean` should keep the saved MTProto session at `.sessions/user.json`
- Runtime-oriented `make` targets should load `.env` automatically; do not require users to `source .env` manually for normal local usage.

## Code change policy
- Keep diffs small and local.
- Do not rename or move files unless it materially improves the repo.
- Do not introduce new dependencies without a clear benefit.
- Update tests for changed behavior.
- Update docs and example scenarios when the public protocol, CLI, setup, or runtime behavior changes.

## Verification
- Run the narrowest relevant validation first.
- If protocol, state modeling, transcript logic, or CLI behavior changes, run at least:
  - `go test ./...`
- If command help or setup flow changes, also check:
  - `make help`
  - `make doctor`
- If live Telegram behavior changes and the local environment is available, prefer validating with:
  - one focused `make run-scenario ...`, or
  - `make run-suite` for broader end-to-end coverage
- Do not claim success without checking command output.

## Safety
- Never hardcode secrets, credentials, session contents, or personal account data.
- Do not commit `.env`, `.sessions/`, or generated artifacts.
- Prefer a dedicated Telegram test account, not a personal primary account.
- Prefer testing against disposable or clearly test-only chats/bots when possible.

## Documentation boundaries
- Keep tracked docs in English unless the user explicitly wants another language.
- Put reusable setup, protocol, and troubleshooting guidance in `README.md` and `docs/*.md`.
- Keep repo-specific agent guidance in this file concise and stable.
- Do not put transient debugging notes, one-off procedures, or personal machine details into `AGENTS.md`.
- Keep `README.md`, `docs/*.md`, `.env.example`, and `make help` consistent when setup or CLI UX changes.
- `.env.example` should include concrete example value formats for required credentials, proxy settings, and optional path overrides; do not leave onboarding-critical fields as formatless placeholders.

## Knowledge capture
- Update `AGENTS.md` only for stable, high-signal repo rules:
  - important invariants,
  - canonical commands,
  - repeated mistakes,
  - validation requirements,
  - project-specific constraints.
- Put long workflows and operational detail into docs, not here.

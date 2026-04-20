SHELL := /bin/bash
ENV_RUN := set -a; [ -f .env ] && source ./.env; set +a;
SCENARIO ?= examples/shelfy-smoke.jsonl

.DEFAULT_GOAL := help

.PHONY: help setup fmt test fixtures login interactive run-scenario run-suite print-config doctor clean

help:
	@printf "Available commands:\\n"
	@printf "  make setup          # go mod tidy\\n"
	@printf "  make test           # go test ./...\\n"
	@printf "  make doctor         # show effective config and session file status\\n"
	@printf "  make print-config   # print a compact config summary\\n"
	@printf "  make login          # create an MTProto session\\n"
	@printf "  make interactive    # JSONL interactive mode\\n"
	@printf "  make run-scenario   # run one JSONL scenario (SCENARIO=...)\\n"
	@printf "  make fixtures       # generate local media fixtures\\n"
	@printf "  make run-suite      # run the full v1 suite\\n"
	@printf "  make clean          # remove local transcripts and fixtures\\n"

setup:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

fixtures:
	./scripts/generate-fixtures.sh

login:
	$(ENV_RUN) go run ./cmd/tg-e2e-tool login

doctor:
	$(ENV_RUN) go run ./cmd/tg-e2e-tool doctor

interactive:
	$(ENV_RUN) go run ./cmd/tg-e2e-tool interactive

run-scenario:
	$(ENV_RUN) go run ./cmd/tg-e2e-tool run-scenario $(SCENARIO)

run-suite: fixtures
	$(ENV_RUN) ./scripts/run-suite.sh

print-config:
	$(ENV_RUN) go run ./cmd/tg-e2e-tool print-config

clean:
	rm -rf artifacts/transcripts artifacts/fixtures

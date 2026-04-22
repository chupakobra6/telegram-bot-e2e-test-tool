CLI := go run ./cmd/tg-e2e-tool
SCENARIO ?= examples/shelfy-smoke.jsonl
RATE_SWEEP_ARGS :=
RUN_BLOCK_ARGS :=
TEXT_MATRIX_ARGS :=
ifneq ($(strip $(CHAT)),)
RATE_SWEEP_ARGS += --chat $(CHAT)
endif
ifneq ($(strip $(RUNS)),)
RATE_SWEEP_ARGS += --runs $(RUNS)
endif
ifneq ($(strip $(ARTIFACT_ROOT)),)
RATE_SWEEP_ARGS += --artifact-root $(ARTIFACT_ROOT)
endif
ifneq ($(strip $(MIN_ACTION_MS)),)
RATE_SWEEP_ARGS += --min-action-ms $(MIN_ACTION_MS)
endif
ifneq ($(strip $(MAX_ACTION_MS)),)
RATE_SWEEP_ARGS += --max-action-ms $(MAX_ACTION_MS)
endif
ifneq ($(strip $(RESOLUTION_MS)),)
RATE_SWEEP_ARGS += --resolution-ms $(RESOLUTION_MS)
endif
ifneq ($(strip $(PREPARE_SCENARIO)),)
RATE_SWEEP_ARGS += $(foreach p,$(PREPARE_SCENARIO),--prepare-scenario $(p))
endif

.DEFAULT_GOAL := help

.PHONY: help setup fmt test fixtures login interactive run-scenario run-block run-suite run-text-matrix rate-sweep print-config doctor clean

help:
	@printf "Available commands:\\n"
	@printf "  make setup          # go mod tidy\\n"
	@printf "  make test           # go test ./...\\n"
	@printf "  make doctor         # show effective config and session file status (.env auto-loaded)\\n"
	@printf "  make print-config   # print a compact config summary\\n"
	@printf "  make login          # create an MTProto session\\n"
	@printf "  make interactive    # JSONL interactive mode\\n"
	@printf "  make run-scenario   # run one or more JSONL scenarios (SCENARIO=..., CHAT=...)\\n"
	@printf "  make run-block      # run a stateful block with optional reset/template rendering (SCENARIO=..., CHAT=..., RUN_BLOCK_ARGS=...)\\n"
	@printf "  make run-text-matrix # run a text-case matrix from CASES=... (CHAT=...)\\n"
	@printf "  make fixtures       # generate local media fixtures\\n"
	@printf "  make run-suite      # run the full v1 suite (CHAT=...)\\n"
	@printf "  make rate-sweep     # binary-search safe action spacing (CHAT=..., PREPARE_SCENARIO=...)\\n"
	@printf "  make clean          # remove local transcripts and fixtures\\n"

setup:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

fixtures:
	$(CLI) fixtures

login:
	$(CLI) login

doctor:
	$(CLI) doctor

interactive:
	$(CLI) interactive

run-scenario:
	CHAT="$(CHAT)" $(CLI) run-scenario $(SCENARIO)

run-block:
	CHAT="$(CHAT)" CONTROL_URL="$(CONTROL_URL)" SHELFY_DEV_CONTROL_URL="$(SHELFY_DEV_CONTROL_URL)" RUN_PREFIX="$(RUN_PREFIX)" $(CLI) run-block $(RUN_BLOCK_ARGS) $(SCENARIO)

run-text-matrix:
	CHAT="$(CHAT)" $(CLI) run-text-matrix --cases "$(CASES)" $(if $(strip $(CANCEL_BUTTON_TEXT)),--cancel-button "$(CANCEL_BUTTON_TEXT)",) --wait-timeout-ms "$(or $(WAIT_TIMEOUT_MS),12000)" $(TEXT_MATRIX_ARGS)

run-suite: fixtures
	CHAT="$(CHAT)" $(CLI) run-suite

rate-sweep: fixtures
	$(CLI) rate-sweep $(RATE_SWEEP_ARGS)

print-config:
	$(CLI) print-config

clean:
	rm -rf artifacts/transcripts artifacts/fixtures

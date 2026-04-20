.PHONY: setup fmt test login interactive run-scenario print-config

setup:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

login:
	go run ./cmd/tg-e2e-tool login

interactive:
	go run ./cmd/tg-e2e-tool interactive

run-scenario:
	go run ./cmd/tg-e2e-tool run-scenario examples/shelfy-smoke.jsonl

print-config:
	go run ./cmd/tg-e2e-tool print-config

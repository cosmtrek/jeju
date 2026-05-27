PROVIDER ?= mock

.PHONY: build test vet test-agent benchmark-terminal-lite benchmark-bfcl-lite

build:
	mkdir -p .jeju-dev/bin
	go build -o .jeju-dev/bin/jeju ./cmd/jeju

test:
	go test ./...

vet:
	go vet ./...

test-agent:
	./scripts/run-agent.sh $(PROVIDER)

benchmark-terminal-lite:
	./scripts/run-terminal-lite-benchmark.sh

benchmark-bfcl-lite:
	./scripts/run-bfcl-lite-benchmark.sh

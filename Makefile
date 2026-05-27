PROVIDER ?= mock

.PHONY: test vet test-agent benchmark-terminal-lite benchmark-bfcl-lite

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

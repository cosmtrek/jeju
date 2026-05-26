.PHONY: test vet test-agent test-agent-deepseek benchmark-terminal-lite benchmark-bfcl-lite

test:
	go test ./...

vet:
	go vet ./...

test-agent:
	./scripts/run-basic-agent.sh

test-agent-deepseek:
	./scripts/run-deepseek-agent.sh

benchmark-terminal-lite:
	./scripts/run-terminal-lite-benchmark.sh

benchmark-bfcl-lite:
	./scripts/run-bfcl-lite-benchmark.sh

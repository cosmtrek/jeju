.PHONY: test vet test-agent test-agent-deepseek

test:
	go test ./...

vet:
	go vet ./...

test-agent:
	./scripts/run-basic-agent.sh

test-agent-deepseek:
	./scripts/run-deepseek-agent.sh

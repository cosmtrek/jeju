PROVIDER ?= mock
VERSION ?= dev
COMMIT := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo none)
BRANCH := $(shell git branch --show-current 2>/dev/null || echo unknown)
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/cosmtrek/jeju/internal/cli.version=$(VERSION) -X github.com/cosmtrek/jeju/internal/cli.commit=$(COMMIT) -X github.com/cosmtrek/jeju/internal/cli.branch=$(BRANCH) -X github.com/cosmtrek/jeju/internal/cli.date=$(DATE)

.PHONY: build test vet test-agent test-long-horizon-agent test-evolve-e2e test-evolve-effect-e2e build-deep-research-agent benchmark-terminal-lite benchmark-bfcl-lite

build:
	mkdir -p .jeju-dev/bin
	go build -ldflags '$(LDFLAGS)' -o .jeju-dev/bin/jeju ./cmd/jeju

test:
	go test ./...

vet:
	go vet ./...

test-agent:
	./scripts/run-agent.sh $(PROVIDER)

test-long-horizon-agent:
	./scripts/run-long-horizon-agent.sh $(if $(filter mock,$(PROVIDER)),mimo,$(PROVIDER))

test-evolve-e2e:
	./scripts/run-evolve-e2e.sh $(PROVIDER)

test-evolve-effect-e2e:
	./scripts/run-evolve-effect-e2e.sh $(PROVIDER)

build-deep-research-agent:
	rm -rf .jeju-dev/deep-research-agent
	mkdir -p .jeju-dev/deep-research-agent
	cp -R tests/fixtures/deep-research/. .jeju-dev/deep-research-agent/

benchmark-terminal-lite:
	./scripts/run-terminal-lite-benchmark.sh

benchmark-bfcl-lite:
	./scripts/run-bfcl-lite-benchmark.sh

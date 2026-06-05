PROVIDER ?= mock
VERSION ?= dev
GO ?= go
CMD ?= ./cmd/jeju
BIN_DIR ?= .jeju-dev/bin
BIN ?= $(BIN_DIR)/jeju
COMMIT := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo none)
BRANCH := $(shell git branch --show-current 2>/dev/null || echo unknown)
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/cosmtrek/jeju/internal/cli.version=$(VERSION) -X github.com/cosmtrek/jeju/internal/cli.commit=$(COMMIT) -X github.com/cosmtrek/jeju/internal/cli.branch=$(BRANCH) -X github.com/cosmtrek/jeju/internal/cli.date=$(DATE)

.PHONY: build install test vet test-agent test-long-horizon-agent test-evolve-e2e test-evolve-effect-e2e build-deep-research-agent benchmark-terminal-lite benchmark-bfcl-lite

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) $(CMD)

install:
	$(GO) install -ldflags '$(LDFLAGS)' $(CMD)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

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

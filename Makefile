.PHONY: test lint build fmt fmt-check test-verbose test-cover test-e2e-agents test-e2e-headless-agents test-e2e-interactive-agents

GO_FILES := $(shell git ls-files --cached --others --exclude-standard '*.go' | while IFS= read -r file; do test ! -f "$$file" || printf '%s ' "$$file"; done)

build:
	go build -o bin/agent-runner ./cmd/agent-runner

test:
	go test ./...

test-verbose:
	go test -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-e2e-agents: test-e2e-headless-agents test-e2e-interactive-agents

# Set E2E_AGENTS=claude,codex to run an explicit subset while developing.
# With no selection, every supported executable and authentication is required.
test-e2e-headless-agents:
	./.validator/go-offline.sh go test -count=1 -timeout 30m -tags e2e_agents ./cmd/agent-runner -run 'HeadlessRealAgentE2E$$' -v

test-e2e-interactive-agents:
	./.validator/go-offline.sh go test -count=1 -timeout 30m -tags e2e_agents ./cmd/agent-runner -run 'InteractiveRealAgentE2E$$' -v

lint:
	golangci-lint run ./...

fmt:
	goimports -w $(GO_FILES)

fmt-check:
	test -z "$$(goimports -l $(GO_FILES))"

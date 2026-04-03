.PHONY: test lint build fmt test-verbose test-cover dev-agent-runner dev-run dev-validate dev-resume

build:
	go build -o bin/agent-runner ./cmd/agent-runner

test:
	go test ./...

test-verbose:
	go test -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run ./...

fmt:
	goimports -w .

# Development commands (go run equivalents)
dev-agent-runner:
	go run ./cmd/agent-runner $(filter-out $@,$(MAKECMDGOALS))

dev-run:
	go run ./cmd/agent-runner $(filter-out $@,$(MAKECMDGOALS))

dev-validate:
	go run ./cmd/agent-runner -validate $(filter-out $@,$(MAKECMDGOALS))

dev-resume:
	go run ./cmd/agent-runner -resume $(if $(SESSION),-session $(SESSION),) $(filter-out $@,$(MAKECMDGOALS))

dev-plan:
	go run ./cmd/agent-runner workflows/plan-change.yaml $(filter-out $@,$(MAKECMDGOALS))

dev-implement:
	go run ./cmd/agent-runner workflows/implement-change.yaml $(filter-out $@,$(MAKECMDGOALS))

# Allow arbitrary args to be passed to dev-* targets
%:
	@:

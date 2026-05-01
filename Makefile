.PHONY: test lint build fmt test-verbose test-cover test-e2e-smoke

build:
	go build -o bin/agent-runner ./cmd/agent-runner

test:
	go test ./...

test-verbose:
	go test -v ./...

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

test-e2e-smoke:
	GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOCACHE="$(PWD)/.validator/cache/go-build" GOPATH="$(PWD)/.validator/cache/go" GOMODCACHE="$(PWD)/.validator/cache/go/pkg/mod" go test -count=1 -tags e2e ./cmd/agent-runner -run TestSmokeTestHeadlessWorkflowE2E -v

lint:
	golangci-lint run ./...

fmt:
	goimports -w .

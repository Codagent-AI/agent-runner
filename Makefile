.PHONY: test lint build fmt test-verbose test-cover

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

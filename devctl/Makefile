BIN := bin/devctl

build:
	go build -o $(BIN) ./cmd/devctl

test:
	go test -race ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/devctl

.PHONY: build test lint fmt clean install

BIN := bin/accounts
DAEMON := bin/accountsd

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/accounts
	CGO_ENABLED=0 go build -o $(DAEMON) ./cmd/accountsd

test:
	go test -race ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/accounts ./cmd/accountsd

.PHONY: build test lint fmt clean install

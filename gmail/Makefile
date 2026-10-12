BIN := bin/gmail

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/gmail

test:
	go test -race ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/gmail

.PHONY: build test lint fmt clean install

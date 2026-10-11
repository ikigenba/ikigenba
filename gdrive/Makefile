BIN := bin/gdrive

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/gdrive

test:
	go test -race ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/gdrive

.PHONY: build test lint fmt clean install

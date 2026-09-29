BIN := bin/dummy

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/dummy

test:
	go test -race ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/dummy

live:
	go test -tags live -count=1 -run '^TestLive' ./...

.PHONY: build test lint fmt clean install live

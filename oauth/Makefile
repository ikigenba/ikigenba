BIN := bin/oauth

build:
	go build -o $(BIN) ./cmd/oauth

test:
	go test -race ./...

vet:
	GOOS=darwin go vet ./...
	GOOS=windows go vet ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run --allow-parallel-runners

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/oauth

live:
	go test -tags live -count=1 -run '^TestLive' ./...

.PHONY: build test vet lint fmt clean install live

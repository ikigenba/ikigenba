BIN := bin/oauth

build:
	go build -o $(BIN) ./cmd/oauth

test:
	go test -race ./...

vet:
	GOOS=darwin go vet ./...
	GOOS=windows go vet ./...

lint:
	GOLANGCI_LINT_CACHE="$$(git rev-parse --absolute-git-dir)/golangci-lint" golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/oauth

.PHONY: build test vet lint fmt clean install

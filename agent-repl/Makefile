BIN := bin/agent-repl

build:
	go build -o $(BIN) ./cmd/agent-repl

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/agent-repl

.PHONY: build test lint fmt clean install

BIN := bin/agent-repl

build:
	go build -o $(BIN) ./cmd/agent-repl

test:
	go test -race ./...

lint:
	golangci-lint run

llm-lint:
	llm-lint cmd internal

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/agent-repl

.PHONY: build test lint llm-lint fmt clean install

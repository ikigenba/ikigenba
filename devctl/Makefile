BIN := bin/devctl

build:
	go build -o $(BIN) ./cmd/devctl

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
	go install ./cmd/devctl

.PHONY: build test lint llm-lint fmt clean install

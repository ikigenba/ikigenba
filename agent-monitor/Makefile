BIN := bin/agent-monitor

build:
	go build -o $(BIN) ./cmd/agent-monitor

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/agent-monitor

.PHONY: build test lint fmt clean install

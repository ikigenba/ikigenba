BIN := bin/opsctl

build:
	go build -o $(BIN) ./cmd/opsctl

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
	go install ./cmd/opsctl

.PHONY: build test lint llm-lint fmt clean install

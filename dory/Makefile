BIN := bin/dory

build:
	go build -o $(BIN) ./cmd/dory

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
	go install ./cmd/dory

.PHONY: build test lint llm-lint fmt clean install

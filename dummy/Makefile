BIN := bin/dummy

build:
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/dummy

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/dummy

.PHONY: build test lint fmt clean install

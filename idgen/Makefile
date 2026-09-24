BIN := bin/idgen

build:
	go build -o $(BIN) ./cmd/idgen

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/idgen

.PHONY: build test lint fmt clean install

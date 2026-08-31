BIN := bin/oauth

build:
	go build -o $(BIN) ./cmd/oauth

test:
	go test -race ./...

vet:
	GOOS=darwin go vet ./...
	GOOS=windows go vet ./...

lint:
	golangci-lint run

llm-lint:
	llm-lint cmd internal

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/oauth

.PHONY: build test vet lint llm-lint fmt clean install

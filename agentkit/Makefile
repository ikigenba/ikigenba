build:
	go build ./...

test:
	go test -race ./...

lint:
	golangci-lint run

llm-lint:
	llm-lint .

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./...

.PHONY: build test lint llm-lint fmt clean install

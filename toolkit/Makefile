build:
	go build ./...

test:
	go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

install:
	go install ./...

.PHONY: build test lint fmt clean install

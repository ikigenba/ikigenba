BIN     := bin/dashboard

# version currently lives in package main; moves to internal/version later.

.PHONY: build run test clean

build:
	@mkdir -p $(dir $(BIN))
	go build -o $(BIN) ./cmd/dashboard

run: build
	./$(BIN) serve

test:
	go test ./...

clean:
	rm -f $(BIN)

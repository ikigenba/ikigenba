# Makefile — local development. Self-contained: builds and runs ledger directly,
# foreground (logs to the terminal, Ctrl-C to stop). The bin/ scripts are the
# production deploy spine (setup/deploy/start/stop on the box) and are not used
# for local running.
APP     := ledger
BIN     := build/$(APP).bin

.PHONY: build run test vet fmt clean

build:
	@mkdir -p $(dir $(BIN))
	go build -trimpath -o $(BIN) ./cmd/ledger

run: build
	./$(BIN)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf build

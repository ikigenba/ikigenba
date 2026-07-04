# Makefile - local development. Self-contained: builds and runs github directly,
# foreground (logs to the terminal, Ctrl-C to stop). The bin/ scripts are the
# production deploy spine (setup/deploy/start/stop on the box) and are not used
# for local running.
APP     := github
BIN     := build/$(APP).bin
VERSION ?= $(shell cat VERSION)
LDFLAGS := -X appkit.version=$(VERSION)

.PHONY: build run test vet fmt clean

build:
	@mkdir -p $(dir $(BIN))
	GOWORK=off go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/github

run: build
	./$(BIN)

test:
	GOWORK=off go test ./...

vet:
	GOWORK=off go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf build

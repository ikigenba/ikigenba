BIN := bin/opsctl
DEPLOY_HOST ?= ikigenba.dev
DEPLOY_USER ?= ec2-user

build:
	go build -o $(BIN) ./cmd/opsctl

deploy:
	GOOS=linux GOARCH=amd64 go build -o $(BIN) ./cmd/opsctl
	scp $(BIN) $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/opsctl
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) sudo install -m 0755 /tmp/opsctl /usr/local/bin/opsctl
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) rm -f /tmp/opsctl
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) opsctl version

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

.PHONY: build deploy test lint llm-lint fmt clean install

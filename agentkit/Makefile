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

live:
	AGENTKIT_OPENAI_OAUTH_FILE=$(HOME)/.agentkit/openai-auth.json \
	AGENTKIT_XAI_OAUTH_FILE=$(HOME)/.agentkit/x-ai-auth.json \
	go test -tags live -count=1 -run '^TestLive' ./...

.PHONY: build test lint llm-lint fmt clean install live

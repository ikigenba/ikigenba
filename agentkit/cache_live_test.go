//go:build live

package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const liveCachePrefix = "Remember this cache proof datum exactly: "

func newLiveCacheConversation(t *testing.T, cell liveMatrixCell) (*Conversation, *bytes.Buffer) {
	t.Helper()
	credential := requireLiveMatrixCredential(t, cell)
	offering, err := Lookup(cell.model, cell.host, cell.wire)
	if err != nil {
		t.Fatalf("look up %s on %s: %v", cell.model, cell.host, err)
	}
	auth, err := offering.Authenticator(APIKeyRotator(credential))
	if err != nil {
		t.Fatalf("build authenticator: %v", err)
	}
	endpoint, err := NewEndpoint(auth)
	if err != nil {
		t.Fatalf("build endpoint: %v", err)
	}
	var log bytes.Buffer
	conversation, err := New(offering.WireFormat, endpoint, cell.model, Config{Log: NewLog(&log, time.Now, "")})
	if err != nil {
		t.Fatalf("build conversation: %v", err)
	}
	return conversation, &log
}

func drainLiveCacheStream(stream *Stream) {
	for range stream.Events() {
	}
}

func assertLiveCachedTokens(t *testing.T, log *bytes.Buffer) {
	t.Helper()
	for _, line := range bytes.Split(log.Bytes(), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record LogRecord
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode cache log record: %v", err)
		}
		if record.Type == RecordUsage && record.Usage != nil && record.Usage.CachedTokens > 0 {
			return
		}
	}
	t.Fatal("second round-trip had no usage record with positive cached tokens")
}

func TestLiveCache(t *testing.T) {
	prefix := strings.Repeat(liveCachePrefix, 10_000)

	t.Run("anthropic-messages", func(t *testing.T) {
		conversation, log := newLiveCacheConversation(t, liveMatrixCell{
			offering: OfferingAnthropicMessages,
			authMode: AuthModeAPIKey,
			host:     HostAnthropic,
			wire:     WireMessages,
			model:    "claude-haiku-4-5",
		})
		if err := conversation.AddSystem(prefix); err != nil {
			t.Fatalf("add cache prefix as system content: %v", err)
		}
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("take savepoint: %v", err)
		}
		stream := conversation.Send(context.Background(), Text{Text: "Reply with exactly: first"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("first suffix: %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("restore savepoint: %v", err)
		}
		log.Reset()
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: second"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("second suffix: %v", err)
		}
		assertLiveCachedTokens(t, log)
	})

	t.Run("openai-responses", func(t *testing.T) {
		conversation, log := newLiveCacheConversation(t, liveMatrixCell{
			offering: OfferingOpenAIResponses,
			authMode: AuthModeAPIKey,
			host:     HostOpenAI,
			wire:     WireResponses,
			model:    "gpt-5.4-nano",
		})
		stream := conversation.Send(context.Background(), Text{Text: prefix + "\nReply with exactly: prefix ready"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("build cache prefix: %v", err)
		}
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("take savepoint: %v", err)
		}
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: first"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("first suffix: %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("restore savepoint: %v", err)
		}
		log.Reset()
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: second"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("second suffix: %v", err)
		}
		assertLiveCachedTokens(t, log)
	})

	t.Run("openai-chat", func(t *testing.T) {
		conversation, log := newLiveCacheConversation(t, liveMatrixCell{
			offering: OfferingOpenAIChat,
			authMode: AuthModeAPIKey,
			host:     HostOpenAI,
			wire:     WireChat,
			model:    "gpt-5.4-nano",
		})
		stream := conversation.Send(context.Background(), Text{Text: prefix + "\nReply with exactly: prefix ready"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("build cache prefix: %v", err)
		}
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("take savepoint: %v", err)
		}
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: first"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("first suffix: %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("restore savepoint: %v", err)
		}
		log.Reset()
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: second"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("second suffix: %v", err)
		}
		assertLiveCachedTokens(t, log)
	})

	t.Run("gemini-generate-content", func(t *testing.T) {
		conversation, log := newLiveCacheConversation(t, liveMatrixCell{
			offering: OfferingGeminiGenerateContent,
			authMode: AuthModeAPIKey,
			host:     HostGemini,
			wire:     WireGenerateContent,
			model:    "gemini-3.1-flash-lite",
		})
		stream := conversation.Send(context.Background(), Text{Text: prefix + "\nReply with exactly: prefix ready"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("build cache prefix: %v", err)
		}
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("take savepoint: %v", err)
		}
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: first"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("first suffix: %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("restore savepoint: %v", err)
		}
		log.Reset()
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: second"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("second suffix: %v", err)
		}
		assertLiveCachedTokens(t, log)
	})

	t.Run("xai-responses", func(t *testing.T) {
		conversation, _ := newLiveCacheConversation(t, liveMatrixCell{
			offering: OfferingXAIResponses,
			authMode: AuthModeAPIKey,
			host:     HostXAI,
			wire:     WireResponses,
			model:    "grok-4.3",
		})
		stream := conversation.Send(context.Background(), Text{Text: prefix + "\nReply with exactly: prefix ready"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("build cache prefix: %v", err)
		}
		sp, err := conversation.Savepoint()
		if err != nil {
			t.Fatalf("take savepoint: %v", err)
		}
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: first"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("first suffix: %v", err)
		}
		if err := conversation.Restore(sp); err != nil {
			t.Fatalf("restore savepoint: %v", err)
		}
		stream = conversation.Send(context.Background(), Text{Text: "Reply with exactly: second"})
		drainLiveCacheStream(stream)
		if err := stream.Err(); err != nil {
			t.Fatalf("second suffix: %v", err)
		}
	})
}

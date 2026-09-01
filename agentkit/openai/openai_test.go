package openai

import (
	"context"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/agentkit"
)

func TestSubscriptionAndBaseURLConflictAtConstruction(t *testing.T) {
	// R-3N6R-J6N6
	calls := 0
	source := tokenSourceFunc(func(context.Context) (string, string, error) {
		calls++
		return "token", "account", nil
	})
	conversation, err := New(Subscription(source), WithBaseURL("https://example.test/responses"))
	if conversation != nil || !errors.Is(err, agentkit.ErrInvalidConfig) {
		t.Fatalf("New = (%v, %v), want nil ErrInvalidConfig", conversation, err)
	}
	if calls != 0 {
		t.Fatalf("token source called %d times during rejected construction", calls)
	}
}

func TestAPIKeyAllowsBaseURL(t *testing.T) {
	conversation, err := New(APIKey("key"), WithBaseURL("https://example.test/responses"))
	if err != nil || conversation == nil {
		t.Fatalf("New = (%v, %v)", conversation, err)
	}
}

package admit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAcquireCallSerializesSameProvider(t *testing.T) {
	// R-67FV-VQ6I
	gate := New(1, 1)
	releaseFirst, err := gate.AcquireCall(context.Background(), "openai")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	acquired := make(chan func(), 1)
	go func() {
		release, err := gate.AcquireCall(context.Background(), "openai")
		if err == nil {
			acquired <- release
		}
	}()

	select {
	case release := <-acquired:
		release()
		t.Fatal("second acquire returned while first still held the only slot")
	case <-time.After(25 * time.Millisecond):
	}
	releaseFirst()

	select {
	case release := <-acquired:
		release()
	case <-time.After(time.Second):
		t.Fatal("second acquire did not return after first slot was released")
	}
}

func TestAcquireCallDoesNotContendAcrossProviders(t *testing.T) {
	// R-68NS-9HX7
	gate := New(1, 1)
	releaseOpenAI, err := gate.AcquireCall(context.Background(), "openai")
	if err != nil {
		t.Fatalf("acquire openai: %v", err)
	}
	defer releaseOpenAI()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	releaseGoogle, err := gate.AcquireCall(ctx, "google")
	if err != nil {
		t.Fatalf("google should have an independent slot: %v", err)
	}
	releaseGoogle()
}

func TestAcquireCallReturnsContextErrorWhileBlocked(t *testing.T) {
	// R-6CBH-ET5A
	gate := New(1, 1)
	release, err := gate.AcquireCall(context.Background(), "openai")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blockedRelease, err := gate.AcquireCall(ctx, "openai")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("blocked acquire error = %v, want context.Canceled", err)
	}
	if blockedRelease != nil {
		t.Fatal("canceled acquire returned a release function")
	}

	release()
	releaseAfterCancel, err := gate.AcquireCall(context.Background(), "openai")
	if err != nil {
		t.Fatalf("canceled acquire retained a slot: %v", err)
	}
	releaseAfterCancel()
}

func TestAcquireRunUsesIndependentGlobalPool(t *testing.T) {
	gate := New(1, 1)
	releaseCall, err := gate.AcquireCall(context.Background(), "openai")
	if err != nil {
		t.Fatalf("acquire call: %v", err)
	}
	defer releaseCall()

	releaseRun, err := gate.AcquireRun(context.Background())
	if err != nil {
		t.Fatalf("run should not contend with call pool: %v", err)
	}
	releaseRun()
}

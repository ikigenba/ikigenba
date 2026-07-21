package eval

import (
	"context"
	"testing"
)

func TestDiskCacheAvoidsCallsAndKeysByModelAndText(t *testing.T) {
	// R-KVRV-LO1X
	dir := t.TempDir()
	calls := 0
	next := func(_ context.Context, texts []string) ([][]float32, error) {
		calls++
		vectors := make([][]float32, len(texts))
		for i := range texts {
			vectors[i] = []float32{float32(len(texts[i])), 1}
		}
		return vectors, nil
	}
	cache := NewDiskCache(dir, "model-a", next)
	if _, err := cache(context.Background(), []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("first run calls = %d", calls)
	}
	if _, err := cache(context.Background(), []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cached run made provider call; total = %d", calls)
	}
	if _, err := cache(context.Background(), []string{"changed"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("changed text did not miss; calls = %d", calls)
	}
	otherModel := NewDiskCache(dir, "model-b", next)
	if _, err := otherModel(context.Background(), []string{"alpha"}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("changed model did not miss; calls = %d", calls)
	}
}

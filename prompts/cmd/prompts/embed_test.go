package main

import (
	"os"
	"strings"
	"testing"
)

func TestEmbedIsMountedAsLoopbackHandler(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(source), `rt.HandleLoopback("POST /embed", embedding.EmbedHandler())`) {
		t.Fatal("registerRoutes does not mount the embedding handler through HandleLoopback")
	}
}

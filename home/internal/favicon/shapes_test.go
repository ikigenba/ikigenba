package favicon

import (
	"net/http"
	"testing"
)

// R-FLT8-W0VT
func TestPositionalConfig(t *testing.T) {
	client := &http.Client{}
	cfg := Config{client, "agent"}
	if cfg.HTTP != client || cfg.UserAgent != "agent" {
		t.Fatal("positional config mismatch")
	}
}

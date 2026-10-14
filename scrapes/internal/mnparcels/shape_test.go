package mnparcels

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-RQ28-62T8
func TestOrderedConfig(t *testing.T) {
	client := &http.Client{}
	writer := &telemetry.Writer{}
	now := func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }
	var slept time.Duration
	sleep := func(_ context.Context, d time.Duration) { slept = d }
	cfg := Config{client, "base", writer, now, sleep, "ua", []string{"Carlton"}, time.Minute}
	if cfg.HTTP != client || cfg.BaseURL != "base" || cfg.Telemetry != writer || cfg.Now() != now() || cfg.UserAgent != "ua" || cfg.Counties[0] != "Carlton" || cfg.Timeout != time.Minute {
		t.Fatal(cfg)
	}
	cfg.Sleep(context.Background(), time.Second)
	if slept != time.Second {
		t.Fatal(slept)
	}
}

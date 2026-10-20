package ppp

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-PWM1-96K6
func TestOrderedConfigFields(t *testing.T) {
	client := &http.Client{}
	writer := &telemetry.Writer{}
	now := func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }
	var slept time.Duration
	sleep := func(_ context.Context, d time.Duration) { slept = d }
	cfg := Config{client, "base", writer, now, sleep, "ua", []string{"file"}, []string{"MN"}, time.Minute, time.Hour}
	if cfg.HTTP != client || cfg.BaseURL != "base" || cfg.Telemetry != writer || cfg.Now() != now() || cfg.UserAgent != "ua" || cfg.Files[0] != "file" || cfg.States[0] != "MN" || cfg.HeaderTimeout != time.Minute || cfg.BodyTimeout != time.Hour {
		t.Fatal(cfg)
	}
	cfg.Sleep(context.Background(), time.Second)
	if slept != time.Second {
		t.Fatal(slept)
	}
}

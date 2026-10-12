package scrapers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
)

// R-1I7E-01UH
func TestOrderedConfigFields(t *testing.T) {
	client := &http.Client{}
	writer := &telemetry.Writer{}
	now := func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
	var slept time.Duration
	sleep := func(_ context.Context, d time.Duration) { slept = d }
	s := settings.Settings{FMCSAStatus: "I"}
	cfg := Config{client, writer, now, sleep, "ua", s}
	if cfg.HTTP != client || cfg.Telemetry != writer || cfg.Now() != now() || cfg.UserAgent != "ua" || cfg.Settings.FMCSAStatus != "I" {
		t.Fatal(cfg)
	}
	cfg.Sleep(context.Background(), time.Second)
	if slept != time.Second {
		t.Fatal(slept)
	}
}

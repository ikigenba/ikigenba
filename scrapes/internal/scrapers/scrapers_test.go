package scrapers_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/fmcsa"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

type fake struct{ name string }

func (f *fake) Name() string                                           { return f.name }
func (f *fake) Scrape(context.Context, func([]source.Row) error) error { return nil }

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// R-1I7E-01UH R-1JFA-DTL6
func TestRegistry(t *testing.T) {
	a, b, duplicate := &fake{"z"}, &fake{"a"}, &fake{"z"}
	r := scrapers.New(a, b, duplicate)
	if !reflect.DeepEqual(r.Names(), []string{"a", "z"}) {
		t.Fatal(r.Names())
	}
	names := r.Names()
	names[0] = "changed"
	if !reflect.DeepEqual(r.Names(), []string{"a", "z"}) {
		t.Fatal("names aliased")
	}
	for name, want := range map[string]source.Scraper{"z": a, "a": b} {
		got, ok := r.Get(name)
		if !ok || got != want {
			t.Fatal(name, got)
		}
	}
	if got, ok := r.Get("bogus"); ok || got != nil {
		t.Fatal(got)
	}
	empty := scrapers.New()
	if empty.Names() == nil || len(empty.Names()) != 0 {
		t.Fatal(empty.Names())
	}
}

// R-1KN6-RLBV
func TestDefault(t *testing.T) {
	attempts, batches, sleeps := 0, 0, 0
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	states := []string{"WI", "IA"}
	var capture telemetry.Capture
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer writer.Shutdown(context.Background(), "test")
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		attempts++
		if !strings.HasPrefix(r.URL.String(), fmcsa.DefaultBaseURL+"?") || r.Header.Get("User-Agent") != "registry ua" {
			t.Fatal(r)
		}
		state := "WI"
		if attempts >= 4 {
			state = "IA"
		}
		if r.URL.Query().Get("$where") != fmcsa.Where(state, "I", 7) {
			t.Fatal(r.URL)
		}
		status, body := 200, `[{"dot_number":"test"}]`
		if attempts == 1 {
			status = 503
			body = ""
		} else if r.URL.Query().Get("$select") != "" {
			body = `[{"count":"1"}]`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	r := scrapers.Default(scrapers.Config{HTTP: client, Telemetry: writer, Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) { sleeps++ }, UserAgent: "registry ua", Settings: settings.Settings{FMCSAStates: states, FMCSAMinPowerUnits: 7, FMCSAStatus: "I"}})
	if !reflect.DeepEqual(r.Names(), []string{fmcsa.Name}) {
		t.Fatal(r.Names())
	}
	sc, ok := r.Get(fmcsa.Name)
	if !ok {
		t.Fatal("missing scraper")
	}
	if err := sc.Scrape(context.Background(), func(rows []source.Row) error {
		batches++
		if len(rows) != 1 || !rows[0].Collected.Equal(now) {
			t.Fatal(rows)
		}
		return nil
	}); err != nil || attempts != 5 || batches != 2 || sleeps != 1 {
		t.Fatal(err, attempts, batches, sleeps)
	}
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != attempts {
		t.Fatal(events)
	}
	for i, event := range events {
		status := int64(200)
		if i == 0 {
			status = 503
		}
		if event.Name != source.Called || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": fmcsa.Name, "status": status, "duration_us": int64(0)}) {
			t.Fatal(event)
		}
	}
	if _, ok = r.Get("bogus"); ok {
		t.Fatal("unknown scraper")
	}
}

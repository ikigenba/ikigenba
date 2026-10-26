package scrapers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/oshaita"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// R-5AK1-9IX9
func TestDefaultOSHAITA(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var capture telemetry.Capture
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer writer.Shutdown(t.Context(), "test")
	var paths []string
	var pauses []time.Duration
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.String())
		if r.Header.Get("User-Agent") != oshaita.UserAgent {
			t.Fatal(r.Header)
		}
		var body bytes.Buffer
		w := csv.NewWriter(&body)
		columns := oshaita.Columns()
		if err := w.Write(columns); err != nil {
			t.Fatal(err)
		}
		for _, state := range []string{"MN", "IA"} {
			row := make([]string, len(columns))
			for i, col := range columns {
				switch col {
				case "id":
					row[i] = r.URL.Path + state
				case "state":
					row[i] = state
				}
			}
			if err := w.Write(row); err != nil {
				t.Fatal(err)
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			t.Fatal(err)
		}
		data := body.Bytes()
		if strings.HasSuffix(r.URL.Path, ".zip") {
			var zipped bytes.Buffer
			z := zip.NewWriter(&zipped)
			entry, err := z.Create("data.csv")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := entry.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			data = zipped.Bytes()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	reg := scrapers.Default(scrapers.Config{HTTP: client, Telemetry: writer, Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }, UserAgent: "registry independent ua", Settings: settings.Settings{OSHAITAFiles: []string{"files/a b.zip", "largefiles/c.csv"}, OSHAITAStates: []string{"IA"}}})
	sc, ok := reg.Get("osha_ita")
	if !ok {
		t.Fatal("missing scraper")
	}
	dir := t.TempDir()
	ctx := source.WithSpool(t.Context(), func() (*os.File, error) { return os.CreateTemp(dir, "summary.") })
	var rows []source.Row
	if err := sc.Scrape(ctx, func(batch []source.Row) error { rows = append(rows, batch...); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{oshaita.DefaultBaseURL + "files/a%20b.zip", oshaita.DefaultBaseURL + "largefiles/c.csv"}) || len(pauses) != 0 || len(rows) != 2 {
		t.Fatal(paths, pauses, rows)
	}
	for _, row := range rows {
		if row.Scraper != oshaita.Name || row.Address.State != "IA" || !row.Scraped.Equal(now) {
			t.Fatal(row)
		}
	}
	if err := writer.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 2 {
		t.Fatal(events)
	}
	for _, event := range events {
		if event.Name != source.Called || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": oshaita.Name, "status": int64(200), "duration_us": int64(0)}) {
			t.Fatal(event)
		}
	}
	// The runtime pause hook must reach the scraper's retries as well.
	attempts := 0
	retryClient := &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	retryRegistry := scrapers.Default(scrapers.Config{HTTP: retryClient, Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }, Settings: settings.Settings{OSHAITAFiles: []string{"files/retry.zip"}, OSHAITAStates: []string{"IA"}}})
	retryScraper, ok := retryRegistry.Get("osha_ita")
	if !ok {
		t.Fatal("missing scraper")
	}
	if err := retryScraper.Scrape(ctx, func([]source.Row) error { t.Error("unexpected batch"); return nil }); err == nil {
		t.Fatal("retry exhaustion succeeded")
	}
	if attempts != oshaita.Retries+1 || !reflect.DeepEqual(pauses, []time.Duration{oshaita.Backoff, 2 * oshaita.Backoff, 4 * oshaita.Backoff}) {
		t.Fatal(attempts, pauses)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

package scrapers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/form5500"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// R-LY7I-TA2V
func TestDefaultForm5500(t *testing.T) {
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	var capture telemetry.Capture
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer writer.Shutdown(t.Context(), "test")
	var paths []string
	var pauses []time.Duration
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.String())
		if r.Header.Get("User-Agent") != "registry filings ua" {
			t.Fatal(r.Header)
		}
		if len(paths) == 1 {
			return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		form, state := form5500.Full, "SPONS_DFE_MAIL_US_STATE"
		if strings.Contains(r.URL.Path, "_SF_") {
			form, state = form5500.Short, "SF_SPONS_US_STATE"
		}
		var body bytes.Buffer
		z := zip.NewWriter(&body)
		entry, err := z.Create("filings.csv")
		if err != nil {
			t.Fatal(err)
		}
		w := csv.NewWriter(entry)
		columns := form5500.Columns(form)
		if err := w.Write(columns); err != nil {
			t.Fatal(err)
		}
		for _, st := range []string{"IA", "MN"} {
			record := make([]string, len(columns))
			for i, col := range columns {
				switch col {
				case "ACK_ID":
					record[i] = string(form) + st
				case state:
					record[i] = st
				}
			}
			if err := w.Write(record); err != nil {
				t.Fatal(err)
			}
		}
		w.Flush()
		if err := w.Error(); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body.Bytes()))}, nil
	})}
	reg := scrapers.Default(scrapers.Config{HTTP: client, Telemetry: writer, Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }, UserAgent: "registry filings ua", Settings: settings.Settings{Form5500Years: []string{"2024"}, Form5500States: []string{"IA"}}})
	sc, ok := reg.Get("form5500")
	if !ok {
		t.Fatal("missing scraper")
	}
	dir := t.TempDir()
	ctx := source.WithSpool(t.Context(), func() (*os.File, error) { return os.CreateTemp(dir, "filing.") })
	var rows []source.Row
	if err := sc.Scrape(ctx, func(batch []source.Row) error { rows = append(rows, batch...); return nil }); err != nil {
		t.Fatal(err)
	}
	full := form5500.DefaultBaseURL + "2024/Latest/F_5500_2024_Latest.zip"
	short := form5500.DefaultBaseURL + "2024/Latest/F_5500_SF_2024_Latest.zip"
	if !reflect.DeepEqual(paths, []string{full, full, short}) || !reflect.DeepEqual(pauses, []time.Duration{form5500.Backoff}) || len(rows) != 2 {
		t.Fatal(paths, pauses, rows)
	}
	for _, row := range rows {
		if row.Scraper != form5500.Name || row.Address.State != "IA" || !row.Scraped.Equal(now) {
			t.Fatal(row)
		}
	}
	if err := writer.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != 3 {
		t.Fatal(events)
	}
	for i, event := range events {
		status := int64(200)
		if i == 0 {
			status = 500
		}
		if event.Name != source.Called || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": form5500.Name, "status": status, "duration_us": int64(0)}) {
			t.Fatal(event)
		}
	}
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
}

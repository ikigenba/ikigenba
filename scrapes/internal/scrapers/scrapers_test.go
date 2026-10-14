package scrapers_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/fmcsa"
	"github.com/ikigenba/ikigenba/scrapes/internal/mnparcels"
	"github.com/ikigenba/ikigenba/scrapes/internal/ppp"
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

// R-SASI-O6F1 R-DBF2-OYH3
func TestDefaultFMCSA(t *testing.T) {
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
	if !reflect.DeepEqual(r.Names(), []string{fmcsa.Name, mnparcels.Name, ppp.Name}) {
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

// R-DCMZ-2Q7S
func TestDefaultPPP(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
			var capture telemetry.Capture
			writer := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer writer.Shutdown(context.Background(), "test")
			var paths []string
			var pauses []time.Duration
			client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.String())
				if r.Header.Get("User-Agent") != "registry ppp ua" {
					t.Fatal(r.Header)
				}
				if retry && len(paths) == 1 {
					return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				var body bytes.Buffer
				csvWriter := csv.NewWriter(&body)
				columns := ppp.Columns()
				if err := csvWriter.Write(columns); err != nil {
					t.Fatal(err)
				}
				for _, state := range []string{"IA", "MN"} {
					record := make([]string, len(columns))
					for i, column := range columns {
						switch column {
						case "LoanNumber":
							record[i] = state + r.URL.Path
						case "BorrowerState":
							record[i] = state
						}
					}
					if err := csvWriter.Write(record); err != nil {
						t.Fatal(err)
					}
				}
				csvWriter.Flush()
				if err := csvWriter.Error(); err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body.Bytes()))}, nil
			})}
			reg := scrapers.Default(scrapers.Config{HTTP: client, Telemetry: writer, Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }, UserAgent: "registry ppp ua", Settings: settings.Settings{PPPFiles: []string{"a.csv", "b.csv"}, PPPStates: []string{"IA"}}})
			sc, ok := reg.Get("ppp")
			if !ok {
				t.Fatal("missing scraper")
			}
			var rows []source.Row
			if err := sc.Scrape(t.Context(), func(batch []source.Row) error { rows = append(rows, batch...); return nil }); err != nil {
				t.Fatal(err)
			}
			wantPaths := []string{ppp.DefaultBaseURL + "a.csv", ppp.DefaultBaseURL + "b.csv"}
			wantPauses := []time.Duration(nil)
			if retry {
				wantPaths = append([]string{ppp.DefaultBaseURL + "a.csv"}, wantPaths...)
				wantPauses = []time.Duration{ppp.Backoff}
			}
			if !reflect.DeepEqual(paths, wantPaths) || !reflect.DeepEqual(pauses, wantPauses) || len(rows) != 2 {
				t.Fatal(paths, pauses, rows)
			}
			for i, row := range rows {
				if row.Scraper != "ppp" || row.Address.State != "IA" || !row.Collected.Equal(now) || !strings.HasSuffix(row.SourceID, []string{"a.csv", "b.csv"}[i]) {
					t.Fatal(row)
				}
			}
			if err := writer.Flush(t.Context()); err != nil {
				t.Fatal(err)
			}
			events := capture.Events()
			if len(events) != len(paths) {
				t.Fatal(events)
			}
			for i, event := range events {
				status := int64(200)
				if retry && i == 0 {
					status = 503
				}
				if event.Name != source.Called || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": "ppp", "status": status, "duration_us": int64(0)}) {
					t.Fatal(event)
				}
			}
		})
	}
}

// R-SD8B-FPWF
func TestDefaultMNParcels(t *testing.T) {
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	var capture telemetry.Capture
	writer := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer writer.Shutdown(t.Context(), "test")
	var requests []string
	var pauses []time.Duration
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.Query().Get("where"))
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.String(), mnparcels.DefaultBaseURL+"?") || r.Header.Get("User-Agent") != "registry parcels ua" {
			t.Fatal(r)
		}
		status, body := 200, `{"features":[{"attributes":{"state_pin":"parcel-fixture"}}]}`
		if len(requests) == 1 {
			status, body = 503, ""
		} else if r.URL.Query().Get("returnCountOnly") == "true" {
			body = `{"count":1}`
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	reg := scrapers.Default(scrapers.Config{HTTP: client, Telemetry: writer, Now: func() time.Time { return now }, Sleep: func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }, UserAgent: "registry parcels ua", Settings: settings.Settings{MNParcelsCounties: []string{"Chisago", "St. Louis"}}})
	sc, ok := reg.Get("mn_parcels")
	if !ok {
		t.Fatal("missing scraper")
	}
	var rows []source.Row
	if err := sc.Scrape(t.Context(), func(batch []source.Row) error { rows = append(rows, batch...); return nil }); err != nil {
		t.Fatal(err)
	}
	want := []string{mnparcels.Where("Chisago"), mnparcels.Where("Chisago"), mnparcels.Where("Chisago"), mnparcels.Where("St. Louis"), mnparcels.Where("St. Louis")}
	if !reflect.DeepEqual(requests, want) || !reflect.DeepEqual(pauses, []time.Duration{mnparcels.Backoff}) || len(rows) != 2 {
		t.Fatal(requests, pauses, rows)
	}
	for _, row := range rows {
		if row.Scraper != mnparcels.Name || !row.Collected.Equal(now) {
			t.Fatal(row)
		}
	}
	if err := writer.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) != len(requests) {
		t.Fatal(events)
	}
	for i, event := range events {
		status := int64(200)
		if i == 0 {
			status = 503
		}
		if event.Name != source.Called || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": mnparcels.Name, "status": status, "duration_us": int64(0)}) {
			t.Fatal(event)
		}
	}
}

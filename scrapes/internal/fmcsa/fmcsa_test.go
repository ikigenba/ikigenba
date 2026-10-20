package fmcsa_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/fmcsa"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func clock() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 789, time.FixedZone("east", 3600)) }
func config(f transport) fmcsa.Config {
	return fmcsa.Config{HTTP: &http.Client{Transport: f}, Now: clock, States: []string{"MN"}, Status: "A", MinPowerUnits: 5, UserAgent: "test ua", Sleep: func(context.Context, time.Duration) {}}
}

// R-0XH3-HY8O R-P24J-OX0T R-0ZWW-9HQ2 R-114S-N9GR
func TestAPI(t *testing.T) {
	if fmcsa.Name != "fmcsa" || fmcsa.DefaultBaseURL != "https://data.transportation.gov/resource/az4n-8mr2.json" || fmcsa.PageSize != 10000 || fmcsa.Retries != 3 || fmcsa.Backoff != time.Second || fmcsa.RequestTimeout != 60*time.Second || fmcsa.Role != "company officer" {
		t.Fatal("constants")
	}
	var sc source.Scraper = fmcsa.New(fmcsa.Config{})
	if sc.Name() != fmcsa.Name {
		t.Fatal(sc.Name())
	}
	if fmcsa.Where("MN", "A", 5) != "phy_state='MN' AND status_code='A' AND power_units::number >= 5" || fmcsa.Where("WI", "I", 0) != "phy_state='WI' AND status_code='I' AND power_units::number >= 0" {
		t.Fatal("where")
	}
}

// R-12CP-117G R-13KL-ESY5 R-14SH-SKOU R-160E-6CFJ
func TestWalk(t *testing.T) {
	raw := `{ "dot_number" : "17", "legal_name":" TEST LLC " }`
	var large strings.Builder
	large.WriteByte('[')
	for i := range fmcsa.PageSize {
		if i > 0 {
			large.WriteByte(',')
		}
		fmt.Fprintf(&large, `{"dot_number":"%d"}`, i)
	}
	large.WriteByte(']')
	type step struct {
		state, offset, body string
		count               bool
	}
	steps := []step{{"MN", "", `[{"count":"0"}]`, true}, {"MN", "0", "[" + raw + "," + raw + "]", false}, {"WI", "", `[{"count":"1"}]`, true}, {"WI", "0", large.String(), false}, {"WI", "10000", `[{"dot_number":"last"}]`, false}, {"IA", "", `[{"count":"0"}]`, true}, {"IA", "0", `[]`, false}}
	next, batches := 0, 0
	var received [][]source.Row
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next >= len(steps) {
			t.Error("extra request")
			w.WriteHeader(400)
			return
		}
		step := steps[next]
		if next == 2 && batches != 1 || next == 4 && batches != 2 || next == 5 && batches != 3 {
			t.Error("batch ordering")
		}
		next++
		want := url.Values{"$where": {fmcsa.Where(step.state, "A", 5)}}
		if step.count {
			want.Set("$select", "count(*)")
		} else {
			want.Set("$offset", step.offset)
			want.Set("$order", "dot_number")
			want.Set("$limit", "10000")
		}
		if !reflect.DeepEqual(r.URL.Query(), want) || r.Method != "GET" || r.Header.Get("User-Agent") != "test ua" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.ContentLength != 0 {
			t.Errorf("request: %v %v", r.URL, r.Header)
		}
		_, _ = io.WriteString(w, step.body)
	}))
	defer srv.Close()
	cfg := config(nil)
	cfg.HTTP = srv.Client()
	cfg.BaseURL = srv.URL
	cfg.States = []string{"MN", "WI", "IA"}
	err := fmcsa.New(cfg).Scrape(context.Background(), func(rows []source.Row) error { batches++; received = append(received, rows); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if next != len(steps) || batches != 3 || len(received[0]) != 2 || len(received[1]) != fmcsa.PageSize || len(received[2]) != 1 {
		t.Fatal(next, batches)
	}
	expected, err := fmcsa.RowOf(json.RawMessage(raw), clock())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(received[0][0], expected) || !reflect.DeepEqual(received[0][1], expected) {
		t.Fatal(received[0])
	}
	for i, row := range received[1] {
		if row.SourceID != fmt.Sprint(i) || !row.Scraped.Equal(clock().UTC().Truncate(time.Second)) {
			t.Fatal(i, row)
		}
	}
}

// R-6FZO-SHYG
func TestRetries(t *testing.T) {
	for _, kind := range []string{"network", "timeout", "429", "500", "599"} {
		t.Run(kind, func(t *testing.T) {
			attempts := 0
			var urls []string
			var sleeps []time.Duration
			cfg := config(func(r *http.Request) (*http.Response, error) {
				attempts++
				urls = append(urls, r.URL.String())
				switch kind {
				case "network":
					return nil, errors.New("offline")
				case "timeout":
					<-r.Context().Done()
					return nil, r.Context().Err()
				case "429":
					return response(429, ""), nil
				case "500":
					return response(500, ""), nil
				default:
					return response(599, ""), nil
				}
			})
			cfg.Timeout = time.Millisecond
			cfg.Sleep = func(_ context.Context, d time.Duration) { sleeps = append(sleeps, d) }
			calls := 0
			err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { calls++; return nil })
			if err == nil || attempts != 4 || calls != 0 || !reflect.DeepEqual(sleeps, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
				t.Fatal(err, attempts, calls, sleeps)
			}
			for _, u := range urls {
				if u != urls[0] {
					t.Fatal(urls)
				}
			}
		})
	}
	attempts := 0
	cfg := config(func(r *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(429, ""), nil
		}
		if r.URL.Query().Get("$select") != "" {
			return response(200, `[{"count":"0"}]`), nil
		}
		return response(200, `[]`), nil
	})
	sleeps := 0
	cfg.Sleep = func(context.Context, time.Duration) { sleeps++ }
	if err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { return nil }); err != nil || attempts != 3 || sleeps != 1 {
		t.Fatal(err, attempts, sleeps)
	}
}

type broken struct{}

func (broken) Read([]byte) (int, error) { return 0, errors.New("body failed") }
func (broken) Close() error             { return nil }

type blockedBody struct{ ctx context.Context }

func (b blockedBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (blockedBody) Close() error               { return nil }

// R-6H7L-69P5
func TestUnreadable(t *testing.T) {
	cases := []struct {
		body string
		page bool
	}{{`{"count":"0"}`, false}, {`[]`, false}, {`[{"count":0}]`, false}, {`[{"count":""}]`, false}, {`[{"count":"-1"}]`, false}, {`[{"count":"1x"}]`, false}, {`[{"count":"0"},{"count":"0"}]`, false}, {`null`, false}, {`{}`, true}, {`null`, true}, {`[5]`, true}, {`[{}]`, true}, {`[{"dot_number":""}]`, true}, {`[{"dot_number":1}]`, true}, {`[null]`, true}, {`[] extra`, true}}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			attempts, sleeps, batches := 0, 0, 0
			cfg := config(func(r *http.Request) (*http.Response, error) {
				attempts++
				if tc.page && r.URL.Query().Get("$select") != "" {
					return response(200, `[{"count":"0"}]`), nil
				}
				return response(200, tc.body), nil
			})
			cfg.Sleep = func(context.Context, time.Duration) { sleeps++ }
			err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { batches++; return nil })
			want := 1
			if tc.page {
				want = 2
			}
			if err == nil || attempts != want || sleeps != 0 || batches != 0 {
				t.Fatal(err, attempts, sleeps, batches)
			}
		})
	}
	for _, kind := range []string{"400", "301", "body", "bodytimeout"} {
		t.Run(kind, func(t *testing.T) {
			attempts, sleeps, batches := 0, 0, 0
			cfg := config(func(r *http.Request) (*http.Response, error) {
				attempts++
				switch kind {
				case "400":
					return response(400, ""), nil
				case "301":
					return response(301, ""), nil
				case "body":
					return &http.Response{StatusCode: 200, Body: broken{}}, nil
				default:
					return &http.Response{StatusCode: 200, Body: blockedBody{r.Context()}}, nil
				}
			})
			cfg.Timeout = time.Millisecond
			cfg.Sleep = func(context.Context, time.Duration) { sleeps++ }
			if err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { batches++; return nil }); err == nil || attempts != 1 || sleeps != 0 || batches != 0 {
				t.Fatal(err, attempts, sleeps, batches)
			}
		})
	}
}

// R-19O3-BNNM
func TestBatchRefusal(t *testing.T) {
	attempts, batches := 0, 0
	refusal := errors.New("store refused")
	cfg := config(func(r *http.Request) (*http.Response, error) {
		attempts++
		if r.URL.Query().Get("$select") != "" {
			return response(200, `[{"count":"1"}]`), nil
		}
		return response(200, `[{"dot_number":"17"}]`), nil
	})
	cfg.States = []string{"MN", "WI"}
	err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { batches++; return refusal })
	if !errors.Is(err, refusal) || attempts != 2 || batches != 1 {
		t.Fatal(err, attempts, batches)
	}
}

// R-1AVZ-PFEB
func TestCancellation(t *testing.T) {
	for _, phase := range []string{"headers", "body", "sleep", "beforebatch", "initial"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			ended := make(chan struct{})
			attempts, batches := 0, 0
			cfg := config(func(r *http.Request) (*http.Response, error) {
				attempts++
				switch phase {
				case "headers":
					close(entered)
					<-r.Context().Done()
					close(ended)
					return nil, r.Context().Err()
				case "body":
					close(entered)
					return &http.Response{StatusCode: 200, Body: blockedBody{r.Context()}}, nil
				case "sleep":
					return response(503, ""), nil
				case "beforebatch":
					if r.URL.Query().Get("$select") != "" {
						return response(200, `[{"count":"1"}]`), nil
					}
					cancel()
					return response(200, `[{"dot_number":"17"}]`), nil
				default:
					return nil, errors.New("unexpected")
				}
			})
			cfg.Sleep = func(sctx context.Context, _ time.Duration) { close(entered); <-sctx.Done() }
			if phase == "initial" {
				cancel()
			}
			done := make(chan error, 1)
			go func() { done <- fmcsa.New(cfg).Scrape(ctx, func([]source.Row) error { batches++; return nil }) }()
			if phase == "headers" || phase == "body" || phase == "sleep" {
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("did not enter")
				}
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || batches != 0 {
					t.Fatal(err, batches)
				}
			case <-time.After(time.Second):
				t.Fatal("cancel hung")
			}
			if phase == "headers" {
				select {
				case <-ended:
				default:
					t.Fatal("request in flight")
				}
			}
			want := 1
			if phase == "initial" {
				want = 0
			}
			if phase == "beforebatch" {
				want = 2
			}
			if attempts != want {
				t.Fatal(attempts)
			}
		})
	}
}

// R-6ERS-EQ7R
func TestTelemetry(t *testing.T) {
	for _, negative := range []bool{false, true} {
		t.Run(fmt.Sprint(negative), func(t *testing.T) {
			var capture telemetry.Capture
			now := clock()
			w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time {
				v := now
				if negative {
					now = now.Add(-1500 * time.Microsecond)
				} else {
					now = now.Add(1500 * time.Microsecond)
				}
				return v
			}, Rand: bytes.NewReader(make([]byte, 1024)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer w.Shutdown(context.Background(), "test")
			attempts := 0
			cfg := config(func(r *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return nil, errors.New("offline")
				}
				if attempts == 2 {
					return response(503, ""), nil
				}
				if r.URL.Query().Get("$select") != "" {
					return response(200, `[{"count":"1"}]`), nil
				}
				return response(200, `[{"dot_number":"17"}]`), nil
			})
			cfg.Telemetry = w
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "test-user", RequestID: "test-request"})
			if err := fmcsa.New(cfg).Scrape(ctx, func([]source.Row) error { return nil }); err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			events := capture.Events()
			if len(events) != 4 {
				t.Fatal(events)
			}
			for i, e := range events {
				duration := int64(1500)
				if negative {
					duration = 0
				}
				statuses := []int64{0, 503, 200, 200}
				if e.Name != source.Called || e.RequestID != "test-request" || e.User != "test-user" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"scraper": fmcsa.Name, "status": statuses[i], "duration_us": duration}) {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-P5S8-U88W R-1FRL-8ID3
func TestRowOf(t *testing.T) {
	raw := json.RawMessage(`{ "dot_number":"17","legal_name":" TEST LLC ","dba_name":"OTHER","phy_street":" TEST ROAD ","phy_city":"TEST CITY","phy_state":"mn","phy_zip":"12345-6789","phy_cnty":" 001 ","company_officer_1":" Test LLC ","company_officer_2":" Other LLC ","power_units":"014","hm_ind":"N","crgo_logpole":"X","crgo_bldgmat":"Y","crgo_meat":"N","crgo_cargoothr_desc":"OTHER","phone":123,"safety_rating":null }`)
	r, err := fmcsa.RowOf(raw, clock())
	if err != nil {
		t.Fatal(err)
	}
	if r.Scraper != fmcsa.Name || r.Run != "" || r.SourceID != "17" || !r.Scraped.Equal(clock().UTC().Truncate(time.Second)) || r.Digest != source.Digest(raw) || r.Name != "TEST LLC" || !reflect.DeepEqual(r.Names, []string{"TEST LLC", "OTHER"}) || r.Address != (source.Address{Street: "TEST ROAD", City: "Test City", State: "MN", Zip: "12345", County: "001"}) || r.Lat != nil || r.Lon != nil || !reflect.DeepEqual(r.People, []source.Person{{Name: "Test LLC", Role: fmcsa.Role}, {Name: "Other LLC", Role: fmcsa.Role}}) || !bytes.Equal(r.Raw, raw) {
		t.Fatal(r)
	}
	expected := map[string]any{"added": nil, "cargo": []string{"bldgmat", "logpole"}, "class": nil, "dot_number": "17", "email": nil, "hazmat": "N", "mcs150_date": nil, "mileage": nil, "mileage_year": nil, "operation": nil, "org_type": nil, "owned_tractors": nil, "owned_trailers": nil, "owned_trucks": nil, "phone": nil, "power_units": "014", "safety_rating": nil, "status": nil, "total_cdl": nil, "total_drivers": nil, "truck_units": nil}
	b, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(r.Facts, b) {
		t.Fatalf("%s\n%s", r.Facts, b)
	}
	inputs := []string{"add_date", "classdef", "dot_number", "email_address", "hm_ind", "mcs150_date", "mcs150_mileage", "mcs150_mileage_year", "carrier_operation", "business_org_desc", "owntract", "owntrail", "owntruck", "phone", "power_units", "safety_rating", "status_code", "total_cdl", "total_drivers", "truck_units"}
	outputs := []string{"added", "class", "dot_number", "email", "hazmat", "mcs150_date", "mileage", "mileage_year", "operation", "org_type", "owned_tractors", "owned_trailers", "owned_trucks", "phone", "power_units", "safety_rating", "status", "total_cdl", "total_drivers", "truck_units"}
	fields := map[string]any{}
	want := map[string]any{"cargo": []string{}}
	for i, k := range inputs {
		fields[k] = fmt.Sprint(i)
		want[outputs[i]] = fmt.Sprint(i)
	}
	b, _ = json.Marshal(fields)
	r, err = fmcsa.RowOf(b, clock())
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(want)
	if !bytes.Equal(r.Facts, b) || r.Name != "" || r.Names == nil || r.People == nil || len(r.People) != 0 {
		t.Fatal(r)
	}
}

// R-1GZH-MA3S
func TestBadRecord(t *testing.T) {
	for _, raw := range []string{"null", "[]", "17", "true", `"test"`, `{}`, `{"dot_number":null}`, `{"dot_number":17}`, `{"dot_number":""}`, `{broken`} {
		r, err := fmcsa.RowOf(json.RawMessage(raw), clock())
		if err == nil || !reflect.DeepEqual(r, source.Row{}) {
			t.Fatal(raw, r, err)
		}
	}
}

// R-P24J-OX0T R-160E-6CFJ
func TestDefaultClientAndPauseCancellation(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Method != http.MethodGet || r.Header.Get("User-Agent") != "default ua" || r.ContentLength != 0 {
			t.Error(r)
		}
		if r.URL.Query().Get("$select") != "" {
			_, _ = io.WriteString(w, `[{"count":"0"}]`)
		} else {
			_, _ = io.WriteString(w, `[]`)
		}
	}))
	defer srv.Close()
	cfg := fmcsa.Config{BaseURL: srv.URL, Now: clock, UserAgent: "default ua", States: []string{"MN"}, Status: "A"}
	if err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { t.Fatal("empty batch"); return nil }); err != nil || attempts != 2 {
		t.Fatal(err, attempts)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan struct{})
	cfg.HTTP = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { close(seen); return response(503, ""), nil })}
	done := make(chan error, 1)
	go func() { done <- fmcsa.New(cfg).Scrape(ctx, func([]source.Row) error { return nil }) }()
	<-seen
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("no cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("default pause ignored cancellation")
	}
}

// R-6H7L-69P5
func TestPageValidatedBeforeBatch(t *testing.T) {
	attempts, batches := 0, 0
	cfg := config(func(r *http.Request) (*http.Response, error) {
		attempts++
		if r.URL.Query().Get("$select") != "" {
			return response(200, `[{"count":"2"}]`), nil
		}
		return response(200, `[{"dot_number":"17"},{}]`), nil
	})
	if err := fmcsa.New(cfg).Scrape(context.Background(), func([]source.Row) error { batches++; return nil }); err == nil || attempts != 2 || batches != 0 {
		t.Fatal(err, attempts, batches)
	}
}

type observedBody struct {
	io.Reader
	complete func()
}

func (b observedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		b.complete()
	}
	return n, err
}
func (observedBody) Close() error { return nil }

// R-14SH-SKOU
func TestPageClockAfterBody(t *testing.T) {
	requests, reads, batches := 0, 0, 0
	complete := true
	pageTime := clock()
	cfg := config(func(r *http.Request) (*http.Response, error) {
		if !complete {
			t.Fatal("next request before complete body")
		}
		complete = false
		requests++
		body := `[{"count":"1"}]`
		if r.URL.Query().Get("$select") == "" {
			switch requests {
			case 2:
				body = `[{"dot_number":"first"},{"dot_number":"second"}]`
			case 4:
				body = `[{"dot_number":"third"}]`
			default:
				body = `[]`
			}
		}
		return &http.Response{StatusCode: 200, Body: observedBody{strings.NewReader(body), func() { complete = true }}}, nil
	})
	cfg.States = []string{"MN", "WI", "IA"}
	cfg.Now = func() time.Time {
		if !complete || (requests != 2 && requests != 4) {
			t.Fatal("clock before nonempty page received", complete, requests)
		}
		reads++
		pageTime = pageTime.Add(time.Hour)
		return pageTime
	}
	err := fmcsa.New(cfg).Scrape(context.Background(), func(rows []source.Row) error {
		batches++
		expectedRows := 2
		if batches == 2 {
			expectedRows = 1
		}
		if reads != batches || len(rows) != expectedRows {
			t.Fatal(reads, rows)
		}
		for _, row := range rows {
			if !row.Scraped.Equal(pageTime.UTC().Truncate(time.Second)) {
				t.Fatal(row.Scraped, pageTime)
			}
		}
		return nil
	})
	if err != nil || requests != 6 || reads != 2 || batches != 2 {
		t.Fatal(err, requests, reads, batches)
	}
}

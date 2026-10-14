package ppp_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/ppp"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func clock() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 789, time.FixedZone("east", 3600)) }
func cfg(f transport) ppp.Config {
	return ppp.Config{HTTP: &http.Client{Transport: f}, Now: clock, Sleep: func(context.Context, time.Duration) {}, Files: []string{"a.csv", "b.csv"}, States: []string{"MN", "WI"}, UserAgent: "test ua"}
}
func record(id, state string) []string { r := make([]string, 53); r[0] = id; r[7] = state; return r }
func csvBody(records ...[]string) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	_ = w.Write(ppp.Columns())
	for _, r := range records {
		_ = w.Write(r)
	}
	w.Flush()
	return b.String()
}

// R-CKLA-A05T R-CLT6-NRWI R-CO8Z-FBDW
func TestAPI(t *testing.T) {
	if ppp.Name != "ppp" || ppp.DefaultBaseURL != "https://data.sba.gov/sites/default/files/distribution/SBA-OCA-2022-07-001/" || ppp.BatchSize != 1000 || ppp.Retries != 3 || ppp.Backoff != time.Second || ppp.HeaderTimeout != 60*time.Second || ppp.BodyTimeout != 20*time.Minute {
		t.Fatal("constants")
	}
	var s source.Scraper = ppp.New(ppp.Config{})
	if s.Name() != ppp.Name {
		t.Fatal(s.Name())
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a.csv" || r.Header.Get("User-Agent") != "default ua" {
			t.Error(r)
		}
		_, _ = io.WriteString(w, csvBody())
	}))
	defer srv.Close()
	if err := ppp.New(ppp.Config{BaseURL: srv.URL + "/", Files: []string{"a.csv"}, UserAgent: "default ua"}).Scrape(context.Background(), func([]source.Row) error { t.Fatal("empty batch"); return nil }); err != nil {
		t.Fatal(err)
	}
}

// R-CN13-1JN7
func TestColumns(t *testing.T) {
	want := strings.Split("LoanNumber DateApproved SBAOfficeCode ProcessingMethod BorrowerName BorrowerAddress BorrowerCity BorrowerState BorrowerZip LoanStatusDate LoanStatus Term SBAGuarantyPercentage InitialApprovalAmount CurrentApprovalAmount UndisbursedAmount FranchiseName ServicingLenderLocationID ServicingLenderName ServicingLenderAddress ServicingLenderCity ServicingLenderState ServicingLenderZip RuralUrbanIndicator HubzoneIndicator LMIIndicator BusinessAgeDescription ProjectCity ProjectCountyName ProjectState ProjectZip CD JobsReported NAICSCode Race Ethnicity UTILITIES_PROCEED PAYROLL_PROCEED MORTGAGE_INTEREST_PROCEED RENT_PROCEED REFINANCE_EIDL_PROCEED HEALTH_CARE_PROCEED DEBT_INTEREST_PROCEED BusinessType OriginatingLenderLocationID OriginatingLender OriginatingLenderCity OriginatingLenderState Gender Veteran NonProfit ForgivenessAmount ForgivenessDate", " ")
	got := ppp.Columns()
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	got[0] = "changed"
	if !reflect.DeepEqual(ppp.Columns(), want) {
		t.Fatal("shared header")
	}
}

// R-VUUZ-BBLC R-CQOS-6UVA R-CRWO-KMLZ R-VW2V-P3C1
func TestWalk(t *testing.T) {
	var records [][]string
	for i := range 2540 {
		state := "mn"
		if i%64 == 0 {
			state = "IA"
		}
		r := record(fmt.Sprint(i), state)
		r[4] = "A," + string([]byte{0xBF, 0xA0, 0x80})
		if i == 1 {
			r[0] = "0"
		}
		if i == 2 {
			r[7] = ""
			r[29] = "mn"
		}
		records = append(records, r)
	}
	attempts, calls, nowReads := 0, 0, 0
	var rows []source.Row
	var sizes []int
	c := cfg(func(r *http.Request) (*http.Response, error) {
		attempts++
		want := ppp.DefaultBaseURL + []string{"a.csv", "b.csv"}[attempts-1]
		if r.URL.String() != want || r.Method != "GET" || r.Header.Get("User-Agent") != "test ua" || r.Body != nil || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Range") != "" {
			t.Error(r)
		}
		if attempts == 2 && calls != 3 {
			t.Error("next file before batches")
		}
		if attempts == 1 {
			return response(200, csvBody(records...)), nil
		}
		return response(200, csvBody(record("", "IA"))), nil
	})
	c.Now = func() time.Time { nowReads++; return clock().Add(time.Duration(nowReads) * time.Hour) }
	err := ppp.New(c).Scrape(context.Background(), func(batch []source.Row) error {
		calls++
		sizes = append(sizes, len(batch))
		for _, r := range batch {
			if !r.Collected.Equal(clock().Add(time.Duration(calls) * time.Hour).UTC().Truncate(time.Second)) {
				t.Fatal(r.Collected)
			}
		}
		rows = append(rows, batch...)
		return nil
	})
	if err != nil || attempts != 2 || !reflect.DeepEqual(sizes, []int{1000, 1000, 500}) || nowReads != calls {
		t.Fatal(err, attempts, sizes, nowReads)
	}
	index := 0
	for _, r := range records {
		if r[7] == "IA" {
			continue
		}
		decoded := append([]string(nil), r...)
		decoded[4] = "A,¿\u00a0\u0080"
		want, e := ppp.RowOf(decoded, rows[index].Collected)
		if e != nil || !reflect.DeepEqual(rows[index], want) {
			t.Fatal(index, rows[index], want, e)
		}
		index++
	}
	c.Files = nil
	if err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error { t.Fatal("batch"); return nil }); err != nil || attempts != 2 {
		t.Fatal(err, attempts)
	}
}

// R-PRZQ-6BXF R-D5BK-S3RM R-PT7M-K3O4
func TestRowOf(t *testing.T) {
	r := make([]string, 53)
	for i := range r {
		r[i] = fmt.Sprint(i)
	}
	r[4] = " ACME INC "
	r[5] = " STREET "
	r[6] = ""
	r[27] = "TEST CITY"
	r[7] = ""
	r[29] = "wi"
	r[8] = ""
	r[30] = "55401-1234"
	r[28] = " county "
	r[33] = " 238220 "
	r[43] = "Limited  Liability Company(LLC)"
	r[16] = ""
	r[52] = "A<B¿\"x, y\""
	got, err := ppp.RowOf(r, clock())
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	raw.WriteByte('{')
	for i, c := range ppp.Columns() {
		if i > 0 {
			raw.WriteByte(',')
		}
		key, _ := json.Marshal(c)
		val, _ := json.Marshal(r[i])
		raw.Write(key)
		raw.WriteByte(':')
		raw.Write(val)
	}
	raw.WriteByte('}')
	keys := strings.Fields("amount business_age business_type cd date_approved debt_interest_proceed forgiven forgiveness_date franchise health_care_proceed hubzone initial_amount jobs_reported lender lender_city lender_state lmi loan_number loan_status loan_status_date mortgage_interest_proceed naics nonprofit payroll_proceed processing_method refinance_eidl_proceed rent_proceed rural_urban sba_office servicing_lender term undisbursed utilities_proceed")
	indices := []int{14, 26, 43, 31, 1, 42, 51, 52, 16, 41, 24, 13, 32, 45, 46, 47, 25, 0, 10, 9, 38, 33, 50, 37, 3, 40, 39, 23, 2, 18, 11, 15, 36}
	facts := map[string]string{}
	for i, k := range keys {
		facts[k] = r[indices[i]]
	}
	f, _ := json.Marshal(facts)
	want := source.Row{Scraper: ppp.Name, SourceID: r[0], Collected: clock().UTC().Truncate(time.Second), Digest: source.Digest(raw.Bytes()), Name: "ACME INC", Names: []string{"ACME INC"}, Address: source.Address{Street: "STREET", City: "Test City", State: "WI", Zip: "55401", County: "county"}, People: []source.Person{}, Raw: raw.Bytes(), Facts: f}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	r[6], r[7], r[8] = " BORROWER CITY ", "mn", "55101-9999"
	got, err = ppp.RowOf(r, clock())
	if err != nil || got.Address != (source.Address{Street: "STREET", City: "Borrower City", State: "MN", Zip: "55101", County: "county"}) {
		t.Fatal(got.Address, err)
	}
	r[4] = ""
	got, err = ppp.RowOf(r, clock())
	if err != nil || got.Name != "" || got.Names == nil || len(got.Names) != 0 {
		t.Fatal(got, err)
	}
}

// R-D7RD-JN90
func TestBadRow(t *testing.T) {
	for _, r := range [][]string{nil, make([]string, 52), make([]string, 54), make([]string, 53)} {
		got, err := ppp.RowOf(r, clock())
		if err == nil || !reflect.DeepEqual(got, source.Row{}) {
			t.Fatal(got, err)
		}
	}
}

// R-POC1-10PC R-PPJX-ESG1 R-CY06-HHBG
func TestInvalidFiles(t *testing.T) {
	h := ppp.Columns()
	swapped := ppp.Columns()
	swapped[6], swapped[7] = swapped[7], swapped[6]
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(swapped)
	w.Flush()
	cases := []string{"", "\"unterminated", strings.Join(h[:52], ",") + "\n", strings.Join(append(h, "extra"), ",") + "\n", strings.Replace(strings.Join(h, ","), "CD", "other", 1) + "\n", b.String(), csvBody(record("", "MN")), csvBody(record("17", "MN")) + "short,row\n", strings.TrimSuffix(csvBody(record("17", "MN")), "\r\n"), csvBody(record("17", "MN")) + "a\"b\n"}
	for _, body := range cases {
		attempts, sleeps, batches := 0, 0, 0
		c := cfg(func(*http.Request) (*http.Response, error) { attempts++; return response(200, body), nil })
		c.Sleep = func(context.Context, time.Duration) { sleeps++ }
		if err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return nil }); err == nil || attempts != 1 || sleeps != 0 || batches != 0 {
			t.Fatal(err, attempts, sleeps, batches)
		}
	}
	for _, status := range []int{301, 400, 404, 600} {
		attempts, sleeps := 0, 0
		c := cfg(func(*http.Request) (*http.Response, error) { attempts++; return response(status, ""), nil })
		c.Sleep = func(context.Context, time.Duration) { sleeps++ }
		if err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error { t.Fatal("batch"); return nil }); err == nil || attempts != 1 || sleeps != 0 {
			t.Fatal(err, attempts, sleeps)
		}
	}
}

// R-CZ82-V925
func TestRetry(t *testing.T) {
	for _, kind := range []string{"network", "timeout", "429", "503", "599"} {
		attempts := 0
		var sleeps []time.Duration
		c := cfg(func(r *http.Request) (*http.Response, error) {
			attempts++
			switch kind {
			case "network":
				return nil, errors.New("offline")
			case "timeout":
				<-r.Context().Done()
				return nil, r.Context().Err()
			case "429":
				return response(429, ""), nil
			case "503":
				return response(503, ""), nil
			default:
				return response(599, ""), nil
			}
		})
		c.HeaderTimeout = time.Millisecond
		c.Sleep = func(_ context.Context, d time.Duration) { sleeps = append(sleeps, d) }
		err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error { t.Fatal("batch"); return nil })
		if err == nil || attempts != 4 || !reflect.DeepEqual(sleeps, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
			t.Fatal(kind, err, attempts, sleeps)
		}
	}
	attempts, sleeps := 0, 0
	c := cfg(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(500, ""), nil
		}
		return response(200, csvBody()), nil
	})
	c.Sleep = func(_ context.Context, d time.Duration) {
		sleeps++
		if d != time.Second {
			t.Error(d)
		}
	}
	if err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error { return nil }); err != nil || attempts != 3 || sleeps != 1 {
		t.Fatal(err, attempts, sleeps)
	}
}

type blocked struct{ ctx context.Context }

func (b blocked) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (blocked) Close() error               { return nil }

type broken struct{}

func (broken) Read([]byte) (int, error) { return 0, errors.New("broken body") }
func (broken) Close() error             { return nil }

type body struct {
	io.Reader
	close func()
}

func (b body) Close() error {
	if b.close != nil {
		b.close()
	}
	return nil
}

// R-VW2V-P3C1 R-PQRT-SK6Q R-PPJX-ESG1
func TestStreaming(t *testing.T) {
	records := make([][]string, 1000)
	for i := range records {
		records[i] = record(fmt.Sprint(i), "MN")
	}
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprint(refuse), func(t *testing.T) {
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			defer func() { _ = writer.Close() }()
			allowed := make(chan struct{})
			written := make(chan struct{})
			closed := make(chan struct{})
			attempts, calls := 0, 0
			c := cfg(func(*http.Request) (*http.Response, error) {
				attempts++
				if attempts > 1 {
					return response(404, ""), nil
				}
				return &http.Response{StatusCode: 200, Body: body{Reader: reader, close: func() { close(closed); _ = reader.Close() }}}, nil
			})
			c.Files = []string{"a.csv", "b.csv"}
			go func() {
				_, _ = io.WriteString(writer, csvBody(records...))
				close(written)
				<-allowed
				_ = writer.Close()
			}()
			done := make(chan error, 1)
			go func() {
				done <- ppp.New(c).Scrape(context.Background(), func(rows []source.Row) error {
					calls++
					if len(rows) != 1000 {
						t.Error(len(rows))
					}
					if refuse {
						return errors.New("refused")
					}
					close(allowed)
					return nil
				})
			}()
			if refuse {
				select {
				case err := <-done:
					if err == nil || calls != 1 || attempts != 1 {
						t.Fatal(err, calls, attempts)
					}
				case <-time.After(time.Second):
					t.Fatal("waited for body after refusal")
				}
				close(allowed)
			} else {
				// Stop the walk at its next file: the first file's batch has already released its body.
				select {
				case <-written:
				case <-time.After(time.Second):
					t.Fatal("not streaming")
				}
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("not finished first body")
				}
				// The second file is refused after the first file finished.
				select {
				case err := <-done:
					if err == nil || calls != 1 || attempts != 2 {
						t.Fatal(err, calls, attempts)
					}
				case <-time.After(time.Second):
					t.Fatal("walk hung")
				}
			}
		})
	}
}

// R-PPJX-ESG1 R-CZ82-V925
func TestBodyFailuresAndSeparateBounds(t *testing.T) {
	for _, kind := range []string{"broken", "timeout", "headerslow", "bodyslow"} {
		t.Run(kind, func(t *testing.T) {
			attempts, sleeps, calls := 0, 0, 0
			c := cfg(func(r *http.Request) (*http.Response, error) {
				attempts++
				if kind == "headerslow" {
					select {
					case <-time.After(30 * time.Millisecond):
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				var b io.ReadCloser
				switch kind {
				case "broken":
					b = body{Reader: io.MultiReader(strings.NewReader(csvBody(record("17", "MN"))), broken{})}
				case "timeout":
					b = body{Reader: io.MultiReader(strings.NewReader(csvBody(record("17", "MN"))), blocked{r.Context()})}
				case "bodyslow":
					pr, pw := io.Pipe()
					b = pr
					go func() {
						select {
						case <-time.After(30 * time.Millisecond):
							_, _ = io.WriteString(pw, csvBody(record("17", "MN")))
						case <-r.Context().Done():
						}
						_ = pw.Close()
					}()
				default:
					b = io.NopCloser(strings.NewReader(csvBody(record("17", "MN"))))
				}
				return &http.Response{StatusCode: 200, Body: b}, nil
			})
			c.Files = []string{"a.csv"}
			c.HeaderTimeout = time.Second
			c.BodyTimeout = 10 * time.Millisecond
			if kind == "bodyslow" {
				c.HeaderTimeout = 10 * time.Millisecond
				c.BodyTimeout = time.Second
			}
			c.Sleep = func(context.Context, time.Duration) { sleeps++ }
			err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error { calls++; return nil })
			success := kind == "headerslow" || kind == "bodyslow"
			if (err == nil) != success || attempts != 1 || sleeps != 0 || (success && calls != 1) || (!success && calls != 0) {
				t.Fatal(err, attempts, sleeps, calls)
			}
		})
	}
}

// R-D1NV-MSJJ R-CLT6-NRWI
func TestCancellation(t *testing.T) {
	for _, phase := range []string{"headers", "body", "sleep", "defaultsleep", "beforebatch", "initial"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			attempts, calls := 0, 0
			c := cfg(func(r *http.Request) (*http.Response, error) {
				attempts++
				switch phase {
				case "headers":
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				case "body":
					close(entered)
					return &http.Response{StatusCode: 200, Body: blocked{r.Context()}}, nil
				case "sleep", "defaultsleep":
					return response(503, ""), nil
				case "beforebatch":
					cancel()
					return response(200, csvBody(record("17", "MN"))), nil
				default:
					return nil, errors.New("unexpected")
				}
			})
			c.Sleep = func(sctx context.Context, _ time.Duration) { close(entered); <-sctx.Done() }
			if phase == "defaultsleep" {
				c.Sleep = nil
				c.HTTP = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { attempts++; close(entered); return response(503, ""), nil })}
			}
			if phase == "initial" {
				cancel()
			}
			done := make(chan error, 1)
			go func() { done <- ppp.New(c).Scrape(ctx, func([]source.Row) error { calls++; return nil }) }()
			if phase == "headers" || phase == "body" || phase == "sleep" || phase == "defaultsleep" {
				select {
				case <-entered:
					cancel()
				case <-time.After(time.Second):
					t.Fatal("not entered")
				}
			}
			select {
			case err := <-done:
				want := 1
				if phase == "initial" {
					want = 0
				}
				if err == nil || attempts != want || calls != 0 {
					t.Fatal(err, attempts, calls)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation hung")
			}
		})
	}
}

// R-D2VS-0KA8
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
			}, Rand: bytes.NewReader(make([]byte, 2048)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer w.Shutdown(context.Background(), "test")
			attempts := 0
			c := cfg(func(*http.Request) (*http.Response, error) {
				if err := w.Flush(context.Background()); err != nil {
					t.Error(err)
				}
				if len(capture.Events()) != attempts {
					t.Error("previous event missing before request")
				}
				attempts++
				if attempts == 1 {
					return nil, errors.New("offline")
				}
				if attempts == 2 {
					return response(500, ""), nil
				}
				return response(200, csvBody(record("17", "MN"))), nil
			})
			c.Telemetry = w
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "user", RequestID: "request"})
			if err := ppp.New(c).Scrape(ctx, func([]source.Row) error {
				if err := w.Flush(context.Background()); err != nil {
					t.Error(err)
				}
				if len(capture.Events()) != attempts {
					t.Error("event not before batch")
				}
				return nil
			}); err != nil {
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
				if e.Name != source.Called || e.RequestID != "request" || e.User != "user" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"scraper": ppp.Name, "status": []int64{0, 500, 200, 200}[i], "duration_us": duration}) {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-PUFI-XVET
func TestPrivacy(t *testing.T) {
	marker := "GHIJKLMNopqrstuvWXYZ0123"
	loan := "1234567890"
	r := make([]string, 53)
	for i := range r {
		r[i] = marker
	}
	r[0] = loan
	r[7] = "MN"
	r[29] = "WI"
	missing := ppp.Columns()
	missing = missing[:52]
	var h strings.Builder
	wcsv := csv.NewWriter(&h)
	_ = wcsv.Write(missing)
	_ = wcsv.Write(r)
	wcsv.Flush()
	cases := []struct {
		status int
		body   string
		refuse bool
	}{{500, csvBody(r), false}, {200, h.String(), false}, {200, csvBody(r) + strings.Join(r[:52], ",") + "\n", false}, {200, strings.TrimSuffix(csvBody(r), "\r\n"), false}, {200, csvBody(append([]string{""}, r[1:]...)), false}, {200, csvBody(r), true}}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var capture telemetry.Capture
			w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: clock, Rand: bytes.NewReader(make([]byte, 2048)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer w.Shutdown(context.Background(), "test")
			c := cfg(func(*http.Request) (*http.Response, error) { return response(tc.status, tc.body), nil })
			c.Telemetry = w
			err := ppp.New(c).Scrape(context.Background(), func([]source.Row) error {
				if tc.refuse {
					return errors.New("refused")
				}
				return nil
			})
			if err == nil || strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), loan) {
				t.Fatal(err)
			}
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, e := range capture.Events() {
				b, _ := json.Marshal(e.Attrs)
				if bytes.Contains(b, []byte(marker)) || bytes.Contains(b, []byte(loan)) {
					t.Fatal(e)
				}
			}
		})
	}
}

// R-VUUZ-BBLC R-VW2V-P3C1
func TestScopeUsesUntrimmedBorrowerBeforeProject(t *testing.T) {
	first := record("spaced-borrower", " MN ")
	first[29] = "MN"
	second := record("other-borrower", "IA")
	second[29] = "MN"
	third := record("borrower-in-scope", "mn")
	third[29] = "IA"
	fourth := record("fallback-in-scope", "")
	fourth[29] = "wi"
	fifth := record("spaced-fallback", "")
	fifth[29] = " WI "
	c := cfg(func(*http.Request) (*http.Response, error) {
		return response(200, csvBody(first, second, third, fourth, fifth)), nil
	})
	c.Files = []string{"a.csv"}
	var got []string
	err := ppp.New(c).Scrape(context.Background(), func(rows []source.Row) error {
		for _, r := range rows {
			got = append(got, r.SourceID)
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, []string{"borrower-in-scope", "fallback-in-scope"}) {
		t.Fatal(got, err)
	}
}

type gatedReader struct {
	io.Reader
	entered chan struct{}
	release chan struct{}
	started bool
}

func (r *gatedReader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
		close(r.entered)
		<-r.release
	}
	return r.Reader.Read(p)
}

// R-D2VS-0KA8
func TestTelemetryExcludesBodyDuration(t *testing.T) {
	var capture telemetry.Capture
	now := clock()
	w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { v := now; now = now.Add(1500 * time.Microsecond); return v }, Rand: bytes.NewReader(make([]byte, 2048)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer w.Shutdown(context.Background(), "test")
	entered, release := make(chan struct{}), make(chan struct{})
	c := cfg(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(&gatedReader{Reader: strings.NewReader(csvBody(record("17", "MN"))), entered: entered, release: release})}, nil
	})
	c.Files = []string{"a.csv"}
	c.Telemetry = w
	done := make(chan error, 1)
	var events []telemetry.Event
	go func() {
		done <- ppp.New(c).Scrape(context.Background(), func([]source.Row) error {
			if err := w.Flush(context.Background()); err != nil {
				t.Error(err)
			}
			events = capture.Events()
			if len(events) != 1 || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"scraper": ppp.Name, "status": int64(200), "duration_us": int64(1500)}) || events[0].RequestID != "" || events[0].User != "" {
				t.Error("request event absent or wrong before batch", events)
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("body not entered")
	}
	now = now.Add(time.Hour)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("body did not finish")
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(capture.Events(), events) {
		t.Fatal("body changed request event", capture.Events())
	}
}

package oshaita_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/oshaita"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

func clock() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 789, time.FixedZone("test", 3600)) }
func fixture() ([]string, []string) {
	h := append(oshaita.Columns(), strings.Fields("ein naics_code naics_year industry_description establishment_type size no_injuries_illnesses created_timestamp change_reason total_deaths")...)
	r := make([]string, len(h))
	for i := range r {
		r[i] = fmt.Sprintf("value-%d", i)
	}
	put(h, r, "id", "12345678901234567890")
	put(h, r, "state", "MN")
	put(h, r, "created_timestamp", "1/1/2023")
	return h, r
}
func put(h, r []string, k, v string) { r[slices.Index(h, k)] = v }
func csvBytes(h []string, rs ...[]string) []byte {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	_ = w.Write(h)
	for _, r := range rs {
		_ = w.Write(r)
	}
	w.Flush()
	return b.Bytes()
}
func archive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, v := range entries {
		e, err := w.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = e.Write(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func spoolContext(t *testing.T) (context.Context, *[]string) {
	t.Helper()
	dir := t.TempDir()
	paths := []string{}
	handles := []*os.File{}
	t.Cleanup(func() {
		for _, f := range handles {
			if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("spool handle not closed: %s: %v", f.Name(), err)
			}
		}
	})
	return source.WithSpool(context.Background(), func() (*os.File, error) {
		for _, p := range paths {
			if _, e := os.Stat(p); !os.IsNotExist(e) {
				t.Errorf("prior spool retained: %s", p)
			}
		}
		f, e := os.CreateTemp(dir, "spool.")
		if e == nil {
			paths = append(paths, f.Name())
			handles = append(handles, f)
		}
		return f, e
	}), &paths
}
func noFiles(t *testing.T, paths []string) {
	t.Helper()
	for _, p := range paths {
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Errorf("spool retained: %s", p)
		}
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func cfg(fn transport) oshaita.Config {
	return oshaita.Config{HTTP: &http.Client{Transport: fn}, Now: clock, Sleep: func(context.Context, time.Duration) {}, Files: []string{"largefiles/a.csv"}, States: []string{"MN", "WI"}}
}
func answer(status int, b []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b))}
}

// R-4DMQ-XPWI R-4EUN-BHN7 R-4G2J-P9DW R-4HAG-314L
func TestAPI(t *testing.T) {
	if oshaita.Name != "osha_ita" || oshaita.DefaultBaseURL != "https://www.osha.gov/sites/default/" || oshaita.UserAgent != "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36" || oshaita.BatchSize != 1000 || oshaita.Retries != 3 || oshaita.Backoff != time.Second || oshaita.HeaderTimeout != 60*time.Second || oshaita.FileTimeout != 30*time.Minute {
		t.Fatal("constants")
	}
	var sc source.Scraper = oshaita.New(oshaita.Config{nil, "", nil, clock, nil, nil, nil, 0, 0})
	if sc.Name() != oshaita.Name {
		t.Fatal(sc.Name())
	}
	ctx, _ := spoolContext(t)
	if err := sc.Scrape(ctx, func([]source.Row) error { t.Fatal("unexpected batch"); return nil }); err != nil {
		t.Fatal(err)
	}
	want := strings.Fields("id company_name establishment_name street_address city state zip_code establishment_id year_filing_for annual_average_employees total_hours_worked")
	h := oshaita.Columns()
	if !reflect.DeepEqual(h, want) {
		t.Fatal(h)
	}
	h[0] = "changed"
	if !reflect.DeepEqual(oshaita.Columns(), want) {
		t.Fatal("shared columns")
	}
}

// R-4IIC-GSVA
func TestDecode(t *testing.T) {
	for _, s := range []string{"", "café", "日本語", "a�b"} {
		if oshaita.Decode(s) != s {
			t.Fatal(s)
		}
	}
	want := []rune("€\u0081‚ƒ„…†‡ˆ‰Š‹Œ\u008dŽ\u008f\u0090‘’“”•–—˜™š›œ\u009džŸ")
	for b := 0x80; b <= 0xff; b++ {
		expected := rune(b)
		if b < 0xa0 {
			expected = want[b-0x80]
		}
		if got := oshaita.Decode(string([]byte{byte(b)})); got != string(expected) || !utf8.ValidString(got) {
			t.Fatalf("byte %x: %q", b, got)
		}
	}
	if got := oshaita.Decode(string([]byte{0xc3, 0xa9, 0x92})); got != "é’" {
		t.Fatal(got)
	}
	if got := oshaita.Decode("Walker" + string([]byte{0x92}) + "s"); got != "Walker’s" {
		t.Fatal(got)
	}
}

// R-4JQ8-UKLZ
func TestSubmitted(t *testing.T) {
	for s, want := range map[string]string{"1/2/2018 8:16:46": "2018-01-02T08:16:46Z", "12/31/2020 23:05:09": "2020-12-31T23:05:09Z", "01JAN22:11:14:00": "2022-01-01T11:14:00Z", "31dec21:23:59:59": "2021-12-31T23:59:59Z", "15MAR99:01:02:03": "1999-03-15T01:02:03Z", "1/1/2023": "2023-01-01T00:00:00Z"} {
		got, ok := oshaita.ParseSubmitted(s)
		if !ok || got.Format(time.RFC3339) != want {
			t.Fatal(s, got, ok)
		}
	}
	for _, s := range []string{"", "2023-01-01", "1/1/23", "2/30/2023", "1JAN22:11:14:00", " 1/1/2023"} {
		if got, ok := oshaita.ParseSubmitted(s); ok || !got.IsZero() {
			t.Fatal(s, got)
		}
	}
}

// R-55OF-QFYH R-56WC-47P6
func TestRow(t *testing.T) {
	for _, names := range [][2]string{{"ACME INC", "ACME INC"}, {"", "DC Warehouse"}, {" ACME INC ", "ACME INC - PLANT 2"}, {"", ""}} {
		h, r := fixture()
		put(h, r, "company_name", names[0])
		put(h, r, "establishment_name", names[1])
		put(h, r, "id", "299604.00")
		put(h, r, "establishment_id", "1234.00")
		put(h, r, "annual_average_employees", "63.00")
		put(h, r, "ein", " 010739420")
		put(h, r, "zip_code", "55401-1234")
		for _, modern := range []bool{false, true} {
			hh := slices.Clone(h)
			if modern {
				hh[slices.Index(h, "created_timestamp")] = "date_submitted"
				for i := range hh {
					hh[i] = strings.ToUpper(hh[i])
				}
			}
			row, e := oshaita.RowOf(hh, r, clock())
			if e != nil {
				t.Fatal(e)
			}
			var raw bytes.Buffer
			raw.WriteByte('{')
			facts := map[string]string{}
			keys := strings.Fields("company_name establishment_name ein id establishment_id year_filing_for naics_code naics_year industry_description establishment_type size annual_average_employees total_hours_worked no_injuries_illnesses date_submitted change_reason")
			for _, k := range keys {
				c := k
				if k == "date_submitted" {
					c = "created_timestamp"
				}
				facts[k] = r[slices.Index(h, c)]
			}
			facts["id"] = "299604"
			facts["establishment_id"] = "1234"
			for i, c := range hh {
				if i > 0 {
					raw.WriteByte(',')
				}
				k, _ := json.Marshal(c)
				v, _ := json.Marshal(r[i])
				raw.Write(k)
				raw.WriteByte(':')
				raw.Write(v)
			}
			raw.WriteByte('}')
			f, _ := json.Marshal(facts)
			ns := source.Names(names[0], names[1])
			name := ""
			if len(ns) > 0 {
				name = ns[0]
			}
			want := source.Row{Scraper: oshaita.Name, SourceID: "299604", Scraped: clock().UTC().Truncate(time.Second), Digest: source.Digest(raw.Bytes()), Name: name, Names: ns, Address: source.NewAddress(r[3], r[4], r[5], r[6], ""), People: []source.Person{}, Facts: f, Raw: raw.Bytes()}
			if !reflect.DeepEqual(row, want) {
				t.Fatalf("got %#v want %#v", row, want)
			}
		}
	}
	h := oshaita.Columns()
	r := make([]string, len(h))
	r[0] = "7"
	row, e := oshaita.RowOf(h, r, clock())
	if e != nil {
		t.Fatal(e)
	}
	var facts map[string]string
	if e = json.Unmarshal(row.Facts, &facts); e != nil {
		t.Fatal(e)
	}
	if len(facts) != 16 || facts["naics_year"] != "" || facts["date_submitted"] != "" {
		t.Fatal(facts)
	}
	h, r = fixture()
	h = append(h, "date_submitted")
	r = append(r, "")
	row, e = oshaita.RowOf(h, r, clock())
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(row.Facts, &facts); e != nil || facts["date_submitted"] != "" {
		t.Fatal(facts, e)
	}
}

// R-NVFZ-5B1K
func TestInvalidRows(t *testing.T) {
	for _, mode := range []string{"short", "long", "nil", "missing", "duplicate", "optional duplicate", "empty id", "dot id"} {
		t.Run(mode, func(t *testing.T) {
			h, r := fixture()
			switch mode {
			case "short":
				r = r[:len(r)-1]
			case "long":
				r = append(r, "")
			case "nil":
				r = nil
			case "missing":
				h[5] = "other"
			case "duplicate":
				h = append(h, "ID")
				r = append(r, "other")
			case "optional duplicate":
				h = append(h, "NAICS_YEAR")
				r = append(r, "other")
			case "empty id":
				r[0] = ""
			case "dot id":
				r[0] = ".00"
			}
			got, e := oshaita.RowOf(h, r, clock())
			if e == nil || !reflect.DeepEqual(got, source.Row{}) {
				t.Fatal(got, e)
			}
		})
	}
}

// R-NQKD-M82S R-4M61-M43D R-520Q-L4QE R-4PTQ-RFBG
func TestWalkAndSpools(t *testing.T) {
	h, r := fixture()
	put(h, r, "company_name", "comma, and\nline")
	put(h, r, "establishment_name", "Walker"+string([]byte{0x92})+"s")
	csvBody := append([]byte{0xef, 0xbb, 0xbf}, csvBytes(h, r)...)
	bodyZip := archive(t, map[string][]byte{"CY 2022.CSV": csvBody, "extra.txt": []byte("companion")})
	ctx, paths := spoolContext(t)
	calls, batches := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		expected := []string{"/largefiles/a.csv", "/files/b%20c.ZIP"}[calls-1]
		if req.URL.EscapedPath() != expected || req.Method != "GET" || req.Header.Get("User-Agent") != oshaita.UserAgent || req.ContentLength != 0 {
			t.Error(req.URL, req.Method, req.Header)
		}
		for _, k := range []string{"Authorization", "Cookie", "Range"} {
			if req.Header.Get(k) != "" {
				t.Error(k)
			}
		}
		if batches != calls-1 {
			t.Error("request before batch")
		}
		noFiles(t, *paths)
		b := csvBody
		if calls == 2 {
			b = bodyZip
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	c := oshaita.Config{HTTP: srv.Client(), BaseURL: srv.URL + "/", Now: clock, Files: []string{"largefiles/a.csv", "files/b c.ZIP"}, States: []string{"MN"}}
	e := oshaita.New(c).Scrape(ctx, func(rows []source.Row) error {
		batches++
		if len(rows) != 1 || rows[0].Name != "comma, and\nline" || rows[0].Names[1] != "Walker’s" {
			t.Error(rows)
		}
		if len(*paths) != batches {
			t.Error(paths)
		}
		b, err := os.ReadFile((*paths)[batches-1])
		want := csvBody
		if batches == 2 {
			want = bodyZip
		}
		if err != nil || !bytes.Equal(b, want) {
			t.Error("spool contents", err)
		}
		return nil
	})
	if e != nil || calls != 2 || batches != 2 {
		t.Fatal(e, calls, batches)
	}
	noFiles(t, *paths)
}

// R-NRS9-ZZTH
func TestBatchesScopeAndCase(t *testing.T) {
	for _, count := range []int{0, 1000, 2500} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			h, _ := fixture()
			for i := range h {
				h[i] = strings.ToUpper(h[i])
			}
			var rs [][]string
			for i := 0; i < count+40; i++ {
				_, r := fixture()
				r[0] = fmt.Sprint(i)
				r[5] = []string{"mn", "Wi"}[i%2]
				if i >= count {
					r[0] = ""
					r[5] = " MN"
				}
				rs = append(rs, r)
			}
			ctx, _ := spoolContext(t)
			c := cfg(func(*http.Request) (*http.Response, error) { return answer(200, csvBytes(h, rs...)), nil })
			n, batches, nowCalls := 0, 0, 0
			c.Now = func() time.Time { nowCalls++; return clock().Add(time.Duration(batches) * time.Second) }
			e := oshaita.New(c).Scrape(ctx, func(rows []source.Row) error {
				if nowCalls != batches+1 {
					t.Errorf("Now calls %d before batch %d", nowCalls, batches+1)
				}
				want := min(1000, count-n)
				if len(rows) != want {
					t.Error(len(rows), want)
				}
				for _, r := range rows {
					if r.SourceID != fmt.Sprint(n) || !r.Scraped.Equal(clock().UTC().Truncate(time.Second).Add(time.Duration(batches)*time.Second)) {
						t.Error(r.SourceID, r.Scraped)
					}
					n++
				}
				batches++
				return nil
			})
			if e != nil || n != count || batches != (count+999)/1000 || nowCalls != batches {
				t.Fatal(e, n, batches)
			}
		})
	}
}

// R-4OLU-DNKR
func TestAmendments(t *testing.T) {
	cases := [][3]string{{"1/2/2018 8:16:46", "1/2/2018 8:16:45", "first"}, {"01JAN22:11:14:00", "02JAN22:09:00:00", "second"}, {"01JAN22:00:00:00", "31DEC21:23:59:59", "first"}, {"1/1/2023", "1/1/2023", "second"}, {"1/5/2023", "", "second"}, {"bad", "1/1/2023", "second"}}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			h, a := fixture()
			a[0] = "5.00"
			put(h, a, "created_timestamp", tc[0])
			put(h, a, "company_name", "first")
			b := slices.Clone(a)
			b[0] = "5"
			put(h, b, "created_timestamp", tc[1])
			put(h, b, "company_name", "second")
			other := slices.Clone(a)
			other[0] = "6"
			out := slices.Clone(b)
			out[5] = "CA"
			put(h, out, "created_timestamp", "1/1/2099")
			ctx, _ := spoolContext(t)
			c := cfg(func(*http.Request) (*http.Response, error) { return answer(200, csvBytes(h, a, other, b, out)), nil })
			c.Files = []string{"a.csv", "b.csv"}
			var rows []source.Row
			e := oshaita.New(c).Scrape(ctx, func(rs []source.Row) error { rows = append(rows, rs...); return nil })
			if e != nil || len(rows) != 4 {
				t.Fatal(e, rows)
			}
			order := []string{"5", "6", "5", "6"}
			if tc[2] == "second" {
				order = []string{"6", "5", "6", "5"}
			}
			for i, r := range rows {
				if r.SourceID != order[i] || (r.SourceID == "5" && r.Name != tc[2]) {
					t.Fatal(rows)
				}
			}
		})
	}
	h, a := fixture()
	a[0] = "3"
	b := slices.Clone(a)
	d := slices.Clone(a)
	put(h, a, "created_timestamp", "1/3/2023")
	put(h, b, "created_timestamp", "1/1/2023")
	put(h, d, "created_timestamp", "1/2/2023")
	ctx, _ := spoolContext(t)
	c := cfg(func(*http.Request) (*http.Response, error) { return answer(200, csvBytes(h, a, b, d)), nil })
	e := oshaita.New(c).Scrape(ctx, func(rs []source.Row) error {
		want, _ := oshaita.RowOf(h, a, clock())
		if len(rs) != 1 || !reflect.DeepEqual(rs[0], want) {
			t.Error(rs)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

// R-4R1N-5725
func TestSpoolRequired(t *testing.T) {
	for _, missing := range []bool{false, true} {
		calls, batches, pauses := 0, 0, 0
		c := cfg(func(*http.Request) (*http.Response, error) {
			calls++
			h, r := fixture()
			return answer(200, csvBytes(h, r)), nil
		})
		c.Sleep = func(context.Context, time.Duration) { pauses++ }
		ctx := context.Background()
		if !missing {
			ctx = source.WithSpool(ctx, func() (*os.File, error) { return nil, errors.New("unavailable") })
		}
		e := oshaita.New(c).Scrape(ctx, func([]source.Row) error { batches++; return nil })
		want := 1
		if missing {
			want = 0
		}
		if e == nil || calls != want || batches != 0 || pauses != 0 {
			t.Fatal(e, calls, batches, pauses)
		}
	}
}

// R-NT06-DRK6 R-NU82-RJAV R-4X55-21RM R-4ZKX-TL90 R-NRS9-ZZTH R-4PTQ-RFBG
func TestFailuresStopBeforeNextFile(t *testing.T) {
	for _, mode := range []string{"missing zip", "missing hours", "duplicate id", "duplicate state", "duplicate optional", "empty", "bad header", "short record", "long record", "quote", "empty id", "dot id", "cut body", "half zip", "bare zip", "no csv", "two csv", "unknown compression", "checksum", "403", "404", "batch"} {
		t.Run(mode, func(t *testing.T) {
			h, r := fixture()
			body := csvBytes(h, r)
			path := "a.csv"
			status := 200
			switch mode {
			case "missing zip":
				h[6] = "other"
				body = csvBytes(h, r)
			case "missing hours":
				h[10] = "other"
				body = csvBytes(h, r)
			case "duplicate id":
				h = append(h, "ID")
				r = append(r, "")
				body = csvBytes(h, r)
			case "duplicate state":
				h = append(h, "state")
				r = append(r, "")
				body = csvBytes(h, r)
			case "duplicate optional":
				h = append(h, "NAICS_YEAR")
				r = append(r, "")
				body = csvBytes(h, r)
			case "empty":
				body = nil
			case "bad header":
				body = []byte("\"unterminated")
			case "short record":
				rs := make([][]string, 1001)
				for i := range rs {
					rs[i] = slices.Clone(r)
					rs[i][0] = fmt.Sprint(i)
				}
				rs[1000] = r[:len(r)-1]
				body = csvBytes(h, rs...)
			case "long record":
				body = csvBytes(h, append(r, "extra"))
			case "quote":
				body = append(csvBytes(h), []byte("bad\"quote,x\n")...)
			case "empty id":
				r[0] = ""
				body = csvBytes(h, r)
			case "dot id":
				r[0] = ".00"
				body = csvBytes(h, r)
			case "half zip":
				path = "a.zip"
				body = archive(t, map[string][]byte{"data.csv": body})
				body = body[:len(body)/2]
			case "bare zip":
				path = "a.zip"
			case "no csv":
				path = "a.zip"
				body = archive(t, map[string][]byte{"data.txt": body})
			case "two csv":
				path = "a.zip"
				body = archive(t, map[string][]byte{"data.csv": body, "other.CSV": body})
			case "unknown compression":
				path = "a.zip"
				body = archive(t, map[string][]byte{"data.csv": body})
				for i := 0; i+12 < len(body); i++ {
					if bytes.Equal(body[i:i+4], []byte{'P', 'K', 1, 2}) {
						body[i+10] = 99
						body[i+11] = 0
					}
				}
			case "checksum":
				path = "a.zip"
				body = archive(t, map[string][]byte{"data.csv": body})
				for i := 0; i+20 < len(body); i++ {
					if bytes.Equal(body[i:i+4], []byte{'P', 'K', 1, 2}) {
						body[i+16] ^= 0xff
					}
				}
			case "403":
				status = 403
			case "404":
				status = 404
			}
			ctx, paths := spoolContext(t)
			calls, batches, pauses := 0, 0, 0
			c := cfg(func(*http.Request) (*http.Response, error) {
				calls++
				resp := answer(status, body)
				if mode == "cut body" {
					resp.Body = &brokenBody{Reader: bytes.NewReader(body)}
				}
				return resp, nil
			})
			c.Files = []string{path, "later.csv"}
			nowCalls := 0
			c.Now = func() time.Time { nowCalls++; return clock() }
			c.Sleep = func(context.Context, time.Duration) { pauses++ }
			e := oshaita.New(c).Scrape(ctx, func([]source.Row) error { batches++; return errors.New("refused") })
			wantBatches := 0
			if mode == "batch" {
				wantBatches = 1
			}
			if e == nil || calls != 1 || batches != wantBatches || pauses != 0 || nowCalls != wantBatches {
				t.Fatal(e, calls, batches, pauses, nowCalls)
			}
			if status != 200 && len(*paths) != 0 {
				t.Fatal("non-200 spool calls", len(*paths))
			}
			noFiles(t, *paths)
		})
	}
}

type brokenBody struct{ Reader *bytes.Reader }

func (b *brokenBody) Read(p []byte) (int, error) {
	n, e := b.Reader.Read(p)
	if errors.Is(e, io.EOF) {
		e = io.ErrUnexpectedEOF
	}
	return n, e
}
func (*brokenBody) Close() error { return nil }

// R-4YD1-FTIB R-538M-YWH3 R-4PTQ-RFBG
func TestRetriesAndTelemetry(t *testing.T) {
	for _, status := range []int{0, 429, 500, 599} {
		for _, exhaust := range []bool{false, true} {
			t.Run(fmt.Sprint(status, exhaust), func(t *testing.T) {
				ctx, paths := spoolContext(t)
				ctx = identity.NewContext(ctx, identity.Caller{UserID: "caller", RequestID: "request"})
				var capture telemetry.Capture
				now := clock()
				w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { v := now; now = now.Add(1500 * time.Microsecond); return v }, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
				defer w.Shutdown(context.Background(), "test")
				calls, batches := 0, 0
				var pauses []time.Duration
				h, r := fixture()
				c := cfg(func(*http.Request) (*http.Response, error) {
					if e := w.Flush(ctx); e != nil {
						t.Error(e)
					}
					if len(capture.Events()) != calls {
						t.Error("missing prior event")
					}
					calls++
					if exhaust || calls == 1 {
						if status == 0 {
							return nil, errors.New("transport failure")
						}
						return answer(status, nil), nil
					}
					return answer(200, csvBytes(h, r)), nil
				})
				c.Telemetry = w
				c.Sleep = func(ctx context.Context, d time.Duration) {
					if ctx.Err() != nil {
						t.Error("sleep context")
					}
					pauses = append(pauses, d)
				}
				e := oshaita.New(c).Scrape(ctx, func([]source.Row) error {
					batches++
					if e := w.Flush(ctx); e != nil {
						t.Error(e)
					}
					if len(capture.Events()) != calls {
						t.Error("missing batch event")
					}
					return nil
				})
				wantCalls, wantBatches := 2, 1
				wantPauses := []time.Duration{time.Second}
				if exhaust {
					wantCalls = 4
					wantBatches = 0
					wantPauses = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
				}
				if (e != nil) != exhaust || calls != wantCalls || batches != wantBatches || !reflect.DeepEqual(pauses, wantPauses) {
					t.Fatal(e, calls, batches, pauses)
				}
				if e := w.Flush(ctx); e != nil {
					t.Fatal(e)
				}
				events := capture.Events()
				if len(events) != wantCalls {
					t.Fatal(events)
				}
				for i, event := range events {
					st := status
					if !exhaust && i == 1 {
						st = 200
					}
					want := telemetry.Attrs{"scraper": oshaita.Name, "status": int64(st), "duration_us": int64(1500)}
					if event.Name != source.Called || event.User != "caller" || event.RequestID != "request" || !reflect.DeepEqual(event.Attrs, want) {
						t.Fatal(event)
					}
				}
				if len(*paths) != wantBatches {
					t.Fatal("spool calls", len(*paths), wantBatches)
				}
				noFiles(t, *paths)
			})
		}
	}
}

// R-4VX8-OA0X R-50SU-7CZP
func TestCancellationAndBounds(t *testing.T) {
	for _, mode := range []string{"cancel body", "file bound", "header bound", "batch bound", "late headers", "slow body", "cancel headers", "cancel batch", "cancel retry"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, paths := spoolContext(t)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				h, r := fixture()
				body := csvBytes(h, r)
				calls, batches := 0, 0
				c := cfg(func(req *http.Request) (*http.Response, error) {
					calls++
					if mode == "header bound" || mode == "cancel headers" {
						if mode == "cancel headers" {
							go func() { <-time.After(5 * time.Millisecond); cancel() }()
						}
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					if mode == "cancel retry" {
						return answer(500, nil), nil
					}
					if mode == "late headers" {
						<-time.After(150 * time.Millisecond)
					}
					if mode == "late headers" || mode == "batch bound" || mode == "cancel batch" {
						return answer(200, body), nil
					}
					rd, wr := io.Pipe()
					go func() {
						if mode == "slow body" {
							select {
							case <-time.After(200 * time.Millisecond):
								_, _ = wr.Write(body)
								_ = wr.Close()
								return
							case <-req.Context().Done():
							}
						}
						<-req.Context().Done()
						_ = wr.CloseWithError(req.Context().Err())
					}()
					if mode == "cancel body" {
						go func() { <-time.After(5 * time.Millisecond); cancel() }()
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: rd}, nil
				})
				c.HeaderTimeout = time.Second
				c.FileTimeout = 100 * time.Millisecond
				if mode == "header bound" {
					c.HeaderTimeout = 50 * time.Millisecond
				}
				if mode == "slow body" {
					c.HeaderTimeout = 50 * time.Millisecond
					c.FileTimeout = 0
				}
				var pauses []time.Duration
				c.Sleep = func(ctx context.Context, d time.Duration) {
					pauses = append(pauses, d)
					if mode == "cancel retry" {
						cancel()
						if ctx.Err() == nil {
							t.Error("unlinked sleep context")
						}
					}
				}
				start := time.Now()
				e := oshaita.New(c).Scrape(ctx, func([]source.Row) error {
					batches++
					if ctx.Err() != nil {
						t.Error("batch after cancellation")
					}
					if mode == "batch bound" {
						<-time.After(200 * time.Millisecond)
					}
					if mode == "cancel batch" {
						cancel()
					}
					return nil
				})
				success := mode == "late headers" || mode == "slow body"
				if (e == nil) != success || time.Since(start) > time.Second {
					t.Fatal(e, time.Since(start))
				}
				wantCalls, wantBatches := 1, 0
				if success || mode == "batch bound" || mode == "cancel batch" {
					wantBatches = 1
				}
				if mode == "header bound" {
					wantCalls = 4
				}
				if calls != wantCalls || batches != wantBatches {
					t.Fatal(calls, batches)
				}
				wantPauses := []time.Duration(nil)
				if mode == "header bound" {
					wantPauses = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
				}
				if mode == "cancel retry" {
					wantPauses = []time.Duration{time.Second}
				}
				if !reflect.DeepEqual(pauses, wantPauses) {
					t.Fatal(pauses)
				}
				noFiles(t, *paths)
			})
		})
	}
}

// R-54GJ-CO7S
func TestSourceValuesStayOutOfErrorsAndEvents(t *testing.T) {
	marker := "QrstUvwxYz012345GhijKLMN"
	id := "98765432101234567890"
	for _, mode := range []string{"status", "missing", "duplicate", "short", "cut", "id", "batch", "amendment"} {
		t.Run(mode, func(t *testing.T) {
			h, r := fixture()
			for i := range r {
				r[i] = marker
			}
			r[0] = id
			r[5] = "MN"
			status := 200
			switch mode {
			case "status":
				status = 500
			case "missing":
				h[6] = "other"
			case "duplicate":
				h = append(h, "ID")
				r = append(r, id)
			case "short":
				r = r[:len(r)-1]
			case "id":
				r[0] = ""
			}
			body := csvBytes(h, r)
			if mode == "amendment" {
				body = csvBytes(h, r, r)
			}
			ctx, paths := spoolContext(t)
			var capture telemetry.Capture
			w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: clock, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer w.Shutdown(context.Background(), "test")
			c := cfg(func(*http.Request) (*http.Response, error) {
				resp := answer(status, body)
				if mode == "cut" {
					resp.Body = &brokenBody{Reader: bytes.NewReader(body)}
				}
				return resp, nil
			})
			c.Telemetry = w
			e := oshaita.New(c).Scrape(ctx, func([]source.Row) error {
				if mode == "amendment" {
					return nil
				}
				return errors.New("refused")
			})
			if (e == nil) != (mode == "amendment") {
				t.Fatal(e)
			}
			if e != nil && (strings.Contains(e.Error(), marker) || strings.Contains(e.Error(), id)) {
				t.Fatal(e)
			}
			if e := w.Flush(ctx); e != nil {
				t.Fatal(e)
			}
			for _, event := range capture.Events() {
				for _, v := range event.Attrs {
					if strings.Contains(fmt.Sprint(v), marker) || strings.Contains(fmt.Sprint(v), id) {
						t.Fatal(event)
					}
				}
			}
			noFiles(t, *paths)
		})
	}
}

// R-NQKD-M82S R-NRS9-ZZTH R-NT06-DRK6 R-NU82-RJAV
func TestAcceptedFileShapes(t *testing.T) {
	for _, mode := range []string{"header only", "required only", "reverse columns", "unknown columns", "quoted BOM", "quoted BOM zip", "no final newline", "companion before", "companion after"} {
		t.Run(mode, func(t *testing.T) {
			h, r := fixture()
			path := "a.csv"
			switch mode {
			case "required only":
				h = h[:len(oshaita.Columns())]
				r = r[:len(h)]
			case "reverse columns":
				slices.Reverse(h)
				slices.Reverse(r)
			case "unknown columns":
				h = append([]string{"before"}, h...)
				r = append([]string{"unknown"}, r...)
				h = append(h, "after")
				r = append(r, "unknown")
			}
			body := csvBytes(h, r)
			if mode == "header only" {
				body = csvBytes(h)
			}
			if mode == "no final newline" {
				body = bytes.TrimSuffix(body, []byte("\r\n"))
			}
			if strings.HasPrefix(mode, "quoted BOM") {
				body = append([]byte{0xef, 0xbb, 0xbf}, append([]byte("\"id\""), body[2:]...)...)
			}
			if mode == "quoted BOM zip" {
				path = "a.ZIP"
				body = archive(t, map[string][]byte{"data.csv": body})
			}
			if strings.HasPrefix(mode, "companion") {
				path = "a.zip"
				var b bytes.Buffer
				z := zip.NewWriter(&b)
				entries := []string{"extra.txt", "data.csv"}
				if mode == "companion after" {
					slices.Reverse(entries)
				}
				for _, name := range entries {
					w, e := z.Create(name)
					if e != nil {
						t.Fatal(e)
					}
					v := []byte("companion")
					if name == "data.csv" {
						v = body
					}
					if _, e = w.Write(v); e != nil {
						t.Fatal(e)
					}
				}
				if e := z.Close(); e != nil {
					t.Fatal(e)
				}
				body = b.Bytes()
			}
			ctx, paths := spoolContext(t)
			c := cfg(func(*http.Request) (*http.Response, error) { return answer(200, body), nil })
			c.Files = []string{path}
			batches := 0
			e := oshaita.New(c).Scrape(ctx, func(rs []source.Row) error {
				batches++
				want, err := oshaita.RowOf(h, r, clock())
				if err != nil || len(rs) != 1 || !reflect.DeepEqual(rs[0], want) {
					t.Error(rs, want, err)
				}
				return nil
			})
			wantBatches := 1
			if mode == "header only" {
				wantBatches = 0
			}
			if e != nil || batches != wantBatches {
				t.Fatal(e, batches)
			}
			noFiles(t, *paths)
		})
	}
}

// R-4EUN-BHN7 R-4M61-M43D R-520Q-L4QE R-538M-YWH3
func TestDefaultClientRedirectsAndEmptyCaller(t *testing.T) {
	h, r := fixture()
	ctx, paths := spoolContext(t)
	var capture telemetry.Capture
	w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: clock, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer w.Shutdown(context.Background(), "test")
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(out http.ResponseWriter, req *http.Request) {
		requests = append(requests, req.URL.Path)
		if req.Header.Get("User-Agent") != oshaita.UserAgent {
			t.Error(req.Header)
		}
		if req.URL.Path == "/a.csv" {
			http.Redirect(out, req, "/redirected.csv", http.StatusFound)
			return
		}
		_, _ = out.Write(csvBytes(h, r))
	}))
	defer srv.Close()
	c := oshaita.Config{BaseURL: srv.URL + "/", Telemetry: w, Now: clock, Files: []string{"a.csv", "b.csv"}, States: []string{"MN"}}
	batches := 0
	e := oshaita.New(c).Scrape(ctx, func([]source.Row) error { batches++; return nil })
	if e != nil || batches != 2 || !reflect.DeepEqual(requests, []string{"/a.csv", "/redirected.csv", "/b.csv"}) {
		t.Fatal(e, batches, requests)
	}
	if e := w.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	events := capture.Events()
	if len(events) != 2 {
		t.Fatal(events)
	}
	for _, event := range events {
		if event.RequestID != "" || event.User != "" || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": oshaita.Name, "status": int64(200), "duration_us": int64(0)}) {
			t.Fatal(event)
		}
	}
	noFiles(t, *paths)
}

// R-4EUN-BHN7 R-4YD1-FTIB R-50SU-7CZP
func TestDefaultURLAndPause(t *testing.T) {
	for _, cancelPause := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelPause), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, paths := spoolContext(t)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				start := time.Now()
				var sent []time.Duration
				c := cfg(func(req *http.Request) (*http.Response, error) {
					if req.URL.String() != oshaita.DefaultBaseURL+"files/a%20b.zip" {
						t.Error(req.URL.String())
					}
					sent = append(sent, time.Since(start))
					if cancelPause {
						go func() { <-time.After(100 * time.Millisecond); cancel() }()
					}
					return answer(500, nil), nil
				})
				c.Files = []string{"files/a b.zip"}
				c.Sleep = nil
				e := oshaita.New(c).Scrape(ctx, func([]source.Row) error { t.Error("batch"); return nil })
				want := []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second}
				elapsed := 7 * time.Second
				if cancelPause {
					want = []time.Duration{0}
					elapsed = 100 * time.Millisecond
				}
				if e == nil || !reflect.DeepEqual(sent, want) || time.Since(start) != elapsed {
					t.Fatal(e, sent, time.Since(start))
				}
				if len(*paths) != 0 {
					t.Fatal("non-200 spool calls", len(*paths))
				}
				noFiles(t, *paths)
			})
		})
	}
}

// R-4EUN-BHN7 R-4VX8-OA0X R-4YD1-FTIB
func TestDefaultTimeoutBoundaries(t *testing.T) {
	for _, mode := range []string{"header within", "header bound", "file within", "file bound"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, paths := spoolContext(t)
				h, r := fixture()
				body := csvBytes(h, r)
				calls := 0
				c := cfg(func(req *http.Request) (*http.Response, error) {
					calls++
					switch mode {
					case "header within":
						<-time.After(oshaita.HeaderTimeout - time.Second)
						return answer(200, body), nil
					case "header bound":
						<-req.Context().Done()
						return nil, req.Context().Err()
					default:
						rd, wr := io.Pipe()
						go func() {
							if mode == "file within" {
								select {
								case <-time.After(oshaita.FileTimeout - time.Second):
									_, _ = wr.Write(body)
									_ = wr.Close()
									return
								case <-req.Context().Done():
								}
							} else {
								<-req.Context().Done()
							}
							_ = wr.CloseWithError(req.Context().Err())
						}()
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: rd}, nil
					}
				})
				start := time.Now()
				batches := 0
				e := oshaita.New(c).Scrape(ctx, func([]source.Row) error { batches++; return nil })
				success := strings.HasSuffix(mode, "within")
				wantCalls, wantBatches := 1, 0
				var elapsed time.Duration
				switch mode {
				case "header within":
					elapsed = oshaita.HeaderTimeout - time.Second
				case "header bound":
					elapsed = oshaita.HeaderTimeout * (oshaita.Retries + 1)
					wantCalls = oshaita.Retries + 1
				case "file within":
					elapsed = oshaita.FileTimeout - time.Second
				case "file bound":
					elapsed = oshaita.FileTimeout
				}
				if success {
					wantBatches = 1
				}
				if (e == nil) != success || calls != wantCalls || batches != wantBatches || time.Since(start) != elapsed {
					t.Fatal(e, calls, batches, time.Since(start), elapsed)
				}
				wantSpools := 1
				if mode == "header bound" {
					wantSpools = 0
				}
				if len(*paths) != wantSpools {
					t.Fatal("spool calls", len(*paths), wantSpools)
				}
				noFiles(t, *paths)
			})
		})
	}
}

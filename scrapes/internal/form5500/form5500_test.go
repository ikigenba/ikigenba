package form5500_test

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

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/form5500"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

func clock() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 789, time.FixedZone("test", 3600)) }
func fixture(f form5500.Form) ([]string, []string) {
	h := form5500.Columns(f)
	r := make([]string, len(h))
	for i := range r {
		r[i] = fmt.Sprintf("value-%d", i)
	}
	put(h, r, "ACK_ID", "12345678901234567890")
	if f == form5500.Full {
		put(h, r, "SPONS_DFE_MAIL_US_STATE", "MN")
	} else {
		put(h, r, "SF_SPONS_US_STATE", "MN")
	}
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
func zipped(t *testing.T, f form5500.Form) []byte {
	h, r := fixture(f)
	return archive(t, map[string][]byte{"filing.CSV": csvBytes(h, r)})
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
func cfg(fn transport) form5500.Config {
	return form5500.Config{HTTP: &http.Client{Transport: fn}, Now: clock, Sleep: func(context.Context, time.Duration) {}, Years: []string{"2024"}, States: []string{"MN", "WI"}, UserAgent: "fixture ua"}
}
func answer(status int, b []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(b))}
}

// R-L02C-3PBF R-L1A8-HH24 R-L2I4-V8ST
func TestAPI(t *testing.T) {
	if form5500.Name != "form5500" || form5500.DefaultBaseURL != "https://askebsa.dol.gov/FOIA%20Files/" || form5500.BatchSize != 1000 || form5500.Retries != 3 || form5500.Backoff != time.Second || form5500.HeaderTimeout != 60*time.Second || form5500.FileTimeout != 30*time.Minute || form5500.SponsorSignerRole != "sponsor signer" || form5500.AdminSignerRole != "administrator signer" || form5500.Full != "5500" || form5500.Short != "5500-SF" {
		t.Fatal("constants")
	}
	var sc source.Scraper = form5500.New(form5500.Config{})
	if sc.Name() != form5500.Name {
		t.Fatal(sc.Name())
	}
	ctx, _ := spoolContext(t)
	if err := sc.Scrape(ctx, func([]source.Row) error { t.Fatal("batch"); return nil }); err != nil {
		t.Fatal(err)
	}
}

// R-L3Q1-90JI
func TestColumns(t *testing.T) {
	for _, tc := range []struct {
		f    form5500.Form
		want string
	}{
		{form5500.Full, `ACK_ID FORM_PLAN_YEAR_BEGIN_DATE FORM_TAX_PRD TYPE_PLAN_ENTITY_CD INITIAL_FILING_IND AMENDED_IND FINAL_FILING_IND SHORT_PLAN_YR_IND COLLECTIVE_BARGAIN_IND PLAN_NAME SPONS_DFE_PN PLAN_EFF_DATE SPONSOR_DFE_NAME SPONS_DFE_DBA_NAME SPONS_DFE_CARE_OF_NAME SPONS_DFE_MAIL_US_ADDRESS1 SPONS_DFE_MAIL_US_ADDRESS2 SPONS_DFE_MAIL_US_CITY SPONS_DFE_MAIL_US_STATE SPONS_DFE_MAIL_US_ZIP SPONS_DFE_LOC_US_ADDRESS1 SPONS_DFE_LOC_US_ADDRESS2 SPONS_DFE_LOC_US_CITY SPONS_DFE_LOC_US_STATE SPONS_DFE_LOC_US_ZIP SPONS_DFE_MAIL_FOREIGN_CNTRY SPONS_DFE_EIN SPONS_DFE_PHONE_NUM BUSINESS_CODE ADMIN_NAME ADMIN_EIN LAST_RPT_SPONS_NAME LAST_RPT_SPONS_EIN SPONS_SIGNED_NAME SPONS_SIGNED_DATE ADMIN_SIGNED_NAME ADMIN_SIGNED_DATE TOT_PARTCP_BOY_CNT TOT_ACT_PARTCP_BOY_CNT TOT_ACTIVE_PARTCP_CNT TYPE_PENSION_BNFT_CODE TYPE_WELFARE_BNFT_CODE DATE_RECEIVED FILING_STATUS PREPARER_FIRM_NAME`},
		{form5500.Short, `ACK_ID SF_PLAN_YEAR_BEGIN_DATE SF_TAX_PRD SF_PLAN_ENTITY_CD SF_INITIAL_FILING_IND SF_AMENDED_IND SF_FINAL_FILING_IND SF_SHORT_PLAN_YR_IND SF_PLAN_NAME SF_PLAN_NUM SF_PLAN_EFF_DATE SF_SPONSOR_NAME SF_SPONSOR_DFE_DBA_NAME SF_SPONS_CARE_OF_NAME SF_SPONS_US_ADDRESS1 SF_SPONS_US_ADDRESS2 SF_SPONS_US_CITY SF_SPONS_US_STATE SF_SPONS_US_ZIP SF_SPONS_LOC_US_ADDRESS1 SF_SPONS_LOC_US_ADDRESS2 SF_SPONS_LOC_US_CITY SF_SPONS_LOC_US_STATE SF_SPONS_LOC_US_ZIP SF_SPONS_FOREIGN_CNTRY SF_SPONS_EIN SF_SPONS_PHONE_NUM SF_BUSINESS_CODE SF_ADMIN_NAME SF_ADMIN_EIN SF_LAST_RPT_SPONS_NAME SF_LAST_RPT_SPONS_EIN SF_SPONS_SIGNED_NAME SF_SPONS_SIGNED_DATE SF_ADMIN_SIGNED_NAME SF_ADMIN_SIGNED_DATE SF_TOT_PARTCP_BOY_CNT SF_TOT_ACT_PARTCP_BOY_CNT SF_TOT_ACT_PARTCP_EOY_CNT SF_TYPE_PENSION_BNFT_CODE SF_TYPE_WELFARE_BNFT_CODE SF_EMPLR_CONTRIB_INCOME_AMT SF_PARTICIP_CONTRIB_INCOME_AMT SF_NET_ASSETS_EOY_AMT DATE_RECEIVED FILING_STATUS SF_PREPARER_FIRM_NAME`}} {
		want := strings.Fields(tc.want)
		got := form5500.Columns(tc.f)
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
		got[0] = "mutated"
		if !reflect.DeepEqual(form5500.Columns(tc.f), want) {
			t.Fatal("shared columns")
		}
	}
	if got := form5500.Columns("unknown"); got == nil || len(got) != 0 {
		t.Fatal(got)
	}
}

// R-XFPR-JZLA R-L65U-0K0W R-L8LM-S3IA R-L9TJ-5V8Z R-LKSM-LSX8
func TestWalk(t *testing.T) {
	ctx, paths := spoolContext(t)
	var requests []string
	var batches [][]source.Row
	var bodies [][]byte
	type input struct {
		f       form5500.Form
		h       []string
		records [][]string
	}
	var inputs []input
	nowCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.Method != "GET" || r.Header.Get("User-Agent") != "fixture ua" || r.Body != http.NoBody && r.ContentLength != 0 {
			t.Error("request shape")
		}
		for _, k := range []string{"Authorization", "Cookie", "Range"} {
			if r.Header.Get(k) != "" {
				t.Error(k)
			}
		}
		if !strings.HasPrefix(r.URL.Path, "/redirect/") {
			noFiles(t, *paths)
			w.Header().Set("Location", "/redirect"+r.URL.Path)
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		f := form5500.Full
		if strings.Contains(r.URL.Path, "_SF_") {
			f = form5500.Short
		}
		h, r0 := fixture(f)
		h = append([]string{"extra"}, h...)
		r0 = append([]string{"A<B, quoted"}, r0...)
		var records [][]string
		for i := range 2540 {
			r := slices.Clone(r0)
			r[0] = fmt.Sprintf("source-record-%d", i)
			put(h, r, "ACK_ID", fmt.Sprintf("%020d", i))
			if i == 41 {
				put(h, r, "ACK_ID", fmt.Sprintf("%020d", 40))
			}
			state := "mn"
			if i < 40 {
				state = "IA"
				put(h, r, "ACK_ID", "")
			}
			if f == form5500.Full {
				put(h, r, "SPONS_DFE_MAIL_US_STATE", state)
				put(h, r, "SPONS_DFE_LOC_US_STATE", "")
			} else {
				put(h, r, "SF_SPONS_US_STATE", state)
				put(h, r, "SF_SPONS_LOC_US_STATE", "")
			}
			records = append(records, r)
		}
		b := archive(t, map[string][]byte{"data.csv": csvBytes(h, records...)})
		bodies = append(bodies, b)
		inputs = append(inputs, input{f, h, records})
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	c := form5500.Config{HTTP: srv.Client(), BaseURL: srv.URL + "/", Now: func() time.Time { nowCalls++; return clock().Add(time.Duration(nowCalls) * time.Second) }, Years: []string{"2024", "2019"}, States: []string{"MN"}, UserAgent: "fixture ua"}
	err := form5500.New(c).Scrape(ctx, func(rows []source.Row) error {
		batches = append(batches, rows)
		b, err := os.ReadFile((*paths)[len(*paths)-1])
		if err != nil || !bytes.Equal(b, bodies[len(bodies)-1]) {
			t.Error("spool bytes")
		}

		in := inputs[len(inputs)-1]
		stamp := clock().Add(time.Duration(len(batches)) * time.Second)
		if nowCalls != len(batches) {
			t.Errorf("Now calls %d for %d batches", nowCalls, len(batches))
		}
		offset := 40 + ((len(batches)-1)%3)*form5500.BatchSize
		for i, row := range rows {
			want, e := form5500.RowOf(in.f, in.h, in.records[offset+i], stamp)
			if e != nil || !reflect.DeepEqual(row, want) {
				t.Errorf("batch %d row %d differs from ordered RowOf: %v", len(batches), i, e)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 8 || len(*paths) != 4 || len(batches) != 12 {
		t.Fatal(len(requests), len(*paths), len(batches))
	}
	for i, year := range []string{"2024", "2019"} {
		for j, suffix := range []string{"", "SF_"} {
			p := "/" + year + "/Latest/F_5500_" + suffix + year + "_Latest.zip"
			if requests[i*4+j*2] != p || requests[i*4+j*2+1] != "/redirect"+p {
				t.Fatal(requests)
			}
		}
	}
	for i, b := range batches {
		want := 1000
		if i%3 == 2 {
			want = 500
		}
		if len(b) != want {
			t.Fatal(len(b))
		}
	}
	noFiles(t, *paths)
}

// R-XFPR-JZLA R-XJDG-PATD
func TestArchiveLayoutFiles(t *testing.T) {
	collect := func(t *testing.T, names []string) [][]source.Row {
		t.Helper()
		ctx, paths := spoolContext(t)
		c := cfg(func(req *http.Request) (*http.Response, error) {
			f := form5500.Full
			if strings.Contains(req.URL.Path, "_SF_") {
				f = form5500.Short
			}
			h, r := fixture(f)
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			for _, name := range names {
				e, err := w.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				data := []byte("layout documentation, not a CSV record\n")
				if strings.HasSuffix(strings.ToLower(name), ".csv") {
					data = csvBytes(h, r)
				}
				if _, err := e.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			return answer(200, b.Bytes()), nil
		})
		var batches [][]source.Row
		if err := form5500.New(c).Scrape(ctx, func(rows []source.Row) error {
			batches = append(batches, rows)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		noFiles(t, *paths)
		return batches
	}
	baseline := collect(t, []string{"f_5500_2024_latest.csv"})
	var want [][]source.Row
	for _, f := range []form5500.Form{form5500.Full, form5500.Short} {
		h, r := fixture(f)
		row, err := form5500.RowOf(f, h, r, clock())
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, []source.Row{row})
	}
	if !reflect.DeepEqual(baseline, want) {
		t.Fatal("CSV alone did not yield both forms' rows")
	}
	for _, suffix := range []string{"csv", "CSV", "CsV"} {
		csvName := "f_5500_2024_latest." + suffix
		layoutName := "f_5500_2024_latest_layout.txt"
		for _, names := range [][]string{{csvName}, {csvName, layoutName}, {layoutName, csvName}} {
			t.Run(strings.Join(names, "+"), func(t *testing.T) {
				if got := collect(t, names); !reflect.DeepEqual(got, baseline) {
					t.Fatal("archive entries changed the batches")
				}
			})
		}
	}
}

// R-LB1F-JMZO R-LC9B-XEQD R-XJDG-PATD R-LICT-U9FU R-LFX1-2PYG
func TestFailures(t *testing.T) {
	h, r := fixture(form5500.Full)
	for _, name := range []string{"missing spool", "failed spool", "404", "bad zip", "zero entries", "two CSV entries", "not csv", "missing column", "duplicate column", "empty header", "bad header", "short record", "empty identifier", "refused batch", "cut body"} {
		t.Run(name, func(t *testing.T) {
			ctx, paths := spoolContext(t)
			calls, batches, sleeps := 0, 0, 0
			body := zipped(t, form5500.Full)
			status := 200
			switch name {
			case "missing spool":
				ctx = context.Background()
			case "failed spool":
				ctx = source.WithSpool(ctx, func() (*os.File, error) { return nil, errors.New("failed") })
			case "404":
				status = 404
			case "bad zip":
				body = []byte("invalid")
			case "zero entries":
				body = archive(t, map[string][]byte{})
			case "two CSV entries":
				body = archive(t, map[string][]byte{"a.csv": csvBytes(h, r), "b.CSV": csvBytes(h, r)})
			case "not csv":
				body = archive(t, map[string][]byte{"f_5500_2024_latest_layout.txt": csvBytes(h, r)})
			case "missing column":
				body = archive(t, map[string][]byte{"a.csv": csvBytes(h[:len(h)-1], r[:len(r)-1])})
			case "duplicate column":
				body = archive(t, map[string][]byte{"a.csv": csvBytes(append(slices.Clone(h), h[0]), append(slices.Clone(r), r[0]))})
			case "empty header":
				body = archive(t, map[string][]byte{"a.csv": nil})
			case "bad header":
				body = archive(t, map[string][]byte{"a.csv": []byte("bad\"header\n")})
			case "short record":
				body = archive(t, map[string][]byte{"a.csv": csvBytes(h, r[:len(r)-1])})
			case "empty identifier":
				rr := slices.Clone(r)
				rr[0] = ""
				body = archive(t, map[string][]byte{"a.csv": csvBytes(h, rr)})
			case "cut body":
				body = body[:len(body)/2]
			}
			c := cfg(func(*http.Request) (*http.Response, error) { calls++; return answer(status, body), nil })
			c.Sleep = func(context.Context, time.Duration) { sleeps++ }
			err := form5500.New(c).Scrape(ctx, func([]source.Row) error { batches++; return errors.New("refused") })
			if err == nil || sleeps != 0 {
				t.Fatal(err, sleeps)
			}
			want := 1
			if name == "missing spool" {
				want = 0
			}
			if calls != want {
				t.Fatal(calls)
			}
			if name != "refused batch" && batches != 0 {
				t.Fatal(batches)
			}
			noFiles(t, *paths)
		})
	}
}

// R-LH4X-GHP5 R-LM0I-ZKNX
func TestRetriesAndTelemetry(t *testing.T) {
	for _, status := range []int{0, 429, 500, 599} {
		for _, exhaust := range []bool{false, true} {
			t.Run(fmt.Sprint(status, exhaust), func(t *testing.T) {
				ctx, paths := spoolContext(t)
				ctx = identity.NewContext(ctx, identity.Caller{UserID: "caller", RequestID: "request"})
				var capture telemetry.Capture
				now := clock()
				w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { v := now; now = now.Add(1500 * time.Microsecond); return v }, Rand: bytes.NewReader(make([]byte, 2048)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
				defer w.Shutdown(context.Background(), "test")
				calls := 0
				var pauses []time.Duration
				c := cfg(func(req *http.Request) (*http.Response, error) {
					if err := w.Flush(ctx); err != nil {
						t.Error(err)
					}
					if len(capture.Events()) != calls {
						t.Error("missing previous event")
					}
					calls++
					if exhaust || calls == 1 {
						if status == 0 {
							return nil, errors.New("transport failure")
						}
						return answer(status, nil), nil
					}
					f := form5500.Full
					if strings.Contains(req.URL.Path, "_SF_") {
						f = form5500.Short
					}
					return answer(200, zipped(t, f)), nil
				})
				c.Telemetry = w
				c.Sleep = func(ctx context.Context, d time.Duration) {
					if ctx.Err() != nil {
						t.Error("sleep context")
					}
					pauses = append(pauses, d)
				}
				err := form5500.New(c).Scrape(ctx, func([]source.Row) error {
					if err := w.Flush(ctx); err != nil {
						t.Error(err)
					}
					if len(capture.Events()) != calls {
						t.Error("missing batch event")
					}
					return nil
				})
				want := 3
				expected := []time.Duration{time.Second}
				if exhaust {
					want = 4
					expected = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
				}
				if (err != nil) != exhaust || calls != want || !reflect.DeepEqual(pauses, expected) {
					t.Fatal(err, calls, pauses)
				}
				if err := w.Flush(ctx); err != nil {
					t.Fatal(err)
				}
				es := capture.Events()
				if len(es) != want {
					t.Fatal(es)
				}
				for i, e := range es {
					st := int64(200)
					if exhaust || i == 0 {
						st = int64(status)
					}
					if e.Name != source.Called || e.RequestID != "request" || e.User != "caller" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"scraper": form5500.Name, "status": st, "duration_us": int64(1500)}) {
						t.Fatal(e)
					}
				}
				noFiles(t, *paths)
			})
		}
	}
}

// R-LOGB-R45B R-LPO8-4VW0 R-LS40-WFDE R-LTBX-A743
func TestRows(t *testing.T) {
	keys := strings.Fields(`ack_id plan_year_begin plan_year_end entity initial_filing amended final_filing short_plan_year collectively_bargained plan_name plan_number plan_effective sponsor_name dba care_of mail_street mail_city mail_state mail_zip loc_street loc_city loc_state loc_zip foreign_country ein phone business_code admin_name admin_ein last_sponsor_name last_sponsor_ein sponsor_signed_name sponsor_signed_date admin_signed_name admin_signed_date participants_boy active_participants_boy active_participants_eoy pension_codes welfare_codes employer_contributions participant_contributions net_assets_eoy received filing_status preparer_firm`)
	for _, f := range []form5500.Form{form5500.Full, form5500.Short} {
		t.Run(string(f), func(t *testing.T) {
			h, r := fixture(f)
			h = append(h, "COLLECTIVELY_BARGAINED", "unknown")
			r = append(r, "Y", "A<B\"x, y\"")
			sponsor, dba, mail1, mail2, loc1, loc2, loccity, locstate, loczip, ss, as := "SPONSOR_DFE_NAME", "SPONS_DFE_DBA_NAME", "SPONS_DFE_MAIL_US_ADDRESS1", "SPONS_DFE_MAIL_US_ADDRESS2", "SPONS_DFE_LOC_US_ADDRESS1", "SPONS_DFE_LOC_US_ADDRESS2", "SPONS_DFE_LOC_US_CITY", "SPONS_DFE_LOC_US_STATE", "SPONS_DFE_LOC_US_ZIP", "SPONS_SIGNED_NAME", "ADMIN_SIGNED_NAME"
			if f == form5500.Short {
				sponsor, dba, mail1, mail2, loc1, loc2, loccity, locstate, loczip, ss, as = "SF_SPONSOR_NAME", "SF_SPONSOR_DFE_DBA_NAME", "SF_SPONS_US_ADDRESS1", "SF_SPONS_US_ADDRESS2", "SF_SPONS_LOC_US_ADDRESS1", "SF_SPONS_LOC_US_ADDRESS2", "SF_SPONS_LOC_US_CITY", "SF_SPONS_LOC_US_STATE", "SF_SPONS_LOC_US_ZIP", "SF_SPONS_SIGNED_NAME", "SF_ADMIN_SIGNED_NAME"
			}
			for k, v := range map[string]string{sponsor: "  Example DBA Whole Name  ", dba: "Other name", mail1: "  1 Mail ", mail2: " STE 3 ", loc1: " 100 MAIN ST ", loc2: "STE 2", loccity: "BIG LAKE", locstate: "mn", loczip: "563531234", ss: " Jane Doe ", as: "John Smith"} {
				put(h, r, k, v)
			}
			row, err := form5500.RowOf(f, h, r, clock())
			if err != nil {
				t.Fatal(err)
			}
			var raw bytes.Buffer
			raw.WriteByte('{')
			for i, c := range h {
				if i > 0 {
					raw.WriteByte(',')
				}
				kb, _ := json.Marshal(c)
				vb, _ := json.Marshal(r[i])
				raw.Write(kb)
				raw.WriteByte(':')
				raw.Write(vb)
			}
			raw.WriteByte('}')
			if !bytes.Equal(row.Raw, raw.Bytes()) || row.Digest != source.Digest(raw.Bytes()) || row.Scraper != form5500.Name || row.Run != "" || row.SourceID != r[0] || row.Scraped != clock().UTC().Truncate(time.Second) || row.Lat != nil || row.Lon != nil || row.Name != "Example DBA Whole Name" || !reflect.DeepEqual(row.Names, []string{"Example DBA Whole Name", "Other name"}) || row.Address != source.NewAddress("100 MAIN ST STE 2", "BIG LAKE", "mn", "563531234", "") || !reflect.DeepEqual(row.People, []source.Person{{Name: "Jane Doe", Role: form5500.SponsorSignerRole}, {Name: "John Smith", Role: form5500.AdminSignerRole}}) {
				t.Fatal(row)
			}
			// Independently pair each fact with the contract's columns, joining only address lines.
			cols := form5500.Columns(f)
			if f == form5500.Short {
				cols = append(cols[:8], append([]string{"COLLECTIVELY_BARGAINED"}, cols[8:]...)...)
			}
			expected := map[string]string{"form": string(f)}
			at := 0
			for _, key := range keys {
				if f == form5500.Full && (key == "employer_contributions" || key == "participant_contributions" || key == "net_assets_eoy") {
					expected[key] = ""
					continue
				}
				expected[key] = r[slices.Index(h, cols[at])]
				at++
				if key == "mail_street" || key == "loc_street" {
					expected[key] = strings.TrimSpace(expected[key]) + " " + strings.TrimSpace(r[slices.Index(h, cols[at])])
					at++
				}
			}
			wantFacts, _ := json.Marshal(expected)
			if !bytes.Equal(row.Facts, wantFacts) {
				t.Fatal(string(row.Facts), string(wantFacts))
			}
			put(h, r, locstate, "")
			put(h, r, as, "jane doe")
			put(h, r, dba, "Example DBA Whole Name")
			row, err = form5500.RowOf(f, h, r, clock())
			if err != nil || row.Address.Street != "1 Mail STE 3" || len(row.People) != 1 || len(row.Names) != 1 {
				t.Fatal(row, err)
			}
			put(h, r, ss, "ſam")
			put(h, r, as, "Sam")
			row, err = form5500.RowOf(f, h, r, clock())
			if err != nil || !reflect.DeepEqual(row.People, []source.Person{{Name: "ſam", Role: form5500.SponsorSignerRole}, {Name: "Sam", Role: form5500.AdminSignerRole}}) {
				t.Fatal(row.People, err)
			}

			for _, bad := range []struct {
				f    form5500.Form
				h, r []string
			}{{"invalid", h, r}, {f, h, r[:len(r)-1]}, {f, h, append(slices.Clone(r), "extra")}, {f, h, nil}, {f, append(slices.Clone(h), h[0]), append(slices.Clone(r), "duplicate")}, {f, h[1:], r[1:]}} {
				v, e := form5500.RowOf(bad.f, bad.h, bad.r, clock())
				if e == nil || !reflect.DeepEqual(v, source.Row{}) {
					t.Fatal(v, e)
				}
			}
			r[0] = ""
			v, e := form5500.RowOf(f, h, r, clock())
			if e == nil || !reflect.DeepEqual(v, source.Row{}) {
				t.Fatal(v, e)
			}
		})
	}
}

// R-LEP4-OY7R R-LJKQ-816J
func TestCancellationAndBounds(t *testing.T) {
	for _, mode := range []string{"cancel body", "file bound", "header bound", "batch bound", "late headers", "slow body"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, paths := spoolContext(t)
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				bodies := map[form5500.Form][]byte{form5500.Full: zipped(t, form5500.Full), form5500.Short: zipped(t, form5500.Short)}
				calls, batches := 0, 0
				c := cfg(func(r *http.Request) (*http.Response, error) {
					calls++
					if mode == "header bound" {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					if mode == "late headers" {
						<-time.After(150 * time.Millisecond)
					}
					f := form5500.Full
					if strings.Contains(r.URL.Path, "_SF_") {
						f = form5500.Short
					}
					if mode == "late headers" || mode == "batch bound" {
						return answer(200, bodies[f]), nil
					}
					rd, wr := io.Pipe()
					go func() {
						if mode == "slow body" {
							select {
							case <-time.After(200 * time.Millisecond):
								_, _ = wr.Write(bodies[f])
								_ = wr.Close()
								return
							case <-r.Context().Done():
							}
						}
						<-r.Context().Done()
						_ = wr.CloseWithError(r.Context().Err())
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
				c.Sleep = func(_ context.Context, d time.Duration) { pauses = append(pauses, d) }
				start := time.Now()
				err := form5500.New(c).Scrape(ctx, func([]source.Row) error {
					batches++
					if mode == "batch bound" {
						<-time.After(200 * time.Millisecond)
					}
					return nil
				})
				success := mode == "late headers" || mode == "slow body"
				if (err == nil) != success || time.Since(start) > time.Second {
					t.Fatal(err, time.Since(start))
				}
				wantCalls, wantBatches := 1, 0
				if success {
					wantCalls, wantBatches = 2, 2
				}
				if mode == "batch bound" {
					wantBatches = 1
				}
				if mode == "header bound" {
					wantCalls = 4
				}
				if calls != wantCalls || batches != wantBatches {
					t.Fatal(calls, batches)
				}
				if mode == "header bound" {
					if !reflect.DeepEqual(pauses, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
						t.Fatal(pauses)
					}
				} else if len(pauses) != 0 {
					t.Fatal(pauses)
				}
				if mode == "file bound" && time.Since(start) != 100*time.Millisecond {
					t.Fatal("file bound", time.Since(start))
				}
				noFiles(t, *paths)
			})
		})
	}
}

// R-LN8F-DCEM
func TestFailureSecrecy(t *testing.T) {
	marker := "GhijkLMnOpQRstUVwxYZ1234"
	id := "98765432109876543210"
	for _, mode := range []string{"status", "header", "record", "identifier", "zip", "batch"} {
		t.Run(mode, func(t *testing.T) {
			h, r := fixture(form5500.Full)
			for i := range r {
				r[i] = marker
			}
			put(h, r, "ACK_ID", id)
			put(h, r, "SPONS_DFE_MAIL_US_STATE", "MN")
			put(h, r, "SPONS_DFE_LOC_US_STATE", "")
			status := 200
			switch mode {
			case "status":
				status = 500
			case "header":
				h[len(h)-1] = "other"
			case "record":
				r = r[:len(r)-1]
			case "identifier":
				r[0] = ""
			}
			body := archive(t, map[string][]byte{"data.csv": csvBytes(h, r)})
			if mode == "zip" {
				body = append([]byte(marker+id), body[:len(body)/2]...)
			}
			ctx, paths := spoolContext(t)
			var capture telemetry.Capture
			w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: clock, Rand: bytes.NewReader(make([]byte, 2048)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer w.Shutdown(context.Background(), "test")
			c := cfg(func(*http.Request) (*http.Response, error) { return answer(status, body), nil })
			c.Telemetry = w
			err := form5500.New(c).Scrape(ctx, func([]source.Row) error { return errors.New("refused") })
			if err == nil || strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), id) {
				t.Fatal(err)
			}
			if e := w.Flush(ctx); e != nil {
				t.Fatal(e)
			}
			for _, e := range capture.Events() {
				for _, v := range e.Attrs {
					text := fmt.Sprint(v)
					if strings.Contains(text, marker) || strings.Contains(text, id) {
						t.Fatal(e)
					}
				}
			}
			noFiles(t, *paths)
		})
	}
}

// R-L8LM-S3IA R-LC9B-XEQD R-XJDG-PATD
func TestScopeEmptyAndStreamingFailure(t *testing.T) {
	for _, mode := range []string{"empty", "location", "foreign", "exact batch", "partial failure", "reordered"} {
		t.Run(mode, func(t *testing.T) {
			ctx, paths := spoolContext(t)
			calls, batches, rows := 0, 0, 0
			c := cfg(func(req *http.Request) (*http.Response, error) {
				calls++
				f := form5500.Full
				if strings.Contains(req.URL.Path, "_SF_") {
					f = form5500.Short
				}
				h, r := fixture(f)
				mail, loc := "SPONS_DFE_MAIL_US_STATE", "SPONS_DFE_LOC_US_STATE"
				if f == form5500.Short {
					mail, loc = "SF_SPONS_US_STATE", "SF_SPONS_LOC_US_STATE"
				}
				put(h, r, mail, "IA")
				put(h, r, loc, "mn")
				var records [][]string
				switch mode {
				case "empty":
				case "location":
					records = [][]string{r}
				case "foreign":
					put(h, r, mail, "")
					put(h, r, loc, "")
					r[0] = ""
					records = [][]string{r}
				case "exact batch", "partial failure":
					for range form5500.BatchSize {
						records = append(records, slices.Clone(r))
					}
					if mode == "partial failure" {
						records = append(records, r[:len(r)-1])
					}
				case "reordered":
					slices.Reverse(h)
					slices.Reverse(r)
					h = append([]string{"before"}, append(h, "after")...)
					r = append([]string{"extra"}, append(r, "extra")...)
					records = [][]string{r}
				}
				return answer(200, archive(t, map[string][]byte{"records.csv": csvBytes(h, records...)})), nil
			})
			err := form5500.New(c).Scrape(ctx, func(rs []source.Row) error { batches++; rows += len(rs); return nil })
			if mode == "partial failure" {
				if err == nil || calls != 1 || batches != 1 || rows != 1000 {
					t.Fatal(err, calls, batches, rows)
				}
			} else {
				want := 2
				if mode == "empty" || mode == "foreign" {
					want = 0
				}
				if mode == "exact batch" {
					want = 2000
				}
				if err != nil || calls != 2 || rows != want {
					t.Fatal(err, calls, rows)
				}
			}
			noFiles(t, *paths)
		})
	}
}

// R-L1A8-HH24 R-L65U-0K0W
func TestDefaultClientAndEmptyYearList(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		f := form5500.Full
		if strings.Contains(r.URL.Path, "_SF_") {
			f = form5500.Short
		}
		h := form5500.Columns(f)
		_, _ = w.Write(archive(t, map[string][]byte{"data.csv": csvBytes(h)}))
	}))
	defer srv.Close()
	ctx, paths := spoolContext(t)
	c := form5500.Config{BaseURL: srv.URL + "/", Years: []string{"2024"}, Now: clock, States: []string{"MN"}}
	if err := form5500.New(c).Scrape(ctx, func([]source.Row) error { t.Fatal("empty batch"); return nil }); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	c.Years = nil
	if err := form5500.New(c).Scrape(ctx, func([]source.Row) error { t.Fatal("batch"); return nil }); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	noFiles(t, *paths)
}

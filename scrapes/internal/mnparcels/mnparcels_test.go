package mnparcels_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/mnparcels"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func clock() time.Time {
	return time.Date(2025, 1, 2, 3, 4, 5, 987654321, time.FixedZone("east", 3600))
}
func config(f transport) mnparcels.Config {
	return mnparcels.Config{HTTP: &http.Client{Transport: f}, Now: clock, Sleep: func(context.Context, time.Duration) {}, Counties: []string{"Carlton"}, UserAgent: "parcel test"}
}
func feature(id string) string {
	return `{"attributes": { "state_pin" : "` + id + `", "owner_name":"A<B" }}`
}
func page(n int) string {
	values := make([]string, n)
	for i := range values {
		values[i] = feature(strconv.Itoa(i))
	}
	return `{"features":[` + strings.Join(values, ",\n") + `]}`
}

// R-ROUB-SB2J R-RQ28-62T8 R-RTPX-BE1B
func TestAPI(t *testing.T) {
	if mnparcels.Name != "mn_parcels" || mnparcels.DefaultBaseURL != "https://enterprise.gisdata.mn.gov/aghost/rest/services/us_mn_state_mngeo/plan_parcels_open/FeatureServer/1/query" || mnparcels.PageSize != 2000 || mnparcels.Retries != 3 || mnparcels.Backoff != time.Second || mnparcels.RequestTimeout != 180*time.Second || mnparcels.OwnerRole != "owner" || mnparcels.TaxpayerRole != "taxpayer" {
		t.Fatal("constants")
	}
	var s source.Scraper = mnparcels.New(mnparcels.Config{})
	if s.Name() != mnparcels.Name {
		t.Fatal(s.Name())
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/query" || r.Header.Get("User-Agent") != "default client" {
			t.Error(r)
		}
		if r.URL.Query().Get("returnCountOnly") == "true" {
			_, _ = io.WriteString(w, `{"count":0}`)
		} else {
			_, _ = io.WriteString(w, page(0))
		}
	}))
	defer srv.Close()
	if err := mnparcels.New(mnparcels.Config{BaseURL: srv.URL + "/query", Counties: []string{"Carlton"}, UserAgent: "default client"}).Scrape(context.Background(), func([]source.Row) error { t.Error("empty batch"); return nil }); err != nil || requests != 2 {
		t.Fatal(err, requests)
	}
}

// R-RRA4-JUJX
func TestWhere(t *testing.T) {
	for _, s := range []string{"St. Louis", "O'Brien", "", " x' OR 'y "} {
		if got, want := mnparcels.Where(s), "co_name='"+strings.ReplaceAll(s, "'", "''")+"'"; got != want {
			t.Fatal(got, want)
		}
	}
}

// R-7TIT-NA3I R-W1GM-M7UI R-7UQQ-11U7 R-RYLI-UH03
func TestWalk(t *testing.T) {
	for _, count := range []string{"0", "5000", "2e3", "99999999999999999999999999999999999999999"} {
		t.Run(count, func(t *testing.T) {
			requests, batches, reads := 0, 0, 0
			var collected []source.Row
			c := config(func(r *http.Request) (*http.Response, error) {
				i := requests
				requests++
				if r.Method != "GET" || r.Body != nil || r.Header.Get("User-Agent") != "parcel test" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Error(r)
				}
				if r.URL.Scheme+"://"+r.URL.Host+r.URL.Path != mnparcels.DefaultBaseURL {
					t.Error(r.URL)
				}
				county := []string{"Carlton", "Carlton", "O'Brien", "O'Brien", "O'Brien"}[i]
				want := url.Values{"where": {mnparcels.Where(county)}, "f": {"json"}}
				if i == 0 || i == 2 {
					want.Set("returnCountOnly", "true")
				} else {
					for k, v := range map[string]string{"outFields": "*", "returnGeometry": "false", "returnCentroid": "true", "outSR": "4326", "orderByFields": "objectid", "resultOffset": "0", "resultRecordCount": "2000"} {
						want.Set(k, v)
					}
					if i == 4 {
						want.Set("resultOffset", "2000")
					}
				}
				if !reflect.DeepEqual(r.URL.Query(), want) {
					t.Error(r.URL.Query(), want)
				}
				if i == 2 && batches != 1 || i == 4 && batches != 2 {
					t.Error("request before previous batch", i, batches)
				}
				if i == 0 || i == 2 {
					return response(200, `{"count":`+count+`}`), nil
				}
				return response(200, page(map[int]int{1: 3, 3: 2000, 4: 1}[i])), nil
			})
			c.Counties = []string{"Carlton", "O'Brien"}
			c.Now = func() time.Time { reads++; return clock().Add(time.Duration(reads) * time.Hour) }
			jar, _ := cookiejar.New(nil)
			u, _ := url.Parse(mnparcels.DefaultBaseURL)
			jar.SetCookies(u, []*http.Cookie{{Name: "secret", Value: "hidden", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}})
			c.HTTP.Jar = jar
			err := mnparcels.New(c).Scrape(context.Background(), func(rows []source.Row) error {
				batches++
				for i, row := range rows {
					want, err := mnparcels.RowOf(json.RawMessage(feature(strconv.Itoa(i))), clock().Add(time.Duration(batches)*time.Hour))
					if err != nil || !reflect.DeepEqual(row, want) {
						t.Error("row differs", i, row, want, err)
					}
				}
				collected = append(collected, rows...)
				return nil
			})
			if err != nil || requests != 5 || batches != 3 || reads != 3 || len(collected) != 2004 {
				t.Fatal(err, requests, batches, reads, len(collected))
			}
		})
	}
	c := config(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected request")
		return nil, errors.New("unexpected")
	})
	c.Counties = nil
	if err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { t.Error("unexpected batch"); return nil }); err != nil {
		t.Fatal(err)
	}
}

// R-W1GM-M7UI R-7UQQ-11U7
func TestEmptyPageAndExactBoundary(t *testing.T) {
	requests, batches := 0, 0
	c := config(func(*http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			return response(200, `{"count":2000}`), nil
		case 2:
			return response(200, page(2000)), nil
		default:
			return response(200, page(0)), nil
		}
	})
	if err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return nil }); err != nil || requests != 3 || batches != 1 {
		t.Fatal(err, requests, batches)
	}
}

// R-7TIT-NA3I R-7UQQ-11U7 R-S4P0-RBPK R-W1GM-M7UI
func TestPinlessPages(t *testing.T) {
	for _, attrs := range []string{`{"state_pin":null,"county_pin":null}`, `{"state_pin":" ","county_pin":""}`, `{}`, `{"state_pin":123,"county_pin":456}`} {
		for _, size := range []int{3, 2000} {
			for _, all := range []bool{false, true} {
				t.Run(fmt.Sprint(attrs, size, all), func(t *testing.T) {
					pinless := `{"attributes":` + attrs + `}`
					features := make([]string, size)
					var wanted []source.Row
					collected := clock().Add(time.Hour)
					for i := range features {
						features[i] = pinless
						if !all && i != 1 {
							features[i] = feature(strconv.Itoa(i))
							row, err := mnparcels.RowOf(json.RawMessage(features[i]), collected)
							if err != nil {
								t.Fatal(err)
							}
							wanted = append(wanted, row)
						}
					}
					var capture telemetry.Capture
					w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: clock, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
					defer w.Shutdown(context.Background(), "test")
					requests, batches := 0, 0
					c := config(func(r *http.Request) (*http.Response, error) {
						requests++
						want := url.Values{"where": {mnparcels.Where("Carlton")}, "f": {"json"}}
						if requests == 1 {
							want.Set("returnCountOnly", "true")
						} else {
							for k, v := range map[string]string{"outFields": "*", "returnGeometry": "false", "returnCentroid": "true", "outSR": "4326", "orderByFields": "objectid", "resultOffset": strconv.Itoa((requests - 2) * 2000), "resultRecordCount": "2000"} {
								want.Set(k, v)
							}
						}
						if r.Method != "GET" || r.URL.Scheme+"://"+r.URL.Host+r.URL.Path != mnparcels.DefaultBaseURL || !reflect.DeepEqual(r.URL.Query(), want) {
							t.Error(r.Method, r.URL, want)
						}
						if requests == 1 {
							return response(200, `{"count":0}`), nil
						}
						if requests == 2 {
							return response(200, `{"features":[`+strings.Join(features, ",\n")+`]}`), nil
						}
						if !all && batches != 1 {
							t.Error("next request before batch", batches)
						}
						return response(200, page(0)), nil
					})
					c.Now = func() time.Time {
						if requests != 2 {
							t.Error("row clock read before page answer", requests)
						}
						return collected
					}
					c.Sleep = func(context.Context, time.Duration) { t.Error("readable page retried") }
					c.Telemetry = w
					if err := mnparcels.New(c).Scrape(context.Background(), func(rows []source.Row) error {
						batches++
						if requests != 2 || !reflect.DeepEqual(rows, wanted) {
							t.Error("batch differs from retained features", requests, len(rows), len(wanted))
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					wantRequests, wantBatches := 2, 1
					if size == 2000 {
						wantRequests = 3
					}
					if all {
						wantBatches = 0
					}
					if requests != wantRequests || batches != wantBatches {
						t.Fatal(requests, batches)
					}
					if err := w.Flush(context.Background()); err != nil {
						t.Fatal(err)
					}
					if events := capture.Events(); len(events) != requests {
						t.Fatal("unexpected skip event", events)
					} else {
						for _, event := range events {
							if event.Name != source.Called || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": mnparcels.Name, "status": int64(200), "duration_us": int64(0)}) {
								t.Fatal("unexpected skip attributes", event)
							}
						}
					}
				})
			}
		}
	}
}

// R-7UQQ-11U7 R-W1GM-M7UI
func TestPinlessShortPageContinuesToNextCounty(t *testing.T) {
	requests, batches := 0, 0
	c := config(func(r *http.Request) (*http.Response, error) {
		requests++
		if requests == 3 && batches != 1 {
			t.Error("next county requested before batch", batches)
		}
		if requests <= 2 && r.URL.Query().Get("where") != mnparcels.Where("Carlton") || requests > 2 && r.URL.Query().Get("where") != mnparcels.Where("Chisago") {
			t.Error(r.URL)
		}
		if r.URL.Query().Get("returnCountOnly") == "true" {
			return response(200, `{"count":3}`), nil
		}
		if requests == 2 {
			return response(200, `{"features":[`+feature("first")+`,{"attributes":{"state_pin":null,"county_pin":null}},`+feature("last")+`]}`), nil
		}
		return response(200, page(0)), nil
	})
	c.Counties = []string{"Carlton", "Chisago"}
	c.Sleep = func(context.Context, time.Duration) { t.Error("readable page retried") }
	if err := mnparcels.New(c).Scrape(context.Background(), func(rows []source.Row) error {
		batches++
		if len(rows) != 2 || rows[0].SourceID != "first" || rows[1].SourceID != "last" {
			t.Error(rows)
		}
		return nil
	}); err != nil || requests != 4 || batches != 1 {
		t.Fatal(err, requests, batches)
	}
}

// R-RZTF-88QS
func TestRetries(t *testing.T) {
	for _, kind := range []string{"client", "429", "500", "503", "599", "error-null", "error-object", "page-recovery"} {
		t.Run(kind, func(t *testing.T) {
			requests, batches := 0, 0
			var sleeps []time.Duration
			c := config(func(*http.Request) (*http.Response, error) {
				requests++
				if kind == "page-recovery" && (requests == 1 || requests == 3) {
					if requests == 1 {
						return response(200, `{"count":1}`), nil
					}
					return response(200, page(1)), nil
				}
				switch kind {
				case "client":
					return nil, errors.New("client failed")
				case "error-null":
					return response(200, `{"error":null}`), nil
				case "error-object":
					return response(200, `{"error":{"code":400,"message":"Unable to complete operation."}}`), nil
				case "page-recovery":
					return response(500, ""), nil
				default:
					status, _ := strconv.Atoi(kind)
					return response(status, ""), nil
				}
			})
			c.Sleep = func(ctx context.Context, d time.Duration) {
				if ctx.Err() != nil {
					t.Error(ctx.Err())
				}
				sleeps = append(sleeps, d)
			}
			err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return nil })
			if kind == "page-recovery" {
				if err != nil || requests != 3 || batches != 1 || !reflect.DeepEqual(sleeps, []time.Duration{time.Second}) {
					t.Fatal(err, requests, batches, sleeps)
				}
			} else if err == nil || requests != 4 || batches != 0 || !reflect.DeepEqual(sleeps, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
				t.Fatal(err, requests, batches, sleeps)
			}
		})
	}
}

// R-RZTF-88QS R-RYLI-UH03
func TestPageErrorExhaustionAndStableRequest(t *testing.T) {
	for _, kind := range []string{"client", "429", "500", "599", "error"} {
		t.Run(kind, func(t *testing.T) {
			requests, batches := 0, 0
			var sleeps []time.Duration
			var firstURL string
			c := config(func(r *http.Request) (*http.Response, error) {
				requests++
				if requests == 1 {
					return response(200, `{"count":1}`), nil
				}
				if requests == 2 {
					firstURL = r.URL.String()
				} else if r.URL.String() != firstURL {
					t.Error("retry request changed", r.URL, firstURL)
				}
				if r.Method != "GET" || r.Body != nil || r.Header.Get("User-Agent") != "parcel test" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Error(r)
				}
				switch kind {
				case "client":
					return nil, errors.New("network")
				case "error":
					return response(200, `{"error":{"code":400,"message":"Unable to complete operation."}}`), nil
				default:
					status, _ := strconv.Atoi(kind)
					return response(status, ""), nil
				}
			})
			c.Sleep = func(_ context.Context, d time.Duration) { sleeps = append(sleeps, d) }
			err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return nil })
			if err == nil || requests != 5 || batches != 0 || !reflect.DeepEqual(sleeps, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}) {
				t.Fatal(err, requests, batches, sleeps)
			}
		})
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// R-7X6I-SLBL R-7TIT-NA3I
func TestTerminalAnswers(t *testing.T) {
	cases := []struct {
		name, body string
		count      bool
		status     int
	}{
		{"bad-status", "", true, 400}, {"redirect", "", true, 302}, {"204", "", true, 204}, {"count-string", `{"count":"2"}`, true, 200}, {"count-negative", `{"count":-1}`, true, 200}, {"count-fraction", `{"count":1.5}`, true, 200}, {"count-null", `{"count":null}`, true, 200}, {"count-missing", `{}`, true, 200}, {"array", `[]`, false, 200}, {"null", `null`, false, 200}, {"missing", `{}`, false, 200}, {"null-features", `{"features":null}`, false, 200}, {"bad-features", `{"features":{}}`, false, 200}, {"oversize", page(2001), false, 200}, {"null-attributes", `{"features":[` + feature("first") + `,{"attributes":null},` + feature("last") + `]}`, false, 200}, {"missing-attributes", `{"features":[{}]}`, false, 200}, {"nonobject-feature", `{"features":[5]}`, false, 200}, {"null-feature", `{"features":[null]}`, false, 200}, {"array-attributes", `{"features":[{"attributes":[]}]}`, false, 200}, {"cut", `{"features":[`, false, 200}, {"trailing", `{"features":[]} trailing`, false, 200}, {"broken", "", false, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests, batches, sleeps := 0, 0, 0
			c := config(func(*http.Request) (*http.Response, error) {
				requests++
				if !tc.count && requests == 1 {
					return response(200, `{"count":1}`), nil
				}
				resp := response(tc.status, tc.body)
				if tc.name == "broken" {
					resp.Body = io.NopCloser(io.MultiReader(strings.NewReader(`{"features":[`+feature("first")), brokenReader{}))
				}
				return resp, nil
			})
			c.Sleep = func(context.Context, time.Duration) { sleeps++ }
			err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return nil })
			want := 2
			if tc.count {
				want = 1
			}
			if err == nil || requests != want || batches != 0 || sleeps != 0 {
				t.Fatal(err, requests, batches, sleeps)
			}
		})
	}
}

// R-S297-ZS86
func TestRefusedBatch(t *testing.T) {
	requests, batches := 0, 0
	c := config(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return response(200, `{"count":9999}`), nil
		}
		return response(200, page(2000)), nil
	})
	c.Counties = append(c.Counties, "Chisago")
	err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return errors.New("refused") })
	if err == nil || requests != 2 || batches != 1 {
		t.Fatal(err, requests, batches)
	}
}

// R-S5WX-53G9 R-S74T-IV6Y
func TestRow(t *testing.T) {
	raw := `{ "state_pin":" state-id ", "county_pin":"local", "co_code":"27145", "owner_name":" ACME ", "owner_more":" LLC ", "tax_name":" JANE DOE ", "anumberpre":" N ","anumber":39583,"anumbersuf":" A ","st_pre_dir":" W ","st_pre_typ":" Old ","st_name":"8th","st_pos_typ":"Avenue","st_pos_dir":" S ","ctu_name":" NORTH BRANCH ","postcomm":"ELSEWHERE","zip":"55056-1234","co_name":" Chisago ","acres_deed":1.07,"acres_poly":2e0,"emv_total":321200,"sale_date":1262304000000,"homestead":"Y","own_add_l1":" one ","own_add_l2":null,"own_add_l3":" ","own_add_l4":" four ","tax_add_l2":" two ","useclass1":"1a/1b/4bb Residential Single Unit","useclass2":" ","abb_legal":{"nested":[1,true]},"exp_date":" raw export ","tax_year":2024,"other":"extra" }`
	f := `{"geometry":null,"attributes":` + raw + `,"centroid":{"x":-92.98930354451028,"y":45.52238037099128}}`
	row, err := mnparcels.RowOf(json.RawMessage(f), clock())
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]any{"acres_deed": json.RawMessage(`1.07`), "acres_poly": json.RawMessage(`2e0`), "co_code": "27145", "county_pin": "local", "emv_bldg": nil, "emv_land": nil, "emv_total": 321200, "exported": " raw export ", "homestead": "Y", "legal": map[string]any{"nested": []any{1, true}}, "owner_address": []string{"one", "four"}, "owner_name": "ACME LLC", "ownership": nil, "plat_name": nil, "sale_date": int64(1262304000000), "sale_value": nil, "state_pin": " state-id ", "tax_address": []string{"two"}, "tax_exempt": nil, "tax_name": " JANE DOE ", "tax_year": 2024, "total_tax": nil, "use_class": []string{"1a/1b/4bb Residential Single Unit"}, "year_built": nil}
	factJSON, _ := json.Marshal(facts)
	lat, lon := 45.52238037099128, -92.98930354451028
	want := source.Row{Scraper: mnparcels.Name, SourceID: " state-id ", Collected: clock().UTC().Truncate(time.Second), Digest: source.Digest([]byte(raw)), Name: "ACME LLC", Names: source.Names("ACME LLC", "JANE DOE"), Address: source.NewAddress("N 39583 A W Old 8th Avenue S", " NORTH BRANCH ", "MN", "55056-1234", " Chisago "), Lat: &lat, Lon: &lon, People: []source.Person{{Name: "ACME LLC", Role: mnparcels.OwnerRole}, {Name: "JANE DOE", Role: mnparcels.TaxpayerRole}}, Facts: factJSON, Raw: json.RawMessage(raw)}
	if !reflect.DeepEqual(row, want) {
		t.Fatalf("got %#v facts %s\nwant %#v facts %s", row, row.Facts, want, want.Facts)
	}
}

// R-S5WX-53G9 R-S74T-IV6Y
func TestRowFallbacks(t *testing.T) {
	for _, tc := range []struct {
		attrs, id, name, city string
		names                 []string
		people                []source.Person
	}{
		{`{"state_pin":null,"co_code":"27145","county_pin":"94-59219-0000","owner_name":null,"owner_more":null,"tax_name":null}`, "27145-94-59219-0000", "", "", []string{}, []source.Person{}},
		{`{"state_pin":" ","co_code":27145,"county_pin":" p ","tax_name":" JANE DOE ","ctu_name":null,"postcomm":"NORTH BRANCH"}`, "- p ", "JANE DOE", "North Branch", []string{"JANE DOE"}, []source.Person{{Name: "JANE DOE", Role: mnparcels.TaxpayerRole}}},
		{`{"state_pin":"id","owner_name":"ACME LLC","tax_name":"Acme Llc"}`, "id", "ACME LLC", "", []string{"ACME LLC", "Acme Llc"}, []source.Person{{Name: "ACME LLC", Role: mnparcels.OwnerRole}}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			for _, centroid := range []string{"", `,"centroid":null`, `,"centroid":[]`, `,"centroid":{"x":1}`, `,"centroid":{"x":"1","y":2}`, `,"centroid":{"x":1,"y":null}`} {
				row, err := mnparcels.RowOf(json.RawMessage(`{"attributes":`+tc.attrs+centroid+`}`), clock())
				if err != nil || row.SourceID != tc.id || row.Name != tc.name || !reflect.DeepEqual(row.Names, tc.names) || !reflect.DeepEqual(row.People, tc.people) || row.Address.City != tc.city || row.Lat != nil || row.Lon != nil || row.Digest != source.Digest([]byte(tc.attrs)) {
					t.Fatal(row, err)
				}
				var facts map[string]any
				if err := json.Unmarshal(row.Facts, &facts); err != nil {
					t.Fatal(err)
				}
				if tc.name == "" && facts["owner_name"] != nil {
					t.Fatal(facts)
				}
				for _, key := range []string{"owner_address", "tax_address", "use_class"} {
					if !reflect.DeepEqual(facts[key], []any{}) {
						t.Fatal(key, facts)
					}
				}
			}
		})
	}
	for _, value := range []string{`321200`, `1.07`, `" bytes "`, `null`, `true`, `[1,2]`, `{"a":1}`} {
		raw := `{"state_pin":"id","emv_total":` + value + `}`
		row, err := mnparcels.RowOf(json.RawMessage(`{"attributes":`+raw+`}`), clock())
		if err != nil {
			t.Fatal(err)
		}
		var facts map[string]json.RawMessage
		if err := json.Unmarshal(row.Facts, &facts); err != nil || string(facts["emv_total"]) != value {
			t.Fatal(err, string(row.Facts))
		}
	}
}

// R-S5WX-53G9
func TestCentroidDoesNotChangeDigestAndLowercaseRoles(t *testing.T) {
	raw := `{"state_pin":"id","owner_name":"Σ","tax_name":"ς"}`
	a, err := mnparcels.RowOf(json.RawMessage(`{"attributes":`+raw+`}`), clock())
	if err != nil {
		t.Fatal(err)
	}
	b, err := mnparcels.RowOf(json.RawMessage(`{"attributes":`+raw+`,"centroid":{"x":1,"y":2}}`), clock())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest || b.Lat == nil || *b.Lat != 2 || b.Lon == nil || *b.Lon != 1 || !reflect.DeepEqual(b.People, []source.Person{{Name: "Σ", Role: mnparcels.OwnerRole}, {Name: "ς", Role: mnparcels.TaxpayerRole}}) {
		t.Fatal(a, b)
	}
}

// R-S8CP-WMXN
func TestInvalidRows(t *testing.T) {
	for _, f := range []string{"", `null`, `[]`, `1`, `"a"`, `{}`, `{"attributes":null}`, `{"attributes":[]}`, `{"attributes":{}}`, `{"attributes":{"state_pin":null,"county_pin":null}}`, `{"attributes":{"state_pin":" ","county_pin":""}}`, `{"attributes":{"state_pin":123,"county_pin":456}}`, `{"attributes":{`} {
		row, err := mnparcels.RowOf(json.RawMessage(f), clock())
		if err == nil || !reflect.DeepEqual(row, source.Row{}) {
			t.Fatal(f, row, err)
		}
	}
}

// R-S4P0-RBPK
func TestTelemetry(t *testing.T) {
	for _, negative := range []bool{false, true} {
		for _, caller := range []bool{false, true} {
			t.Run(fmt.Sprint(negative, caller), func(t *testing.T) {
				var capture telemetry.Capture
				now := clock()
				w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time {
					v := now
					delta := 1500 * time.Microsecond
					if negative {
						delta = -delta
					}
					now = now.Add(delta)
					return v
				}, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
				defer w.Shutdown(context.Background(), "test")
				flush := func() {
					if err := w.Flush(context.Background()); err != nil {
						t.Error(err)
					}
				}
				attempts := 0
				c := config(func(*http.Request) (*http.Response, error) {
					flush()
					if len(capture.Events()) != attempts {
						t.Error("previous event not recorded")
					}
					attempts++
					switch attempts {
					case 1:
						return nil, errors.New("network")
					case 2:
						return response(500, ""), nil
					case 3:
						return response(200, `{"count":1}`), nil
					case 4:
						return response(200, `{"error":null}`), nil
					default:
						return response(200, page(1)), nil
					}
				})
				c.Telemetry = w
				ctx := context.Background()
				if caller {
					ctx = identity.NewContext(ctx, identity.Caller{UserID: "user", RequestID: "request"})
				}
				if err := mnparcels.New(c).Scrape(ctx, func([]source.Row) error {
					flush()
					if len(capture.Events()) != attempts {
						t.Error("event not recorded before batch")
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				flush()
				events := capture.Events()
				if len(events) != 5 {
					t.Fatal(events)
				}
				for i, event := range events {
					duration := int64(1500)
					if negative {
						duration = 0
					}
					user, request := "", ""
					if caller {
						user, request = "user", "request"
					}
					if event.Name != source.Called || event.User != user || event.RequestID != request || !reflect.DeepEqual(event.Attrs, telemetry.Attrs{"scraper": mnparcels.Name, "status": []int64{0, 500, 200, 200, 200}[i], "duration_us": duration}) {
						t.Fatal(event)
					}
				}
			})
		}
	}
}

// R-ZY04-9OL8 R-7UQQ-11U7
func TestPrivacy(t *testing.T) {
	marker, number := "GHIJKLMNopqrstuvWXYZ0123", "123456789012"
	valid := `{"attributes":{"state_pin":"` + marker + `","emv_total":` + number + `}}`
	for _, kind := range []string{"500", "error", "null-attributes", "oversize", "cut", "batch", "pinless"} {
		t.Run(kind, func(t *testing.T) {
			var capture telemetry.Capture
			w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: clock, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
			defer w.Shutdown(context.Background(), "test")
			c := config(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("returnCountOnly") == "true" {
					return response(200, `{"count":1}`), nil
				}
				switch kind {
				case "500":
					return response(500, marker+number), nil
				case "error":
					return response(200, `{"error":{"message":"`+marker+number+`"}}`), nil
				case "null-attributes":
					return response(200, `{"features":[{"attributes":null},`+valid+`]}`), nil
				case "pinless":
					return response(200, `{"features":[{"attributes":{"owner_name":"`+marker+`","emv_total":`+number+`}}]}`), nil
				case "oversize":
					return response(200, `{"features":[`+strings.Repeat(valid+",", 2000)+valid+`]}`), nil
				case "cut":
					return response(200, `{"features":[`+valid+`,`+marker+number), nil
				default:
					return response(200, `{"features":[`+valid+`]}`), nil
				}
			})
			c.Telemetry = w
			err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error {
				if kind == "pinless" {
					t.Error("pinless batch")
				}
				return errors.New("refused")
			})
			if kind == "pinless" && err != nil || kind != "pinless" && (err == nil || strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), number)) {
				t.Fatal(err)
			}
			if err := w.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, event := range capture.Events() {
				for _, v := range event.Attrs {
					value := fmt.Sprint(v)
					if strings.Contains(value, marker) || strings.Contains(value, number) {
						t.Fatal(event)
					}
				}
			}
		})
	}
}

type blockedBody struct{ ctx context.Context }

func (b blockedBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (blockedBody) Close() error               { return nil }

// R-RZTF-88QS R-7X6I-SLBL R-RQ28-62T8
func TestAttemptBounds(t *testing.T) {
	for _, phase := range []string{"headers", "count-body", "page-body", "error-body"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started := time.Now()
				attempts, batches, sleeps := 0, 0, 0
				c := config(func(r *http.Request) (*http.Response, error) {
					attempts++
					deadline, ok := r.Context().Deadline()
					if !ok || time.Until(deadline) != 25*time.Millisecond {
						t.Error("attempt lacks configured bound")
					}
					if phase == "headers" {
						<-r.Context().Done()
						return nil, r.Context().Err()
					}
					if phase == "page-body" && attempts == 1 {
						return response(200, `{"count":1}`), nil
					}
					var body io.ReadCloser = blockedBody{r.Context()}
					if phase == "error-body" {
						body = structReadCloser{io.MultiReader(strings.NewReader(`{"error":null}`), body)}
					}
					return &http.Response{StatusCode: 200, Body: body}, nil
				})
				c.Timeout = 25 * time.Millisecond
				c.Sleep = func(context.Context, time.Duration) { sleeps++ }
				err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { batches++; return nil })
				wantAttempts, wantSleeps := 1, 0
				if phase == "headers" {
					wantAttempts, wantSleeps = 4, 3
				}
				if phase == "page-body" {
					wantAttempts = 2
				}
				if err == nil || attempts != wantAttempts || sleeps != wantSleeps || batches != 0 {
					t.Fatal(err, attempts, sleeps, batches)
				}
				wantElapsed := 25 * time.Millisecond
				if phase == "headers" {
					wantElapsed *= 4
				}
				if elapsed := time.Since(started); elapsed != wantElapsed {
					t.Fatal("attempt bound", elapsed, wantElapsed)
				}
			})
		})
	}
	// The default bound is observed on the bubble's virtual clock as well.
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		c := config(func(r *http.Request) (*http.Response, error) {
			attempts++
			deadline, ok := r.Context().Deadline()
			remaining := time.Until(deadline)
			if !ok || remaining != mnparcels.RequestTimeout {
				t.Error("default bound", remaining)
			}
			if attempts == 1 {
				return response(200, `{"count":0}`), nil
			}
			return response(200, page(0)), nil
		})
		if err := mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { return nil }); err != nil {
			t.Fatal(err)
		}
	})
}

type structReadCloser struct{ io.Reader }

func (structReadCloser) Close() error { return nil }

// R-S3H4-DJYV R-RQ28-62T8
func TestCancellation(t *testing.T) {
	for _, phase := range []string{"initial", "headers", "body", "sleep", "defaultsleep", "beforebatch", "betweenpages"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			attempts, batches := 0, 0
			c := config(func(r *http.Request) (*http.Response, error) {
				attempts++
				switch phase {
				case "headers":
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				case "body":
					close(entered)
					return &http.Response{StatusCode: 200, Body: blockedBody{r.Context()}}, nil
				case "sleep":
					return response(503, ""), nil
				case "defaultsleep":
					close(entered)
					return response(503, ""), nil
				}
				if attempts == 1 {
					return response(200, `{"count":2000}`), nil
				}
				return response(200, page(2000)), nil
			})
			c.Sleep = func(sctx context.Context, _ time.Duration) { close(entered); <-sctx.Done() }
			if phase == "defaultsleep" {
				c.Sleep = nil
			}
			if phase == "initial" {
				cancel()
			}
			if phase == "beforebatch" {
				c.Now = func() time.Time { cancel(); return clock() }
			}
			done := make(chan error, 1)
			go func() {
				done <- mnparcels.New(c).Scrape(ctx, func([]source.Row) error {
					batches++
					if phase == "betweenpages" {
						cancel()
					}
					return nil
				})
			}()
			switch phase {
			case "headers", "body", "sleep", "defaultsleep":
				select {
				case <-entered:
					cancel()
				case <-time.After(time.Second):
					t.Fatal("phase not reached")
				}
			}
			select {
			case err := <-done:
				wantAttempts, wantBatches := 1, 0
				if phase == "initial" {
					wantAttempts = 0
				}
				if phase == "beforebatch" || phase == "betweenpages" {
					wantAttempts = 2
				}
				if phase == "betweenpages" {
					wantBatches = 1
				}
				if err == nil || attempts != wantAttempts || batches != wantBatches {
					t.Fatal(err, attempts, batches)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation hung")
			}
		})
	}
}

type gatedBody struct {
	io.Reader
	entered, release chan struct{}
	first            bool
}

func (b *gatedBody) Read(p []byte) (int, error) {
	if !b.first {
		b.first = true
		close(b.entered)
		<-b.release
	}
	return b.Reader.Read(p)
}

// R-S4P0-RBPK R-W1GM-M7UI
func TestTelemetryBeforeBodyAndSequentialReading(t *testing.T) {
	var capture telemetry.Capture
	now := clock()
	w := telemetry.New(telemetry.Config{Service: "scrapes", Sink: &capture, Now: func() time.Time { v := now; now = now.Add(1500 * time.Microsecond); return v }, Rand: bytes.NewReader(make([]byte, 4096)), Stderr: io.Discard, Sleep: func(context.Context, time.Duration) {}})
	defer w.Shutdown(context.Background(), "test")
	entered, release := make(chan struct{}), make(chan struct{})
	attempts := 0
	c := config(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(&gatedBody{Reader: strings.NewReader(`{"count":0}`), entered: entered, release: release})}, nil
		}
		return response(200, page(0)), nil
	})
	c.Telemetry = w
	done := make(chan error, 1)
	go func() {
		done <- mnparcels.New(c).Scrape(context.Background(), func([]source.Row) error { t.Error("empty batch"); return nil })
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("body not reached")
	}
	if err := w.Flush(context.Background()); err != nil {
		t.Error(err)
	}
	events := capture.Events()
	if attempts != 1 || len(events) != 1 || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"scraper": mnparcels.Name, "status": int64(200), "duration_us": int64(1500)}) {
		t.Error(attempts, events)
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
	events = capture.Events()
	if len(events) != 2 {
		t.Fatal(events)
	}
	for _, e := range events {
		if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"scraper": mnparcels.Name, "status": int64(200), "duration_us": int64(1500)}) {
			t.Fatal(e)
		}
	}
}

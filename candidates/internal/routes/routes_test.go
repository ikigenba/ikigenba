package routes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/routes"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func placed(precision string) string {
	return `{"status":"OK","results":[{"formatted_address":"resolved destination","geometry":{"location":{"lat":46.01,"lng":-92.94},"location_type":"` + precision + `"}},{"ignored":true}],"partial_match":true}`
}
func client(rt transport) *routes.Client {
	return &routes.Client{HTTP: &http.Client{Transport: rt}, Key: "zz-test-key-zz", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
}

// R-NJ2H-R9QJ
func checkInvariant(t *testing.T, o routes.Outcome, charged bool) {
	t.Helper()
	errorsAllowed := map[string]bool{"": true, routes.FailTransport: true, routes.FailTimeout: true, routes.FailCanceled: true, routes.FailShape: true, routes.FailStatus: true}
	whyAllowed := map[string]bool{"": true, routes.WhyNotPlaced: true, routes.WhyTooCoarse: true, routes.WhyNoRoute: true}
	precisionAllowed := map[string]bool{"": true, routes.PrecisionBuilding: true, routes.PrecisionStreet: true, routes.PrecisionTown: true}
	if !errorsAllowed[o.Error] || !whyAllowed[o.Why] || !precisionAllowed[o.Precision] || (o.Error != "" && o.Why != "") {
		t.Fatalf("invalid words: %+v", o)
	}
	routed := o.Error == "" && o.Why == ""
	if (o.Minutes != nil) != routed || (o.Miles != nil) != routed || (routed && (*o.Minutes < 0 || *o.Miles < 0 || o.Precision == routes.PrecisionTown || o.Precision == "")) {
		t.Fatalf("invalid route: %+v", o)
	}
	var failed int64
	if o.Error != "" && o.Sent > 0 {
		failed = 1
	}
	if o.Sent < 0 || o.Sent > 2 || o.Failed != failed || o.CostNanos < 0 || (!charged && o.CostNanos != 0) {
		t.Fatalf("invalid tally: %+v", o)
	}
}

// R-N0S0-0PM4 R-N37S-S93I R-N4FP-60U7 R-N5NL-JSKW
// R-NXPA-CIMV R-NYX6-QADK R-7ORV-988I R-7PZR-MZZ7
func TestConstants(t *testing.T) {
	const timeout = routes.Timeout
	type word string
	type price int32
	var words = []word{routes.PrecisionBuilding, routes.PrecisionStreet, routes.PrecisionTown, routes.RoutesURL, routes.GeocodeURL}
	var prices = []price{routes.RoutePriceNanos, routes.GeocodePriceNanos}
	if len(words) != 5 || len(prices) != 2 {
		t.Fatal("constant use")
	}
	const _ = routes.FailTransport + routes.FailTimeout + routes.FailStatus + routes.FailShape + routes.FailCanceled + routes.WhyNotPlaced + routes.WhyTooCoarse + routes.WhyNoRoute
	if timeout != 10*time.Second || routes.PrecisionBuilding != "building" || routes.PrecisionStreet != "street" || routes.PrecisionTown != "town" {
		t.Fatal("constants")
	}
	if routes.RoutesURL != "https://routes.googleapis.com/directions/v2:computeRoutes" || routes.GeocodeURL != "https://maps.googleapis.com/maps/api/geocode/json" || routes.RoutePriceNanos != 5000000 || routes.GeocodePriceNanos != 5000000 {
		t.Fatal("wire constants")
	}
	for _, words := range [][]string{{routes.FailTransport, routes.FailTimeout, routes.FailStatus, routes.FailShape, routes.FailCanceled}, {routes.WhyNotPlaced, routes.WhyTooCoarse, routes.WhyNoRoute}} {
		seen := map[string]bool{}
		for _, word := range words {
			if word == "" || strings.Contains(word, "\n") || seen[word] {
				t.Fatal("invalid copy constant")
			}
			seen[word] = true
		}
	}
}

// R-N6VH-XKBL R-N83E-BC2A R-N9BA-P3SZ R-NP5Z-O4G0
// R-NBR3-GNAD R-O053-4249 R-7R7O-0RPW R-NWHD-YQW6
func TestWireAndDeterminism(t *testing.T) {
	type capture struct {
		Method, URL string
		Header      http.Header
		Body        string
	}
	var captures [2][]capture
	for i := range captures {
		var calls, timers int
		rt := transport(func(r *http.Request) (*http.Response, error) {
			calls++
			if timers != calls {
				t.Fatalf("timer not called once before request: %d %d", timers, calls)
			}
			data, _ := io.ReadAll(r.Body)
			captures[i] = append(captures[i], capture{r.Method, r.URL.String(), r.Header.Clone(), string(data)})
			if calls == 1 {
				base, _ := url.Parse(routes.GeocodeURL)
				wantQuery := url.Values{"address": {"street & city"}, "key": {"test key &"}}
				if r.Method != http.MethodGet || r.URL.Scheme != base.Scheme || r.URL.Host != base.Host || r.URL.Path != base.Path || !reflect.DeepEqual(r.URL.Query(), wantQuery) || len(data) != 0 {
					t.Fatalf("geocode request: %+v", captures[i][0])
				}
				return response(200, placed("ROOFTOP"))
			}
			if calls != 2 || r.Method != http.MethodPost || r.URL.String() != routes.RoutesURL || r.Header.Get("X-Goog-Api-Key") != "test key &" || r.Header.Get("X-Goog-FieldMask") != "routes.duration,routes.distanceMeters" || r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("route request: %+v", captures[i][1])
			}
			var got any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal([]byte(`{"origin":{"address":"origin &"},"destination":{"location":{"latLng":{"latitude":46.01,"longitude":-92.94}}},"travelMode":"DRIVE","routingPreference":"TRAFFIC_UNAWARE"}`), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("route body: %s", data)
			}
			return response(200, `{"routes":[{"duration":"4635s","distanceMeters":139561}]}`)
		})
		// Unkeyed construction proves the declared field order by use.
		c := &routes.Client{&http.Client{Transport: rt}, "test key &", func(d time.Duration) <-chan time.Time {
			if d != routes.Timeout {
				t.Fatal(d)
			}
			timers++
			return make(chan time.Time)
		}}
		got := c.Drive(context.Background(), "origin &", "street & city")
		minutes, miles := int64(77), 86.7
		want := routes.Outcome{&minutes, &miles, "resolved destination", routes.PrecisionBuilding, "OK", "", "", 2, 0, routes.GeocodePriceNanos + routes.RoutePriceNanos}
		if !reflect.DeepEqual(got, want) || calls != 2 || timers != 2 {
			t.Fatalf("got %+v want %+v", got, want)
		}
	}
	if !reflect.DeepEqual(captures[0], captures[1]) {
		t.Fatal("requests differ")
	}
}

// R-NQDW-1W6P R-NRLS-FNXE R-NSTO-TFO3
// R-7JFG-FDWG  R-7GZN-NUF2 R-O2KV-VLLN
// R-NGMO-ZQ95 R-NHUL-DHZU R-NV9H-KZ5H R-7I7K-1M5R
func TestAnswers(t *testing.T) {
	type testCase struct {
		name, geo, route       string
		geoStatus, routeStatus int
		want                   routes.Outcome
	}
	base := routes.Outcome{Address: "resolved destination", Precision: routes.PrecisionBuilding, Status: "OK", Sent: 2, CostNanos: routes.GeocodePriceNanos}
	cases := []testCase{
		{name: "not placed", geo: `{"status":"ZERO_RESULTS","results":[]}`, want: routes.Outcome{Status: "ZERO_RESULTS", Why: routes.WhyNotPlaced, Sent: 1, CostNanos: routes.GeocodePriceNanos}},
		{name: "town", geo: placed("APPROXIMATE"), want: routes.Outcome{Address: "resolved destination", Precision: routes.PrecisionTown, Status: "OK", Why: routes.WhyTooCoarse, Sent: 1, CostNanos: routes.GeocodePriceNanos}},
	}
	for _, precision := range []string{"ROOFTOP", "RANGE_INTERPOLATED", "GEOMETRIC_CENTER"} {
		for _, body := range []string{`{}`, `{"routes":[]}`} {
			want := base
			want.Why = routes.WhyNoRoute
			want.CostNanos += routes.RoutePriceNanos
			if precision != "ROOFTOP" {
				want.Precision = routes.PrecisionStreet
			}
			cases = append(cases, testCase{name: precision + body, geo: placed(precision), route: body, want: want})
		}
	}
	for _, values := range []struct {
		duration string
		meters   int
		minutes  int64
		miles    float64
	}{{"1770s", 16093, 30, 10}, {"1769s", 2414, 29, 1.5}, {"0s", 80, 0, 0}, {"1.5s", 0, 0, 0}} {
		want := base
		want.Minutes = &values.minutes
		want.Miles = &values.miles
		want.CostNanos += routes.RoutePriceNanos
		body, _ := json.Marshal(map[string]any{"routes": []any{map[string]any{"duration": values.duration, "distanceMeters": values.meters}}})
		cases = append(cases, testCase{name: values.duration, route: string(body), want: want})
	}
	zeroMin, zeroMiles := int64(0), float64(0)
	omitted := base
	omitted.Minutes = &zeroMin
	omitted.Miles = &zeroMiles
	omitted.CostNanos += routes.RoutePriceNanos
	cases = append(cases, testCase{name: "omitted route numbers", route: `{"routes":[{}]}`, want: omitted})
	for _, body := range []string{"", `not json`, `[]`, `null`, `{}`, `{"status":7}`, `{"status":"OK","results":[]}`, strings.ReplaceAll(placed("ROOFTOP"), `"formatted_address":"resolved destination",`, ""), placed("UNKNOWN"), strings.ReplaceAll(placed("ROOFTOP"), `"lat":46.01`, `"lat":"46"`)} {
		cases = append(cases, testCase{name: "geo shape " + body, geo: body, want: routes.Outcome{Error: routes.FailShape, Sent: 1, Failed: 1, Status: func() string {
			if strings.Contains(body, `"OK"`) {
				return "OK"
			}
			return ""
		}()}})
	}
	for _, body := range []string{"", `not json`, `[]`, `null`, `{"routes":null}`, `{"routes":{}}`, `{"routes":[null]}`, `{"routes":[{"duration":"4635"}]}`, `{"routes":[{"duration":4635}]}`, `{"routes":[{"duration":"NaNs"}]}`, `{"routes":[{"duration":"Infs"}]}`, `{"routes":[{"duration":"1e3s"}]}`, `{"routes":[{"duration":"-5s"}]}`, `{"routes":[{"distanceMeters":"139561"}]}`, `{"routes":[{"distanceMeters":-1}]}`, `{"routes":[{"distanceMeters":1.2}]}`, `{"routes":[{"distanceMeters":1.00000000000000001}]}`} {
		want := base
		want.Error = routes.FailShape
		want.Failed = 1
		cases = append(cases, testCase{name: "route shape " + body, route: body, want: want})
	}
	for _, status := range []int{403, 500} {
		cases = append(cases, testCase{name: "geo HTTP", geoStatus: status, geo: "refused", want: routes.Outcome{Sent: 1, Failed: 1, Error: routes.FailStatus}})
		want := base
		want.Error = routes.FailStatus
		want.Failed = 1
		cases = append(cases, testCase{name: "route HTTP", routeStatus: status, route: "refused", want: want})
	}
	for _, status := range []int{200, 400} {
		cases = append(cases, testCase{name: "geo refusal", geoStatus: status, geo: `{"status":"REQUEST_DENIED","error_message":"secret detail"}`, want: routes.Outcome{Sent: 1, Failed: 1, Error: routes.FailStatus, Status: "REQUEST_DENIED"}})
	}
	refused := base
	refused.Error = routes.FailStatus
	refused.Failed = 1
	refused.Status = "INVALID_ARGUMENT"
	cases = append(cases, testCase{name: "route refusal", routeStatus: 400, route: `{"error":{"status":"INVALID_ARGUMENT","message":"secret detail"}}`, want: refused})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := client(func(*http.Request) (*http.Response, error) {
				calls++
				status, body := tc.geoStatus, tc.geo
				if calls == 2 {
					status, body = tc.routeStatus, tc.route
				}
				if status == 0 {
					status = 200
				}
				if calls == 1 && body == "" && tc.geo == "" && !strings.HasPrefix(tc.name, "geo shape") {
					body = placed("ROOFTOP")
				}
				return response(status, body)
			})
			got := c.Drive(context.Background(), "base", "destination")
			if !reflect.DeepEqual(got, tc.want) || int64(calls) != tc.want.Sent {
				t.Fatalf("got %+v want %+v calls %d", got, tc.want, calls)
			}
			checkInvariant(t, got, tc.want.CostNanos > 0)
		})
	}
}

func TestRoutingShapeAndRounding(t *testing.T) {
	cases := []struct {
		name, body     string
		minutes        int64
		miles          float64
		noRoute, shape bool
	}{
		{name: "example", body: `{"routes":[{"distanceMeters":139561,"duration":"4635s"}]}`, minutes: 77, miles: 86.7},
		{name: "round up", body: `{"routes":[{"duration":"1770s","distanceMeters":16093}]}`, minutes: 30, miles: 10},
		{name: "round down", body: `{"routes":[{"duration":"1769s","distanceMeters":2414}]}`, minutes: 29, miles: 1.5},
		{name: "zero", body: `{"routes":[{"duration":"0s","distanceMeters":80}]}`},
		{name: "fractional seconds", body: `{"routes":[{"duration":"1.5s"}]}`},
		{name: "below minute midpoint", body: `{"routes":[{"duration":"29.999s"}]}`},
		{name: "minute midpoint", body: `{"routes":[{"duration":"30s"}]}`, minutes: 1},
		{name: "below limit", body: `{"routes":[{"duration":"999999999.5s"}]}`, minutes: 16666667},
		{name: "limit", body: `{"routes":[{"duration":"1000000000s"}]}`, minutes: 16666667},
		{name: "decimal limit", body: `{"routes":[{"duration":"1000000000.0s"}]}`, minutes: 16666667},
		{name: "rounded to limit", body: `{"routes":[{"duration":"1000000000.00000001s"}]}`, minutes: 16666667},
		{name: "numbers omitted", body: `{"routes":[{}]}`},
		{name: "leading zeros", body: `{"routes":[{"duration":"000060.0s"}]}`, minutes: 1},
		{name: "whole decimal meters", body: `{"routes":[{"distanceMeters":16093.0}]}`, miles: 10},
		{name: "whole exponent meters", body: `{"routes":[{"distanceMeters":1.6093e4}]}`, miles: 10},
		{name: "negative zero meters", body: `{"routes":[{"distanceMeters":-0}]}`},
		{name: "only first route", body: `{"routes":[{"duration":"60s","distanceMeters":1609},null],"ignored":true}`, minutes: 1, miles: 1},
		{name: "absent routes", body: `{}`, noRoute: true},
		{name: "empty routes", body: `{"routes":[]}`, noRoute: true},
		{name: "top array", body: `[]`, shape: true},
		{name: "routes object", body: `{"routes":{}}`, shape: true},
		{name: "routes null", body: `{"routes":null}`, shape: true},
		{name: "first null", body: `{"routes":[null]}`, shape: true},
		{name: "missing suffix", body: `{"routes":[{"duration":"4635"}]}`, shape: true},
		{name: "numeric duration", body: `{"routes":[{"duration":4635}]}`, shape: true},
		{name: "nan duration", body: `{"routes":[{"duration":"NaNs"}]}`, shape: true},
		{name: "infinite duration", body: `{"routes":[{"duration":"Infs"}]}`, shape: true},
		{name: "exponent duration", body: `{"routes":[{"duration":"1e3s"}]}`, shape: true},
		{name: "negative duration", body: `{"routes":[{"duration":"-5s"}]}`, shape: true},
		{name: "above limit", body: `{"routes":[{"duration":"1000000000.5s"}]}`, shape: true},
		{name: "huge duration", body: `{"routes":[{"duration":"999999999999999999999s"}]}`, shape: true},
		{name: "float overflow", body: `{"routes":[{"duration":"` + strings.Repeat("9", 400) + `s"}]}`, shape: true},
		{name: "plus sign", body: `{"routes":[{"duration":"+5s"}]}`, shape: true},
		{name: "leading space", body: `{"routes":[{"duration":" 5s"}]}`, shape: true},
		{name: "missing integer", body: `{"routes":[{"duration":".5s"}]}`, shape: true},
		{name: "missing fraction", body: `{"routes":[{"duration":"5.s"}]}`, shape: true},
		{name: "non ASCII digits", body: `{"routes":[{"duration":"５s"}]}`, shape: true},
		{name: "null duration", body: `{"routes":[{"duration":null}]}`, shape: true},
		{name: "string distance", body: `{"routes":[{"distanceMeters":"139561"}]}`, shape: true},
		{name: "negative distance", body: `{"routes":[{"distanceMeters":-1}]}`, shape: true},
		{name: "fractional distance", body: `{"routes":[{"distanceMeters":1.2}]}`, shape: true},
		{name: "small exact fraction", body: `{"routes":[{"distanceMeters":1.00000000000000001}]}`, shape: true},
		{name: "null distance", body: `{"routes":[{"distanceMeters":null}]}`, shape: true},
		{name: "trailing JSON", body: `{"routes":[{}]} {}`, shape: true},
	}
	for _, precision := range []struct{ wire, word string }{{"ROOFTOP", routes.PrecisionBuilding}, {"RANGE_INTERPOLATED", routes.PrecisionStreet}, {"GEOMETRIC_CENTER", routes.PrecisionStreet}} {
		for _, tc := range cases {
			t.Run(precision.wire+"/"+tc.name, func(t *testing.T) {
				calls := 0
				c := client(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return response(200, placed(precision.wire))
					}
					return response(200, tc.body)
				})
				got := c.Drive(context.Background(), "base", "destination")
				want := routes.Outcome{Address: "resolved destination", Precision: precision.word, Status: "OK", Sent: 2, CostNanos: routes.GeocodePriceNanos}
				if tc.shape {
					want.Error, want.Failed = routes.FailShape, 1
				} else {
					want.CostNanos += routes.RoutePriceNanos
					if tc.noRoute {
						want.Why = routes.WhyNoRoute
					} else {
						want.Minutes, want.Miles = &tc.minutes, &tc.miles
					}
				}
				if !reflect.DeepEqual(got, want) || calls != 2 {
					t.Fatalf("got %+v want %+v calls %d", got, want, calls)
				}
				checkInvariant(t, got, true)
			})
		}
	}
}

// R-NAJ7-2VJO R-NNY3-ACPB R-NCYZ-UF12 R-NKAE-51H8
func TestEarlyAndTransportFailure(t *testing.T) {
	c := client(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New(r.URL.String() + " zz-test-key-zz")
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.After = func(time.Duration) <-chan time.Time { t.Fatal("unexpected timer"); return nil }
	if got := c.Drive(ctx, "base", "dest"); !reflect.DeepEqual(got, routes.Outcome{Error: routes.FailCanceled}) {
		t.Fatal(got)
	} else {
		checkInvariant(t, got, false)
	}
	c.HTTP = nil
	if got := c.Drive(context.Background(), "base", "dest"); !reflect.DeepEqual(got, routes.Outcome{Error: routes.FailTransport}) {
		t.Fatal(got)
	} else {
		checkInvariant(t, got, false)
	}
	refused := client(func(*http.Request) (*http.Response, error) { return response(500, "zz-test-key-zz") })
	if got := refused.Drive(context.Background(), "base", "dest"); !reflect.DeepEqual(got, routes.Outcome{Sent: 1, Failed: 1, Error: routes.FailStatus}) {
		t.Fatal(got)
	} else {
		checkInvariant(t, got, false)
	}
	for _, second := range []bool{false, true} {
		calls := 0
		c = client(func(r *http.Request) (*http.Response, error) {
			calls++
			if second && calls == 1 {
				return response(200, placed("ROOFTOP"))
			}
			return nil, errors.New(r.URL.String() + " zz-test-key-zz")
		})
		got := c.Drive(context.Background(), "base", "dest")
		want := routes.Outcome{Sent: 1, Failed: 1, Error: routes.FailTransport}
		if second {
			want.Sent = 2
			want.CostNanos = routes.GeocodePriceNanos
			want.Address = "resolved destination"
			want.Precision = routes.PrecisionBuilding
			want.Status = "OK"
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v want %+v", got, want)
		}
		checkInvariant(t, got, second)
	}
}

// R-NE6W-86RR R-NFES-LYIG R-NV9H-KZ5H R-NBR3-GNAD
func TestControlledCancellation(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		for _, second := range []bool{false, true} {
			t.Run(fmt.Sprintf("timeout=%t/second=%t", timeout, second), func(t *testing.T) {
				started := make(chan *http.Request, 1)
				release := make(chan struct{})
				timer := make(chan time.Time, 1)
				timers := 0
				calls := 0
				c := client(func(r *http.Request) (*http.Response, error) {
					calls++
					if second && calls == 1 {
						return response(200, placed("ROOFTOP"))
					}
					started <- r
					<-r.Context().Done()
					<-release
					return nil, r.Context().Err()
				})
				c.After = func(d time.Duration) <-chan time.Time {
					timers++
					if d != routes.Timeout {
						t.Fatal(d)
					}
					if second && timers == 1 {
						return make(chan time.Time)
					}
					return timer
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan routes.Outcome, 1)
				go func() { done <- c.Drive(ctx, "base", "dest") }()
				req := <-started
				failure := routes.FailCanceled
				if timeout {
					timer <- time.Time{}
					failure = routes.FailTimeout
				} else {
					cancel()
				}
				<-req.Context().Done()
				select {
				case <-done:
					t.Fatal("returned before transport")
				default:
				}
				close(release)
				got := <-done
				want := routes.Outcome{Sent: 1, Failed: 1, Error: failure}
				if second {
					want.Sent = 2
					want.CostNanos = routes.GeocodePriceNanos
					want.Address = "resolved destination"
					want.Precision = routes.PrecisionBuilding
					want.Status = "OK"
				}
				if !reflect.DeepEqual(got, want) || int64(timers) != want.Sent || int64(calls) != want.Sent {
					t.Fatalf("got %+v want %+v", got, want)
				}
				checkInvariant(t, got, second)
			})
		}
	}
}

// R-NMQ6-WKYM
func TestConcurrentCalls(t *testing.T) {
	var count atomic.Int64
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	c := client(func(*http.Request) (*http.Response, error) {
		count.Add(1)
		entered <- struct{}{}
		<-release
		return nil, errors.New("failed")
	})
	var wg sync.WaitGroup
	results := make(chan routes.Outcome, 8)
	for range 8 {
		wg.Go(func() { results <- c.Drive(context.Background(), "base", "dest") })
	}
	for range 8 {
		<-entered
	}
	close(release)
	wg.Wait()
	close(results)
	for got := range results {
		if !reflect.DeepEqual(got, routes.Outcome{Sent: 1, Failed: 1, Error: routes.FailTransport}) {
			t.Fatal(got)
		}
	}
	if count.Load() != 8 {
		t.Fatal(count.Load())
	}
}

// R-O053-4249 R-7JFG-FDWG
func TestPreserveCoordinateNumbers(t *testing.T) {
	calls := 0
	c := client(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(200, strings.ReplaceAll(placed("ROOFTOP"), `"lat":46.01,"lng":-92.94`, `"lat":46.010000000000000001,"lng":-92.940000000000000001`))
		}
		var body struct {
			Destination struct {
				Location struct {
					LatLng struct {
						Latitude  json.Number
						Longitude json.Number
					}
				}
			}
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, pair := range [][2]string{{string(body.Destination.Location.LatLng.Latitude), "46.010000000000000001"}, {string(body.Destination.Location.LatLng.Longitude), "-92.940000000000000001"}} {
			got, ok := new(big.Rat).SetString(pair[0])
			want, _ := new(big.Rat).SetString(pair[1])
			if !ok || got.Cmp(want) != 0 {
				t.Fatalf("coordinate changed: %s", pair[0])
			}
		}
		return response(200, `{}`)
	})
	got := c.Drive(context.Background(), "base", "destination")
	if calls != 2 || got.Why != routes.WhyNoRoute || got.Error != "" {
		t.Fatal(got)
	}
}

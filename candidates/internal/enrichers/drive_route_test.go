package enrichers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
)

// R-695C-OVSL R-6AD9-2NJA R-6BL5-GF9Z R-6CT1-U70O R-6F8U-LQI2 R-6MK8-WCY8 R-6P01-NWFM
func TestDriveRouteSurfaceAndReadiness(t *testing.T) {
	type word string
	var kind word = enrichers.KindDriveRoute
	var source word = enrichers.SourceGoogleRoutes
	if kind != "drive_route" || source != "google_routes" {
		t.Fatal(kind, source)
	}
	var router enrichers.Router = enrichers.DriveRoute{}
	var failer enrichers.Failer = router
	if router.Kind() != string(kind) || !router.Fetched() || failer.Failed(nil) {
		t.Fatal(router)
	}
	tally := enrichers.Tally{1, 2, 3}
	if tally.Sent != 1 || tally.Failed != 2 || tally.CostNanos != 3 {
		t.Fatal(tally)
	}
	for _, c := range []*routes.Client{nil, {Key: "fixture"}, {HTTP: &http.Client{}}} {
		d := enrichers.DriveRoute{c}
		if d.Ready() != (c != nil && c.HTTP != nil) {
			t.Fatal(c)
		}
		for _, v := range []any{d} {
			if _, ok := v.(enrichers.Deriver); ok {
				t.Fatal("deriver")
			}
			if _, ok := v.(enrichers.Fetcher); ok {
				t.Fatal("fetcher")
			}
			if _, ok := v.(enrichers.Prompter); ok {
				t.Fatal("prompter")
			}
		}
	}
}

// R-6Q7Y-1O6B R-6RFU-FFX0
func TestRouteInputsAndDestination(t *testing.T) {
	d := enrichers.DriveRoute{}
	s := enrichers.Subject{Street: "12 Main St", City: "Sandstone", State: "MN", Zip: "55072", Origin: "Sandstone, MN"}
	before := s
	sum := sha256.Sum256([]byte("12 Main St\tSandstone\tMN\t55072\tSandstone, MN"))
	want := hex.EncodeToString(sum[:])
	if d.Inputs(s) != want || enrichers.Destination(s) != "12 Main St, Sandstone, MN 55072" || !reflect.DeepEqual(s, before) {
		t.Fatal(s)
	}
	for _, change := range []func(*enrichers.Subject){func(v *enrichers.Subject) { v.Street = "12 main St" }, func(v *enrichers.Subject) { v.City = "" }, func(v *enrichers.Subject) { v.State = "" }, func(v *enrichers.Subject) { v.Zip = "" }, func(v *enrichers.Subject) { v.Origin = "other" }} {
		v := s
		change(&v)
		if d.Inputs(v) == "" || d.Inputs(v) == want {
			t.Fatal(v)
		}
	}
	v := s
	v.ID = "other"
	v.Name = "other"
	v.Phone = "other"
	v.Email = "other"
	v.Website = "other"
	v.WebsiteSource = "other"
	if d.Inputs(v) != want {
		t.Fatal("unrelated fields")
	}
	for _, v := range []enrichers.Subject{{Street: s.Street}, {Origin: s.Origin}, {}} {
		if d.Inputs(v) != "" {
			t.Fatal(v)
		}
	}
	for _, tc := range []struct {
		s    enrichers.Subject
		want string
	}{{enrichers.Subject{Street: "s", State: "MN", Zip: "z"}, "s, MN z"}, {enrichers.Subject{Street: "s", Zip: "z"}, "s z"}, {enrichers.Subject{Street: "s", City: "c", State: "MN"}, "s, c, MN"}, {enrichers.Subject{}, ""}} {
		if enrichers.Destination(tc.s) != tc.want {
			t.Fatal(tc)
		}
	}
}

// R-6GGQ-ZI8R R-6TVN-6ZEE R-6V3J-KR53 R-6WBF-YIVS R-7171-HLUK
func TestRouteFoundEncodingValidationAndFailure(t *testing.T) {
	n, m := int64(47), 38.2
	f := enrichers.RouteFound{&n, &m, "a", "building", "s", "", "", 0}
	b, err := json.Marshal(f)
	if err != nil || string(b) != `{"minutes":47,"miles":38.2,"address":"a","precision":"building","status":"s","why":"","request_error":"","cost_nanos":0}` {
		t.Fatal(string(b), err)
	}
	var decoded enrichers.RouteFound
	if json.Unmarshal(b, &decoded) != nil || !reflect.DeepEqual(f, decoded) {
		t.Fatal(decoded)
	}
	var object map[string]any
	_ = json.Unmarshal(b, &object)
	for key := range object {
		for _, old := range []string{"Candidate", "Kind", "State", "URL", "Title", "StatusCode", "Size", "Error", "Source", "Cost", "At"} {
			if strings.EqualFold(key, old) {
				t.Fatal(key)
			}
		}
	}
	d := enrichers.DriveRoute{}
	for _, raw := range []string{string(b), `{}`, ` {"minutes":null,"miles":0} `, `{"precision":"street"}`, `{"precision":"town"}`, `{"request_error":"x"}`} {
		if !d.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"minutes":-1}`, `{"minutes":1.5}`, `{"minutes":"47"}`, `{"miles":-0.1}`, `{"miles":"38.2"}`, `{"cost_nanos":-1}`, `{"precision":"Building"}`, `{"precision":"rooftop"}`, `{"error":"x"}`, `{"x":1}`, `null`, `[]`, `{} {}`, ``} {
		if d.Valid(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, v := range []enrichers.RouteFound{{}, {Minutes: &n}, {Why: "coarse"}, {Error: "x"}, {Minutes: &n, Error: "x"}} {
		if v.Failed() != (v.Error != "") {
			t.Fatal(v)
		}
	}
	for _, raw := range []string{`{"request_error":"x","cost_nanos":0}`, `{"request_error":"x","y":1}`} {
		if !d.Failed(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{string(b), `{}`, `[]`, `{} x`, ``} {
		if d.Failed(json.RawMessage(raw)) {
			t.Fatal(raw)
		}
	}
}

type routeTransport func(*http.Request) (*http.Response, error)

func (f routeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// R-6SNQ-T7NP
func TestDriveRouteRelaysClientOutcome(t *testing.T) {
	for _, precision := range []string{"ROOFTOP", "APPROXIMATE"} {
		t.Run(precision, func(t *testing.T) {
			calls := 0
			timers := 0
			s := enrichers.Subject{Street: "12 Main St", City: "Sandstone", State: "MN", Zip: "55072", Origin: "base"}
			before := s
			c := &routes.Client{Key: "fixture", After: func(time.Duration) <-chan time.Time { timers++; return make(chan time.Time) }, HTTP: &http.Client{Transport: routeTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body := `{"routes":[{"duration":"2820s","distanceMeters":61477}]}`
				if calls == 1 {
					if r.URL.Query().Get("address") != enrichers.Destination(s) {
						t.Fatal(r.URL)
					}
					body = `{"status":"OK","results":[{"formatted_address":"resolved","geometry":{"location":{"lat":46,"lng":-92},"location_type":"` + precision + `"}}]}`
				} else {
					var payload struct {
						Origin struct {
							Address string `json:"address"`
						} `json:"origin"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Origin.Address != s.Origin {
						t.Fatalf("route origin = %q, %v", payload.Origin.Address, err)
					}
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			result, tally := (enrichers.DriveRoute{Routes: c}).Route(context.Background(), s)
			var found enrichers.RouteFound
			_ = json.Unmarshal(result.Found, &found)
			expected := int64(2)
			minutes, miles := int64(47), 38.2
			want := enrichers.RouteFound{Minutes: &minutes, Miles: &miles, Address: "resolved", Precision: routes.PrecisionBuilding, Status: "OK", CostNanos: routes.GeocodePriceNanos + routes.RoutePriceNanos}
			if precision == "APPROXIMATE" {
				expected = 1
				want = enrichers.RouteFound{Address: "resolved", Precision: routes.PrecisionTown, Status: "OK", Why: routes.WhyTooCoarse, CostNanos: routes.GeocodePriceNanos}
			}
			if !reflect.DeepEqual(found, want) {
				t.Fatalf("route found = %#v, want %#v", found, want)
			}

			if tally.Sent != expected || tally.Failed != 0 || tally.CostNanos != expected*routes.GeocodePriceNanos || result.Cost != float64(tally.CostNanos)/1e9 || found.CostNanos != tally.CostNanos || result.Source != enrichers.SourceGoogleRoutes || calls != int(expected) || timers != int(expected) || !reflect.DeepEqual(s, before) {
				t.Fatal(result, tally, calls, timers)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			calls = 0
			timers = 0
			result, tally = (enrichers.DriveRoute{Routes: c}).Route(ctx, s)
			_ = json.Unmarshal(result.Found, &found)
			if found.Error != routes.FailCanceled || tally != (enrichers.Tally{}) || calls != 0 || timers != 0 {
				t.Fatal(found, tally)
			}
		})
	}
}

// R-6SNQ-T7NP
func TestDriveRouteRelaysTimedOutRequest(t *testing.T) {
	requested := make(chan struct{})
	timer := make(chan time.Time)
	c := &routes.Client{Key: "fixture", After: func(time.Duration) <-chan time.Time { return timer }, HTTP: &http.Client{Transport: routeTransport(func(r *http.Request) (*http.Response, error) {
		close(requested)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	done := make(chan struct {
		r enrichers.Result
		t enrichers.Tally
	}, 1)
	go func() {
		r, tally := (enrichers.DriveRoute{Routes: c}).Route(context.Background(), enrichers.Subject{Street: "street", Origin: "base"})
		done <- struct {
			r enrichers.Result
			t enrichers.Tally
		}{r, tally}
	}()
	<-requested
	timer <- time.Unix(1, 0)
	got := <-done
	var found enrichers.RouteFound
	if json.Unmarshal(got.r.Found, &found) != nil || found.Error != routes.FailTimeout || found.Minutes != nil || found.Miles != nil || got.r.Cost != 0 || got.t != (enrichers.Tally{Sent: 1, Failed: 1}) {
		t.Fatal(got, found)
	}
}

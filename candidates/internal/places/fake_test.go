package places_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/places"
)

// R-EVA0-RZ77
func TestFakeAnswers(t *testing.T) {
	cl := &places.Client{HTTP: &http.Client{Transport: places.Fake{}}, Key: "sandbox-test-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	seen := map[string]bool{}
	for _, circle := range []places.Circle{{Lat: 44.25, Lon: -93.5, Radius: 199}, {Lat: 44.25, Lon: -93.5, Radius: 200}, {Lat: 44.25, Lon: -93.5, Radius: 600}, {Lat: 44.5, Lon: -93.5, Radius: 600}, {Lat: 44.25, Lon: -93.75, Radius: 600}} {
		a := cl.Nearby(context.Background(), circle)
		n := places.FakeFew
		if circle.Radius >= places.FakeFullRadius {
			n = places.MaxResults
		}
		if a.Error != "" || a.Sent != 1 || a.Failed != 0 || a.CostNanos != places.PriceNanos || len(a.Places) != n {
			t.Fatalf("fake=%#v", a)
		}
		for _, p := range a.Places {
			_, _, _, zip := places.Address(p.Address)
			if p.ID == "" || seen[p.ID] || p.Name == "" || p.Website != "" || p.Phone != "" || zip != "" {
				t.Fatalf("fake place=%#v", p)
			}
			seen[p.ID] = true
		}
		if again := cl.Nearby(context.Background(), circle); !reflect.DeepEqual(a, again) {
			t.Fatal("fake answer changed for equal circle")
		}
	}
	for _, tt := range []struct {
		radius   float64
		budget   int64
		requests int64
		cut      bool
	}{{600, 100, 100, true}, {150, 100, 1, false}} {
		s := cl.Sweep(context.Background(), places.First(44, -93, tt.radius), tt.budget)
		if s.Requests != tt.requests || s.Circles != tt.requests || s.CostNanos != tt.requests*places.PriceNanos || s.CeilingReached != tt.cut || s.Error != "" || s.Failed != 0 {
			t.Fatalf("fake sweep=%#v", s)
		}
		if tt.radius == 150 && len(s.Found) != places.FakeFew {
			t.Fatal("small circle count")
		}
	}
}

// R-EVA0-RZ77
func TestFakeRequestBoundary(t *testing.T) {
	for _, tt := range []struct {
		method, url, body string
		code, count       int
	}{
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":1}}}`, 200, places.FakeFew},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":200}}}`, 200, places.MaxResults},
		{http.MethodGet, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":200}}}`, 404, 0},
		{http.MethodPost, "https://unreachable.invalid/", `{"locationRestriction":{"circle":{"radius":200}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL + "?query=yes", `{"locationRestriction":{"circle":{"radius":200}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, "", 404, 0},
		{http.MethodPost, places.NearbyURL, "null", 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":"200"}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":null}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":true}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":1e}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":01}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":NaN}}}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{"radius":200}}} {}`, 404, 0},
		{http.MethodPost, places.NearbyURL, `{"locationRestriction":{"circle":{}}}`, 404, 0},
	} {
		t.Run(fmt.Sprint(tt.method, tt.url, tt.body), func(t *testing.T) {
			r, err := http.NewRequest(tt.method, tt.url, strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			// A client using Fake must answer even for a host with no listener.
			h := &http.Client{Transport: places.Fake{}}
			resp, err := h.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.code {
				t.Fatalf("status=%d want %d", resp.StatusCode, tt.code)
			}
			if tt.code == 200 {
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				cl := client(func(*http.Request) (*http.Response, error) { return response(200, string(data)) })
				a := cl.Nearby(context.Background(), places.Circle{})
				if a.Error != "" || len(a.Places) != tt.count {
					t.Fatalf("fake shape=%#v", a)
				}
			}
		})
	}
}

func fakeNumberPlaces(t *testing.T, radius, latitude, longitude string, count int) []places.Place {
	t.Helper()
	body := `{"locationRestriction":{"circle":{"center":{"latitude":` + latitude + `,"longitude":` + longitude + `},"radius":` + radius + `}}}`
	r, err := http.NewRequest(http.MethodPost, places.NearbyURL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (places.Fake{}).RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("radius %s status=%d", radius, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	cl := client(func(*http.Request) (*http.Response, error) { return response(200, string(data)) })
	a := cl.Nearby(context.Background(), places.Circle{})
	if a.Error != "" || len(a.Places) != count {
		t.Fatalf("radius %s answer=%#v", radius, a)
	}
	seen := map[string]bool{}
	for _, p := range a.Places {
		_, _, _, zip := places.Address(p.Address)
		if p.ID == "" || seen[p.ID] || p.Name == "" || p.Website != "" || p.Phone != "" || zip != "" {
			t.Fatalf("fake number place=%#v", p)
		}
		seen[p.ID] = true
	}
	return a.Places
}

// R-EVA0-RZ77
func TestFakeExactJSONNumbers(t *testing.T) {
	for _, tt := range []struct {
		radius string
		count  int
	}{
		{"1e309", places.MaxResults}, {"-1e309", places.FakeFew},
		{"1e-309", places.FakeFew}, {"-1e-309", places.FakeFew},
		{"1e999999999999999999999999999999", places.MaxResults},
		{"1e-999999999999999999999999999999", places.FakeFew},
		{"-1e999999999999999999999999999999", places.FakeFew},
		{"0e999999999999999999999999999999", places.FakeFew},
		{"199.999999999999999999999999999999999999999", places.FakeFew},
		{"200.000000000000000000000000000000000000001", places.MaxResults},
		{"2e+2", places.MaxResults}, {"200e-1", places.FakeFew},
	} {
		t.Run(tt.radius, func(t *testing.T) { fakeNumberPlaces(t, tt.radius, "44.25", "-93.5", tt.count) })
	}

	groups := []struct {
		radii []string
		count int
	}{
		{[]string{"1e309", "10e308", "1000e306"}, places.MaxResults},
		{[]string{"2e309", "20e308"}, places.MaxResults},
		{[]string{"1.00000000000000000000000001e309"}, places.MaxResults},
		{[]string{"1e310"}, places.MaxResults},
		{[]string{"200", "2e2", "200.000", "0.2e3"}, places.MaxResults},
		{[]string{"0", "-0", "-0.0e123", "0e999999999999999999999999"}, places.FakeFew},
		{[]string{"1e-309", "10e-310"}, places.FakeFew},
		{[]string{"-1e309", "-10e308"}, places.FakeFew},
	}
	seen := map[string]bool{}
	for _, group := range groups {
		want := fakeNumberPlaces(t, group.radii[0], "44.25", "-93.5", group.count)
		for _, p := range want {
			if seen[p.ID] {
				t.Fatalf("distinct numeric circles share ID %q", p.ID)
			}
			seen[p.ID] = true
		}
		for _, radius := range group.radii {
			got := fakeNumberPlaces(t, radius, "4425e-2", "-0.935e2", group.count)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("equal numeric circles %s and %s differ", radius, group.radii[0])
			}
		}
	}
	for _, center := range [][2]string{{"1e309", "-93.5"}, {"2e309", "-93.5"}, {"44.25", "1e309"}, {"44.25", "2e309"}} {
		for _, p := range fakeNumberPlaces(t, "1e309", center[0], center[1], places.MaxResults) {
			if seen[p.ID] {
				t.Fatalf("different centers share ID %q", p.ID)
			}
			seen[p.ID] = true
		}
	}
}

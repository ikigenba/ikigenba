package places_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
)

func requestCircle(t *testing.T, r *http.Request) places.Circle {
	t.Helper()
	var v struct {
		LocationRestriction struct {
			Circle struct {
				Center struct{ Latitude, Longitude float64 }
				Radius float64
			}
		}
	}
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		t.Error(err)
	}
	c := v.LocationRestriction.Circle
	return places.Circle{Lat: c.Center.Latitude, Lon: c.Center.Longitude, Radius: c.Radius}
}

// R-0BGQ-7SGV
func TestSweepProcedure(t *testing.T) {
	first := places.Circle{Lat: 44.5, Lon: -93.25, Radius: 800}
	full := fullPlaces()
	children := places.Cover(first)
	var grandchildren []places.Circle
	for _, child := range children {
		grandchildren = append(grandchildren, places.Cover(child)...)
	}
	for _, tt := range []struct {
		name                                  string
		budget                                int64
		mode                                  string
		wantCircles, wantRequests, wantFailed int64
		cut                                   bool
		word, status                          string
		wantFound                             int
	}{
		{"single", 100, "single", 1, 1, 0, false, "", "", 1},
		{"empty", 100, "empty", 1, 1, 0, false, "", "", 0},
		{"split", 100, "split", 5, 5, 0, false, "", "", 20},
		{"partial", 3, "split", 3, 3, 0, true, "", "", 20},
		{"repeat", 10, "repeat", 10, 10, 0, true, "", "", 20},
		{"zero", 0, "repeat", 0, 0, 0, true, "", "", 0},
		{"negative", -1, "repeat", 0, 0, 0, true, "", "", 0},
		{"failure", 100, "failure", 1, 2, 1, false, places.FailStatus, "DENIED", 20},
		{"first-failure", 100, "first-failure", 0, 1, 1, false, places.FailShape, "", 0},
		{"two-level", 100, "two-level", 9, 9, 0, false, "", "", 21},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []places.Circle
			cl := client(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, requestCircle(t, r))
				n := len(calls)
				switch tt.mode {
				case "single":
					return response(200, wire(places.Place{ID: "alone"}))
				case "first-failure":
					return response(200, "null")
				case "failure":
					if n == 2 {
						return response(500, `{"error":{"status":"DENIED"}}`)
					}
				case "two-level":
					if n == 2 {
						return response(200, wire(full...))
					}
					if n == 3 {
						return response(200, wire(full[0], places.Place{ID: "later-new"}))
					}
				}
				if tt.mode == "repeat" || (tt.mode != "empty" && n == 1) {
					return response(200, wire(full...))
				}
				return response(200, "{}")
			})
			s := cl.Sweep(context.Background(), first, tt.budget)
			if s.Found == nil || len(s.Found) != tt.wantFound || s.Circles != tt.wantCircles || s.Requests != tt.wantRequests || s.Failed != tt.wantFailed || s.CostNanos != tt.wantCircles*places.PriceNanos || s.CeilingReached != tt.cut || s.Error != tt.word || s.Status != tt.status {
				t.Fatalf("sweep=%#v", s)
			}
			wantCalls := []places.Circle{}
			if tt.wantRequests > 0 {
				wantCalls = append(wantCalls, first)
			}
			for n := int64(1); n < tt.wantRequests; n++ {
				if n <= 4 {
					wantCalls = append(wantCalls, children[n-1])
				} else {
					wantCalls = append(wantCalls, grandchildren[n-5])
				}
			}
			if !reflect.DeepEqual(calls, wantCalls) && (len(calls) != 0 || len(wantCalls) != 0) {
				t.Fatalf("circles=%#v want %#v", calls, wantCalls)
			}
			for i, f := range s.Found {
				want := places.Found{Place: full[i%places.MaxResults], Circle: first}
				if tt.mode == "single" {
					want.Place = places.Place{ID: "alone"}
				}
				if i == 20 {
					want = places.Found{Place: places.Place{ID: "later-new"}, Circle: children[1]}
				}
				if f != want {
					t.Fatalf("found[%d]=%#v want %#v", i, f, want)
				}
			}
		})
	}
}

// R-0BGQ-7SGV
func TestSweepCanceledBeforeRequest(t *testing.T) {
	cl := client(func(*http.Request) (*http.Response, error) { t.Error("unexpected request"); return response(200, "{}") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	want := places.Sweep{Found: []places.Found{}, Error: places.FailCanceled}
	if got := cl.Sweep(ctx, places.Circle{}, 10); !reflect.DeepEqual(got, want) {
		t.Fatalf("sweep=%#v", got)
	}
}

// R-07T1-2H8S R-090X-G8ZH
func TestCircles(t *testing.T) {
	for _, tt := range []struct{ in, want float64 }{{-1, 50}, {0, 50}, {49, 50}, {50, 50}, {548.5, 548.5}, {50000, 50000}, {60000, 50000}, {math.NaN(), 50}, {math.Inf(-1), 50}, {math.Inf(1), 50000}} {
		want := places.Circle{Lat: 44.25, Lon: -93.5, Radius: tt.want}
		if got := places.First(want.Lat, want.Lon, tt.in); got != want {
			t.Errorf("first=%#v want %#v", got, want)
		}
	}
	c := places.Circle{Lat: 45, Lon: -93, Radius: 600}
	d := c.Radius / 2 / parks.EarthRadius * 180 / math.Pi
	e := c.Radius / 2 / (parks.EarthRadius * math.Cos(c.Lat*math.Pi/180)) * 180 / math.Pi
	want := []places.Circle{{Lat: 45 + d, Lon: -93 - e, Radius: 450}, {Lat: 45 + d, Lon: -93 + e, Radius: 450}, {Lat: 45 - d, Lon: -93 - e, Radius: 450}, {Lat: 45 - d, Lon: -93 + e, Radius: 450}}
	got := places.Cover(c)
	if len(got) != 4 {
		t.Fatal("cover length")
	}
	for i, k := range got {
		if math.Abs(k.Lat-want[i].Lat) > 1e-9 || math.Abs(k.Lon-want[i].Lon) > 1e-9 || k.Radius != want[i].Radius {
			t.Errorf("child=%#v want %#v", k, want[i])
		}
	}
	got[0].Lat = 0
	if places.Cover(c)[0].Lat == 0 {
		t.Fatal("cover shares mutable slice")
	}
}

// R-0A8T-U0Q6
func TestCoverContainsDisc(t *testing.T) {
	for _, lat := range []float64{-60, 0, 45.5, 60} {
		for _, radius := range []float64{50, 600, 50000} {
			c := places.Circle{Lat: lat, Lon: -93, Radius: radius}
			cover := places.Cover(c)
			for _, fraction := range []float64{0, .25, .5, .75, 1} {
				for bearing := range 360 {
					// A great-circle destination at the specified bearing and distance.
					phi := lat * math.Pi / 180
					theta := float64(bearing) * math.Pi / 180
					distance := radius * fraction / parks.EarthRadius
					p := math.Asin(math.Sin(phi)*math.Cos(distance) + math.Cos(phi)*math.Sin(distance)*math.Cos(theta))
					lon := -93 + math.Atan2(math.Sin(theta)*math.Sin(distance)*math.Cos(phi), math.Cos(distance)-math.Sin(phi)*math.Sin(p))*180/math.Pi
					pointLat := p * 180 / math.Pi
					inside := false
					for _, child := range cover {
						if parks.Distance(child.Lat, child.Lon, pointLat, lon) <= child.Radius {
							inside = true
							break
						}
					}
					if !inside {
						t.Fatalf("lat=%v radius=%v fraction=%v bearing=%d uncovered", lat, radius, fraction, bearing)
					}
				}
			}
		}
	}
}

// R-0IS4-IEX1
func TestCeiling(t *testing.T) {
	for _, tt := range []struct {
		n    int64
		want bool
	}{{-1, false}, {0, false}, {1, true}, {100, true}, {5000, true}, {5001, false}, {math.MinInt64, false}, {math.MaxInt64, false}} {
		if got := places.ValidCeiling(tt.n); got != tt.want {
			t.Errorf("ceiling %d=%v", tt.n, got)
		}
	}
	for n := int64(places.CeilingMin); n <= places.CeilingMax; n++ {
		if !places.ValidCeiling(n) {
			t.Fatal(fmt.Sprint("invalid interior ceiling ", n))
		}
	}
}

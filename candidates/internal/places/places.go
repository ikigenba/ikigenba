// Package places searches circles through Google's Nearby Search service.
package places

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
)

// Wire constants, prices, limits and sandbox sizing.
const (
	Name                         = "places"
	NearbyURL                    = "https://places.googleapis.com/v1/places:searchNearby"
	FieldMask                    = "places.id,places.displayName,places.formattedAddress,places.primaryType,places.websiteUri,places.nationalPhoneNumber"
	MaxResults                   = 20
	PriceNanos                   = 35000000
	SeedCeiling                  = 100
	CeilingMin                   = 1
	CeilingMax                   = 5000
	FakeFew                      = 3
	MinRadius                    = 50
	MaxRadius                    = 50000
	ChildRatio                   = 0.75
	FakeFullRadius               = 200
	Timeout        time.Duration = 10 * time.Second
	FailTransport  string        = "Places transport failed"
	FailTimeout    string        = "Places request timed out"
	FailStatus     string        = "Places service refused the request"
	FailShape      string        = "Places service returned an unexpected answer"
	FailCanceled   string        = "Places request canceled"
)

// Circle is a centre in degrees and a radius in metres.
type Circle struct{ Lat, Lon, Radius float64 }

// Place holds the fields supplied by Nearby Search.
type Place struct{ ID, Name, Address, PrimaryType, Website, Phone string }

// Answer holds one request's places and accounting.
type Answer struct {
	Places                  []Place
	Status, Error           string
	Sent, Failed, CostNanos int64
}

// Found preserves the circle where a place was first seen.
type Found struct {
	Place  Place
	Circle Circle
}

// Sweep holds one breadth-first search's results and accounting.
type Sweep struct {
	Found                                []Found
	Circles, Requests, Failed, CostNanos int64
	CeilingReached                       bool
	Status, Error                        string
}

// Client carries the request transport, credential and timeout seam.
type Client struct {
	HTTP  *http.Client
	Key   string
	After func(d time.Duration) <-chan time.Time
}

type wireAnswer struct {
	status  int
	body    []byte
	failure string
}

// Nearby searches exactly one circle without exposing transport diagnostics.
func (cl *Client) Nearby(ctx context.Context, c Circle) Answer {
	if ctx.Err() != nil {
		return Answer{Error: FailCanceled}
	}
	if cl.HTTP == nil {
		return Answer{Error: FailTransport}
	}
	body, err := json.Marshal(map[string]any{"locationRestriction": map[string]any{"circle": map[string]any{"center": map[string]float64{"latitude": c.Lat, "longitude": c.Lon}, "radius": c.Radius}}, "maxResultCount": MaxResults})
	if err != nil {
		return Answer{Error: FailShape}
	}
	a := cl.request(ctx, body)
	out := Answer{Sent: 1}
	switch {
	case a.failure != "":
		out.Error = a.failure
	case a.status != http.StatusOK:
		out.Error = FailStatus
		out.Status = refusal(a.body, cl.Key)
	default:
		var ok bool
		out.Places, ok = decodePlaces(a.body)
		if !ok {
			out.Error = FailShape
		}
	}
	if out.Error != "" {
		out.Failed = 1
	} else {
		out.CostNanos = PriceNanos
	}
	return out
}

func (cl *Client) request(ctx context.Context, body []byte) wireAnswer {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r, err := http.NewRequestWithContext(rctx, http.MethodPost, NearbyURL, bytes.NewReader(body))
	if err != nil {
		return wireAnswer{failure: FailTransport}
	}
	r.Header.Set("X-Goog-Api-Key", cl.Key)
	r.Header.Set("X-Goog-FieldMask", FieldMask)
	r.Header.Set("Content-Type", "application/json")
	after := cl.After
	if after == nil {
		after = time.After
	}
	timer := after(Timeout)
	done := make(chan wireAnswer, 1)
	go func() {
		resp, e := cl.HTTP.Do(r)
		if e != nil {
			done <- wireAnswer{failure: FailTransport}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		data, e := io.ReadAll(resp.Body)
		if e != nil {
			done <- wireAnswer{failure: FailTransport}
			return
		}
		done <- wireAnswer{status: resp.StatusCode, body: data}
	}()
	select {
	case a := <-done:
		return a
	case <-timer:
		cancel()
		<-done
		return wireAnswer{failure: FailTimeout}
	case <-ctx.Done():
		cancel()
		<-done
		return wireAnswer{failure: FailCanceled}
	}
}

// Sweep searches full circles breadth first and keeps the first occurrence of each ID.
func (cl *Client) Sweep(ctx context.Context, first Circle, budget int64) Sweep {
	out := Sweep{Found: []Found{}}
	queue := []Circle{first}
	seen := map[string]bool{}
	for len(queue) > 0 && out.Requests < budget {
		c := queue[0]
		queue = queue[1:]
		a := cl.Nearby(ctx, c)
		out.Requests += a.Sent
		out.Failed += a.Failed
		out.CostNanos += a.CostNanos
		if a.Error != "" {
			out.Error, out.Status = a.Error, a.Status
			break
		}
		out.Circles++
		for _, p := range a.Places {
			if !seen[p.ID] {
				seen[p.ID] = true
				out.Found = append(out.Found, Found{p, c})
			}
		}
		if len(a.Places) == MaxResults {
			queue = append(queue, Cover(c)...)
		}
	}
	out.CeilingReached = out.Error == "" && len(queue) > 0
	return out
}

// First clamps a park's radius to the service's supported bounds.
func First(lat, lon, radius float64) Circle {
	if math.IsNaN(radius) || radius < MinRadius {
		radius = MinRadius
	} else if radius > MaxRadius {
		radius = MaxRadius
	}
	return Circle{lat, lon, radius}
}

// Cover returns overlapping quadrant circles in NW, NE, SW, SE order.
func Cover(c Circle) []Circle {
	d := c.Radius / 2 / parks.EarthRadius * 180 / math.Pi
	e := c.Radius / 2 / (parks.EarthRadius * math.Cos(c.Lat*math.Pi/180)) * 180 / math.Pi
	r := c.Radius * ChildRatio
	return []Circle{{c.Lat + d, c.Lon - e, r}, {c.Lat + d, c.Lon + e, r}, {c.Lat - d, c.Lon - e, r}, {c.Lat - d, c.Lon + e, r}}
}

// ValidCeiling reports whether the request ceiling is within its bounds.
func ValidCeiling(n int64) bool { return n >= CeilingMin && n <= CeilingMax }

// Package routes measures drives through Google's geocoding and routing services.
package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Wire endpoints, precision words, prices and the per-request time limit.
const (
	Timeout           time.Duration = 10 * time.Second
	PrecisionBuilding               = "building"
	PrecisionStreet                 = "street"
	PrecisionTown                   = "town"
	RoutesURL                       = "https://routes.googleapis.com/directions/v2:computeRoutes"
	GeocodeURL                      = "https://maps.googleapis.com/maps/api/geocode/json"
	RoutePriceNanos                 = 5000000
	GeocodePriceNanos               = 5000000
	FailTransport     string        = "Route transport failed"
	FailTimeout       string        = "Route request timed out"
	FailStatus        string        = "Route service refused the request"
	FailShape         string        = "Route service returned an unexpected answer"
	FailCanceled      string        = "Route request canceled"
	WhyNotPlaced      string        = "Destination was not placed"
	WhyTooCoarse      string        = "Destination was placed only to the town"
	WhyNoRoute        string        = "No drive route was found"
)

// Client uses the supplied transport, credential and request timer.
type Client struct {
	HTTP  *http.Client
	Key   string
	After func(d time.Duration) <-chan time.Time
}

// Outcome records the answer without preserving transport errors or credentials.
type Outcome struct {
	Minutes                                *int64
	Miles                                  *float64
	Address, Precision, Status, Why, Error string
	Sent, Failed, CostNanos                int64
}

type answer struct {
	status  int
	body    []byte
	failure string
}

func (c *Client) request(ctx context.Context, method, address string, body []byte) answer {
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, address, bytes.NewReader(body))
	if err != nil {
		return answer{failure: FailTransport}
	}
	if method == http.MethodPost {
		req.Header.Set("X-Goog-Api-Key", c.Key)
		req.Header.Set("X-Goog-FieldMask", "routes.duration,routes.distanceMeters")
		req.Header.Set("Content-Type", "application/json")
	}
	after := c.After
	if after == nil {
		after = time.After
	}
	timer := after(Timeout)
	done := make(chan answer, 1)
	go func() {
		resp, e := c.HTTP.Do(req)
		if e != nil {
			done <- answer{failure: FailTransport}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		data, e := io.ReadAll(resp.Body)
		if e != nil {
			done <- answer{failure: FailTransport}
			return
		}
		done <- answer{status: resp.StatusCode, body: data}
	}()
	select {
	case a := <-done:
		return a
	case <-timer:
		cancel()
		<-done
		return answer{failure: FailTimeout}
	case <-ctx.Done():
		cancel()
		<-done
		return answer{failure: FailCanceled}
	}
}

// Drive geocodes the destination and routes only placements closer than a town.
func (c *Client) Drive(ctx context.Context, origin, destination string) Outcome {
	o := Outcome{}
	if ctx.Err() != nil {
		o.Error = FailCanceled
		return o
	}
	if c.HTTP == nil {
		o.Error = FailTransport
		return o
	}
	address := GeocodeURL + "?" + url.Values{"address": {destination}, "key": {c.Key}}.Encode()
	o.Sent++
	a := c.request(ctx, http.MethodGet, address, nil)
	if a.failure != "" {
		return failed(o, a.failure)
	}
	obj := object(a.body)
	status, ok := obj["status"].(string)
	if ok {
		o.Status = status
	}
	if a.status != http.StatusOK || (ok && status != "OK" && status != "ZERO_RESULTS") {
		return failed(o, FailStatus)
	}
	if !ok {
		return failed(o, FailShape)
	}
	if status == "ZERO_RESULTS" {
		o.Why = WhyNotPlaced
		o.CostNanos = GeocodePriceNanos
		return o
	}
	lat, lng, valid := place(obj, &o)
	if !valid {
		o.Address, o.Precision = "", ""
		return failed(o, FailShape)
	}
	o.CostNanos = GeocodePriceNanos
	if o.Precision == PrecisionTown {
		o.Why = WhyTooCoarse
		return o
	}
	payload, _ := json.Marshal(map[string]any{
		"origin":      map[string]any{"address": origin},
		"destination": map[string]any{"location": map[string]any{"latLng": map[string]any{"latitude": lat, "longitude": lng}}},
		"travelMode":  "DRIVE", "routingPreference": "TRAFFIC_UNAWARE",
	})
	o.Sent++
	a = c.request(ctx, http.MethodPost, RoutesURL, payload)
	if a.failure != "" {
		return failed(o, a.failure)
	}
	obj = object(a.body)
	if a.status != http.StatusOK {
		if detail, yes := obj["error"].(map[string]any); yes {
			if s, yes := detail["status"].(string); yes {
				o.Status = s
			}
		}
		return failed(o, FailStatus)
	}
	sec, meters, found, valid := route(obj)
	if !valid {
		return failed(o, FailShape)
	}
	o.CostNanos += RoutePriceNanos
	if !found {
		o.Why = WhyNoRoute
		return o
	}
	minutes, miles := int64(math.Round(sec/60)), math.Round(meters/1609.344*10)/10
	o.Minutes, o.Miles = &minutes, &miles
	return o
}

func failed(o Outcome, failure string) Outcome { o.Error = failure; o.Failed = 1; return o }
func object(data []byte) map[string]any {
	var obj map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if decoder.Decode(&obj) != nil {
		return nil
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil
	}
	return obj
}
func place(obj map[string]any, o *Outcome) (float64, float64, bool) {
	results, ok := obj["results"].([]any)
	if !ok || len(results) == 0 {
		return 0, 0, false
	}
	first, _ := results[0].(map[string]any)
	address, ok := first["formatted_address"].(string)
	if !ok {
		return 0, 0, false
	}
	geometry, _ := first["geometry"].(map[string]any)
	location, _ := geometry["location"].(map[string]any)
	lat, latOK := number(location["lat"])
	lng, lngOK := number(location["lng"])
	if !latOK || !lngOK || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return 0, 0, false
	}
	switch geometry["location_type"] {
	case "ROOFTOP":
		o.Precision = PrecisionBuilding
	case "RANGE_INTERPOLATED", "GEOMETRIC_CENTER":
		o.Precision = PrecisionStreet
	case "APPROXIMATE":
		o.Precision = PrecisionTown
	default:
		return 0, 0, false
	}
	o.Address = address
	return lat, lng, true
}

var durationPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?s$`)

func route(obj map[string]any) (seconds, meters float64, found, valid bool) {
	if obj == nil {
		return 0, 0, false, false
	}
	raw, exists := obj["routes"]
	if !exists {
		return 0, 0, false, true
	}
	routes, ok := raw.([]any)
	if !ok {
		return 0, 0, false, false
	}
	if len(routes) == 0 {
		return 0, 0, false, true
	}
	first, ok := routes[0].(map[string]any)
	if !ok {
		return 0, 0, false, false
	}
	if raw, exists := first["duration"]; exists {
		text, ok := raw.(string)
		if !ok || !durationPattern.MatchString(text) {
			return 0, 0, false, false
		}
		var err error
		seconds, err = strconv.ParseFloat(strings.TrimSuffix(text, "s"), 64)
		if err != nil || seconds > 1e9 {
			return 0, 0, false, false
		}
	}
	if raw, exists := first["distanceMeters"]; exists {
		meters, ok = number(raw)
		if !ok || meters < 0 || meters > 1e9 || math.Trunc(meters) != meters {
			return 0, 0, false, false
		}
	}
	return seconds, meters, true, true
}

func number(raw any) (float64, bool) {
	n, ok := raw.(json.Number)
	if !ok {
		return 0, false
	}
	value, err := n.Float64()
	return value, err == nil
}

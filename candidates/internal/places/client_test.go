package places_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func client(rt transport) *places.Client {
	return &places.Client{HTTP: &http.Client{Transport: rt}, Key: "synthetic-key", After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
}
func wire(list ...places.Place) string {
	x := make([]map[string]any, len(list))
	for i, p := range list {
		x[i] = map[string]any{"id": p.ID, "displayName": map[string]any{"text": p.Name}, "formattedAddress": p.Address, "primaryType": p.PrimaryType, "websiteUri": p.Website, "nationalPhoneNumber": p.Phone}
	}
	b, _ := json.Marshal(map[string]any{"places": x})
	return string(b)
}
func fullPlaces() []places.Place {
	out := make([]places.Place, places.MaxResults)
	for i := range out {
		out[i] = places.Place{ID: fmt.Sprintf("synthetic-place-%d", i)}
	}
	return out
}
func failedAnswer(word, status string, sent int64) places.Answer {
	return places.Answer{Error: word, Status: status, Sent: sent, Failed: sent}
}
func requireAnswer(t *testing.T, got, want places.Answer) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answer = %#v, want %#v", got, want)
	}
}

// R-ZOAM-Y5DO R-ZPIJ-BX4D R-ZQQF-POV2 R-ZRYC-3GLR R-ZT68-H8CG
func TestPublicContract(t *testing.T) {
	type word string
	type number int32
	type fraction float32
	var names = []word{places.Name, places.NearbyURL, places.FieldMask}
	var counts = []number{places.MaxResults, places.PriceNanos, places.SeedCeiling, places.CeilingMin, places.CeilingMax, places.FakeFew}
	var radii = []fraction{places.MinRadius, places.MaxRadius, places.ChildRatio, places.FakeFullRadius}
	if !reflect.DeepEqual(names, []word{"places", "https://places.googleapis.com/v1/places:searchNearby", "places.id,places.displayName,places.formattedAddress,places.primaryType,places.websiteUri,places.nationalPhoneNumber"}) || !reflect.DeepEqual(counts, []number{20, 35000000, 100, 1, 5000, 3}) || !reflect.DeepEqual(radii, []fraction{50, 50000, .75, 200}) {
		t.Fatal("wire constants")
	}
	const timeout time.Duration = places.Timeout
	if timeout != 10*time.Second {
		t.Fatal("timeout")
	}
	const _ = places.FailTransport + places.FailTimeout + places.FailStatus + places.FailShape + places.FailCanceled
	seen := map[string]bool{}
	for _, s := range []string{places.FailTransport, places.FailTimeout, places.FailStatus, places.FailShape, places.FailCanceled} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal("invalid fail word")
		}
		seen[s] = true
	}
	c := places.Circle{12, 34, 56}
	p := places.Place{"id", "name", "address", "type", "website", "phone"}
	a := places.Answer{[]places.Place{p}, "status", "error", 1, 2, 3}
	f := places.Found{p, c}
	s := places.Sweep{[]places.Found{f}, 1, 2, 3, 4, true, "status", "error"}
	cl := places.Client{nil, "key", nil}
	contract := struct {
		nearby   func(context.Context, places.Circle) places.Answer
		sweep    func(context.Context, places.Circle, int64) places.Sweep
		first    func(float64, float64, float64) places.Circle
		cover    func(places.Circle) []places.Circle
		reading  func(places.Place) sources.Reading
		address  func(string) (string, string, string, string)
		domain   func(string) string
		category func(string) string
		ceiling  func(int64) bool
	}{cl.Nearby, cl.Sweep, places.First, places.Cover, places.Reading, places.Address, places.DomainKey, places.Category, places.ValidCeiling}
	var _ http.RoundTripper = places.Fake{}
	if contract.nearby(context.Background(), c).Error != places.FailTransport || contract.sweep(context.Background(), c, 1).Error != places.FailTransport || contract.first(12, 34, 56) != c || len(contract.cover(c)) != 4 || contract.reading(p).Name != p.Name || contract.domain("") != "" || contract.category("") != "" || contract.ceiling(0) {
		t.Fatal("public function use")
	}
	street, city, state, zip := contract.address("")
	if street != "" || city != "" || state != "" || zip != "" {
		t.Fatal("address signature use")
	}
	if a.Places[0] != p || a.Status != "status" || a.Error != "error" || a.Sent != 1 || a.Failed != 2 || a.CostNanos != 3 || s.Found[0] != f || s.Circles != 1 || s.Requests != 2 || s.Failed != 3 || s.CostNanos != 4 || !s.CeilingReached || s.Status != "status" || s.Error != "error" {
		t.Fatal("public shapes")
	}
}

// R-NTX3-3VZX R-NWCV-VFHB
func TestNoRequest(t *testing.T) {
	cl := client(func(*http.Request) (*http.Response, error) { t.Error("unexpected request"); return response(200, "{}") })
	cl.After = func(time.Duration) <-chan time.Time { t.Error("unexpected timer"); return nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireAnswer(t, cl.Nearby(ctx, places.Circle{}), failedAnswer(places.FailCanceled, "", 0))
	cl.HTTP = nil
	requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), failedAnswer(places.FailTransport, "", 0))
}

// R-ZVM1-8RTU R-ZY1U-0BB8 R-ZZ9Q-E31X
func TestRequest(t *testing.T) {
	c := places.Circle{Lat: 44.25, Lon: -91.75, Radius: 612.5}
	timers, requests := 0, 0
	cl := client(func(r *http.Request) (*http.Response, error) {
		requests++
		if timers != 1 {
			t.Error("timer did not begin with request")
		}
		if r.Method != http.MethodPost || r.URL.String() != places.NearbyURL || r.URL.RawQuery != "" || r.Header.Get("X-Goog-Api-Key") != "synthetic-key" || r.Header.Get("X-Goog-FieldMask") != places.FieldMask || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %#v", r)
		}
		var got any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		want := map[string]any{"locationRestriction": map[string]any{"circle": map[string]any{"center": map[string]any{"latitude": c.Lat, "longitude": c.Lon}, "radius": c.Radius}}, "maxResultCount": float64(20)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body = %#v", got)
		}
		return response(200, "{}")
	})
	cl.After = func(d time.Duration) <-chan time.Time {
		timers++
		if d != places.Timeout {
			t.Error("timer duration")
		}
		return make(chan time.Time)
	}
	a := cl.Nearby(context.Background(), c)
	requireAnswer(t, a, places.Answer{Places: []places.Place{}, Sent: 1, CostNanos: places.PriceNanos})
	if timers != 1 || requests != 1 {
		t.Fatalf("timers=%d requests=%d", timers, requests)
	}
}

// R-00HM-RUSM R-01PJ-5MJB R-WLKP-KHHP
func TestResponseShapes(t *testing.T) {
	p := places.Place{ID: "synthetic-a", Name: "Sample Fabricators", Address: "17 Example Rd, Testville, MN 55123, USA", PrimaryType: "manufacturer", Website: "https://fabricators.test/", Phone: "(218) 555-0123"}
	good := []struct {
		body string
		list []places.Place
	}{
		{"{}", []places.Place{}}, {`{"places":[]}`, []places.Place{}},
		{wire(p), []places.Place{p}},
		{`{"places":[{"id":"one","displayName":{"languageCode":"en"}},{"id":"two"}],"extra":true}`, []places.Place{{ID: "one"}, {ID: "two"}}},
		{wire(fullPlaces()...), fullPlaces()},
	}
	for i, tt := range good {
		t.Run(fmt.Sprintf("good-%d", i), func(t *testing.T) {
			cl := client(func(*http.Request) (*http.Response, error) { return response(200, tt.body) })
			requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), places.Answer{Places: tt.list, Sent: 1, CostNanos: places.PriceNanos})
		})
	}
	bad := []string{"", "not json", "[]", "null", "5", `{} {}`, `{"places":null}`, `{"places":{}}`, `{"places":"x"}`, `{"places":[null]}`, `{"places":[[]]}`, `{"places":[{}]}`, `{"places":[{"id":""}]}`, `{"places":[{"id":7}]}`, `{"places":[{"id":null}]}`, `{"places":[{"id":"a","displayName":null}]}`, `{"places":[{"id":"a","displayName":[]}]}`, `{"places":[{"id":"a","displayName":{"text":5}}]}`}
	for _, field := range []string{"formattedAddress", "primaryType", "websiteUri", "nationalPhoneNumber"} {
		for _, value := range []string{"5", "null", "{}", "[]"} {
			bad = append(bad, `{"places":[{"id":"a","`+field+`":`+value+`}]}`)
		}
	}
	bad = append(bad, wire(append(fullPlaces(), places.Place{ID: "extra"})...))
	for i, body := range bad {
		t.Run(fmt.Sprintf("bad-%d", i), func(t *testing.T) {
			cl := client(func(*http.Request) (*http.Response, error) { return response(200, body) })
			requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), failedAnswer(places.FailShape, "", 1))
		})
	}
}

// R-WLKP-KHHP
func TestRefusalAndSecrecy(t *testing.T) {
	tests := []struct{ body, key, status string }{
		{`{"error":{"status":"PERMISSION_DENIED","message":"synthetic-key"}}`, "synthetic-key", "PERMISSION_DENIED"},
		{`{"error":{"status":"_"}}`, "key", "_"},
		{`{"error":{"status":"` + strings.Repeat("A", 64) + `"}}`, "key", strings.Repeat("A", 64)},
		{`{"error":{"status":"` + strings.Repeat("A", 65) + `"}}`, "key", ""},
		{`{"error":{"status":"PERMISSION_DENIED"}}`, "PERMISSION_DENIED", ""},
		{`{"error":{"status":"bad status"}}`, "key", ""}, {`{"error":{"status":"synthetic-key"}}`, "synthetic-key", ""},
		{`{"error":{"status":"BAD2"}}`, "key", ""}, {`{"error":{"status":""}}`, "key", ""},
		{`{"error":{"status":3}}`, "key", ""}, {`{"error":null}`, "key", ""}, {`[]`, "key", ""}, {"", "key", ""},
		{`{"error":{"status":"DENIED"}} {}`, "key", ""}, {"synthetic-key", "synthetic-key", ""},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			cl := client(func(*http.Request) (*http.Response, error) { return response(403, tt.body) })
			cl.Key = tt.key
			requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), failedAnswer(places.FailStatus, tt.status, 1))
			sw := cl.Sweep(context.Background(), places.Circle{}, 5)
			if sw.Error != places.FailStatus || sw.Status != tt.status || strings.Contains(sw.Error, cl.Key) || strings.Contains(sw.Status, places.NearbyURL) {
				t.Fatalf("sweep = %#v", sw)
			}
		})
	}
	cl := client(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New(r.URL.String() + " synthetic-key")
	})
	requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), failedAnswer(places.FailTransport, "", 1))
	sw := cl.Sweep(context.Background(), places.Circle{}, 5)
	if sw.Error != places.FailTransport || sw.Status != "" {
		t.Fatalf("sweep=%#v", sw)
	}
	// Caller-controlled place fields are preserved, including a matching marker.
	p := places.Place{ID: "synthetic-key", Name: places.NearbyURL, Address: "synthetic-key", PrimaryType: "synthetic-key", Website: places.NearbyURL, Phone: "synthetic-key"}
	cl = client(func(*http.Request) (*http.Response, error) { return response(200, wire(p)) })
	requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), places.Answer{Places: []places.Place{p}, Sent: 1, CostNanos: places.PriceNanos})
}

// R-Y3PK-VAFC
func TestAnswerAndSweepSecrecy(t *testing.T) {
	for _, key := range []string{"zz-test-key-zz", "DENIED", "A"} {
		for _, mode := range []string{"transport", "body-read", "raw-refusal", "status-key", "status-containing-key", "status-url", "shape", "empty", "place-key", "place-url", "timeout", "canceled", "already-canceled"} {
			for _, call := range []string{"nearby", "sweep"} {
				t.Run(key+"/"+mode+"/"+call, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if mode == "already-canceled" {
						cancel()
					}
					timer := make(chan time.Time, 1)
					requestURL := make(chan string, 1)
					cl := client(func(r *http.Request) (*http.Response, error) {
						url := r.URL.String()
						requestURL <- url
						diagnostic := key + " " + url
						switch mode {
						case "transport":
							return nil, errors.New(diagnostic)
						case "body-read":
							return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotest.ErrReader(errors.New(diagnostic)))}, nil
						case "raw-refusal":
							return response(http.StatusForbidden, diagnostic)
						case "status-key", "status-containing-key", "status-url":
							status := key
							switch mode {
							case "status-containing-key":
								status = "PREFIX_" + key + "_SUFFIX"
							case "status-url":
								status = url
							}
							body, err := json.Marshal(map[string]any{"error": map[string]string{"status": status, "message": diagnostic}})
							if err != nil {
								t.Error(err)
							}
							return response(http.StatusForbidden, string(body))
						case "shape":
							return response(http.StatusOK, diagnostic)
						case "place-key", "place-url":
							value := key
							if mode == "place-url" {
								value = url
							}
							return response(http.StatusOK, wire(places.Place{ID: value, Name: value, Address: value, PrimaryType: value, Website: value, Phone: value}))
						case "timeout", "canceled":
							if mode == "timeout" {
								timer <- time.Time{}
							} else {
								cancel()
							}
							<-r.Context().Done()
							return nil, errors.New(diagnostic)
						default:
							return response(http.StatusOK, "{}")
						}
					})
					cl.Key = key
					cl.After = func(time.Duration) <-chan time.Time { return timer }
					var status, failure string
					if call == "nearby" {
						a := cl.Nearby(ctx, places.Circle{})
						status, failure = a.Status, a.Error
					} else {
						s := cl.Sweep(ctx, places.Circle{}, 5)
						status, failure = s.Status, s.Error
					}
					var url string
					select {
					case url = <-requestURL:
					default:
					}
					for _, word := range []string{status, failure} {
						if word == key || (url != "" && word == url) {
							t.Fatal("answer exposed a credential or request URL")
						}
					}
				})
			}
		}
	}
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not complete")
		var zero T
		return zero
	}
}

// R-02XF-JEA0
func TestInFlightFailures(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan context.Context, 1)
			canceled := make(chan struct{})
			release := make(chan struct{})
			timer := make(chan time.Time, 1)
			cl := client(func(r *http.Request) (*http.Response, error) {
				started <- r.Context()
				<-r.Context().Done()
				close(canceled)
				<-release
				return nil, errors.New("injected cancellation")
			})
			cl.After = func(d time.Duration) <-chan time.Time {
				if d != places.Timeout {
					t.Error("timer duration")
				}
				return timer
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan places.Answer, 1)
			go func() { done <- cl.Nearby(ctx, places.Circle{}) }()
			rctx := await(t, started)
			word := places.FailTimeout
			if mode == "timeout" {
				timer <- time.Time{}
			} else {
				cancel()
				word = places.FailCanceled
			}
			await(t, canceled)
			if rctx.Err() == nil {
				t.Error("request context still live")
			}
			select {
			case <-done:
				t.Error("returned before transport returned")
			default:
			}
			close(release)
			requireAnswer(t, await(t, done), failedAnswer(word, "", 1))
		})
	}
	cl := client(func(*http.Request) (*http.Response, error) { return nil, errors.New("injected failure") })
	requireAnswer(t, cl.Nearby(context.Background(), places.Circle{}), failedAnswer(places.FailTransport, "", 1))
}

// R-06L4-OPI3
func TestConcurrentCallsAndDeterministicRequests(t *testing.T) {
	cl := client(func(r *http.Request) (*http.Response, error) {
		var body struct {
			LocationRestriction struct {
				Circle struct{ Center struct{ Latitude float64 } }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		return response(200, wire(places.Place{ID: fmt.Sprint(body.LocationRestriction.Circle.Center.Latitude)}))
	})
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := places.Circle{Lat: float64(i), Radius: 50}
			if i%2 == 0 {
				a := cl.Nearby(context.Background(), c)
				requireAnswer(t, a, places.Answer{Places: []places.Place{{ID: fmt.Sprint(i)}}, Sent: 1, CostNanos: places.PriceNanos})
			} else {
				s := cl.Sweep(context.Background(), c, 10)
				if !reflect.DeepEqual(s, places.Sweep{Found: []places.Found{{Place: places.Place{ID: fmt.Sprint(i)}, Circle: c}}, Circles: 1, Requests: 1, CostNanos: places.PriceNanos}) {
					t.Errorf("sweep=%#v", s)
				}
			}
		}()
	}
	wg.Wait()
	type request struct {
		method, url string
		header      http.Header
		body        string
	}
	var traces [2][]request
	for i := range traces {
		n := 0
		c := client(func(r *http.Request) (*http.Response, error) {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			traces[i] = append(traces[i], request{r.Method, r.URL.String(), r.Header.Clone(), string(b)})
			n++
			if n == 1 {
				return response(200, wire(fullPlaces()...))
			}
			return response(200, "{}")
		})
		c.Sweep(context.Background(), places.Circle{Lat: 44, Lon: -92, Radius: 600}, 100)
	}
	if len(traces[0]) != 5 || !reflect.DeepEqual(traces[0], traces[1]) {
		t.Fatal("request traces differ")
	}
}

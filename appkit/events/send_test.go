package events

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type deliveryTransport func(*http.Request) (*http.Response, error)

func (f deliveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

// R-GBCK-DQND R-XT58-KHC0
func TestSendRejectsBeforeTransport(t *testing.T) {
	func() {
		defer func() {
			p := recover()
			if p == nil || !strings.Contains(p.(string), "client is nil") {
				t.Error(p)
			}
		}()
		_, _ = Send(context.Background(), nil, "x", Delivery{})
	}()
	calls := 0
	c := &http.Client{Transport: deliveryTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })}
	invalid := deliveredFixture()
	invalid.Seq = 0
	huge := deliveredFixture()
	huge.Attrs = Attrs{"x": strings.Repeat("x", MaxDeliveryBytes)}
	for _, d := range []Delivery{{invalid, 1}, {deliveredFixture(), 0}, {huge, 1}} {
		r, e := Send(context.Background(), c, "%", d)
		if r != (Result{}) || !errors.Is(e, ErrRejected) {
			t.Fatal(r, e)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

// R-FGV2-TH40 R-FD7D-O5VX R-XRXC-6PLB R-XUD4-Y92P R-XWSX-PSK3
func TestSendRequest(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "value"), time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	defer cancel()
	d := Delivery{deliveredFixture(), 17}
	e := d.Event
	want, _ := json.Marshal(struct {
		ID        string         `json:"id"`
		Time      string         `json:"time"`
		Service   string         `json:"service"`
		Event     string         `json:"event"`
		RequestID string         `json:"request_id"`
		User      string         `json:"user"`
		Attrs     map[string]any `json:"attrs"`
		Cause     string         `json:"cause"`
		Depth     int            `json:"depth"`
		Seq       int64          `json:"seq"`
		Received  string         `json:"received"`
		Attempt   int            `json:"attempt"`
	}{e.ID, e.Time.UTC().Format("2006-01-02T15:04:05.000000Z"), e.Service, e.Name, e.RequestID, e.User, e.Attrs, e.Cause, e.Depth, e.Seq, e.Received.UTC().Format("2006-01-02T15:04:05.000000Z"), 17})
	calls := 0
	fail := errors.New("failure")
	c := &http.Client{Transport: deliveryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		b, _ := io.ReadAll(r.Body)
		if string(b) != string(want) || r.URL.String() != "http://consumer/events" || r.Method != "POST" || !reflect.DeepEqual(r.Header.Values("Content-Type"), []string{"application/json"}) {
			t.Error(r, string(b))
		}
		a, _ := ctx.Deadline()
		z, _ := r.Context().Deadline()
		if a != z || r.Context().Value(key{}) != "value" {
			t.Error("context")
		}
		cancel()
		return nil, fail
	})}
	r, err := Send(ctx, c, "consumer", d)
	if r != (Result{}) || !errors.Is(err, fail) || !errors.Is(err, context.Canceled) || errors.Is(err, ErrRejected) || calls != 1 {
		t.Fatal(r, err, calls)
	}
	r, err = Send(context.Background(), c, "%", d)
	if r != (Result{}) || err == nil || errors.Is(err, ErrRejected) || calls != 1 {
		t.Fatal(r, err, calls)
	}
}

// R-XVL1-C0TE R-XY0U-3KAS R-Y0GM-V3S6 R-Y1OJ-8VIV R-Y2WF-MN9K R-GNJK-7G2B
func TestSendResponses(t *testing.T) {
	type testcase struct {
		s        int
		b, retry string
		o        Outcome
		delay    time.Duration
		valid    bool
	}
	cases := []testcase{{200, `{"outcome":"ok"}`, "", OK(), 0, true}, {200, `{"outcome":"skip"}`, "", Skip(), 0, true}, {500, `{"outcome":"error","error":"message"}`, "", Fail("message"), 0, true}, {500, `{"outcome":"error","error":"\ud800"}`, "", Fail("�"), 0, true}, {200, `{"\u006futcome":"\u006fk"}`, "", OK(), 0, true}, {404, "invalid", "10", Skip(), 0, true}}
	for _, b := range []string{`{"outcome":"error"}`, `{"outcome":"ok","extra":1}`, `{"outcome":"ok","\u006futcome":"ok"}`, `{"outcome":"ok"} {}`, `["ok"]`, `{"outcome":null}`, "\xff", strings.Repeat(" ", MaxEventBytes) + `{"outcome":"ok"}`} {
		cases = append(cases, testcase{200, b, "", Outcome{}, 0, false})
	}
	for _, b := range []string{`{"outcome":"error"}`, `{"outcome":"error","error":null}`, `{"outcome":"error","error":1}`, `{"outcome":"ok","error":""}`, `{"outcome":"error","error":"","extra":1}`, `{"outcome":"error","error":"a","error":"b"}`} {
		cases = append(cases, testcase{500, b, "", Outcome{}, 0, false})
	}
	for _, s := range []int{429, 503, 415} {
		for _, v := range []string{"0", "12", "00012", "184467440737095516160", "9223372037", "Wed, 21 Oct 2015 07:28:00 GMT", "-1", "+2", " 2", "2 ", "1.5", ""} {
			delay := time.Duration(0)
			if s != 415 {
				switch v {
				case "12", "00012":
					delay = 12 * time.Second
				case "184467440737095516160", "9223372037":
					delay = time.Duration(math.MaxInt64)
				}
			}
			cases = append(cases, testcase{s, "", v, Outcome{}, delay, false})
		}
	}
	cases = append(cases, testcase{200, strings.Repeat(" ", MaxEventBytes-len(`{"outcome":"ok"}`)) + `{"outcome":"ok"}`, "", OK(), 0, true})
	for _, tc := range cases {
		body := &trackedBody{Reader: strings.NewReader(tc.b)}
		c := &http.Client{Transport: deliveryTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.s, Header: http.Header{"Retry-After": []string{tc.retry, "99"}, "Content-Type": []string{"text/plain"}}, Body: body}, nil
		})}
		r, e := Send(context.Background(), c, "consumer", Delivery{deliveredFixture(), 1})
		if (e == nil) != tc.valid || errors.Is(e, ErrRejected) || r != (Result{tc.s, tc.o, tc.delay}) || !body.closed {
			t.Fatalf("case %+v result %+v error %v closed %v", tc, r, e, body.closed)
		}
	}
}

// R-GORG-L7T0
func TestSendConcurrent(t *testing.T) {
	c := &http.Client{Transport: deliveryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"outcome":"ok"}`))}, nil
	})}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			r, e := Send(context.Background(), c, "consumer", Delivery{deliveredFixture(), 1})
			if e != nil || r.Outcome != OK() {
				t.Error(r, e)
			}
		})
	}
	wg.Wait()
}

// R-GNJK-7G2B R-XWSX-PSK3
func TestSendRedirectErrorClosesBody(t *testing.T) {
	body := &trackedBody{Reader: strings.NewReader("redirect")}
	stop := errors.New("stop redirect")
	c := &http.Client{Transport: deliveryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://consumer/elsewhere"}}, Body: body}, nil
	}), CheckRedirect: func(*http.Request, []*http.Request) error { return stop }}
	result, err := Send(context.Background(), c, "consumer", Delivery{deliveredFixture(), 1})
	if result != (Result{}) || !errors.Is(err, stop) || errors.Is(err, ErrRejected) || !body.closed {
		t.Fatal(result, err, body.closed)
	}
}

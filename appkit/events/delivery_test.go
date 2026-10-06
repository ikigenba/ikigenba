package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func deliveredFixture() Event {
	return Event{ID: "evt_0123456789abcdef", Time: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Service: "repos", Name: "repo.pushed", Attrs: Attrs{}, Seq: 1, Received: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC)}
}
func deliveryRequest(h http.Handler, method, content, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, EventsPath, strings.NewReader(body))
	if content != "" {
		r.Header.Set("Content-Type", content)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func fixtureBody(t *testing.T) string {
	t.Helper()
	b, err := deliveryBody(Delivery{deliveredFixture(), 1})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// R-F3G6-LZYD R-F4O2-ZRP2 R-F5VZ-DJFR R-F73V-RB6G R-F8BS-52X5 R-F9JO-IUNU R-FARK-WMEJ R-FBZH-AE58
// R-FI2Z-78UP R-FJAV-L0LE R-FKIR-YSC3 R-FMYK-QBTH R-FO6H-43K6
func TestOutcomeValues(t *testing.T) {
	typ := reflect.TypeOf(Outcome{})
	for i := 0; i < typ.NumField(); i++ {
		if typ.Field(i).PkgPath == "" {
			t.Fatal("exported Outcome field", typ.Field(i).Name)
		}
	}
	var zero Outcome
	firstOK, firstSkip, firstFail := OK(), Skip(), Fail("a")
	if zero != Fail("") || firstOK != OK() || firstSkip != Skip() || firstFail != Fail("a") || Fail("a") == Fail("b") {
		t.Fatal("comparison")
	}
	for _, o := range []Outcome{OK(), Skip(), Fail("a")} {
		if o == OK() && o == Skip() || o == OK() && o == Fail(o.Message()) || o == Skip() && o == Fail(o.Message()) {
			t.Fatal("distinct kinds")
		}
	}
	for _, tc := range []struct {
		o    Outcome
		k, m string
	}{{OK(), "ok", ""}, {Skip(), "skip", ""}, {Fail("msg"), "error", "msg"}, {zero, "error", ""}} {
		if tc.o.Kind() != tc.k || tc.o.Message() != tc.m {
			t.Fatal(tc)
		}
	}
	type outcomeText string
	var a outcomeText = OutcomeOK
	var b outcomeText = OutcomeSkip
	var c outcomeText = OutcomeError
	if a != "ok" || b != "skip" || c != "error" || PanicMessage != "event handler panicked" {
		t.Fatal("constants")
	}
	type deliveryLimit int64
	var limit deliveryLimit = MaxDeliveryBytes
	if limit != 131072 {
		t.Fatal(limit)
	}
}

// R-FPED-HVAV R-FQM9-VN1K R-FEFA-1XMM R-FFN6-FPDB
func TestHandlerRegistration(t *testing.T) {
	for _, makeHandler := range []func(Handlers) http.Handler{DeliveryHandler, func(h Handlers) http.Handler { return DeclarationsHandler(nil, h) }} {
		for _, h := range []Handlers{nil, {}, {"*": func(context.Context, Delivery) Outcome { return OK() }}} {
			makeHandler(h)
		}
		for _, key := range []string{"bad\"name", "repo.pushed"} {
			func() {
				defer func() {
					p := recover()
					if p == nil || !strings.Contains(fmt.Sprint(p), strconv.Quote(key)) {
						t.Errorf("panic %v", p)
					}
				}()
				handler := Handler(nil)
				if key != "repo.pushed" {
					handler = func(context.Context, Delivery) Outcome { return OK() }
				}
				makeHandler(Handlers{key: handler})
			}()
		}
	}
	h := Handlers{"repo.pushed": func(context.Context, Delivery) Outcome { return OK() }}
	delivery := DeliveryHandler(h)
	declarations := DeclarationsHandler(nil, h)
	h["repo.pushed"] = func(context.Context, Delivery) Outcome { return Skip() }
	h["new.event"] = h["repo.pushed"]
	if w := deliveryRequest(delivery, "POST", "application/json", fixtureBody(t)); w.Body.String() != `{"outcome":"ok"}` {
		t.Fatal("replacement changed snapshot")
	}
	delete(h, "repo.pushed")
	if w := deliveryRequest(delivery, "POST", "application/json", fixtureBody(t)); w.Body.String() != `{"outcome":"ok"}` {
		t.Fatal(w.Body.String())
	}
	if w := deliveryRequest(declarations, "GET", "", ""); w.Body.String() != `{"emits":[],"accepts":["repo.pushed"]}` {
		t.Fatal(w.Body.String())
	}
}

// R-FRU6-9ES9 R-FT22-N6IY R-FU9Z-0Y9N R-FVHV-EQ0C R-FWPR-SHR1 R-FXXO-69HQ
func TestDeliveryValidation(t *testing.T) {
	calls := 0
	h := DeliveryHandler(Handlers{"repo.pushed": func(context.Context, Delivery) Outcome { calls++; return OK() }})
	good := fixtureBody(t)
	cases := []struct {
		method, ct, body string
		status           int
	}{{"GET", "", good, 405}, {"POST", "", good, 415}, {"POST", "text/plain", good, 415}, {"POST", "application/json; bad", good, 415}, {"POST", "application/json", strings.Repeat(" ", MaxDeliveryBytes+1), 413}, {"POST", "application/json", "{}", 400}}
	for _, bad := range []string{strings.Replace(good, `"attempt":1`, `"attempt":0`, 1), strings.Replace(good, `"attempt":1`, `"attempt":1.0`, 1), strings.Replace(good, `"attempt":1`, `"attempt":"1"`, 1), strings.Replace(good, `"attempt":1`, `"attempt":1,"\u0061ttempt":2`, 1), good + "{}", strings.Replace(good, `"attrs":{}`, `"attrs":null`, 1), strings.Replace(good, `"time":"2026-01-02T03:04:05.000000Z"`, `"time":"yesterday"`, 1), strings.Replace(good, `"user":"",`, "", 1), strings.Replace(good, `"attempt":1`, `"attempt":1,"extra":true`, 1), strings.Replace(strings.Replace(good, `,"seq":1`, "", 1), `,"received":"2026-01-02T03:04:06.000000Z"`, "", 1), "[" + good + "]", strings.Replace(good, `"attrs":{}`, `"attrs":{"x":1,"x":2}`, 1), strings.Replace(good, `"service":"repos"`, "\"service\":\"\xff\"", 1), strings.Replace(good, `,"attempt":1`, "", 1), strings.Replace(good, `"seq":1`, `"seq":0`, 1)} {
		cases = append(cases, struct {
			method, ct, body string
			status           int
		}{"POST", "application/json", bad, 400})
	}
	for _, tc := range cases {
		w := deliveryRequest(h, tc.method, tc.ct, tc.body)
		if w.Code != tc.status || w.Body.Len() != 0 {
			t.Errorf("%d got %d %s", tc.status, w.Code, w.Body.String())
		}
		if tc.status == 405 && !reflect.DeepEqual(w.Header().Values("Allow"), []string{"POST"}) {
			t.Fatal(w.Header())
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
	for _, body := range []string{good, strings.Repeat(" ", MaxDeliveryBytes-len(good)) + good, strings.Replace(good, `"attempt":1`, `"\u0061ttempt":2`, 1)} {
		if w := deliveryRequest(h, "POST", "application/json; charset=utf-8", body); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := deliveryRequest(DeliveryHandler(nil), "POST", "application/json", good); w.Code != 404 || w.Body.Len() != 0 {
		t.Fatal(w)
	}
	wild := DeliveryHandler(Handlers{"*": func(context.Context, Delivery) Outcome { return Skip() }})
	if w := deliveryRequest(wild, "POST", "application/json", good); w.Body.String() != `{"outcome":"skip"}` || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
		t.Fatal(w)
	}
}

// R-EYKL-2WZL R-EZSH-GOQA R-F28A-887O R-FZ5K-K18F R-G0DG-XSZ4 R-G1LD-BKPT R-TLCA-5EFJ R-TMK6-J668
func TestDeliveryInvocationAndOutcomes(t *testing.T) {
	type key struct{}
	ctx, cancel := context.WithDeadline(context.WithValue(context.Background(), key{}, "value"), time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	defer cancel()
	var expected Event
	b := fixtureBody(t)
	raw := strings.Replace(b, `,"attempt":1`, "", 1)
	if err := json.Unmarshal([]byte(raw), &expected); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var f Handler = func(c context.Context, d Delivery) Outcome {
		calls++
		if !reflect.DeepEqual(d.Event, expected) || d.Attempt != 1 {
			t.Error(d)
		}
		cause, ok := FromContext(c)
		if !ok || cause != (Cause{expected.ID, expected.Depth}) || c.Value(key{}) != "value" {
			t.Error("context")
		}
		a, _ := ctx.Deadline()
		z, _ := c.Deadline()
		if a != z {
			t.Error("deadline")
		}
		cancel()
		if c.Err() != context.Canceled {
			t.Error("cancellation")
		}
		return OK()
	}
	h := DeliveryHandler(Handlers{"repo.pushed": f, "*": func(context.Context, Delivery) Outcome { t.Error("wildcard called"); return Skip() }})
	r := httptest.NewRequest("POST", EventsPath, strings.NewReader(b)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if calls != 1 || w.Code != 200 || w.Body.String() != `{"outcome":"ok"}` || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
		t.Fatal(w, calls)
	}
	for _, msg := range []string{"", "failure", strings.Repeat("x", MaxEventBytes/8-1) + "€end", strings.Repeat("\x00", MaxEventBytes)} {
		h := DeliveryHandler(Handlers{"*": func(context.Context, Delivery) Outcome { return Fail(msg) }})
		w := deliveryRequest(h, "POST", "application/json", b)
		m := msg
		// Derive the character-safe prefix independently through rune iteration.
		if len(msg) > MaxEventBytes/8 {
			end := 0
			for i := range msg {
				if i > MaxEventBytes/8 {
					break
				}
				end = i
			}
			m = msg[:end]
		}
		want, _ := json.Marshal(struct {
			Outcome string `json:"outcome"`
			Error   string `json:"error"`
		}{"error", m})
		if w.Code != 500 || w.Body.String() != string(want) || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
			t.Fatal(w.Code, w.Body.String(), string(want))
		}
	}
	count := 0
	panicky := DeliveryHandler(Handlers{"*": func(context.Context, Delivery) Outcome {
		count++
		if count == 1 {
			panic("private")
		}
		return Skip()
	}})
	if w := deliveryRequest(panicky, "POST", "application/json", b); w.Code != 500 || w.Body.String() != `{"outcome":"error","error":"event handler panicked"}` {
		t.Fatal(w)
	}
	if w := deliveryRequest(panicky, "POST", "application/json", b); w.Code != 200 || w.Body.String() != `{"outcome":"skip"}` || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"application/json"}) {
		t.Fatal(w)
	}
}

// R-G592-GVXW R-GA4N-ZYWO R-G6GY-UNOL R-G8WR-M75Z
func TestDeclarationsAndConcurrentHandlers(t *testing.T) {
	h := Handlers{"*": func(_ context.Context, d Delivery) Outcome {
		if d.Event.Name != "repo.pushed" {
			t.Error(d)
		}
		return OK()
	}, "z.event": func(context.Context, Delivery) Outcome { return Skip() }}
	dec := DeclarationsHandler(nil, h)
	for _, method := range []string{"POST", "HEAD"} {
		w := deliveryRequest(dec, method, "", "")
		if w.Code != 405 || w.Body.Len() != 0 || !reflect.DeepEqual(w.Header().Values("Allow"), []string{"GET"}) {
			t.Fatal(w)
		}
	}
	body := fixtureBody(t)
	dh := DeliveryHandler(h)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			w := deliveryRequest(dec, "GET", "", "")
			if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != `{"emits":[],"accepts":["*","z.event"]}` {
				t.Error(w)
			}
			if w := deliveryRequest(dh, "POST", "application/json", body); w.Code != 200 {
				t.Error(w)
			}
		})
	}
	wg.Wait()
}

// R-G8WR-M75Z
func TestDeclarationsEmissions(t *testing.T) {
	em := New(Config{Service: "test", Stderr: io.Discard, Now: func() time.Time { return deliveredFixture().Time }, Rand: strings.NewReader(strings.Repeat("a", 128)), Sleep: func(context.Context, time.Duration) {}, Emits: []Emission{{Event: "z.event", Attrs: []string{"second", "first"}}, {Event: "a.event"}}})
	t.Cleanup(func() {
		em.Shutdown(context.Background())
	})
	w := deliveryRequest(DeclarationsHandler(em, nil), "GET", "", "")
	if w.Body.String() != `{"emits":[{"event":"a.event","attrs":[]},{"event":"z.event","attrs":["second","first"]}],"accepts":[]}` {
		t.Fatal(w.Body.String())
	}
}

// R-G592-GVXW
func TestConcurrentDeliveryIsolation(t *testing.T) {
	type key struct{}
	h := DeliveryHandler(Handlers{"*": func(ctx context.Context, d Delivery) Outcome {
		if d.Attempt != ctx.Value(key{}).(int) || d.Event.RequestID != strconv.Itoa(d.Attempt) {
			t.Error("request contamination", d)
		}
		cause, ok := FromContext(ctx)
		if !ok || cause.ID != d.Event.ID {
			t.Error("cause", cause)
		}
		return OK()
	}})
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Go(func() {
			e := deliveredFixture()
			e.RequestID = strconv.Itoa(i)
			e.ID = fmt.Sprintf("evt_%016x", i)
			b, err := deliveryBody(Delivery{e, i})
			if err != nil {
				t.Error(err)
				return
			}
			r := httptest.NewRequest("POST", EventsPath, strings.NewReader(string(b))).WithContext(context.WithValue(context.Background(), key{}, i))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Error(w)
			}
		})
	}
	wg.Wait()
}

// R-TLCA-5EFJ
func TestDeliveryZeroOutcome(t *testing.T) {
	h := DeliveryHandler(Handlers{"*": func(context.Context, Delivery) Outcome { return Outcome{} }})
	w := deliveryRequest(h, "POST", "application/json", fixtureBody(t))
	if w.Code != 500 || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != `{"outcome":"error","error":""}` {
		t.Fatal(w)
	}
}

package broker_test

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
)

func TestDisconnect(t *testing.T) {
	// R-R1KN-5PEZ R-R40F-X8WD
	for _, status := range []int{200, 400, 502} {
		t.Run(time.Duration(status).String(), func(t *testing.T) {
			f := setup(t)
			c := f.seed(t, f.now.Add(time.Hour))
			f.status = status
			got, err := f.b.Disconnect(f.ctx, "google")
			if err != nil || !reflect.DeepEqual(got, c) || len(f.forms) != 1 || f.forms[0].Get("token") != c.RefreshToken {
				t.Fatal(got, err)
			}
			_, err = f.st.Get(f.ctx, "user", "google")
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatal("not deleted")
			}
			f.flush(t)
			if status == 200 {
				f.assertEvents(t, trail.Deleted, c)
			}
			if len(f.ec.Events()) != 1 || f.ec.Events()[0].Name != trail.Deleted {
				t.Fatal("bus")
			}
			n := 1
			if status != 200 {
				n = 2
				if f.tc.Events()[0].Name != trail.RevokeFailed || f.tc.Events()[0].Attrs["status"] != int64(status) || f.tc.Events()[0].User != "user" || f.tc.Events()[0].RequestID != "request" || !reflect.DeepEqual(f.tc.Events()[0].Attrs, telemetry.Attrs{"provider": "google", "account": c.AccountEmail, "status": int64(status)}) {
					t.Fatal("failure missing")
				}
			}
			if len(f.tc.Events()) != n || f.tc.Events()[n-1].Name != trail.Deleted {
				t.Fatal("trail")
			}
		})
	}
}
func TestDisconnectFailures(t *testing.T) {
	// R-R58C-B0N2 R-R40F-X8WD
	for _, kind := range []string{"unknown", "missing", "foreign", "read", "delete", "delete-refused"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			c := f.seed(t, f.now.Add(time.Hour))
			p := "google"
			ctx := f.ctx
			want := store.ErrUnreachable
			switch kind {
			case "unknown":
				p = "nope"
				want = broker.ErrUnknownProvider
			case "missing":
				p = "other"
				f.cfg.Clients[0].Provider.Name = p
				f.b = broker.New(f.cfg)
				want = store.ErrNotFound
			case "foreign":
				ctx = identity.NewContext(ctx, identity.Caller{UserID: "other"})
				want = store.ErrNotFound
			case "read":
				f.d.SetFailing(true)
			case "delete":
				f.hook = func() { f.d.SetFailing(true) }
			case "delete-refused":
				f.status = 400
				f.hook = func() { f.d.SetFailing(true) }
			}
			got, err := f.b.Disconnect(ctx, p)
			if !errors.Is(err, want) || !reflect.DeepEqual(got, store.Connection{}) {
				t.Fatal(got, err)
			}
			f.d.SetFailing(false)
			held, _ := f.st.Get(f.ctx, "user", "google")
			if !reflect.DeepEqual(c, held) {
				t.Fatal("changed")
			}
			f.flush(t)
			if len(f.ec.Events()) != 0 {
				t.Fatal("bus")
			}
			n := 0
			if kind == "delete-refused" {
				n = 1
			}
			if len(f.tc.Events()) != n {
				t.Fatal("trail")
			}
			if kind != "delete" && kind != "delete-refused" && len(f.forms) != 0 {
				t.Fatal("provider called")
			}
		})
	}
}

func TestDisconnectUnreachableProvider(t *testing.T) {
	// R-R40F-X8WD
	f := setup(t)
	c := f.seed(t, f.now.Add(time.Hour))
	f.cfg.Clients[0].Provider.RevokeURL = "http://provider.invalid/revoke"
	f.cfg.Clients[0].HTTP = &http.Client{Transport: failedTransport{}}
	got, err := broker.New(f.cfg).Disconnect(f.ctx, "google")
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatal(got, err)
	}
	f.flush(t)
	ev := f.tc.Events()
	if len(ev) != 2 || ev[0].Name != trail.RevokeFailed || ev[0].Attrs["status"] != int64(0) || len(f.ec.Events()) != 1 {
		t.Fatal(ev)
	}
}

type failedTransport struct{}

func (failedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("test transport unavailable")
}

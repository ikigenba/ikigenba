package trail

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/connections/internal/store"
)

// R-A9R2-BZGJ R-AAYY-PR78
func TestDeclarations(t *testing.T) {
	want := []events.Emission{{Event: "connection.created", Attrs: []string{"provider", "account", "scopes"}}, {Event: "connection.deleted", Attrs: []string{"provider", "account", "scopes"}}, {Event: "connection.broken", Attrs: []string{"provider", "account", "scopes"}}}
	if Created != want[0].Event || Deleted != want[1].Event || Broken != want[2].Event || RevokeFailed != "connection.revoke_failed" {
		t.Fatal("names")
	}
	got := Emits()
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	got[0].Event = "changed"
	got[1].Attrs[0] = "changed"
	if !reflect.DeepEqual(Emits(), want) {
		t.Fatal("shared declarations")
	}
}

// R-AC6V-3IXX R-ADER-HAOM R-AFUK-8U60
func TestRecords(t *testing.T) {
	for _, identified := range []bool{false, true} {
		for _, scopes := range [][]string{nil, {"scope-two", "scope-one"}} {
			t.Run(strings.Join(scopes, "-")+strconv.FormatBool(identified), func(t *testing.T) {
				now := func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }
				tc := &telemetry.Capture{}
				ec := &events.Capture{}
				w := telemetry.New(telemetry.Config{Service: "connections", Sink: tc, Now: now, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096))})
				em := events.New(events.Config{Service: "connections", Sink: ec, Emits: Emits(), Now: now, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096))})
				defer w.Shutdown(context.Background(), "")
				defer em.Shutdown(context.Background())
				ctx := context.Background()
				caller := identity.Caller{}
				cause := events.Cause{}
				if identified {
					caller = identity.Caller{UserID: "owner", Email: "private-user@example.test", RequestID: "req-example"}
					cause = events.Cause{ID: "evt_0123456789abcdef", Depth: 2}
					ctx = identity.NewContext(ctx, caller)
					ctx = events.NewContext(ctx, cause)
				}
				c := store.Connection{ID: "private-id", UserID: "owner", UserEmail: "private-user@example.test", Provider: "provider-name", AccountEmail: "account@example.test", AccountSubject: "private-subject", AccessToken: "private-access-token", RefreshToken: "private-refresh-token", Scopes: scopes}
				for _, name := range []string{Created, Deleted, Broken} {
					Record(ctx, w, em, name, c)
				}
				Record(ctx, w, em, RevokeFailed, c)
				Record(ctx, w, em, "ignored", c)
				RevokeFailure(ctx, w, c, 400)
				RevokeFailure(ctx, w, c, 0)
				if err := w.Flush(ctx); err != nil {
					t.Fatal(err)
				}
				if err := em.Flush(ctx); err != nil {
					t.Fatal(err)
				}
				ts, es := tc.Events(), ec.Events()
				if len(ts) != 5 || len(es) != 3 {
					t.Fatalf("counts %d %d", len(ts), len(es))
				}
				for i, name := range []string{Created, Deleted, Broken} {
					attrs := telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(scopes, " ")}
					if ts[i].Name != name || !reflect.DeepEqual(ts[i].Attrs, attrs) || ts[i].RequestID != caller.RequestID || ts[i].User != caller.UserID {
						t.Fatal(ts[i])
					}
					if es[i].Name != name || !reflect.DeepEqual(es[i].Attrs, events.Attrs(attrs)) || es[i].RequestID != caller.RequestID || es[i].User != caller.UserID || es[i].Cause != cause.ID {
						t.Fatal(es[i])
					}
					depth := 0
					if identified {
						depth = cause.Depth + 1
					}
					if es[i].Depth != depth {
						t.Fatal(es[i])
					}
				}
				for i, status := range []int64{400, 0} {
					e := ts[i+3]
					if e.Name != RevokeFailed || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "status": status}) || e.User != caller.UserID || e.RequestID != caller.RequestID {
						t.Fatal(e)
					}
				}
			})
		}
	}
}

package settings_test

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/connections/internal/settings"
)

func lookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// R-07ZY-FFRF R-0BNN-KQZI R-0K6Y-956D
func TestDefaultsDrain(t *testing.T) {
	if got := settings.Defaults(); got != (settings.Settings{DrainSeconds: 5}) {
		t.Fatal(got)
	}
	for _, n := range []int64{1, 5, math.MaxInt64 / int64(time.Second), math.MaxInt64} {
		want := time.Duration(math.MaxInt64)
		if n <= math.MaxInt64/int64(time.Second) {
			want = time.Duration(n) * time.Second
		}
		if got := (settings.Settings{DrainSeconds: n}).Drain(); got != want {
			t.Fatalf("%d: %v", n, got)
		}
	}
}

// R-097U-T7I4 R-0CVJ-YIQ7
func TestOrigins(t *testing.T) {
	yes := []string{"http://connections.wip.localhost:7400", "http://localhost:7400", "https://connections.us.ikigenba.dev", "https://localhost:80", "http://localhost:443", "http://localhost:1", "http://localhost:65535"}
	no := []string{"", "http://localhost:7400/", "http://localhost:7400/google/callback", "http://localhost:7400?x=1", "http://localhost:7400#x", "http://u@localhost:7400", " http://localhost:7400", "http://", "http://localhost:", "http://localhost:x", "ftp://localhost", "HTTP://localhost", "http://a.example.:7400", "http://.a.localhost:7400", "http://a..localhost:7400", "http://Connections.localhost:7400", "http://localhost:07400", "http://localhost:0", "http://localhost:65536", "http://localhost:80", "https://localhost:443", "http://a:1:2", "http://é", "http://[::1]", "http://a:١"}
	for _, s := range yes {
		if !settings.IsOrigin(s) {
			t.Error(s)
		}
	}
	for _, s := range no {
		if settings.IsOrigin(s) {
			t.Error(s)
		}
	}
}

// R-0FBC-Q27L R-0GJ9-3TYA R-0HR5-HLOZ R-0IZ1-VDFO
func TestRead(t *testing.T) {
	for _, s := range []string{"0", "00", "-1", "+5", "2.5", "5s", "05", " 5", "5 ", "abc", "١"} {
		_, e := settings.Read(lookup(map[string]string{"DRAIN_SECONDS": s, "IKIGENBA_CALLBACK_URL": "bad"}))
		var se *settings.Error
		if !errors.As(e, &se) || se.Name != "DRAIN_SECONDS" || se.Value != s || se.Unit != "seconds" || e.Error() != "DRAIN_SECONDS is '"+s+"', not a positive whole number of seconds" {
			t.Fatalf("%q: %v", s, e)
		}
	}
	for _, tc := range []struct {
		s string
		n int64
	}{{"", 5}, {"1", 1}, {"19", 19}, {"9223372036854775807", math.MaxInt64}, {"9223372036854775808", math.MaxInt64}, {"99999999999999999999999", math.MaxInt64}} {
		calls := []string{}
		got, e := settings.Read(func(k string) (string, bool) {
			calls = append(calls, k)
			switch k {
			case "DRAIN_SECONDS":
				return tc.s, true
			case "IKIGENBA_CALLBACK_URL":
				return "https://origin.example:123", true
			default:
				t.Fatalf("unexpected %s", k)
				return "", false
			}
		})
		if e != nil || got != (settings.Settings{DrainSeconds: tc.n, CallbackURL: "https://origin.example:123"}) || !reflect.DeepEqual(calls, []string{"DRAIN_SECONDS", "IKIGENBA_CALLBACK_URL"}) {
			t.Fatalf("%q: %v %v %v", tc.s, got, e, calls)
		}
	}
	got, e := settings.Read(lookup(nil))
	if e != nil || got != settings.Defaults() {
		t.Fatal(got, e)
	}
	for _, s := range []string{"", "http://localhost:7400/", "bad"} {
		_, e := settings.Read(lookup(map[string]string{"IKIGENBA_CALLBACK_URL": s}))
		var oe *settings.OriginError
		if !errors.As(e, &oe) || oe.Name != "IKIGENBA_CALLBACK_URL" || oe.Value != s || e.Error() != "IKIGENBA_CALLBACK_URL is '"+s+"', not an origin" {
			t.Fatal(e)
		}
	}
	se := &settings.Error{Name: "n", Value: "v", Unit: "u"}
	if se.Error() != "n is 'v', not a positive whole number of u" {
		t.Fatal(se)
	}
	oe := &settings.OriginError{Name: "n", Value: "v"}
	if oe.Error() != "n is 'v', not an origin" {
		t.Fatal(oe)
	}
}

// R-0AFR-6Z8T R-0LEU-MWX2 R-0MMR-0ONR
func TestSecrets(t *testing.T) {
	names := []string{"first", "second"}
	m := map[string]string{"first": " leading\nsecret ", "second": "other", "unasked": "value"}
	got, e := settings.Secrets(lookup(m), names)
	if e != nil || !reflect.DeepEqual(got, map[string]string{"first": m["first"], "second": m["second"]}) {
		t.Fatal(got, e)
	}
	got, e = settings.Secrets(lookup(nil), nil)
	if e != nil || got == nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	for _, m := range []map[string]string{nil, {"first": ""}, {"first": "yes"}, {"first": "yes", "second": ""}} {
		got, e := settings.Secrets(lookup(m), names)
		want := "first"
		if m["first"] != "" {
			want = "second"
		}
		var me *settings.MissingError
		if got != nil || !errors.As(e, &me) || me.Name != want || e.Error() != want+" is not set" {
			t.Fatal(got, e)
		}
	}
}

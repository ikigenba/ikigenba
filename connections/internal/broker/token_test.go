package broker_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
)

func TestFreshToken(t *testing.T) {
	// R-SBCH-VKEY R-SCKE-9C5N
	f := setup(t)
	c := f.seed(t, f.now.Truncate(time.Second).Add(broker.RefreshMargin))
	tok, err := f.b.Token(f.ctx, "google")
	if err != nil || tok.AccessToken != c.AccessToken || tok.Expires != c.AccessExpires.UTC().Format(time.RFC3339) || tok.Account != c.AccountEmail || !reflect.DeepEqual(tok.Scopes, c.Scopes) || tok.Scopes == nil || len(f.forms) != 0 {
		t.Fatal(tok, err)
	}
	got, _ := f.st.Get(f.ctx, "user", "google")
	if !reflect.DeepEqual(got, c) {
		t.Fatal("changed")
	}
	f.flush(t)
	if len(f.tc.Events())+len(f.ec.Events()) != 0 {
		t.Fatal("events")
	}
}
func TestRefreshToken(t *testing.T) {
	// R-QXWY-0E6W
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "retain", true: "replace"}[replace], func(t *testing.T) {
			f := setup(t)
			c := f.seed(t, f.now.Add(time.Minute))
			if replace {
				f.body = `{"access_token":"new-access-token-sensitive","refresh_token":"new-refresh-token-sensitive","expires_in":3599,"scope":"extra openid"}`
			}
			if !replace {
				f.body = `{"access_token":"fresh-access-token-sensitive","expires_in":3599}`
			}
			tok, err := f.b.Token(f.ctx, "google")
			if err != nil || len(f.forms) != 1 || f.forms[0].Get("refresh_token") != c.RefreshToken {
				t.Fatal(tok, err)
			}
			got, err := f.st.Get(f.ctx, "user", "google")
			if err != nil || tok.AccessToken != got.AccessToken || tok.Expires != got.AccessExpires.Format(time.RFC3339) || !reflect.DeepEqual(tok.Scopes, got.Scopes) {
				t.Fatal(got, err)
			}
			wantToken := "fresh-access-token-sensitive"
			if replace {
				wantToken = "new-access-token-sensitive"
			}
			if got.AccessToken != wantToken || !got.AccessExpires.Equal(f.now.Truncate(time.Second).Add(3599*time.Second)) {
				t.Fatal("grant not stored", got)
			}
			if replace {
				if got.RefreshToken != "new-refresh-token-sensitive" || !reflect.DeepEqual(got.Scopes, []string{"openid", "extra"}) {
					t.Fatal("not replaced")
				}
			} else if got.RefreshToken != c.RefreshToken || !reflect.DeepEqual(got.Scopes, c.Scopes) {
				t.Fatal("not retained")
			}
			f.flush(t)
			if len(f.tc.Events())+len(f.ec.Events()) != 0 {
				t.Fatal("events")
			}
		})
	}
}
func TestTokenFailures(t *testing.T) {
	// R-QZ4U-E5XL R-R0CQ-RXOA R-SHFZ-SF4F R-SINW-66V4
	for _, kind := range []string{"unknown", "missing", "foreign", "broken", "refused", "failed", "read", "write", "mark"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			c := f.seed(t, f.now.Add(time.Minute))
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
			case "broken":
				_, err := f.st.MarkBroken(ctx, "user", p)
				if err != nil {
					t.Fatal(err)
				}
				want = broker.ErrReconnect
			case "refused":
				f.status = 400
				want = broker.ErrReconnect
			case "failed":
				f.status = 502
				want = broker.ErrProvider
			case "read":
				f.d.SetFailing(true)
			case "write":
				f.hook = func() { f.d.SetFailing(true) }
			case "mark":
				f.status = 400
				f.hook = func() { f.d.SetFailing(true) }
			}
			tok, err := f.b.Token(ctx, p)
			if !errors.Is(err, want) || !reflect.DeepEqual(tok, broker.Token{}) {
				t.Fatal(tok, err)
			}
			f.d.SetFailing(false)
			got, _ := f.st.Get(f.ctx, "user", "google")
			if kind == "refused" || kind == "broken" {
				if got.Status != store.StatusBroken {
					t.Fatal("not broken")
				}
			} else if !reflect.DeepEqual(got, c) {
				t.Fatal("changed", got)
			}
			f.flush(t)
			if kind == "refused" {
				f.assertEvents(t, trail.Broken, got)
				if len(f.tc.Events()) != 1 || len(f.ec.Events()) != 1 || f.tc.Events()[0].Name != trail.Broken || f.tc.Events()[0].User != "user" {
					t.Fatal("events")
				}
				_, err = f.b.Token(ctx, p)
				if !errors.Is(err, broker.ErrReconnect) || len(f.forms) != 1 {
					t.Fatal("retried")
				}
			} else if len(f.tc.Events())+len(f.ec.Events()) != 0 {
				t.Fatal("events")
			}
			if kind == "unknown" || kind == "missing" || kind == "foreign" || kind == "broken" || kind == "read" {
				if len(f.forms) != 0 {
					t.Fatal("provider called")
				}
			}
		})
	}
}

func TestConcurrentRefresh(t *testing.T) {
	f := setup(t)
	c := f.seed(t, f.now.Add(time.Minute))
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("refresh_token") != c.RefreshToken {
			t.Error("refresh token")
		}
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, `{"access_token":"concurrent-new-access","expires_in":3599}`)
	}))
	defer srv.Close()
	f.cfg.Clients[0].Provider.TokenURL = srv.URL
	f.cfg.Clients[0].HTTP = srv.Client()
	b := broker.New(f.cfg)
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := b.Token(f.ctx, "google"); done <- err }()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			close(release)
			t.Fatal("both refreshes did not enter")
		}
	}
	close(release)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("refresh did not finish")
		}
	}
	f.flush(t)
	if len(f.ec.Events())+len(f.tc.Events()) != 0 {
		t.Fatal("events")
	}
}

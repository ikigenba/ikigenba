package web_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/web"
)

func TestOAuthTokenRefusals(t *testing.T) {
	// R-SJVS-JYLT R-SL3O-XQCI R-SMBL-BI37 R-R6G8-OSDR
	for _, kind := range []string{"POST", "HEAD", "/token/nope", "/token/", "/token/google/x", "/token/goo%67le", "missing", "broken", "absolute", "provider", "store"} {
		t.Run(kind, func(t *testing.T) {
			f := oauthSetup(t)
			method := "GET"
			path := "/token/google"
			status := 404
			v := broker.Refusal{Error: broker.RefusalUnknownProvider}
			switch kind {
			case "POST", "HEAD":
				method = kind
				status = 405
				v.Error = broker.RefusalMethod
				f.d.SetFailing(true)
			case "missing":
				v.Error = broker.RefusalNotConnected
			case "broken", "absolute":
				f.seed(t)
				if _, err := f.st.MarkBroken(context.Background(), "user", "google"); err != nil {
					t.Fatal(err)
				}
				status = 409
				v.Error = broker.RefusalReconnect
				v.URL = "/google/connect"
				if kind == "absolute" {
					f.cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
					if err := os.WriteFile(f.cfg.ServicesPath, []byte(`{"services":[{"name":"connections","url":"https://registered.example/","description":"","socket":"","enabled":true,"mcp":true}]}`), 0600); err != nil {
						t.Fatal(err)
					}
					v.URL = "https://registered.example/google/connect"
				}
			case "provider":
				f.seed(t)
				f.status = 502
				status = 502
				v.Error = broker.RefusalProvider
			case "store":
				f.d.SetFailing(true)
				status = 503
				v.Error = broker.RefusalUnavailable
			default:
				path = kind
				f.d.SetFailing(true)
			}
			w := oauthRequest(web.Handler(f.cfg), method, path, "")
			want, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if w.Code != status || w.Body.String() != string(want)+"\n" || w.Header().Get("Content-Type") != "application/json" {
				t.Fatal(w.Code, w.Body.String())
			}
			if status == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal("allow")
			}
			if kind != "provider" && len(f.forms) != 0 {
				t.Fatal("provider called")
			}
		})
	}
}
func TestOAuthTokenReuse(t *testing.T) {
	// R-SORE-31KL R-R6G8-OSDR
	f := oauthSetup(t)
	f.seed(t)
	h := web.Handler(f.cfg)
	a := oauthRequest(h, "GET", "/token/google", "")
	b := oauthRequest(h, "GET", "/token/google", "")
	if a.Code != 200 || b.Code != 200 || a.Body.String() != b.Body.String() || len(f.forms) != 1 {
		t.Fatal(a.Code, b.Code, a.Body.String(), b.Body.String())
	}
	c, err := f.st.Get(context.Background(), "user", "google")
	if err != nil {
		t.Fatal(err)
	}
	var tok broker.Token
	if err = json.Unmarshal(a.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	if a.Body.String() != string(expected)+"\n" || a.Header().Get("Content-Type") != "application/json" || len(a.Header().Values("Content-Type")) != 1 {
		t.Fatal("json answer")
	}
	if tok.AccessToken != "new-access-token-sensitive" || !reflect.DeepEqual(tok.Scopes, c.Scopes) || tok.Account != c.AccountEmail || tok.Expires != f.now.Add(3599*time.Second).Format(time.RFC3339) {
		t.Fatal(tok)
	}
}

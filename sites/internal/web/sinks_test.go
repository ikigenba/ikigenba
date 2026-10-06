package web_test

import (
	"bytes"
	"context"
	"errors"

	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/web"
)

type testSink struct {
	deliver func(context.Context, telemetry.Event) error
}

func (s testSink) Deliver(ctx context.Context, e telemetry.Event) error { return s.deliver(ctx, e) }

// R-X87M-GTAD R-X9FI-UL12
func TestDeliveryDoesNotChangeAnswers(t *testing.T) {
	f := fresh(t)
	f.repository(t)
	sha := f.commit(t, "site body")
	site := f.add(t, "blog", store.Public)
	if e := f.cfg.Cache.Unpack(context.Background(), site.ID, site.Repo, sha); e != nil {
		t.Fatal(e)
	}
	if _, e := f.cfg.Store.Publish(context.Background(), site.ID, sha); e != nil {
		t.Fatal(e)
	}
	baseline := map[string]struct {
		status  int
		headers map[string][]string
		body    string
	}{}
	for _, mode := range []string{"success", "failure", "blocked"} {
		var stderr bytes.Buffer
		release := make(chan struct{})
		entered := make(chan struct{})
		var once sync.Once
		sink := testSink{deliver: func(ctx context.Context, _ telemetry.Event) error {
			switch mode {
			case "failure":
				return errors.New("sink refused")
			case "blocked":
				once.Do(func() { close(entered) })
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}}
		writer := telemetry.New(telemetry.Config{Service: "sites", Version: "fixture", Sink: sink, Stderr: &stderr, Now: func() time.Time { return time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC) }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(randomBytes(65))})
		cfg := f.cfg
		cfg.Telemetry = writer
		cfg.MCP = newServer(writer)
		cfg.Rand = bytes.NewReader(randomBytes(66))
		f.h = web.Handler(cfg)
		if mode == "blocked" {
			writer.Emit(context.Background(), "service.started", telemetry.Attrs{"version": "fixture"})
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("sink did not enter")
			}
		}
		for _, path := range []string{"/", "/blog/"} {
			user := ""
			if path == "/" {
				user = "user"
			}
			r := f.get(t, "GET", path, "sites", user, nil)
			if r.Code != 200 {
				t.Fatalf("%s %s status %d", mode, path, r.Code)
			}
			if mode == "success" {
				baseline[path] = struct {
					status  int
					headers map[string][]string
					body    string
				}{r.Code, r.Header(), r.Body.String()}
			} else {
				want := baseline[path]
				if r.Code != want.status || r.Body.String() != want.body || !reflect.DeepEqual(map[string][]string(r.Header()), want.headers) {
					t.Fatalf("sink changed %s answer", path)
				}
			}
		}
		if mode == "success" {
			for _, q := range []struct{ method, path, user string }{{"GET", "/nope", ""}, {"POST", "/blog/", ""}, {"GET", "/mcp", ""}} {
				f.get(t, q.method, q.path, "sites", q.user, nil)
			}
			f.call(t, "publish", `{"name":"absent"}`)
			closed := fresh(t)
			closed.db.SetFailing(true)
			cc := closed.cfg
			cc.Telemetry = writer
			cc.MCP = newServer(writer)
			closed.h = web.Handler(cc)
			closed.get(t, "GET", "/blog/", "sites", "", nil)
			if e := writer.Flush(context.Background()); e != nil {
				t.Fatal(e)
			}
			if stderr.Len() != 0 {
				t.Fatalf("handled failures on stderr: %s", stderr.String())
			}
		}
		close(release)
		writer.Shutdown(context.Background(), "test finished")
		if mode == "failure" && !strings.Contains(stderr.String(), "undelivered event") {
			t.Fatal("sink was never exercised")
		}
	}
}

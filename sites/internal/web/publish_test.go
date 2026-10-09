package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/tools"
)

// R-0K3T-2K4V
func TestPublishKeepsOldTreeVisibleUntilInstallation(t *testing.T) {
	f := fresh(t)
	f.repository(t)
	old := f.commit(t, "old body")
	s := f.add(t, "blog", store.Public)
	if e := f.cfg.Cache.Unpack(context.Background(), s.ID, s.Repo, old); e != nil {
		t.Fatal(e)
	}
	if _, e := f.cfg.Store.Publish(context.Background(), s.ID, old); e != nil {
		t.Fatal(e)
	}
	before, e := f.cfg.Store.Find(context.Background(), "user", "blog")
	if e != nil {
		t.Fatal(e)
	}
	initial := f.get(t, "GET", "/blog/", "sites", "", nil)
	f.commit(t, "new body")
	checkpoints := 0
	checking := false
	check := func() {
		t.Helper()
		if checking {
			t.Fatal("old tree caused a git run")
		}
		checking = true
		defer func() { checking = false }()
		checkpoints++
		now, e := f.cfg.Store.Find(context.Background(), "user", "blog")
		if e != nil {
			t.Fatal(e)
		}
		if now.Commit != before.Commit || !now.Published.Equal(before.Published) {
			t.Fatal("catalog changed before tree installation")
		}
		if st, e := os.Stat(f.cfg.Cache.Dir(s.ID, old)); e != nil || !st.IsDir() {
			t.Fatal("old tree removed")
		}
		r := f.get(t, "GET", "/blog/", "sites", "", nil)
		if r.Code != initial.Code || r.Header().Get("ETag") != initial.Header().Get("ETag") || r.Body.String() != initial.Body.String() {
			t.Fatal("old site response changed")
		}
	}
	f.after = func(time.Duration) <-chan time.Time { check(); return make(chan time.Time) }
	f.unpacked = func(string, string) { check() }
	result := f.call(t, "publish", `{"name":"blog"}`)
	if result.IsError() {
		t.Fatal("publish refused")
	}
	if checkpoints < 3 {
		t.Fatalf("too few checkpoints %d", checkpoints)
	}
	after := f.get(t, "GET", "/blog/", "sites", "", nil)
	if after.Body.String() != "new body" || after.Header().Get("ETag") == initial.Header().Get("ETag") {
		t.Fatal("published response not updated")
	}
}

// R-0BKI-E5Y0
func TestRefusedPublishPreservesServing(t *testing.T) {
	for _, mode := range []string{"missing", "timeout", "git", "size"} {
		t.Run(mode, func(t *testing.T) {
			f := fresh(t)
			f.repository(t)
			old := f.commit(t, "old body")
			s := f.add(t, "blog", store.Public)
			if e := f.cfg.Cache.Unpack(context.Background(), s.ID, s.Repo, old); e != nil {
				t.Fatal(e)
			}
			if _, e := f.cfg.Store.Publish(context.Background(), s.ID, old); e != nil {
				t.Fatal(e)
			}
			before, e := f.cfg.Store.Find(context.Background(), "user", "blog")
			if e != nil {
				t.Fatal(e)
			}
			initial := f.get(t, "GET", "/blog/", "sites", "", nil)
			args := `{"name":"blog"}`
			switch mode {
			case "missing":
				args = `{"name":"blog","ref":"absent"}`
			case "timeout":
				f.after = func(time.Duration) <-chan time.Time {
					ch := make(chan time.Time, 1)
					ch <- time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
					return ch
				}
			case "git":
				f.commit(t, "new body")
				parent := filepath.Dir(f.cfg.Cache.Dir(s.ID, old))
				info, e := os.Stat(parent)
				if e != nil {
					t.Fatal(e)
				}
				if e := os.Chmod(parent, info.Mode().Perm()&^0222); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { _ = os.Chmod(parent, info.Mode().Perm()) })
			case "size":
				f.commit(t, "new body") // replace limits/cache below with a small approved size limit.
				replaceSmallCache(t, f)
			}
			result := f.call(t, "publish", args)
			if !result.IsError() {
				t.Fatal("expected refusal", mode)
			}
			b, e := json.Marshal(result)
			if e != nil {
				t.Fatal(e)
			}
			var answer struct{ Content []struct{ Text string } }
			if e = json.Unmarshal(b, &answer); e != nil {
				t.Fatal(e)
			}
			if len(answer.Content) != 1 {
				t.Fatal("refusal shape")
			}
			text := answer.Content[0].Text
			switch mode {
			case "missing":
				if text != fmt.Sprintf(tools.NoCommit, "absent") {
					t.Fatal(text)
				}
			case "timeout":
				if text != fmt.Sprintf(tools.TimedOut, 600) {
					t.Fatal(text)
				}
			case "git":
				if text != tools.GitFailed {
					t.Fatal(text)
				}
			case "size":
				if text != fmt.Sprintf(tools.TooLarge, 7) {
					t.Fatal(text)
				}
			}
			after, e := f.cfg.Store.Find(context.Background(), "user", "blog")
			if e != nil {
				t.Fatal(e)
			}
			if after.Commit != before.Commit || !after.Published.Equal(before.Published) {
				t.Fatal("refusal changed catalog")
			}
			r := f.get(t, "GET", "/blog/", "sites", "", nil)
			if r.Code != initial.Code || r.Header().Get("ETag") != initial.Header().Get("ETag") || r.Body.String() != initial.Body.String() {
				t.Fatal("refusal changed site response")
			}
		})
	}
}

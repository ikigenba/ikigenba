package smarthttp_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func TestRealClientsCloneFetchAndGzip(t *testing.T) {
	// R-D4P7-M7WP R-DD8I-AM3K
	f := setup(t)
	repo := f.create("notes")
	empty := f.create("empty")
	work := f.working("source")
	first := f.commit(work, "one")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	server := f.server()
	before := f.refs(repo.ID)
	objectsBefore := f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "count-objects", "-v")
	for _, version := range []string{"0", "2"} {
		clone := filepath.Join(f.root, "clone"+version)
		f.client("", "-c", "protocol.version="+version, "clone", server.URL+"/notes.git", clone)
		same(t, strings.TrimSpace(f.gitRun(clone, "rev-parse", "HEAD")), first)
		url := server.URL + "/empty.git"
		emptyClone := filepath.Join(f.root, "empty"+version)
		result := f.client("", "-c", "protocol.version="+version, "clone", url, emptyClone)
		if !strings.Contains(result, "cloned an empty repository") {
			t.Fatalf("empty clone warning absent: %s", result)
		}
		same(t, strings.TrimSpace(f.gitRun(emptyClone, "remote", "get-url", "origin")), url)
	}
	same(t, f.refs(repo.ID), before)
	same(t, f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "count-objects", "-v"), objectsBefore)
	same(t, f.refs(empty.ID), "")
	second := f.commit(work, "two")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	for _, version := range []string{"0", "2"} {
		clone := filepath.Join(f.root, "clone"+version)
		f.client(clone, "fetch")
		same(t, strings.TrimSpace(f.gitRun(clone, "rev-parse", "origin/main")), second)
	}
	before = f.refs(repo.ID)
	objectsBefore = f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "count-objects", "-v")
	request := []byte(packet("want "+second+" side-band-64k ofs-delta no-progress\n") + "0000" + packet("done\n"))
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	_, err := z.Write(request)
	must(t, err)
	must(t, z.Close())
	var responses [][]byte
	for i, body := range [][]byte{request, compressed.Bytes()} {
		r, err := http.NewRequestWithContext(deadline(t), "POST", server.URL+"/notes.git/git-upload-pack", bytes.NewReader(body))
		must(t, err)
		r.Header.Set("X-User-Id", "alice")
		r.Header.Set("X-Request-Id", "gzip")
		r.Header.Set("Content-Type", "application/x-git-upload-pack-request")
		if i == 1 {
			r.Header.Set("Content-Encoding", "gzip")
		}
		response, err := server.Client().Do(r)
		must(t, err)
		same(t, response.StatusCode, 200)
		b, err := io.ReadAll(response.Body)
		must(t, err)
		must(t, response.Body.Close())
		responses = append(responses, b)
	}
	same(t, responses[0], responses[1])
	same(t, f.refs(repo.ID), before)
	same(t, f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "count-objects", "-v"), objectsBefore)
	for _, sha := range []string{first, second} {
		f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "cat-file", "-e", sha)
	}
	fetched := 0
	for _, event := range ownEvents(f.events()) {
		if event.Name == "repo.fetched" {
			fetched++
			same(t, event.User, "alice")
			if event.RequestID != "request" && event.RequestID != "gzip" {
				t.Fatalf("wrong request envelope %q", event.RequestID)
			}
			if event.Attrs["repo"] != repo.ID && event.Attrs["repo"] != empty.ID {
				t.Fatal("wrong repository envelope")
			}
		}
	}
	if fetched == 0 {
		t.Fatal("no fetch events")
	}
}

func TestRealClientPushMutationsAndChunked(t *testing.T) {
	// R-D5X3-ZZNE R-DD8I-AM3K R-0AXP-9L2Q
	f := setup(t)
	repo := f.create("notes")
	chunked := make(chan bool, 32)
	handler := f.handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/notes.git/git-receive-pack" {
			chunked <- r.ContentLength == -1 && len(r.TransferEncoding) == 1 && r.TransferEncoding[0] == "chunked"
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	work := f.working("source")
	comparison := filepath.Join(f.root, "comparison.git")
	f.gitRun("", "init", "--bare", comparison)
	trace := filepath.Join(f.root, "trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "gc.auto", "1")
	first := f.commit(work, "one")
	push := func(args ...string) {
		t.Helper()
		f.client(work, append([]string{"push", server.URL + "/notes.git"}, args...)...)
		f.gitRun(work, append([]string{"push", comparison}, args...)...)
		same(t, f.refs(repo.ID), f.gitRun("", "--git-dir="+comparison, "for-each-ref", "--format=%(refname) %(objectname)"))
	}
	push("main")
	f.gitRun(work, "branch", "topic")
	push("topic")
	second := f.commit(work, "two")
	push("main")
	f.gitRun(work, "reset", "--hard", first)
	push("--force", "main")
	push(":topic")
	f.gitRun(work, "branch", "another", second)
	f.gitRun(work, "tag", "mark", second)
	push("another", "mark")
	write(t, filepath.Join(work, "large"), noise(256*1024))
	f.gitRun(work, "add", "large")
	f.gitRun(work, "commit", "-m", "chunked fixture")
	f.client(work, "-c", "http.postBuffer=1024", "push", server.URL+"/notes.git", "main")
	f.gitRun(work, "push", comparison, "main")
	sawChunked := false
	for len(chunked) > 0 {
		if <-chunked {
			sawChunked = true
		}
	}
	if !sawChunked {
		t.Fatal("client did not send a chunked push without Content-Length")
	}
	same(t, f.refs(repo.ID), f.gitRun("", "--git-dir="+comparison, "for-each-ref", "--format=%(refname) %(objectname)"))
	for _, sha := range strings.Fields(f.gitRun(work, "rev-list", "--objects", "--all")) {
		if len(sha) == 40 {
			f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "cat-file", "-e", sha)
		}
	}
	packs, err := os.ReadDir(filepath.Join(f.store.Dir(repo.ID), "objects", "pack"))
	must(t, err)
	same(t, len(packs), 0)
	b, err := os.ReadFile(filepath.Clean(trace))
	must(t, err)
	if strings.Contains(string(b), `"cmd_name":"gc"`) || strings.Contains(string(b), `"argv":["git","gc"`) {
		t.Fatal("automatic gc ran")
	}
	pushed := 0
	for _, event := range ownEvents(f.events()) {
		if event.Name == "repo.pushed" {
			pushed++
			same(t, event.RequestID, "request")
			same(t, event.User, "alice")
			same(t, event.Attrs["repo"], repo.ID)
		}
	}
	if pushed < 7 {
		t.Fatalf("only %d push events", pushed)
	}
}

func noise(n int) string {
	b := make([]byte, n)
	var state uint32 = 0x13572468
	for i := range b {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		b[i] = byte(state & 0xff)
	}
	return string(b)
}

func TestDynamicPackLimitAndRejectionEvents(t *testing.T) {
	// R-XDNC-RTG4 R-969O-PS2H R-99XD-V3AK
	f := setup(t)
	repo := f.create("notes")
	work := f.working("source")
	write(t, filepath.Join(work, "large"), noise(32*1024))
	f.gitRun(work, "add", "large")
	f.gitRun(work, "commit", "-m", "large fixture")
	sha := strings.TrimSpace(f.gitRun(work, "rev-parse", "HEAD"))
	f.settings.PushMaxBytes = 100
	f.resetLimits()
	small := f.server()
	args := []string{"-c", "http.extraHeader=X-User-Id: alice", "-c", "http.extraHeader=X-Request-Id: rejected", "push", small.URL + "/notes.git", "main"}
	b, err := f.gitTry(work, args...)
	if err == nil || !strings.Contains(string(b), "pack exceeds maximum allowed size") {
		t.Fatalf("small cap push: %v %s", err, b)
	}
	same(t, f.refs(repo.ID), "")
	_, err = f.gitTry("", "--git-dir="+f.store.Dir(repo.ID), "cat-file", "-e", sha)
	if err == nil {
		t.Fatal("rejected object is readable")
	}
	events := ownEvents(f.events())
	same(t, len(events), 1)
	same(t, events[0].Name, "operation.rejected")
	same(t, events[0].RequestID, "rejected")
	same(t, events[0].User, "alice")
	same(t, events[0].Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "push", "limit": "push_max_bytes"})
	f.settings.PushMaxBytes = 1024 * 1024
	f.resetLimits()
	large := f.server()
	f.client(work, "push", large.URL+"/notes.git", "main")
	same(t, strings.TrimSpace(f.gitRun("", "--git-dir="+f.store.Dir(repo.ID), "rev-parse", "main")), sha)
	f.gitRun(work, "checkout", "--orphan", "unrelated")
	f.commit(work, "unrelated")
	f.gitRun(work, "branch", "-f", "main", "HEAD")
	before := len(f.events())
	b, err = f.gitTry(work, "-c", "http.extraHeader=X-User-Id: alice", "push", large.URL+"/notes.git", "main")
	if err == nil {
		t.Fatalf("non-fast-forward accepted: %s", b)
	}
	for _, e := range ownEvents(f.events()[before:]) {
		if e.Name == "operation.rejected" && e.Attrs["limit"] == "push_max_bytes" {
			t.Fatal("wrong push limit rejection")
		}
	}
}

func TestSizePreflightAndOvershootFetch(t *testing.T) {
	// R-59WS-LHAB R-5B4O-Z910 R-8U2O-W2NJ R-5CCL-D0RP
	f := setup(t)
	repo := f.create("notes")
	size, err := f.store.Size(deadline(t), repo.ID)
	must(t, err)
	f.settings.RepoMaxBytes = size + 1
	f.resetLimits()
	server := f.server()
	work := f.working("source")
	sha := f.commit(work, "overshoot")
	f.client(work, "push", server.URL+"/notes.git", "main")
	size, err = f.store.Size(deadline(t), repo.ID)
	must(t, err)
	if size < f.settings.RepoMaxBytes {
		t.Fatal("fixture did not exceed cap")
	}
	f.client("", "clone", server.URL+"/notes.git", filepath.Join(f.root, "clone"))
	same(t, strings.TrimSpace(f.gitRun(filepath.Join(f.root, "clone"), "rev-parse", "HEAD")), sha)
	f.limits.Drain()
	before := len(f.events())
	timers := len(f.clock.timers)
	for _, tc := range []struct{ method, path string }{{"GET", "/notes.git/info/refs?service=git-receive-pack"}, {"POST", "/notes.git/git-receive-pack"}} {
		w := f.request(tc.method, tc.path, strings.NewReader("0000"))
		outcome(t, w, 507, "repository is at its size limit of "+strconv.FormatInt(f.settings.RepoMaxBytes, 10)+" bytes\n")
		same(t, w.Header().Get("Retry-After"), "")
	}
	same(t, len(f.clock.timers), timers)
	events := ownEvents(f.events()[before:])
	same(t, len(events), 2)
	for _, e := range events {
		same(t, e.Name, "operation.rejected")
		same(t, e.Attrs, telemetry.Attrs{"repo": repo.ID, "operation": "push", "limit": "repo_max_bytes"})
	}
}

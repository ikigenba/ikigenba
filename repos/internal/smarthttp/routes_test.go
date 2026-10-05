package smarthttp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
)

func TestResolutionPrecedesRoutingAndLimits(t *testing.T) {
	// R-3KVL-SAXF R-3M3I-62O4 R-3NBE-JUET R-3OJA-XM5I R-3S70-2XDL
	for _, state := range []string{"unknown", "foreign", "invalid", "id", "unavailable", "closed", "available"} {
		t.Run(state, func(t *testing.T) {
			f := setup(t)
			repo := f.create("notes")
			name := "notes"
			switch state {
			case "unknown":
				name = "missing"
			case "foreign":
				name = "foreign"
				_, err := f.store.Create(deadline(t), "bob", name)
				must(t, err)
			case "invalid":
				name = "Bad"
			case "id":
				name = repo.ID
			case "unavailable":
				must(t, os.Remove(filepath.Join(f.store.Dir(repo.ID), "HEAD")))
				must(t, f.store.Verify(deadline(t), f.writer))
			case "closed":
				must(t, f.store.Close())
			}
			trace := filepath.Join(f.root, "refusal-trace.json")
			f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
			must(t, os.WriteFile(trace, nil, 0600))
			baseline := len(f.events())
			f.limits.Drain()
			for _, method := range []string{"GET", "POST", "HEAD", "PUT", "DELETE"} {
				for _, rest := range []string{"", "/", "/info/refs?service=git-upload-pack", "/git-receive-pack", "/HEAD"} {
					// Available smart routes are checked separately below.
					if state == "available" && ((method == "GET" && strings.HasPrefix(rest, "/info/refs?")) || (method == "POST" && rest == "/git-receive-pack")) {
						continue
					}
					w := f.request(method, "/"+name+".git"+rest, strings.NewReader("ignored"))
					status, body := 404, "repository not found\n"
					if state == "closed" {
						status, body = 500, "cannot reach the repositories; try again later\n"
					}
					if state == "unavailable" {
						status, body = 503, "repository unavailable\n"
					}
					if state == "available" {
						body = "not found\n"
					}
					if method == "HEAD" {
						body = ""
					}
					outcome(t, w, status, body)
					same(t, w.Header().Get("Retry-After"), "")
				}
			}
			same(t, len(ownEvents(f.events()[baseline:])), 0)
			same(t, len(f.clock.timers), 0)
			traceBody, err := os.ReadFile(filepath.Clean(trace))
			must(t, err)
			same(t, string(traceBody), "")
		})
	}
}

func TestSmartRouteDefinition(t *testing.T) {
	// R-CTQ4-6A8G R-3OJA-XM5I
	f := setup(t)
	f.create("notes")
	for _, tc := range []struct {
		method, path string
		smart        bool
	}{
		{"GET", "/notes.git/info/refs?service=git-upload-pack", true},
		{"GET", "/notes.git/info/refs?x=one&service=git-receive-pack", true},
		{"POST", "/notes.git/git-upload-pack?service=wrong", true},
		{"POST", "/notes.git/git-receive-pack", true},
		{"GET", "/notes.git/info/refs", false},
		{"GET", "/notes.git/info/refs?service=git-upload-pack&service=git-upload-pack", false},
		{"GET", "/notes.git/info/refs?service=git-upload-pack&broken=%", false},
		{"GET", "/notes.git/info/refs?service=git-upload-pack;other=x", false},
		{"GET", "/notes.git/info/refs?service=other", false},
		{"GET", "/notes.git/HEAD", false},
		{"GET", "/notes.git/objects/info/packs", false},
		{"GET", "/notes.git/objects/ab/cdef", false},
		{"GET", "/notes.git/git-upload-pack", false},
		{"POST", "/notes.git/info/refs?service=git-receive-pack", false},
		{"GET", "/notes.git", false}, {"GET", "/notes.git/", false},
		{"HEAD", "/notes.git/info/refs?service=git-upload-pack", false},
	} {
		w := f.request(tc.method, tc.path, strings.NewReader("0000"))
		if tc.smart {
			same(t, w.Code, 200)
			f.clock.take(t)
		} else {
			body := "not found\n"
			if tc.method == "HEAD" {
				body = ""
			}
			outcome(t, w, 404, body)
		}
	}
}

func TestAdvertisementAndGitConfigurationEnvironment(t *testing.T) {
	// R-0AXP-9L2Q R-L2IS-717R R-1DOR-GX9C R-3S70-2XDL
	f := setup(t)
	repo := f.create("notes")
	work := f.working("work")
	sha := f.commit(work, "one")
	f.gitRun(work, "push", f.store.Dir(repo.ID), "main")
	trace := filepath.Join(f.root, "trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.configParams", "http.getanyfile,http.uploadpack,http.receivepack,receive.maxinputsize,receive.autogc,gc.auto")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.envVars", "GIT_PROJECT_ROOT,PATH_INFO,REQUEST_METHOD,QUERY_STRING,CONTENT_TYPE,REMOTE_USER,GIT_HTTP_EXPORT_ALL,HTTP_GIT_PROTOCOL,HTTP_CONTENT_ENCODING,LC_ALL,CONTENT_LENGTH,REMOTE_ADDR,HTTP_AUTHORIZATION,FIXTURE_MARKER")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "gc.auto", "1")
	// Git.Find's environment source retains this slice's values.
	f.env = append(f.env, "FIXTURE_MARKER=unchanged")
	// Find receives its own environment callback so the appended value is supplied.
	g, err := findFixtureGit(f)
	must(t, err)
	f.git = g
	for _, service := range []string{"git-upload-pack", "git-receive-pack"} {
		must(t, os.WriteFile(trace, nil, 0600))
		r := httptest.NewRequest("GET", "/notes.git/info/refs?service="+service+"&unused=yes", nil)
		r.Header.Set("X-User-Id", "alice")
		r.Header.Set("X-Request-Id", "request")
		r.Header.Set("Content-Type", "fixture/type")
		r.Header.Set("Git-Protocol", "version=0")
		r.Header.Set("Content-Encoding", "identity")
		r.Header.Set("Authorization", "Bearer omitted")
		r.Header.Set("Content-Length", "123")
		w := httptest.NewRecorder()
		f.handler().ServeHTTP(w, r)
		same(t, w.Code, 200)
		expected := map[string][]string{"Content-Type": {"application/x-" + service + "-advertisement"}, "Cache-Control": {"no-cache, max-age=0, must-revalidate"}, "Expires": {"Fri, 01 Jan 1980 00:00:00 GMT"}, "Pragma": {"no-cache"}}
		same(t, w.Header(), expected)
		prefix := "001e# service=git-upload-pack\n0000"
		if service == "git-receive-pack" {
			prefix = "001f# service=git-receive-pack\n0000"
		}
		if !strings.HasPrefix(w.Body.String(), prefix) || !strings.HasSuffix(w.Body.String(), "0000") || (service == "git-upload-pack" && !strings.Contains(w.Body.String(), sha+" HEAD")) || !strings.Contains(w.Body.String(), sha+" refs/heads/main") {
			t.Fatalf("bad advertisement %q", w.Body.String())
		}
		params := traceParams(t, trace)
		assertBackendArguments(t, trace, f.settings.PushMaxBytes)
		for key, value := range map[string]string{"http.getanyfile": "false", "http.uploadpack": "true", "http.receivepack": "true", "receive.maxinputsize": strconv.FormatInt(f.settings.PushMaxBytes, 10), "receive.autogc": "false", "gc.auto": "0", "GIT_PROJECT_ROOT": filepath.Dir(f.store.Dir(repo.ID)), "PATH_INFO": "/" + repo.ID + ".git/info/refs", "REQUEST_METHOD": "GET", "QUERY_STRING": "service=" + service, "CONTENT_TYPE": "fixture/type", "REMOTE_USER": "alice", "GIT_HTTP_EXPORT_ALL": "1", "HTTP_GIT_PROTOCOL": "version=0", "HTTP_CONTENT_ENCODING": "identity", "LC_ALL": "C", "FIXTURE_MARKER": "unchanged"} {
			same(t, params[key], value)
		}
		for _, key := range []string{"CONTENT_LENGTH", "REMOTE_ADDR", "HTTP_AUTHORIZATION"} {
			same(t, params[key], "")
		}
		// Compare every byte with the same real backend's unmediated CGI
		// output, including capabilities whose order and values git owns.
		cmd := f.git.Command(deadline(t), "", []string{
			"GIT_PROJECT_ROOT=" + filepath.Dir(f.store.Dir(repo.ID)),
			"PATH_INFO=/" + repo.ID + ".git/info/refs", "REQUEST_METHOD=GET",
			"QUERY_STRING=service=" + service, "CONTENT_TYPE=fixture/type",
			"REMOTE_USER=alice", "GIT_HTTP_EXPORT_ALL=1", "HTTP_GIT_PROTOCOL=version=0",
			"HTTP_CONTENT_ENCODING=identity", "LC_ALL=C",
		}, "-c", "http.getanyfile=false", "-c", "http.uploadpack=true", "-c", "http.receivepack=true",
			"-c", "receive.maxInputSize="+strconv.FormatInt(f.settings.PushMaxBytes, 10),
			"-c", "receive.autogc=false", "-c", "gc.auto=0", "http-backend")
		output, err := cmd.Output()
		must(t, err)
		status, header, body, err := git.ReadHeader(bytes.NewReader(output))
		must(t, err)
		answer, err := io.ReadAll(body)
		must(t, err)
		same(t, w.Code, status)
		same(t, w.Header(), header)
		same(t, w.Body.Bytes(), answer)
	}
	same(t, len(ownEvents(f.events())), 0)
}

func TestPostBackendConfigurationAndEnvironment(t *testing.T) {
	// R-0AXP-9L2Q R-L2IS-717R
	f := setup(t)
	repo := f.create("notes")
	trace := filepath.Join(f.root, "post-trace.json")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.eventTarget", trace)
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.configParams", "http.getanyfile,http.uploadpack,http.receivepack,receive.maxinputsize,receive.autogc,gc.auto")
	f.gitRun("", "config", "--file", filepath.Join(f.root, "global"), "trace2.envVars", "GIT_PROJECT_ROOT,PATH_INFO,REQUEST_METHOD,QUERY_STRING,CONTENT_TYPE,REMOTE_USER,GIT_HTTP_EXPORT_ALL,HTTP_GIT_PROTOCOL,HTTP_CONTENT_ENCODING,LC_ALL,CONTENT_LENGTH,REMOTE_ADDR,HTTP_AUTHORIZATION,FIXTURE_MARKER")
	f.env = append(f.env, "FIXTURE_MARKER=kept", "LC_ALL=override", "PATH_INFO=override", "REMOTE_USER=override")
	g, err := findFixtureGit(f)
	must(t, err)
	f.git = g
	for _, service := range []string{"git-upload-pack", "git-receive-pack"} {
		must(t, os.WriteFile(trace, nil, 0600))
		r := httptest.NewRequest("POST", "/notes.git/"+service+"?service=ignored", strings.NewReader("0000"))
		r.Header.Set("X-User-Id", "alice")
		r.Header.Set("X-Request-Id", "request")
		r.Header["Content-Type"] = []string{"application/x-" + service + "-request", "ignored"}
		r.Header["Git-Protocol"] = []string{"version=0", "version=2"}
		r.Header["Content-Encoding"] = []string{"identity", "gzip"}
		r.Header.Set("Authorization", "Bearer omitted")
		w := httptest.NewRecorder()
		f.handler().ServeHTTP(w, r)
		same(t, w.Code, 200)
		params := traceParams(t, trace)
		assertBackendArguments(t, trace, f.settings.PushMaxBytes)
		for key, value := range map[string]string{"http.getanyfile": "false", "http.uploadpack": "true", "http.receivepack": "true", "receive.maxinputsize": strconv.FormatInt(f.settings.PushMaxBytes, 10), "receive.autogc": "false", "gc.auto": "0", "GIT_PROJECT_ROOT": filepath.Dir(f.store.Dir(repo.ID)), "PATH_INFO": "/" + repo.ID + ".git/" + service, "REQUEST_METHOD": "POST", "QUERY_STRING": "", "CONTENT_TYPE": "application/x-" + service + "-request", "REMOTE_USER": "alice", "GIT_HTTP_EXPORT_ALL": "1", "HTTP_GIT_PROTOCOL": "version=0", "HTTP_CONTENT_ENCODING": "identity", "LC_ALL": "C", "FIXTURE_MARKER": "kept"} {
			same(t, params[key], value)
		}
		for _, key := range []string{"CONTENT_LENGTH", "REMOTE_ADDR", "HTTP_AUTHORIZATION"} {
			same(t, params[key], "")
		}
	}
}

func assertBackendArguments(t *testing.T, path string, maximum int64) {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	count := 0
	for line := range strings.SplitSeq(string(b), "\n") {
		if line == "" {
			continue
		}
		var event struct {
			Event string
			Argv  []string
		}
		must(t, json.Unmarshal([]byte(line), &event))
		if event.Event == "start" && len(event.Argv) > 1 && event.Argv[len(event.Argv)-1] == "http-backend" {
			count++
			same(t, event.Argv[1:], []string{"-c", "http.getanyfile=false", "-c", "http.uploadpack=true", "-c", "http.receivepack=true", "-c", "receive.maxInputSize=" + strconv.FormatInt(maximum, 10), "-c", "receive.autogc=false", "-c", "gc.auto=0", "http-backend"})
		}
	}
	same(t, count, 1)
}

func traceParams(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	must(t, err)
	type traceEvent struct {
		Event, Param, Value, Sid string
		Argv                     []string
	}
	var events []traceEvent
	session := ""
	for line := range strings.SplitSeq(string(b), "\n") {
		if line == "" {
			continue
		}
		var event traceEvent
		must(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
		if event.Event == "start" && len(event.Argv) > 0 && event.Argv[len(event.Argv)-1] == "http-backend" {
			session = event.Sid
		}
	}
	if session == "" {
		t.Fatal("backend session not found")
	}
	result := map[string]string{}
	for _, event := range events {
		if event.Event == "def_param" && event.Sid == session {
			result[event.Param] = event.Value
		}
	}
	return result
}

func TestRejectedGitDoesNotWriteProcessStreams(t *testing.T) {
	// R-0JGZ-XZ9L
	f := setup(t)
	f.create("notes")
	outR, outW, err := os.Pipe()
	must(t, err)
	errR, errW, err := os.Pipe()
	must(t, err)
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; must(t, outR.Close()); must(t, errR.Close()) }()
	f.request(http.MethodPost, "/notes.git/git-upload-pack", strings.NewReader("invalid request"))
	os.Stdout, os.Stderr = oldOut, oldErr
	must(t, outW.Close())
	must(t, errW.Close())
	stdout, err := io.ReadAll(outR)
	must(t, err)
	stderr, err := io.ReadAll(errR)
	must(t, err)
	same(t, string(stdout), "")
	same(t, string(stderr), "")
}

func assertIdle(t *testing.T, f *fixture, id string) {
	t.Helper()
	p := f.limits.Pressure()
	same(t, p.Read.Active, int64(0))
	same(t, p.Write.Active, int64(0))
	same(t, p.Read.Queued, int64(0))
	same(t, p.Write.Queued, int64(0))
	same(t, f.limits.Busy(id), false)
}

func queueGrant(f *fixture, id string, op limits.Op, lock bool) *limits.Grant {
	g, err := f.limits.Acquire(deadline(f.t), id, op, lock)
	must(f.t, err)
	return g
}

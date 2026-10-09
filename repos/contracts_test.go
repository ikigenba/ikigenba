package repos_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	repogit "github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/maintenance"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/smarthttp"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
	"github.com/ikigenba/ikigenba/repos/internal/web"
)

// R-S5H8-N3EM R-S6P5-0V5B: Access both embedded templates before and after
// moving into an empty test directory, and compare every byte.
func TestEmbeddedAssets(t *testing.T) {
	first := files(t, repos.Assets(), []string{"about.html", "landing.html"})
	// Chdir directly: testing.T.Chdir would also mutate the PWD environment.
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chdir(empty); err != nil {
		t.Fatal(err)
	}
	second := files(t, repos.Assets(), []string{"about.html", "landing.html"})
	for name, body := range first {
		if len(body) == 0 || !bytes.Equal(body, second[name]) {
			t.Errorf("embedded template %s changed or is empty", name)
		}
	}
}

// R-F240-C369 R-F3BW-PUWY R-F4JT-3MNN R-DATL-1144: Compare embedded deployment files to
// the publicly consumed constants, without opening the checkout.
func TestEmbeddedDeploymentFiles(t *testing.T) {
	const nginx = cli.NginxConf + ""
	if nginx != "client_max_body_size    0;\nproxy_request_buffering off;\nproxy_http_version      1.1;\nproxy_buffering         on;\nproxy_read_timeout      3600s;\nproxy_send_timeout      3600s;\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n" {
		t.Fatal("nginx fragment changed")
	}
	got := files(t, repos.Etc(), []string{"manifest.toml", "nginx.conf"})
	if string(got["manifest.toml"]) != cli.Manifest {
		t.Error("embedded manifest differs from Manifest")
	}
	if string(got["nginx.conf"]) != cli.NginxConf {
		t.Error("embedded nginx fragment differs from NginxConf")
	}
}

func files(t *testing.T, filesystem fs.FS, names []string) map[string][]byte {
	t.Helper()
	entries, err := fs.ReadDir(filesystem, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(names) {
		t.Fatalf("root entries=%d, want %d", len(entries), len(names))
	}
	result := make(map[string][]byte)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("unexpected root entry %s (%s)", entry.Name(), info.Mode())
		}
		result[entry.Name()], err = fs.ReadFile(filesystem, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		if _, ok := result[name]; !ok {
			t.Fatalf("missing root entry %s", name)
		}
	}
	return result
}

func contractNow() time.Time                       { return time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC) }
func contractAfter(time.Duration) <-chan time.Time { return make(chan time.Time) }

type contractRandom struct {
	mu sync.Mutex
	n  byte
}

func (r *contractRandom) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

type contractFixture struct {
	dir     string
	git     *repogit.Git
	store   *store.Store
	limits  *limits.Limits
	bus     *events.Emitter
	writer  *telemetry.Writer
	capture *telemetry.Capture
}

func gitFixture(t *testing.T, dir string) (string, []string, *repogit.Git) {
	t.Helper()
	executable, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + filepath.Dir(executable), "HOME=" + dir, "XDG_CONFIG_HOME=" + dir,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=",
		"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.test", "GIT_COMMITTER_NAME=Fixture",
		"GIT_COMMITTER_EMAIL=fixture@example.test", "GIT_AUTHOR_DATE=2001-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2001-02-03T04:05:06Z"}
	g, err := repogit.Find(filepath.Dir(executable), func() []string { return append([]string(nil), env...) })
	if err != nil {
		t.Fatal(err)
	}
	return executable, env, g
}

func newContractFixture(t *testing.T) *contractFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &contractFixture{dir: t.TempDir(), capture: &telemetry.Capture{}}
	_, _, f.git = gitFixture(t, f.dir)
	var err error
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(f.dir, "catalog.db"), Migrations: repos.Migrations(), Now: contractNow})
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = store.Open(t.Context(), d, store.Config{Root: filepath.Join(f.dir, "repos"), Git: f.git, Now: contractNow, Rand: &contractRandom{}})
	if err != nil {
		t.Fatal(err)
	}
	f.limits = limits.New(settings.Defaults(), limits.Clock{Now: contractNow, After: contractAfter})
	f.writer = telemetry.New(telemetry.Config{Service: web.ServiceName, Version: "fixture-display", Sink: f.capture,
		Stderr: io.Discard, Now: contractNow, Rand: &contractRandom{}, Sleep: func(context.Context, time.Duration) {}})
	f.bus = events.New(events.Config{Service: web.ServiceName, Sink: &events.Capture{}, Stderr: io.Discard, Now: contractNow, Rand: &contractRandom{}, Telemetry: f.writer, Emits: smarthttp.Emits()})
	t.Cleanup(func() {
		f.bus.Shutdown(context.Background())
		f.writer.Shutdown(context.Background(), "test complete")
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

// R-UI1W-LL6T R-SXIX-FTGL R-SYQT-TL7A R-T4UB-QFWR: Construct the exact
// store types and call each catalog, verification and path operation.
func TestStorePublicContract(t *testing.T) {
	f := newContractFixture(t)
	r, err := f.store.Create(t.Context(), "owner", "notes")
	if err != nil {
		t.Fatal(err)
	}
	want := store.Repo{ID: r.ID, Name: "notes", Owner: "owner", Created: contractNow(), Available: true}
	if r != want {
		t.Fatalf("Repo=%+v, want %+v", r, want)
	}
	if store.ErrNotFound == nil {
		t.Fatal("nil ErrNotFound")
	}
	if _, err := f.store.Find(t.Context(), "owner", "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Find missing: %v", err)
	}
	if err := f.store.Verify(t.Context(), f.writer); err != nil {
		t.Fatal(err)
	}
	for _, list := range []func(context.Context) ([]store.Repo, error){f.store.All, func(ctx context.Context) ([]store.Repo, error) { return f.store.List(ctx, "owner") }} {
		got, err := list(t.Context())
		if err != nil || len(got) != 1 || got[0] != r {
			t.Fatalf("catalog=%+v, %v", got, err)
		}
	}
	got, err := f.store.Find(t.Context(), "owner", r.ID)
	if err != nil || got != r {
		t.Fatalf("Find=%+v, %v", got, err)
	}
	if size, err := f.store.Size(t.Context(), r.ID); err != nil || size < 0 {
		t.Fatalf("Size=%d, %v", size, err)
	}
	for _, id := range []string{r.ID, "", "arbitrary/name", "../outside", "already.git"} {
		if got, want := f.store.Dir(id), filepath.Join(f.dir, "repos", id+".git"); got != want {
			t.Errorf("Dir(%q)=%q, want %q", id, got, want)
		}
	}
}

// R-XSTZ-E5Q0: Open distinguishes unusable repository roots.
func TestStoreStartupErrorKinds(t *testing.T) {
	dir := t.TempDir()
	_, _, g := gitFixture(t, dir)
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(dir, "catalog.db"), Migrations: repos.Migrations(), Now: contractNow})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	block := filepath.Join(dir, "file")
	if err := os.WriteFile(block, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), d, store.Config{Root: block, Git: g, Now: contractNow, Rand: strings.NewReader("abcdefgh")})
	if s != nil || store.ErrRoot == nil || !errors.Is(err, store.ErrRoot) || errors.Is(store.ErrRoot, store.ErrNotFound) {
		t.Fatalf("Open=%v,%v", s, err)
	}
}

// R-T16M-L4OO R-T2EI-YWFD R-T3MF-CO62: Exercise the complete byte alphabets
// and the boundaries, rather than only example strings.
func TestRepositoryValidators(t *testing.T) {
	const prefix = store.IDPrefix + ""
	if prefix != "rep_" {
		t.Fatalf("IDPrefix=%q", prefix)
	}
	base := "0123456789abcdef"
	if !store.ValidID(prefix + base) {
		t.Fatal("valid ID refused")
	}
	for n := 0; n <= 18; n++ {
		if got, want := store.ValidID(prefix+strings.Repeat("a", n)), n == 16; got != want {
			t.Errorf("ID length %d=%t", n, got)
		}
	}
	for b := 0; b < 256; b++ {
		s := prefix + base[:5] + string(byte(b)) + base[6:]
		want := b >= '0' && b <= '9' || b >= 'a' && b <= 'f'
		if store.ValidID(s) != want {
			t.Errorf("ID byte %d", b)
		}
		name := "a" + string(byte(b)) + "z"
		want = b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-'
		if store.ValidName(name) != want {
			t.Errorf("name byte %d", b)
		}
		first := string(byte(b)) + "z"
		if store.ValidName(first) != (want && b != '-') {
			t.Errorf("initial name byte %d", b)
		}
	}
	for n := 0; n <= 65; n++ {
		if store.ValidName(strings.Repeat("a", n)) != (n >= 1 && n <= 64) {
			t.Errorf("name length %d", n)
		}
	}
	for _, id := range []string{"", "REP_" + base, base, "x" + prefix + base, prefix + base + "x"} {
		if store.ValidID(id) {
			t.Errorf("invalid ID accepted %q", id)
		}
	}
}

// R-T628-47NG R-T7A4-HZE5 R-T8I0-VR4U R-T9PX-9IVJ: Find uses the first
// eligible absolute directory and accepts executable symlinks only.
func TestGitDiscoveryPublicContract(t *testing.T) {
	_ = repogit.Git{}
	const copySize = repogit.CopyBufferSize + 0.5
	const headerSize = repogit.MaxHeaderBytes + 0.5
	if copySize != 32768.5 || headerSize != 65536.5 {
		t.Fatal("git constants changed")
	}
	dir := t.TempDir()
	executable, env, _ := gitFixture(t, dir)
	first, second, notExec, notFile := filepath.Join(dir, "first"), filepath.Join(dir, "second"), filepath.Join(dir, "not-executable"), filepath.Join(dir, "not-file")
	for _, path := range []string{first, second, notExec, notFile} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{first, second} {
		if err := os.Symlink(executable, filepath.Join(path, "git")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(notExec, "git"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(notFile, "git"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", ":relative", notExec + ":" + notFile, filepath.Join(dir, "absent")} {
		g, err := repogit.Find(path, func() []string { return env })
		if g != nil || repogit.ErrNotFound == nil || !errors.Is(err, repogit.ErrNotFound) {
			t.Errorf("Find(%q)=%v,%v", path, g, err)
		}
	}
	g, err := repogit.Find(":"+"relative:"+notExec+":"+notFile+":"+first+":"+second, func() []string { return env })
	if err != nil || g == nil {
		t.Fatalf("Find=%v,%v", g, err)
	}
	cmd := g.Command(t.Context(), dir, nil, "--version")
	if cmd.Path != filepath.Join(first, "git") || cmd.Process != nil {
		t.Fatalf("selected executable %q", cmd.Path)
	}
	if out, err := g.Output(t.Context(), dir, "--version"); err != nil || !strings.HasPrefix(string(out), "git version ") {
		t.Fatalf("Output=%s,%v", out, err)
	}
}

// R-EZO7-KJOV R-RE62-MIXJ: Commands observe the supplied environment at each
// call, honor their directory, and report nonzero exits and cancellation.
func TestGitOutputEnvironmentAndFailures(t *testing.T) {
	dir := t.TempDir()
	executable, env, _ := gitFixture(t, dir)
	config := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(config, []byte("[fixture]\nvalue = first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	current := append(append([]string(nil), env...), "GIT_CONFIG_GLOBAL="+config)
	g, err := repogit.Find(filepath.Dir(executable), func() []string { return current })
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"first", "second"} {
		if value == "second" {
			other := filepath.Join(dir, "other-config")
			if err := os.WriteFile(other, []byte("[fixture]\nvalue = second\n"), 0600); err != nil {
				t.Fatal(err)
			}
			current = append(append([]string(nil), env...), "GIT_CONFIG_GLOBAL="+other)
		}
		if out, err := g.Output(t.Context(), dir, "config", "--get", "fixture.value"); err != nil || string(out) != value+"\n" {
			t.Fatalf("Output=%q,%v", out, err)
		}
	}
	if _, err := g.Output(t.Context(), dir, "config", "--get", "fixture.absent"); err == nil {
		t.Error("nonzero exit succeeded")
	}
	if _, err := g.Output(t.Context(), filepath.Join(dir, "absent"), "--version"); err == nil {
		t.Error("missing working directory succeeded")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := g.Output(ctx, dir, "--version"); err == nil {
		t.Error("cancelled Output succeeded")
	}
	if _, err := g.Output(t.Context(), dir, "init", "--bare", "fixture.git"); err != nil {
		t.Fatal(err)
	}
	if out, err := g.Output(t.Context(), filepath.Join(dir, "fixture.git"), "rev-parse", "--is-bare-repository"); err != nil || string(out) != "true\n" {
		t.Fatalf("directory Output=%q,%v", out, err)
	}
}

// R-TDDM-EU3M R-TELI-SLUB R-RFDZ-0AO8: Call the exported clock and settings,
// and drive a repository hold through the exported methods.
func TestLimitsPublicContract(t *testing.T) {
	if limits.Fetch != "fetch" || limits.Push != "push" || limits.Maintenance != "maintenance" {
		t.Fatal("operation names changed")
	}
	s := settings.Defaults()
	s.ReadSlots = 3
	s.WriteSlots = 4
	marker := make(chan time.Time)
	var duration time.Duration
	l := limits.New(s, limits.Clock{Now: contractNow, After: func(d time.Duration) <-chan time.Time { duration = d; return marker }})
	if l.Settings() != s || l.Clock().Now() != contractNow() || l.Clock().After(17*time.Minute) != marker || duration != 17*time.Minute {
		t.Fatal("settings or injected clock changed")
	}
	defaults := limits.New(s, limits.Clock{}).Clock()
	if defaults.Now == nil || defaults.After == nil {
		t.Fatal("nil default clock")
	}
	release, ok := l.TryHold("repo-fixture")
	if !ok || release == nil || !l.Busy("repo-fixture") {
		t.Fatal("hold not active")
	}
	release()
	if l.Busy("repo-fixture") {
		t.Fatal("hold not released")
	}
	l.Drain()
	if !l.Draining() {
		t.Fatal("Drain did not take effect")
	}
}

// R-TLWX-38AH R-RHTR-RU5M R-RJ1O-5LWB R-RK9K-JDN0: Consume all clone
// exports, including nested/derived context and unmodified URL inputs.
func TestClonePublicContract(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://fixture.test/", nil)
	if got := clone.Base(request, filepath.Join(t.TempDir(), "absent")); got != "https://fixture.test" {
		t.Fatalf("Base=%q", got)
	}
	for _, pair := range [][2]string{{"", ""}, {"https://fixture.test/", "name/with/slash"}, {" arbitrary ", "question?"}} {
		if got, want := clone.URL(pair[0], pair[1]), pair[0]+"/"+pair[1]+".git"; got != want {
			t.Errorf("URL=%q, want %q", got, want)
		}
	}
	ctx := t.Context()
	if b, ok := clone.FromContext(ctx); b != "" || ok {
		t.Fatal("empty context has base")
	}
	for _, base := range []string{"https://first.test", "", "https://last.test"} {
		ctx = clone.NewContext(ctx, base)
		derived, cancel := context.WithCancel(ctx)
		b, ok := clone.FromContext(derived)
		cancel()
		if b != base || !ok {
			t.Fatalf("derived base=%q,%t", b, ok)
		}
	}
	credentials := clone.Credentials{Intro: "intro", Helper: "helper", Warning: "warning"}
	if credentials.Text() != "intro\n\nhelper\n\nwarning" {
		t.Fatal("Credentials.Text changed")
	}
	guidance := clone.Guidance("https://repos.fixture.test")
	if guidance.Intro == "" || guidance.Helper == "" || guidance.Warning == "" || guidance.Text() != guidance.Intro+"\n\n"+guidance.Helper+"\n\n"+guidance.Warning {
		t.Fatal("Guidance did not supply credentials text")
	}
}

// R-SK41-8CAY R-U3B1-EAAS R-TJH4-BOT3:
// Construct the exact exported configs and serve a route and MCP discovery.
func TestHandlerPublicContracts(t *testing.T) {
	const name = web.ServiceName + ""
	const description = web.Description + ""
	if name != "repos" || description == "" || strings.ContainsAny(description, "\"\\") || strings.ContainsFunc(description, unicode.IsControl) {
		t.Fatal("service metadata changed")
	}
	f := newContractFixture(t)
	mcpServer := mcp.NewServer(mcp.ServerConfig{Name: name, Version: "fixture-display", Telemetry: f.writer})
	h := web.Handler(web.Config{Banner: func(u page.User) page.Banner {
		return page.Banner{Service: name, Version: "fixture-display", Email: u.Email}
	}, MCP: mcpServer, ServicesPath: filepath.Join(f.dir, "services.json"), Store: f.store, Git: f.git, Limits: f.limits, Telemetry: f.writer, Events: f.bus})
	r := httptest.NewRequest(http.MethodGet, "http://fixture.test/", nil)
	r.Header.Set("X-User-Id", "owner")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("landing=%d: %s", rec.Code, rec.Body.String())
	}
	smart := smarthttp.Handler(smarthttp.Config{Store: f.store, Git: f.git, Limits: f.limits, Telemetry: f.writer, Events: f.bus})
	rec = httptest.NewRecorder()
	smart.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://fixture.test/missing.git/info/refs?service=git-upload-pack", nil))
	if rec.Code == http.StatusOK {
		t.Fatal("missing git repository succeeded")
	}
	direct := mcp.NewServer(mcp.ServerConfig{Name: name, Version: "fixture-display", Telemetry: f.writer})
	tools.Register(direct, tools.Config{Store: f.store, Limits: f.limits, Telemetry: f.writer})
	server := httptest.NewServer(identity.Require(direct))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client()})
	listed, err := client.ListTools(t.Context(), identity.Caller{UserID: "owner", RequestID: "request-fixture"})
	if err != nil || len(listed) != 6 {
		t.Fatalf("registered tools=%v,%v", listed, err)
	}
}

// R-F0W3-YBFK: Exercise both scheduling and immediate cycles with each
// declared config field, without firing the injected interval.
func TestMaintenancePublicContract(t *testing.T) {
	f := newContractFixture(t)
	calls := 0
	cfg := maintenance.Config{Store: f.store, Git: f.git, Limits: f.limits, Telemetry: f.writer, Hold: func(context.Context, string) { calls++ }}
	exercise := func(scheduler *maintenance.Scheduler) {
		if scheduler == nil {
			t.Fatal("nil scheduler")
		}
		maintenance.Cycle(t.Context(), cfg)
		scheduler.Stop(t.Context())
		if calls != 0 {
			t.Fatal("maintenance held a nonexistent repository")
		}
	}
	exercise(maintenance.Start(cfg))
}

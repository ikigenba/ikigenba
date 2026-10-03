package tools_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/clone"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/settings"
	"github.com/ikigenba/ikigenba/repos/internal/store"
	"github.com/ikigenba/ikigenba/repos/internal/tools"
)

type toolsFixture struct {
	Store       *store.Store
	StoreConfig store.Config
	Git         *git.Git
	GitPath     string
	Env         []string
	Limits      *limits.Limits
	Writer      *telemetry.Writer
	Capture     *telemetry.Capture
	Client      *mcp.Client
	Caller      identity.Caller
	Base        string
	Root        string
	DBParent    string
}

func toolsContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func toolsMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func toolsEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func newToolsFixture(t *testing.T) *toolsFixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	root := t.TempDir()
	path, err := exec.LookPath("git")
	toolsMust(t, err)
	env := []string{"PATH=" + filepath.Dir(path), "HOME=" + root, "XDG_CONFIG_HOME=" + root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2001-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2001-02-03T04:05:06Z"}
	g, err := git.Find(filepath.Dir(path), func() []string { return append([]string(nil), env...) })
	toolsMust(t, err)
	now := func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 123456789, time.FixedZone("fixture", 3600)) }
	parent := filepath.Join(root, "catalog")
	cfg := store.Config{Source: filepath.Join(parent, "repos.sqlite"), Root: filepath.Join(root, "repos"), Git: g, Now: now, Rand: toolsIDSource()}
	s, err := store.Open(toolsContext(t), cfg)
	toolsMust(t, err)
	t.Cleanup(func() { toolsMust(t, s.Close()) })
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "repos", Version: "fixture", Sink: capture, Stderr: io.Discard, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 4096)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		w.Shutdown(ctx, "test")
	})
	l := limits.New(settings.Defaults(), limits.Clock{Now: now, After: func(time.Duration) <-chan time.Time { t.Error("unexpected Limits.After"); return make(chan time.Time) }})
	f := &toolsFixture{Store: s, StoreConfig: cfg, Git: g, GitPath: path, Env: env, Limits: l, Writer: w, Capture: capture, Caller: identity.Caller{UserID: "owner", RequestID: "fixture-request"}, Base: "https://repos.fixture.invalid", Root: cfg.Root, DBParent: parent}
	f.Client = serveTools(t, tools.Config{Store: s, Limits: l, Telemetry: w}, f.Base, false)
	return f
}

func serveTools(t *testing.T, cfg tools.Config, base string, withMiddleware bool) *mcp.Client {
	t.Helper()
	srv := mcp.NewServer(mcp.ServerConfig{Name: "repos", Version: "fixture", Telemetry: cfg.Telemetry})
	tools.Register(srv, cfg)
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(clone.NewContext(r.Context(), base)))
	})
	h = identity.Require(h)
	if withMiddleware {
		h = telemetry.Middleware(cfg.Telemetry, h)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 15 * time.Second
	return mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: client, Name: "fixture", Version: "fixture"})
}

func (f *toolsFixture) call(t *testing.T, name, args string) mcp.Result {
	t.Helper()
	var raw json.RawMessage
	if args != "" {
		raw = json.RawMessage(args)
	}
	result, err := f.Client.CallTool(toolsContext(t), f.Caller, name, raw)
	toolsMust(t, err)
	return result
}
func (f *toolsFixture) create(t *testing.T, owner, name string) store.Repo {
	t.Helper()
	r, err := f.Store.Create(toolsContext(t), owner, name)
	toolsMust(t, err)
	return r
}
func (f *toolsFixture) events(t *testing.T) []telemetry.Event {
	t.Helper()
	toolsMust(t, f.Writer.Flush(toolsContext(t)))
	return f.Capture.Events()
}
func (f *toolsFixture) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(toolsContext(t), f.GitPath, args...)
	cmd.Dir = dir
	cmd.Env = append([]string(nil), f.Env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
func resultMembers(t *testing.T, result mcp.Result) map[string]json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(result)
	toolsMust(t, err)
	var members map[string]json.RawMessage
	toolsMust(t, json.Unmarshal(raw, &members))
	return members
}
func successObject(t *testing.T, result mcp.Result) json.RawMessage {
	t.Helper()
	members := resultMembers(t, result)
	if _, ok := members["isError"]; ok {
		t.Fatalf("unexpected isError: %s", mustResultJSON(t, result))
	}
	object, ok := members["structuredContent"]
	if !ok {
		t.Fatalf("missing structuredContent: %s", mustResultJSON(t, result))
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	toolsMust(t, json.Unmarshal(members["content"], &blocks))
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != string(object) {
		t.Fatalf("content differs from structuredContent: %s", mustResultJSON(t, result))
	}
	var compact bytes.Buffer
	toolsMust(t, json.Compact(&compact, []byte(blocks[0].Text)))
	toolsEqual(t, blocks[0].Text, compact.String())
	return object
}
func mustResultJSON(t *testing.T, result mcp.Result) string {
	t.Helper()
	raw, err := json.Marshal(result)
	toolsMust(t, err)
	return string(raw)
}
func refusal(t *testing.T, result mcp.Result, text string) {
	t.Helper()
	members := resultMembers(t, result)
	if _, ok := members["structuredContent"]; ok {
		t.Fatalf("refusal has structuredContent: %s", mustResultJSON(t, result))
	}
	want := resultMembers(t, mcp.ErrorResult(text))
	delete(members, "_meta")
	toolsEqual(t, members, want)
}

type toolsDiskEntry struct {
	Mode fs.FileMode
	Data string
}

func toolsSnapshot(t *testing.T, root string) map[string]toolsDiskEntry {
	t.Helper()
	fsroot, err := os.OpenRoot(root)
	toolsMust(t, err)
	defer func() { toolsMust(t, fsroot.Close()) }()
	entries := map[string]toolsDiskEntry{}
	toolsMust(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		e := toolsDiskEntry{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			file, err := fsroot.Open(rel)
			if err != nil {
				return err
			}
			b, err := io.ReadAll(file)
			closeErr := file.Close()
			if closeErr != nil {
				return closeErr
			}
			if err != nil {
				return err
			}
			e.Data = string(b)
		} else if info.Mode()&os.ModeSymlink != 0 {
			e.Data, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		entries[rel] = e
		return nil
	}))
	return entries
}
func toolsArguments(repo, name string) string {
	b, _ := json.Marshal(map[string]string{"repo": repo, "name": name})
	return string(b)
}
func toolsRepoArgument(repo string) string { return fmt.Sprintf(`{"repo":%q}`, repo) }

func toolsIDSource() io.Reader {
	data := make([]byte, 8192)
	for i := 0; i < len(data); i += 8 {
		binary.BigEndian.PutUint64(data[i:i+8], uint64(i/8+1))
	}
	return bytes.NewReader(data)
}

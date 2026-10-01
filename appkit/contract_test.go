package appkit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
)

// R-YFJ3-L9UN
func TestConsumerOwnsOutput(t *testing.T) {
	root := t.TempDir()
	stdout := contractFile(t)
	stderr := contractFile(t)
	oldout, olderr, oldlog := os.Stdout, os.Stderr, log.Writer()
	var defaultLog bytes.Buffer
	os.Stdout, os.Stderr = stdout, stderr
	log.SetOutput(&defaultLog)
	defer func() { os.Stdout, os.Stderr = oldout, olderr; log.SetOutput(oldlog) }()
	contractExercise(t, root, true)
	for _, stream := range []*os.File{stdout, stderr} {
		if _, err := stream.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 0 {
			t.Errorf("unsupplied process stream received %q", data)
		}
	}
	if defaultLog.Len() != 0 {
		t.Errorf("default logger received %q", defaultLog.String())
	}
}

// R-9TRL-4U3V
func TestPublicAPIWorkingDirectoryIndependence(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	for i, dir := range []string{first, second} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		// Unrelated cwd files deliberately disagree; every consumer path below remains absolute.
		decoy := fmt.Sprintf(`{"services":[{"name":"cwd_%d","url":"/decoy","description":"Decoy.","socket":"/unused","enabled":true,"mcp":true,"icon":"<svg>decoy</svg>"}]}`, i)
		for _, name := range []string{"services.json", "services"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(decoy), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"banner.html", "theme.css", "launcher.js"} {
			if err := os.WriteFile(filepath.Join(dir, "assets", name), []byte(fmt.Sprintf("unrelated cwd %d", i)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Chdir(first)
	want := contractExercise(t, root, false)
	t.Chdir(second)
	got := contractExercise(t, root, false)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("working directory changed public behavior\nfirst: %#v\nsecond: %#v", want, got)
	}
}

func contractFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "capture")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

type contractInput struct {
	Value string `json:"value"`
}
type contractOutput struct {
	Value string `json:"value"`
}
type contractFloat struct {
	Value float64 `json:"value"`
}

func contractExercise(t *testing.T, root string, includePageExtra bool) []string {
	t.Helper()
	var observed []string
	note := func(name string, values ...any) { observed = append(observed, name+": "+fmt.Sprint(values...)) }
	path := filepath.Join(root, "services.json")
	content := `{"services":[{"name":"sample","url":"https://sample.example","description":"Sample.","socket":"/not-opened/sample.sock","enabled":true,"mcp":true,"icon":"<svg></svg>"}]}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(root, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, bad, filepath.Join(root, "missing"), root, ""} {
		entries, err := services.Read(p)
		note("Read", entries, err)
		entry, ok := entries.Find("sample")
		note("Find", entry, ok)
		entry, ok = entries.Find("absent")
		note("Find absent", entry, ok)
	}
	user := page.User{Email: "person@example.com", ProfileURL: "/profile", LogoutURL: "/logout"}
	for _, p := range []string{path, bad, filepath.Join(root, "missing"), ""} {
		t.Setenv(services.Variable, p)
		kit := page.New("sample", "consumer-version")
		banner := kit.Banner(user)
		note("New/Banner", banner)
		if includePageExtra {
			templates := page.Templates()
			for _, name := range []string{"banner", "launcher", "footer"} {
				var output bytes.Buffer
				err := templates.ExecuteTemplate(&output, name, banner)
				note("Templates "+name, output.String(), err)
			}
		}
	}
	if includePageExtra {
		for _, name := range []string{"theme.css", "launcher.js", "InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2", "OFL.txt", "TABLER-LICENSE.txt", "missing"} {
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
				rec := httptest.NewRecorder()
				page.Static().ServeHTTP(rec, httptest.NewRequest(method, page.StaticPrefix+name, nil))
				note("Static", rec.Code, rec.Header(), rec.Body.String())
			}
		}
	}
	caller := identity.Caller{UserID: "person", Email: "person@example.com", RequestID: "request"}
	ctx := identity.NewContext(context.Background(), caller)
	c, ok := identity.FromContext(ctx)
	note("NewContext/FromContext", c, ok)
	c, ok = identity.FromContext(context.Background())
	note("FromContext absent", c, ok)
	for _, present := range []bool{true, false} {
		for _, logging := range []bool{true, false} {
			var supplied bytes.Buffer
			var writer io.Writer
			if logging {
				writer = &supplied
			}
			handler := identity.Require("sample", writer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, ok := identity.FromContext(r.Context())
				note("Require next", c, ok)
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			if present {
				identity.Forward(caller, req)
			} else {
				identity.Forward(identity.Caller{}, req)
			}
			note("Forward", req.Header)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			note("Require", rec.Code, rec.Body.String(), supplied.String())
		}
	}
	for _, r := range []mcp.Result{mcp.TextResult("text"), mcp.ErrorResult("failure"), {}} {
		data, err := r.MarshalJSON()
		note("Result MarshalJSON/IsError", string(data), err, r.IsError())
		var parsed mcp.Result
		err = parsed.UnmarshalJSON(data)
		note("Result UnmarshalJSON", err, parsed.IsError())
	}
	for _, data := range []string{"{", `{"content":[]}`, `{"content":"bad"}`} {
		var r mcp.Result
		err := r.UnmarshalJSON([]byte(data))
		note("Result invalid", err, r.IsError())
	}
	note("RPCError.Error", (&mcp.RPCError{Code: 42, Message: "failure"}).Error())
	note("HTTPError.Error", (&mcp.HTTPError{StatusCode: 502, Body: "failure"}).Error())
	yes, no := true, false
	for _, a := range []mcp.Annotations{{}, {ReadOnlyHint: &yes}, {DestructiveHint: &no}} {
		note("ToolInfo.Effect", (mcp.ToolInfo{Annotations: a}).Effect())
	}
	note("NewServer invalid", contractPanic(func() { mcp.NewServer(mcp.ServerConfig{}) }))
	t.Setenv(services.Variable, path)
	for _, logging := range []bool{true, false} {
		var supplied bytes.Buffer
		var writer io.Writer
		if logging {
			writer = &supplied
		}
		server := mcp.NewServer(mcp.ServerConfig{Name: "sample", Version: "consumer-version", Stderr: writer})
		mcp.AddTool(server, mcp.Tool[contractInput, contractOutput]{Name: "typed", Description: "Echo input.", Effect: mcp.Read, Handler: func(_ context.Context, _ identity.Caller, in contractInput) (contractOutput, error) {
			return contractOutput(in), nil
		}})
		mcp.AddTool(server, mcp.Tool[contractInput, contractOutput]{Name: "fail", Description: "Fail call.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, contractInput) (contractOutput, error) {
			return contractOutput{}, errors.New("handler failed")
		}})
		mcp.AddTool(server, mcp.Tool[contractInput, contractFloat]{Name: "bad_output", Description: "Return invalid output.", Effect: mcp.Read, Handler: func(context.Context, identity.Caller, contractInput) (contractFloat, error) {
			return contractFloat{math.Inf(1)}, nil
		}})
		mcp.AddRawTool(server, mcp.RawTool[contractInput]{Name: "raw", Description: "Return raw text.", Effect: mcp.Additive, Handler: func(context.Context, identity.Caller, contractInput) (mcp.Result, error) {
			return mcp.TextResult("raw"), nil
		}})
		mcp.AddRawTool(server, mcp.RawTool[contractInput]{Name: "panic_call", Description: "Panic during call.", Effect: mcp.Destructive, Handler: func(context.Context, identity.Caller, contractInput) (mcp.Result, error) { panic("consumer panic") }})
		note("AddTool invalid", contractPanic(func() { mcp.AddTool(server, mcp.Tool[contractInput, contractOutput]{Name: "invalid"}) }))
		note("AddRawTool invalid", contractPanic(func() { mcp.AddRawTool(server, mcp.RawTool[contractInput]{Name: "invalid"}) }))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader("{")))
		note("ServeHTTP missing caller", rec.Code, rec.Body.String())
		rec = httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://example.test/mcp", strings.NewReader("{"))
		req = req.WithContext(ctx)
		server.ServeHTTP(rec, req)
		note("ServeHTTP invalid", rec.Code, rec.Header(), rec.Body.String())
		host := httptest.NewServer(identity.Require("sample", writer, server))
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client(), Name: "consumer", Version: "consumer-version"})
		tools, err := client.ListTools(ctx, caller)
		// JSON records schema bytes and pointer annotation values rather than pointer addresses.
		data, jsonErr := json.Marshal(tools)
		note("ListTools", string(data), err, jsonErr)
		for _, name := range []string{"typed", "raw", "fail", "panic_call", "bad_output", "unknown"} {
			result, err := client.CallTool(ctx, caller, name, json.RawMessage(`{"value":"echo"}`))
			data, jsonErr := result.MarshalJSON()
			note("CallTool "+name, string(data), err, jsonErr)
		}
		_, err = client.CallTool(ctx, caller, "typed", json.RawMessage("{"))
		note("CallTool invalid args", err)
		host.Close()
		note("supplied mcp diagnostics", supplied.String())
	}
	for _, cfg := range []mcp.ClientConfig{{}, {Endpoint: "://invalid"}} {
		client := mcp.NewClient(cfg)
		tools, err := client.ListTools(ctx, caller)
		note("ListTools invalid endpoint", tools, err)
		result, err := client.CallTool(ctx, caller, "typed", nil)
		data, jsonErr := result.MarshalJSON()
		note("CallTool invalid endpoint", string(data), err, jsonErr)
	}
	return observed
}

func contractPanic(fn func()) (value any) {
	defer func() { value = recover() }()
	fn()
	return nil
}

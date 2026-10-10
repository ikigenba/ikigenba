package appkit_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
)

// R-XNGM-IWEW
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

// R-XOOI-WO5L
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
		for _, name := range []string{"banner.html", "theme.css", "feedback.js"} {
			if err := os.WriteFile(filepath.Join(dir, "assets", name), []byte(fmt.Sprintf("unrelated cwd %d", i)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Chdir(first)
	want := contractExercise(t, root, false)
	t.Chdir(second)
	got := contractExercise(t, root, false)
	if !slices.Equal(got, want) {
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
	for _, commit := range []string{"", "abc", "abcdefghijk", "abcdefghijk-dirty", "世甲乙丙丁戊己庚辛-dirty"} {
		for _, release := range []string{"", "host-label"} {
			t.Setenv(version.CommitVariable, commit)
			t.Setenv(version.ReleaseVariable, release)
			id := version.Read()
			note("version.Read", id)
			note("Identity.String", id.String())
			note("version.Display", version.Display())
		}
	}
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
		kit := page.New("sample", version.Identity{Release: "consumer-release", Commit: "consumer-commit"})
		banner := kit.Banner(user)
		note("New/Banner", banner)
		if includePageExtra {
			templates := page.Templates()
			for _, name := range []string{"banner", "footer", "preload"} {
				var output bytes.Buffer
				err := templates.ExecuteTemplate(&output, name, banner)
				note("Templates "+name, output.String(), err)
			}
		}
	}
	if includePageExtra {
		note("PreloadURL", page.PreloadURL())
		for _, name := range []string{"theme.css", "feedback.js", "favicon.svg", "InterVariable.woff2", "InterVariable-Italic.woff2", "JetBrainsMono.woff2", "OFL.txt", "TABLER-LICENSE.txt", "missing"} {
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
		handler := identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		note("Require", rec.Code, rec.Body.String())
		optional := identity.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := identity.FromContext(r.Context())
			note("Optional next", c, ok)
			w.WriteHeader(http.StatusNoContent)
		}))
		rec = httptest.NewRecorder()
		optional.ServeHTTP(rec, req)
		note("Optional", rec.Code, rec.Body.String())
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
		var diagnostics io.Writer
		if logging {
			diagnostics = &supplied
		}
		capture := &telemetry.Capture{}
		writer, pauses := contractWriter(t, capture, diagnostics)
		server := mcp.NewServer(mcp.ServerConfig{Name: "sample", Version: "consumer-version", Telemetry: writer})
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
		host := httptest.NewServer(telemetry.Middleware(writer, identity.Require(server)))
		t.Cleanup(host.Close)
		client := mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: telemetry.SiblingClient(writer, "sample", host.Client().Transport), Name: "consumer", Version: "consumer-version"})
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
		writer.Shutdown(context.Background(), "done")
		note("MCP telemetry", capture.Events(), *pauses)
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
	contractTelemetryExercise(ctx, t, root, note)
	contractDatabaseExercise(t, root, note)
	return observed
}

func contractPanic(fn func()) (value any) {
	defer func() { value = recover() }()
	fn()
	return nil
}

// R-XFFJ-0MAV
func TestTelemetryPackageImport(*testing.T) {
	// The unaliased import and calls prove the published package path and name by use.
	var capture telemetry.Capture
	_ = capture.Events()
}

func contractWriter(t *testing.T, sink telemetry.Sink, stderr io.Writer) (*telemetry.Writer, *[]time.Duration) {
	t.Helper()
	var pauses []time.Duration
	writer := telemetry.New(telemetry.Config{
		Service: "sample", Version: "consumer-version", Sink: sink, Stderr: stderr,
		Now:   func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC) },
		Sleep: func(_ context.Context, duration time.Duration) { pauses = append(pauses, duration) },
		Rand:  strings.NewReader(strings.Repeat("0123456789abcdef", 64)),
	})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "cleanup") })
	return writer, &pauses
}

type contractSink func(context.Context, telemetry.Event) error

func (sink contractSink) Deliver(ctx context.Context, event telemetry.Event) error {
	return sink(ctx, event)
}

func contractTelemetryExercise(ctx context.Context, t *testing.T, root string, note func(string, ...any)) {
	t.Helper()
	capture := &telemetry.Capture{}
	var supplied bytes.Buffer
	writer, pauses := contractWriter(t, capture, &supplied)
	note("Writer.Now", writer.Now())
	writer.Ready()
	writer.Ready()
	writer.Emit(ctx, "sample.observed", telemetry.Attrs{"value": "metadata"})
	writer.Emit(ctx, "bad name", telemetry.Attrs{"value": []string{"invalid"}})
	note("Writer.Flush", writer.Flush(context.Background()))
	writer.Shutdown(context.Background(), "done")
	writer.Emit(ctx, "sample.observed", nil)
	note("Writer/Capture", capture.Events(), supplied.String(), *pauses)
	event := telemetry.Event{Time: writer.Now(), Service: "sample", Name: "sample.observed", Attrs: telemetry.Attrs{"value": int64(1)}}
	data, err := event.MarshalJSON()
	note("Event.MarshalJSON", string(data), err)
	note("Capture.Deliver", capture.Deliver(ctx, event))
	note("Capture.Events", capture.Events())
	_, err = (telemetry.Event{}).MarshalJSON()
	note("Event invalid", err)
	note("New invalid", contractPanic(func() { telemetry.New(telemetry.Config{}) }))
	note("Middleware invalid", contractPanic(func() { telemetry.Middleware(nil, http.NotFoundHandler()) }))
	note("SiblingClient invalid", contractPanic(func() { telemetry.SiblingClient(nil, "sample", nil) }))
	note("IngestHandler invalid", contractPanic(func() { telemetry.IngestHandler(nil) }))
	for _, failure := range []error{errors.New("sink unavailable"), telemetry.ErrRejected} {
		for _, logging := range []bool{true, false} {
			var diagnostics bytes.Buffer
			var output io.Writer
			if logging {
				output = &diagnostics
			}
			reject := contractSink(func(context.Context, telemetry.Event) error { return failure })
			failed, failedPauses := contractWriter(t, reject, output)
			failed.Emit(ctx, "sample.observed", nil)
			note("Failed writer Flush", failed.Flush(context.Background()))
			failed.Shutdown(context.Background(), "done")
			note("Failed writer diagnostics", diagnostics.String(), *failedPauses)
		}
	}
	middlewareCapture := &telemetry.Capture{}
	middlewareWriter, middlewarePauses := contractWriter(t, middlewareCapture, &supplied)
	for _, authenticated := range []bool{true, false} {
		handler := telemetry.Middleware(middlewareWriter, identity.Require(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			middlewareWriter.Emit(req.Context(), "sample.observed", nil)
			w.WriteHeader(http.StatusNoContent)
		})))
		req := httptest.NewRequest(http.MethodGet, "http://example.test/path?private=value", nil)
		if authenticated {
			identity.Forward(identity.Caller{UserID: "person", Email: "person@example.com", RequestID: "request"}, req)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		note("Middleware Require", rec.Code, rec.Header(), rec.Body.String())
	}
	middlewareWriter.Shutdown(context.Background(), "done")
	note("Middleware events", middlewareCapture.Events(), *middlewarePauses)
	wireCapture := &telemetry.Capture{}
	ingest := telemetry.IngestHandler(wireCapture)
	for _, body := range []string{string(data), "{", strings.Repeat(" ", telemetry.MaxEventBytes+1)} {
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			for _, mediaType := range []string{"application/json", "text/plain"} {
				req := httptest.NewRequest(method, "http://telemetry"+telemetry.IngestPath, strings.NewReader(body))
				req.Header.Set("Content-Type", mediaType)
				rec := httptest.NewRecorder()
				ingest.ServeHTTP(rec, req)
				note("IngestHandler", rec.Code, rec.Header(), rec.Body.String())
			}
		}
	}
	socket := filepath.Join(root, "wire.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(telemetry.IngestPath, ingest)
	mux.HandleFunc("/sibling", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-User-Id", req.Header.Get("X-User-Id"))
		w.WriteHeader(http.StatusNoContent)
	})
	host := &http.Server{Handler: mux, ErrorLog: log.New(&supplied, "", 0), ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- host.Serve(listener) }()
	defer func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	}()
	path := filepath.Join(root, "telemetry.json")
	content, err := json.Marshal(map[string]any{"services": []map[string]any{{"name": telemetry.ServiceName, "url": "http://telemetry", "description": "Trail.", "socket": socket, "enabled": true, "mcp": false}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(services.Variable, path)
	sink := telemetry.NewSocketSink()
	note("NewSocketSink Deliver", sink.Deliver(ctx, event))
	note("NewSocketSink invalid", sink.Deliver(ctx, telemetry.Event{}))
	defaultWriter, defaultPauses := contractWriter(t, nil, &supplied)
	defaultWriter.Ready()
	defaultWriter.Emit(ctx, "sample.observed", nil)
	note("Socket writer Flush", defaultWriter.Flush(context.Background()))
	defaultWriter.Shutdown(context.Background(), "done")
	note("Wire events", wireCapture.Events(), *defaultPauses)
	transport := telemetry.SocketTransport(socket)
	defer transport.CloseIdleConnections()
	siblingCapture := &telemetry.Capture{}
	siblingWriter, siblingPauses := contractWriter(t, siblingCapture, &supplied)
	client := telemetry.SiblingClient(siblingWriter, "sibling", transport)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://sibling/sibling?private=value", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	note("SocketTransport/SiblingClient", response.StatusCode, response.Header.Get("X-User-Id"))
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	siblingWriter.Shutdown(context.Background(), "done")
	note("Sibling events", siblingCapture.Events(), *siblingPauses)
	for _, missing := range []string{"", filepath.Join(root, "missing"), filepath.Join(root, "bad.json")} {
		t.Setenv(services.Variable, missing)
		note("Socket sink unavailable", sink.Deliver(ctx, event))
	}
	note("Supplied telemetry diagnostics", supplied.String())
}

// R-NNUH-KZA9
func TestDatabasePackageImport(t *testing.T) {
	cfg := db.Config{Path: filepath.Join(t.TempDir(), "import.db"), Migrations: fstest.MapFS{}, Now: func() time.Time { return time.Unix(42, 0) }}
	handle, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func contractDatabaseExercise(t *testing.T, root string, note func(string, ...any)) {
	t.Helper()
	ctx := context.Background()
	var diagnostics bytes.Buffer
	cfg := db.Config{Path: filepath.Join(root, "contract.db"), Migrations: fstest.MapFS{"0001_data.sql": &fstest.MapFile{Data: []byte("CREATE TABLE data(n INTEGER)")}}, Now: func() time.Time { return time.Unix(42, 0) }, Service: "sample", Stderr: &diagnostics}
	handle, err := db.Open(ctx, cfg)
	note("db.Open", handle != nil, err)
	if err != nil {
		t.Fatal(err)
	}
	openedHandle := handle
	t.Cleanup(func() {
		if err := openedHandle.Close(); err != nil {
			t.Error(err)
		}
	})
	note("db.Write", handle.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM data"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO data VALUES(42)")
		return err
	}))
	var n int
	readErr := handle.Read(ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT n FROM data").Scan(&n) })
	note("db.Read", readErr, n)
	sentinel := errors.New("consumer callback failure")
	for _, failing := range []bool{true, false} {
		handle.SetFailing(failing)
		note("db.Read failure", handle.Read(ctx, func(*sql.Tx) error { return sentinel }))
		note("db.Write failure", handle.Write(ctx, func(*sql.Tx) error { return sentinel }))
	}
	note("db.Read panic", contractPanic(func() { _ = handle.Read(ctx, func(*sql.Tx) error { panic("consumer panic") }) }))
	note("db.Write panic", contractPanic(func() { _ = handle.Write(ctx, func(*sql.Tx) error { panic("consumer panic") }) }))
	var status bytes.Buffer
	statusErr := db.Status(ctx, cfg, &status)
	note("db.Status", statusErr, status.String())
	note("db.Close", handle.Close(), handle.Close())
	note("db.Read closed", handle.Read(ctx, func(*sql.Tx) error { return nil }))
	note("db.Write closed", handle.Write(ctx, func(*sql.Tx) error { return nil }))
	for _, path := range []string{"", "file:invalid", root, filepath.Join(root, "bad.json")} {
		bad := cfg
		bad.Path = path
		handle, err := db.Open(ctx, bad)
		note("db.Open invalid", handle != nil, err)
		status.Reset()
		statusErr := db.Status(ctx, bad, &status)
		note("db.Status invalid", statusErr, status.String())
	}
	invalid := cfg
	invalid.Migrations = nil
	handle, err = db.Open(ctx, invalid)
	note("db.Open invalid migrations", handle != nil, err)
	status.Reset()
	statusErr = db.Status(ctx, invalid, &status)
	note("db.Status invalid migrations", statusErr, status.String())
	for _, logging := range []bool{true, false} {
		diagnostics.Reset()
		var stderr io.Writer
		if logging {
			stderr = &diagnostics
		}
		ahead := cfg
		ahead.Path = filepath.Join(root, fmt.Sprintf("ahead-%t.db", logging))
		ahead.Stderr = stderr
		newer, err := db.Open(ctx, ahead)
		if err != nil {
			t.Fatal(err)
		}
		err = newer.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.Exec("INSERT OR REPLACE INTO schema_migrations(version, applied_at) VALUES(2, '2024-01-02T03:04:05.000000Z')")
			return err
		})
		closeErr := newer.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("prepare ahead database: write=%v close=%v", err, closeErr)
		}
		diagnostics.Reset()
		older, err := db.Open(ctx, ahead)
		note("db.Open ahead", older != nil, err, diagnostics.String())
		if err != nil {
			t.Fatal(err)
		}
		note("db.Close ahead", older.Close())
		status.Reset()
		statusErr := db.Status(ctx, ahead, &status)
		note("db.Status ahead", statusErr, status.String(), diagnostics.String())
	}
	note("db diagnostics", diagnostics.String())
}

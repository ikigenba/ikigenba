package daemon_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

type presignerFunc func(context.Context, string, time.Duration) (string, error)

func (f presignerFunc) PresignCallerIdentity(ctx context.Context, binding string, expires time.Duration) (string, error) {
	return f(ctx, binding, expires)
}

type metadataFunc func(context.Context) (cloud.InstanceFacts, error)

func (f metadataFunc) Instance(ctx context.Context) (cloud.InstanceFacts, error) {
	return f(ctx)
}

type machineProbe struct {
	machine.Machine
	apply   func(context.Context, protocol.DesiredState) error
	inspect func(context.Context, protocol.DesiredState) (protocol.Report, error)
	install func(context.Context, io.Reader, string) error
}

func (m machineProbe) Apply(ctx context.Context, s protocol.DesiredState) error {
	if m.apply != nil {
		return m.apply(ctx, s)
	}
	return m.Machine.Apply(ctx, s)
}

func (m machineProbe) Inspect(ctx context.Context, s protocol.DesiredState) (protocol.Report, error) {
	if m.inspect != nil {
		return m.inspect(ctx, s)
	}
	return m.Machine.Inspect(ctx, s)
}

func (m machineProbe) InstallDaemon(ctx context.Context, body io.Reader, digest string) error {
	if m.install != nil {
		return m.install(ctx, body, digest)
	}
	return m.Machine.InstallDaemon(ctx, body, digest)
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type reply struct {
	status int
	body   string
}

func state(version int64, digest string) reply {
	return reply{http.StatusOK, fmt.Sprintf(`{"protocol":%d,"host":"host01","version":%d,"daemon":%q}`, protocol.Version, version, digest)}
}

type scenario struct {
	daemon.Config
	now          time.Time
	sleeps       []time.Duration
	headers      []string
	requests     []string
	reports      []protocol.Report
	diagnostics  bytes.Buffer
	memory       *machine.Memory
	before       func(*http.Request, int) error
	after        func(*http.Request, *http.Response, int)
	sign         func() error
	binding      string
	bindings     []string
	requestStart time.Time
}

func newScenario(t *testing.T, replies ...reply) *scenario {
	t.Helper()
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(served.Add(1)) - 1
		if n >= len(replies) {
			t.Errorf("unexpected HTTP request %d", n+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(replies[n].status)
		_, _ = io.WriteString(w, replies[n].body)
	}))
	t.Cleanup(srv.Close)

	s := &scenario{
		now:     time.Date(2026, 11, 17, 10, 0, 0, int(250*time.Millisecond), time.FixedZone("test", -6*60*60)),
		binding: strings.TrimPrefix(srv.URL, "http://"),
	}
	s.memory = machine.NewMemory(machine.MemoryConfig{Now: func() time.Time { return s.now }, Sleep: func(context.Context, time.Duration) {}})
	s.Config = daemon.Config{
		BaseURL: srv.URL,
		Presigner: presignerFunc(func(_ context.Context, binding string, expires time.Duration) (string, error) {
			s.bindings = append(s.bindings, binding)
			if expires != hostauth.PresignExpires {
				t.Errorf("signature lifetime = %v", expires)
			}
			if s.sign != nil {
				if err := s.sign(); err != nil {
					return "", err
				}
			}
			token := "https://id.example/?X-Amz-Date=" + s.now.UTC().Format("20060102T150405Z")
			s.headers = append(s.headers, base64.RawURLEncoding.EncodeToString([]byte(token)))
			return token, nil
		}),
		Metadata: metadataFunc(func(context.Context) (cloud.InstanceFacts, error) {
			return cloud.InstanceFacts{InstanceID: "i-fake", Type: "test.type", AMI: "ami-fake"}, nil
		}),
		Machine: s.memory,
		Digest:  "my-digest",
		Now:     func() time.Time { return s.now },
		Sleep: func(_ context.Context, d time.Duration) {
			s.sleeps = append(s.sleeps, d)
			s.now = s.now.Add(d)
		},
		Stderr: &s.diagnostics,
	}
	transport := srv.Client().Transport
	s.HTTP = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		index := len(s.requests)
		if index >= len(replies)+2 {
			t.Fatalf("too many requests: %v", s.requests)
		}
		s.requests = append(s.requests, req.Method+" "+req.URL.RequestURI())
		if got, want := req.Header.Get("User-Agent"), daemon.UserAgent; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		timeout := daemon.RequestTimeout
		if req.URL.Path == protocol.BinaryPath {
			timeout = daemon.DownloadTimeout
			if value := req.Header.Get(hostauth.HeaderName); value != "" {
				t.Errorf("binary request carried identity %q", value)
			}
		} else if len(s.headers) == 0 || req.Header.Get(hostauth.HeaderName) != s.headers[len(s.headers)-1] {
			t.Errorf("request carried the wrong identity: %v", req.Header)
		}
		if !s.requestStart.IsZero() {
			upper, stop := context.WithTimeout(context.Background(), timeout)
			defer stop()
			latest, _ := upper.Deadline()
			deadline, ok := req.Context().Deadline()
			if !ok || deadline.Before(s.requestStart.Add(timeout)) || deadline.After(latest) {
				t.Errorf("request deadline = %v, want %v to %v", deadline, s.requestStart.Add(timeout), latest)
			}
		}
		if req.URL.Path == protocol.ReportPath {
			if v := req.Header.Get("Content-Type"); v != protocol.ContentJSON {
				t.Errorf("report content type = %q", v)
			}
			readBody, err := req.GetBody()
			if err != nil {
				t.Fatalf("get report body: %v", err)
			}
			var r protocol.Report
			if err := json.NewDecoder(readBody).Decode(&r); err != nil {
				t.Fatalf("decode report body: %v", err)
			}
			_ = readBody.Close()
			s.reports = append(s.reports, r)
		}
		if s.before != nil {
			if err := s.before(req, index); err != nil {
				if req.Body != nil {
					_ = req.Body.Close()
				}
				return nil, err
			}
		}
		resp, err := transport.RoundTrip(req)
		if err == nil && s.after != nil {
			s.after(req, resp, index)
		}
		return resp, err
	})}
	return s
}

func (s *scenario) run(ctx context.Context, t *testing.T) error {
	t.Helper()
	// The run seam supplies business time. Use the same deadline machinery
	// as HTTP contexts only to bracket timeout creation without waiting.
	lower, end := context.WithTimeout(context.Background(), 0)
	defer end()
	s.requestStart, _ = lower.Deadline()
	return daemon.Run(ctx, s.Config)
}

func fetchQuery(after int64, digest string) string {
	return fmt.Sprintf("GET %s?%s=%d&%s=%s", protocol.StatePath, protocol.QueryAfter, after, protocol.QueryDigest, digest)
}

func wantRequests(t *testing.T, s *scenario, want ...string) {
	t.Helper()
	if !slices.Equal(s.requests, want) {
		t.Errorf("requests = %v, want %v", s.requests, want)
	}
}

func wantPauses(t *testing.T, s *scenario, want ...time.Duration) {
	t.Helper()
	if !slices.Equal(s.sleeps, want) {
		t.Errorf("pauses = %v, want %v", s.sleeps, want)
	}
}

func wantDiagnostics(t *testing.T, s *scenario, messages ...string) {
	t.Helper()
	raw := s.diagnostics.String()
	if len(messages) != 0 && !strings.HasSuffix(raw, "\n") {
		t.Errorf("stderr lacks final LF: %q", raw)
	}
	var actual []string
	if raw != "" {
		actual = strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	}
	if len(actual) != len(messages) {
		t.Fatalf("stderr = %q, want one line per message %q", raw, messages)
	}
	for i, msg := range messages {
		if !strings.HasPrefix(actual[i], "accountsd: "+msg+": ") || strings.ContainsRune(actual[i], '\r') {
			t.Errorf("invalid diagnostic for %s: %q", msg, actual[i])
		}
	}
}

// R-68G9-C4D1 R-6GZK-0IJW
func TestRunRejectsBadBaseURL(t *testing.T) {
	for _, raw := range []string{"", "/relative", "ftp://host", "https:///missing", "https://:443", "http://user@host", "https://host?a=1", "http://host?", "http://host#"} {
		t.Run(raw, func(t *testing.T) {
			s := newScenario(t)
			s.BaseURL = raw
			if err := daemon.Run(t.Context(), s.Config); err == nil {
				t.Error("Run accepted malformed base URL")
			}
			wantRequests(t, s)
		})
	}
}

// R-6GZK-0IJW R-95C2-ZTFX R-97RV-RCXB R-L1CI-Q6CF R-6VMC-LRG8
func TestFetchFailuresBackOffWithoutChangingHost(t *testing.T) {
	bad := reply{http.StatusOK, "partial document"}
	noContent := reply{http.StatusNoContent, ""}
	s := newScenario(t, noContent, bad, bad, bad, state(4, ""), noContent, noContent, state(6, ""), noContent)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	s.BaseURL = "http://ACCOUNTS.EXAMPLE:444"
	signs := 0
	s.sign = func() error {
		signs++
		if signs == 1 {
			return errors.New("broken\r\nidentity")
		}
		return nil
	}
	s.before = func(req *http.Request, index int) error {
		if req.URL.Host != "ACCOUNTS.EXAMPLE:444" {
			t.Errorf("request host = %q", req.URL.Host)
		}
		req.URL.Host = s.binding
		if index == 0 {
			return errors.New("connection lost")
		}
		return nil
	}
	var inspected []int64
	s.Machine = machineProbe{
		Machine: s.memory,
		inspect: func(ctx context.Context, st protocol.DesiredState) (protocol.Report, error) {
			inspected = append(inspected, st.Version)
			return s.memory.Inspect(ctx, st)
		},
		install: func(context.Context, io.Reader, string) error {
			t.Fatal("fetch failure tried to install another binary")
			return nil
		},
	}
	wantAppliedBefore := []int{0, 0, 0, 0, 0, 0, 1, 1, 1, 2}
	sleep := s.Sleep
	s.Sleep = func(ctx context.Context, d time.Duration) {
		i := len(s.sleeps)
		if got := s.memory.State().Applies; i < len(wantAppliedBefore) && got != wantAppliedBefore[i] {
			t.Errorf("before pause %d: applied %d states, want %d", i, got, wantAppliedBefore[i])
		}
		sleep(ctx, d)
	}
	s.after = func(req *http.Request, _ *http.Response, _ int) {
		if req.URL.Path == protocol.ReportPath && len(s.reports) == 2 {
			cancel()
		}
	}
	if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	for _, binding := range s.bindings {
		if binding != "accounts.example:444" {
			t.Errorf("signed binding = %q", binding)
		}
	}
	first := fetchQuery(0, s.Digest)
	wantRequests(t, s, first, first, first, first, first, first, "POST "+protocol.ReportPath,
		fetchQuery(4, s.Digest), fetchQuery(4, s.Digest), "POST "+protocol.ReportPath)
	wantPauses(t, s, 5*time.Second, 10*time.Second, 20*time.Second, 40*time.Second,
		time.Minute, time.Minute, 750*time.Millisecond, time.Second, daemon.MinBackoff, time.Second)
	wantDiagnostics(t, s, daemon.DiagFetch, daemon.DiagFetch, daemon.DiagFetch, daemon.DiagFetch, daemon.DiagFetch, daemon.DiagFetch, daemon.DiagFetch)
	if first, _, _ := strings.Cut(s.diagnostics.String(), "\n"); !strings.HasSuffix(first, "broken  identity") {
		t.Errorf("presigning error was not flattened: %q", s.diagnostics.String())
	}
	if got := s.memory.State().Applies; got != 2 || !slices.Equal(inspected, []int64{4, 6}) {
		t.Errorf("applied %d states, inspected %v, want two states with versions 4 and 6", got, inspected)
	}
}

// R-97RV-RCXB
func TestFetchLetsDecoderReadTheStateStream(t *testing.T) {
	s := newScenario(t, reply{http.StatusOK, ""})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &longBody{}
	s.after = func(_ *http.Request, resp *http.Response, _ int) {
		_ = resp.Body.Close()
		resp.Body = body
	}
	s.Sleep = func(context.Context, time.Duration) { cancel() }
	if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	wantRequests(t, s, fetchQuery(0, s.Digest))
	wantDiagnostics(t, s, daemon.DiagFetch)
	if !body.closed || body.read != protocol.MaxStateBytes+1 || s.memory.State().Applies != 0 {
		t.Errorf("state stream = %+v, machine = %+v", body, s.memory.State())
	}
}

type longBody struct {
	read   int
	closed bool
}

func (b *longBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	b.read += len(p)
	// Keep a broken consumer that attempts to slurp the body bounded in the test.
	if b.read > protocol.MaxStateBytes+1 {
		return len(p), io.EOF
	}
	return len(p), nil
}

func (b *longBody) Close() error {
	b.closed = true
	return nil
}

// R-L2KF-3Y34
func TestSelfUpdateStreamsAndExitsBeforeApplyOrReport(t *testing.T) {
	binary := "\x7fELFbinary"
	digest, err := protocol.Digest(strings.NewReader(binary))
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{"older", "newer"} {
		t.Run(current, func(t *testing.T) {
			s := newScenario(t, state(1, digest), reply{http.StatusOK, binary})
			s.Digest = current
			var stream io.ReadCloser
			s.after = func(req *http.Request, resp *http.Response, _ int) {
				if req.URL.Path == protocol.BinaryPath {
					stream = resp.Body
				}
			}
			s.Machine = machineProbe{
				Machine: s.memory,
				inspect: func(context.Context, protocol.DesiredState) (protocol.Report, error) {
					t.Fatal("inspect called during self-update")
					return protocol.Report{}, nil
				},
				install: func(ctx context.Context, body io.Reader, got string) error {
					if body != stream || got != digest {
						t.Errorf("install body = %T, digest = %q", body, got)
					}
					return s.memory.InstallDaemon(ctx, body, got)
				},
			}
			if err := s.run(t.Context(), t); err != nil {
				t.Fatal(err)
			}
			wantRequests(t, s, fetchQuery(0, current), "GET "+protocol.BinaryPath)
			wantPauses(t, s)
			wantDiagnostics(t, s)
			if got := s.memory.State(); got.Daemon != digest || got.Applies != 0 {
				t.Errorf("machine after update = %+v", got)
			}
		})
	}
}

// R-L3SB-HPTT R-6PIU-OWQR R-6QQR-2OHG R-98ZS-54O0 R-6T6J-U7YU
func TestCyclesReportAndUpdateFailuresBackOffIndependently(t *testing.T) {
	ack := reply{http.StatusNoContent, ""}
	s := newScenario(t,
		state(1, "previous-digest"), reply{http.StatusBadGateway, ""}, reply{http.StatusBadRequest, ""},
		state(2, "previous-digest"), reply{http.StatusOK, "invalid"}, ack,
		state(3, "my-digest"), ack,
		state(4, "previous-digest"), reply{http.StatusServiceUnavailable, ""}, ack,
		state(5, ""), ack,
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	applyErr := errors.New(strings.Repeat("x", protocol.MaxErrorBytes-1) + "é")
	var order []string
	s.Machine = machineProbe{
		Machine: s.memory,
		apply: func(ctx context.Context, st protocol.DesiredState) error {
			order = append(order, "apply")
			if st.Version == 2 || st.Version == 5 {
				return applyErr
			}
			return s.memory.Apply(ctx, st)
		},
		inspect: func(ctx context.Context, st protocol.DesiredState) (protocol.Report, error) {
			order = append(order, "inspect")
			facts, err := s.memory.Inspect(ctx, st)
			facts.Error = "inspect supplied this"
			facts.Applied = 999
			facts.FailedUnits = make([]string, protocol.MaxFailedUnits+1)
			for i := range facts.FailedUnits {
				facts.FailedUnits[i] = fmt.Sprintf("unit-%03d", len(facts.FailedUnits)-i)
			}
			if st.Version == 2 {
				return facts, errors.New("inspect failed with partial facts")
			}
			return facts, err
		},
	}
	metadata := s.Metadata
	metadataCalls := 0
	s.Metadata = metadataFunc(func(ctx context.Context) (cloud.InstanceFacts, error) {
		order = append(order, "metadata")
		metadataCalls++
		if metadataCalls == 2 {
			return cloud.InstanceFacts{InstanceID: "partial instance", Type: "partial type", AMI: "partial image"}, errors.New("metadata failed")
		}
		return metadata.Instance(ctx)
	})
	s.before = func(req *http.Request, _ int) error {
		if req.URL.Path == protocol.ReportPath {
			order = append(order, "report")
		}
		return nil
	}
	s.after = func(req *http.Request, _ *http.Response, _ int) {
		if req.URL.Path == protocol.ReportPath && len(s.reports) == 5 {
			cancel()
		}
	}
	if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	post, get := "POST "+protocol.ReportPath, "GET "+protocol.BinaryPath
	wantRequests(t, s, fetchQuery(0, s.Digest), get, post, fetchQuery(1, s.Digest), get, post,
		fetchQuery(2, s.Digest), post, fetchQuery(3, s.Digest), get, post, fetchQuery(4, s.Digest), post)
	wantPauses(t, s, 750*time.Millisecond, daemon.MinBackoff, time.Second, 2*daemon.MinBackoff, time.Second, time.Second,
		time.Second, daemon.MinBackoff, time.Second)
	wantDiagnostics(t, s, daemon.DiagUpdate, daemon.DiagReport, daemon.DiagUpdate, daemon.DiagApply, daemon.DiagUpdate, daemon.DiagApply)
	if want := slices.Repeat([]string{"apply", "inspect", "metadata", "report"}, 5); !slices.Equal(order, want) {
		t.Errorf("calls = %v, want %v", order, want)
	}
	if sorted := slices.Sorted(slices.Values(s.headers)); len(slices.Compact(sorted)) != len(s.headers) {
		t.Errorf("headers repeated: %v", s.headers)
	}
	if len(s.reports) != 5 {
		t.Fatalf("reports = %d, want 5", len(s.reports))
	}
	for i, report := range s.reports {
		if report.Protocol != protocol.Version || report.Attempted != int64(i+1) || report.DaemonVersion != s.Digest {
			t.Errorf("report %d envelope = %+v", i, report)
		}
		if i != 1 && (report.InstanceID != "i-fake" || report.Type != "test.type" || report.AMI != "ami-fake" ||
			report.OS != machine.MemoryOS || len(report.FailedUnits) != protocol.MaxFailedUnits ||
			report.FailedUnits[0] != "unit-001" || report.FailedUnits[protocol.MaxFailedUnits-1] != "unit-100" ||
			!slices.IsSorted(report.FailedUnits)) {
			t.Errorf("report %d carried wrong facts: %+v", i, report)
		}
	}
	truncated := strings.Repeat("x", protocol.MaxErrorBytes-1)
	wantSecond := protocol.Report{Protocol: protocol.Version, Attempted: 2, Applied: 1, Error: truncated, DaemonVersion: s.Digest}
	if !reflect.DeepEqual(s.reports[1], wantSecond) || !utf8.ValidString(s.reports[1].Error) {
		t.Errorf("failed inspect/metadata or apply precedence incorrect: %+v", s.reports[1])
	}
	for i, applied := range []int64{1, 1, 3, 4, 4} {
		if got := s.reports[i].Applied; got != applied {
			t.Errorf("report %d applied = %d, want %d", i, got, applied)
		}
	}
	if s.reports[0].Error == "" || s.reports[2].Error != "" || s.reports[3].Error == "" || s.reports[4].Error != truncated {
		t.Error("reports did not carry the update/apply errors in their respective cycles")
	}
}

// R-L3SB-HPTT R-6QQR-2OHG R-98ZS-54O0
func TestTransportFailuresAndInitiallyUnappliedState(t *testing.T) {
	s := newScenario(t, state(7, "another"), state(8, "my-digest"), reply{http.StatusNoContent, ""})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.memory.SetFailure(errors.New("first apply interrupted"))
	s.before = func(req *http.Request, i int) error {
		if i == 1 || i == 2 {
			return fmt.Errorf("transport failure for %s", req.URL.Path)
		}
		return nil
	}
	s.after = func(req *http.Request, _ *http.Response, _ int) {
		if req.URL.Path == protocol.ReportPath {
			cancel()
		}
	}
	if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	wantRequests(t, s, fetchQuery(0, s.Digest), "GET "+protocol.BinaryPath, "POST "+protocol.ReportPath,
		fetchQuery(7, s.Digest), "POST "+protocol.ReportPath)
	wantDiagnostics(t, s, daemon.DiagUpdate, daemon.DiagApply, daemon.DiagReport, daemon.DiagApply)
	wantPauses(t, s, 750*time.Millisecond, daemon.MinBackoff, time.Second)
	if len(s.reports) != 2 || s.reports[0].Applied != 0 || s.reports[0].Error != "first apply interrupted" {
		t.Errorf("initial report did not capture the apply failure: %v", s.reports)
	}
}

// R-6UEG-7ZPJ
func TestRunStopsAtCancellationBetweenMachineCalls(t *testing.T) {
	for _, step := range []string{"before fetch", "fetch", "apply", "inspect", "metadata", "install"} {
		t.Run(step, func(t *testing.T) {
			digest := ""
			if step == "install" {
				digest = "another"
			}
			s := newScenario(t, state(1, digest), reply{http.StatusOK, "binary"})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var seen []string
			call := func(ctx context.Context, label string) {
				seen = append(seen, label)
				if label == step {
					cancel()
					if err := ctx.Err(); !errors.Is(err, context.Canceled) {
						t.Errorf("the context of %s was not canceled: %v", label, err)
					}
				}
			}
			s.before = func(req *http.Request, _ int) error {
				label := "fetch"
				if req.URL.Path == protocol.BinaryPath {
					label = "download"
				}
				call(req.Context(), label)
				return req.Context().Err()
			}
			s.Machine = machineProbe{
				Machine: s.memory,
				apply: func(ctx context.Context, _ protocol.DesiredState) error {
					call(ctx, "apply")
					return nil
				},
				inspect: func(ctx context.Context, _ protocol.DesiredState) (protocol.Report, error) {
					call(ctx, "inspect")
					return protocol.Report{}, nil
				},
				install: func(ctx context.Context, _ io.Reader, _ string) error {
					call(ctx, "install")
					return nil
				},
			}
			s.Metadata = metadataFunc(func(ctx context.Context) (cloud.InstanceFacts, error) {
				call(ctx, "metadata")
				return cloud.InstanceFacts{}, nil
			})
			if step == "before fetch" {
				cancel()
			}
			if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
				t.Fatalf("Run = %v", err)
			}
			want := map[string][]string{
				"before fetch": nil,
				"fetch":        {"fetch"},
				"apply":        {"fetch", "apply"},
				"inspect":      {"fetch", "apply", "inspect"},
				"metadata":     {"fetch", "apply", "inspect", "metadata"},
				"install":      {"fetch", "download", "install"},
			}
			if !slices.Equal(seen, want[step]) || len(s.reports) != 0 {
				t.Errorf("calls = %v, want %v, reports = %d", seen, want[step], len(s.reports))
			}
		})
	}
}

// R-6T6J-U7YU
func TestSigningAcrossSecondBoundaryStillAvoidsReplay(t *testing.T) {
	s := newScenario(t, state(1, ""), reply{http.StatusNoContent, ""})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.sign = func() error {
		if len(s.headers) == 0 {
			s.now = s.now.Add(900 * time.Millisecond)
		}
		return nil
	}
	s.after = func(req *http.Request, _ *http.Response, _ int) {
		if req.URL.Path == protocol.ReportPath {
			cancel()
		}
	}
	if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	wantPauses(t, s, 850*time.Millisecond)
	if len(s.headers) != 2 || s.headers[0] == s.headers[1] {
		t.Errorf("headers = %v, want distinct signed dates", s.headers)
	}
}

// R-L3SB-HPTT
func TestUpdateFailuresCapTheirBackoff(t *testing.T) {
	var replies []reply
	const cycles = 6
	for i := 1; i <= cycles; i++ {
		replies = append(replies, state(int64(i), "another"), reply{http.StatusBadGateway, ""}, reply{http.StatusNoContent, ""})
	}
	s := newScenario(t, replies...)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sleep := s.Sleep
	s.Sleep = func(ctx context.Context, d time.Duration) {
		sleep(ctx, d)
		if len(s.sleeps) == 2*cycles {
			cancel()
		}
	}
	if err := s.run(ctx, t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	wantPauses(t, s, 750*time.Millisecond, 5*time.Second, time.Second, 10*time.Second, time.Second,
		20*time.Second, time.Second, 40*time.Second, time.Second, time.Minute, time.Second, time.Minute)
	wantDiagnostics(t, s, slices.Repeat([]string{daemon.DiagUpdate}, cycles)...)
	if got := s.memory.State().Applies; got != cycles || len(s.reports) != cycles {
		t.Errorf("applied %d states and posted %d reports, want %d of each", got, len(s.reports), cycles)
	}
}

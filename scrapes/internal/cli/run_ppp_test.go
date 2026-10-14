package cli_test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/ppp"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

func pppFile(t *testing.T, states []string) string {
	t.Helper()
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	columns := ppp.Columns()
	if err := w.Write(columns); err != nil {
		t.Fatal(err)
	}
	for n, state := range states {
		record := make([]string, len(columns))
		for i, name := range columns {
			switch name {
			case "LoanNumber":
				record[i] = fmt.Sprintf("loan-%d", n)
			case "BorrowerState", "ProjectState":
				record[i] = state
			}
		}
		if err := w.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// R-UV88-UEJU
func TestRunPPPFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"unset", nil},
		{"empty", map[string]string{"PPP_FILES": ""}},
		{"ordered", map[string]string{"PPP_FILES": "b.csv,a.csv,b.csv"}},
		{"spelling", map[string]string{"PPP_FILES": "A-1_b.CSV"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := pppFile(t, []string{"IA"})
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}}
			f := startRun(t, runOptions{Env: tc.env, Source: source})
			v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "ppp"}))
			if done := f.awaitRun(v["id"].(string)); done["status"] != store.StatusDone {
				t.Fatal(done)
			}
			cfg, err := settings.Read(func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			requests := source.Requests()
			if len(requests) != len(cfg.PPPFiles) {
				t.Fatalf("requests %d, files %v", len(requests), cfg.PPPFiles)
			}
			for i, r := range requests {
				if r.Method != http.MethodGet || r.URL.String() != ppp.DefaultBaseURL+cfg.PPPFiles[i] || r.Header.Get("User-Agent") != pages.ServiceName+" ("+pages.Description+")" {
					t.Fatalf("request %d: %v", i, r)
				}
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop failed")
			}
		})
	}
}

// R-UYVX-ZPRX
func TestRunPPPStates(t *testing.T) {
	for _, tc := range []struct {
		env   map[string]string
		count int
	}{
		{nil, 2}, {map[string]string{"PPP_STATES": "IA"}, 1}, {map[string]string{"FMCSA_STATES": "IA"}, 2}, {map[string]string{"PPP_STATES": "WI,MN,WI"}, 2},
	} {
		t.Run(fmt.Sprint(tc.env), func(t *testing.T) {
			body := pppFile(t, []string{"MN", "WI", "IA"})
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			}}
			f := startRun(t, runOptions{Env: tc.env, Source: source})
			v := resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "ppp"}))
			if done := f.awaitRun(v["id"].(string)); done["status"] != store.StatusDone || done["rows"] != float64(tc.count) {
				t.Fatal(done)
			}
		})
	}
}

// R-YZYT-P0Z3
func TestRunRejectsPPPSettings(t *testing.T) {
	for key, value := range map[string]string{"PPP_FILES": "a/b.csv", "PPP_STATES": "mn"} {
		out, errout := &outputBuffer{}, &outputBuffer{}
		var seen []string
		p := cli.Process{Stdout: out, Stderr: errout, Dir: t.TempDir(), Unsetenv: func(string) error { t.Fatal("unset"); return nil }, HTTP: &http.Client{Transport: forbiddenIO{t}}, LookupEnv: func(k string) (string, bool) { seen = append(seen, k); return value, k == key }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherited"); return nil, nil }}
		want := settings.WantStates
		if key == "PPP_FILES" {
			want = settings.WantFiles
		}
		if code := cli.Run(t.Context(), p); code != cli.ExitUsage || out.String() != "" || errout.writes != 1 || errout.String() != "scrapes: "+key+" is '"+value+"', not "+want+"\n" {
			t.Fatalf("refusal %d %q %q %v", code, out.String(), errout.String(), seen)
		}
		for _, key := range seen {
			if key == "LISTEN_PID" || key == "LISTEN_FDS" || key == "IKIGENBA_SERVICES" {
				t.Fatalf("lookup after refusal: %s", key)
			}
		}

		if entries, err := os.ReadDir(p.Dir); err != nil || len(entries) != 0 {
			t.Fatalf("state created: %v %v", entries, err)
		}
	}
}

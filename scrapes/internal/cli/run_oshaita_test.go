package cli_test

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/scrapes/internal/cli"
	"github.com/ikigenba/ikigenba/scrapes/internal/oshaita"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

func itaFile(t *testing.T, path string, states []string) []byte {
	t.Helper()
	var csvBody bytes.Buffer
	w := csv.NewWriter(&csvBody)
	columns := oshaita.Columns()
	if err := w.Write(columns); err != nil {
		t.Fatal(err)
	}
	for i, state := range states {
		record := make([]string, len(columns))
		for j, col := range columns {
			switch col {
			case "id":
				record[j] = fmt.Sprintf("%s-%d", path, i)
			case "state":
				record[j] = state
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
	if !strings.HasSuffix(path, ".zip") {
		return csvBody.Bytes()
	}
	var body bytes.Buffer
	z := zip.NewWriter(&body)
	entry, err := z.Create("summary.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(csvBody.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

// R-7OL3-H2DK
func TestRunOSHAITAFiles(t *testing.T) {
	for _, files := range []string{"", "files/b c.zip,largefiles/a.csv,files/b c.zip", "largefiles/a.csv"} {
		t.Run(files, func(t *testing.T) {
			env := map[string]string{}
			if files != "" {
				env["OSHA_ITA_FILES"] = files
			}
			cfg, err := settings.Read(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(itaFile(t, r.URL.Path, []string{"IA"})))}, nil
			}}
			f := startRun(t, runOptions{Env: env, Source: source})
			id := runID(t, resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "osha_ita"})))
			if done := f.awaitRun(id); done["status"] != store.StatusDone {
				t.Fatal(done)
			}
			var got, want []string
			for _, req := range source.Requests() {
				got = append(got, req.URL.String())
				if req.Method != http.MethodGet || req.Header.Get("User-Agent") != oshaita.UserAgent {
					t.Fatal(req)
				}
			}
			for _, path := range cfg.OSHAITAFiles {
				want = append(want, oshaita.DefaultBaseURL+strings.ReplaceAll(path, " ", "%20"))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal(got, want)
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop")
			}
		})
	}
}

// R-PJIU-68NQ
func TestRunOSHAITAStates(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		rows float64
	}{
		{"default", nil, 4},
		{"ia", map[string]string{"OSHA_ITA_STATES": "IA"}, 2},
		{"independent", map[string]string{"PPP_STATES": "IA", "FMCSA_STATES": "IA", "FORM5500_STATES": "IA"}, 4},
		{"ordered", map[string]string{"OSHA_ITA_STATES": "WI,MN,WI"}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"OSHA_ITA_FILES": "files/a.zip,largefiles/b.csv"}
			for k, v := range tc.env {
				env[k] = v
			}
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(itaFile(t, r.URL.Path, []string{"MN", "WI", "IA"})))}, nil
			}}
			f := startRun(t, runOptions{Env: env, Source: source})
			id := runID(t, resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "osha_ita"})))
			if done := f.awaitRun(id); done["status"] != store.StatusDone || done["rows"] != tc.rows {
				t.Fatal(done)
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop")
			}
		})
	}
}

// R-7R0W-8LUY
func TestRunOSHAITASpool(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	var observations [][]string
	source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		body := &observedFilingBody{reader: bytes.NewReader(itaFile(t, r.URL.Path, []string{"MN"})), observe: func() {
			entries, err := os.ReadDir(filepath.Join(dir, "runs"))
			if err != nil {
				t.Error(err)
				return
			}
			names := []string{}
			for _, entry := range entries {
				if entry.Type().IsRegular() {
					names = append(names, entry.Name())
				}
			}
			mu.Lock()
			observations = append(observations, names)
			mu.Unlock()
		}}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	}}
	f := startRun(t, runOptions{Dir: dir, Env: map[string]string{"OSHA_ITA_FILES": "files/a.zip,largefiles/b.csv"}, Source: source})
	id := runID(t, resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "osha_ita"})))
	if done := f.awaitRun(id); done["status"] != store.StatusDone {
		t.Fatal(done)
	}
	mu.Lock()
	snapshots := append([][]string{}, observations...)
	mu.Unlock()
	if len(snapshots) != 2 {
		t.Fatal(snapshots)
	}
	for _, names := range snapshots {
		found := false
		for _, name := range names {
			if strings.HasPrefix(name, id+".") && name != id+".db" {
				found = true
			}
		}
		if !found {
			t.Fatal(names)
		}
	}
	checkStateTree(t, dir, id)
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop")
	}
}

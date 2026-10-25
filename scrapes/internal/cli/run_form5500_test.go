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
	"github.com/ikigenba/ikigenba/scrapes/internal/form5500"
	"github.com/ikigenba/ikigenba/scrapes/internal/pages"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

func filingArchive(t *testing.T, short bool, states []string, locationState string) []byte {
	t.Helper()
	form, mail, loc := form5500.Full, "SPONS_DFE_MAIL_US_STATE", "SPONS_DFE_LOC_US_STATE"
	if short {
		form, mail, loc = form5500.Short, "SF_SPONS_US_STATE", "SF_SPONS_LOC_US_STATE"
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	entry, err := z.Create("filings.csv")
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(entry)
	columns := form5500.Columns(form)
	if err := w.Write(columns); err != nil {
		t.Fatal(err)
	}
	for i, state := range states {
		record := make([]string, len(columns))
		for j, col := range columns {
			switch col {
			case "ACK_ID":
				record[j] = fmt.Sprintf("%s-%d", form, i)
			case mail:
				record[j] = state
			case loc:
				record[j] = locationState
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
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// R-KWEM-YE3C
func TestRunForm5500Years(t *testing.T) {
	for _, years := range []string{"", "2024", "2019,2024", "2019,2019"} {
		t.Run(years, func(t *testing.T) {
			env := map[string]string{}
			if years != "" {
				env["FORM5500_YEARS"] = years
			}
			cfg, err := settings.Read(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
			if err != nil {
				t.Fatal(err)
			}
			archives := map[bool][]byte{false: filingArchive(t, false, []string{"IA"}, "IA"), true: filingArchive(t, true, []string{"IA"}, "IA")}
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archives[strings.Contains(r.URL.Path, "_SF_")]))}, nil
			}}
			f := startRun(t, runOptions{Env: env, Source: source})
			id := runID(t, resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "form5500"})))
			if done := f.awaitRun(id); done["status"] != store.StatusDone {
				t.Fatal(done)
			}
			requests := source.Requests()
			var want []string
			for _, year := range cfg.Form5500Years {
				want = append(want, form5500.DefaultBaseURL+year+"/Latest/F_5500_"+year+"_Latest.zip", form5500.DefaultBaseURL+year+"/Latest/F_5500_SF_"+year+"_Latest.zip")
			}
			var got []string
			for _, r := range requests {
				got = append(got, r.URL.String())
				if r.Method != http.MethodGet || r.Header.Get("User-Agent") != pages.ServiceName+" ("+pages.Description+")" {
					t.Fatal(r)
				}
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

// R-KXMJ-C5U1
func TestRunForm5500States(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		rows float64
	}{
		{"default", nil, 4}, {"ia", map[string]string{"FORM5500_STATES": "IA"}, 2}, {"independent", map[string]string{"PPP_STATES": "IA", "FMCSA_STATES": "IA"}, 4}, {"ordered", map[string]string{"FORM5500_STATES": "WI,MN,WI"}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"FORM5500_YEARS": "2024"}
			for k, v := range tc.env {
				env[k] = v
			}
			archives := map[bool][]byte{false: filingArchive(t, false, []string{"MN", "WI", "IA"}, ""), true: filingArchive(t, true, []string{"MN", "WI", "IA"}, "")}
			source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(archives[strings.Contains(r.URL.Path, "_SF_")]))}, nil
			}}
			f := startRun(t, runOptions{Env: env, Source: source})
			id := runID(t, resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "form5500"})))
			if done := f.awaitRun(id); done["status"] != store.StatusDone || done["rows"] != tc.rows {
				t.Fatal(done)
			}
			if f.stop() != cli.ExitSuccess {
				t.Fatal("stop")
			}
		})
	}
}

type observedFilingBody struct {
	reader  *bytes.Reader
	once    sync.Once
	observe func()
}

func (b *observedFilingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if err == io.EOF {
		b.once.Do(b.observe)
	}
	return n, err
}
func (*observedFilingBody) Close() error { return nil }

// R-KYUF-PXKQ
func TestRunForm5500Spool(t *testing.T) {
	dir := t.TempDir()
	archives := map[bool][]byte{false: filingArchive(t, false, []string{"MN"}, ""), true: filingArchive(t, true, []string{"MN"}, "")}
	var mu sync.Mutex
	var observations [][]string
	source := &fakeSource{page: func(r *http.Request) (*http.Response, error) {
		body := &observedFilingBody{reader: bytes.NewReader(archives[strings.Contains(r.URL.Path, "_SF_")]), observe: func() {
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
	f := startRun(t, runOptions{Dir: dir, Env: map[string]string{"FORM5500_YEARS": "2024"}, Source: source})
	id := runID(t, resultJSON(t, f.call(fixtureCaller, "scrape", map[string]any{"scraper": "form5500"})))
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
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 || entries[0].Name() != "runs" || entries[1].Name() != "state" {
		t.Fatal(entries, err)
	}
	entries, err = os.ReadDir(filepath.Join(dir, "runs"))
	if err != nil || len(entries) != 1 || entries[0].Name() != id+".db" || !entries[0].Type().IsRegular() {
		t.Fatal(entries, err)
	}
	if f.stop() != cli.ExitSuccess {
		t.Fatal("stop")
	}
}

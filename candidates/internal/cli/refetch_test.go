package cli_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/cli"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// The work list must come from the upgrade of pages written by the previous
// release, through the database's permitted rollback-fixture seam.
func seedRunRefetch(t *testing.T, h *runHarness, count int) (string, []string) {
	t.Helper()
	old := fstest.MapFS{}
	files, err := fs.ReadDir(candidates.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() == "0027_page_reasons.sql" {
			continue
		}
		body, e := fs.ReadFile(candidates.Migrations(), file.Name())
		if e != nil {
			t.Fatal(e)
		}
		old[file.Name()] = &fstest.MapFile{Data: body}
	}
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(h.p.Dir, "state", "candidates.db"), Migrations: old, Now: h.p.Now, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	const candidate = "cand_1111111111111111"
	urls := make([]string, count)
	err = d.Write(t.Context(), func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(t.Context(), `INSERT INTO candidates(id,status,merged_into,name,names,names_lower,street,city,state,zip,county,phone,email,people,class,cargo,created,updated,name_source,name_record,address_source,address_record,phone_source,phone_record,email_source,email_record,other_names,other_addresses,other_phones,other_emails) VALUES (?,'active','','old','["old"]','old','','','','','','','','[]','','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','','','','','','','','','[]','[]','[]','[]')`, candidate); e != nil {
			return e
		}
		for i := range urls {
			urls[i] = fmt.Sprintf("https://repair.example/page%d", i)
			if _, e := tx.ExecContext(t.Context(), `INSERT INTO pages(candidate,url,title,text,size,fetched,user_id,user_email) VALUES(?,?,?,?,?,?,?,?)`, candidate, urls[i], "old title", "", website.BodyCap, h.p.Now().UTC().Format(time.RFC3339), "old-user", "old@example.test"); e != nil {
				return e
			}
		}
		return nil
	})
	closeErr := d.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("legacy seed: %v; close: %v", err, closeErr)
	}
	return candidate, urls
}

func waitRunRefetch(t *testing.T, h *runHarness) {
	t.Helper()
	select {
	case <-h.err.refetched:
	case <-time.After(5 * time.Second):
		t.Fatalf("repair did not finish: %s", h.err.String())
	}
}

// R-X4R0-QRFK
func TestRunRefetchKeepsReadableNewestCopyWithoutRequests(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	delete(h.env, services.Variable)
	candidate, urls := seedRunRefetch(t, h, 1)
	h.err.refetched = make(chan struct{})
	text := strings.TrimSpace(strings.Repeat("readable words ", website.MinChars))
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != urls[0] {
			t.Errorf("repair request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader("<p>" + text + "</p>")), Request: r}, nil
	})}
	websiteTimers(t, h, nil)
	h.start()
	waitRunRefetch(t, h)
	_, st := runStore(t, h.p.Dir, h.p.Now)
	count, err := st.CountPages(t.Context(), candidate)
	if err != nil || count.Copies != 2 {
		t.Fatalf("repair page count: %+v, %v", count, err)
	}
	pages, err := st.Pages(t.Context(), candidate)
	if err != nil || len(pages) != 1 {
		t.Fatalf("newest pages: %+v, %v", pages, err)
	}
	if pages[0].URL != urls[0] || pages[0].Reason != "" {
		t.Fatalf("newest page metadata: %+v", pages[0])
	}
	page, err := st.GetPage(t.Context(), candidate, pages[0].Number)
	if err != nil || !strings.Contains(page.Text, text) {
		t.Fatalf("newest page text: %+v, %v", page, err)
	}
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	line := "candidates: pages refetched: 1 read, 0 scripted, 0 redirect, 0 empty, 0 failed\n"
	if strings.Count(h.err.String(), line) != 1 {
		t.Fatalf("repair diagnostic: %q", h.err.String())
	}
}

// R-X76T-IAWY R-GC07-H8O0
func TestRunRefetchDoesNotDelayReadyAndStopsWithRun(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	delete(h.env, services.Variable)
	candidate, urls := seedRunRefetch(t, h, 1)
	entered, cancelled := make(chan struct{}), make(chan struct{})
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.String() != urls[0] {
			t.Errorf("repair request: %s %s", r.Method, r.URL)
		}
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		return nil, r.Context().Err()
	})}
	websiteTimers(t, h, nil)
	h.start()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("repair did not fetch")
	}
	select {
	case <-cancelled:
		t.Fatal("fetch was not held while ready")
	default:
	}
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("Run returned before cancelling the fetch")
	}
	if text := h.err.String(); strings.Contains(text, "candidates: pages refetch") {
		t.Fatalf("cancelled repair diagnostic: %q", text)
	}
	_, st := runStore(t, h.p.Dir, h.p.Now)
	count, err := st.CountPages(context.Background(), candidate)
	if err != nil || count.Copies != 1 {
		t.Fatalf("cancelled page count: %+v, %v", count, err)
	}
	runAssertSilent(t, h)
}

// R-XWMU-PTF3 R-GC07-H8O0
func TestRunRefetchUsesProcessTimersOutsidePasses(t *testing.T) {
	h := newRunHarness(t, t.TempDir(), "")
	candidate, urls := seedRunRefetch(t, h, 2)
	h.err.refetched = make(chan struct{})
	var requests, intervals, timeouts atomic.Int64
	text := strings.TrimSpace(strings.Repeat("readable words ", website.MinChars))
	h.p.HTTP = &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		i := requests.Add(1) - 1
		if i >= int64(len(urls)) || r.Method != http.MethodGet || r.URL.String() != urls[i] {
			t.Errorf("repair request %d: %s %s", i, r.Method, r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader("<p>" + text + "</p>")), Request: r}, nil
	})}
	h.p.After = func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		switch d {
		case cli.SweepInterval:
		case website.Interval:
			intervals.Add(1)
			ch <- time.Time{}
		case website.Timeout:
			timeouts.Add(1)
		default:
			t.Errorf("unexpected timer %v", d)
		}
		return ch
	}
	h.start()
	waitRunRefetch(t, h)
	_, st := runStore(t, h.p.Dir, h.p.Now)
	ps, err := st.ListPasses(context.Background(), "", "", 1)
	if err != nil || len(ps) != 0 {
		t.Fatalf("repair recorded passes: %+v, %v", ps, err)
	}
	count, err := st.CountPages(context.Background(), candidate)
	if err != nil || count.Copies != 4 {
		t.Fatalf("repair count: %+v, %v", count, err)
	}
	if requests.Load() != 2 || intervals.Load() != 1 || timeouts.Load() != 2 {
		t.Fatalf("requests=%d intervals=%d timeouts=%d", requests.Load(), intervals.Load(), timeouts.Load())
	}
	if code := h.stop(); code != cli.ExitSuccess {
		t.Fatalf("exit %d", code)
	}
	line := "candidates: pages refetched: 2 read, 0 scripted, 0 redirect, 0 empty, 0 failed\n"
	if strings.Count(h.err.String(), "candidates: facts filled\n") > 1 || strings.Count(h.err.String(), line) != 1 || strings.ReplaceAll(strings.ReplaceAll(h.err.String(), line, ""), "candidates: facts filled\n", "") != "" || h.out.String() != "" {
		t.Fatalf("streams stdout=%q stderr=%q", h.out.String(), h.err.String())
	}
}

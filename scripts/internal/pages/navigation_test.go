package pages_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func catalogRecords(t *testing.T, f *fixture, count int) []store.Script {
	t.Helper()
	var random bytes.Buffer
	for i := 1; i <= count+1; i++ {
		if err := binary.Write(&random, binary.BigEndian, uint64(i)); err != nil {
			t.Fatal(err)
		}
	}
	creator := store.New(f.db, store.Config{Now: func() time.Time { return fixedTime }, Rand: &random})
	records := make([]store.Script, count)
	for i := count - 1; i >= 0; i-- {
		sc, err := creator.Create(t.Context(), store.Draft{Owner: "owner", Name: fmt.Sprintf("script-%03d", i), Repo: "rep_0102030405060708", Ref: "main"})
		if err != nil {
			t.Fatal(err)
		}
		records[i] = sc
	}
	if _, err := creator.Create(t.Context(), store.Draft{Owner: "other", Name: "a-private", Repo: "rep_0102030405060708", Ref: "main"}); err != nil {
		t.Fatal(err)
	}
	return records
}

func catalogData(records []store.Script, p, prev, next string) pages.LandingData {
	d := pages.LandingData{Banner: signedBanner(), Page: p, Prev: prev, Next: next}
	for _, sc := range records {
		d.Scripts = append(d.Scripts, pages.ScriptRow{Name: sc.Name, URL: "/" + sc.Name + "/", Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: sc.Ref})
	}
	return d
}

func runData(f *fixture, sc store.Script, records []store.Run, count int, p, prev, next string) pages.ScriptData {
	d := pages.ScriptData{Banner: withTrail(signedBanner(), page.Level{Name: sc.Name, URL: "/" + sc.Name + "/"}), Script: pages.ScriptCard{ID: sc.ID, Name: sc.Name, Repo: pages.Repo{ID: sc.Repo, Name: "Repository"}, Ref: sc.Ref, Created: sc.Created.UTC().Format(pages.CardLayout), CreatedAt: sc.Created.UTC().Format(time.RFC3339), RunsKept: count, KeepNewest: f.cfg.KeepCount, KeepDays: f.cfg.KeepDays}, Page: p, Prev: prev, Next: next}
	for _, u := range records {
		d.Runs = append(d.Runs, pages.RunRow{ID: u.ID, URL: "/" + sc.Name + "/runs/" + u.ID + "/", Status: u.Status, Commit: u.SHA[:7], Started: u.Started.UTC().Format(pages.MinuteLayout), StartedAt: u.Started.UTC().Format(time.RFC3339)})
	}
	return d
}

func runToken(tool string, u store.Run) string {
	return (paging.Token{Tool: tool}).Encode(u.Started.UTC().Format(time.RFC3339), u.ID)
}

func checkNavigationPage(t *testing.T, f *fixture, path, name string, data any) {
	t.Helper()
	get := f.request(t.Context(), "GET", path, "owner")
	requireEqual(t, get.Code, 200)
	requireEqual(t, get.Header(), http.Header{"Content-Type": {"text/html; charset=utf-8"}})
	requireEqual(t, get.Body.String(), rendered(t, name, data))
	head := f.request(t.Context(), "HEAD", path, "owner")
	requireEqual(t, head.Code, get.Code)
	requireEqual(t, head.Header(), get.Header())
	requireEqual(t, head.Body.Len(), 0)
}

func checkFirstRedirect(t *testing.T, f *fixture, path, first string) {
	t.Helper()
	get := f.request(t.Context(), "GET", path, "owner")
	requireEqual(t, get.Code, 303)
	requireEqual(t, get.Header().Values("Location"), []string{first})
	head := f.request(t.Context(), "HEAD", path, "owner")
	requireEqual(t, head.Code, get.Code)
	requireEqual(t, head.Header(), get.Header())
	requireEqual(t, head.Body.Len(), 0)
}

// R-CR91-FPFI R-CSGX-TH67 R-CW4M-YSEA R-D5VU-0YBU R-D8BM-SHT8 R-DARF-K1AM
func TestCatalogNavigationWindows(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			records := catalogRecords(t, f, count)
			var forwards []pages.LandingData
			p := ""
			for start := 0; ; start += paging.DefaultLimit {
				end := min(start+paging.DefaultLimit, count)
				prev, next := "", ""
				if start > 0 && start < count {
					prev = (paging.Token{Tool: "list-before"}).Encode(records[start].Name)
				}
				if end < count {
					next = (paging.Token{Tool: "list"}).Encode(records[end-1].Name)
				}
				want := catalogData(records[start:end], p, prev, next)
				checkNavigationPage(t, f, "/?page="+url.QueryEscape(p)+"&page=bogus&limit=1&after=ignored", "landing", want)
				forwards = append(forwards, want)
				if next == "" {
					break
				}
				p = next
			}
			for i := len(forwards) - 1; i > 0; i-- {
				current := forwards[i]
				p = current.Prev
				if i == 1 {
					checkFirstRedirect(t, f, "/?page="+url.QueryEscape(p)+"&from=landing", "/")
					break
				}
				earlier := forwards[i-1]
				earlier.Page = p
				checkNavigationPage(t, f, "/?page="+url.QueryEscape(p), "landing", earlier)
			}
			if count <= paging.DefaultLimit {
				checkFirstRedirect(t, f, "/?page=last&from=landing", "/")
			} else {
				want := catalogData(records[count-paging.DefaultLimit:], "last", (paging.Token{Tool: "list-before"}).Encode(records[count-paging.DefaultLimit].Name), "")
				checkNavigationPage(t, f, "/?page=last&page=bogus&limit=1", "landing", want)
			}
			if count > 0 {
				empty := (paging.Token{Tool: "list"}).Encode(records[count-1].Name)
				checkNavigationPage(t, f, "/?page="+url.QueryEscape(empty), "landing", catalogData(nil, empty, "", ""))
				before := (paging.Token{Tool: "list-before"}).Encode(records[0].Name)
				checkFirstRedirect(t, f, "/?page="+url.QueryEscape(before), "/")
			}
		})
	}
}

// R-CTOU-78WW R-CUWQ-L0NL R-CXCJ-CK4Z R-D73Q-EQ2J R-D9JJ-69JX R-DBZB-XT1B
func TestRunNavigationWindows(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t)
			sc := f.create(t, "owner", "alpha")
			other := f.create(t, "other", "private")
			f.add(t, other, count*2+10, store.StatusRunning, "", 0, 0)
			var records []store.Run
			for i := 1; i <= count; i++ {
				u, err := f.cfg.Store.AddRun(t.Context(), store.Run{ID: fmt.Sprintf("run_%016x", i*2), Script: sc.ID, SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: sc.Ref, Trigger: store.TriggerManual, User: sc.Owner, Status: store.StatusRunning, Started: fixedTime})
				if err != nil {
					t.Fatal(err)
				}
				records = append([]store.Run{u}, records...)
			}
			var forwards []pages.ScriptData
			p := ""
			for start := 0; ; start += paging.DefaultLimit {
				end := min(start+paging.DefaultLimit, count)
				prev, next := "", ""
				if start > 0 && start < count {
					prev = runToken("runs-before", records[start])
				}
				if end < count {
					next = runToken("runs", records[end-1])
				}
				want := runData(f, sc, records[start:end], count, p, prev, next)
				checkNavigationPage(t, f, "/alpha/?page="+url.QueryEscape(p)+"&page=bogus&limit=1&started=ignored", "script", want)
				forwards = append(forwards, want)
				if next == "" {
					break
				}
				p = next
			}
			for i := len(forwards) - 1; i > 0; i-- {
				p = forwards[i].Prev
				if i == 1 {
					checkFirstRedirect(t, f, "/alpha/?page="+url.QueryEscape(p)+"&from=landing", "/alpha/")
					break
				}
				earlier := forwards[i-1]
				earlier.Page = p
				checkNavigationPage(t, f, "/alpha/?page="+url.QueryEscape(p), "script", earlier)
			}
			if count <= paging.DefaultLimit {
				checkFirstRedirect(t, f, "/alpha/?page=last&from=landing", "/alpha/")
			} else {
				want := runData(f, sc, records[count-paging.DefaultLimit:], count, "last", runToken("runs-before", records[count-paging.DefaultLimit]), "")
				checkNavigationPage(t, f, "/alpha/?page=last&page=bogus&limit=1", "script", want)
			}
			if count > 0 {
				empty := runToken("runs", records[count-1])
				checkNavigationPage(t, f, "/alpha/?page="+url.QueryEscape(empty), "script", runData(f, sc, nil, count, empty, "", ""))
				checkFirstRedirect(t, f, "/alpha/?page="+url.QueryEscape(runToken("runs-before", records[0])), "/alpha/")
			}
		})
	}
}

// R-CW4M-YSEA R-CR91-FPFI R-CSGX-TH67
func TestCatalogBeforeMissingAndDeletedBounds(t *testing.T) {
	f := setup(t)
	records := catalogRecords(t, f, paging.DefaultLimit+2)
	bound := records[len(records)-1]
	check := func(key string) {
		p := (paging.Token{Tool: "list-before"}).Encode(key)
		window := records[1 : len(records)-1]
		want := catalogData(window, p, (paging.Token{Tool: "list-before"}).Encode(window[0].Name), (paging.Token{Tool: "list"}).Encode(window[len(window)-1].Name))
		checkNavigationPage(t, f, "/?page="+url.QueryEscape(p), "landing", want)
	}
	check(records[len(records)-2].Name + "a")
	if err := f.cfg.Store.Delete(t.Context(), bound.ID); err != nil {
		t.Fatal(err)
	}
	check(bound.Name)
}

// R-CXCJ-CK4Z R-CTOU-78WW R-CUWQ-L0NL
func TestRunsBeforeMissingAndDeletedBounds(t *testing.T) {
	f := setup(t)
	sc := f.create(t, "owner", "alpha")
	count := paging.DefaultLimit + 2
	var records []store.Run
	for i := 1; i <= count; i++ {
		u, err := f.cfg.Store.AddRun(t.Context(), store.Run{ID: fmt.Sprintf("run_%016x", i*2), Script: sc.ID, SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: sc.Ref, Trigger: store.TriggerManual, User: sc.Owner, Status: store.StatusRunning, Started: fixedTime})
		if err != nil {
			t.Fatal(err)
		}
		records = append([]store.Run{u}, records...)
	}
	bound := records[count-1]
	check := func(id string, n int) {
		p := (paging.Token{Tool: "runs-before"}).Encode(bound.Started.UTC().Format(time.RFC3339), id)
		window := records[1 : count-1]
		want := runData(f, sc, window, n, p, runToken("runs-before", window[0]), runToken("runs", window[len(window)-1]))
		checkNavigationPage(t, f, "/alpha/?page="+url.QueryEscape(p), "script", want)
	}
	check("run_0000000000000003", count)
	if err := f.cfg.Store.DeleteRun(t.Context(), bound.ID); err != nil {
		t.Fatal(err)
	}
	check(bound.ID, count-1)
}

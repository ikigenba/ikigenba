package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/bookmarks"
	"github.com/ikigenba/ikigenba/home/internal/favicon"
	"github.com/ikigenba/ikigenba/home/internal/pages"
)

type entropy byte

func (e *entropy) Read(p []byte) (int, error) {
	for i := range p {
		*e++
		p[i] = byte(*e)
	}
	return len(p), nil
}
func fixedNow() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC) }
func database(t *testing.T) (*db.DB, *bookmarks.Store) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "home.db"), Migrations: home.Migrations(), Now: fixedNow, Service: pages.ServiceName, Stderr: new(bytes.Buffer)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, bookmarks.New(d, bookmarks.Config{Now: fixedNow, Rand: new(entropy)})
}
func newStore(t *testing.T) *bookmarks.Store { t.Helper(); _, s := database(t); return s }

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, typ, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{typ}}, Body: io.NopCloser(strings.NewReader(body))}
}
func newFetcher(f transportFunc) *favicon.Fetcher {
	if f == nil {
		f = func(*http.Request) (*http.Response, error) { return response(404, "text/plain", ""), nil }
	}
	return favicon.New(favicon.Config{HTTP: &http.Client{Transport: f}})
}
func testHandler(t *testing.T, cfg pages.Config) http.Handler {
	t.Helper()
	if cfg.Store == nil {
		cfg.Store = newStore(t)
	}
	if cfg.Favicon == nil {
		cfg.Favicon = newFetcher(nil)
	}
	return pages.Handler(cfg)
}
func held(t *testing.T, s *bookmarks.Store) []bookmarks.Bookmark {
	t.Helper()
	k, e := s.List(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func create(t *testing.T, s *bookmarks.Store, name string, icon bookmarks.Icon) bookmarks.Bookmark {
	t.Helper()
	k, e := s.Create(context.Background(), bookmarks.Draft{Name: name, URL: "https://supplied.test/" + url.PathEscape(name), CreatedBy: "fixture-owner", Icon: icon})
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func form(body string) *http.Request {
	r := request("POST", "/bookmarks?name=query-name&url=https://query.test")
	r.Body = io.NopCloser(strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}
func formValues(name, link string) *http.Request {
	return form(url.Values{"name": {name}, "url": {link}}.Encode())
}
func dataFor(b page.Banner, k []bookmarks.Bookmark, s bookmarks.Submission, e bookmarks.FieldErrors, n bool) pages.BookmarksData {
	b.Trail = []page.Level{{Name: "bookmarks", URL: "/bookmarks"}}
	d := pages.BookmarksData{b, nil, s, e, n}
	for _, m := range k {
		d.Bookmarks = append(d.Bookmarks, pages.BookmarkRow{m.ID, m.Name, m.URL, m.HasIcon, m.Created})
	}
	return d
}
func checkPage(t *testing.T, r *httptest.ResponseRecorder, status int, name string, data any) {
	t.Helper()
	if r.Code != status || !reflect.DeepEqual(r.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || r.Body.String() != execute(t, name, data) {
		t.Fatalf("page status %d want %d headers %v template equality %v", r.Code, status, r.Header(), r.Body.String() == execute(t, name, data))
	}
}
func checkUnavailable(t *testing.T, r *httptest.ResponseRecorder, head bool) {
	t.Helper()
	body := bookmarks.Unreachable + "\n"
	if head {
		body = ""
	}
	if r.Code != 503 || !reflect.DeepEqual(r.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || r.Body.String() != body {
		t.Fatalf("unavailable answer %d %v %q", r.Code, r.Header(), r.Body.String())
	}
}

// R-LE4W-DLUN R-LFCS-RDLC R-LK8E-AGK4 R-9R9H-FC8X R-LSRO-YUQZ R-4Z6P-XD0E R-51MI-OWHS
func TestBookmarksPageFreshAndHead(t *testing.T) {
	_, s := database(t)
	b := banner()
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return b }, Store: s, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	for step := range 4 {
		var k bookmarks.Bookmark
		if step == 1 {
			k = create(t, s, "Zulu<&", bookmarks.Icon{Type: "image/svg+xml", Data: []byte("supplied-icon")})
		}
		if step == 2 {
			create(t, s, "Alpha", bookmarks.Icon{})
		}
		if step == 3 {
			for _, m := range held(t, s) {
				if _, e := s.Delete(context.Background(), m.ID); e != nil {
					t.Fatal(e)
				}
			}
		}
		_ = k
		before := held(t, s)
		for _, query := range []string{"", "?icon=none", "?icon=other", "?icon=none&icon=other", "?other=none"} {
			get := answer(h, request("GET", "/bookmarks"+query))
			checkPage(t, get, 200, "bookmarks", dataFor(b, before, bookmarks.Submission{}, bookmarks.FieldErrors{}, strings.HasPrefix(query, "?icon=none")))
			head := answer(h, request("HEAD", "/bookmarks"+query))
			if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
				t.Fatal("HEAD differs")
			}
		}
		if !reflect.DeepEqual(before, held(t, s)) {
			t.Fatal("read mutated store")
		}
	}
}

// R-JMYZ-84F7 R-JLR2-UCOI R-JD7S-5YHN R-JEFO-JQ8C R-JQMO-DFNA R-LQBW-7B9L
func TestLandingBookmarksFreshAndUnavailable(t *testing.T) {
	d, s := database(t)
	b := banner()
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return b }, Store: s, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	for step := range 4 {
		if step == 1 {
			create(t, s, "Zulu", bookmarks.Icon{})
		}
		if step == 2 {
			create(t, s, "Alpha", bookmarks.Icon{Type: "image/png", Data: []byte("supplied")})
		}
		if step == 3 {
			for _, m := range held(t, s) {
				if _, e := s.Delete(context.Background(), m.ID); e != nil {
					t.Fatal(e)
				}
			}
		}
		root := b
		root.Trail = nil
		data := pages.LandingData{Banner: root}
		for _, m := range held(t, s) {
			data.Bookmarks = append(data.Bookmarks, pages.BookmarkTile{m.ID, m.Name, m.URL, m.HasIcon})
		}
		checkPage(t, answer(h, request("GET", "/")), 200, "landing", data)
	}
	d.SetFailing(true)
	for _, method := range []string{"GET", "HEAD"} {
		checkUnavailable(t, answer(h, request(method, "/")), method == "HEAD")
	}
	root := b
	root.Trail = nil
	checkPage(t, answer(h, request("GET", "/nope")), 404, "notfound", pages.NoticeData{Banner: root})
	b.Trail = []page.Level{{Name: "about", URL: "/about"}}
	checkPage(t, answer(h, request("GET", "/about")), 200, "about", pages.AboutData{Banner: b, Description: pages.Description})
}

// R-4FOS-KGGG R-LXNA-HXPR R-1EYQ-X5NH R-LWFE-45Z2 R-RRER-HUUS R-MB26-PEVE R-MDHZ-GYCS R-1JUC-G8M9
func TestAddValidationAndMediaType(t *testing.T) {
	_, s := database(t)
	create(t, s, "existing", bookmarks.Icon{})
	b := banner()
	var fetched atomic.Int32
	c := new(telemetry.Capture)
	w := writer(t, c, new(bytes.Buffer))
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return b }, Store: s, Telemetry: w, Favicon: newFetcher(func(*http.Request) (*http.Response, error) {
		fetched.Add(1)
		return response(404, "text/plain", ""), nil
	})})
	before := held(t, s)
	for _, body := range []string{"", "name=++&url=bad", "name=first&name=second&url=%zz&url=invalid-second", "bad=%zz&name=valid&url=no", "name=ok;bad=value&url=invalid"} {
		r := form(body)
		r.Header["Content-Type"] = []string{" APPLICATION/X-WWW-FORM-URLENCODED ; ignored", "text/plain"}
		v, _ := url.ParseQuery(body)
		sub := bookmarks.Submission{Name: v.Get("name"), URL: v.Get("url")}
		checkPage(t, answer(h, r), 422, "bookmarks", dataFor(b, before, sub, bookmarks.Validate(sub), false))
	}
	for _, typ := range []string{"", "text/plain", "application/json", "multipart/form-data", "application/x-www-form-urlencoded-x"} {
		r := formValues("valid", "https://site.test")
		r.Header["Content-Type"] = []string{typ, "application/x-www-form-urlencoded"}
		count := new(countBody)
		r.Body = count
		out := answer(h, r)
		if out.Code != 415 || out.Body.Len() != 0 || count.reads != 0 {
			t.Fatal(out.Code, count.reads)
		}
	}
	if fetched.Load() != 0 || !reflect.DeepEqual(before, held(t, s)) {
		t.Fatal("invalid submission changed state or fetched")
	}
	flush(t, w)
	for _, e := range c.Events() {
		if e.Name != "request.started" && e.Name != "request.finished" {
			t.Fatal(e)
		}
	}
}

type countBody struct{ reads int }

func (c *countBody) Read([]byte) (int, error) { c.reads++; return 0, io.EOF }
func (*countBody) Close() error               { return nil }

// R-50EM-B4R3 R-4EGW-6OPR R-1CIY-5M63 R-55A7-U7PV R-M2IW-10OJ R-56I4-7ZGK R-M4YO-SK5X R-58XW-ZIXY
func TestAddStoresFetchedIconAndEvents(t *testing.T) {
	for _, icon := range []bool{false, true} {
		t.Run(fmt.Sprint(icon), func(t *testing.T) {
			_, s := database(t)
			create(t, s, "other", bookmarks.Icon{})
			before := held(t, s)
			var calls atomic.Int32
			f := newFetcher(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if icon {
					if r.URL.Path == "/favicon.ico" {
						return response(200, "image/png", "supplied-icon-bytes"), nil
					}
					return response(200, "text/html", ""), nil
				}
				return response(404, "text/plain", ""), nil
			})
			c := new(telemetry.Capture)
			w := writer(t, c, new(bytes.Buffer))
			b := banner()
			h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return b }, Store: s, Favicon: f, Telemetry: w})
			r := formValues("  Unique  ", "  https://site.test/path  ")
			r.Header["X-User-Id"] = []string{"first-user", "second-user"}
			r.Header.Set("X-User-Email", "different-email@test")
			out := answer(h, r)
			location := "/bookmarks?icon=none"
			if icon {
				location = "/bookmarks"
			}
			if out.Code != 303 || out.Body.Len() != 0 || !reflect.DeepEqual(out.Header().Values("Location"), []string{location}) {
				t.Fatal(out.Code, out.Header())
			}
			after := held(t, s)
			if len(after) != len(before)+1 {
				t.Fatal(after)
			}
			var added bookmarks.Bookmark
			for _, m := range after {
				if m.Name == "Unique" {
					added = m
				} else if !reflect.DeepEqual(m, before[0]) {
					t.Fatal("existing bookmark changed")
				}
			}
			if added.ID == "" || added.URL != "https://site.test/path" || added.CreatedBy != "first-user" {
				t.Fatal(added)
			}
			if calls.Load() == 0 {
				t.Fatal("handler did not fetch")
			}
			data, typ := f.Fetch(context.Background(), "https://site.test/path")
			if added.HasIcon != (len(data) > 0) {
				t.Fatal(added)
			}
			if added.HasIcon {
				i, e := s.Icon(context.Background(), added.ID)
				if e != nil || i.Type != typ || !bytes.Equal(i.Data, data) {
					t.Fatal(i, e)
				}
			}
			if calls.Load() == 0 {
				t.Fatal("no fetch")
			}
			flush(t, w)
			events := c.Events()
			if len(events) != 3 || events[0].Name != "request.started" || events[1].Name != "bookmark.created" || events[2].Name != "request.finished" {
				t.Fatal(events)
			}
			e := events[1]
			if e.RequestID != "request-value" || e.User != "first-user" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"bookmark": added.ID, "name": added.Name, "icon": added.HasIcon, "icon_bytes": int64(len(data))}) {
				t.Fatal(e)
			}
			calls.Store(0)
			sub := bookmarks.Submission{Name: " unique ", URL: "https://other.test"}
			out = answer(h, formValues(sub.Name, sub.URL))
			checkPage(t, out, 422, "bookmarks", dataFor(b, after, sub, bookmarks.FieldErrors{Name: bookmarks.NameTakenMessage}, false))
			flush(t, w)
			duplicateEvents := c.Events()[3:]
			if len(duplicateEvents) != 2 || duplicateEvents[0].Name != "request.started" || duplicateEvents[1].Name != "request.finished" {
				t.Fatal(duplicateEvents)
			}
			if calls.Load() == 0 || !reflect.DeepEqual(after, held(t, s)) {
				t.Fatal("duplicate skipped fetch or changed store")
			}
		})
	}
}

// R-542B-GFZ6
func TestConcurrentSameNameAdds(t *testing.T) {
	_, s := database(t)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f := newFetcher(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/path" {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return response(404, "text/plain", ""), nil
	})
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return banner() }, Store: s, Favicon: f, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	done := make(chan *httptest.ResponseRecorder, 2)
	for _, name := range []string{" Race ", "rACE"} {
		go func() { done <- answer(h, formValues(name, "https://site.test/path")) }()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("adds serialized before fetch")
		}
	}
	once.Do(func() { close(release) })
	statuses := map[int]int{}
	for range 2 {
		select {
		case out := <-done:
			statuses[out.Code]++
		case <-time.After(3 * time.Second):
			t.Fatal("add did not complete")
		}
	}
	if statuses[303] != 1 || statuses[422] != 1 || len(held(t, s)) != 1 {
		t.Fatal(statuses, held(t, s))
	}
}

// R-LGKP-55C1 R-LHSL-IX2Q R-LJ0H-WOTF R-RSMN-VMLH R-MH5O-M9KV R-SLLP-I4YQ R-5CLM-4U61 R-5DTI-ILWQ R-SKDT-4D81 R-1JUC-G8M9 R-LNW3-FRS7 R-5G9B-A5E4 R-51MI-OWHS
func TestIconAndDelete(t *testing.T) {
	_, s := database(t)
	icon := bookmarks.Icon{Type: "image/svg+xml; supplied=parameter", Data: []byte("supplied-icon-contents")}
	k := create(t, s, "with icon", icon)
	no := create(t, s, "no icon", bookmarks.Icon{})
	b := banner()
	root := b
	root.Trail = nil
	c := new(telemetry.Capture)
	w := writer(t, c, new(bytes.Buffer))
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return b }, Store: s, Telemetry: w})
	before := held(t, s)
	if pages.IconCacheControl != "private, max-age=3600" || pages.IconContentSecurityPolicy != "default-src 'none'; style-src 'unsafe-inline'; sandbox" || pages.IconContentTypeOptions != "nosniff" {
		t.Fatal("icon policies")
	}
	for _, id := range []string{k.ID, no.ID, "unknown<&"} {
		path := "/bookmarks/" + url.PathEscape(id) + "/icon?ignored=true"
		get := request("GET", path)
		count := new(countBody)
		get.Body = count
		out := answer(h, get)
		if id == k.ID {
			if out.Code != 200 || out.Body.String() != string(icon.Data) {
				t.Fatal(out.Code, out.Body.String())
			}
			for name, value := range map[string]string{"Content-Type": icon.Type, "Cache-Control": pages.IconCacheControl, "Content-Security-Policy": pages.IconContentSecurityPolicy, "X-Content-Type-Options": pages.IconContentTypeOptions} {
				if !reflect.DeepEqual(out.Header().Values(name), []string{value}) {
					t.Fatal(out.Header())
				}
			}
		} else {
			checkPage(t, out, 404, "notfound", pages.NoticeData{Banner: root})
			if len(out.Header().Values("Cache-Control")) != 0 {
				t.Fatal(out.Header())
			}
		}
		head := request("HEAD", path)
		head.Body = count
		hout := answer(h, head)
		if hout.Code != out.Code || !reflect.DeepEqual(hout.Header(), out.Header()) || hout.Body.Len() != 0 || count.reads != 0 {
			t.Fatal("HEAD or body read")
		}
	}
	if !reflect.DeepEqual(before, held(t, s)) {
		t.Fatal("icon changed store")
	}
	flush(t, w)
	start := len(c.Events())
	for _, id := range []string{k.ID, k.ID, "missing"} {
		r := request("POST", "/bookmarks/"+id+"/delete?ignored=yes")
		count := new(countBody)
		r.Body = count
		out := answer(h, r)
		if out.Code != 303 || !reflect.DeepEqual(out.Header().Values("Location"), []string{"/bookmarks"}) || out.Body.Len() != 0 || count.reads != 0 {
			t.Fatal(out.Code, out.Header(), count.reads)
		}
	}
	after := held(t, s)
	if len(after) != 1 || !reflect.DeepEqual(after[0], no) {
		t.Fatal(after)
	}
	flush(t, w)
	events := c.Events()[start:]
	if len(events) != 7 {
		t.Fatal(events)
	}
	if events[0].Name != "request.started" || events[1].Name != "bookmark.deleted" || events[2].Name != "request.finished" {
		t.Fatal(events)
	}
	e := events[1]
	if e.RequestID != "request-value" || e.User != "caller" || !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"bookmark": k.ID}) {
		t.Fatal(e)
	}
	for i := 3; i < len(events); i += 2 {
		if events[i].Name != "request.started" || events[i+1].Name != "request.finished" {
			t.Fatal(events)
		}
	}
}

// R-RMJ5-YRW0 R-ROYY-QBDE R-RQ6V-4343 R-LWFE-45Z2 R-5G9B-A5E4 R-RRER-HUUS R-MEPV-UQ3H R-M8MD-XVE0 R-LQBW-7B9L R-MDHZ-GYCS R-1JUC-G8M9 R-51MI-OWHS
func TestBookmarkRefusalsAndUnreachable(t *testing.T) {
	d, s := database(t)
	create(t, s, "unchanged", bookmarks.Icon{})
	before := held(t, s)
	var calls atomic.Int32
	c := new(telemetry.Capture)
	w := writer(t, c, new(bytes.Buffer))
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return banner() }, Store: s, Telemetry: w, Favicon: newFetcher(func(*http.Request) (*http.Response, error) { calls.Add(1); return response(404, "text/plain", ""), nil })})
	for _, down := range []bool{false, true} {
		d.SetFailing(down)
		for _, tc := range []struct {
			path, allow string
			methods     []string
		}{{"/bookmarks", "GET, HEAD, POST", []string{"PUT", "PATCH", "DELETE", "OPTIONS"}}, {"/bookmarks/unknown/icon", "GET, HEAD", []string{"POST", "PUT", "DELETE", "OPTIONS"}}, {"/bookmarks/unknown/delete", "POST", []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS"}}} {
			for _, method := range tc.methods {
				for _, cancelled := range []bool{false, true} {
					r := request(method, tc.path)
					ctx, cancel := context.WithCancel(r.Context())
					if cancelled {
						cancel()
					}
					r = r.WithContext(ctx)
					count := new(countBody)
					r.Body = count
					out := answer(h, r)
					cancel()
					if out.Code != 405 || !reflect.DeepEqual(out.Header().Values("Allow"), []string{tc.allow}) || out.Body.Len() != 0 || (tc.path != "/bookmarks" && count.reads != 0) {
						t.Fatal(method, tc.path, out.Code, out.Header(), count.reads)
					}
				}
			}
		}
		fetchBefore := calls.Load()
		r := request("POST", "/bookmarks")
		r.Header.Set("Content-Type", "text/plain")
		count := new(countBody)
		r.Body = count
		out := answer(h, r)
		if out.Code != 415 || out.Body.Len() != 0 || count.reads != 0 || calls.Load() != fetchBefore {
			t.Fatal(out.Code, count.reads)
		}
		if down {
			for _, tc := range []struct{ method, path string }{{"GET", "/bookmarks"}, {"HEAD", "/bookmarks"}, {"GET", "/bookmarks/id/icon"}, {"HEAD", "/bookmarks/id/icon"}, {"POST", "/bookmarks/id/delete"}} {
				r := request(tc.method, tc.path)
				count := new(countBody)
				r.Body = count
				checkUnavailable(t, answer(h, r), tc.method == "HEAD")
				if tc.path != "/bookmarks" && count.reads != 0 {
					t.Fatal("body read")
				}
			}
			fetchBefore := calls.Load()
			checkUnavailable(t, answer(h, formValues("", "invalid")), false)
			if calls.Load() != fetchBefore {
				t.Fatal("invalid submission fetched while down")
			}
			checkUnavailable(t, answer(h, formValues("valid", "https://site.test")), false)
		}
	}
	d.SetFailing(false)
	if !reflect.DeepEqual(before, held(t, s)) {
		t.Fatal("refusal changed store")
	}
	flush(t, w)
	for _, e := range c.Events() {
		if e.Name != "request.started" && e.Name != "request.finished" {
			t.Fatal(e)
		}
	}
}

// R-4AT7-1DHO
func TestStoreFailsDuringFetch(t *testing.T) {
	d, s := database(t)
	create(t, s, "preserved", bookmarks.Icon{})
	before := held(t, s)
	f := newFetcher(func(*http.Request) (*http.Response, error) {
		d.SetFailing(true)
		return response(404, "text/plain", ""), nil
	})
	c := new(telemetry.Capture)
	w := writer(t, c, new(bytes.Buffer))
	h := testHandler(t, pages.Config{Banner: func(page.User) page.Banner { return banner() }, Store: s, Favicon: f, Telemetry: w})
	checkUnavailable(t, answer(h, formValues("new", "https://site.test")), false)
	d.SetFailing(false)
	if !reflect.DeepEqual(before, held(t, s)) {
		t.Fatal("failed insert changed state")
	}
	flush(t, w)
	if len(c.Events()) != 2 {
		t.Fatal(c.Events())
	}
}

// R-9R9H-FC8X R-1EYQ-X5NH R-6F6M-CZ89 R-647I-X1K0
func TestBookmarksUseRequestBanner(t *testing.T) {
	_, s := database(t)
	create(t, s, "existing", bookmarks.Icon{})
	var users []page.User
	var banners []page.Banner
	h := testHandler(t, pages.Config{Store: s, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer)), Banner: func(u page.User) page.Banner {
		b := banner()
		b.Email = u.Email
		b.Release = fmt.Sprint(len(users))
		b.ProfileURL = u.ProfileURL
		b.LogoutURL = u.LogoutURL
		users = append(users, u)
		banners = append(banners, b)
		return b
	}})
	for i, r := range []*http.Request{request("GET", "/bookmarks?icon=none"), formValues("", "invalid"), formValues(" existing ", "https://site.test")} {
		r.Header.Set("X-User-Email", fmt.Sprintf("supplied-%d@test", i))
		r.Host = fmt.Sprintf("home.supplied-%d.test", i)
		users, banners = nil, nil
		out := answer(h, r)
		wantUser := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: fmt.Sprintf("https://auth.supplied-%d.test/", i), LogoutURL: fmt.Sprintf("https://auth.supplied-%d.test/logout", i)}
		sub := bookmarks.Submission{}
		fieldErrors := bookmarks.FieldErrors{}
		status := 200
		if i == 1 {
			sub = bookmarks.Submission{Name: "", URL: "invalid"}
			fieldErrors = bookmarks.Validate(sub)
			status = 422
		}
		if i == 2 {
			sub = bookmarks.Submission{Name: " existing ", URL: "https://site.test"}
			fieldErrors.Name = bookmarks.NameTakenMessage
			status = 422
		}
		matches := false
		for j, u := range users {
			if u == wantUser && out.Body.String() == execute(t, "bookmarks", dataFor(banners[j], held(t, s), sub, fieldErrors, i == 0)) {
				matches = true
			}
		}
		if !matches || out.Code != status || !reflect.DeepEqual(out.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
			t.Fatal("page did not use a banner returned for its request user", users)
		}
		if len(out.Header().Values("Set-Cookie")) != 0 {
			t.Fatal(out.Header())
		}
	}
}

// R-SK7F-XBVN
func TestMutationAnswersIndependentOfDelivery(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	sinks := []telemetry.Sink{new(telemetry.Capture), sinkFunc(func(context.Context, telemetry.Event) error { return telemetry.ErrRejected }), sinkFunc(func(ctx context.Context, _ telemetry.Event) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})}
	var baseline [2]*httptest.ResponseRecorder
	for _, sink := range sinks {
		_, s := database(t)
		w := writer(t, sink, new(bytes.Buffer))
		h := testHandler(t, pages.Config{Store: s, Telemetry: w, Banner: func(page.User) page.Banner { return banner() }})
		for step := range 2 {
			r := formValues("supplied", "https://site.test")
			if step == 1 {
				r = request("POST", "/bookmarks/"+held(t, s)[0].ID+"/delete")
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- answer(h, r) }()
			var out *httptest.ResponseRecorder
			select {
			case out = <-done:
			case <-time.After(time.Second):
				t.Fatal("mutation waited for delivery")
			}
			if baseline[step] == nil {
				baseline[step] = out
			} else if baseline[step].Code != out.Code || !reflect.DeepEqual(baseline[step].Header(), out.Header()) || baseline[step].Body.String() != out.Body.String() {
				t.Fatal("sink changed answer")
			}
		}
	}
}

// R-KVBE-BUXX R-516Q-ODTS
func TestFormRequestEventAccounting(t *testing.T) {
	c := new(telemetry.Capture)
	w := writer(t, c, new(bytes.Buffer))
	h := testHandler(t, pages.Config{Telemetry: w, Banner: func(page.User) page.Banner { return banner() }})
	body := url.Values{"name": {"supplied"}, "url": {"https://site.test"}}.Encode()
	r := form(body)
	r.Header.Set("X-Request-Id", "supplied-request")
	out := answer(h, r)
	flush(t, w)
	events := c.Events()
	if len(events) != 3 {
		t.Fatal(events)
	}
	if events[0].Name != "request.started" || !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": "POST", "path": "/bookmarks"}) {
		t.Fatal(events)
	}
	if events[2].Name != "request.finished" || !reflect.DeepEqual(events[2].Attrs, telemetry.Attrs{"status": int64(out.Code), "duration_us": int64(0), "request_bytes": int64(len(body)), "response_bytes": int64(out.Body.Len())}) {
		t.Fatal(events)
	}
	for _, e := range events {
		if e.RequestID != "supplied-request" || e.User != "caller" {
			t.Fatal(e)
		}
	}
	if len(out.Header().Values("Set-Cookie")) != 0 {
		t.Fatal(out.Header())
	}
}

package tools_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func pageChecker(requests *atomic.Int64) *website.Checker {
	return &website.Checker{Client: &http.Client{Transport: websiteTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		h := http.Header{"Content-Type": []string{"text/html"}}
		code, body := 200, "<html><head><title>fixture title</title></head><body>"+strings.Repeat("fixture content ", 30)+"</body></html>"
		switch r.URL.Path {
		case "/missing":
			code = 404
		case "/empty":
			body = ""
		case "/plain":
			h.Set("Content-Type", "text/plain")
			body = "plain fixture"
		case "/off":
			code = 302
			h.Set("Location", "https://other.test/landed")
		case "/redirect":
			code = 302
			h.Set("Location", "https://acme.test/landed")
		}
		return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}, After: immediateWebsite}
}
func setSite(t *testing.T, f *fixture, id, site string) {
	t.Helper()
	state := sources.WebsiteHTTPS
	if site == "" {
		state = sources.WebsiteNone
	}
	_, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "acme.test", Inputs: "fixture", Found: encodedJSON(website.Result{State: state, URL: site})}})
	if err != nil {
		t.Fatal(err)
	}
}
func wantPage(p store.Page) tools.Page {
	return tools.Page{p.Number, p.URL, p.Title, p.Size, p.Reason, stamp(p.Fetched), p.UserID, p.UserEmail, urls.PagePath(p.Candidate, p.Number)}
}

// R-OA9E-OG3G R-OCP7-FZKU R-XK9X-0W5T R-OK0L-QM10 R-XNXM-67DW
func TestPageSchemaAndJSON(t *testing.T) {
	f := newFixture(t)
	infos, err := f.client.ListTools(t.Context(), caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range infos {
		if i.Name == "add_page" && string(i.InputSchema) != `{"type":"object","properties":{"candidate":{"type":"string"},"url":{"type":"string"}},"required":["candidate","url"],"additionalProperties":false}` {
			t.Fatal(string(i.InputSchema))
		}
	}
	capText := strconv.Itoa(store.PageCap)
	capSentence := tools.RefusePageCap
	if !strings.Contains(capSentence, capText) || strings.TrimSpace(tools.RefuseFetch) != tools.RefuseFetch {
		t.Fatal("invalid refusal copy")
	}
	p := tools.Page{1, "u", "", 2, "", "f", "b", "", "p"}
	b := encodedJSON(p)
	if !reflect.DeepEqual(objectKeys(t, b), []string{"number", "url", "size", "fetched", "by", "path"}) {
		t.Fatal(string(b))
	}
	p.Title, p.Email, p.Reason = "chosen-title", "chosen-email", website.ReasonScripted
	if !reflect.DeepEqual(objectKeys(t, encodedJSON(p)), []string{"number", "url", "title", "size", "reason", "fetched", "by", "email", "path"}) {
		t.Fatal(p)
	}
	for _, a := range []map[string]any{{"candidate": "c"}, {"url": "u"}, {"candidate": "c", "url": "u", "extra": true}} {
		if !f.call("add_page", a).IsError() {
			t.Fatal(a)
		}
	}
}

// R-P2B3-H65F R-M3JT-JKMP R-GADH-O9XX R-7T91-FLVY
func TestAddPageRefusals(t *testing.T) {
	var requests atomic.Int64
	k := pageChecker(&requests)
	f := fixtureWithWebsite(t, nil, k)
	id := f.seed(1)[0]
	a := tools.AddPageArgs{Candidate: id, URL: "https://acme.test/about"}
	f.d.SetFailing(true)
	bad := a
	bad.Candidate = "bogus"
	refusal(t, f.call("add_page", bad), passes.RefusalNotFound, tools.RefuseNotFound)
	for _, u := range []string{"ftp://acme.test/", "/about", "acme.test/about", "https://"} {
		bad = a
		bad.URL = u
		refusal(t, f.call("add_page", bad), passes.RefusalInvalid, tools.RefuseInvalid)
	}
	refusal(t, f.call("add_page", a), passes.RefusalUnavailable, tools.RefuseUnavailable)
	f.d.SetFailing(false)
	absent := newFixture(t)
	refusal(t, absent.call("add_page", a), passes.RefusalUnavailable, tools.RefuseUnavailable)
	bad = a
	bad.Candidate = "cand_0000000000000000"
	refusal(t, f.call("add_page", bad), passes.RefusalNotFound, tools.RefuseNotFound)
	refusal(t, f.call("add_page", a), passes.RefusalInvalid, tools.RefuseNoWebsite)
	setSite(t, f, id, "")
	refusal(t, f.call("add_page", a), passes.RefusalInvalid, tools.RefuseNoWebsite)
	setSite(t, f, id, "https://www.acme.test/home")
	for _, u := range []string{"https://other.test/", "https://acme.test.other.test/"} {
		bad = a
		bad.URL = u
		refusal(t, f.call("add_page", bad), passes.RefusalInvalid, tools.RefuseOffSite)
	}
	if requests.Load() != 0 {
		t.Fatal(requests.Load())
	}
	for _, path := range []string{"missing", "empty", "plain"} {
		bad = a
		bad.URL = "https://acme.test/" + path
		pg := k.Get(t.Context(), bad.URL)
		if pg.Error == "" {
			t.Fatal(pg)
		}
		refusal(t, f.call("add_page", bad), passes.RefusalUnavailable, tools.RefuseFetch+" "+pg.Error)
	}
	bad = a
	bad.URL = "https://acme.test/off"
	refusal(t, f.call("add_page", bad), passes.RefusalInvalid, tools.RefuseOffSite)
	ps, err := f.st.Pages(t.Context(), id)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
	n, err := f.st.CountPages(t.Context(), id)
	if err != nil || n.URLs != 0 || n.Copies != 0 {
		t.Fatal(n, err)
	}
}

// R-XRLB-BILZ R-XNXM-67DW R-14E6-KS50
func TestAddPageCopiesAndCandidateHistory(t *testing.T) {
	var requests atomic.Int64
	k := pageChecker(&requests)
	f := fixtureWithWebsite(t, nil, k)
	ids := f.seed(2)
	id := ids[0]
	setSite(t, f, id, "https://www.acme.test/home")
	empty := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if empty.Pages == nil || len(empty.Pages) != 0 {
		t.Fatal(empty)
	}
	var numbers []int64
	for i, u := range []string{"https://acme.test/about", "https://acme.test/about", "https://shop.acme.test/", "https://acme.test/redirect"} {
		r := f.callAs(other, "add_page", tools.AddPageArgs{Candidate: id, URL: u})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		p := decodeList[tools.Page](t, r)
		stored, err := f.st.GetPage(t.Context(), id, p.Number)
		if err != nil {
			t.Fatal(err)
		}
		pg := k.Get(t.Context(), u)
		if stored.Text != pg.Text || p.URL != pg.URL || p.Title != pg.Title || p.Size != pg.Size || p.By != other.UserID || p.Email != other.Email {
			t.Fatal(p, stored, pg)
		}
		success(t, r, wantPage(stored))
		numbers = append(numbers, p.Number)
		if i == 2 {
			result := f.call("candidate", tools.CandidateArgs{Candidate: id})
			detail := decodeList[tools.Detail](t, result)
			if len(detail.Pages) != 2 || detail.Pages[0].URL != u || detail.Pages[1].URL != "https://acme.test/about" || detail.Pages[1].Number != numbers[1] {
				t.Fatal(detail.Pages)
			}
			var wire struct {
				Pages []map[string]json.RawMessage `json:"pages"`
			}
			if err := json.Unmarshal(decode(t, result).Structured, &wire); err != nil {
				t.Fatal(err)
			}
			for _, page := range wire.Pages {
				if _, exists := page["text"]; exists {
					t.Fatal(page)
				}
			}
		}
		if strings.Contains(string(decode(t, r).Structured), `"text":`) {
			t.Fatal(decode(t, r))
		}
	}
	if numbers[1] <= numbers[0] {
		t.Fatal(numbers)
	}
	pages, err := f.st.Pages(t.Context(), id)
	if err != nil || len(pages) != 3 {
		t.Fatal(pages, err)
	}
	detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	for i, p := range pages {
		if detail.Pages[i] != wantPage(p) {
			t.Fatal(detail.Pages, p)
		}
	}
	if detail.Pages[0].URL != "https://acme.test/landed" || detail.Pages[2].Number != numbers[1] {
		t.Fatal(detail.Pages)
	}
	r := f.call("decide", tools.DecideArgs{Kind: store.KindMerge, Candidate: ids[1], Other: id, Why: "same fixture"})
	if r.IsError() {
		t.Fatal(decode(t, r))
	}
	detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
	if detail.Pages == nil || len(detail.Pages) != 0 {
		t.Fatal(detail)
	}
	refusal(t, f.call("add_page", tools.AddPageArgs{Candidate: id, URL: "https://acme.test/about"}), passes.RefusalInvalid, tools.RefuseNoWebsite)
	ps, err := f.st.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(ps) != 0 {
		t.Fatal(ps, err)
	}
}

// R-M3JT-JKMP
func TestAddPageCapCountsURLs(t *testing.T) {
	var requests atomic.Int64
	k := pageChecker(&requests)
	f := fixtureWithWebsite(t, nil, k)
	id := f.seed(1)[0]
	setSite(t, f, id, "https://acme.test/")
	for i := 0; i < store.PageCap-1; i++ {
		_, err := f.st.AddPage(t.Context(), store.Page{Candidate: id, URL: fmt.Sprintf("https://acme.test/%d", i), UserID: caller.UserID})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.st.AddPage(t.Context(), store.Page{Candidate: id, URL: "https://acme.test/0", UserID: caller.UserID})
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"https://acme.test/new", "https://acme.test/0"} {
		if r := f.call("add_page", tools.AddPageArgs{Candidate: id, URL: u}); r.IsError() {
			t.Fatal(decode(t, r))
		}
	}
	before := requests.Load()
	refusal(t, f.call("add_page", tools.AddPageArgs{Candidate: id, URL: "https://acme.test/overflow"}), passes.RefusalInvalid, tools.RefusePageCap)
	if requests.Load() != before {
		t.Fatal("cap fetched")
	}
	n, err := f.st.CountPages(t.Context(), id)
	if err != nil || n.URLs != store.PageCap || n.Copies != store.PageCap+2 {
		t.Fatal(n, err)
	}
}

// R-OGBM-5RDM R-PP39-YIBD
func TestSiteTextProjection(t *testing.T) {
	f := newFixture(t)
	id := f.seed(1)[0]
	co := f.create("site projection")
	before := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true}))
	if len(before.Members) != 1 || before.Members[0].SiteText != nil {
		t.Fatal(before)
	}
	for _, exit := range []*int{nil, new(int)} {
		f.now = f.now.Add(time.Second)
		found := encodedJSON(enrichers.SiteTextFound{Run: "prompt-run", Status: "exited", ExitCode: exit, Pages: 2, CostNanos: 100})
		_, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: enrichers.KindSiteText, Source: enrichers.SourcePrompts, Inputs: "site-input", Found: found, Cost: 0.0000001}})
		if err != nil {
			t.Fatal(err)
		}
		after := decodeList[tools.ProspectListRead](t, f.call("prospect_list", tools.ReadProspectListArgs{ProspectList: co.ID, Peek: true}))
		if after.Matching != before.Matching || after.Members[0].Candidate.ID != id || after.Members[0].SiteText == nil || *after.Members[0].SiteText != stamp(f.now) {
			t.Fatal(after)
		}
		detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
		e := detail.EnrichmentResults[0]
		if e.Kind != enrichers.KindSiteText || e.Source != enrichers.SourcePrompts || e.Inputs != "site-input" || e.Cost != 0.0000001 || string(e.Found) != string(found) || e.At != stamp(f.now) {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(objectKeys(t, encodedJSON(e)), []string{"kind", "source", "inputs", "found", "cost", "at"}) {
			t.Fatal(e)
		}
	}
}

// R-XLHT-ENWI R-57N2-LEHE R-XRLB-BILZ R-XNXM-67DW R-14E6-KS50 R-7UGX-TDMN
func TestAddUnreadablePageKeepsCopyAndLaterReplacesIt(t *testing.T) {
	if tools.RefuseUnreadable == "" || strings.TrimSpace(tools.RefuseUnreadable) != tools.RefuseUnreadable || strings.ContainsAny(tools.RefuseUnreadable, "\r\n") {
		t.Fatal("unreadable copy")
	}
	for _, copy := range []string{tools.RefuseNotFound, tools.RefuseInvalid, tools.RefuseBusy, tools.RefuseStopping, tools.RefuseUnavailable, tools.RefuseNoWebsite, tools.RefuseOffSite, tools.RefusePageCap, tools.RefuseFetch} {
		if tools.RefuseUnreadable == copy {
			t.Fatal("duplicate refusal copy")
		}
	}
	for _, reason := range []string{website.ReasonScripted, website.ReasonRedirect, website.ReasonEmpty} {
		t.Run(reason, func(t *testing.T) {
			readable := false
			body := "<html><head><title>Unreadable title</title></head><body>short</body></html>"
			if reason == website.ReasonScripted {
				body = "<html><head><title>Unreadable title</title></head><body><script>application()</script></body></html>"
			}
			if reason == website.ReasonRedirect {
				body = `<html><head><title>Unreadable title</title><meta http-equiv="refresh" content="0;url=/next"></head><body>short</body></html>`
			}
			checker := &website.Checker{Client: &http.Client{Transport: websiteTransport(func(r *http.Request) (*http.Response, error) {
				content := body
				if readable {
					content = "<html><head><title>Readable title</title></head><body>" + strings.Repeat("Readable business content. ", 20) + "</body></html>"
				}
				header := http.Header{"Content-Type": []string{"text/html"}}
				code := http.StatusOK
				if r.URL.Path == "/off" {
					code = http.StatusFound
					header.Set("Location", "https://other.test/landed")
				}
				return &http.Response{StatusCode: code, Header: header, Body: io.NopCloser(strings.NewReader(content)), Request: r}, nil
			})}, After: immediateWebsite}
			f := fixtureWithWebsite(t, nil, checker)
			id := f.seed(1)[0]
			setSite(t, f, id, "https://acme.test/")
			url := "https://acme.test/unreadable"
			pg := checker.Get(t.Context(), url)
			if pg.Reason != reason || pg.Text != "" || pg.Error != "" {
				t.Fatal(pg)
			}
			refusal(t, f.callAs(other, "add_page", tools.AddPageArgs{Candidate: id, URL: url}), passes.RefusalUnreadable, tools.RefuseUnreadable+" "+reason)
			pages, err := f.st.Pages(t.Context(), id)
			if err != nil || len(pages) != 1 {
				t.Fatal(pages, err)
			}
			p := pages[0]
			if p.URL != url || p.Title != pg.Title || p.Size != pg.Size || p.Reason != reason || p.Text != "" || p.UserID != other.UserID || p.UserEmail != other.Email {
				t.Fatal(p, pg)
			}
			stored, err := f.st.GetPage(t.Context(), id, p.Number)
			if err != nil || stored.Text != "" || stored.Reason != reason {
				t.Fatal(stored, err)
			}
			detail := decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
			if len(detail.Pages) != 1 || detail.Pages[0] != wantPage(p) {
				t.Fatal(detail.Pages)
			}
			refusal(t, f.call("add_page", tools.AddPageArgs{Candidate: id, URL: "https://acme.test/off"}), passes.RefusalInvalid, tools.RefuseOffSite)
			pages, err = f.st.Pages(t.Context(), id)
			if err != nil || len(pages) != 1 {
				t.Fatal(pages, err)
			}
			readable = true
			result := f.call("add_page", tools.AddPageArgs{Candidate: id, URL: url})
			newer := decodeList[tools.Page](t, result)
			if result.IsError() || newer.Reason != "" || newer.Number <= p.Number {
				t.Fatal(newer)
			}
			detail = decodeList[tools.Detail](t, f.call("candidate", tools.CandidateArgs{Candidate: id}))
			if len(detail.Pages) != 1 || detail.Pages[0].Reason != "" || detail.Pages[0].Number != newer.Number {
				t.Fatal(detail.Pages)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(encodedJSON(detail.Pages[0]), &wire); err != nil {
				t.Fatal(err)
			}
			if _, exists := wire["reason"]; exists {
				t.Fatal("readable reason present")
			}
			passesHeld, err := f.st.ListPasses(t.Context(), "", "", 100)
			if err != nil || len(passesHeld) != 0 {
				t.Fatal(passesHeld, err)
			}
		})
	}
}

// R-XV90-GTU2
func TestUnreadablePageCountsTowardCap(t *testing.T) {
	var requests atomic.Int64
	checker := &website.Checker{Client: &http.Client{Transport: websiteTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader("<html><body>short</body></html>")), Request: r}, nil
	})}, After: immediateWebsite}
	f := fixtureWithWebsite(t, nil, checker)
	id := f.seed(1)[0]
	setSite(t, f, id, "https://acme.test/")
	for i := range store.PageCap - 1 {
		_, err := f.st.AddPage(t.Context(), store.Page{Candidate: id, URL: fmt.Sprintf("https://acme.test/%d", i), UserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
	}
	args := tools.AddPageArgs{Candidate: id, URL: "https://acme.test/unreadable"}
	refusal(t, f.call("add_page", args), passes.RefusalUnreadable, tools.RefuseUnreadable+" "+website.ReasonEmpty)
	count := requests.Load()
	refusal(t, f.call("add_page", tools.AddPageArgs{Candidate: id, URL: "https://acme.test/overflow"}), passes.RefusalInvalid, tools.RefusePageCap)
	if requests.Load() != count {
		t.Fatal("overflow fetched")
	}
	refusal(t, f.call("add_page", args), passes.RefusalUnreadable, tools.RefuseUnreadable+" "+website.ReasonEmpty)
	if requests.Load() != count+1 {
		t.Fatal("held URL not fetched")
	}
}

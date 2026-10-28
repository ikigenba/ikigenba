package pages

import (
	"bytes"
	"context"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-QUV8-7895 R-QW34-KZZU R-QXB0-YRQJ R-QYIX-CJH8 R-0M3J-HEKX R-464Z-NBFU R-QTNB-TGIG
func TestNavigationWalksAndTail(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 1, 3*paging.DefaultLimit + 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			cfg, prospectLists, passes := navigationFixture(t, count)
			// Every key ties, including both sides of every window boundary.
			for i := 1; i < count; i++ {
				if prospectLists[i].Name != prospectLists[0].Name || !passes[i].Started.Equal(passes[0].Started) {
					t.Fatal("fixture must exercise tie breakers")
				}
			}
			for _, prospectListList := range []bool{true, false} {
				for _, other := range navigationOtherValues(prospectLists, passes, !prospectListList) {
					t.Run(strconv.FormatBool(prospectListList)+"/"+other, func(t *testing.T) {
						navigationWalk(t, cfg, prospectLists, passes, prospectListList, other)
					})
				}
			}
		})
	}
}

func navigationFixture(t *testing.T, count int) (Config, []store.ProspectList, []store.Pass) {
	t.Helper()
	cfg, _ := pageFixture(t)
	ctx := context.Background()
	for i := 0; i < count; i++ {
		_, err := cfg.Store.CreateProspectList(ctx, store.ProspectList{Name: "navigation-prospect_list", Size: store.ProspectListSizeDefault, CreatedUserID: "navigation-creator", CreatedUserEmail: "navigation@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "navigation-starter"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = cfg.Store.FinishPass(ctx, p.ID, store.Counts{Rows: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	prospectLists, err := cfg.Store.ListProspectLists(ctx, "", "", count+1)
	if err != nil || len(prospectLists) != count {
		t.Fatal("prospect_lists fixture", len(prospectLists), err)
	}
	passes, err := cfg.Store.ListPasses(ctx, "", "", count+1)
	if err != nil || len(passes) != count {
		t.Fatal("passes fixture", len(passes), err)
	}
	return cfg, prospectLists, passes
}

func navigationToken(prospectLists []store.ProspectList, passes []store.Pass, prospectListList, before bool, index int) string {
	suffix := ""
	if before {
		suffix = "-before"
	}
	if prospectListList {
		e := prospectLists[index]
		return (paging.Token{Tool: "prospect_lists" + suffix}).Encode(e.Name, e.ID)
	}
	p := passes[index]
	return (paging.Token{Tool: "passes" + suffix}).Encode(p.Started.UTC().Format(time.RFC3339), p.ID)
}

func navigationOtherValues(prospectLists []store.ProspectList, passes []store.Pass, prospectListList bool) []string {
	values := []string{"", navigationToken(prospectLists, passes, prospectListList, false, paging.DefaultLimit-1), "last"}
	if len(prospectLists) > paging.DefaultLimit+1 {
		values = append(values, navigationToken(prospectLists, passes, prospectListList, true, len(prospectLists)-1))
	}
	return values
}

func navigationBounds(prospectLists []store.ProspectList, passes []store.Pass, prospectListList bool, value string) (int, int) {
	count := len(prospectLists)
	if value == "" {
		return 0, min(count, paging.DefaultLimit)
	}
	if value == "last" {
		return count - paging.DefaultLimit, count
	}
	for i := 0; i < count; i++ {
		if value == navigationToken(prospectLists, passes, prospectListList, false, i) {
			return i + 1, min(count, i+1+paging.DefaultLimit)
		}
		if value == navigationToken(prospectLists, passes, prospectListList, true, i) {
			return max(0, i-paging.DefaultLimit), i
		}
	}
	panic("navigation fixture token not found")
}

func navigationExpected(cfg Config, prospectLists []store.ProspectList, passes []store.Pass, cp, pp string) LandingData {
	data := LandingData{Banner: bannerFor(cfg), ProspectLists: []ProspectListRow{}, Passes: []PassRow{}, ProspectListsPage: cp, PassesPage: pp}
	start, end := navigationBounds(prospectLists, passes, true, cp)
	for i := start; i < end; i++ {
		data.ProspectLists = append(data.ProspectLists, prospectListRow(prospectLists[i]))
	}
	if cp != "" && start < end {
		data.ProspectListsPrev = navigationToken(prospectLists, passes, true, true, start)
	}
	if end < len(prospectLists) {
		data.ProspectListsNext = navigationToken(prospectLists, passes, true, false, end-1)
	}
	start, end = navigationBounds(prospectLists, passes, false, pp)
	for i := start; i < end; i++ {
		data.Passes = append(data.Passes, passRow(passes[i]))
	}
	if pp != "" && start < end {
		data.PassesPrev = navigationToken(prospectLists, passes, false, true, start)
	}
	if end < len(passes) {
		data.PassesNext = navigationToken(prospectLists, passes, false, false, end-1)
	}
	return data
}

func navigationPath(prospectListList bool, value, other string) string {
	cp, pp := other, value
	if prospectListList {
		cp, pp = value, other
	}
	return "/?" + (url.Values{"prospect_lists_page": {cp}, "passes_page": {pp}}).Encode()
}

// Observe the handler's template data through a template function, then
// check its actual HTTP body against the unmodified embedded template.
func navigationLanding(t *testing.T, cfg Config, path string, want LandingData) LandingData {
	t.Helper()
	captureSet, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got LandingData
	calls := 0
	cloned, err := captureSet.templates.Funcs(template.FuncMap{"navigationCapture": func(d LandingData) string { got = d; calls++; return "" }}).Parse(`{{define "landing"}}{{navigationCapture .}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	captured := cfg
	captured.Pages = &Set{templates: cloned}
	response := requestPage(captured, http.MethodGet, path, "reader")
	if response.Code != http.StatusOK || calls != 1 {
		t.Fatal("capture", path, response.Code, calls)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("landing data %s\ngot: %+v\nwant: %+v", path, got, want)
	}
	var expected bytes.Buffer
	if err = cfg.Pages.templates.ExecuteTemplate(&expected, "landing", got); err != nil {
		t.Fatal(err)
	}
	response = requestPage(cfg, http.MethodGet, path, "reader")
	if response.Code != http.StatusOK || response.Body.String() != expected.String() {
		t.Fatal("rendered landing differs", path, response.Code)
	}
	return got
}

func navigationWalk(t *testing.T, cfg Config, prospectLists []store.ProspectList, passes []store.Pass, prospectListList bool, other string) {
	t.Helper()
	var windows []LandingData
	value := ""
	for step := 0; ; step++ {
		if step > len(prospectLists)/paging.DefaultLimit+1 {
			t.Fatal("forward walk did not terminate")
		}
		cp, pp := other, value
		if prospectListList {
			cp, pp = value, other
		}
		got := navigationLanding(t, cfg, navigationPath(prospectListList, value, other), navigationExpected(cfg, prospectLists, passes, cp, pp))
		windows = append(windows, got)
		next := got.PassesNext
		if prospectListList {
			next = got.ProspectListsNext
		}
		if next == "" {
			break
		}
		value = next
	}
	if len(windows) != (len(prospectLists)+paging.DefaultLimit-1)/paging.DefaultLimit {
		t.Fatal("forward window count", len(windows))
	}
	// Follow each actual Prev, requiring precisely the preceding forward
	// window, including full row data.
	for j := len(windows) - 1; j > 0; j-- {
		prev := windows[j].PassesPrev
		if prospectListList {
			prev = windows[j].ProspectListsPrev
		}
		if prev == "" {
			t.Fatal("missing backward token", j)
		}
		if j == 1 {
			response := requestPage(cfg, http.MethodGet, navigationPath(prospectListList, prev, other), "reader")
			location := "/"
			if other != "" {
				param := "prospect_lists_page"
				if prospectListList {
					param = "passes_page"
				}
				location = "/?" + param + "=" + other
			}
			if response.Code != http.StatusSeeOther || !reflect.DeepEqual(response.Header().Values("Location"), []string{location}) || response.Body.Len() != 0 {
				t.Fatal("backward first-window redirect", response)
			}
		} else {
			cp, pp := other, prev
			if prospectListList {
				cp, pp = prev, other
			}
			got := navigationLanding(t, cfg, navigationPath(prospectListList, prev, other), navigationExpected(cfg, prospectLists, passes, cp, pp))
			if prospectListList && !reflect.DeepEqual(got.ProspectLists, windows[j-1].ProspectLists) || !prospectListList && !reflect.DeepEqual(got.Passes, windows[j-1].Passes) {
				t.Fatal("backward window differs", j)
			}
			windows[j-1] = got
		}
	}
	cp, pp := other, "last"
	if prospectListList {
		cp, pp = "last", other
	}
	last := navigationLanding(t, cfg, navigationPath(prospectListList, "last", other), navigationExpected(cfg, prospectLists, passes, cp, pp))
	// Derive the tail independently from the full forward walk's rows.
	if prospectListList {
		all := []ProspectListRow{}
		for _, window := range windows {
			all = append(all, window.ProspectLists...)
		}
		if !reflect.DeepEqual(last.ProspectLists, all[len(all)-paging.DefaultLimit:]) || last.ProspectListsPage != "last" || last.ProspectListsNext != "" || last.ProspectListsPrev != navigationToken(prospectLists, passes, true, true, len(all)-paging.DefaultLimit) {
			t.Fatal("prospect_lists last is not forward-walk tail")
		}
	} else {
		all := []PassRow{}
		for _, window := range windows {
			all = append(all, window.Passes...)
		}
		if !reflect.DeepEqual(last.Passes, all[len(all)-paging.DefaultLimit:]) || last.PassesPage != "last" || last.PassesNext != "" || last.PassesPrev != navigationToken(prospectLists, passes, false, true, len(all)-paging.DefaultLimit) {
			t.Fatal("passes last is not forward-walk tail")
		}
	}
}

// R-QSFF-FORR
func TestLandingAgreesWithTools(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			cfg, prospectLists, passes := navigationFixture(t, count)
			writer := telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: fixtureTime, Rand: &sequence{}, Stderr: io.Discard})
			defer writer.Shutdown(t.Context(), "fixture-stop")
			server := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Telemetry: writer})
			tools.Register(server, tools.Config{Store: cfg.Store})
			endpoint := httptest.NewServer(identity.Require(server))
			defer endpoint.Close()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint.URL})
			for _, prospectListList := range []bool{true, false} {
				token := ""
				for step := 0; ; step++ {
					if step > count/paging.DefaultLimit+1 {
						t.Fatal("tool walk did not terminate")
					}
					next := ""
					ids := []string{}
					if prospectListList {
						var answer tools.ProspectListList
						callPageTool(t, client, "prospect_lists", token, &answer)
						for _, row := range answer.ProspectLists {
							ids = append(ids, row.ID)
						}
						if answer.Next != nil {
							next = *answer.Next
						}
					} else {
						var answer tools.PassList
						callPageTool(t, client, "passes", token, &answer)
						for _, row := range answer.Passes {
							ids = append(ids, row.ID)
						}
						if answer.Next != nil {
							next = *answer.Next
						}
					}
					others := []string{""}
					if count > paging.DefaultLimit {
						others = navigationOtherValues(prospectLists, passes, !prospectListList)
					}
					for _, other := range others {
						cp, pp := other, token
						if prospectListList {
							cp, pp = token, other
						}
						data := navigationLanding(t, cfg, navigationPath(prospectListList, token, other), navigationExpected(cfg, prospectLists, passes, cp, pp))
						got := []string{}
						gotNext, pageValue := data.PassesNext, data.PassesPage
						if prospectListList {
							for _, row := range data.ProspectLists {
								got = append(got, row.ID)
							}
							gotNext, pageValue = data.ProspectListsNext, data.ProspectListsPage
						} else {
							for _, row := range data.Passes {
								got = append(got, row.ID)
							}
						}
						if !reflect.DeepEqual(got, ids) || gotNext != next || pageValue != token {
							t.Fatal("tool and page differ", prospectListList, got, ids, gotNext, next, pageValue, token)
						}
					}
					if next == "" {
						break
					}
					token = next
				}
			}
		})
	}
}

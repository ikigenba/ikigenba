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

// R-BCBE-X9AQ R-BDJB-B11F R-BER7-OSS4 R-BFZ4-2KIT R-AMPI-W2Q5 R-ANXF-9UGU R-AYWI-PS53
func TestNavigationWalksAndTail(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 1, 3*paging.DefaultLimit + 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			cfg, cohorts, passes := navigationFixture(t, count)
			// Every key ties, including both sides of every window boundary.
			for i := 1; i < count; i++ {
				if cohorts[i].Name != cohorts[0].Name || !passes[i].Started.Equal(passes[0].Started) {
					t.Fatal("fixture must exercise tie breakers")
				}
			}
			for _, cohortList := range []bool{true, false} {
				for _, other := range navigationOtherValues(cohorts, passes, !cohortList) {
					t.Run(strconv.FormatBool(cohortList)+"/"+other, func(t *testing.T) {
						navigationWalk(t, cfg, cohorts, passes, cohortList, other)
					})
				}
			}
		})
	}
}

func navigationFixture(t *testing.T, count int) (Config, []store.Cohort, []store.Pass) {
	t.Helper()
	cfg, _ := pageFixture(t)
	ctx := context.Background()
	for i := 0; i < count; i++ {
		_, err := cfg.Store.CreateCohort(ctx, store.Cohort{Name: "navigation-cohort", Size: store.CohortSizeDefault, CreatedUserID: "navigation-creator", CreatedUserEmail: "navigation@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := cfg.Store.AddPass(ctx, store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "navigation-starter"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = cfg.Store.FinishPass(ctx, p.ID, store.Counts{Rows: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	cohorts, err := cfg.Store.ListCohorts(ctx, "", "", count+1)
	if err != nil || len(cohorts) != count {
		t.Fatal("cohorts fixture", len(cohorts), err)
	}
	passes, err := cfg.Store.ListPasses(ctx, "", "", count+1)
	if err != nil || len(passes) != count {
		t.Fatal("passes fixture", len(passes), err)
	}
	return cfg, cohorts, passes
}

func navigationToken(cohorts []store.Cohort, passes []store.Pass, cohortList, before bool, index int) string {
	suffix := ""
	if before {
		suffix = "-before"
	}
	if cohortList {
		e := cohorts[index]
		return (paging.Token{Tool: "cohorts" + suffix}).Encode(e.Name, e.ID)
	}
	p := passes[index]
	return (paging.Token{Tool: "passes" + suffix}).Encode(p.Started.UTC().Format(time.RFC3339), p.ID)
}

func navigationOtherValues(cohorts []store.Cohort, passes []store.Pass, cohortList bool) []string {
	values := []string{"", navigationToken(cohorts, passes, cohortList, false, paging.DefaultLimit-1), "last"}
	if len(cohorts) > paging.DefaultLimit+1 {
		values = append(values, navigationToken(cohorts, passes, cohortList, true, len(cohorts)-1))
	}
	return values
}

func navigationBounds(cohorts []store.Cohort, passes []store.Pass, cohortList bool, value string) (int, int) {
	count := len(cohorts)
	if value == "" {
		return 0, min(count, paging.DefaultLimit)
	}
	if value == "last" {
		return count - paging.DefaultLimit, count
	}
	for i := 0; i < count; i++ {
		if value == navigationToken(cohorts, passes, cohortList, false, i) {
			return i + 1, min(count, i+1+paging.DefaultLimit)
		}
		if value == navigationToken(cohorts, passes, cohortList, true, i) {
			return max(0, i-paging.DefaultLimit), i
		}
	}
	panic("navigation fixture token not found")
}

func navigationExpected(cfg Config, cohorts []store.Cohort, passes []store.Pass, cp, pp string) LandingData {
	data := LandingData{Banner: bannerFor(cfg), Cohorts: []CohortRow{}, Passes: []PassRow{}, CohortsPage: cp, PassesPage: pp}
	start, end := navigationBounds(cohorts, passes, true, cp)
	for i := start; i < end; i++ {
		data.Cohorts = append(data.Cohorts, cohortRow(cohorts[i]))
	}
	if cp != "" && start < end {
		data.CohortsPrev = navigationToken(cohorts, passes, true, true, start)
	}
	if end < len(cohorts) {
		data.CohortsNext = navigationToken(cohorts, passes, true, false, end-1)
	}
	start, end = navigationBounds(cohorts, passes, false, pp)
	for i := start; i < end; i++ {
		data.Passes = append(data.Passes, passRow(passes[i]))
	}
	if pp != "" && start < end {
		data.PassesPrev = navigationToken(cohorts, passes, false, true, start)
	}
	if end < len(passes) {
		data.PassesNext = navigationToken(cohorts, passes, false, false, end-1)
	}
	return data
}

func navigationPath(cohortList bool, value, other string) string {
	cp, pp := other, value
	if cohortList {
		cp, pp = value, other
	}
	return "/?" + (url.Values{"cohorts_page": {cp}, "passes_page": {pp}}).Encode()
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

func navigationWalk(t *testing.T, cfg Config, cohorts []store.Cohort, passes []store.Pass, cohortList bool, other string) {
	t.Helper()
	var windows []LandingData
	value := ""
	for step := 0; ; step++ {
		if step > len(cohorts)/paging.DefaultLimit+1 {
			t.Fatal("forward walk did not terminate")
		}
		cp, pp := other, value
		if cohortList {
			cp, pp = value, other
		}
		got := navigationLanding(t, cfg, navigationPath(cohortList, value, other), navigationExpected(cfg, cohorts, passes, cp, pp))
		windows = append(windows, got)
		next := got.PassesNext
		if cohortList {
			next = got.CohortsNext
		}
		if next == "" {
			break
		}
		value = next
	}
	if len(windows) != (len(cohorts)+paging.DefaultLimit-1)/paging.DefaultLimit {
		t.Fatal("forward window count", len(windows))
	}
	// Follow each actual Prev, requiring precisely the preceding forward
	// window, including full row data.
	for j := len(windows) - 1; j > 0; j-- {
		prev := windows[j].PassesPrev
		if cohortList {
			prev = windows[j].CohortsPrev
		}
		if prev == "" {
			t.Fatal("missing backward token", j)
		}
		if j == 1 {
			response := requestPage(cfg, http.MethodGet, navigationPath(cohortList, prev, other), "reader")
			location := "/"
			if other != "" {
				param := "cohorts_page"
				if cohortList {
					param = "passes_page"
				}
				location = "/?" + param + "=" + other
			}
			if response.Code != http.StatusSeeOther || !reflect.DeepEqual(response.Header().Values("Location"), []string{location}) || response.Body.Len() != 0 {
				t.Fatal("backward first-window redirect", response)
			}
		} else {
			cp, pp := other, prev
			if cohortList {
				cp, pp = prev, other
			}
			got := navigationLanding(t, cfg, navigationPath(cohortList, prev, other), navigationExpected(cfg, cohorts, passes, cp, pp))
			if cohortList && !reflect.DeepEqual(got.Cohorts, windows[j-1].Cohorts) || !cohortList && !reflect.DeepEqual(got.Passes, windows[j-1].Passes) {
				t.Fatal("backward window differs", j)
			}
			windows[j-1] = got
		}
	}
	cp, pp := other, "last"
	if cohortList {
		cp, pp = "last", other
	}
	last := navigationLanding(t, cfg, navigationPath(cohortList, "last", other), navigationExpected(cfg, cohorts, passes, cp, pp))
	// Derive the tail independently from the full forward walk's rows.
	if cohortList {
		all := []CohortRow{}
		for _, window := range windows {
			all = append(all, window.Cohorts...)
		}
		if !reflect.DeepEqual(last.Cohorts, all[len(all)-paging.DefaultLimit:]) || last.CohortsPage != "last" || last.CohortsNext != "" || last.CohortsPrev != navigationToken(cohorts, passes, true, true, len(all)-paging.DefaultLimit) {
			t.Fatal("cohorts last is not forward-walk tail")
		}
	} else {
		all := []PassRow{}
		for _, window := range windows {
			all = append(all, window.Passes...)
		}
		if !reflect.DeepEqual(last.Passes, all[len(all)-paging.DefaultLimit:]) || last.PassesPage != "last" || last.PassesNext != "" || last.PassesPrev != navigationToken(cohorts, passes, false, true, len(all)-paging.DefaultLimit) {
			t.Fatal("passes last is not forward-walk tail")
		}
	}
}

// R-AXOM-C0EE
func TestLandingAgreesWithTools(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			cfg, cohorts, passes := navigationFixture(t, count)
			writer := telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: fixtureTime, Rand: &sequence{}, Stderr: io.Discard})
			defer writer.Shutdown(t.Context(), "fixture-stop")
			server := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Telemetry: writer})
			tools.Register(server, tools.Config{Store: cfg.Store})
			endpoint := httptest.NewServer(identity.Require(server))
			defer endpoint.Close()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: endpoint.URL})
			for _, cohortList := range []bool{true, false} {
				token := ""
				for step := 0; ; step++ {
					if step > count/paging.DefaultLimit+1 {
						t.Fatal("tool walk did not terminate")
					}
					next := ""
					ids := []string{}
					if cohortList {
						var answer tools.CohortList
						callPageTool(t, client, "cohorts", token, &answer)
						for _, row := range answer.Cohorts {
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
						others = navigationOtherValues(cohorts, passes, !cohortList)
					}
					for _, other := range others {
						cp, pp := other, token
						if cohortList {
							cp, pp = token, other
						}
						data := navigationLanding(t, cfg, navigationPath(cohortList, token, other), navigationExpected(cfg, cohorts, passes, cp, pp))
						got := []string{}
						gotNext, pageValue := data.PassesNext, data.PassesPage
						if cohortList {
							for _, row := range data.Cohorts {
								got = append(got, row.ID)
							}
							gotNext, pageValue = data.CohortsNext, data.CohortsPage
						} else {
							for _, row := range data.Passes {
								got = append(got, row.ID)
							}
						}
						if !reflect.DeepEqual(got, ids) || gotNext != next || pageValue != token {
							t.Fatal("tool and page differ", cohortList, got, ids, gotNext, next, pageValue, token)
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

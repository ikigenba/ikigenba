package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestLandingControllerWiresEveryInteractiveControlInOneBrowserSession(t *testing.T) {
	// R-SPNZ-LS3V
	// R-SQVV-ZJUK
	// R-SS3S-DBL9
	// R-STBO-R3BY
	// R-SUJL-4V2N
	// R-SVRH-IMTC
	// R-SWZD-WEK1
	// R-SY7A-A6AQ
	chromePath, err := exec.LookPath("google-chrome")
	if err != nil {
		t.Fatalf("google-chrome must be on PATH: %v", err)
	}

	fixture := newLandingFixture(t)
	keys := []string{
		"code/alpha", "code/beta", "code/demo", "code/zeta",
		"prompts/alpha", "prompts/beta", "scripts/alpha", "scripts/beta",
		"scripts/gamma", "sites/alpha", "sites/beta", "sites/gamma",
	}
	for _, key := range keys {
		kind, name, _ := strings.Cut(key, "/")
		fixture.add(t, kind, name, "browser-owner", 1, false)
	}

	site := loadReposWWW(t)
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", landingHandler(site, fixture.store, fixture.custody, "repos", "v9.8.7-test"))
	mux.Handle("GET /static/", site.Static())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	timeoutContext, cancelTimeout := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancelTimeout)
	browserContext, cancelBrowser, err := launchLandingBrowser(timeoutContext, chromePath, t.TempDir())
	if err != nil {
		t.Fatalf("launch google-chrome after one retry: %v", err)
	}
	t.Cleanup(cancelBrowser)

	if err := chromedp.Run(browserContext,
		chromedp.ActionFunc(func(ctx context.Context) error {
			params := struct {
				Permissions []browser.PermissionType `json:"permissions"`
				Origin      string                   `json:"origin"`
			}{Permissions: []browser.PermissionType{browser.PermissionTypeClipboardReadWrite, browser.PermissionTypeClipboardSanitizedWrite}, Origin: server.URL}
			return cdp.Execute(ctx, "Browser.grantPermissions", params, nil)
		}),
		chromedp.Navigate(server.URL),
		chromedp.WaitVisible("#repo-search", chromedp.ByQuery),
		chromedp.WaitVisible(".pager", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("boot landing controller: %v", err)
	}
	if label := browserText(t, browserContext, "#pager-label"); label != "Page 1 of 2" {
		t.Fatalf("initial pager label = %q, want %q", label, "Page 1 of 2")
	}

	if err := chromedp.Run(browserContext, chromedp.SendKeys("#repo-search", "cdm", chromedp.ByQuery)); err != nil {
		t.Fatalf("type filter query: %v", err)
	}
	if got := browserRowKeys(t, browserContext); !reflect.DeepEqual(got, []string{"code/demo"}) {
		t.Fatalf("filtered row names = %v, want [code/demo]", got)
	}

	if err := chromedp.Run(browserContext,
		chromedp.SendKeys("#repo-search", strings.Repeat(kb.Backspace, 3), chromedp.ByQuery),
		chromedp.Click("th[data-sort-key='key']", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("clear query and click Name header: %v", err)
	}
	descending := reverseLandingKeys(keys)
	if got := browserRowKeys(t, browserContext); !reflect.DeepEqual(got, descending[:10]) {
		t.Fatalf("descending first page = %v, want %v", got, descending[:10])
	}
	if sortDirection := browserAttribute(t, browserContext, "th[data-sort-key='key']", "aria-sort"); sortDirection != "descending" {
		t.Fatalf("first Name click aria-sort = %q, want descending", sortDirection)
	}
	if err := chromedp.Run(browserContext, chromedp.Click("th[data-sort-key='key']", chromedp.ByQuery)); err != nil {
		t.Fatalf("click Name header a second time: %v", err)
	}
	if got := browserRowKeys(t, browserContext); !reflect.DeepEqual(got, keys[:10]) {
		t.Fatalf("restored ascending first page = %v, want %v", got, keys[:10])
	}
	if sortDirection := browserAttribute(t, browserContext, "th[data-sort-key='key']", "aria-sort"); sortDirection != "ascending" {
		t.Fatalf("second Name click aria-sort = %q, want ascending", sortDirection)
	}

	if err := chromedp.Run(browserContext,
		chromedp.SendKeys("#repo-search", "cdm", chromedp.ByQuery),
		chromedp.Click("#repo-clear", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("filter then click Clear: %v", err)
	}
	var searchValue string
	if err := chromedp.Run(browserContext, chromedp.Value("#repo-search", &searchValue, chromedp.ByQuery)); err != nil {
		t.Fatalf("read cleared search: %v", err)
	}
	if searchValue != "" {
		t.Fatalf("Clear left search value %q", searchValue)
	}
	if got := browserRowKeys(t, browserContext); !reflect.DeepEqual(got, keys[:10]) {
		t.Fatalf("Clear restored rows = %v, want %v", got, keys[:10])
	}
	if label := browserText(t, browserContext, "#pager-label"); label != "Page 1 of 2" {
		t.Fatalf("Clear restored pager label = %q, want Page 1 of 2", label)
	}

	if err := chromedp.Run(browserContext, chromedp.Click("#pager-next", chromedp.ByQuery)); err != nil {
		t.Fatalf("click Next: %v", err)
	}
	if label := browserText(t, browserContext, "#pager-label"); label != "Page 2 of 2" {
		t.Fatalf("Next pager label = %q, want Page 2 of 2", label)
	}
	if got := browserRowKeys(t, browserContext); !reflect.DeepEqual(got, keys[10:]) {
		t.Fatalf("second page rows = %v, want %v", got, keys[10:])
	}
	if err := chromedp.Run(browserContext, chromedp.Click("#pager-prev", chromedp.ByQuery)); err != nil {
		t.Fatalf("click Prev: %v", err)
	}
	if label := browserText(t, browserContext, "#pager-label"); label != "Page 1 of 2" {
		t.Fatalf("Prev pager label = %q, want Page 1 of 2", label)
	}
	if got := browserRowKeys(t, browserContext); !reflect.DeepEqual(got, keys[:10]) {
		t.Fatalf("Prev rows = %v, want %v", got, keys[:10])
	}

	var icon struct {
		Namespace string  `json:"namespace"`
		Width     float64 `json:"width"`
		Height    float64 `json:"height"`
	}
	var tableWidthBefore float64
	if err := chromedp.Run(browserContext,
		chromedp.Evaluate(`(() => { const svg = document.querySelector('.copy-btn svg'); const box = svg.getBoundingClientRect(); return {namespace: svg.namespaceURI, width: box.width, height: box.height}; })()`, &icon),
		chromedp.Evaluate(`document.querySelector('.repo-table').getBoundingClientRect().width`, &tableWidthBefore),
	); err != nil {
		t.Fatalf("inspect copy icon and table: %v", err)
	}
	if icon.Namespace != "http://www.w3.org/2000/svg" || icon.Width <= 0 || icon.Height <= 0 {
		t.Fatalf("copy icon = %#v, want rendered SVG namespace element", icon)
	}
	if err := chromedp.Run(browserContext,
		chromedp.Click(".copy-btn", chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('.copy-btn .copy-label').textContent === 'Copied'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
	); err != nil {
		t.Fatalf("click copy button: %v", err)
	}
	var clipboardText string
	var tableWidthAfter float64
	if err := chromedp.Run(browserContext,
		chromedp.Evaluate(`navigator.clipboard.readText()`, &clipboardText, func(params *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return params.WithAwaitPromise(true)
		}),
		chromedp.Evaluate(`document.querySelector('.repo-table').getBoundingClientRect().width`, &tableWidthAfter),
	); err != nil {
		t.Fatalf("read clipboard and table after copy: %v", err)
	}
	wantCloneURL := server.URL + "/srv/repos/git/code/alpha.git"
	if clipboardText != wantCloneURL {
		t.Fatalf("clipboard text = %q, want %q", clipboardText, wantCloneURL)
	}
	if label := browserText(t, browserContext, ".copy-btn .copy-label"); label != "Copied" {
		t.Fatalf("copy label = %q, want Copied", label)
	}
	if tableWidthAfter != tableWidthBefore {
		t.Fatalf("table width changed after copy: before=%v after=%v", tableWidthBefore, tableWidthAfter)
	}
}

func launchLandingBrowser(parent context.Context, chromePath, profilesRoot string) (context.Context, context.CancelFunc, error) {
	var launchErrors []string
	for attempt := 1; attempt <= 2; attempt++ {
		options := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
		options = append(options,
			chromedp.ExecPath(chromePath),
			chromedp.UserDataDir(filepath.Join(profilesRoot, fmt.Sprintf("attempt-%d", attempt))),
			chromedp.Flag("headless", "new"),
			chromedp.NoSandbox,
		)
		allocatorContext, cancelAllocator := chromedp.NewExecAllocator(parent, options...)
		browserContext, cancelContext := chromedp.NewContext(allocatorContext)
		if err := chromedp.Run(browserContext); err == nil {
			return browserContext, func() {
				_ = chromedp.Cancel(browserContext)
				cancelContext()
				cancelAllocator()
			}, nil
		} else {
			launchErrors = append(launchErrors, fmt.Sprintf("attempt %d: %v", attempt, err))
		}
		cancelContext()
		cancelAllocator()
	}
	return nil, func() {}, fmt.Errorf("%s", strings.Join(launchErrors, "; "))
}

func browserRowKeys(t *testing.T, ctx context.Context) []string {
	t.Helper()
	var keys []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`Array.from(document.querySelectorAll('.repo-table tbody td[data-label="Name"]'), cell => cell.textContent)`, &keys)); err != nil {
		t.Fatalf("read browser row keys: %v", err)
	}
	return keys
}

func browserText(t *testing.T, ctx context.Context, selector string) string {
	t.Helper()
	var value string
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf("document.querySelector(%q).textContent", selector), &value)); err != nil {
		t.Fatalf("read %s text: %v", selector, err)
	}
	return value
}

func browserAttribute(t *testing.T, ctx context.Context, selector, name string) string {
	t.Helper()
	value, ok := "", false
	if err := chromedp.Run(ctx, chromedp.AttributeValue(selector, name, &value, &ok, chromedp.ByQuery)); err != nil {
		t.Fatalf("read %s %s: %v", selector, name, err)
	}
	if !ok {
		t.Fatalf("%s has no %s attribute", selector, name)
	}
	return value
}

func reverseLandingKeys(keys []string) []string {
	reversed := make([]string, len(keys))
	for index := range keys {
		reversed[index] = keys[len(keys)-1-index]
	}
	return reversed
}

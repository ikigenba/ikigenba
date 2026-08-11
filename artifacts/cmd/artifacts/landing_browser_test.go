package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	appkitdb "appkit/db"
	artifactdata "artifacts/internal/artifacts"
	"artifacts/internal/db"
	artifactweb "artifacts/internal/web"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// R-55UH-BF0N
// R-572D-P6RC
func TestLandingControllerFiltersAndSortsInRealBrowserWithoutInjection(t *testing.T) {
	chrome, err := exec.LookPath("google-chrome")
	if err != nil {
		chrome, err = exec.LookPath("chromium")
	}
	if err != nil {
		t.Fatal("headless Chrome is a hard test precondition: google-chrome and chromium are not on PATH")
	}

	conn, err := appkitdb.Open(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), conn, migrations); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(conn, nil)
	created := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	fixtures := []db.CreateArtifactParams{
		{ID: "small", OwnerID: "owner", OwnerEmail: "small@example.com", Filename: "alpha.txt", Description: "ordinary", Visibility: "private", Size: 10, ContentHash: "one", CreatedAt: created},
		{ID: "largest", OwnerID: "owner", OwnerEmail: "large@example.com", Filename: "beta.bin", Description: "contains needle", Visibility: "public", Size: 3000, ContentHash: "two", CreatedAt: created.Add(time.Minute)},
		{ID: "most-downloaded", OwnerID: "owner", OwnerEmail: "hostile@example.com", Filename: `<script id="injected">alert(1)</script>.txt`, Description: `<script>alert("description")</script>`, Visibility: "private", Size: 100, ContentHash: "three", CreatedAt: created.Add(2 * time.Minute)},
	}
	for _, fixture := range fixtures {
		if _, err := store.CreateArtifact(context.Background(), fixture); err != nil {
			t.Fatal(err)
		}
	}
	for id, count := range map[string]int{"small": 2, "largest": 4, "most-downloaded": 9} {
		for range count {
			if changed, err := store.IncrementDownloadCount(context.Background(), id); err != nil || !changed {
				t.Fatalf("increment %s = %v, %v", id, changed, err)
			}
		}
	}

	svc := &artifactdata.Service{Store: store}
	mux := http.NewServeMux()
	mux.Handle("GET /srv/artifacts/{$}", artifactweb.LandingHandler(svc, "artifacts", "v0.1.0"))
	mux.Handle("GET /srv/artifacts/static/", http.StripPrefix("/srv/artifacts/", artifactweb.StaticHandler()))
	server := httptest.NewServer(mux)
	defer server.Close()

	options := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome))
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancelAllocator()
	browser, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	browser, cancelTimeout := context.WithTimeout(browser, 20*time.Second)
	defer cancelTimeout()
	var dialogOpened atomic.Bool
	chromedp.ListenTarget(browser, func(event any) {
		if _, ok := event.(*page.EventJavascriptDialogOpening); ok {
			dialogOpened.Store(true)
		}
	})

	firstID := func(want string) chromedp.Action {
		return chromedp.Poll(fmt.Sprintf(`document.querySelector(".artifact-table tbody tr") && document.querySelector(".artifact-table tbody tr").dataset.artifactId === %q`, want), nil)
	}
	visibleCount := func(want int) chromedp.Action {
		return chromedp.Poll(fmt.Sprintf(`document.querySelectorAll(".artifact-table tbody tr").length === %d`, want), nil)
	}
	var injected bool
	if err := chromedp.Run(browser,
		chromedp.Navigate(server.URL+"/srv/artifacts/"),
		chromedp.WaitVisible("#artifact-filter"),
		visibleCount(3),
		chromedp.SendKeys("#artifact-filter", "needle"),
		visibleCount(1),
		firstID("largest"),
		chromedp.Evaluate(`var input=document.querySelector("#artifact-filter"); input.value=""; input.dispatchEvent(new Event("input", {bubbles:true}));`, nil),
		visibleCount(3),
		chromedp.Click(`button[data-sort-key="size"]`),
		firstID("largest"),
		chromedp.Click(`button[data-sort-key="size"]`),
		firstID("small"),
		chromedp.Click(`button[data-sort-key="downloads"]`),
		firstID("most-downloaded"),
		chromedp.SetValue("#visibility-filter", "public"),
		chromedp.Evaluate(`document.querySelector("#visibility-filter").dispatchEvent(new Event("change", {bubbles:true}))`, nil),
		visibleCount(1),
		firstID("largest"),
		chromedp.Evaluate(`document.querySelector("#injected") !== null`, &injected),
	); err != nil {
		t.Fatal(err)
	}
	if dialogOpened.Load() || injected {
		t.Fatalf("hostile stored markup executed: dialog=%v injectedNode=%v", dialogOpened.Load(), injected)
	}
}

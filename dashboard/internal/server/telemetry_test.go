package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	dashtelemetry "dashboard/internal/telemetry"
)

func telemetryTestServer(t *testing.T) (*http.Server, serverDeps) {
	t.Helper()
	deps := newServerDeps(t)
	store := dashtelemetry.NewStore()
	store.SetCapacities(16*1024*1024*1024, 256*1024*1024*1024)
	store.Append(dashtelemetry.SeriesSystemMem, dashtelemetry.Sample{At: time.Unix(1, 0), Value: 8 * 1024 * 1024 * 1024})
	store.Append(dashtelemetry.SeriesSystemDisk, dashtelemetry.Sample{At: time.Unix(1, 0), Value: 128 * 1024 * 1024 * 1024})
	store.Append(dashtelemetry.SeriesServiceMem("crm"), dashtelemetry.Sample{At: time.Unix(1, 0), Value: 64 * 1024 * 1024})
	store.Append(dashtelemetry.SeriesServiceDisk("crm"), dashtelemetry.Sample{At: time.Unix(1, 0), Value: 512 * 1024 * 1024})

	opts := deps.opts()
	opts.Telemetry = store
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, deps
}

func TestTelemetryPageRendersChartsForSignedInSession(t *testing.T) {
	srv, deps := telemetryTestServer(t)
	cookie := mintSession(t, deps, "owner@int.ikigenba.com")

	rec := do(t, srv, "GET", "https://int.ikigenba.com/telemetry",
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})

	// R-FI68-9AT0
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div id="telemetry-block" data-fragment="/telemetry/fragment">`) {
		t.Errorf("telemetry page missing poll container:\n%s", body)
	}
	if !strings.Contains(body, `<div class="telemetry-charts" aria-label="Telemetry charts">`) {
		t.Errorf("telemetry page missing charts block:\n%s", body)
	}
	if !strings.Contains(body, `class="telemetry-chart telemetry-hero-chart"`) {
		t.Errorf("telemetry page missing rendered hero chart SVG:\n%s", body)
	}
}

func TestTelemetryPageRedirectsSignedOutToIndex(t *testing.T) {
	srv, _ := telemetryTestServer(t)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/telemetry", nil)

	// R-FJE4-N2JP
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
	if strings.Contains(rec.Body.String(), `telemetry-charts`) || strings.Contains(rec.Body.String(), `telemetry-hero-chart`) {
		t.Errorf("signed-out redirect rendered telemetry charts:\n%s", rec.Body.String())
	}
}

func TestTelemetryFragmentRendersChartsForSignedInSession(t *testing.T) {
	srv, deps := telemetryTestServer(t)
	cookie := mintSession(t, deps, "owner@int.ikigenba.com")

	rec := do(t, srv, "GET", "https://int.ikigenba.com/telemetry/fragment",
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})

	// R-FKM1-0UAE
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div class="telemetry-charts" aria-label="Telemetry charts">`) {
		t.Errorf("telemetry fragment missing charts block:\n%s", body)
	}
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("telemetry fragment rendered full page chrome:\n%s", body)
	}
	if !strings.Contains(body, `class="telemetry-chart telemetry-stacked-chart"`) {
		t.Errorf("telemetry fragment missing rendered stacked chart SVG:\n%s", body)
	}
}

func TestTelemetryFragmentRequiresSession(t *testing.T) {
	srv, _ := telemetryTestServer(t)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/telemetry/fragment", nil)

	// R-FLTX-EM13
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `telemetry-charts`) || strings.Contains(rec.Body.String(), `telemetry-hero-chart`) {
		t.Errorf("signed-out fragment rendered telemetry charts:\n%s", rec.Body.String())
	}
}

func TestTelemetryPollScriptIsServed(t *testing.T) {
	srv := testServer(t)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.js", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`document.getElementById("telemetry-block")`,
		`block.dataset.fragment`,
		`fetch(fragURL, { credentials: "same-origin" })`,
		`setInterval(refreshTelemetry, 60000)`,
	} {
		// R-FN1T-SDRS
		if !strings.Contains(body, want) {
			t.Errorf("served app.js missing telemetry poll token %q:\n%s", want, body)
		}
	}
}

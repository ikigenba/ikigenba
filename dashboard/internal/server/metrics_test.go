package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"dashboard/internal/metrics"
)

func metricsTestServer(t *testing.T) (*http.Server, serverDeps) {
	t.Helper()
	deps := newServerDeps(t)
	store := metrics.NewStore()
	store.SetCapacities(16*1024*1024*1024, 256*1024*1024*1024)
	store.Append(metrics.SeriesSystemMem, metrics.Sample{At: time.Unix(1, 0), Value: 8 * 1024 * 1024 * 1024})
	store.Append(metrics.SeriesSystemDisk, metrics.Sample{At: time.Unix(1, 0), Value: 128 * 1024 * 1024 * 1024})
	store.Append(metrics.SeriesServiceMem("crm"), metrics.Sample{At: time.Unix(1, 0), Value: 64 * 1024 * 1024})
	store.Append(metrics.SeriesServiceDisk("crm"), metrics.Sample{At: time.Unix(1, 0), Value: 512 * 1024 * 1024})

	opts := deps.opts()
	opts.Metrics = store
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, deps
}

func TestMetricsPageRendersChartsForSignedInSession(t *testing.T) {
	srv, deps := metricsTestServer(t)
	cookie := mintSession(t, deps, "owner@int.ikigenba.com")

	rec := do(t, srv, "GET", "https://int.ikigenba.com/metrics",
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})

	// R-WWGV-S5V0
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<title>Metrics · ikigenba dashboard</title>`,
		`<h1>Metrics</h1>`,
		`<div id="metrics-block" data-fragment="/metrics/fragment">`,
		`<div class="metrics-charts" aria-label="Metrics charts">`,
		`class="metrics-chart metrics-hero-chart"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "telemetry") {
		t.Errorf("metrics page contains retired surface name:\n%s", body)
	}
}

func TestMetricsPageRedirectsMissingAndInvalidSessionsToIndex(t *testing.T) {
	srv, _ := metricsTestServer(t)
	tests := []struct {
		name   string
		header map[string]string
	}{
		{name: "missing"},
		{name: "invalid", header: map[string]string{"Cookie": sessionCookieName + "=invalid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, srv, "GET", "https://int.ikigenba.com/metrics", tt.header)

			// R-WXOS-5XLP
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "/" {
				t.Errorf("Location = %q, want /", loc)
			}
			if strings.Contains(rec.Body.String(), `metrics-charts`) || strings.Contains(rec.Body.String(), `metrics-hero-chart`) {
				t.Errorf("redirect rendered metrics charts:\n%s", rec.Body.String())
			}
		})
	}
}

func TestMetricsFragmentRendersChartsForSignedInSession(t *testing.T) {
	srv, deps := metricsTestServer(t)
	cookie := mintSession(t, deps, "owner@int.ikigenba.com")

	rec := do(t, srv, "GET", "https://int.ikigenba.com/metrics/fragment",
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})

	// R-WYWO-JPCE
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<div class="metrics-charts" aria-label="Metrics charts">`) {
		t.Errorf("metrics fragment missing charts block:\n%s", body)
	}
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("metrics fragment rendered full page chrome:\n%s", body)
	}
	if !strings.Contains(body, `class="metrics-chart metrics-stacked-chart"`) {
		t.Errorf("metrics fragment missing rendered stacked chart SVG:\n%s", body)
	}
}

func TestMetricsFragmentRequiresSession(t *testing.T) {
	srv, _ := metricsTestServer(t)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/metrics/fragment", nil)

	// R-X1CH-B8TS
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `metrics-charts`) || strings.Contains(rec.Body.String(), `metrics-hero-chart`) {
		t.Errorf("signed-out fragment rendered metrics charts:\n%s", rec.Body.String())
	}
}

func TestMetricsPollScriptIsServed(t *testing.T) {
	srv := testServer(t)

	rec := do(t, srv, "GET", "https://int.ikigenba.com/static/app.js", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// R-X2KD-P0KH
	for _, want := range []string{
		`document.getElementById("metrics-block")`,
		`block.dataset.fragment`,
		`fetch(fragURL, { credentials: "same-origin" })`,
		`setInterval(refreshMetrics, 60000)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("served app.js missing metrics poll token %q:\n%s", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "telemetry") {
		t.Errorf("served app.js contains retired surface identifier:\n%s", body)
	}
}

func TestRetiredTelemetryRoutesReturnNotFoundForSignedInSession(t *testing.T) {
	srv, deps := metricsTestServer(t)
	cookie := mintSession(t, deps, "owner@int.ikigenba.com")

	// R-X3SA-2SB6
	for _, path := range []string{"/telemetry", "/telemetry/fragment"} {
		rec := do(t, srv, "GET", "https://int.ikigenba.com"+path,
			map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want exactly 404", path, rec.Code)
		}
	}
}

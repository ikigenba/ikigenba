package server

import (
	"bytes"
	"errors"
	"net/http"

	"dashboard/internal/metrics"
	"dashboard/internal/session"
)

type metricsPageData struct {
	Owner        string
	OwnerInitial string
	Charts       metrics.ChartView
}

// handleMetrics renders the signed-in metrics page. Anonymous and dead
// sessions go back to the login landing; the XHR fragment uses a 401 instead.
func (a *app) handleMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := a.sessionOwner(r)
		if !ok {
			if c, err := r.Cookie(sessionCookieName); err == nil {
				if _, lerr := a.sessions.Lookup(r.Context(), c.Value); errors.Is(lerr, session.ErrInvalid) {
					clearSessionCookie(w, r)
				}
			}
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		data := metricsPageData{
			Owner:        owner,
			OwnerInitial: ownerInitial(owner),
			Charts:       a.metricsChartView(),
		}

		var buf bytes.Buffer
		if err := a.tmpl.ExecuteTemplate(&buf, "metrics.html", data); err != nil {
			a.logger.Error("metrics.render", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

// handleMetricsFragment renders only the chart block the client refreshes on
// the collector's one-minute cadence.
func (a *app) handleMetricsFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.requireSession(w, r); !ok {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := a.tmpl.ExecuteTemplate(w, "metrics_charts", a.metricsChartView()); err != nil {
			a.logger.Error("metrics.fragment.render", "err", err)
		}
	}
}

func (a *app) metricsChartView() metrics.ChartView {
	if a.metrics == nil {
		return metrics.NewChartView(metrics.Snapshot{})
	}
	return metrics.NewChartView(a.metrics.Snapshot())
}

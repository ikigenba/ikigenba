package server

import (
	"bytes"
	"errors"
	"net/http"

	"dashboard/internal/session"
	"dashboard/internal/telemetry"
)

type telemetryPageData struct {
	Owner        string
	OwnerInitial string
	Charts       telemetry.ChartView
}

// handleTelemetry renders the signed-in telemetry page. Anonymous and dead
// sessions go back to the login landing; the XHR fragment uses a 401 instead.
func (a *app) handleTelemetry() http.HandlerFunc {
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

		data := telemetryPageData{
			Owner:        owner,
			OwnerInitial: ownerInitial(owner),
			Charts:       a.telemetryChartView(),
		}

		var buf bytes.Buffer
		if err := a.tmpl.ExecuteTemplate(&buf, "telemetry.html", data); err != nil {
			a.logger.Error("telemetry.render", "err", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

// handleTelemetryFragment renders only the chart block the client refreshes on
// the collector's one-minute cadence.
func (a *app) handleTelemetryFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.requireSession(w, r); !ok {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := a.tmpl.ExecuteTemplate(w, "telemetry_charts", a.telemetryChartView()); err != nil {
			a.logger.Error("telemetry.fragment.render", "err", err)
		}
	}
}

func (a *app) telemetryChartView() telemetry.ChartView {
	if a.telemetry == nil {
		return telemetry.NewChartView(telemetry.Snapshot{})
	}
	return telemetry.NewChartView(a.telemetry.Snapshot())
}

package web

import (
	"errors"
	"io"
	"net/http"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

func refusal(w http.ResponseWriter, r *http.Request, status int, line string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, line+"\n")
	}
}
func connectRoute(b *broker.Broker, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !urls.SameOrigin(r) {
			refusal(w, r, http.StatusForbidden, urls.CrossOrigin)
			return
		}
		loc, err := b.Begin(r.Context(), name)
		if err != nil {
			switch {
			case errors.Is(err, broker.ErrNoCallback):
				refusal(w, r, 500, broker.NoCallback)
			case errors.Is(err, broker.ErrRandom):
				refusal(w, r, 500, broker.NoRandom)
			default:
				refusal(w, r, 503, store.Unreachable)
			}
			return
		}
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	}
}
func callbackRoute(b *broker.Broker, cfg Config, set *pages.Set, cl providers.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := b.Complete(r.Context(), cl.Provider.Name, r.URL.Query())
		if err == nil {
			w.Header().Set("Location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}
		if errors.Is(err, store.ErrUnreachable) {
			refusal(w, r, 503, store.Unreachable)
			return
		}
		f := ""
		status := 400
		switch {
		case errors.Is(err, broker.ErrMissing):
			f = pages.FailureMissing
		case errors.Is(err, broker.ErrUnknown):
			f = pages.FailureUnknown
		case errors.Is(err, broker.ErrExpired):
			f = pages.FailureExpired
		case errors.Is(err, broker.ErrForeign):
			f = pages.FailureForeign
		case errors.Is(err, broker.ErrDenied):
			f = pages.FailureDenied
		case errors.Is(err, broker.ErrExchange):
			f = pages.FailureExchange
			status = 502
		case errors.Is(err, broker.ErrAccount):
			f = pages.FailureAccount
			status = 502
		}
		a := urls.Auth(r, cfg.ServicesPath)
		banner := cfg.Banner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: a + "/", LogoutURL: a + "/logout"})
		banner.Trail = nil
		set.Write(w, r, status, "notice", pages.NoticeData{Banner: banner, DisplayName: cl.Provider.DisplayName, Failure: f})
	}
}

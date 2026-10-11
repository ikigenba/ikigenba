package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

func jsonAnswer(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
func tokenRoute(b *broker.Broker, cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			jsonAnswer(w, 405, broker.Refusal{Error: broker.RefusalMethod})
			return
		}
		name := strings.TrimPrefix(r.URL.EscapedPath(), "/token/")
		tok, err := b.Token(r.Context(), name)
		if err == nil {
			jsonAnswer(w, 200, tok)
			return
		}
		status := 503
		v := broker.Refusal{Error: broker.RefusalUnavailable}
		switch {
		case errors.Is(err, broker.ErrUnknownProvider):
			status = 404
			v.Error = broker.RefusalUnknownProvider
		case errors.Is(err, store.ErrNotFound):
			status = 404
			v.Error = broker.RefusalNotConnected
		case errors.Is(err, broker.ErrReconnect):
			status = 409
			v.Error = broker.RefusalReconnect
			v.URL = urls.Connect(urls.Configured(cfg.ServicesPath), name)
		case errors.Is(err, broker.ErrProvider):
			status = 502
			v.Error = broker.RefusalProvider
		}
		jsonAnswer(w, status, v)
	}
}

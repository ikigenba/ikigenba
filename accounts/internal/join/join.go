package join

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config supplies the enrollment page's dependencies.
type Config struct {
	Pages      *pages.Set
	Store      *store.Store
	Configured bool
	Domain     string
	Limiter    *Limiter
	Telemetry  *telemetry.Writer
}

// Handler serves the invite form and accepts enrollment submissions.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(cfg.serve)
}

func (cfg Config) serve(w http.ResponseWriter, r *http.Request) {
	headers := w.Header()
	headers.Set("Cache-Control", "no-store")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("X-Robots-Tag", "noindex, nofollow")

	var body []byte
	var bodyErr error
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		addr, err := valid.Address(r.Header.Get("X-Real-IP"))
		if err != nil {
			addr = ""
		}
		if ok, wait := cfg.Limiter.Allow(addr); !ok {
			seconds := (wait + time.Second - 1) / time.Second
			headers.Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
			cfg.Pages.Write(w, r, http.StatusTooManyRequests, "join", pages.JoinData{Outcome: pages.JoinLimited})
			return
		}
	default:
		headers.Set("Allow", "GET, HEAD, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != Path || !cfg.Configured {
		cfg.unusable(w, r)
		return
	}
	tokens := r.URL.Query()[TokenParam]
	if len(tokens) != 1 || !store.ValidToken(tokens[0]) {
		cfg.unusable(w, r)
		return
	}
	token := tokens[0]
	if r.Method == http.MethodPost && r.Body != nil {
		body, bodyErr = io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if _, oversized := errors.AsType[*http.MaxBytesError](bodyErr); oversized {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
	}
	invite, err := cfg.Store.InviteByToken(r.Context(), token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		cfg.unusable(w, r)
	case err != nil:
		unavailable(w, r)
	case r.Method == http.MethodPost:
		user, key := submitted(r, body, bodyErr)
		cfg.enroll(w, r, token, invite, user, key)
	default:
		cfg.Pages.Write(w, r, http.StatusOK, "join", cfg.inviteData(invite))
	}
}

func submitted(r *http.Request, body []byte, bodyErr error) (string, string) {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if bodyErr != nil || media != "application/x-www-form-urlencoded" {
		return "", ""
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return "", ""
	}
	return form.Get("user"), form.Get("key")
}

func (cfg Config) enroll(w http.ResponseWriter, r *http.Request, token string, invite store.Invite, user, key string) {
	data := cfg.inviteData(invite)
	data.User, data.Key = user, key
	nameRefusal := func() {
		data.Outcome = pages.JoinName
		cfg.Pages.Write(w, r, http.StatusUnprocessableEntity, "join", data)
	}
	if err := valid.Name(user); err != nil {
		nameRefusal()
		return
	}
	ctx := r.Context()
	held, err := cfg.Store.NameHeld(ctx, user)
	if err != nil {
		unavailable(w, r)
		return
	}
	if held {
		nameRefusal()
		return
	}
	parsed, err := valid.ParseKey(strings.Trim(key, " \t\r\n"))
	if err != nil {
		data.Outcome = pages.JoinKey
		cfg.Pages.Write(w, r, http.StatusUnprocessableEntity, "join", data)
		return
	}
	used, created, err := cfg.Store.Enroll(ctx, token, user, store.Key{Type: parsed.Type, Body: parsed.Body, Line: parsed.Line()})
	switch {
	case err == nil:
		account, err := cfg.Store.Account(ctx, used.Account)
		host := ""
		if err == nil {
			host = account.Host
		}
		eventCtx := identity.NewContext(ctx, identity.Caller{RequestID: r.Header.Get("X-Request-Id")})
		trail.InviteUsed(eventCtx, cfg.Telemetry, used.ID, used.Account, host, created.Name, created.Admin)
		data.Outcome, data.User, data.Key, data.Admin = pages.JoinDone, created.Name, "", created.Admin
		cfg.Pages.Write(w, r, http.StatusOK, "join", data)
	case errors.Is(err, store.ErrNotFound):
		cfg.unusable(w, r)
	case errors.Is(err, store.ErrTaken):
		nameRefusal()
	case errors.Is(err, store.ErrAccountFull):
		data.Outcome = pages.JoinFull
		cfg.Pages.Write(w, r, http.StatusConflict, "join", data)
	default:
		unavailable(w, r)
	}
}

func (cfg Config) inviteData(invite store.Invite) pages.JoinData {
	return pages.JoinData{Account: invite.Account, DNSName: invite.Account + "." + cfg.Domain, Admin: invite.Admin}
}

func (cfg Config) unusable(w http.ResponseWriter, r *http.Request) {
	cfg.Pages.Write(w, r, http.StatusNotFound, "join", pages.JoinData{Outcome: pages.JoinUnusable})
}

func unavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, store.Unreachable+"\n")
	}
}

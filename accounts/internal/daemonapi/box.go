package daemonapi

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (s *Server) serveBox(w http.ResponseWriter, r *http.Request) {
	if s.cfg.BoxUser == nil || s.cfg.BoxAdmin == nil {
		http.Error(w, NotFound, http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	uid, err := parseUID(r.URL.RawQuery)
	if err != nil {
		http.Error(w, BadRequest, http.StatusBadRequest)
		return
	}
	host, verified := s.identify(w, r)
	if verified == nil {
		return
	}
	user, err := s.store.ActiveUserByUID(verified.Context(), host.Name, uid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, NotUser, http.StatusForbidden)
			return
		}
		http.Error(w, store.Unreachable, http.StatusServiceUnavailable)
		return
	}
	select {
	case s.boxSlots <- struct{}{}:
		defer func() { <-s.boxSlots }()
	default:
		http.Error(w, Busy, http.StatusServiceUnavailable)
		return
	}
	s.forwardBox(w, verified, user)
}

func parseUID(raw string) (int64, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return 0, err
	}
	uid, err := decimalQueryValue(query[protocol.QueryUID])
	if uid > math.MaxUint32 {
		return 0, errors.New("uid exceeds its wire range")
	}
	return uid, err
}

func (s *Server) forwardBox(w http.ResponseWriter, r *http.Request, user store.User) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, protocol.MaxBoxRequest))
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		http.Error(w, TooLarge, http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, BadRequest, http.StatusBadRequest)
		return
	}
	method := protocol.BoxMethod(raw)
	if method == "" {
		http.Error(w, BadRequest, http.StatusBadRequest)
		return
	}

	handler := s.cfg.BoxAdmin
	if method == protocol.MethodToolsList && !user.Admin {
		handler = s.cfg.BoxUser
	}
	original, _ := identity.FromContext(r.Context())
	ctx := identity.NewContext(r.Context(), identity.Caller{
		UserID:    box.ActorPrefix + user.Name,
		RequestID: original.RequestID,
	})
	request := r.Clone(ctx)
	for name := range request.Header {
		if strings.EqualFold(name, "Origin") {
			delete(request.Header, name)
		}
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	handler.ServeHTTP(w, request)
}

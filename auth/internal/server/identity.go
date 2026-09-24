package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ikigenba/ikigenba/auth/internal/store"
)

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	identity, bearer, err := s.identity(r, true)
	if err != nil {
		s.writeIdentityError(w, r, bearer, err)
		return
	}

	w.Header().Set(HeaderUserID, identity.UserID)
	w.Header().Set(HeaderUserEmail, identity.Email)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	identity, bearer, err := s.identity(r, false)
	if err != nil {
		s.writeIdentityError(w, r, bearer, err)
		return
	}

	body, err := json.Marshal(struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}{ID: identity.UserID, Email: identity.Email})
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) identity(r *http.Request, touch bool) (store.Identity, bool, error) {
	if authorization := r.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
		secret := strings.TrimPrefix(authorization, "Bearer ")
		if touch {
			identity, err := s.st.TouchTokenIdentity(secret, s.now())
			return identity, true, err
		}
		identity, err := s.st.LookupTokenIdentity(secret, s.now())
		return identity, true, err
	}

	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return store.Identity{}, false, store.ErrNotFound
	}
	if touch {
		identity, err := s.st.TouchSession(cookie.Value, s.now())
		return identity, false, err
	}
	identity, err := s.st.LookupSessionIdentity(cookie.Value, s.now())
	return identity, false, err
}

func (s *Server) writeIdentityError(w http.ResponseWriter, r *http.Request, bearer bool, err error) {
	if !errors.Is(err, store.ErrNotFound) {
		s.writeServerError(w, r, err)
		return
	}
	if bearer {
		writePlainError(w, http.StatusForbidden, "token refused")
		return
	}
	writePlainError(w, http.StatusUnauthorized, "sign in required")
}

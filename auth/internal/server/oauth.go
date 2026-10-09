package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/store"
)

// DefaultClientName is the display name for a registration without a name.
const DefaultClientName string = "MCP client"

func (s *Server) mcpOrigin(r *http.Request) string {
	scheme, authority, _ := strings.Cut(ownOrigin(r.Host, s.cfg.PublicURL), "://")
	return scheme + "://mcp." + strings.TrimPrefix(authority, "auth.")
}

func (s *Server) acceptableResource(r *http.Request, value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.IsAbs() && u.User == nil && u.Fragment == "" && !strings.Contains(value, "#") && asciiEqualFold(u.Scheme+"://"+u.Host, s.mcpOrigin(r))
}

func acceptableRedirect(value string) bool {
	if len(value) > 2048 || strings.Contains(value, "#") {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil {
		return false
	}
	return strings.HasPrefix(value, "https://") && u.Hostname() != "" || strings.HasPrefix(value, "http://") && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")
}

func loopbackParts(value string) (host, portless string) {
	for _, h := range []string{"localhost", "127.0.0.1"} {
		prefix := "http://" + h
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		tail := value[len(prefix):]
		if tail != "" && !strings.ContainsRune(":/?", rune(tail[0])) {
			continue
		}
		if strings.HasPrefix(tail, ":") {
			i := 1
			for i < len(tail) && tail[i] >= '0' && tail[i] <= '9' {
				i++
			}
			tail = tail[i:]
		}
		return h, prefix + tail
	}
	return "", ""
}

func resolvedRedirect(client store.Client, requested string) string {
	if requested == "" {
		if len(client.RedirectURIs) == 1 {
			return client.RedirectURIs[0]
		}
		return ""
	}
	for _, registered := range client.RedirectURIs {
		if requested == registered {
			return requested
		}
		ah, ap := loopbackParts(requested)
		bh, bp := loopbackParts(registered)
		if ah != "" && ah == bh && ap == bp {
			return requested
		}
	}
	return ""
}

func oauthParameters(r *http.Request) url.Values {
	if r.Method == http.MethodGet {
		return r.URL.Query()
	}
	_ = r.ParseForm()
	return r.PostForm
}

func oauthRedirect(w http.ResponseWriter, params url.Values, uri, key, value string) {
	separator := "?"
	if strings.Contains(uri, "?") {
		separator = "&"
	}
	location := uri + separator + key + "=" + value
	if state := params.Get("state"); state != "" {
		location += "&state=" + url.QueryEscape(state)
	}
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusFound)
}

func oauthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func oauthError(w http.ResponseWriter, code string) {
	oauthJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": "OAuth request refused: " + code})
}

func (s *Server) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
	origin := ownOrigin(r.Host, s.cfg.PublicURL)
	oauthJSON(w, http.StatusOK, map[string]any{"issuer": origin, "authorization_endpoint": origin + "/authorize", "token_endpoint": origin + "/token", "registration_endpoint": origin + "/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	if err != nil || len(body) > 65536 || !json.Valid(body) || !strings.HasPrefix(strings.TrimLeft(string(body), " \t\r\n"), "{") {
		oauthError(w, "invalid_client_metadata")
		return
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(body, &object)
	name := DefaultClientName
	if raw, ok := object["client_name"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &name) != nil || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 200 || strings.ContainsRune(name, 0) {
			oauthError(w, "invalid_client_metadata")
			return
		}
	}
	var rawURIs []json.RawMessage
	raw, present := object["redirect_uris"]
	arrayOK := present && json.Unmarshal(raw, &rawURIs) == nil && len(raw) > 0 && raw[0] == '['
	if arrayOK {
		for _, rawURI := range rawURIs {
			var uri string
			if json.Unmarshal(rawURI, &uri) == nil && strings.ContainsRune(uri, 0) {
				oauthError(w, "invalid_client_metadata")
				return
			}
		}
	}
	if !arrayOK || len(rawURIs) < 1 || len(rawURIs) > 10 {
		oauthError(w, "invalid_redirect_uri")
		return
	}
	uris := make([]string, len(rawURIs))
	for i, rawURI := range rawURIs {
		if string(rawURI) == "null" || json.Unmarshal(rawURI, &uris[i]) != nil || !acceptableRedirect(uris[i]) {
			oauthError(w, "invalid_redirect_uri")
			return
		}
	}
	client, err := s.st.RegisterClient(name, uris, s.now())
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	s.record(r, "client.registered", "", telemetry.Attrs{"client": client.ID})
	w.Header().Set("Cache-Control", "no-store")
	oauthJSON(w, http.StatusCreated, map[string]any{"client_id": client.ID, "client_id_issued_at": client.CreatedAt.Unix(), "client_name": client.Name, "redirect_uris": client.RedirectURIs, "grant_types": []string{"authorization_code"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"})
}

func (s *Server) authorizationFault(r *http.Request, p url.Values) string {
	for name, values := range p {
		if strings.ContainsRune(name, 0) {
			return "invalid_request"
		}
		for _, value := range values {
			if strings.ContainsRune(value, 0) {
				return "invalid_request"
			}
		}
	}
	if p.Get("response_type") == "" {
		return "invalid_request"
	}
	if p.Get("response_type") != "code" {
		return "unsupported_response_type"
	}
	if p.Get("code_challenge") == "" || p.Get("code_challenge_method") != "S256" {
		return "invalid_request"
	}
	if p.Get("resource") != "" && !s.acceptableResource(r, p.Get("resource")) {
		return "invalid_target"
	}
	return ""
}

type approveData struct {
	Banner                                                                                    page.Banner
	ClientName, Gateway, Until, ReturnHost, ClientID, RedirectURI, Challenge, State, Resource string
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.Header.Get("Origin") != ownOrigin(r.Host, s.cfg.PublicURL) {
		writePlainError(w, http.StatusForbidden, "origin refused")
		return
	}
	p := oauthParameters(r)
	if p.Get("client_id") == "" {
		writePlainError(w, http.StatusBadRequest, "client refused")
		return
	}
	now := s.now()
	client, err := s.st.LookupClient(p.Get("client_id"), now)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writePlainError(w, http.StatusBadRequest, "client refused")
		} else {
			s.writeServerError(w, r, err)
		}
		return
	}
	uri := resolvedRedirect(client, p.Get("redirect_uri"))
	if uri == "" {
		writePlainError(w, http.StatusBadRequest, "redirect URI refused")
		return
	}
	if fault := s.authorizationFault(r, p); fault != "" {
		oauthRedirect(w, p, uri, "error", fault)
		return
	}
	user, err := s.tokenSessionIdentity(r)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.writeServerError(w, r, err)
			return
		}
		query := r.URL.RawQuery
		if r.Method == http.MethodPost {
			p.Del("decision")
			query = p.Encode()
		}
		w.Header().Set("Location", "/?return="+url.QueryEscape(ownOrigin(r.Host, s.cfg.PublicURL)+"/authorize?"+query))
		w.WriteHeader(http.StatusFound)
		return
	}
	resource := p.Get("resource")
	if resource == "" {
		resource = s.mcpOrigin(r) + "/mcp"
	}
	if r.Method == http.MethodGet {
		_, authority, _ := strings.Cut(uri, "//")
		authority = strings.SplitN(authority, "/", 2)[0]
		authority = strings.SplitN(authority, "?", 2)[0]
		authority = strings.SplitN(authority, "#", 2)[0]
		_, gateway, _ := strings.Cut(s.mcpOrigin(r), "://")
		data := approveData{Banner: s.pageBanner(user.Email), ClientName: client.Name, Gateway: gateway, Until: now.Add(store.ClientTokenTTL).UTC().Format("2006-01-02"), ReturnHost: authority, ClientID: p.Get("client_id"), RedirectURI: uri, Challenge: p.Get("code_challenge"), State: p.Get("state"), Resource: resource}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = authTemplates.ExecuteTemplate(w, "approve", data)
		return
	}
	switch p.Get("decision") {
	case "deny":
		s.record(r, "client.denied", user.UserID, telemetry.Attrs{"client": client.ID})
		oauthRedirect(w, p, uri, "error", "access_denied")
	case "approve":
		code, err := s.st.CreateAuthCode(client.ID, user.UserID, uri, p.Get("code_challenge"), resource, now)
		if err != nil {
			s.writeServerError(w, r, err)
			return
		}
		s.record(r, "client.approved", user.UserID, telemetry.Attrs{"client": client.ID})
		oauthRedirect(w, p, uri, "code", code.Code)
	default:
		writePlainError(w, http.StatusBadRequest, "decision refused")
	}
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	p := oauthParameters(r)
	now := s.now()
	var code store.AuthCode
	var err error
	if p.Get("code") != "" {
		code, err = s.st.ConsumeAuthCode(p.Get("code"), now)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.writeServerError(w, r, err)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if grant := p.Get("grant_type"); grant != "" && grant != "authorization_code" {
		oauthError(w, "unsupported_grant_type")
		return
	}
	for _, key := range []string{"grant_type", "code", "redirect_uri", "client_id", "code_verifier"} {
		if p.Get(key) == "" {
			oauthError(w, "invalid_request")
			return
		}
	}
	digest := sha256.Sum256([]byte(p.Get("code_verifier")))
	if err != nil || code.ClientID != p.Get("client_id") || code.RedirectURI != p.Get("redirect_uri") || code.Challenge != base64.RawURLEncoding.EncodeToString(digest[:]) {
		oauthError(w, "invalid_grant")
		return
	}
	if resource := p.Get("resource"); resource != "" && !s.acceptableResource(r, resource) {
		oauthError(w, "invalid_target")
		return
	}
	if _, err = s.st.LookupClient(code.ClientID, now); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			oauthError(w, "invalid_grant")
		} else {
			s.writeServerError(w, r, err)
		}
		return
	}
	_, authority, _ := strings.Cut(s.mcpOrigin(r), "://")
	token, secret, err := s.st.CreateClientToken(code, stripNumericPort(authority), now)
	if err != nil {
		s.writeServerError(w, r, err)
		return
	}
	s.record(r, "token.minted", code.UserID, telemetry.Attrs{"token": token.ID})
	oauthJSON(w, http.StatusOK, map[string]any{"access_token": secret, "token_type": "Bearer", "expires_in": int64(store.ClientTokenTTL.Seconds())})
}

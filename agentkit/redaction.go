package agentkit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// RedactionMark replaces credential secrets in diagnostics.
const RedactionMark string = "[REDACTED]"

type requestSecretKey struct{}

type secretSet struct {
	mu     sync.Mutex
	values []string
}

func (s *secretSet) remember(secret string) {
	if len(secret) < 16 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, value := range s.values {
		if value == secret {
			return
		}
	}
	s.values = append(s.values, secret)
}

func (s *secretSet) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.values...)
}

func (s *secretSet) rememberOAuth(body []byte) {
	for _, value := range oauthSecrets(body) {
		s.remember(value)
	}
}

// Reject provider-derived URL components before any request can disclose a
// credential. A failed cache deletion leaves its lifecycle open for inspection.
func requestURLSafety(req *http.Request, secrets []string) error {
	decodedURL, _ := url.QueryUnescape(req.URL.String())
	for _, secret := range secrets {
		if strings.Contains(req.URL.String(), secret) || strings.Contains(req.URL.Path, secret) || strings.Contains(decodedURL, secret) {
			return &Error{Category: CategoryInvalidRequest, Message: "provider resource URL contains a credential"}
		}
	}
	return nil
}

// Clone client configuration so its redirect policy and transport remain the
// consumer's, while validating the URL after that policy has accepted it.
func credentialSafeDo(client *http.Client, request *http.Request) (*http.Response, error) {
	if err := requestURLSafety(request, requestSecrets(request)); err != nil {
		return nil, err
	}
	guarded := *client
	guarded.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if client.CheckRedirect != nil {
			if err := client.CheckRedirect(next, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return requestURLSafety(next, requestSecrets(request))
	}
	return guarded.Do(request)
}

func rememberRequestSecrets(req *http.Request, secrets *secretSet) {
	*req = *req.WithContext(context.WithValue(req.Context(), requestSecretKey{}, secrets))
}

func requestSecrets(req *http.Request) []string {
	if req == nil {
		return nil
	}
	secrets, _ := req.Context().Value(requestSecretKey{}).(*secretSet)
	if secrets == nil {
		return nil
	}
	return secrets.snapshot()
}

func oauthSecrets(body []byte) []string {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	var secrets []string
	for _, key := range []string{"access_token", "refresh_token"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && len(value) >= 16 {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func redactText(text string, secrets []string) string {
	ordered := append([]string(nil), secrets...)
	sort.SliceStable(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	var pairs []string
	for _, secret := range ordered {
		if len(secret) >= 16 {
			pairs = append(pairs, secret, RedactionMark)
		}
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

type redactedCause struct {
	text   string
	causes []error
	source error
}

func (e *redactedCause) Error() string   { return e.text }
func (e *redactedCause) Unwrap() []error { return e.causes }
func (e *redactedCause) Is(target error) bool {
	return errors.Is(e.source, target)
}

// Rebuild the reachable chain rather than wrapping an unsafe original cause.
// Unchanged leaves (including context sentinels) retain their identity.
func redactError(err error, secrets []string) error {
	if err == nil || len(secrets) == 0 {
		return err
	}
	if e, ok := any(err).(*Error); ok {
		cloned := *e
		cloned.Code = redactText(e.Code, secrets)
		cloned.Message = redactText(e.Message, secrets)
		cloned.Endpoint.Endpoint = redactText(e.Endpoint.Endpoint, secrets)
		cloned.Endpoint.AuthMode = redactText(e.Endpoint.AuthMode, secrets)
		cloned.Endpoint.Model = redactText(e.Endpoint.Model, secrets)
		cloned.err = redactError(e.err, secrets)
		return &cloned
	}
	var causes []error
	switch e := any(err).(type) {
	case interface{ Unwrap() []error }:
		for _, cause := range e.Unwrap() {
			causes = append(causes, redactError(cause, secrets))
		}
	case interface{ Unwrap() error }:
		causes = append(causes, redactError(e.Unwrap(), secrets))
	}
	text := redactText(err.Error(), secrets)
	if len(causes) == 0 && text == err.Error() {
		return err
	}
	return &redactedCause{text: text, causes: causes, source: err}
}

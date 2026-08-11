package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"net/http"
	"strconv"
	"time"

	"appkit/telemetry"

	"eventplane/correlation"
)

// Identity is the authenticated caller, as told to us authoritatively by nginx.
// OwnerID is the stable key used for scoping, lookups, and authorization. The
// remaining owner fields are display data and must never be used as identity
// keys.
type Identity struct {
	OwnerID      string
	OwnerEmail   string
	OwnerName    string
	OwnerPicture string
	ClientID     string
}

type identityCtxKey struct{}
type recordIdentityCtxKey struct{}

type recordIdentity struct {
	id Identity
	ok bool
}

// withIdentity stashes the caller identity on the request context.
func withIdentity(ctx context.Context, id Identity) context.Context {
	if captured, ok := ctx.Value(recordIdentityCtxKey{}).(*recordIdentity); ok {
		captured.id = id
		captured.ok = true
	}
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// IdentityFrom returns the caller identity on the context, and whether one was
// present. Handlers behind RequireIdentity always get ok == true.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(identityCtxKey{}).(Identity)
	return id, ok
}

type telemetryResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
	hash   hash.Hash
	wrote  bool
}

func (w *telemetryResponseWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *telemetryResponseWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.bytes += n
		_, _ = w.hash.Write(p[:n])
	}
	return n, err
}

func (w *telemetryResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *telemetryResponseWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func recorderContext(recorder *telemetry.Recorder, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(telemetry.WithRecorder(r.Context(), recorder)))
	})
}

func recordMiddleware(recorder *telemetry.Recorder, service string, excluded []string, next http.Handler) http.Handler {
	exclude := make(map[string]struct{}, len(excluded))
	for _, path := range excluded {
		exclude[path] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if recorder == nil || (r.Method == http.MethodPost && r.URL.Path == "/mcp") {
			next.ServeHTTP(w, r)
			return
		}
		if excludedRequest(exclude, r) {
			next.ServeHTTP(w, r)
			return
		}

		query, _ := json.Marshal(r.URL.Query())
		captured := &recordIdentity{}
		r = r.WithContext(context.WithValue(r.Context(), recordIdentityCtxKey{}, captured))
		rw := &telemetryResponseWriter{ResponseWriter: w, status: http.StatusOK, hash: sha256.New()}
		started := time.Now()
		next.ServeHTTP(rw, r)
		// ServeMux assigns Request.Pattern while dispatching. Checking again after
		// the handler lets callers exclude method-aware route templates such as
		// "GET /completions/{id}" without also suppressing POST on a sibling route.
		if excludedRequest(exclude, r) {
			return
		}

		var actor *telemetry.Actor
		id, ok := IdentityFrom(r.Context())
		if !ok && captured.ok {
			id, ok = captured.id, true
		}
		if !ok {
			id = Identity{
				OwnerEmail: r.Header.Get("X-Owner-Email"),
				ClientID:   r.Header.Get("X-Client-Id"),
			}
			ok = id.OwnerEmail != "" || id.ClientID != ""
		}
		if ok && (id.OwnerEmail != "" || id.ClientID != "") {
			actor = &telemetry.Actor{OwnerEmail: id.OwnerEmail, ClientID: id.ClientID}
		}
		recorder.Record(telemetry.Record{
			CorrelationID: correlation.FromContext(r.Context()),
			Service:       service,
			Kind:          telemetry.KindRequest,
			Actor:         actor,
			Op:            r.Method + " " + r.URL.Path,
			Params:        telemetry.EncodeParams(query, nil),
			Outcome: &telemetry.Outcome{
				Status:     strconv.Itoa(rw.status),
				DurationMS: time.Since(started).Milliseconds(),
				Bytes:      rw.bytes,
				SHA256:     hex.EncodeToString(rw.hash.Sum(nil)),
			},
		})
	})
}

func excludedRequest(excluded map[string]struct{}, r *http.Request) bool {
	for _, candidate := range []string{r.URL.Path, r.Method + " " + r.URL.Path, r.Pattern} {
		if candidate == "" {
			continue
		}
		if _, ok := excluded[candidate]; ok {
			return true
		}
	}
	return false
}

// requireIdentityHeaders does NO token parsing, NO ValidateAccess, NO hashing —
// a suite service performs no token logic at all. nginx is the only ingress,
// the server binds 127.0.0.1, and nginx sets X-Owner-* / X-Client-Id
// authoritatively only AFTER a successful auth_request against the dashboard
// (clearing any inbound spoof first). So an empty X-Owner-Id means the
// request did not come through the authenticated front door (or nginx is
// misconfigured) — we refuse to serve it (PLAN §2.9).
//
// We trust X-Owner-* / X-Client-Id precisely because that loopback bind plus
// nginx-only ingress is the entire security boundary; binding a public interface
// would let anyone spoof these headers, which is why the server binds 127.0.0.1.
func (a *appHandler) requireIdentityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.Header.Get("X-Owner-Id")
		if ownerID == "" {
			// No authenticated identity: this request did not transit nginx's
			// auth_request gate. The MCP challenge points clients at our PRM doc.
			w.Header().Set("WWW-Authenticate",
				`Bearer resource_metadata="`+a.resourceID+`/.well-known/oauth-protected-resource"`)
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":             "unauthorized",
				"error_description": "missing authenticated identity",
			})
			return
		}
		id := Identity{
			OwnerID:      ownerID,
			OwnerEmail:   r.Header.Get("X-Owner-Email"),
			OwnerName:    r.Header.Get("X-Owner-Name"),
			OwnerPicture: r.Header.Get("X-Owner-Picture"),
			ClientID:     r.Header.Get("X-Client-Id"),
		}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), id)))
	})
}

// securityHeaders sets transport-hardening headers that don't depend on auth:
// nosniff and no-store on every response, and HSTS only when the request arrived
// over HTTPS (signalled by nginx's X-Forwarded-Proto).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if isHTTPS(r) {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// isHTTPS reports whether the request reached the front door over TLS. nginx
// terminates TLS and forwards the original scheme via X-Forwarded-Proto; on
// plain localhost there is no TLS and no header, so HSTS is correctly omitted.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}

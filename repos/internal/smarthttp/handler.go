// Package smarthttp streams the host git's smart HTTP protocol.
package smarthttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	"github.com/ikigenba/ikigenba/repos/internal/limits"
	"github.com/ikigenba/ikigenba/repos/internal/store"
)

// Config supplies the repositories, git, shared limits and event writer.
type Config struct {
	Store     *store.Store
	Git       *git.Git
	Limits    *limits.Limits
	Telemetry *telemetry.Writer
}

// Handler serves smart routes after resolving the caller's repository.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cfg.serve(w, r) })
}

type route struct {
	name, rest, service string
	op                  limits.Op
	push                bool
}

func classify(r *http.Request) route {
	segment, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	q := route{name: strings.TrimSuffix(segment, ".git")}
	if len(segment)+1 < len(r.URL.Path) {
		q.rest = "/" + rest
	}
	if r.Method == http.MethodGet && q.rest == "/info/refs" {
		values, err := url.ParseQuery(r.URL.RawQuery)
		if err == nil && len(values["service"]) == 1 {
			q.service = values["service"][0]
		}
	} else if r.Method == http.MethodPost && (q.rest == "/git-upload-pack" || q.rest == "/git-receive-pack") {
		q.service = q.rest[1:]
	}
	switch q.service {
	case "git-upload-pack":
		q.op = limits.Fetch
	case "git-receive-pack":
		q.op = limits.Push
		q.push = r.Method == http.MethodPost
	default:
		q.service = ""
	}
	return q
}

func answer(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = fmt.Fprintln(w, message)
	}
}

func storeAnswer(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		answer(w, r, 404, "repository not found")
	} else {
		answer(w, r, 500, "cannot reach the repositories; try again later")
	}
}

func (cfg Config) serve(w http.ResponseWriter, r *http.Request) {
	q := classify(r)
	if !store.ValidName(q.name) {
		storeAnswer(w, r, store.ErrNotFound)
		return
	}
	caller, _ := identity.FromContext(r.Context())
	repo, err := cfg.Store.Find(r.Context(), caller.UserID, q.name)
	if err != nil {
		storeAnswer(w, r, err)
		return
	}
	if !repo.Available {
		answer(w, r, 503, "repository unavailable")
		return
	}
	if q.service == "" {
		answer(w, r, 404, "not found")
		return
	}
	if q.op == limits.Push {
		size, sizeErr := cfg.Store.Size(r.Context(), repo.ID)
		if sizeErr != nil {
			storeAnswer(w, r, sizeErr)
			return
		}
		if size >= cfg.Limits.Settings().RepoMaxBytes {
			cfg.operation(r.Context(), "operation.rejected", repo.ID, q.op, "repo_max_bytes", 0)
			answer(w, r, 507, fmt.Sprintf("repository is at its size limit of %d bytes", cfg.Limits.Settings().RepoMaxBytes))
			return
		}
	}
	grant, err := cfg.Limits.Acquire(r.Context(), repo.ID, q.op, q.push)
	if err != nil {
		limit, message := "", "too many git operations; try again later"
		switch {
		case errors.Is(err, limits.ErrQueueFull):
			limit = "queue_length"
		case errors.Is(err, limits.ErrQueueTimeout):
			limit = "queue_seconds"
		case errors.Is(err, limits.ErrDraining):
			limit = "draining"
			message = "repos is stopping; try again later"
		default:
			return
		}
		cfg.operation(r.Context(), "operation.rejected", repo.ID, q.op, limit, 0)
		w.Header().Set("Retry-After", strconv.FormatInt(cfg.Limits.Settings().QueueSeconds, 10))
		answer(w, r, 503, message)
		return
	}
	defer grant.Release()
	repo, err = cfg.Store.Find(r.Context(), caller.UserID, repo.ID)
	if err != nil {
		storeAnswer(w, r, err)
		return
	}
	cfg.stream(w, r, q, repo.ID, grant)
}

func (cfg Config) operation(ctx context.Context, event, id string, op limits.Op, limit string, waited time.Duration) {
	a := telemetry.Attrs{"repo": id, "operation": string(op)}
	if event == "operation.waited" {
		a["wait_us"] = int64(waited / time.Microsecond)
	} else {
		a["limit"] = limit
	}
	cfg.Telemetry.Emit(ctx, event, a)
}

func (cfg Config) environment(r *http.Request, q route, id string) []string {
	caller, _ := identity.FromContext(r.Context())
	query := ""
	if r.Method == http.MethodGet {
		query = "service=" + q.service
	}
	return []string{
		"GIT_PROJECT_ROOT=" + filepath.Dir(cfg.Store.Dir(id)), "PATH_INFO=/" + id + ".git" + q.rest,
		"REQUEST_METHOD=" + r.Method, "QUERY_STRING=" + query, "CONTENT_TYPE=" + r.Header.Get("Content-Type"),
		"REMOTE_USER=" + caller.UserID, "GIT_HTTP_EXPORT_ALL=1", "HTTP_GIT_PROTOCOL=" + r.Header.Get("Git-Protocol"),
		"HTTP_CONTENT_ENCODING=" + r.Header.Get("Content-Encoding"), "LC_ALL=C",
	}
}

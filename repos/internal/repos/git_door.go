package repos

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"eventplane/outbox"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"
)

const gitBasicChallenge = `Basic realm="repos"`

// GitDoorHandler serves live repositories using git's smart-HTTP CGI backend.
func GitDoorHandler(service *Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		service.serveGit(w, request)
	})
}

func (s *Service) serveGit(w http.ResponseWriter, request *http.Request) {
	kind, name, rest, ok := parseGitDoorPath(request.URL.Path)
	if !ok {
		http.NotFound(w, request)
		return
	}
	repository, err := s.repositoryPath(kind, name)
	if err != nil {
		if errors.Is(err, ErrValidation) || errors.Is(err, ErrNotFound) {
			http.NotFound(w, request)
			return
		}
		http.Error(w, "git repository unavailable", http.StatusInternalServerError)
		return
	}

	actor, scope, authenticated, forbidden := s.gitDoorIdentity(request.Context(), request, kind, name)
	if forbidden {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !authenticated {
		if request.Header.Get("X-Forwarded-Proto") == "" {
			w.Header().Set("WWW-Authenticate", gitBasicChallenge)
		} else {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata=""`)
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	isReceivePack := request.Method == http.MethodPost && rest == "git-receive-pack"
	var before map[string]string
	if isReceivePack {
		before, err = s.custody.Refs(request.Context(), kind, name)
		if err != nil {
			http.Error(w, "git repository unavailable", http.StatusInternalServerError)
			return
		}
	}

	var cgi bytes.Buffer
	env := gitCGIEnvironment(request, s.custody.root, kind, name, rest, actor, scope)
	if err := s.custody.git.RunIn(request.Context(), repository, env, request.Body, &cgi, "http-backend"); err != nil {
		http.Error(w, "git backend failed", http.StatusInternalServerError)
		return
	}
	if isReceivePack {
		after, refsErr := s.custody.Refs(request.Context(), kind, name)
		if refsErr != nil {
			http.Error(w, "git repository unavailable", http.StatusInternalServerError)
			return
		}
		if err := s.appendGitPushEvents(request.Context(), kind, name, actor, before, after); err != nil {
			http.Error(w, "record push events", http.StatusInternalServerError)
			return
		}
	}
	if err := writeCGIResponse(w, &cgi); err != nil {
		http.Error(w, "invalid git backend response", http.StatusInternalServerError)
	}
}

func parseGitDoorPath(path string) (kind, name, rest string, ok bool) {
	if !strings.HasPrefix(path, "/git/") {
		return "", "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(path, "/git/"), "/", 3)
	if len(parts) != 3 || !strings.HasSuffix(parts[1], ".git") || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), parts[2], true
}

func (s *Service) gitDoorIdentity(ctx context.Context, request *http.Request, kind, name string) (actor, scope string, ok, forbidden bool) {
	if request.Header.Get("X-Forwarded-Proto") == "" {
		_, password, present := request.BasicAuth()
		if !present || password == "" || s.store == nil || s.custody == nil {
			return "", "", false, false
		}
		digest := sha256.Sum256([]byte(password))
		token, err := s.store.LookupRunToken(ctx, hex.EncodeToString(digest[:]))
		if errors.Is(err, sql.ErrNoRows) || err != nil || !token.ExpiresAt.After(s.custody.Now()) {
			return "", "", false, false
		}
		if token.Kind != kind || token.Name != name {
			return "", "", false, true
		}
		return "run:" + kind + "/" + name, "run", true, false
	}
	if request.Header.Get("X-Owner-Id") == "" {
		return "", "", false, false
	}
	return request.Header.Get("X-Owner-Email"), "owner", true, false
}

func gitCGIEnvironment(request *http.Request, root, kind, name, rest, actor, scope string) []string {
	contentLength := request.Header.Get("Content-Length")
	if request.ContentLength >= 0 {
		contentLength = strconv.FormatInt(request.ContentLength, 10)
	}
	return []string{
		"GIT_PROJECT_ROOT=" + filepath.Join(root, "git"),
		"GIT_HTTP_EXPORT_ALL=1",
		"PATH_INFO=/" + kind + "/" + name + ".git/" + rest,
		"REQUEST_METHOD=" + request.Method,
		"QUERY_STRING=" + request.URL.RawQuery,
		"CONTENT_TYPE=" + request.Header.Get("Content-Type"),
		"CONTENT_LENGTH=" + contentLength,
		"HTTP_CONTENT_ENCODING=" + request.Header.Get("Content-Encoding"),
		"REMOTE_USER=" + actor,
		"REPOS_PUSH_SCOPE=" + scope,
		"GIT_COMMITTER_NAME=" + actor,
		"GIT_COMMITTER_EMAIL=" + actor,
		"GIT_AUTHOR_NAME=" + actor,
		"GIT_AUTHOR_EMAIL=" + actor,
	}
}

func (s *Service) appendGitPushEvents(ctx context.Context, kind, name, actor string, before, after map[string]string) error {
	if s.store == nil || s.producer == nil {
		return fmt.Errorf("repos: push event dependencies are not configured")
	}
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for ref, sha := range after {
		oldSHA := before[ref]
		if !strings.HasPrefix(ref, "refs/heads/") || sha == oldSHA {
			continue
		}
		payload, err := json.Marshal(PushPayload{Kind: kind, Name: name, Branch: strings.TrimPrefix(ref, "refs/heads/"), SHA: sha, OldSHA: oldSHA, Actor: actor})
		if err != nil {
			return err
		}
		if err := s.producer.Append(ctx, tx, outbox.Event{Kind: "push", Subject: "/" + kind + "/" + name, Payload: payload}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func writeCGIResponse(w http.ResponseWriter, response io.Reader) error {
	reader := bufio.NewReader(response)
	headers, err := textproto.NewReader(reader).ReadMIMEHeader()
	if err != nil {
		return err
	}
	status := http.StatusOK
	if value := headers.Get("Status"); value != "" {
		code, parseErr := strconv.Atoi(strings.Fields(value)[0])
		if parseErr != nil {
			return parseErr
		}
		status = code
		headers.Del("Status")
	}
	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(status)
	_, err = io.Copy(w, reader)
	return err
}

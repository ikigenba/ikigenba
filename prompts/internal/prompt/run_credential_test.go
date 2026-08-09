package prompt

import (
	"context"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"prompts/internal/sandbox"
	"prompts/internal/version"
)

const (
	testGitUsername = "run-user"
	testGitToken    = "run-token-secret"
)

type runTokenRequest struct {
	nameKey string
	runID   string
	ttl     time.Duration
}

type authenticatedGitPlane struct {
	*gitVersionPlane
	cloneBase string

	mu             sync.Mutex
	tokenCalls     []runTokenRequest
	authorization  []string
	omitCredential bool
}

func (p *authenticatedGitPlane) RunToken(_ context.Context, nameKey, runID string, ttl time.Duration) (version.Credential, error) {
	p.mu.Lock()
	p.tokenCalls = append(p.tokenCalls, runTokenRequest{nameKey: nameKey, runID: runID, ttl: ttl})
	omit := p.omitCredential
	p.mu.Unlock()
	credential := version.Credential{CloneURL: p.cloneBase + "/" + nameKey + ".git"}
	if !omit {
		credential.Username = testGitUsername
		credential.Password = testGitToken
	}
	return credential, nil
}

func (p *authenticatedGitPlane) recordAuthorization(value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authorization = append(p.authorization, value)
}

func (p *authenticatedGitPlane) calls() []runTokenRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]runTokenRequest(nil), p.tokenCalls...)
}

func (p *authenticatedGitPlane) headers() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.authorization...)
}

func newAuthenticatedGitWorkspaceService(t *testing.T) (*Service, *sandbox.Manager, *authenticatedGitPlane, Prompt) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test")
	base := newGitVersionPlane(t)
	backendPath := filepath.Join(strings.TrimSpace(gitTest(t, "--exec-path")), "git-http-backend")
	if _, err := os.Stat(backendPath); err != nil {
		t.Fatalf("git http-backend: %v", err)
	}
	backend := &cgi.Handler{
		Path: backendPath,
		Env: []string{
			"GIT_PROJECT_ROOT=" + base.root,
			"GIT_HTTP_EXPORT_ALL=1",
		},
	}
	plane := &authenticatedGitPlane{gitVersionPlane: base}
	wantAuthorization := credentialExtraHeader(version.Credential{Username: testGitUsername, Password: testGitToken})
	door := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		plane.recordAuthorization(header)
		if header != strings.TrimPrefix(wantAuthorization, "Authorization: ") {
			w.Header().Set("WWW-Authenticate", `Basic realm="runs"`)
			http.Error(w, "run credential required", http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(door.Close)
	plane.cloneBase = door.URL

	store := newTestStore(t)
	runsDir := filepath.Join(t.TempDir(), "state", "runs")
	sb, err := sandbox.New(runsDir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, sb, runsDir, &fakeRunner{})
	svc.Version = plane
	svc.RunTTL = 30 * time.Minute
	prompt, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{
		Name: "Authenticated workspace", UserPrompt: "initial", Config: validConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := plane.repo(prompt.NameKey)
	gitTest(t, "--git-dir", repo, "config", "http.receivepack", "true")
	return svc, sb, plane, prompt
}

func TestRunCredentialIsLocalConfigAndMintedForEveryRun(t *testing.T) {
	// R-S49H-XMZB
	// R-S6PA-P6GP
	svc, sb, plane, prompt := newAuthenticatedGitWorkspaceService(t)
	first, err := svc.Run(t.Context(), ownerA, prompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Run(t.Context(), ownerA, prompt.ID)
	if err != nil {
		t.Fatal(err)
	}

	cloneURL := plane.cloneBase + "/" + prompt.NameKey + ".git"
	wantHeader := credentialExtraHeader(version.Credential{Username: testGitUsername, Password: testGitToken})
	for _, run := range []Run{first, second} {
		root := sb.Root(run.ID)
		if got := gitTest(t, "-C", root, "config", "--get", "http."+cloneURL+".extraHeader"); got != wantHeader {
			t.Fatalf("run %s extraHeader = %q, want %q", run.ID, got, wantHeader)
		}
		remote := gitTest(t, "-C", root, "config", "--get", "remote.origin.url")
		if remote != cloneURL {
			t.Fatalf("run %s origin = %q, want %q", run.ID, remote, cloneURL)
		}
		if strings.Contains(remote, testGitUsername) || strings.Contains(remote, testGitToken) {
			t.Fatalf("run %s origin leaks credential: %q", run.ID, remote)
		}
	}
	calls := plane.calls()
	if len(calls) != 2 {
		t.Fatalf("RunToken calls = %+v, want one per run", calls)
	}
	if calls[0].runID != first.ID || calls[1].runID != second.ID || calls[0].runID == calls[1].runID {
		t.Fatalf("RunToken run ids = %q, %q; want %q, %q", calls[0].runID, calls[1].runID, first.ID, second.ID)
	}
	for _, call := range calls {
		if call.ttl < 35*time.Minute {
			t.Fatalf("RunToken ttl = %s, want at least 35m", call.ttl)
		}
	}
}

func TestRunCloneRequiresCredentialAndRemovesFailedWorkspace(t *testing.T) {
	// R-S5HE-BEQ0
	svc, sb, plane, prompt := newAuthenticatedGitWorkspaceService(t)
	if _, err := svc.Run(t.Context(), ownerA, prompt.ID); err != nil {
		t.Fatalf("authenticated run: %v", err)
	}
	headers := plane.headers()
	if len(headers) == 0 {
		t.Fatal("git door recorded no authenticated fetch requests")
	}
	want := strings.TrimPrefix(credentialExtraHeader(version.Credential{Username: testGitUsername, Password: testGitToken}), "Authorization: ")
	for _, header := range headers {
		if header != want {
			t.Fatalf("authenticated fetch Authorization = %q, want %q", header, want)
		}
	}

	plane.mu.Lock()
	plane.omitCredential = true
	plane.authorization = nil
	plane.mu.Unlock()
	if _, err := svc.Run(t.Context(), ownerA, prompt.ID); err == nil {
		t.Fatal("unauthenticated clone succeeded")
	}
	calls := plane.calls()
	failedRunID := calls[len(calls)-1].runID
	if _, err := os.Stat(sb.Root(failedRunID)); !os.IsNotExist(err) {
		t.Fatalf("failed run workspace stat error = %v, want not exist", err)
	}
	headers = plane.headers()
	if len(headers) == 0 {
		t.Fatal("git door recorded no unauthenticated clone attempt")
	}
	for _, header := range headers {
		if header != "" {
			t.Fatalf("unauthenticated clone sent Authorization %q", header)
		}
	}
}

func TestRunCredentialPushesRunBranchButDoorRejectsMain(t *testing.T) {
	// R-RZDW-EK0J
	svc, sb, plane, prompt := newAuthenticatedGitWorkspaceService(t)
	run, err := svc.Run(t.Context(), ownerA, prompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	repo := plane.repo(prompt.NameKey)
	mainBefore := gitTest(t, "--git-dir", repo, "rev-parse", "refs/heads/main")
	hook := filepath.Join(repo, "hooks", "pre-receive")
	hookBody := "#!/bin/sh\nwhile read old new ref; do\n  if [ \"$ref\" = refs/heads/main ]; then\n    echo 'main is protected' >&2\n    exit 1\n  fi\ndone\n"
	if err := os.WriteFile(hook, []byte(hookBody), 0o755); err != nil {
		t.Fatal(err)
	}

	root := sb.Root(run.ID)
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("run result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, "-C", root, "add", "result.txt")
	gitTest(t, "-C", root, "commit", "-m", "run result")
	commit := gitTest(t, "-C", root, "rev-parse", "HEAD")
	branch := "refs/heads/ikigenba/run-" + run.ID
	gitTest(t, "-C", root, "push", "origin", "HEAD:"+branch)
	if got := gitTest(t, "--git-dir", repo, "rev-parse", branch); got != commit {
		t.Fatalf("run branch sha = %s, want %s", got, commit)
	}

	cmd := exec.Command("git", "-C", root, "push", "origin", "HEAD:refs/heads/main")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("push to main succeeded: %s", output)
	} else if !strings.Contains(string(output), "main is protected") {
		t.Fatalf("push to main error = %v, output = %s", err, output)
	}
	if got := gitTest(t, "--git-dir", repo, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("main changed from %s to %s", mainBefore, got)
	}
}

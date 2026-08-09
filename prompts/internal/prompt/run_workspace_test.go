package prompt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"prompts/internal/sandbox"
	"prompts/internal/version"
)

type gitVersionPlane struct {
	t        *testing.T
	root     string
	archived bool
}

func newGitVersionPlane(t *testing.T) *gitVersionPlane {
	return &gitVersionPlane{t: t, root: t.TempDir()}
}

func (p *gitVersionPlane) repo(key string) string { return filepath.Join(p.root, key+".git") }

func gitTest(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (p *gitVersionPlane) Create(_ context.Context, key string, _ version.Owner, _ string) error {
	gitTest(p.t, "init", "--bare", p.repo(key))
	return nil
}

func (p *gitVersionPlane) Commit(_ context.Context, key string, files []version.File, message, actor string) (string, error) {
	work := p.t.TempDir()
	if _, err := os.Stat(filepath.Join(p.repo(key), "refs", "heads", "main")); os.IsNotExist(err) {
		gitTest(p.t, "init", "-b", "main", work)
		gitTest(p.t, "-C", work, "remote", "add", "origin", p.repo(key))
	} else {
		gitTest(p.t, "clone", "-b", "main", p.repo(key), work)
	}
	gitTest(p.t, "-C", work, "config", "user.name", actor)
	gitTest(p.t, "-C", work, "config", "user.email", "test@localhost")
	for _, file := range files {
		path := filepath.Join(work, file.Path)
		if file.Delete {
			_ = os.Remove(path)
			continue
		}
		if err := os.WriteFile(path, file.Data, 0o644); err != nil {
			p.t.Fatal(err)
		}
	}
	gitTest(p.t, "-C", work, "add", "-A")
	gitTest(p.t, "-C", work, "commit", "-m", message)
	gitTest(p.t, "-C", work, "push", "origin", "main")
	gitTest(p.t, "--git-dir", p.repo(key), "symbolic-ref", "HEAD", "refs/heads/main")
	return gitTest(p.t, "-C", work, "rev-parse", "HEAD"), nil
}

func (p *gitVersionPlane) Head(_ context.Context, key string) (string, error) {
	if p.archived {
		return "", version.ErrUnavailable
	}
	return gitTest(p.t, "--git-dir", p.repo(key), "rev-parse", "refs/heads/main"), nil
}

func (p *gitVersionPlane) Read(_ context.Context, key, ref string) (version.Definition, error) {
	if p.archived {
		return version.Definition{}, version.ErrUnavailable
	}
	read := func(path string, optional bool) ([]byte, error) {
		cmd := exec.Command("git", "--git-dir", p.repo(key), "show", ref+":"+path)
		out, err := cmd.Output()
		if err != nil && optional {
			return nil, nil
		}
		return out, err
	}
	user, err := read("prompt.md", false)
	if err != nil {
		return version.Definition{}, err
	}
	system, err := read("system.md", true)
	if err != nil {
		return version.Definition{}, err
	}
	cfg, err := read("config.json", false)
	if err != nil {
		return version.Definition{}, err
	}
	return version.Definition{UserPrompt: string(user), SystemPrompt: string(system), ConfigJSON: cfg}, nil
}

func (p *gitVersionPlane) Rename(context.Context, string, string, version.Owner, string) error {
	return nil
}
func (p *gitVersionPlane) Archive(context.Context, string, version.Owner, string) error {
	p.archived = true
	return nil
}
func (p *gitVersionPlane) RunToken(_ context.Context, key, _ string, _ time.Duration) (version.Credential, error) {
	if p.archived {
		return version.Credential{}, version.ErrUnavailable
	}
	return version.Credential{CloneURL: "file://" + p.repo(key)}, nil
}

func newGitWorkspaceService(t *testing.T) (*Service, *Store, *sandbox.Manager, *gitVersionPlane, string) {
	store := newTestStore(t)
	runsDir := filepath.Join(t.TempDir(), "state", "runs")
	sb, err := sandbox.New(runsDir)
	if err != nil {
		t.Fatal(err)
	}
	plane := newGitVersionPlane(t)
	svc := NewService(store, sb, runsDir, &fakeRunner{})
	svc.Version = plane
	return svc, store, sb, plane, runsDir
}

func TestRunWorkspacePinsHeadChecksOutRunBranchAndHidesGit(t *testing.T) {
	// R-RWY3-N0J5
	// R-RY60-0S9U
	// R-S1TP-63HX
	// R-S31L-JV8M
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, sb, _, runsDir := newGitWorkspaceService(t)
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Pinned", UserPrompt: "first", SystemPrompt: "system", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	manual, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := store.GetRun(t.Context(), manual.ID); err != nil || stored.DefinitionSha != manual.DefinitionSha {
		t.Fatalf("stored first run = %+v, %v", stored, err)
	}
	root := sb.Root(manual.ID)
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	if got := gitTest(t, "-C", root, "rev-parse", "HEAD"); got != manual.DefinitionSha {
		t.Fatalf("HEAD = %s, want %s", got, manual.DefinitionSha)
	}
	if got := gitTest(t, "-C", root, "rev-parse", "--abbrev-ref", "HEAD"); got != "ikigenba/run-"+manual.ID {
		t.Fatalf("branch = %s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "prompt.md")); string(got) != "first" {
		t.Fatalf("prompt.md = %q", got)
	}
	if entries, err := os.ReadDir(filepath.Join(runsDir, manual.ID)); err != nil || len(entries) != 1 || entries[0].Name() != "sandbox" {
		t.Fatalf("manual run files = %+v, %v", entries, err)
	}

	updated, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: p.Name, UserPrompt: "second", SystemPrompt: "system", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	event, err := svc.RunByEvent(t.Context(), updated.ID, "crm", "changed", "/x", "event-1", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.DefinitionSha == manual.DefinitionSha {
		t.Fatal("second run did not move to the new head")
	}
	if stored, _ := store.GetRun(t.Context(), manual.ID); stored.DefinitionSha != manual.DefinitionSha {
		t.Fatalf("first run sha changed: %+v", stored)
	}
	inputEntries, err := os.ReadDir(filepath.Join(runsDir, event.ID, "input"))
	if err != nil || len(inputEntries) != 1 || inputEntries[0].Name() != "event.json" {
		t.Fatalf("event input = %+v, %v", inputEntries, err)
	}
	if got, err := svc.RunExecuted(t.Context(), ownerA, event.ID); err != nil || got.UserPrompt != "second" {
		t.Fatalf("event definition = %+v, %v", got, err)
	}

	listed, err := sb.List(manual.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].Name != "config.json" || listed[1].Name != "prompt.md" || listed[2].Name != "system.md" {
		t.Fatalf("workspace listing = %+v", listed)
	}
	for _, entry := range listed {
		if entry.Name == ".git" {
			t.Fatal(".git exposed by list")
		}
	}
	if _, err := sb.Read(manual.ID, ".git/config", 0, 0); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf(".git read error = %v", err)
	}
	if got, err := sb.Read(manual.ID, "prompt.md", 0, 0); err != nil || got != "first\n" {
		t.Fatalf("prompt read = %q, %v", got, err)
	}
}

func TestRunExecutedReadsPinnedCommitAfterWorkspaceAndMainChange(t *testing.T) {
	// R-S0LS-SBR8
	// R-ZOYX-Y9WA
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, sb, _, _ := newGitWorkspaceService(t)
	original := Config{Provider: "anthropic", Model: testAnthropicModel, MaxTokens: 11}
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Mutable", UserPrompt: "original", Config: original})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sb.Root(run.ID), "prompt.md"), []byte("working tree overwrite"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sb.Root(run.ID), "config.json"), []byte(`{"model":"wrong"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	updatedConfig := original
	updatedConfig.MaxTokens = 99
	if _, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: p.Name, UserPrompt: "new main", Config: updatedConfig}); err != nil {
		t.Fatal(err)
	}
	frozen, err := svc.RunExecuted(t.Context(), ownerA, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	live, err := svc.LoadFromPrompt(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if frozen.UserPrompt != "original" || !reflect.DeepEqual(frozen.Config, original) {
		t.Fatalf("frozen = %+v", frozen)
	}
	if live.UserPrompt != "new main" || !reflect.DeepEqual(live.Config, updatedConfig) {
		t.Fatalf("live = %+v", live)
	}
}

type unavailableVersion struct{}

func (unavailableVersion) Create(context.Context, string, version.Owner, string) error {
	return version.ErrUnavailable
}
func (unavailableVersion) Commit(context.Context, string, []version.File, string, string) (string, error) {
	return "", version.ErrUnavailable
}
func (unavailableVersion) Head(context.Context, string) (string, error) {
	return "", version.ErrUnavailable
}
func (unavailableVersion) Read(context.Context, string, string) (version.Definition, error) {
	return version.Definition{}, version.ErrUnavailable
}
func (unavailableVersion) Rename(context.Context, string, string, version.Owner, string) error {
	return version.ErrUnavailable
}
func (unavailableVersion) Archive(context.Context, string, version.Owner, string) error {
	return version.ErrUnavailable
}
func (unavailableVersion) RunToken(context.Context, string, string, time.Duration) (version.Credential, error) {
	return version.Credential{}, version.ErrUnavailable
}

func TestDeletedPromptRunUsesOnlyRunRowAndLocalClone(t *testing.T) {
	// R-ZREQ-PTDO
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, _, plane, _ := newGitWorkspaceService(t)
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Gone", UserPrompt: "survives", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(t.Context(), ownerA, p.ID); err != nil {
		t.Fatal(err)
	}
	if !plane.archived {
		t.Fatal("repository was not archived")
	}
	svc.Version = unavailableVersion{}
	runs, err := svc.RunList(t.Context(), ownerA, p.ID)
	if err != nil || len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
	executed, err := svc.RunExecuted(t.Context(), ownerA, run.ID)
	if err != nil || executed.UserPrompt != "survives" || executed.Config.Model != validConfig().Model {
		t.Fatalf("executed = %+v, %v", executed, err)
	}
}

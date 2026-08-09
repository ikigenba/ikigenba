package prompt

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"prompts/internal/version"
)

type versionCall struct {
	op, key, to, message, actor, ref string
	files                            []version.File
}

type recordingVersion struct {
	calls       []versionCall
	definitions map[string]version.Definition
	commitErr   error
	renameErr   error
}

func newRecordingVersion() *recordingVersion {
	return &recordingVersion{definitions: make(map[string]version.Definition)}
}

func (f *recordingVersion) Create(_ context.Context, key, actor string) error {
	f.calls = append(f.calls, versionCall{op: "create", key: key, actor: actor})
	return nil
}

func (f *recordingVersion) Commit(_ context.Context, key string, files []version.File, message, actor string) (string, error) {
	copyFiles := append([]version.File(nil), files...)
	f.calls = append(f.calls, versionCall{op: "commit", key: key, files: copyFiles, message: message, actor: actor})
	if f.commitErr != nil {
		return "", f.commitErr
	}
	d := f.definitions[key]
	for _, file := range files {
		switch file.Path {
		case "prompt.md":
			d.UserPrompt = string(file.Data)
		case "system.md":
			if file.Delete {
				d.SystemPrompt = ""
			} else {
				d.SystemPrompt = string(file.Data)
			}
		case "config.json":
			d.ConfigJSON = append([]byte(nil), file.Data...)
		}
	}
	f.definitions[key] = d
	return "sha", nil
}

func (f *recordingVersion) Head(context.Context, string) (string, error) { return "sha", nil }

func (f *recordingVersion) Read(_ context.Context, key, ref string) (version.Definition, error) {
	f.calls = append(f.calls, versionCall{op: "read", key: key, ref: ref})
	d, ok := f.definitions[key]
	if !ok {
		return version.Definition{}, version.ErrNotFound
	}
	return d, nil
}

func (f *recordingVersion) Rename(_ context.Context, from, to string) error {
	f.calls = append(f.calls, versionCall{op: "rename", key: from, to: to})
	if f.renameErr != nil {
		return f.renameErr
	}
	f.definitions[to] = f.definitions[from]
	delete(f.definitions, from)
	return nil
}

func (f *recordingVersion) Archive(_ context.Context, key string) error {
	f.calls = append(f.calls, versionCall{op: "archive", key: key})
	return nil
}

func (f *recordingVersion) RunToken(context.Context, string, string, time.Duration) (version.Credential, error) {
	return version.Credential{}, nil
}

func callsByOp(calls []versionCall, op string) []versionCall {
	var out []versionCall
	for _, call := range calls {
		if call.op == op {
			out = append(out, call)
		}
	}
	return out
}

func filePaths(files []version.File) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func fileByPath(t *testing.T, files []version.File, path string) version.File {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("files %v do not contain %q", filePaths(files), path)
	return version.File{}
}

func TestCreateCommitsExactInitialDefinitionBatch(t *testing.T) {
	// R-RLZ0-72UW
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	cfg := Config{Provider: "anthropic", Model: testAnthropicModel, MaxTokens: 17}
	created, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{
		Name: "Three Files", UserPrompt: "user body", SystemPrompt: "system body", Config: cfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{plane.calls[0].op, plane.calls[1].op}; !reflect.DeepEqual(got, []string{"create", "commit"}) {
		t.Fatalf("call order = %v, want create then commit", got)
	}
	commit := callsByOp(plane.calls, "commit")
	if len(commit) != 1 || !reflect.DeepEqual(filePaths(commit[0].files), []string{"prompt.md", "config.json", "system.md"}) {
		t.Fatalf("commits = %+v", commit)
	}
	if got := string(fileByPath(t, commit[0].files, "prompt.md").Data); got != "user body" {
		t.Fatalf("prompt.md = %q", got)
	}
	var gotConfig Config
	if err := json.Unmarshal(fileByPath(t, commit[0].files, "config.json").Data, &gotConfig); err != nil || !reflect.DeepEqual(gotConfig, cfg) {
		t.Fatalf("config.json = %+v, err=%v", gotConfig, err)
	}
	if commit[0].message != "create Three Files" || commit[0].actor != "prompts:"+created.ID {
		t.Fatalf("commit attribution = %+v", commit[0])
	}

	plane.calls = nil
	if _, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Two Files", UserPrompt: "body", Config: cfg}); err != nil {
		t.Fatal(err)
	}
	commit = callsByOp(plane.calls, "commit")
	if len(commit) != 1 || !reflect.DeepEqual(filePaths(commit[0].files), []string{"prompt.md", "config.json"}) {
		t.Fatalf("empty-system commits = %+v", commit)
	}
}

func TestUpdateCommitsOnlyChangedDefinitionAndRenamesBeforeRow(t *testing.T) {
	// R-ROES-YMCA
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Old", UserPrompt: "body", SystemPrompt: "system", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	plane.calls = nil
	changedConfig := validConfig()
	changedConfig.MaxTokens = 99
	if _, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: p.Name, UserPrompt: p.UserPrompt, SystemPrompt: p.SystemPrompt, Config: changedConfig}); err != nil {
		t.Fatal(err)
	}
	commits := callsByOp(plane.calls, "commit")
	if len(commits) != 1 || !reflect.DeepEqual(filePaths(commits[0].files), []string{"config.json"}) {
		t.Fatalf("config update commits = %+v", commits)
	}

	plane.calls = nil
	if _, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: p.Name, UserPrompt: p.UserPrompt, Config: changedConfig}); err != nil {
		t.Fatal(err)
	}
	commits = callsByOp(plane.calls, "commit")
	if len(commits) != 1 {
		t.Fatalf("system clear commits = %+v", commits)
	}
	system := fileByPath(t, commits[0].files, "system.md")
	if !system.Delete || len(system.Data) != 0 {
		t.Fatalf("system.md clear = %+v, want Delete", system)
	}

	plane.calls = nil
	plane.renameErr = version.ErrUnavailable
	_, err = svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: "New", UserPrompt: p.UserPrompt, Config: changedConfig})
	if err == nil {
		t.Fatal("name-only update succeeded despite failed rename")
	}
	if len(callsByOp(plane.calls, "rename")) != 1 || len(callsByOp(plane.calls, "commit")) != 0 {
		t.Fatalf("name-only calls = %+v", plane.calls)
	}
	stored, err := store.GetPrompt(t.Context(), ownerA, p.ID)
	if err != nil || stored.Name != "Old" || stored.NameKey != "old" {
		t.Fatalf("stored prompt after failed rename = %+v, err=%v", stored, err)
	}
}

func TestImportVersionsFirstAndRefreshBatches(t *testing.T) {
	// R-RPMP-CE2Z
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	svc.Fetcher = stubFetcher{data: []byte("first bytes\n")}
	p, err := svc.Import(t.Context(), ownerA, ownerA, "/mirror/prompt.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(callsByOp(plane.calls, "create")) != 1 {
		t.Fatalf("first import calls = %+v", plane.calls)
	}
	commits := callsByOp(plane.calls, "commit")
	if len(commits) != 1 || !reflect.DeepEqual(filePaths(commits[0].files), []string{"prompt.md", "config.json"}) || string(commits[0].files[0].Data) != "first bytes\n" {
		t.Fatalf("first import commits = %+v", commits)
	}

	p.Config.MaxTokens = 321
	if err := store.UpdatePrompt(t.Context(), ownerA, p); err != nil {
		t.Fatal(err)
	}
	plane.calls = nil
	svc.Fetcher = stubFetcher{data: []byte("second bytes\n")}
	p, err = svc.Import(t.Context(), ownerA, ownerA, "/mirror/prompt.md", "")
	if err != nil {
		t.Fatal(err)
	}
	commits = callsByOp(plane.calls, "commit")
	if len(callsByOp(plane.calls, "create")) != 0 || len(commits) != 1 || !reflect.DeepEqual(filePaths(commits[0].files), []string{"prompt.md"}) {
		t.Fatalf("re-import calls = %+v", plane.calls)
	}
	if p.Config.MaxTokens != 321 || string(commits[0].files[0].Data) != "second bytes\n" {
		t.Fatalf("re-import prompt=%+v files=%+v", p, commits[0].files)
	}
}

func TestDeleteArchivesDefinitionAndPreservesCompletedRun(t *testing.T) {
	// R-RQUL-Q5TO
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, runsDir := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Keep History", UserPrompt: "body", Config: validConfig(), Triggers: []TriggerSpec{{Filter: "cron:tick/nightly"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRunTerminal(t.Context(), run.ID, RunSucceeded, store.nowStr(), "", ""); err != nil {
		t.Fatal(err)
	}
	plane.calls = nil
	if err := svc.Delete(t.Context(), ownerA, p.ID); err != nil {
		t.Fatal(err)
	}
	archives := callsByOp(plane.calls, "archive")
	if len(archives) != 1 || archives[0].key != p.NameKey {
		t.Fatalf("archive calls = %+v", archives)
	}
	if _, err := store.GetPrompt(t.Context(), ownerA, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted prompt lookup err = %v", err)
	}
	if triggers, err := store.ListTriggers(t.Context(), p.ID); err != nil || len(triggers) != 0 {
		t.Fatalf("triggers = %+v, err=%v", triggers, err)
	}
	requireRunArtifactsWithoutCall(t, store, runsDir, run)
}

func requireRunArtifactsWithoutCall(t *testing.T, store *Store, runsDir string, run Run) {
	t.Helper()
	if got, err := store.GetRun(t.Context(), run.ID); err != nil || got.Status != RunSucceeded {
		t.Fatalf("completed run = %+v, err=%v", got, err)
	}
	if executed, err := LoadFromRun(runsDir, run.ID); err != nil || executed.UserPrompt != "body" {
		t.Fatalf("read surviving run input = %+v, err=%v", executed, err)
	}
}

func TestVersionFailureRollsBackCreateAndLeavesUpdateRowUnchanged(t *testing.T) {
	// R-RS2I-3XKD
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	plane.commitErr = version.ErrUnavailable
	_, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Rollback", UserPrompt: "body", Config: validConfig()})
	if err == nil || !strings.Contains(err.Error(), "version plane") {
		t.Fatalf("create error = %v", err)
	}
	requirePromptCount(t, store, 0)

	plane.commitErr = nil
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Original", UserPrompt: "body", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	plane.commitErr = version.ErrUnavailable
	_, err = svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: "Renamed", UserPrompt: "changed", Config: validConfig()})
	if err == nil || !strings.Contains(err.Error(), "version plane") {
		t.Fatalf("update error = %v", err)
	}
	stored, err := store.GetPrompt(t.Context(), ownerA, p.ID)
	if err != nil || stored.Name != "Original" || stored.NameKey != "original" || stored.UserPrompt != "body" {
		t.Fatalf("stored prompt after failed update = %+v, err=%v", stored, err)
	}
}

func TestListAvoidsDefinitionReadsWhileGetReadsExactlyOnce(t *testing.T) {
	// R-SGGH-RCE9
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	for _, name := range []string{"One", "Two", "Three"} {
		if _, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: name, UserPrompt: "body " + name, SystemPrompt: "system " + name, Config: validConfig()}); err != nil {
			t.Fatal(err)
		}
	}
	plane.calls = nil
	listed, err := svc.List(t.Context(), ownerA)
	if err != nil {
		t.Fatal(err)
	}
	if len(callsByOp(plane.calls, "read")) != 0 || len(listed) != 3 {
		t.Fatalf("list len=%d calls=%+v", len(listed), plane.calls)
	}
	for _, item := range listed {
		if item.Name == "" || item.NameKey == "" || item.CreatedAt == "" || item.UpdatedAt == "" || item.UserPrompt != "" || item.SystemPrompt != "" || item.Config != (Config{}) {
			t.Fatalf("list item is not metadata-only: %+v", item)
		}
	}

	plane.calls = nil
	got, err := svc.Get(t.Context(), ownerA, listed[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	reads := callsByOp(plane.calls, "read")
	if len(reads) != 1 || reads[0].key != listed[0].NameKey || reads[0].ref != "main" {
		t.Fatalf("get reads = %+v", reads)
	}
	if got.UserPrompt == "" || got.SystemPrompt == "" || got.Config.Model != testAnthropicModel {
		t.Fatalf("get definition = %+v", got)
	}
}

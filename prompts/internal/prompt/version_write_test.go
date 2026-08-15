package prompt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"prompts/internal/version"
	"reflect"
	"strings"
	"testing"
	"time"
)

type versionCall struct {
	op, key, to, message, actor, ref string
	owner                            version.Owner
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

func (f *recordingVersion) Create(_ context.Context, key string, owner version.Owner, actor string) error {
	f.calls = append(f.calls, versionCall{op: "create", key: key, owner: owner, actor: actor})
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

func (f *recordingVersion) Rename(_ context.Context, from, to string, owner version.Owner, actor string) error {
	f.calls = append(f.calls, versionCall{op: "rename", key: from, to: to, owner: owner, actor: actor})
	if f.renameErr != nil {
		return f.renameErr
	}
	f.definitions[to] = f.definitions[from]
	delete(f.definitions, from)
	return nil
}

func (f *recordingVersion) Archive(_ context.Context, key string, owner version.Owner, actor string) error {
	f.calls = append(f.calls, versionCall{op: "archive", key: key, owner: owner, actor: actor})
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
	if plane.calls[0].owner != (version.Owner{ID: ownerA, Email: ownerA}) || plane.calls[0].actor != "prompts:"+created.ID {
		t.Fatalf("create identity = %+v", plane.calls[0])
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
	if _, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: ptr(p.Name), UserPrompt: ptr("body"), SystemPrompt: ptr("system"), Config: ptr(changedConfig)}); err != nil {
		t.Fatal(err)
	}
	commits := callsByOp(plane.calls, "commit")
	if len(commits) != 1 || !reflect.DeepEqual(filePaths(commits[0].files), []string{"config.json"}) {
		t.Fatalf("config update commits = %+v", commits)
	}

	plane.calls = nil
	if _, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: ptr(p.Name), UserPrompt: ptr("body"), SystemPrompt: ptr(""), Config: ptr(changedConfig)}); err != nil {
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
	_, err = svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: ptr("New"), UserPrompt: ptr("body"), Config: ptr(changedConfig)})
	if err == nil {
		t.Fatal("name-only update succeeded despite failed rename")
	}
	if len(callsByOp(plane.calls, "rename")) != 1 || len(callsByOp(plane.calls, "commit")) != 0 {
		t.Fatalf("name-only calls = %+v", plane.calls)
	}
	rename := callsByOp(plane.calls, "rename")[0]
	if rename.owner != (version.Owner{ID: ownerA, Email: ownerA}) || rename.actor != "prompts:"+p.ID {
		t.Fatalf("rename identity = %+v", rename)
	}
	stored, err := store.GetPrompt(t.Context(), ownerA, p.ID)
	if err != nil || stored.Name != "Old" || stored.NameKey != "old" {
		t.Fatalf("stored prompt after failed rename = %+v, err=%v", stored, err)
	}
}

func TestUpdateMergesConfigAndUserPromptFieldsIndependently(t *testing.T) {
	// R-A8EU-5VUL
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	originalConfig := validConfig()
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{
		Name: "Merge Target", UserPrompt: "original body", SystemPrompt: "keep system", Config: originalConfig,
	})
	if err != nil {
		t.Fatal(err)
	}

	changedConfig := originalConfig
	changedConfig.MaxTokens = 4321
	plane.calls = nil
	updated, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Config: ptr(changedConfig)})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != p.Name || updated.NameKey != p.NameKey {
		t.Fatalf("config-only metadata = %+v, want name=%q key=%q", updated, p.Name, p.NameKey)
	}
	if renames := callsByOp(plane.calls, "rename"); len(renames) != 0 {
		t.Fatalf("config-only renames = %+v", renames)
	}
	commits := callsByOp(plane.calls, "commit")
	if len(commits) != 1 || !reflect.DeepEqual(filePaths(commits[0].files), []string{"config.json"}) {
		t.Fatalf("config-only commits = %+v", commits)
	}
	stored, err := store.GetPrompt(t.Context(), ownerA, p.ID)
	if err != nil || stored.Name != p.Name || stored.NameKey != p.NameKey {
		t.Fatalf("config-only stored row = %+v, err=%v", stored, err)
	}
	detail, err := svc.Get(t.Context(), ownerA, p.ID)
	if err != nil || !reflect.DeepEqual(detail.Config, changedConfig) || detail.UserPrompt != "original body" || detail.SystemPrompt != "keep system" {
		t.Fatalf("config-only definition = %+v, err=%v", detail, err)
	}

	plane.calls = nil
	if _, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{UserPrompt: ptr("changed body")}); err != nil {
		t.Fatal(err)
	}
	commits = callsByOp(plane.calls, "commit")
	if len(commits) != 1 || !reflect.DeepEqual(filePaths(commits[0].files), []string{"prompt.md"}) {
		t.Fatalf("user-prompt-only commits = %+v", commits)
	}
	detail, err = svc.Get(t.Context(), ownerA, p.ID)
	if err != nil || detail.UserPrompt != "changed body" || !reflect.DeepEqual(detail.Config, changedConfig) {
		t.Fatalf("user-prompt-only definition = %+v, err=%v", detail, err)
	}
}

func TestUpdateRejectsEmptySentUserPromptWithoutPlaneWrites(t *testing.T) {
	// R-A9MQ-JNLA
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Guarded", UserPrompt: "keep me", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}
	wantDefinition := plane.definitions[p.NameKey]

	for _, body := range []string{"", " \t\n "} {
		plane.calls = nil
		_, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{UserPrompt: ptr(body)})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("Update user_prompt %q error = %v, want ValidationError", body, err)
		}
		if len(callsByOp(plane.calls, "commit")) != 0 || len(callsByOp(plane.calls, "rename")) != 0 {
			t.Fatalf("Update user_prompt %q plane calls = %+v", body, plane.calls)
		}
		if got := plane.definitions[p.NameKey]; !reflect.DeepEqual(got, wantDefinition) {
			t.Fatalf("Update user_prompt %q changed definition to %+v", body, got)
		}
	}
}

func TestUpdateEmptyNameRenamesToULIDKeyWithoutCommit(t *testing.T) {
	// R-AAUM-XFBZ
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	p, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{Name: "Named Prompt", UserPrompt: "body", Config: validConfig()})
	if err != nil {
		t.Fatal(err)
	}

	plane.calls = nil
	updated, err := svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	wantKey := NameKey("", p.ID)
	renames := callsByOp(plane.calls, "rename")
	if len(renames) != 1 || renames[0].key != p.NameKey || renames[0].to != wantKey {
		t.Fatalf("renames = %+v, want %q -> %q", renames, p.NameKey, wantKey)
	}
	if commits := callsByOp(plane.calls, "commit"); len(commits) != 0 {
		t.Fatalf("name-only commits = %+v", commits)
	}
	stored, err := store.GetPrompt(t.Context(), ownerA, p.ID)
	if err != nil || updated.Name != "" || updated.NameKey != wantKey || stored.Name != "" || stored.NameKey != wantKey {
		t.Fatalf("updated=%+v stored=%+v err=%v, want empty name and key %q", updated, stored, err, wantKey)
	}
}

func TestCreateRejectsEmptyUserPromptBeforeRowOrPlaneWrites(t *testing.T) {
	// R-AC2J-B72O
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, store, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane

	for _, body := range []string{"", " \t\n "} {
		_, err := svc.Create(t.Context(), ownerA, ownerA, CreateInput{UserPrompt: body, Config: validConfig()})
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("Create user_prompt %q error = %v, want ValidationError", body, err)
		}
	}
	requirePromptCount(t, store, 0)
	if len(callsByOp(plane.calls, "create")) != 0 || len(callsByOp(plane.calls, "commit")) != 0 {
		t.Fatalf("rejected create plane calls = %+v", plane.calls)
	}
}

func TestImportRejectsEmptyMirrorBodyWithoutChangingExistingDefinition(t *testing.T) {
	// R-ADAF-OYTD
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, _, _ := newTestService(t)
	plane := newRecordingVersion()
	svc.Version = plane
	svc.Fetcher = stubFetcher{data: []byte("existing body\n")}
	existing, err := svc.Import(t.Context(), ownerA, ownerA, "/mirror/existing.md", "")
	if err != nil {
		t.Fatal(err)
	}
	wantDefinition := plane.definitions[existing.NameKey]

	for i, body := range [][]byte{{}, []byte(" \t\n ")} {
		plane.calls = nil
		svc.Fetcher = stubFetcher{data: body}
		_, err := svc.Import(t.Context(), ownerA, ownerA, fmt.Sprintf("/mirror/rejected-%d.md", i), "")
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("Import body %q error = %v, want ValidationError", body, err)
		}
		if len(plane.calls) != 0 {
			t.Fatalf("Import body %q plane calls = %+v", body, plane.calls)
		}
		if got := plane.definitions[existing.NameKey]; !reflect.DeepEqual(got, wantDefinition) {
			t.Fatalf("Import body %q changed existing definition to %+v", body, got)
		}
	}
	detail, err := svc.Get(t.Context(), ownerA, existing.ID)
	if err != nil || detail.UserPrompt != "existing body\n" {
		t.Fatalf("existing definition = %+v, err=%v", detail, err)
	}
}

func TestImportVersionsFirstAndRefreshBatches(t *testing.T) {
	// R-RPMP-CE2Z
	t.Setenv("ANTHROPIC_API_KEY", "test")
	svc, _, _, _ := newTestService(t)
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

	cfg := validConfig()
	cfg.MaxTokens = 321
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	definition := plane.definitions[p.NameKey]
	definition.ConfigJSON = raw
	plane.definitions[p.NameKey] = definition
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
	detail, getErr := svc.Get(t.Context(), ownerA, p.ID)
	if getErr != nil || detail.Config.MaxTokens != 321 || string(commits[0].files[0].Data) != "second bytes\n" {
		t.Fatalf("re-import prompt=%+v err=%v files=%+v", detail, getErr, commits[0].files)
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
	svc.Version = nil
	run, err := svc.Run(t.Context(), ownerA, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateRunTerminal(t.Context(), run.ID, RunSucceeded, store.nowStr(), "", ""); err != nil {
		t.Fatal(err)
	}
	svc.Version = plane
	plane.calls = nil
	if err := svc.Delete(t.Context(), ownerA, p.ID); err != nil {
		t.Fatal(err)
	}
	archives := callsByOp(plane.calls, "archive")
	if len(archives) != 1 || archives[0].key != p.NameKey || archives[0].owner != (version.Owner{ID: ownerA, Email: ownerA}) || archives[0].actor != "prompts:"+p.ID {
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
	if executed, err := LoadFromRun(runsDir, run.ID, run.DefinitionSha); err != nil || executed.UserPrompt != "body" {
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
	_, err = svc.Update(t.Context(), ownerA, p.ID, UpdateInput{Name: ptr("Renamed"), UserPrompt: ptr("changed"), Config: ptr(validConfig())})
	if err == nil || !strings.Contains(err.Error(), "version plane") {
		t.Fatalf("update error = %v", err)
	}
	stored, err := store.GetPrompt(t.Context(), ownerA, p.ID)
	if err != nil || stored.Name != "Original" || stored.NameKey != "original" {
		t.Fatalf("stored prompt after failed update = %+v, err=%v", stored, err)
	}
}

func TestListAvoidsDefinitionReadsWhileGetReadsExactlyOnce(t *testing.T) {
	// R-SGGH-RCE9
	// R-SF8L-DKNK
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
	if len(plane.calls) != 1 || plane.calls[0].op != "read" || plane.calls[0].key != listed[0].NameKey || plane.calls[0].ref != "main" {
		t.Fatalf("get calls = %+v, want exactly one read(%q, main)", plane.calls, listed[0].NameKey)
	}
	if got.UserPrompt == "" || got.SystemPrompt == "" || got.Config.Model != testAnthropicModel {
		t.Fatalf("get definition = %+v", got)
	}
}

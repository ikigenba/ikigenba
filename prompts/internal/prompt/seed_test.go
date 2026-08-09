package prompt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"prompts/internal/ids"
	"prompts/internal/version"
)

type seedCommit struct {
	nameKey string
	files   []version.File
	message string
	actor   string
}

type seedVersionFake struct {
	heads              map[string]string
	commits            []seedCommit
	createCalls        int
	unavailableCreates int
	alwaysUnavailable  bool
}

func (f *seedVersionFake) Create(context.Context, string, string) error {
	f.createCalls++
	if f.alwaysUnavailable || f.unavailableCreates > 0 {
		if f.unavailableCreates > 0 {
			f.unavailableCreates--
		}
		return fmt.Errorf("repos starting: %w", version.ErrUnavailable)
	}
	return nil
}

func (f *seedVersionFake) Commit(_ context.Context, nameKey string, files []version.File, message, actor string) (string, error) {
	copyFiles := make([]version.File, len(files))
	for i, file := range files {
		copyFiles[i] = file
		copyFiles[i].Data = append([]byte(nil), file.Data...)
	}
	f.commits = append(f.commits, seedCommit{nameKey: nameKey, files: copyFiles, message: message, actor: actor})
	if f.heads == nil {
		f.heads = make(map[string]string)
	}
	f.heads[nameKey] = "seeded-head"
	return "seeded-head", nil
}

func (f *seedVersionFake) Head(_ context.Context, nameKey string) (string, error) {
	return f.heads[nameKey], nil
}

func (*seedVersionFake) Read(context.Context, string, string) (version.Definition, error) {
	return version.Definition{}, nil
}
func (*seedVersionFake) Rename(context.Context, string, string) error { return nil }
func (*seedVersionFake) Archive(context.Context, string) error        { return nil }
func (*seedVersionFake) RunToken(context.Context, string, string, time.Duration) (version.Credential, error) {
	return version.Credential{}, nil
}

func insertSeedDefinition(t *testing.T, store *Store, name, userPrompt, systemPrompt, configJSON string) string {
	t.Helper()
	return insertSeedDefinitionWithID(t, store, ids.NewULID(), name, userPrompt, systemPrompt, configJSON)
}

func insertSeedDefinitionWithID(t *testing.T, store *Store, id, name, userPrompt, systemPrompt, configJSON string) string {
	t.Helper()
	now := store.nowStr()
	_, err := store.db.Exec(
		`INSERT INTO prompts
		 (id, owner_id, owner_email, name, user_prompt, system_prompt, config_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)`,
		id, "owner-"+id, id+"@example.com", name, userPrompt, systemPrompt, configJSON, now, now,
	)
	if err != nil {
		t.Fatalf("insert seed definition: %v", err)
	}
	return id
}

func newSeedService(store *Store, plane version.Client) *Service {
	svc := NewService(store, nil, "", nil)
	svc.Version = plane
	return svc
}

// R-RTAE-HPB2
func TestSeedDefinitionsIsIdempotentAfterRepositoriesHaveHeads(t *testing.T) {
	store := newTestStore(t)
	idsByName := make(map[string]string)
	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		idsByName[name] = insertSeedDefinition(t, store, name, name+" body", "", `{"model":"m"}`)
	}
	plane := &seedVersionFake{heads: make(map[string]string)}
	svc := newSeedService(store, plane)

	seeded, err := svc.SeedDefinitions(context.Background())
	if err != nil || seeded != 3 || len(plane.commits) != 3 {
		t.Fatalf("first SeedDefinitions = (%d, %v), commits=%d; want (3, nil), 3", seeded, err, len(plane.commits))
	}
	firstCommitCount := len(plane.commits)
	seeded, err = svc.SeedDefinitions(context.Background())
	if err != nil || seeded != 0 {
		t.Fatalf("second SeedDefinitions = (%d, %v), want (0, nil)", seeded, err)
	}
	if got := len(plane.commits) - firstCommitCount; got != 0 {
		t.Fatalf("second SeedDefinitions issued %d commits, want zero", got)
	}
	for name, id := range idsByName {
		p, err := store.GetPromptByID(context.Background(), id)
		if err != nil || p.NameKey == "" {
			t.Errorf("prompt %q after second pass = %+v, err=%v; want persisted name_key", name, p, err)
		}
	}
}

// R-RUIA-VH1R
func TestSeedDefinitionsPreservesRawColumnsAndSuffixesCollidingKeys(t *testing.T) {
	store := newTestStore(t)
	rawConfig := "{\n  \"z\": 1, \"a\": [3, 2]\n}\n"
	reportID := insertSeedDefinitionWithID(t, store, "01SEED00000000000000000001", "Report", "report bytes\n", "system bytes\n", rawConfig)
	otherID := insertSeedDefinitionWithID(t, store, "01SEED00000000000000000002", "report!", "other bytes", "", ` { "verbatim": true } `)
	plane := &seedVersionFake{heads: make(map[string]string)}

	seeded, err := newSeedService(store, plane).SeedDefinitions(context.Background())
	if err != nil || seeded != 2 {
		t.Fatalf("SeedDefinitions = (%d, %v), want (2, nil)", seeded, err)
	}
	byKey := make(map[string]seedCommit)
	for _, commit := range plane.commits {
		byKey[commit.nameKey] = commit
	}
	if got := fileDataByPath(byKey["report"].files); !reflect.DeepEqual(got, map[string]string{
		"prompt.md": "report bytes\n", "config.json": rawConfig, "system.md": "system bytes\n",
	}) {
		t.Fatalf("report files = %#v, want exact three legacy column values", got)
	}
	if got := fileDataByPath(byKey["report-2"].files); !reflect.DeepEqual(got, map[string]string{
		"prompt.md": "other bytes", "config.json": ` { "verbatim": true } `,
	}) {
		t.Fatalf("report-2 files = %#v, want exact two legacy column values", got)
	}
	for id, wantKey := range map[string]string{reportID: "report", otherID: "report-2"} {
		p, err := store.GetPromptByID(context.Background(), id)
		if err != nil || p.NameKey != wantKey {
			t.Errorf("prompt %s key = %q, err=%v; want %q", id, p.NameKey, err, wantKey)
		}
	}
	if byKey["report"].message != "seed Report" || byKey["report-2"].message != "seed report!" {
		t.Fatalf("seed messages = %q, %q", byKey["report"].message, byKey["report-2"].message)
	}
}

func fileDataByPath(files []version.File) map[string]string {
	result := make(map[string]string, len(files))
	for _, file := range files {
		result[file.Path] = string(file.Data)
	}
	return result
}

// R-RVQ7-98SG
func TestSeedDefinitionsRetriesUnavailablePlaneAndFailsLoudlyAtBound(t *testing.T) {
	store := newTestStore(t)
	insertSeedDefinition(t, store, "Retry", "body", "", `{}`)
	plane := &seedVersionFake{heads: make(map[string]string), unavailableCreates: 2}
	svc := newSeedService(store, plane)
	svc.seedSleep = func(context.Context, time.Duration) error { return nil }

	seeded, err := svc.SeedDefinitions(context.Background())
	if err != nil || seeded != 1 || plane.createCalls != 3 || len(plane.commits) != 1 {
		t.Fatalf("retrying SeedDefinitions = (%d, %v), creates=%d commits=%d; want success on third create and one commit", seeded, err, plane.createCalls, len(plane.commits))
	}

	failingStore := newTestStore(t)
	insertSeedDefinition(t, failingStore, "Unavailable", "body", "", `{}`)
	failingPlane := &seedVersionFake{heads: make(map[string]string), alwaysUnavailable: true}
	failingSvc := newSeedService(failingStore, failingPlane)
	failingSvc.seedRetryWindow = 250 * time.Millisecond
	var waited time.Duration
	failingSvc.seedSleep = func(_ context.Context, delay time.Duration) error {
		waited += delay
		return nil
	}

	seeded, err = failingSvc.SeedDefinitions(context.Background())
	if err == nil || !errors.Is(err, version.ErrUnavailable) || !strings.Contains(err.Error(), "version plane") {
		t.Fatalf("exhausted SeedDefinitions = (%d, %v), want version-plane ErrUnavailable", seeded, err)
	}
	if seeded != 0 || len(failingPlane.commits) != 0 {
		t.Fatalf("exhausted SeedDefinitions seeded=%d commits=%d, want zero", seeded, len(failingPlane.commits))
	}
	if waited != failingSvc.seedRetryWindow {
		t.Fatalf("backoff waited %s, want bounded window %s", waited, failingSvc.seedRetryWindow)
	}
}

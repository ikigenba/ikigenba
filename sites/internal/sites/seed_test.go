package sites

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type seedCreateCall struct {
	slug  string
	owner Owner
}

type seedCommitCall struct {
	slug    string
	message string
	changes []FileChange
}

type seedVersionRecorder struct {
	creates        []seedCreateCall
	commits        []seedCommitCall
	failCommitSlug string
}

func (r *seedVersionRecorder) Create(_ context.Context, slug string, owner Owner) error {
	r.creates = append(r.creates, seedCreateCall{slug: slug, owner: owner})
	return nil
}

func (r *seedVersionRecorder) Commit(_ context.Context, slug, message string, changes []FileChange) (Commit, error) {
	copied := make([]FileChange, len(changes))
	for i, change := range changes {
		copied[i] = change
		copied[i].Data = append([]byte(nil), change.Data...)
	}
	r.commits = append(r.commits, seedCommitCall{slug: slug, message: message, changes: copied})
	if slug == r.failCommitSlug {
		return Commit{}, errors.New("injected commit failure")
	}
	return Commit{Sha: slug + "-sha"}, nil
}

func (r *seedVersionRecorder) Export(context.Context, string) ([]TreeEntry, Commit, error) {
	return nil, Commit{}, errors.New("unexpected Export")
}

func (r *seedVersionRecorder) Rename(context.Context, string, string, Owner) error {
	return errors.New("unexpected Rename")
}

func (r *seedVersionRecorder) Delete(context.Context, string, Owner) error {
	return errors.New("unexpected Delete")
}

func insertSeedSite(t *testing.T, store *Store, site Site) {
	t.Helper()
	seeded := 0
	if site.RepoSeeded {
		seeded = 1
	}
	_, err := store.db.ExecContext(context.Background(), `INSERT INTO sites
		(slug, name, source_path, visibility, owner_id, owner_email, repo_sha, repo_seeded, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, site.Slug, site.Name, site.SourcePath,
		site.Visibility, site.OwnerID, site.OwnerEmail, site.RepoSha, seeded,
		"2026-05-01T10:00:00.000000000Z", "2026-05-02T11:00:00.000000000Z")
	if err != nil {
		t.Fatalf("insert seed site %q: %v", site.Slug, err)
	}
}

func writeSeedFile(t *testing.T, layout Layout, site Site, name string, data []byte) {
	t.Helper()
	path := filepath.Join(layout.SiteDir(site.Visibility, site.Slug), filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create seed directory: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
}

func TestSeedCreatesRepositoryAndSingleInitialCommit(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	layout := Layout{Root: t.TempDir()}
	alpha := Site{Slug: "alpha", Name: "Alpha", Visibility: Public, OwnerID: "owner-a", OwnerEmail: "a@example.com"}
	empty := Site{Slug: "empty", Name: "Empty", Visibility: Private, OwnerID: "owner-e", OwnerEmail: "e@example.com"}
	insertSeedSite(t, store, alpha)
	insertSeedSite(t, store, empty)
	writeSeedFile(t, layout, alpha, "index.html", []byte("<h1>alpha</h1>"))
	writeSeedFile(t, layout, alpha, "css/app.css", []byte("body { color: navy; }"))
	if err := os.MkdirAll(layout.SiteDir(empty.Visibility, empty.Slug), 0o755); err != nil {
		t.Fatal(err)
	}

	version := &seedVersionRecorder{}
	got, err := Seed(ctx, store, layout, version)
	// R-FFN9-X9GL
	if err != nil || got != 2 {
		t.Fatalf("Seed = %d, %v; want 2, nil", got, err)
	}
	wantCreates := []seedCreateCall{{"alpha", Owner{"owner-a", "a@example.com"}}, {"empty", Owner{"owner-e", "e@example.com"}}}
	if !reflect.DeepEqual(version.creates, wantCreates) {
		t.Fatalf("Create calls = %+v, want %+v", version.creates, wantCreates)
	}
	if len(version.commits) != 1 {
		t.Fatalf("Commit call count = %d, want exactly 1", len(version.commits))
	}
	commit := version.commits[0]
	if commit.slug != "alpha" || commit.message != "seed alpha" || len(commit.changes) != 2 {
		t.Fatalf("Commit = %+v, want one two-file alpha commit", commit)
	}
	wantFiles := map[string][]byte{"index.html": []byte("<h1>alpha</h1>"), "css/app.css": []byte("body { color: navy; }")}
	for _, change := range commit.changes {
		want, ok := wantFiles[change.Path]
		if !ok || change.Delete || !bytes.Equal(change.Data, want) {
			t.Fatalf("change = %+v, want matching write from %v", change, wantFiles)
		}
		delete(wantFiles, change.Path)
	}
	if len(wantFiles) != 0 {
		t.Fatalf("missing committed files: %v", wantFiles)
	}
	alphaAfter, _ := store.Get(ctx, "alpha")
	emptyAfter, _ := store.Get(ctx, "empty")
	if !alphaAfter.RepoSeeded || alphaAfter.RepoSha != "alpha-sha" || !emptyAfter.RepoSeeded || emptyAfter.RepoSha != "" {
		t.Fatalf("seed state: alpha=%+v empty=%+v", alphaAfter, emptyAfter)
	}
}

func TestSeedRerunUsesSeededFlagAndMakesNoRequests(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	layout := Layout{Root: t.TempDir()}
	empty := Site{Slug: "empty", Name: "Empty", Visibility: Public, OwnerID: "owner", OwnerEmail: "owner@example.com"}
	insertSeedSite(t, store, empty)
	if err := os.MkdirAll(layout.SiteDir(empty.Visibility, empty.Slug), 0o755); err != nil {
		t.Fatal(err)
	}
	first := &seedVersionRecorder{}
	if got, err := Seed(ctx, store, layout, first); err != nil || got != 1 {
		t.Fatalf("first Seed = %d, %v; want 1, nil", got, err)
	}
	insertSeedSite(t, store, Site{Slug: "normal", Name: "Normal", Visibility: Private, OwnerID: "new", OwnerEmail: "new@example.com", RepoSeeded: true})

	second := &seedVersionRecorder{}
	got, err := Seed(ctx, store, layout, second)
	// R-FGV6-B17A
	if err != nil || got != 0 {
		t.Fatalf("second Seed = %d, %v; want 0, nil", got, err)
	}
	if len(second.creates) != 0 || len(second.commits) != 0 {
		t.Fatalf("second Seed requests: creates=%+v commits=%+v, want none", second.creates, second.commits)
	}
	emptyAfter, _ := store.Get(ctx, "empty")
	if !emptyAfter.RepoSeeded || emptyAfter.RepoSha != "" {
		t.Fatalf("empty site state = %+v, want seeded with empty sha", emptyAfter)
	}
}

func TestSeedPreservesTreesAndRowIdentityOnFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	layout := Layout{Root: t.TempDir()}
	sites := []Site{
		{Slug: "alpha", Name: "Alpha Name", Visibility: Public, OwnerID: "owner-a", OwnerEmail: "a@example.com", SourcePath: "/Alpha"},
		{Slug: "bravo", Name: "Bravo Name", Visibility: Private, OwnerID: "owner-b", OwnerEmail: "b@example.com", SourcePath: "/Bravo"},
		{Slug: "charlie", Name: "Charlie Name", Visibility: Unlisted, OwnerID: "owner-c", OwnerEmail: "c@example.com", SourcePath: "/Charlie"},
	}
	for _, site := range sites {
		insertSeedSite(t, store, site)
		writeSeedFile(t, layout, site, "index.html", []byte(site.Name))
		writeSeedFile(t, layout, site, "assets/data.bin", []byte{0, 1, byte(len(site.Slug)), 255})
	}
	filesBefore := snapshotSeedFiles(t, layout.Root)
	rowsBefore, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	version := &seedVersionRecorder{failCommitSlug: "bravo"}
	got, err := Seed(ctx, store, layout, version)
	// R-FI32-OSXZ
	if err == nil || got != 1 {
		t.Fatalf("Seed with second commit failure = %d, %v; want 1, error", got, err)
	}
	if filesAfter := snapshotSeedFiles(t, layout.Root); !reflect.DeepEqual(filesAfter, filesBefore) {
		t.Fatalf("served tree changed: before=%v after=%v", filesBefore, filesAfter)
	}
	rowsAfter, listErr := store.List(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	for i := range rowsBefore {
		before, after := rowsBefore[i], rowsAfter[i]
		before.RepoSha, after.RepoSha = "", ""
		before.RepoSeeded, after.RepoSeeded = false, false
		if before != after {
			t.Fatalf("row %q identity changed: before=%+v after=%+v", before.Slug, before, after)
		}
	}
	if !rowsAfter[0].RepoSeeded || rowsAfter[0].RepoSha != "alpha-sha" || rowsAfter[1].RepoSeeded || rowsAfter[1].RepoSha != "" || rowsAfter[2].RepoSeeded {
		t.Fatalf("failure checkpoint states = %+v", rowsAfter)
	}
	if len(version.creates) != 2 || len(version.commits) != 2 || version.creates[0].slug != "alpha" || version.creates[1].slug != "bravo" {
		t.Fatalf("failure call sequence creates=%+v commits=%+v", version.creates, version.commits)
	}
}

func snapshotSeedFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatalf("snapshot files: %v", err)
	}
	return files
}

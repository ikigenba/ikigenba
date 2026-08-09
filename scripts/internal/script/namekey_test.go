package script

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"scripts/internal/ids"
)

func TestSlugifyAndRepoKey(t *testing.T) {
	// R-22YE-GFY3
	scriptID := "01JABCDEF0123456789XYZABCD"
	tests := []struct {
		name string
		want string
	}{
		{"Nightly Export!", "nightly-export"},
		{"  a//b  ", "a-b"},
		{"!!!", "script-" + strings.ToLower(scriptID)},
	}
	for _, tt := range tests {
		got := slugify(tt.name, scriptID)
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.name, got, tt.want)
		}
		if RepoKey(got) != "scripts/"+got {
			t.Errorf("RepoKey(%q) = %q", got, RepoKey(got))
		}
	}

	unicodeKey := slugify("Ünïcödé ✨", scriptID)
	if !regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`).MatchString(unicodeKey) {
		t.Errorf("unicode slug %q is not an ASCII slug", unicodeKey)
	}
	for _, b := range []byte(unicodeKey) {
		if b >= 0x80 {
			t.Fatalf("unicode slug %q contains non-ASCII byte %#x", unicodeKey, b)
		}
	}
	longKey := slugify(strings.Repeat("a-", 100), scriptID)
	if len(longKey) > 48 || strings.HasPrefix(longKey, "-") || strings.HasSuffix(longKey, "-") {
		t.Errorf("long slug = %q (%d bytes), want <= 48 bytes with trimmed dashes", longKey, len(longKey))
	}
	for _, key := range []string{unicodeKey, longKey} {
		if RepoKey(key) != "scripts/"+key {
			t.Errorf("RepoKey(%q) = %q", key, RepoKey(key))
		}
	}
}

func TestNameKeysAreUniqueAcrossOwners(t *testing.T) {
	// R-246A-U7OS
	s := newTestStore(t)
	ctx := context.Background()
	insert := func(owner string) Script {
		t.Helper()
		now := nowStr()
		sc := Script{
			ID: ids.NewULID(), OwnerID: owner, OwnerEmail: owner + "@example.test",
			Name: "Nightly Export", Body: "print('ok')", Config: Config{Interpreter: "python3"},
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.InsertScript(ctx, sc); err != nil {
			t.Fatalf("InsertScript(%s): %v", owner, err)
		}
		got, err := s.GetScript(ctx, owner, sc.ID)
		if err != nil {
			t.Fatalf("GetScript(%s): %v", owner, err)
		}
		return got
	}
	first := insert("owner-one")
	second := insert("owner-two")
	third := insert("owner-three")
	if first.NameKey != "nightly-export" || second.NameKey != "nightly-export-2" || third.NameKey != "nightly-export-3" {
		t.Fatalf("name keys = %q, %q, %q", first.NameKey, second.NameKey, third.NameKey)
	}

	now := nowStr()
	_, err := s.db.ExecContext(ctx, `INSERT INTO scripts
		(id, owner_id, owner_email, name, config_json, name_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.NewULID(), "different-owner", "different@example.test", "Other", `{}`, first.NameKey, now, now)
	if err == nil {
		t.Fatal("direct SQL duplicate name_key succeeded; want UNIQUE constraint error")
	}
}

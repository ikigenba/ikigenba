package store

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// R-UMXQ-8USM
var _ func(*Store, string, TokenKind, *TokenPosition, int) ([]Token, error) = (*Store).ListTokensBefore

func TestListTokensBeforeOwnerKindWindowAndReverseOrder(t *testing.T) {
	// R-UO5M-MMJB
	st := openTokenTestStore(t, bytes.NewReader(nil))
	now := tokenTestNow()
	insertTokenUser(t, st, "owner", "owner@example.com", now)
	insertTokenUser(t, st, "other", "other@example.com", now)
	// All ordering branches: last use, creation tie breaker, byte order,
	// and never-used tokens, even when created after the used ones.
	order := []struct {
		suffix  string
		created time.Time
		used    *time.Time
	}{
		{"recent", now.Add(-4 * time.Hour), tokenTimePointer(now)},
		{"tie-A", now.Add(-2 * time.Hour), tokenTimePointer(now.Add(-time.Hour))},
		{"tie-a", now.Add(-2 * time.Hour), tokenTimePointer(now.Add(-time.Hour))},
		{"tie-older", now.Add(-3 * time.Hour), tokenTimePointer(now.Add(-time.Hour))},
		{"unused-A", now.Add(time.Hour), nil},
		{"unused-a", now.Add(time.Hour), nil},
		{"unused-older", now, nil},
	}
	wants := map[TokenKind][]Token{}
	for _, kind := range []TokenKind{TokenPersonal, TokenClient} {
		for i, row := range order {
			tok := Token{ID: string(kind) + row.suffix, UserID: "owner", Name: row.suffix,
				Hash: idcodec.HashSecret(string(kind) + row.suffix), Kind: kind, Enabled: i%2 == 0,
				CreatedAt: row.created, LastUsedAt: row.used}
			if i%2 == 1 {
				tok.ExpiresAt = tokenTimePointer(now.Add(-time.Hour))
			}
			if kind == TokenClient {
				tok.Host = "mcp.test"
			}
			insertToken(t, st, tok)
			wants[kind] = append(wants[kind], tok)
		}
		insertToken(t, st, Token{ID: string(kind) + "foreign", UserID: "other", Kind: kind, Hash: idcodec.HashSecret(string(kind) + "foreign"), CreatedAt: now.Add(2 * time.Hour), LastUsedAt: tokenTimePointer(now.Add(time.Hour))})
	}
	before := domainRows(t, st)
	for _, kind := range []TokenKind{TokenPersonal, TokenClient} {
		positions := []struct {
			name   string
			before *TokenPosition
			first  int
		}{
			{name: "last window", first: len(order)},
			{name: "before used rows", before: &TokenPosition{LastUsedAt: tokenTimePointer(now.Add(time.Nanosecond)), CreatedAt: now}, first: 0},
			{name: "between last uses", before: &TokenPosition{LastUsedAt: tokenTimePointer(now.Add(-time.Minute)), CreatedAt: now}, first: 1},
			{name: "before used creation tie", before: &TokenPosition{LastUsedAt: tokenTimePointer(now.Add(-time.Hour)), CreatedAt: now.Add(-time.Hour)}, first: 1},
			{name: "between used ids", before: &TokenPosition{LastUsedAt: tokenTimePointer(now.Add(-time.Hour)), CreatedAt: now.Add(-2 * time.Hour), ID: string(kind) + "tie-Z"}, first: 2},
			{name: "between used creation times", before: &TokenPosition{LastUsedAt: tokenTimePointer(now.Add(-time.Hour)), CreatedAt: now.Add(-150 * time.Minute)}, first: 3},
			{name: "after every used row", before: &TokenPosition{LastUsedAt: tokenTimePointer(now.Add(-2 * time.Hour)), CreatedAt: now}, first: 4},
			{name: "before unused rows", before: &TokenPosition{CreatedAt: now.Add(2 * time.Hour)}, first: 4},
			{name: "between unused ids", before: &TokenPosition{CreatedAt: now.Add(time.Hour), ID: string(kind) + "unused-Z"}, first: 5},
			{name: "between unused creation times", before: &TokenPosition{CreatedAt: now.Add(time.Minute)}, first: 6},
			{name: "after every row", before: &TokenPosition{CreatedAt: now.Add(-time.Hour)}, first: 7},
			{name: "bound id", before: &TokenPosition{CreatedAt: now.Add(time.Hour), ID: "' OR 1=1 --"}, first: 4},
		}
		for i, token := range wants[kind] {
			positions = append(positions, struct {
				name   string
				before *TokenPosition
				first  int
			}{name: "at " + token.ID, before: &TokenPosition{LastUsedAt: token.LastUsedAt, CreatedAt: token.CreatedAt, ID: token.ID}, first: i})
		}
		for _, position := range positions {
			for _, n := range []int{-3, 0, 1, 3, len(order), len(order) + 1, 100} {
				got, err := st.ListTokensBefore("owner", kind, position.before, n)
				preceding := wants[kind][:position.first]
				count := min(max(n, 0), len(preceding))
				want := make([]Token, count)
				for i := range count {
					want[i] = preceding[len(preceding)-1-i]
				}
				if err != nil || len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
					t.Fatalf("ListTokensBefore(%q,%s,%d) = %#v,%v; want %#v", kind, position.name, n, got, err, want)
				}
			}
			for _, owner := range []string{"missing-owner", "' OR 1=1 --"} {
				got, err := st.ListTokensBefore(owner, kind, position.before, 100)
				if err != nil || len(got) != 0 {
					t.Fatalf("absent owner %q, %s = %#v,%v", owner, position.name, got, err)
				}
			}
			got, err := st.ListTokensBefore("owner", TokenKind("' OR 1=1 --"), position.before, 100)
			if err != nil || len(got) != 0 {
				t.Fatalf("absent kind, %s = %#v,%v", position.name, got, err)
			}
		}
	}
	if after := domainRows(t, st); !reflect.DeepEqual(before, after) {
		t.Fatal("list changed rows")
	}
}

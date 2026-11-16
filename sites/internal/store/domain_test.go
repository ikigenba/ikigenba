package store_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func TestDomainValidation(t *testing.T) {
	// R-HZRC-IC6D R-7D6G-XGSL
	label := strings.Repeat("a", 63)
	for _, domain := range []string{"a", "localhost", "example.com", "a.1-2", "my-site.example.dev", "xn--bcher-kva.example", label + ".com", label + "." + label + "." + label + "." + strings.Repeat("a", 61)} {
		if !store.ValidDomain(domain) {
			t.Fatalf("valid domain refused: %q", domain)
		}
	}
	for _, domain := range []string{"", "1.2.3.4", "example.123", "Example.com", "exa_mple.com", "example.com.", ".example.com", "example..com", "-a.com", "a-.com", "a.-b.com", "example.com-", "é.example", "example.com:443", "a/host", "a\x00.com", "a b.com", strings.Repeat("a", 64) + ".com", label + "." + label + "." + label + "." + strings.Repeat("a", 62)} {
		if store.ValidDomain(domain) {
			t.Fatalf("invalid domain accepted: %q", domain)
		}
	}
}

func assertDomainRow(t *testing.T, d *db.DB, x store.Site) {
	t.Helper()
	equal(t, d.Read(ctx, func(tx *sql.Tx) error {
		var domain sql.NullString
		var payload string
		if err := tx.QueryRowContext(ctx, "SELECT domain,payload FROM sites WHERE id=?", x.ID).Scan(&domain, &payload); err != nil {
			return err
		}
		equal(t, domain, sql.NullString{String: x.Domain, Valid: x.Domain != ""})
		var members map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &members); err != nil {
			return err
		}
		for name := range members {
			if strings.EqualFold(name, "Domain") {
				t.Fatalf("domain leaked into payload: %s", name)
			}
		}
		return nil
	}), nil)
}

func assertDomainReads(t *testing.T, s *store.Store, want store.Site) {
	t.Helper()
	found, err := s.Find(ctx, want.Owner, want.Name)
	equal(t, err, nil)
	equal(t, found, want)
	found, err = s.BySlug(ctx, want.Slug)
	equal(t, err, nil)
	equal(t, found, want)
	if want.Domain != "" {
		found, err = s.ByDomain(ctx, want.Domain)
		equal(t, err, nil)
		equal(t, found, want)
	}
	for _, read := range []func() ([]store.Site, error){
		func() ([]store.Site, error) { return s.List(ctx, want.Owner, "", 100) },
		func() ([]store.Site, error) { return s.Visible(ctx, want.Owner, "", 100) },
		func() ([]store.Site, error) { return s.VisibleBefore(ctx, want.Owner, "", 100) },
		func() ([]store.Site, error) { return s.VisibleBefore(ctx, want.Owner, "zz", 100) },
	} {
		rows, err := read()
		equal(t, err, nil)
		matched := false
		for _, row := range rows {
			if row.ID == want.ID {
				matched = true
				equal(t, row, want)
			}
		}
		if !matched {
			t.Fatal("site missing from list")
		}
	}
	if want.Visibility == store.Public {
		found, err = s.SetApex(ctx, want.ID)
		equal(t, err, nil)
		equal(t, found, want)
		found, set, err := s.Apex(ctx)
		equal(t, err, nil)
		equal(t, set, true)
		equal(t, found, want)
		equal(t, s.ClearApex(ctx), nil)
	}
}

func TestDomainLifecycle(t *testing.T) {
	// R-HXBJ-QSOZ R-HYJG-4KFO R-ID68-PTC0 R-IAQF-Y9UM R-IFM1-HCTE R-I0Z8-W3X2 R-IEE5-3L2P
	path := filepath.Join(t.TempDir(), "db")
	s, handle := openAt(t, path, store.Config{})
	x := create(t, s, draft("docs"))
	equal(t, x.Domain, "")
	assertDomainReads(t, s, x)
	assertDomainRow(t, handle, x)
	for _, value := range []string{"", "missing.example", "DOCS.EXAMPLE"} {
		got, err := s.ByDomain(ctx, value)
		equal(t, got, store.Site{})
		checkErr(t, err, store.ErrNotFound)
	}
	var err error
	x, err = s.Publish(ctx, x.ID, strings.Repeat("a", 40), "published")
	equal(t, err, nil)
	equal(t, x.Domain, "")
	assertDomainReads(t, s, x)
	assertDomainRow(t, handle, x)
	for _, domain := range []string{"docs.example", "docs.example", "other.example", "", ""} {
		previous := x
		var changed []string
		x, changed, err = s.Update(ctx, x.ID, store.Change{Domain: &domain})
		equal(t, err, nil)
		want := []string{}
		if previous.Domain != domain {
			want = append(want, "domain")
		}
		equal(t, changed, want)
		previous.Domain = domain
		equal(t, x, previous)
		assertDomainReads(t, s, x)
		assertDomainRow(t, handle, x)
		x, err = s.Publish(ctx, x.ID, strings.Repeat("b", 40), "new-published")
		equal(t, err, nil)
		equal(t, x.Domain, domain)
		assertDomainRow(t, handle, x)
		if domain != "" {
			got, err := s.ByDomain(ctx, strings.ToUpper(domain))
			equal(t, got, store.Site{})
			checkErr(t, err, store.ErrNotFound)
		}
	}
	private, public, domain, listed, ref, configured := store.Private, store.Public, "reopen.example", false, "next", "configured"
	_, _, err = s.Update(ctx, x.ID, store.Change{Visibility: &private})
	equal(t, err, nil)
	previous := x
	previous.Visibility, previous.Listed, previous.Ref, previous.Path, previous.Domain = public, listed, ref, configured, domain
	x, changed, err := s.Update(ctx, x.ID, store.Change{Visibility: &public, Listed: &listed, Ref: &ref, Path: &configured, Domain: &domain})
	equal(t, err, nil)
	equal(t, changed, []string{"visibility", "listed", "ref", "path", "domain"})
	equal(t, x, previous)
	assertDomainReads(t, s, x)
	assertDomainRow(t, handle, x)
	x, changed, err = s.Update(ctx, x.ID, store.Change{})
	equal(t, err, nil)
	equal(t, changed, []string{})
	x, changed, err = s.Update(ctx, x.ID, store.Change{Visibility: &public, Listed: &listed, Ref: &ref, Path: &configured, Domain: &domain})
	equal(t, err, nil)
	equal(t, changed, []string{})
	equal(t, handle.Close(), nil)
	s, _ = openAt(t, path, store.Config{})
	assertDomainReads(t, s, x)
}

func TestDomainUpdateRefusals(t *testing.T) {
	// R-IGTX-V4K3 R-IMXF-RZ9K R-I72Q-SYMJ
	s := open(t, store.Config{})
	a := create(t, s, draft("domain"))
	domain, other, empty, private, public, bad, ref, listed, configured := "held.example", "other.example", "", store.Private, store.Public, "Bad.example", "changed", false, "new-path"
	a, _, err := s.Update(ctx, a.ID, store.Change{Domain: &domain})
	equal(t, err, nil)
	b := create(t, s, draft("second"))
	pd := draft("private")
	pd.Owner, pd.Visibility = "bob", store.Private
	p := create(t, s, pd)
	_, err = s.SetApex(ctx, a.ID)
	equal(t, err, nil)
	cases := []struct {
		id      string
		change  store.Change
		refusal error
	}{
		{"missing", store.Change{}, store.ErrNotFound},
		{"missing", store.Change{Domain: &bad}, nil},
		{a.ID, store.Change{Domain: &bad, Visibility: &private}, nil},
		{a.ID, store.Change{Visibility: &private}, store.ErrNotPublic},
		{a.ID, store.Change{Visibility: &private, Domain: &empty}, store.ErrNotPublic},
		{b.ID, store.Change{Domain: &domain}, store.ErrDomainTaken},
		{p.ID, store.Change{Domain: &domain}, store.ErrDomainNotPublic},
		{p.ID, store.Change{Domain: &other, Visibility: &private}, store.ErrDomainNotPublic},
	}
	for _, tc := range cases {
		before := snapshot(t, s)
		tc.change.Listed, tc.change.Ref, tc.change.Path = &listed, &ref, &configured
		got, fields, err := s.Update(ctx, tc.id, tc.change)
		equal(t, got, store.Site{})
		equal(t, fields, []string(nil))
		if tc.refusal == nil {
			catalog(t, err)
		} else {
			checkErr(t, err, tc.refusal)
		}
		equal(t, snapshot(t, s), before)
	}
	equal(t, s.ClearApex(ctx), nil)
	for _, change := range []store.Change{{Visibility: &private}, {Visibility: &private, Domain: &other}, {Visibility: &private, Domain: &domain}} {
		before := snapshot(t, s)
		got, fields, err := s.Update(ctx, a.ID, change)
		equal(t, got, store.Site{})
		equal(t, fields, []string(nil))
		checkErr(t, err, store.ErrDomainNotPublic)
		equal(t, snapshot(t, s), before)
	}
	p, _, err = s.Update(ctx, p.ID, store.Change{Visibility: &public, Domain: &other})
	equal(t, err, nil)
	equal(t, p.Domain, other)
	a, fields, err := s.Update(ctx, a.ID, store.Change{Visibility: &private, Domain: &empty})
	equal(t, err, nil)
	equal(t, fields, []string{"visibility", "domain"})
	equal(t, a.Visibility, private)
	equal(t, a.Domain, "")
}

func TestDeleteFreesDomain(t *testing.T) {
	// R-II1U-8WAS
	for _, reopen := range []bool{false, true} {
		t.Run(fmt.Sprint(reopen), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			s, d := openAt(t, path, store.Config{})
			a := create(t, s, draft("first"))
			b := create(t, s, draft("second"))
			domain := "freed.example"
			_, _, err := s.Update(ctx, a.ID, store.Change{Domain: &domain})
			equal(t, err, nil)
			_, err = s.Delete(ctx, a.ID)
			equal(t, err, nil)
			if reopen {
				equal(t, d.Close(), nil)
				s, _ = openAt(t, path, store.Config{})
			}
			got, err := s.ByDomain(ctx, domain)
			equal(t, got, store.Site{})
			checkErr(t, err, store.ErrNotFound)
			got, _, err = s.Update(ctx, b.ID, store.Change{Domain: &domain})
			equal(t, err, nil)
			equal(t, got.Domain, domain)
		})
	}
}

func TestDomainMigrationPreservesRows(t *testing.T) {
	// R-I9IJ-KI3X
	path := filepath.Join(t.TempDir(), "db")
	migrations := fstest.MapFS{}
	for _, name := range []string{"0001_catalog.sql", "0002_listed.sql", "0003_backfill_listed.sql"} {
		contents, err := fs.ReadFile(sites.Migrations(), name)
		equal(t, err, nil)
		migrations[name] = &fstest.MapFile{Data: contents}
	}
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: migrations, Now: func() time.Time { return stamp }})
	equal(t, err, nil)
	t.Cleanup(func() { _ = d.Close() })
	var records []store.Site
	var payloads []string
	for i, listed := range []bool{false, true} {
		x := store.Site{ID: fmt.Sprintf("sit_%016x", i), Name: fmt.Sprint("site", i), Slug: fmt.Sprint("slug", i), Owner: "alice", Repo: "repo", Ref: "main", Listed: listed, Visibility: store.Public, Created: stamp.UTC().Truncate(time.Second)}
		payload, err := json.Marshal(x)
		equal(t, err, nil)
		records = append(records, x)
		payloads = append(payloads, string(payload))
		equal(t, d.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload,listed) VALUES(?,?,?,?,?,?)", x.ID, x.Name, x.Slug, x.Owner, string(payload), x.Listed)
			return err
		}), nil)
	}
	equal(t, d.Close(), nil)
	s, handle := openAt(t, path, store.Config{})
	for i, x := range records {
		equal(t, handle.Read(ctx, func(tx *sql.Tx) error {
			var id, name, slug, owner, payload string
			var listed bool
			var domain sql.NullString
			if err := tx.QueryRowContext(ctx, "SELECT id,name,slug,owner,payload,listed,domain FROM sites WHERE id=?", x.ID).Scan(&id, &name, &slug, &owner, &payload, &listed, &domain); err != nil {
				return err
			}
			equal(t, []string{id, name, slug, owner, payload}, []string{x.ID, x.Name, x.Slug, x.Owner, payloads[i]})
			equal(t, listed, x.Listed)
			equal(t, domain, sql.NullString{})
			return nil
		}), nil)
		assertDomainReads(t, s, x)
	}
}

func TestDomainColumnSurvivesPreviousReleaseWrites(t *testing.T) {
	// R-IBYC-C1LB R-IEE5-3L2P
	for _, member := range []string{"Domain", "domain", "DOMAIN", "DoMaIn"} {
		for _, column := range []string{"", "actual.example"} {
			t.Run(member+column, func(t *testing.T) {
				s, d := openAt(t, filepath.Join(t.TempDir(), "db"), store.Config{})
				x := create(t, s, draft("compat"))
				if column != "" {
					var err error
					x, _, err = s.Update(ctx, x.ID, store.Change{Domain: &column})
					equal(t, err, nil)
				}
				payload, err := json.Marshal(x)
				equal(t, err, nil)
				var members map[string]any
				equal(t, json.Unmarshal(payload, &members), nil)
				members[member] = "forged.example"
				payload, err = json.Marshal(members)
				equal(t, err, nil)
				equal(t, d.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload,listed) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, listed=excluded.listed", x.ID, x.Name, x.Slug, x.Owner, string(payload), x.Listed)
					return err
				}), nil)
				assertDomainReads(t, s, x)
				inserted := x
				inserted.ID, inserted.Name, inserted.Slug, inserted.Owner, inserted.Domain = "sit_000000000000fffe", "old-insert", "old-insert", "bob", ""
				members["ID"], members["Name"], members["Slug"], members["Owner"] = inserted.ID, inserted.Name, inserted.Slug, inserted.Owner
				insertPayload, err := json.Marshal(members)
				equal(t, err, nil)
				equal(t, d.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload,listed) VALUES(?,?,?,?,?,?)", inserted.ID, inserted.Name, inserted.Slug, inserted.Owner, string(insertPayload), inserted.Listed)
					return err
				}), nil)
				assertDomainReads(t, s, inserted)
				members["ID"], members["Name"], members["Slug"], members["Owner"] = x.ID, x.Name, x.Slug, x.Owner
				if column != "" {
					// The previous release ignores the new column, even when it makes the site private.
					delete(members, member)
					members["Visibility"] = store.Private
					payload, err = json.Marshal(members)
					equal(t, err, nil)
					equal(t, d.Write(ctx, func(tx *sql.Tx) error {
						_, err := tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload,listed) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, listed=excluded.listed", x.ID, x.Name, x.Slug, x.Owner, string(payload), x.Listed)
						return err
					}), nil)
					x.Visibility = store.Private
					assertDomainReads(t, s, x)
					before := snapshot(t, s)
					_, _, err = s.Update(ctx, x.ID, store.Change{})
					checkErr(t, err, store.ErrDomainNotPublic)
					equal(t, snapshot(t, s), before)
				}
			})
		}
	}
}

func TestConcurrentDomainInvariants(t *testing.T) {
	// R-IJ9Q-MO1H
	for range 12 {
		s := open(t, store.Config{})
		a := create(t, s, draft("first"))
		b := create(t, s, draft("second"))
		domain := "race.example"
		start := make(chan struct{})
		var wg sync.WaitGroup
		var firstErr, secondErr error
		wg.Go(func() { <-start; _, _, firstErr = s.Update(ctx, a.ID, store.Change{Domain: &domain}) })
		wg.Go(func() { <-start; _, _, secondErr = s.Update(ctx, b.ID, store.Change{Domain: &domain}) })
		close(start)
		wg.Wait()
		if firstErr == nil {
			checkErr(t, secondErr, store.ErrDomainTaken)
		} else {
			checkErr(t, firstErr, store.ErrDomainTaken)
			equal(t, secondErr, nil)
		}
		held, err := s.ByDomain(ctx, domain)
		equal(t, err, nil)
		if held.ID != a.ID && held.ID != b.ID {
			t.Fatal("unknown domain holder")
		}
		start = make(chan struct{})
		wg.Go(func() { <-start; _, firstErr = s.Delete(ctx, held.ID) })
		other := a
		if other.ID == held.ID {
			other = b
		}
		wg.Go(func() { <-start; _, _, secondErr = s.Update(ctx, other.ID, store.Change{Domain: &domain}) })
		close(start)
		wg.Wait()
		equal(t, firstErr, nil)
		if secondErr != nil {
			checkErr(t, secondErr, store.ErrDomainTaken)
		}
		rows, err := s.List(ctx, "alice", "", 10)
		equal(t, err, nil)
		equal(t, len(rows), 1)
	}
	for range 12 {
		s := open(t, store.Config{})
		x := create(t, s, draft("public"))
		start := make(chan struct{})
		var wg sync.WaitGroup
		var domainErr, privateErr error
		domain, private := "race.example", store.Private
		wg.Go(func() { <-start; _, _, domainErr = s.Update(ctx, x.ID, store.Change{Domain: &domain}) })
		wg.Go(func() { <-start; _, _, privateErr = s.Update(ctx, x.ID, store.Change{Visibility: &private}) })
		close(start)
		wg.Wait()
		if domainErr == nil {
			checkErr(t, privateErr, store.ErrDomainNotPublic)
		} else {
			checkErr(t, domainErr, store.ErrDomainNotPublic)
			equal(t, privateErr, nil)
		}
		got, err := s.Find(ctx, x.Owner, x.Name)
		equal(t, err, nil)
		if got.Domain != "" && got.Visibility != store.Public {
			t.Fatal("private domain holder")
		}
	}
}

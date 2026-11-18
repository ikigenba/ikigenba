package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

// R-HDRA-ODSB
func TestEmptyDatabase(t *testing.T) {
	_, s, now := fixture(t)
	eq(t, value(t, result(s.ListHosts(ctx, 0, 1))), []store.Host{})
	eq(t, value(t, result(s.ListHostsBefore(ctx, 5, 1))), []store.Host{})
	eq(t, value(t, result(s.LastHosts(ctx, 1))), []store.Host{})
	eq(t, value(t, result(s.ListAccounts(ctx, "", 1))), []store.Account{})
	eq(t, value(t, result(s.ListAccountsBefore(ctx, "missing", 1))), []store.Account{})
	eq(t, value(t, result(s.LastAccounts(ctx, 1))), []store.Account{})
	eq(t, value(t, result(s.ListUsers(ctx, "", "", "", 1))), []store.User{})
	eq(t, value(t, result(s.ListUsersBefore(ctx, "missing", "missing", "missing", 1))), []store.User{})
	eq(t, value(t, result(s.LastUsers(ctx, "", 1))), []store.User{})
	eq(t, value(t, result(s.Runnable(ctx, *now, "", 1))), []store.Operation{})
	eq(t, value(t, result(s.MarkSilent(ctx, *now, "", 1))), []store.Host{})
	lookup := []observation{
		observe(s.Host(ctx, "missing")), observe(s.HostByInstance(ctx, "missing")), observe(s.Operation(ctx, "missing")),
		observe(s.Account(ctx, "missing")), observe(s.User(ctx, "missing", "missing")), observe(s.Desired(ctx, "missing")),
		observe(s.Report(ctx, "missing")), observe(s.Host(ctx, "")), observe(s.Account(ctx, "")), observe(s.User(ctx, "", "")),
	}
	for _, call := range lookup {
		call.assertZero(t, store.ErrNotFound)
	}
}

type observation struct {
	out, zero any
	err       error
}

func observe[T any](v T, err error) observation {
	var zero T
	return observation{v, zero, err}
}

func observe2[T, U any](v T, u U, err error) observation {
	type pair struct {
		first  T
		second U
	}
	return observe(pair{v, u}, err)
}

func noOutput(err error) observation { return observe(struct{}{}, err) }

func (o observation) assertZero(t *testing.T, want error) {
	t.Helper()
	checkErr(t, o.err, want)
	eq(t, o.out, o.zero)
}

func listCalls(c context.Context, s *store.Store, limit int) []observation {
	return []observation{
		observe(s.ListHosts(c, 0, limit)), observe(s.ListHostsBefore(c, 2, limit)), observe(s.LastHosts(c, limit)),
		observe(s.ListAccounts(c, "", limit)), observe(s.ListAccountsBefore(c, "z", limit)), observe(s.LastAccounts(c, limit)),
		observe(s.ListUsers(c, "", "", "", limit)), observe(s.ListUsersBefore(c, "", "z", "z", limit)),
		observe(s.LastUsers(c, "", limit)), observe(s.OperationsOf(c, "host01", "", limit)),
		observe(s.Runnable(c, baseTime, "", limit)), observe(s.MarkSilent(c, baseTime, "", limit)),
		observe(s.PruneGrows(c, baseTime, limit)),
	}
}

// R-JA1O-3JB7 R-JB9K-HB1W
func TestInvalidInput(t *testing.T) {
	_, s, _ := fixture(t)
	for _, limit := range []int{0, -1, paging.MaxLimit + 2} {
		for _, call := range listCalls(ctx, s, limit) {
			call.assertZero(t, store.ErrUnreachable)
		}
	}
	for _, call := range listCalls(ctx, s, paging.MaxLimit+1) {
		checkErr(t, call.err, nil)
	}
	h, op := create(t, s)
	a, _ := place(t, s, "account", h.Name)
	key := firstKey
	key.Line = ""
	for _, call := range []observation{
		observe2(s.CreateHost(ctx, store.HostSpec{Type: "type", RootGB: -1, DataGB: 10}, caller)),
		observe2(s.StartHostOp(ctx, h.Name, "", store.Target{}, caller)),
		observe2(s.RetryHost(ctx, h.Name, (store.Operation{}).Caller)), observe(s.Operation(ctx, "")),
		observe(s.OperationsOf(ctx, "", "", 1)), noOutput(s.RecordStep(ctx, op.ID, "", 1, baseTime)),
		noOutput(s.RecordStep(ctx, op.ID, "step", 0, baseTime)), noOutput(s.Advance(ctx, op.ID, store.Update{DataGB: -1})),
		noOutput(s.Defer(ctx, "", baseTime, 1)), noOutput(s.Fail(ctx, "", "failed")), noOutput(s.Finish(ctx, "", store.Update{})),
		noOutput(s.RecordChange(ctx, op.ID, a.Slug, "")), observe2(s.PlaceAccount(ctx, "", h.Name, caller)),
		observe2(s.RemoveAccount(ctx, "", caller)), observe(s.AccountsOn(ctx, "")),
		observe(s.AddUser(ctx, a.Slug, "user", key, caller)),
		observe(s.AddKey(ctx, a.Slug, "user", key)), observe2(s.RemoveKey(ctx, a.Slug, "user", store.Key{})),
		observe(s.Lock(ctx, a.Slug, "")), observe(s.Unlock(ctx, a.Slug, "")), observe(s.Delete(ctx, a.Slug, "")),
		observe(s.UsersOf(ctx, "")), observe(s.Desired(ctx, "")), observe(s.RecordReport(ctx, "", protocol.Report{}, baseTime)),
		observe(s.Report(ctx, "")), observe(s.NoteStray(ctx, "resource", "", "", baseTime)), observe(s.Explains(ctx, "")),
		noOutput(s.NoteGrow(ctx, "", baseTime)),
		observe(s.Allow(ctx, "", "192.0.2.7")), observe(s.Disallow(ctx, "", "192.0.2.7")),
		observe(s.Unban(ctx, "", "192.0.2.7")), observe(s.UnbanRunning(ctx, "")),
	} {
		call.assertZero(t, store.ErrUnreachable)
	}
}

func exercise(c context.Context, s *store.Store, host string, opID string) []observation {
	createTwo := observe2(s.CreateHost(c, spec, caller))
	out := []observation{
		createTwo, noOutput(s.RecordStep(c, opID, "prepare", 2, baseTime)),
		noOutput(s.Advance(c, opID, store.Update{InstanceID: "instance", DataVolume: "volume"})),
		noOutput(s.Defer(c, opID, baseTime.Add(time.Second), 1)), observe(s.NextTry(c)), observe(s.Host(c, host)),
		observe(s.HostByInstance(c, "instance")), observe(s.Operation(c, opID)),
		noOutput(s.Fail(c, opID, "retryable")), observe2(s.RetryHost(c, host, caller)), noOutput(s.Finish(c, opID, store.Update{})),
		observe(s.Allow(c, host, "198.51.100.0/24")), observe(s.Allow(c, host, "192.0.2.7")),
		observe(s.Disallow(c, host, "192.0.2.7")), observe(s.Unban(c, host, "192.0.2.7")),
		observe(s.UnbanRunning(c, "198.51.100.7")),
		observe2(s.StartHostOp(c, host, store.OpStop, store.Target{}, caller)),
		observe2(s.PlaceAccount(c, "business", host, caller)), observe(s.Account(c, "business")), observe(s.AccountsOn(c, host)),
	}
	out = append(out, listCalls(c, s, 3)...)
	out = append(out,
		observe(s.AddUser(c, "business", "person", firstKey, caller)), observe(s.User(c, "business", "person")),
		observe(s.UsersOf(c, "business")), observe(s.AddKey(c, "business", "person", firstKey)),
		observe2(s.RemoveKey(c, "business", "person", firstKey)), observe(s.Lock(c, "business", "person")),
		observe(s.Unlock(c, "business", "person")), observe(s.Lock(c, "business", "person")), observe(s.Delete(c, "business", "person")),
		observe(s.Desired(c, host)), observe(s.RecordReport(c, host, protocol.Report{Applied: 100}, baseTime)),
		observe(s.Report(c, host)), observe(s.NoteStray(c, "resource", "instance", host, baseTime)),
		observe(s.Explains(c, "instance")), noOutput(s.NoteGrow(c, "volume", baseTime)),
		noOutput(s.RecordChange(c, "op_0000000000000004", "business", "change")),
		observe2(s.RemoveAccount(c, "business", caller)),
	)
	return out
}

// R-JCHG-V2SL
func TestDatabaseFailuresTakePrecedence(t *testing.T) {
	d, s, _ := fixture(t)
	h, op := create(t, s)
	a, _ := place(t, s, "business", h.Name)
	value(t, result(s.AddUser(ctx, a.Slug, "person", firstKey, caller)))
	initial := value(t, result(s.Desired(ctx, h.Name)))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	check := func(c context.Context, cause error) {
		t.Helper()
		for _, call := range exercise(c, s, h.Name, op.ID) {
			call.assertZero(t, store.ErrUnreachable)
			if cause != nil {
				checkErr(t, call.err, cause)
			}
		}
		observation := observe(s.Host(c, ""))
		observation.assertZero(t, store.ErrUnreachable)
		if cause != nil {
			checkErr(t, observation.err, cause)
		}
	}
	check(cancelled, context.Canceled)
	d.SetFailing(true)
	check(cancelled, context.Canceled)
	check(ctx, nil)
	d.SetFailing(false)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), initial)
	eq(t, value(t, result(s.Operation(ctx, op.ID))), op)
	checkErr(t, d.Close(), nil)
	check(ctx, nil)
}

func TestSnapshotKeepsItsViewAndDoesNotChangeStoreReads(t *testing.T) {
	d, s, _ := fixture(t)
	h, _ := create(t, s)
	err := store.ReadSnapshot(ctx, s, func(snapshot *store.Snapshot) error {
		eq(t, value(t, result(snapshot.Host(ctx, h.Name))), h)
		place(t, s, "business", h.Name)
		eq(t, value(t, result(snapshot.Host(ctx, h.Name))), h)
		eq(t, value(t, result(s.Host(ctx, h.Name))).Accounts, 1)
		d.SetFailing(true)
		defer d.SetFailing(false)
		observe(s.Host(ctx, h.Name)).assertZero(t, store.ErrUnreachable)
		return nil
	})
	checkErr(t, err, nil)
}

// R-PDCM-AJ5R R-JDPD-8UJA R-JG56-0E0O R-2AZ8-MC5D
func TestAdditiveMigrations(t *testing.T) {
	migrations := accounts.Migrations()
	old := value(t, result(fs.ReadFile(migrations, "0001_accounts.sql")))
	eq(t, string(old), "CREATE TABLE groups (slug TEXT PRIMARY KEY, created TEXT NOT NULL, created_user_id TEXT NOT NULL) WITHOUT ROWID;\n"+
		"CREATE TABLE users (name TEXT PRIMARY KEY, created TEXT NOT NULL, created_user_id TEXT NOT NULL) WITHOUT ROWID;\n")
	newSchema := value(t, result(fs.ReadFile(migrations, "0002_hosts.sql")))
	stmts := strings.Split(string(newSchema), ";")
	if len(stmts) <= 1 {
		t.Fatal("host migration has no statements")
	}
	createStatement := regexp.MustCompile(`^CREATE (TABLE|INDEX|UNIQUE INDEX)\b`)
	legacyTable := regexp.MustCompile(`\b(groups|users)\b`)
	for _, statement := range stmts[:len(stmts)-1] {
		if !createStatement.MatchString(strings.TrimSpace(statement)) || legacyTable.MatchString(statement) {
			t.Fatalf("migration statement is not additive: %q", statement)
		}
	}
	eq(t, strings.TrimSpace(stmts[len(stmts)-1]), "")

	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	_, baseline := open(t, filepath.Join(t.TempDir(), "plain.db"), migrations, cfg)
	cfg.Rand = new(counterRandom)
	d, withoutLegacy := open(t, filepath.Join(t.TempDir(), "dropped.db"), migrations, cfg)
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DROP TABLE groups; DROP TABLE users;")
		return err
	}), nil)
	var extra strings.Builder
	bansSchema := value(t, result(fs.ReadFile(migrations, "0003_bans.sql")))
	for _, table := range tableNames(string(newSchema) + string(bansSchema)) {
		extra.WriteString("ALTER TABLE " + table + " ADD COLUMN future_note TEXT;\n")
	}
	expanded := fstest.MapFS{"0001_accounts.sql": {Data: old}, "0002_hosts.sql": {Data: newSchema},
		"0003_bans.sql": {Data: bansSchema}, "0004_expand.sql": {Data: []byte(extra.String())}}
	cfg.Rand = new(counterRandom)
	_, withExtra := open(t, filepath.Join(t.TempDir(), "expand.db"), expanded, cfg)
	for _, s := range []*store.Store{baseline, withoutLegacy, withExtra} {
		create(t, s)
	}
	const host, opID = "host01", "op_0000000000000001"
	watch, release := baseline.Watch(host)
	defer release()
	want := exercise(ctx, baseline, host, opID)
	for _, call := range want {
		checkErr(t, call.err, nil)
	}
	closed(t, watch, true)
	for _, s := range []*store.Store{withoutLegacy, withExtra} {
		w, cancel := s.Watch(host)
		eq(t, exercise(ctx, s, host, opID), want)
		closed(t, w, true)
		cancel()
	}
}

func tableNames(schema string) []string {
	createTable := regexp.MustCompile(`\bCREATE TABLE ([a-z_][a-z_0-9]*)\b`)
	var names []string
	for _, match := range createTable.FindAllStringSubmatch(schema, -1) {
		names = append(names, match[1])
	}
	return names
}

// R-29RC-8KEO
func TestBansMigrationExpandsPreviousSchema(t *testing.T) {
	migrations := accounts.Migrations()
	groups := value(t, result(fs.ReadFile(migrations, "0001_accounts.sql")))
	hosts := value(t, result(fs.ReadFile(migrations, "0002_hosts.sql")))
	bans := value(t, result(fs.ReadFile(migrations, "0003_bans.sql")))
	previous := fstest.MapFS{"0001_accounts.sql": {Data: groups}, "0002_hosts.sql": {Data: hosts}}
	oldNames := tableNames(string(groups) + string(hosts))
	oldTable := regexp.MustCompile(`\b(` + strings.Join(oldNames, "|") + `)\b`)
	createStatement := regexp.MustCompile(`^CREATE (TABLE|INDEX|UNIQUE INDEX)\b`)
	for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(string(bans)), ";"), ";") {
		if !createStatement.MatchString(strings.TrimSpace(statement)) || oldTable.MatchString(statement) {
			t.Fatalf("migration statement is not additive: %q", statement)
		}
	}

	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	path := filepath.Join(t.TempDir(), "previous.db")
	d, _ := open(t, path, previous, cfg)
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO counters (name, value) VALUES ('host', 7)")
		return err
	}), nil)
	checkErr(t, d.Close(), nil)
	d, expanded := open(t, path, migrations, cfg)
	h, _ := create(t, expanded)
	eq(t, h.Seq, int64(8))
	eq(t, h.Allowlist, []string{})
	eq(t, value(t, result(expanded.Desired(ctx, h.Name))).Unban, []string{})
	checkErr(t, d.Close(), nil)

	d, _ = open(t, path, previous, cfg)
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE counters SET value = 11 WHERE name = 'host'")
		return err
	}), nil)
	checkErr(t, d.Close(), nil)
	_, expanded = open(t, path, migrations, cfg)
	eq(t, value(t, result(expanded.Host(ctx, h.Name))), h)
	h, _ = create(t, expanded)
	eq(t, h.Seq, int64(12))
}

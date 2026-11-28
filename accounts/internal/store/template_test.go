package store_test

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-VFH9-BRZW.
var (
	_ struct{ URL, Ref string }                                                                   = store.TemplateSettings{}
	_ func() store.TemplateSettings                                                               = store.DefaultTemplateSettings
	_ func(*store.Store, context.Context) (store.TemplateSettings, error)                         = (*store.Store).TemplateSettings
	_ func(*store.Store, context.Context, *string, *string) (store.TemplateSettings, bool, error) = (*store.Store).SetTemplateSettings
)

// R-VHX2-3BHA R-VJ4Y-H37Z.
func TestTemplateSettingsRead(t *testing.T) {
	d, s, _ := fixture(t)
	defaults := store.TemplateSettings{URL: protocol.DefaultTemplateURL, Ref: protocol.DefaultTemplateRef}
	eq(t, store.DefaultTemplateSettings(), defaults)
	eq(t, store.DefaultTemplateSettings(), defaults)
	eq(t, value(t, result(s.TemplateSettings(ctx))), defaults)

	custom := store.TemplateSettings{URL: "https://git.example.test/team", Ref: "release/2"}
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO template_settings (scope, url, ref) VALUES ('', ?, ?)", custom.URL, custom.Ref)
		return err
	}), nil)
	eq(t, value(t, result(s.TemplateSettings(ctx))), custom)
	eq(t, store.DefaultTemplateSettings(), defaults)
}

// R-VKCU-UUYO R-VLKR-8MPD.
func TestTemplateSettingsUpdateAndDesired(t *testing.T) {
	d, s, _ := fixture(t)
	host, _ := create(t, s)
	other, _ := create(t, s)
	watch, release := s.Watch(host.Name)
	defer release()
	before := value(t, result(s.Desired(ctx, host.Name)))
	eq(t, before.TemplateURL, protocol.DefaultTemplateURL)
	eq(t, before.TemplateRef, protocol.DefaultTemplateRef)

	updated := store.TemplateSettings{URL: protocol.DefaultTemplateURL, Ref: "v2"}
	got, changed, err := s.SetTemplateSettings(ctx, nil, &updated.Ref)
	checkErr(t, err, nil)
	eq(t, changed, true)
	eq(t, got, updated)
	eq(t, value(t, result(s.TemplateSettings(ctx))), updated)
	after := value(t, result(s.Desired(ctx, host.Name)))
	eq(t, after.TemplateURL, updated.URL)
	eq(t, after.TemplateRef, updated.Ref)
	eq(t, after.Version, before.Version)
	eq(t, value(t, result(s.Host(ctx, other.Name))).Version, other.Version)
	select {
	case <-watch:
		t.Fatal("setting update notified a version watcher")
	default:
	}

	got, changed, err = s.SetTemplateSettings(ctx, nil, &updated.Ref)
	checkErr(t, err, nil)
	eq(t, changed, false)
	eq(t, got, updated)
	updated.URL = "https://git.example.test/other"
	got, changed, err = s.SetTemplateSettings(ctx, &updated.URL, nil)
	checkErr(t, err, nil)
	eq(t, changed, true)
	eq(t, got, updated)
	after = value(t, result(s.Desired(ctx, host.Name)))
	eq(t, after.TemplateURL, updated.URL)
	eq(t, after.TemplateRef, updated.Ref)
	eq(t, after.Version, before.Version)
	badURL, badRef := "http://git.example.test", "a..b"
	for _, args := range [][2]*string{{&badURL, nil}, {nil, &badRef}, {&updated.URL, &badRef}} {
		got, changed, err = s.SetTemplateSettings(ctx, args[0], args[1])
		checkErr(t, err, store.ErrUnreachable)
		eq(t, changed, false)
		eq(t, got, store.TemplateSettings{})
		eq(t, value(t, result(s.TemplateSettings(ctx))), updated)
	}

	d.SetFailing(true)
	got, changed, err = s.SetTemplateSettings(ctx, nil, nil)
	checkErr(t, err, store.ErrUnreachable)
	eq(t, changed, false)
	eq(t, got, store.TemplateSettings{})
	got, err = s.TemplateSettings(ctx)
	checkErr(t, err, store.ErrUnreachable)
	eq(t, got, store.TemplateSettings{})
	d.SetFailing(false)
	eq(t, value(t, result(s.TemplateSettings(ctx))), updated)
}

// R-VKCU-UUYO.
func TestSetTemplateSettingsKeepsOnlyTheEmptyScopeOnChange(t *testing.T) {
	d, s, _ := fixture(t)
	defaults := store.DefaultTemplateSettings()
	insertSpare := func() (inserted int64) {
		t.Helper()
		checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
			r, err := tx.ExecContext(ctx, `INSERT INTO template_settings (scope, url, ref) VALUES ('spare', ?, ?)
				ON CONFLICT (scope) DO NOTHING`, defaults.URL, "spare")
			if err != nil {
				return err
			}
			inserted, err = r.RowsAffected()
			return err
		}), nil)
		return inserted
	}
	eq(t, insertSpare(), int64(1))
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER refuse_template_setting BEFORE INSERT ON template_settings
			WHEN NEW.scope = '' BEGIN SELECT RAISE(ABORT, 'test refused template setting'); END`)
		return err
	}), nil)

	got, changed, err := s.SetTemplateSettings(ctx, nil, nil)
	checkErr(t, err, nil)
	eq(t, got, defaults)
	eq(t, changed, false)
	eq(t, insertSpare(), int64(0))

	ref := "replacement"
	got, changed, err = s.SetTemplateSettings(ctx, nil, &ref)
	checkErr(t, err, store.ErrUnreachable)
	if !strings.Contains(err.Error(), "test refused template setting") {
		t.Fatalf("SetTemplateSettings error = %v, want trigger refusal", err)
	}
	eq(t, got, store.TemplateSettings{})
	eq(t, changed, false)
	eq(t, insertSpare(), int64(0))
	eq(t, value(t, result(s.TemplateSettings(ctx))), defaults)

	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DROP TRIGGER refuse_template_setting")
		return err
	}), nil)
	got, changed, err = s.SetTemplateSettings(ctx, nil, &ref)
	checkErr(t, err, nil)
	eq(t, got, store.TemplateSettings{URL: defaults.URL, Ref: ref})
	eq(t, changed, true)
	eq(t, value(t, result(s.TemplateSettings(ctx))), got)
	eq(t, insertSpare(), int64(1))
}

// R-VMSN-MEG2.
func TestRecordReportKeepsTheFirstTemplateCommitForEachLocalID(t *testing.T) {
	d, s, now := fixture(t)
	one, _ := create(t, s)
	two, _ := create(t, s)
	a, _ := place(t, s, "acme", one.Name)
	b, _ := place(t, s, "other", two.Name)
	u := value(t, result(s.AddUser(ctx, a.Slug, "daffy", firstKey, caller)))
	v := value(t, result(s.AddUser(ctx, b.Slug, "bugs", firstKey, caller)))
	eq(t, a.Template, "")
	eq(t, u.Template, "")

	seedCommit := strings.Repeat("a", 40)
	homeCommit := strings.Repeat("b", 64)
	otherCommit := strings.Repeat("c", 40)
	report := protocol.Report{
		Protocol: protocol.Version,
		Seeds: []protocol.Seed{
			{GID: a.GID, Commit: strings.Repeat("A", 40)}, {GID: a.GID, Commit: seedCommit},
			{GID: b.GID, Commit: seedCommit}, {GID: b.GID + u.UID + v.UID, Commit: seedCommit},
		},
		Homes: []protocol.Home{
			{UID: u.UID, Commit: strings.Repeat("c", 41)}, {UID: u.UID, Commit: homeCommit},
			{UID: v.UID, Commit: homeCommit}, {UID: b.GID + u.UID + v.UID, Commit: homeCommit},
		},
	}
	_, err := s.RecordReport(ctx, one.Name, protocol.Report{Protocol: protocol.Version, Attempted: 1, Applied: 1}, *now)
	checkErr(t, err, nil)
	priorReport := value(t, result(s.Report(ctx, one.Name)))
	priorHost := value(t, result(s.Host(ctx, one.Name)))
	priorAccount := value(t, result(s.Account(ctx, a.Slug)))
	priorUser := value(t, result(s.User(ctx, a.Slug, u.Name)))
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER refuse_home_commit BEFORE UPDATE OF template ON ssh_users
			WHEN OLD.name = 'daffy' AND NEW.template <> ''
			BEGIN SELECT RAISE(ABORT, 'test refused home commit'); END`)
		return err
	}), nil)
	_, err = s.RecordReport(ctx, one.Name, report, now.Add(time.Minute))
	checkErr(t, err, store.ErrUnreachable)
	if !strings.Contains(err.Error(), "test refused home commit") {
		t.Fatalf("RecordReport error = %v, want trigger refusal", err)
	}
	eq(t, value(t, result(s.Report(ctx, one.Name))), priorReport)
	eq(t, value(t, result(s.Host(ctx, one.Name))), priorHost)
	eq(t, value(t, result(s.Account(ctx, a.Slug))), priorAccount)
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))), priorUser)
	checkErr(t, d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DROP TRIGGER refuse_home_commit")
		return err
	}), nil)

	_, err = s.RecordReport(ctx, one.Name, report, *now)
	checkErr(t, err, nil)
	saved := value(t, result(s.Report(ctx, one.Name)))
	eq(t, saved.Body.Seeds, report.Seeds)
	eq(t, saved.Body.Homes, report.Homes)
	_, err = s.RecordReport(ctx, one.Name, protocol.Report{
		Seeds: []protocol.Seed{{GID: a.GID, Commit: otherCommit}},
		Homes: []protocol.Home{{UID: u.UID, Commit: otherCommit}},
	}, *now)
	checkErr(t, err, nil)

	eq(t, value(t, result(s.Account(ctx, a.Slug))).Template, seedCommit)
	eq(t, value(t, result(s.Account(ctx, b.Slug))).Template, "")
	listAccounts := value(t, result(s.ListAccounts(ctx, "", 10)))
	eq(t, listAccounts[0].Template, seedCommit)
	eq(t, listAccounts[1].Template, "")
	eq(t, value(t, result(s.AccountsOn(ctx, one.Name)))[0].Template, seedCommit)
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))).Template, homeCommit)
	eq(t, value(t, result(s.User(ctx, b.Slug, v.Name))).Template, "")
	eq(t, value(t, result(s.UserNamed(ctx, u.Name))).Template, homeCommit)
	eq(t, value(t, result(s.ActiveUserByUID(ctx, one.Name, u.UID))).Template, homeCommit)
	eq(t, value(t, result(s.ListUsers(ctx, a.Slug, "", "", 10)))[0].Template, homeCommit)
	eq(t, value(t, result(s.UsersOf(ctx, a.Slug)))[0].Template, homeCommit)
	admin, _, err := s.SetAdmin(ctx, a.Slug, u.Name, true)
	checkErr(t, err, nil)
	eq(t, admin.Template, homeCommit)
	eq(t, value(t, result(s.UsersAs(ctx, u.Name)))[0].Template, homeCommit)
	eq(t, value(t, result(s.Lock(ctx, a.Slug, u.Name))).Template, homeCommit)
	deleted := value(t, result(s.Delete(ctx, a.Slug, u.Name)))
	eq(t, deleted.Template, homeCommit)
	_, err = s.RecordReport(ctx, one.Name, protocol.Report{Protocol: protocol.Version, Applied: deleted.Version}, *now)
	checkErr(t, err, nil)
	removed, _ := remove(t, s, a.Slug)
	eq(t, removed.Template, seedCommit)
}

// R-VP8G-DXXG.
func TestTemplateMigrationContents(t *testing.T) {
	raw := value(t, result(fs.ReadFile(accounts.Migrations(), "0009_team_template.sql")))
	eq(t, string(raw), "CREATE TABLE template_settings (scope TEXT PRIMARY KEY, url TEXT NOT NULL, ref TEXT NOT NULL) WITHOUT ROWID;\n"+
		"ALTER TABLE accounts ADD COLUMN template TEXT NOT NULL DEFAULT '';\n"+
		"ALTER TABLE ssh_users ADD COLUMN template TEXT NOT NULL DEFAULT '';\n")
}

// R-VQGC-RPO5.
func TestTemplateMigrationUpgradesPreviousRecords(t *testing.T) {
	migrations := accounts.Migrations()
	earlier, _ := earlierMigrations(t)
	for _, name := range []string{"0006_kept_volumes.sql", "0007_host_backups.sql", "0008_weekly_retention.sql"} {
		earlier[name] = &fstest.MapFile{Data: value(t, result(fs.ReadFile(migrations, name)))}
	}
	path := filepath.Join(t.TempDir(), "record.db")
	cfg := store.Config{Now: func() time.Time { return baseTime }, Rand: new(counterRandom)}
	d, s := open(t, path, earlier, cfg)
	host, op := create(t, s)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	a, op := place(t, s, "acme", host.Name)
	checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	u := value(t, result(s.AddUser(ctx, a.Slug, "daffy", firstKey, caller)))
	checkErr(t, d.Close(), nil)

	_, s = open(t, path, migrations, cfg)
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Template, "")
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))).Template, "")
	eq(t, value(t, result(s.TemplateSettings(ctx))), store.DefaultTemplateSettings())
	commit := strings.Repeat("f", 64)
	_, err := s.RecordReport(ctx, host.Name, protocol.Report{
		Seeds: []protocol.Seed{{GID: a.GID, Commit: commit}},
		Homes: []protocol.Home{{UID: u.UID, Commit: commit}},
	}, baseTime)
	checkErr(t, err, nil)
	eq(t, value(t, result(s.Account(ctx, a.Slug))).Template, commit)
	eq(t, value(t, result(s.User(ctx, a.Slug, u.Name))).Template, commit)
}

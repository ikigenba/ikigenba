package pages_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

var otherBackupSettings = store.BackupSettings{
	EveryMinutes: 30, Hourly: 16, Daily: 5, Weekly: 9, Monthly: 2, WindowDays: 50,
	FilesStaleMinutes: 90, DatabaseStaleMinutes: 12, CheckDays: 6,
}

func writePageBackupSettings(t *testing.T, f *fixture, b store.BackupSettings) {
	t.Helper()
	ctx := t.Context()
	if err := f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO backup_settings
			(scope, every_minutes, hourly, daily, weekly, monthly, window_days, files_stale_minutes, database_stale_minutes, check_days)
			VALUES ('', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			b.EveryMinutes, b.Hourly, b.Daily, b.Weekly, b.Monthly, b.WindowDays, b.FilesStaleMinutes, b.DatabaseStaleMinutes, b.CheckDays)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func aboutData(banner page.Banner, b store.BackupSettings) pages.AboutData {
	return pages.AboutData{
		Banner: banner, Description: pages.Description, DefaultType: valid.DefaultType,
		DefaultRootGB: valid.DefaultRootGB, DefaultDataGB: valid.DefaultDataGB,
		BackupEveryMinutes: b.EveryMinutes, BackupHourly: b.Hourly, BackupDaily: b.Daily, BackupWeekly: b.Weekly,
		BackupWindowDays: b.WindowDays, BackupFilesStaleMinutes: b.FilesStaleMinutes, BackupDatabaseStaleMinutes: b.DatabaseStaleMinutes,
		TemplateURL: protocol.DefaultTemplateURL, TemplateRef: protocol.DefaultTemplateRef,
	}
}

// R-89DY-T88B R-CN7X-MZ19
func TestAboutReadsStoredSettingsWithOrWithoutHosts(t *testing.T) {
	f := setup(t)
	writePageBackupSettings(t, f, otherBackupSettings)
	target := "/about?unused=one"
	for _, configured := range []bool{false, true} {
		f.cfg.Configured = configured
		for _, method := range []string{"GET", "HEAD"} {
			want := aboutData(banner(f, target, []page.Level{pages.AboutLevel}), otherBackupSettings)
			same(t, answer(f, method, target, "viewer"), expected(f, method, target, "about", 200, want))
		}
	}
}

func fillBackupTemplates(data *pages.HostData, stamp, visible, reason string) {
	healths := []string{store.BackupNone, store.BackupHealthy, store.BackupStale, store.BackupError}
	states := []string{"", protocol.StepPending, protocol.StepDone, protocol.StepFailed}
	for i, state := range states {
		backup := pages.BackupRow{Account: "chosen-account", Health: healths[i], FilesStale: i != 0,
			Snapshot: stamp, SnapshotText: visible, Checked: stamp, CheckedText: visible,
			Error: reason, Final: state, Restore: state, Databases: []pages.DatabaseRow{}}
		if state == protocol.StepDone {
			backup.FinalAt, backup.FinalAtText = stamp, visible
		}
		if i != 0 {
			for _, health := range healths {
				backup.Databases = append(backup.Databases, pages.DatabaseRow{Path: "data/chosen.db", Health: health, Written: stamp, WrittenText: visible, Error: reason})
			}
			backup.Databases[0].Written, backup.Databases[0].WrittenText, backup.Databases[0].Error = "", "", ""
		} else {
			backup.Snapshot, backup.SnapshotText, backup.Checked, backup.CheckedText, backup.Error = "", "", "", "", ""
		}
		backup.DatabaseTotal = len(backup.Databases)
		if i > 1 {
			backup.DatabaseTotal += i
		}
		data.Backups = append(data.Backups, backup)
	}
	for _, kind := range []string{protocol.RestoreDatabase, protocol.RestoreFiles} {
		for _, state := range states[1:] {
			restore := pages.RestoreRow{Account: "chosen-account", Kind: kind, Path: "data/chosen.db",
				AsOf: stamp, AsOfText: visible, Asked: stamp, AskedText: visible, State: state}
			switch state {
			case protocol.StepDone:
				restore.Copy = "data/chosen-copy.db"
			case protocol.StepFailed:
				restore.Reason = reason
			}
			data.Restores = append(data.Restores, restore)
		}
	}
}

func checkPageStore(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func backupPageHost(t *testing.T, f *fixture, volume string) store.Host {
	t.Helper()
	st, ctx := f.cfg.Store, t.Context()
	host, op, err := st.CreateHost(ctx, hostSpec, caller)
	checkPageStore(t, err)
	checkPageStore(t, st.Finish(ctx, op.ID, store.Update{DataVolume: volume, InstanceID: "i-id", AllocationID: "allocation-id"}))
	return host
}

func placeBackupPageAccount(t *testing.T, f *fixture, host, slug string) {
	t.Helper()
	st, ctx := f.cfg.Store, t.Context()
	_, op, err := st.PlaceAccount(ctx, slug, host, caller)
	checkPageStore(t, err)
	checkPageStore(t, st.Finish(ctx, op.ID, store.Update{}))
}

func writePageReport(t *testing.T, f *fixture, host string, backups []protocol.AccountBackup, results ...protocol.RestoreResult) {
	t.Helper()
	_, err := f.cfg.Store.RecordReport(t.Context(), host, protocol.Report{
		Protocol: protocol.Version, Backups: backups, Restores: results,
	}, f.cfg.Now())
	checkPageStore(t, err)
}

func checkBackupHostPage(t *testing.T, f *fixture, h store.Host, view pages.HostView, reported time.Time, data pages.HostData) {
	t.Helper()
	target := "/hosts/" + h.Name
	data.Banner = banner(f, target, []page.Level{pages.HostLevel(h.Name)})
	data.Host = view
	if data.Seeds == nil {
		data.Seeds = make([]pages.SeedRow, 0, len(data.Backups))
		for _, row := range data.Backups {
			data.Seeds = append(data.Seeds, pages.SeedRow{Account: row.Account})
		}
	}
	if !reported.IsZero() {
		record, err := f.cfg.Store.Host(t.Context(), h.Name)
		checkPageStore(t, err)
		data.Report = &pages.ReportView{
			Received: reported.UTC().Format(time.RFC3339), ReceivedText: reported.UTC().Format(pages.SecondLayout), Wanted: record.Version,
			FailedUnits: []string{}, Slices: []pages.SliceRow{}, Bans: []pages.BanRow{}, Allowlist: []string{},
		}
	}
	same(t, answer(f, "GET", target, "viewer"), expected(f, "GET", target, "host", 200, data))
	got := pages.CapturePageData[pages.HostData](t, f.cfg, request("GET", target, "viewer"), "host")
	if got.DatabaseCount != data.DatabaseCount || got.RestoreTotal != data.RestoreTotal {
		t.Fatalf("host template received DatabaseCount %d and RestoreTotal %d, want %d and %d",
			got.DatabaseCount, got.RestoreTotal, data.DatabaseCount, data.RestoreTotal)
	}
	if !reflect.DeepEqual(got.Backups, data.Backups) {
		t.Fatalf("host template received backups %+v, want %+v", got.Backups, data.Backups)
	}
	if data.Restores == nil {
		data.Restores = []pages.RestoreRow{}
	}
	if !reflect.DeepEqual(got.Restores, data.Restores) {
		t.Fatalf("host template received restores %+v, want %+v", got.Restores, data.Restores)
	}
}

func viewOfBackupHost(h store.Host, domain string) pages.HostView {
	return pages.HostView{
		Name: h.Name, DNSName: h.Name + "." + domain, Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB,
		State: store.HostRunning, Health: store.HealthNone, InstanceID: "i-id", Created: created, CreatedText: createdSecond, Allowlist: []string{},
	}
}

// R-CN7X-MZ19 R-COFU-0QRY R-CPNQ-EIIN
func TestLandingBackupHealthUsesThePageClockAndHostCloseSurvivesStop(t *testing.T) {
	f := setup(t)
	st, ctx := f.cfg.Store, t.Context()
	h := backupPageHost(t, f, "")
	for _, slug := range []string{"stale", "errored", "unreported"} {
		placeBackupPageAccount(t, f, h.Name, slug)
	}
	f.setClock(time.Date(2026, 11, 25, 12, 0, 7, 0, time.UTC))
	closeAt := f.cfg.Now()
	snapshot := closeAt.Add(-120 * time.Minute)
	data := pages.LandingData{
		Configured: true, Domain: f.cfg.Domain, Users: []pages.UserRow{}, KeptVolumes: []pages.KeptVolumeRow{},
		Hosts: []pages.HostRow{{Name: h.Name, DNSName: h.Name + "." + f.cfg.Domain, State: store.HostRunning, Health: store.HealthSilent,
			Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB, AccountCount: 3}},
	}
	for _, slug := range []string{"errored", "stale", "unreported"} {
		data.Accounts = append(data.Accounts, pages.AccountRow{Slug: slug, Host: h.Name, DNSName: slug + "." + f.cfg.Domain,
			State: store.AccountActive, Backup: store.BackupNone, Created: created, CreatedText: createdMinute})
	}
	checkLandingBackupData := func() {
		t.Helper()
		checkLanding(t, f, "/", data)
		got := pages.CapturePageData[pages.LandingData](t, f.cfg, request("GET", "/", "viewer"), "landing")
		if !reflect.DeepEqual(got.Hosts, data.Hosts) {
			t.Fatalf("landing template received host rows %+v, want %+v", got.Hosts, data.Hosts)
		}
	}
	checkLandingBackupData()

	_, closing, err := st.CloseHost(ctx, h.Name, caller)
	checkPageStore(t, err)
	backups := []protocol.AccountBackup{{Account: "stale", Snapshot: snapshot}}
	writePageReport(t, f, h.Name, backups)
	checkPageStore(t, st.Finish(ctx, closing.ID, store.Update{}))
	row := &data.Hosts[0]
	row.Health = store.HealthPending
	row.Closed, row.ClosedText = closeAt.Format(time.RFC3339), closeAt.Format(pages.MinuteLayout)
	row.Reported, row.ReportedText = row.Closed, row.ClosedText
	data.Accounts[1].Backup = store.BackupHealthy
	data.Accounts[1].Snapshot, data.Accounts[1].SnapshotText = snapshot.Format(time.RFC3339), snapshot.Format(pages.MinuteLayout)
	checkLandingBackupData()

	// The store clock still reads the close time. Moving only the page clock makes the snapshot stale.
	f.cfg.Now = func() time.Time { return closeAt.Add(time.Second) }
	data.Accounts[1].Backup, row.BackupFlagged = store.BackupStale, 1
	checkLandingBackupData()
	const backupError = "chosen file backup error"
	writePageReport(t, f, h.Name, []protocol.AccountBackup{{Account: "errored", Error: backupError}})
	row.Reported = closeAt.Add(time.Second).Format(time.RFC3339)
	data.Accounts[0].Backup, row.BackupFlagged = store.BackupError, 2
	checkLandingBackupData()

	view := viewOfBackupHost(h, f.cfg.Domain)
	view.Health, view.AccountCount = store.HealthPending, 3
	view.Reported, view.ReportedText, view.Closed, view.ClosedText, view.BackupFlagged = row.Reported, row.ReportedText, row.Closed, row.ClosedText, 2
	checkBackupHostPage(t, f, h, view, f.cfg.Now(), pages.HostData{Backups: []pages.BackupRow{
		{Account: "errored", Health: store.BackupError, Final: protocol.StepPending, Error: backupError, Databases: []pages.DatabaseRow{}},
		{Account: "stale", Health: store.BackupStale, Final: protocol.StepPending, FilesStale: true,
			Snapshot: snapshot.Format(time.RFC3339), SnapshotText: snapshot.Format(pages.SecondLayout), Databases: []pages.DatabaseRow{}},
		{Account: "unreported", Health: store.BackupNone, Final: protocol.StepPending, Databases: []pages.DatabaseRow{}},
	}})

	for _, step := range []struct{ operation, state string }{{store.OpStop, store.HostStopped}, {store.OpStart, store.HostRunning}} {
		_, op, err := st.StartHostOp(ctx, h.Name, step.operation, store.Target{}, caller)
		checkPageStore(t, err)
		checkPageStore(t, st.Finish(ctx, op.ID, store.Update{}))
		row.State = step.state
		checkLandingBackupData()
	}
	_, opening, err := st.OpenHost(ctx, h.Name, caller)
	checkPageStore(t, err)
	checkPageStore(t, st.Finish(ctx, opening.ID, store.Update{}))
	row.Closed, row.ClosedText = "", ""
	checkLandingBackupData()

	configured := otherBackupSettings
	configured.FilesStaleMinutes = 150
	writePageBackupSettings(t, f, configured)
	data.Accounts[1].Backup, row.BackupFlagged = store.BackupHealthy, 1
	checkLandingBackupData()
}

// R-PL5K-0229
func TestHostBackupRowsUseFoldedFactsAndFirstReportEntry(t *testing.T) {
	f := setup(t)
	writePageBackupSettings(t, f, otherBackupSettings)
	st, ctx := f.cfg.Store, t.Context()
	h := backupPageHost(t, f, "vol-backup")
	for _, slug := range []string{"zulu", "alpha"} {
		placeBackupPageAccount(t, f, h.Name, slug)
	}
	_, closing, err := st.CloseHost(ctx, h.Name, caller)
	checkPageStore(t, err)
	now := f.cfg.Now()
	snapshot, check := now.Add(-100*time.Minute), now.Add(-time.Minute)
	const reportError = "chosen check failure"
	const databaseError = "chosen database WAL failure"
	const zuluDatabase = "data/zulu.db"
	var databases []protocol.Database
	var wantDBs []pages.DatabaseRow
	for i := 59; i >= 0; i-- {
		databases = append(databases, protocol.Database{Path: fmt.Sprintf("data/%02d.db", i)})
	}
	for i := range pages.MaxDatabaseRows {
		wantDBs = append(wantDBs, pages.DatabaseRow{Path: fmt.Sprintf("data/%02d.db", i), Health: store.BackupNone})
	}
	databases[58].Error, wantDBs[1].Error, wantDBs[1].Health = databaseError, databaseError, store.BackupError
	databases[57].Synced, wantDBs[2].Health = now.Add(-13*time.Minute), store.BackupStale
	databases[56].Synced, databases[56].Written, wantDBs[3].Health = now, check, store.BackupHealthy
	wantDBs[3].Written, wantDBs[3].WrittenText = check.UTC().Format(time.RFC3339), check.UTC().Format(pages.SecondLayout)
	writePageReport(t, f, h.Name, []protocol.AccountBackup{
		{Account: "zulu", Snapshot: now, Final: protocol.Step{ID: closing.ID, State: protocol.StepDone, At: check},
			Databases: []protocol.Database{{Path: zuluDatabase}}, DatabaseTotal: 4},
		{Account: "alpha", Databases: databases, DatabaseTotal: 55},
		{Account: "alpha", Snapshot: snapshot, Checked: check, Error: reportError,
			Final:     protocol.Step{ID: closing.ID, State: protocol.StepFailed, At: check},
			Databases: []protocol.Database{{Path: "data/alternate.db"}}},
	})
	view := viewOfBackupHost(h, f.cfg.Domain)
	view.DataVolume, view.Health, view.State, view.AccountCount = "vol-backup", store.HealthPending, store.HostClosing, 2
	view.Reported, view.ReportedText, view.BackupFlagged = now.UTC().Format(time.RFC3339), now.UTC().Format(pages.MinuteLayout), 1
	want := []pages.BackupRow{
		{Account: "alpha", Health: store.BackupError, FilesStale: true, Snapshot: snapshot.UTC().Format(time.RFC3339),
			SnapshotText: snapshot.UTC().Format(pages.SecondLayout), Checked: check.UTC().Format(time.RFC3339), CheckedText: check.UTC().Format(pages.SecondLayout),
			Error: reportError, Final: protocol.StepFailed, Databases: wantDBs, DatabaseTotal: 60},
		{Account: "zulu", Health: store.BackupHealthy, Snapshot: now.UTC().Format(time.RFC3339), SnapshotText: now.UTC().Format(pages.SecondLayout),
			Final: protocol.StepDone, FinalAt: check.UTC().Format(time.RFC3339), FinalAtText: check.UTC().Format(pages.SecondLayout),
			Databases: []pages.DatabaseRow{{Path: zuluDatabase, Health: store.BackupNone}}, DatabaseTotal: 4},
	}
	checkBackupHostPage(t, f, h, view, now, pages.HostData{Backups: want, DatabaseCount: len(wantDBs) + 1})

	checkPageStore(t, st.Fail(ctx, closing.ID, reportError))
	_, _, _, err = st.ReplaceHost(ctx, h.Name, store.ReplaceBackup, store.Target{}, caller)
	checkPageStore(t, err)
	view.State, view.Replace = store.HostReplacing, store.ReplaceBackup
	for i := range want {
		want[i].Restore = protocol.StepPending
	}
	checkBackupHostPage(t, f, h, view, now, pages.HostData{Backups: want, DatabaseCount: len(wantDBs) + 1})
}

// R-PL5K-0229 R-PMDG-DTSY
func TestHostDatabasesUseReportsFromTheCurrentPlacement(t *testing.T) {
	f := setup(t)
	st, ctx := f.cfg.Store, t.Context()
	h := backupPageHost(t, f, "")
	const replaced, unchanged = "acme", "zulu"
	placeBackupPageAccount(t, f, h.Name, replaced)
	placeBackupPageAccount(t, f, h.Name, unchanged)

	f.advance(time.Second)
	received := f.cfg.Now().UTC()
	backups := []protocol.AccountBackup{
		{Account: replaced, Databases: []protocol.Database{{Path: "data/old.db"}}, DatabaseTotal: 4},
		{Account: unchanged, Databases: []protocol.Database{{Path: "data/retained.db"}}, DatabaseTotal: 1},
	}
	writePageReport(t, f, h.Name, backups)
	view := viewOfBackupHost(h, f.cfg.Domain)
	view.AccountCount, view.Health = 2, store.HealthPending
	view.Reported, view.ReportedText = received.Format(time.RFC3339), received.Format(pages.MinuteLayout)
	data := pages.HostData{Backups: []pages.BackupRow{
		{Account: replaced, Health: store.BackupNone, Databases: []pages.DatabaseRow{{Path: "data/old.db", Health: store.BackupNone}}, DatabaseTotal: 4},
		{Account: unchanged, Health: store.BackupNone, Databases: []pages.DatabaseRow{{Path: "data/retained.db", Health: store.BackupNone}}, DatabaseTotal: 1},
	}, DatabaseCount: 2}
	checkBackupHostPage(t, f, h, view, received, data)

	f.advance(time.Second)
	_, removal, _, err := st.RemoveAccount(ctx, replaced, caller)
	checkPageStore(t, err)
	checkPageStore(t, st.Finish(ctx, removal.ID, store.Update{}))
	placeBackupPageAccount(t, f, h.Name, replaced)
	data.Backups[0].Databases, data.Backups[0].DatabaseTotal, data.DatabaseCount = []pages.DatabaseRow{}, 0, 1
	checkBackupHostPage(t, f, h, view, received, data)

	// Keep the clock at the new placement's creation time for this report.
	received = f.cfg.Now().UTC()
	backups[0].Databases[0].Path, backups[0].DatabaseTotal = "data/current.db", 5
	writePageReport(t, f, h.Name, backups)
	view.Reported, view.ReportedText = received.Format(time.RFC3339), received.Format(pages.MinuteLayout)
	data.Backups[0].Databases = []pages.DatabaseRow{{Path: "data/current.db", Health: store.BackupNone}}
	data.Backups[0].DatabaseTotal, data.DatabaseCount = 5, 2
	checkBackupHostPage(t, f, h, view, received, data)
}

// R-CUJB-XLHF R-PMDG-DTSY
func TestHostRestoresShowNewestRowsAndTheirResultsUntilNameReuse(t *testing.T) {
	f := setup(t)
	st, ctx := f.cfg.Store, t.Context()
	h := backupPageHost(t, f, "vol-restore")
	const slug = "acme"
	placeBackupPageAccount(t, f, h.Name, slug)
	asOf := f.cfg.Now().UTC().Add(-time.Minute)
	backups := []protocol.AccountBackup{{Account: slug, Snapshots: []int64{asOf.Unix()}}}
	writePageReport(t, f, h.Name, backups)

	view := viewOfBackupHost(h, f.cfg.Domain)
	view.DataVolume, view.AccountCount, view.Health = "vol-restore", 1, store.HealthPending
	data := pages.HostData{Backups: []pages.BackupRow{{Account: slug, Health: store.BackupNone, Databases: []pages.DatabaseRow{}}}}
	const count = 30
	for i := range count {
		f.advance(time.Second)
		asked := f.cfg.Now().UTC()
		request := store.RestoreRequest{Account: slug, Kind: protocol.RestoreFiles, Path: fmt.Sprintf("files/restore-%02d", i), AsOf: asOf}
		restore, op, err := st.RequestRestore(ctx, request, caller)
		checkPageStore(t, err)
		want := pages.RestoreRow{
			Account: slug, Kind: protocol.RestoreFiles, Path: request.Path, AsOf: asOf.Format(time.RFC3339), AsOfText: asOf.Format(pages.SecondLayout),
			State: protocol.StepPending, Asked: asked.Format(time.RFC3339), AskedText: asked.Format(pages.SecondLayout),
		}
		if i == count-1 {
			previous := asked.Add(-time.Second)
			view.Reported, view.ReportedText = previous.Format(time.RFC3339), previous.Format(pages.MinuteLayout)
			pending := data
			pending.RestoreTotal++
			pending.Restores = append([]pages.RestoreRow{want}, data.Restores[:pages.MaxRestoreRows-1]...)
			checkBackupHostPage(t, f, h, view, previous, pending)
		}
		want.State = protocol.StepDone
		want.Copy = request.Path + "-copy"
		if i == count-2 {
			want.State, want.Copy, want.Reason = protocol.StepFailed, "", "chosen restore error"
		}
		writePageReport(t, f, h.Name, backups, protocol.RestoreResult{ID: restore.ID, State: want.State, Copy: want.Copy, Error: want.Reason})
		checkPageStore(t, st.Finish(ctx, op.ID, store.Update{}))
		data.Restores = append([]pages.RestoreRow{want}, data.Restores...)
		data.RestoreTotal++
		if i == 0 || i+1 == pages.MaxRestoreRows {
			view.Reported, view.ReportedText = asked.Format(time.RFC3339), asked.Format(pages.MinuteLayout)
			checkBackupHostPage(t, f, h, view, asked, data)
		}
	}
	data.Restores = data.Restores[:pages.MaxRestoreRows]
	now := f.cfg.Now().UTC()
	view.Reported, view.ReportedText = now.Format(time.RFC3339), now.Format(pages.MinuteLayout)
	checkBackupHostPage(t, f, h, view, now, data)

	_, removal, _, err := st.RemoveAccount(ctx, slug, caller)
	checkPageStore(t, err)
	checkPageStore(t, st.Finish(ctx, removal.ID, store.Update{}))
	_, deletion, err := st.DeleteHost(ctx, h.Name, true, false, caller)
	checkPageStore(t, err)
	checkPageStore(t, st.Finish(ctx, deletion.ID, store.Update{}))
	reused, _, err := st.CreateHost(ctx, hostSpec, caller)
	checkPageStore(t, err)
	if reused.Name != h.Name {
		t.Fatalf("fixture host name = %s, want %s", reused.Name, h.Name)
	}
	empty := viewOfBackupHost(reused, f.cfg.Domain)
	empty.State, empty.InstanceID = store.HostProvisioning, ""
	empty.Created, empty.CreatedText = now.Format(time.RFC3339), now.Format(pages.SecondLayout)
	checkBackupHostPage(t, f, reused, empty, time.Time{}, pages.HostData{Backups: []pages.BackupRow{}, Restores: []pages.RestoreRow{}})
}

// R-MBKM-ROR1 R-MHO4-OJGI R-CQVM-SA9C R-CS3J-6201
func TestReplacementKeepsVolumeAtItsOwnTimeAndLiveHostPage(t *testing.T) {
	f := setup(t)
	st, ctx := f.cfg.Store, t.Context()
	ordinary := createKeptVolume(t, f, "vol-deleted")
	h := backupPageHost(t, f, "vol-replaced")
	if ordinary.Host != h.Name {
		t.Fatalf("fixture host name = %s, want reused %s", h.Name, ordinary.Host)
	}
	view := viewOfBackupHost(h, f.cfg.Domain)
	view.DataVolume = "vol-replaced"
	data := pages.HostData{Backups: []pages.BackupRow{}, Restores: []pages.RestoreRow{}}
	checkBackupHostPage(t, f, h, view, time.Time{}, data)

	f.advance(time.Minute)
	_, op, _, err := st.ReplaceHost(ctx, h.Name, store.ReplaceBackup, store.Target{}, caller)
	checkPageStore(t, err)
	view.State, view.Replace = store.HostReplacing, store.ReplaceBackup
	checkBackupHostPage(t, f, h, view, time.Time{}, data)
	f.advance(time.Minute)
	keptAt := f.cfg.Now().UTC()
	checkPageStore(t, st.Advance(ctx, op.ID, store.Update{HostState: store.HostRestoring, DataVolume: "vol-new"}))
	view.State, view.DataVolume = store.HostRestoring, "vol-new"
	checkBackupHostPage(t, f, h, view, time.Time{}, data)
	const failure = "chosen replacement failure"
	checkPageStore(t, st.Fail(ctx, op.ID, failure))
	view.State, view.Reason = store.HostFailed, failure
	checkBackupHostPage(t, f, h, view, time.Time{}, data)
	_, retry, err := st.RetryHost(ctx, h.Name, caller)
	checkPageStore(t, err)
	f.advance(time.Minute)
	checkPageStore(t, st.Finish(ctx, retry.ID, store.Update{}))
	view.State, view.Replace, view.Reason = store.HostRunning, "", ""
	checkBackupHostPage(t, f, h, view, time.Time{}, data)

	volumes := make([]pages.KeptVolumeRow, 0, paging.DefaultLimit)
	for i := 1; i < paging.DefaultLimit; i++ {
		volumes = append(volumes, createKeptVolume(t, f, fmt.Sprintf("vol-newer-%02d", paging.DefaultLimit-i)))
	}
	volumes = append(volumes, pages.KeptVolumeRow{
		Volume: "vol-replaced", Host: h.Name, Cause: store.CauseReplaced, SizeGB: hostSpec.DataGB,
		Deleted: keptAt.Format(time.RFC3339), DeletedText: keptAt.Format(pages.MinuteLayout),
	})
	next := token("kept_volumes", keptAt.UnixNano(), "vol-replaced")
	landing := pages.LandingData{
		Configured: true, Domain: f.cfg.Domain, Accounts: []pages.AccountRow{}, Users: []pages.UserRow{},
		Hosts: []pages.HostRow{{Name: h.Name, DNSName: view.DNSName, State: store.HostRunning, Health: store.HealthNone,
			Type: hostSpec.Type, RootGB: hostSpec.RootGB, DataGB: hostSpec.DataGB}},
		KeptVolumes: volumes, VolumesNext: next,
	}
	checkLanding(t, f, "/", landing)
	client := pageToolClient(t, f)
	toolWindow := pageToolContent[tools.KeptVolumeList](t, client, tools.ToolKeptVolumes, tools.KeptVolumesArgs{})
	if toolWindow.Next == nil || *toolWindow.Next != next || len(toolWindow.KeptVolumes) != paging.DefaultLimit {
		t.Fatalf("kept_volumes result has %d rows and next %v, want %d rows and next %q", len(toolWindow.KeptVolumes), toolWindow.Next, paging.DefaultLimit, next)
	}
	wantReplaced := tools.KeptVolume{
		Volume: "vol-replaced", Host: h.Name, Cause: store.CauseReplaced, SizeGB: hostSpec.DataGB,
		Deleted: keptAt.Format(time.RFC3339), State: store.VolumeKept,
	}
	if got := toolWindow.KeptVolumes[paging.DefaultLimit-1]; got != wantReplaced {
		t.Fatalf("kept_volumes final row = %+v, want %+v", got, wantReplaced)
	}
	deletedAt, err := time.Parse(time.RFC3339, ordinary.Deleted)
	checkPageStore(t, err)
	landing.KeptVolumes = []pages.KeptVolumeRow{ordinary}
	landing.VolumesPage, landing.VolumesNext = next, ""
	landing.VolumesPrev = token("kept_volumes-before", deletedAt.UnixNano(), ordinary.Volume)
	checkLanding(t, f, "/?volumes_page="+next, landing)
}

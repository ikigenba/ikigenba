package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// R-C9YP-IZAO, R-CB6L-WR1D.
func TestBackupPrefixes(t *testing.T) {
	host := struct{ prefix func(string, int64) string }{HostPrefix}
	prefixes := []struct {
		prefix func(string) string
		want   string
	}{
		{AccountPrefix, "accounts/acme/"},
		{FilesPrefix, "accounts/acme/files"},
		{DataPrefix, "accounts/acme/data"},
	}
	for _, prefix := range prefixes {
		if got := prefix.prefix("acme"); got != prefix.want {
			t.Errorf("prefix = %q, want %q", got, prefix.want)
		}
	}
	for _, tt := range []struct {
		seq  int64
		want string
	}{
		{7, "hosts/host01/7/"},
		{0, "hosts/host01/0/"},
		{-42, "hosts/host01/-42/"},
		{math.MaxInt64, "hosts/host01/9223372036854775807/"},
	} {
		if got := host.prefix("host01", tt.seq); got != tt.want {
			t.Errorf("host prefix = %q, want %q", got, tt.want)
		}
	}
}

// R-CCEI-AIS2, R-CDME-OAIR.
func TestBackupConstants(t *testing.T) {
	type limit uint16
	type word string
	if got, want := [...]limit{MaxBackupAccounts, MaxDatabases, MaxSnapshots, MaxBackupError, MaxDatabasePath, MaxRestores, MaxCopyPath},
		[...]limit{50, 50, 32, 256, 256, 10, 640}; got != want {
		t.Errorf("backup limits = %v, want %v", got, want)
	}
	if got, want := [...]word{StepPending, StepDone, StepFailed, CodeNotEmpty, CodeFailed, RestoreDatabase, RestoreFiles},
		[...]word{"pending", "done", "failed", "not_empty", "failed", "database", "files"}; got != want {
		t.Errorf("backup words = %v, want %v", got, want)
	}
}

// R-CG27-FU05, R-CHA3-TLQU, R-CII0-7DHJ, R-CM5P-COPM, R-CNDL-QGGB, R-COLI-4870, R-CPTE-HZXP.
func TestBackupWireObjects(t *testing.T) {
	// Struct conversion checks the ordered Go fields independently of their JSON tags.
	_ = struct {
		Bucket     string
		Region     string
		Settings   Settings
		Host       Credentials
		Accounts   []AccountCredentials
		HostPrefix string
		Close      string
		Restore    *HostRestore
		Restores   []RestoreRequest
	}(Backup{})
	_ = struct {
		EveryMinutes         int
		Hourly               int
		Daily                int
		Monthly              int
		WindowDays           int
		FilesStaleMinutes    int
		DatabaseStaleMinutes int
		CheckDays            int
	}(Settings{})
	_ = struct {
		AccessKeyID     string
		SecretAccessKey string
		SessionToken    string
		Expires         time.Time
	}(Credentials{})
	_ = struct {
		Account     string
		Credentials Credentials
	}(AccountCredentials{})
	_ = struct {
		ID       string
		Accounts []RestoreAccount
	}(HostRestore{})
	_ = struct {
		Slug      string
		GID       int64
		Databases []string
	}(RestoreAccount{})
	_ = struct {
		ID      string
		Account string
		Kind    string
		Path    string
		User    string
		AsOf    time.Time
	}(RestoreRequest{})
	_ = struct {
		Account        string
		Snapshot       time.Time
		Snapshots      []int64
		Checked        time.Time
		Error          string
		Databases      []Database
		DatabaseTotal  int
		DatabaseErrors int
		Synced         time.Time
		Written        time.Time
		Final          Step
		Restored       Step
	}(AccountBackup{})
	_ = struct {
		Path    string
		Oldest  time.Time
		Written time.Time
		Synced  time.Time
		Error   string
	}(Database{})
	_ = struct {
		ID    string
		State string
		At    time.Time
		Code  string
	}(Step{})
	_ = struct {
		Snapshot time.Time
		Error    string
		Final    Step
		Restored Step
	}(HostBackup{})
	_ = struct {
		ID    string
		State string
		Copy  string
		Error string
	}(RestoreResult{})
	when := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	every, hourly, daily, monthly, window, filesStale, dbStale, checkDays := 60, 24, 7, 1, 35, 120, 10, 7
	total, failures := 3, 1
	var gid int64 = 20001
	settings := Settings{every, hourly, daily, monthly, window, filesStale, dbStale, checkDays}
	credentials := Credentials{"backup-key", "backup-secret", "backup-token", when}
	accountCredentials := AccountCredentials{"acme", credentials}
	restoreAccount := RestoreAccount{"acme", gid, []string{"data/example.db"}}
	hostRestore := HostRestore{"restore-host", []RestoreAccount{restoreAccount}}
	request := RestoreRequest{"restore-part", "acme", RestoreFiles, "example.txt", "daffy", when}
	backup := Backup{"example-bucket", "us-east-1", settings, credentials, []AccountCredentials{accountCredentials}, "hosts/host01/1/", "close-host", &hostRestore, []RestoreRequest{request}}
	db := Database{"data/example.db", when, when, when, "database fault"}
	step := Step{"close-host", StepDone, when, ""}
	account := AccountBackup{"acme", when, []int64{10}, when, "account fault", []Database{db}, total, failures, when, when, step, step}
	host := HostBackup{when, "host fault", step, step}
	result := RestoreResult{"restore-part", StepFailed, "restored/example.txt", "restore fault"}
	const stamp = `"2026-11-18T12:00:00Z"`
	const settingsJSON = `{"every_minutes":60,"hourly":24,"daily":7,"monthly":1,"window_days":35,"files_stale_minutes":120,"database_stale_minutes":10,"check_days":7}`
	const credentialsJSON = `{"access_key_id":"backup-key","secret_access_key":"backup-secret","session_token":"backup-token","expires":` + stamp + `}`
	const accountCredentialsJSON = `{"account":"acme","credentials":` + credentialsJSON + `}`
	const restoreAccountJSON = `{"slug":"acme","gid":20001,"databases":["data/example.db"]}`
	const hostRestoreJSON = `{"id":"restore-host","accounts":[` + restoreAccountJSON + `]}`
	const requestJSON = `{"id":"restore-part","account":"acme","kind":"files","path":"example.txt","user":"daffy","as_of":` + stamp + `}`
	const stepJSON = `{"id":"close-host","state":"done","at":` + stamp + `,"code":""}`
	const dbJSON = `{"path":"data/example.db","oldest":` + stamp + `,"written":` + stamp + `,"synced":` + stamp + `,"error":"database fault"}`
	for _, tt := range []struct {
		value any
		want  string
	}{
		{settings, settingsJSON},
		{credentials, credentialsJSON},
		{accountCredentials, accountCredentialsJSON},
		{restoreAccount, restoreAccountJSON},
		{hostRestore, hostRestoreJSON},
		{request, requestJSON},
		{backup, `{"bucket":"example-bucket","region":"us-east-1","settings":` + settingsJSON + `,"host":` + credentialsJSON + `,"accounts":[` + accountCredentialsJSON + `],"host_prefix":"hosts/host01/1/","close":"close-host","restore":` + hostRestoreJSON + `,"restores":[` + requestJSON + `]}`},
		{Backup{}, `{"bucket":"","region":"","settings":{"every_minutes":0,"hourly":0,"daily":0,"monthly":0,"window_days":0,"files_stale_minutes":0,"database_stale_minutes":0,"check_days":0},"host":{"access_key_id":"","secret_access_key":"","session_token":"","expires":"0001-01-01T00:00:00Z"},"accounts":null,"host_prefix":"","close":"","restore":null,"restores":null}`},
		{step, stepJSON},
		{db, dbJSON},
		{account, `{"account":"acme","snapshot":` + stamp + `,"snapshots":[10],"checked":` + stamp + `,"error":"account fault","databases":[` + dbJSON + `],"database_total":3,"database_errors":1,"synced":` + stamp + `,"written":` + stamp + `,"final":` + stepJSON + `,"restored":` + stepJSON + `}`},
		{host, `{"snapshot":` + stamp + `,"error":"host fault","final":` + stepJSON + `,"restored":` + stepJSON + `}`},
		{result, `{"id":"restore-part","state":"failed","copy":"restored/example.txt","error":"restore fault"}`},
	} {
		got, err := json.Marshal(tt.value)
		if err != nil || string(got) != tt.want {
			t.Errorf("marshal %T = %s, %v, want %s", tt.value, got, err, tt.want)
		}
	}
}

// R-CUP0-12WH, R-CVWW-EUN6.
func TestDecodePreviousBackupWire(t *testing.T) {
	state, err := DecodeState(strings.NewReader(`{"protocol":1,"host":"host01","version":5,"daemon":"","data_volume":"","root_gb":20,"data_gb":50,"accounts":[],"users":[],"allowlist":[],"unban":[],"domain":"hosts.example.com"}`))
	wantState := DesiredState{Protocol: 1, Host: "host01", Version: 5, RootGB: 20, DataGB: 50, Accounts: []Account{}, Users: []User{}, Allowlist: []string{}, Unban: []string{}, Domain: "hosts.example.com"}
	if err != nil || !reflect.DeepEqual(state, wantState) {
		t.Errorf("previous state = %+v, %v, want %+v", state, err, wantState)
	}
	report, err := DecodeReport(strings.NewReader(`{"protocol":1,"attempted":3,"applied":3,"hostname":"host01.hosts.example.com"}`))
	wantReport := Report{Protocol: 1, Attempted: 3, Applied: 3, Hostname: "host01.hosts.example.com"}
	if err != nil || !reflect.DeepEqual(report, wantReport) {
		t.Errorf("previous report = %+v, %v, want %+v", report, err, wantReport)
	}
}

// R-CS97-9JF3, R-GLHD-0XVJ.
func TestBoundedBackups(t *testing.T) {
	longEscaped := strings.Repeat("<", 50)
	longMultibyte := strings.Repeat("a", 253) + "é"
	original := Report{
		Backups: []AccountBackup{
			{Account: "beta", DatabaseTotal: 65},
			{Account: "acme", Error: strings.Repeat("a", 300), DatabaseErrors: 2},
		},
		HostBackup: HostBackup{Error: longEscaped, Final: Step{State: StepFailed, Code: CodeNotEmpty}},
	}
	for i := range MaxSnapshots + 8 {
		original.Backups[1].Snapshots = append(original.Backups[1].Snapshots, int64(i))
	}
	for _, account := range []int{0, 1} {
		for i := 29; i >= 0; i-- {
			original.Backups[account].Databases = append(original.Backups[account].Databases, Database{Path: fmt.Sprintf("data/%02d.db", i), Error: longMultibyte})
		}
		original.Backups[account].Databases = append(original.Backups[account].Databases, Database{Path: strings.Repeat("&", 43)})
	}
	for i := 59; i >= 2; i-- {
		original.Backups = append(original.Backups, AccountBackup{Account: fmt.Sprintf("name%02d", i), Databases: []Database{{Path: "data/sample.db"}}})
	}
	for i := 19; i >= 0; i-- {
		copyPath := strings.Repeat("a", 638)
		if i == 0 {
			copyPath += "a"
		}
		original.Restores = append(original.Restores, RestoreResult{ID: fmt.Sprintf("result-%02d", i), State: StepDone, Copy: copyPath, Error: longEscaped})
	}
	before, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	got := original.Bounded()
	if len(got.Backups) != MaxBackupAccounts || len(got.Restores) != MaxRestores {
		t.Fatalf("kept %d backups and %d restores, want %d and %d", len(got.Backups), len(got.Restores), MaxBackupAccounts, MaxRestores)
	}
	for i, account := range got.Backups {
		wantAccount := "acme"
		if i == 1 {
			wantAccount = "beta"
		} else if i > 1 {
			wantAccount = fmt.Sprintf("name%02d", i)
		}
		if account.Account != wantAccount {
			t.Errorf("backup %d = %q, want %q", i, account.Account, wantAccount)
		}
	}
	acme, beta := got.Backups[0], got.Backups[1]
	if acme.Error != strings.Repeat("a", 254) || acme.DatabaseTotal != 31 || acme.DatabaseErrors != 2 || beta.DatabaseTotal != 65 {
		t.Errorf("backup facts = error %q, totals %d and %d, errors %d", acme.Error, acme.DatabaseTotal, beta.DatabaseTotal, acme.DatabaseErrors)
	}
	wantSnapshots := make([]int64, MaxSnapshots)
	for i := range wantSnapshots {
		wantSnapshots[i] = int64(MaxSnapshots + 7 - i)
	}
	if !slices.Equal(acme.Snapshots, wantSnapshots) {
		t.Errorf("snapshots = %v, want %v", acme.Snapshots, wantSnapshots)
	}
	for i, account := range got.Backups {
		wantCount := 0
		switch i {
		case 0:
			wantCount = 30
		case 1:
			wantCount = MaxDatabases - 30
		}
		if len(account.Databases) != wantCount {
			t.Errorf("account %s retains %d databases, want %d", account.Account, len(account.Databases), wantCount)
		}
		for j, db := range account.Databases {
			if want := (Database{Path: fmt.Sprintf("data/%02d.db", j), Error: strings.Repeat("a", 253)}); db != want {
				t.Errorf("database %d of %q = %+v, want %+v", j, account.Account, db, want)
			}
		}
	}
	wantHost := original.HostBackup
	wantHost.Error = strings.Repeat("<", 42)
	if got.HostBackup != wantHost {
		t.Errorf("host backup = %+v, want %+v", got.HostBackup, wantHost)
	}
	for i, result := range got.Restores {
		want := RestoreResult{ID: fmt.Sprintf("result-%02d", i), State: StepDone, Copy: strings.Repeat("a", 638), Error: strings.Repeat("<", 42)}
		if i == 0 {
			want.State, want.Copy = StepFailed, ""
		}
		if result != want {
			t.Errorf("restore %d = %+v, want %+v", i, result, want)
		}
	}
	after, err := json.Marshal(original)
	if err != nil || string(after) != string(before) {
		t.Error("Bounded mutated its receiver's backup lists")
	}
}

// R-GNX5-SHCX.
func TestBackupReportBudget(t *testing.T) {
	n := int64(math.MaxInt64)
	f := -1.2345678901234567e-06
	when := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	ordinary := strings.Repeat("a", 80)
	escaped64 := strings.Repeat("\x01", 64)
	longError := strings.Repeat("\x01", 5000)
	backupError := strings.Repeat("\x01", 42) + strings.Repeat("a", 958)
	name := strings.Repeat("a", 32)
	disk := Disk{n, n, n, n}
	step := Step{"op_0123456789abcdef", StepPending, when, CodeNotEmpty}
	report := Report{
		Protocol: Version, Attempted: n, Applied: n, Error: longError,
		InstanceID: ordinary, Type: ordinary, AMI: ordinary, OS: longError, Kernel: escaped64,
		Booted: when, DaemonVersion: ordinary, Load: [3]float64{f, f, f}, CPUPercent: f,
		MemoryUsed: n, MemoryAvailable: n, SwapUsed: n, SwapTotal: n, Root: disk, Data: disk,
		Sessions: int(n), Updates: int(n), BanTotal: int(n), Hostname: escaped64,
		HostBackup: HostBackup{when, backupError, step, step},
	}
	for range 150 {
		report.FailedUnits = append(report.FailedUnits, strings.Repeat("\\", 250)+".swap")
	}
	for range 60 {
		report.Slices = append(report.Slices, Slice{name, f, n})
		report.Allowlist = append(report.Allowlist, strings.Repeat("a", 43))
	}
	for range 250 {
		report.SignIns = append(report.SignIns, SignIn{name, when})
		report.Bans = append(report.Bans, Ban{strings.Repeat("a", 45), when, when})
	}
	for i := range 60 {
		account := AccountBackup{
			Account: distinctASCII(i, 32), Snapshot: when,
			Checked: when, Error: backupError, DatabaseTotal: int(n), DatabaseErrors: int(n),
			Synced: when, Written: when, Final: step, Restored: step,
		}
		for range 40 {
			account.Snapshots = append(account.Snapshots, 253402300799)
		}
		for j := range 30 {
			account.Databases = append(account.Databases, Database{distinctASCII(j, 254), when, when, when, backupError})
		}
		report.Backups = append(report.Backups, account)
	}
	for range 20 {
		report.Restores = append(report.Restores, RestoreResult{step.ID, StepPending, strings.Repeat("a", 638), backupError})
	}
	raw, err := json.Marshal(report.Bounded())
	const budget = MaxReportBytes - 32<<10
	if err != nil || len(raw) > budget {
		t.Fatalf("bounded report marshals to %d bytes, %v, want at most %d", len(raw), err, budget)
	}
}

func distinctASCII(index, length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return letters[index/26:index/26+1] + letters[index%26:index%26+1] + strings.Repeat("a", length-2)
}

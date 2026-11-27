package protocol

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// R-TUOF-MNIY, R-TVWC-0F9N, R-TYC4-RYR1, R-U4FM-OTGI.
func TestConstantsAndSignatures(t *testing.T) {
	wantStrings := []string{"/accountsd", "/accountsd/v1/state", "/accountsd/v1/report", "after", "digest", "application/json", "application/octet-stream", "sha256:", "active", "removed", "active", "locked", "deleted"}
	gotStrings := []string{BinaryPath, StatePath, ReportPath, QueryAfter, QueryDigest, ContentJSON, ContentBinary, DigestPrefix, AccountActive, AccountRemoved, UserActive, UserLocked, UserDeleted}
	if !reflect.DeepEqual(gotStrings, wantStrings) {
		t.Fatalf("wire strings = %q, want %q", gotStrings, wantStrings)
	}
	gotInts := []int{Version, MaxStateBytes, MaxReportBytes, MaxBinaryBytes, MaxErrorBytes, MaxFailedUnits, MaxSlices, MaxSignIns, DigestBuffer}
	wantInts := []int{1, 2 << 20, 256 << 10, 64 << 20, 4096, 100, 50, 200, 32 << 10}
	if !reflect.DeepEqual(gotInts, wantInts) || LongPoll != 45*time.Second {
		t.Fatalf("wire limits = %v, poll = %v", gotInts, LongPoll)
	}
	_ = [9]uint64{Version, MaxStateBytes, MaxReportBytes, MaxBinaryBytes, MaxErrorBytes, MaxFailedUnits, MaxSlices, MaxSignIns, DigestBuffer}
	if reflect.TypeOf(LongPoll) != reflect.TypeOf(time.Duration(0)) {
		t.Fatalf("LongPoll has type %T", LongPoll)
	}
	_ = struct {
		state  func(io.Reader) (DesiredState, error)
		report func(io.Reader) (Report, error)
		digest func(io.Reader) (string, error)
		bound  func(Report) Report
	}{DecodeState, DecodeReport, Digest, Report.Bounded}
	seen := make(map[error]bool)
	for _, err := range []error{ErrTooLarge, ErrMalformed, ErrVersion} {
		if err == nil || seen[err] {
			t.Fatalf("error is nil or duplicated: %v", err)
		}
		seen[err] = true
	}
}

// R-YFOX-ORCE R-YGWU-2J33
func TestBoxConstants(t *testing.T) {
	type wireName string
	got := [4]wireName{BoxPath, QueryUID, MethodToolsList, MethodToolsCall}
	want := [4]wireName{"/accountsd/v1/box", "uid", "tools/list", "tools/call"}
	if got != want {
		t.Errorf("box wire names = %v, want %v", got, want)
	}
	if got, want := [2]uint64{MaxBoxRequest, MaxBoxAnswer}, [2]uint64{64 << 10, 1 << 20}; got != want {
		t.Errorf("box limits = %v, want %v", got, want)
	}
}

// R-X76X-03BY.
func TestBanLimits(t *testing.T) {
	const (
		bans      uint16 = MaxBans
		allowlist uint16 = MaxAllowlist
		unbans    uint16 = MaxUnbans
	)
	if bans != 200 || allowlist != 50 || unbans != 50 {
		t.Fatalf("ban limits = %d, %d, %d", bans, allowlist, unbans)
	}
}

// R-CEUB-229G, R-CJPW-L588, R-CPTE-HZXP, R-U0RX-JI8F, R-U37Q-B1PT, R-X5Z0-MBL9.
func TestWireObjects(t *testing.T) {
	// Struct conversion checks the ordered Go fields independently of their JSON tags.
	_ = struct {
		Protocol   int
		Host       string
		Version    int64
		Daemon     string
		DataVolume string
		RootGB     int
		DataGB     int
		Accounts   []Account
		Users      []User
		Allowlist  []string
		Unban      []string
		Domain     string
		Backup     *Backup
	}(DesiredState{})
	_ = struct {
		Protocol        int
		Attempted       int64
		Applied         int64
		Error           string
		InstanceID      string
		Type            string
		AMI             string
		OS              string
		Kernel          string
		Booted          time.Time
		DaemonVersion   string
		Load            [3]float64
		CPUPercent      float64
		MemoryUsed      int64
		MemoryAvailable int64
		SwapUsed        int64
		SwapTotal       int64
		Root            Disk
		Data            Disk
		Sessions        int
		Updates         int
		Reboot          bool
		FailedUnits     []string
		Slices          []Slice
		SignIns         []SignIn
		Bans            []Ban
		BanTotal        int
		Allowlist       []string
		Hostname        string
		Backups         []AccountBackup
		HostBackup      HostBackup
		Restores        []RestoreResult
	}(Report{})
	const emptyReportBackups = `,"backups":null,"host_backup":{"snapshot":"0001-01-01T00:00:00Z","error":"","final":{"id":"","state":"","at":"0001-01-01T00:00:00Z","code":""},"restored":{"id":"","state":"","at":"0001-01-01T00:00:00Z","code":""}},"restores":null}`
	when := time.Date(2026, 11, 17, 12, 13, 14, 0, time.UTC)
	banned := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	ban := Ban{"192.0.2.77", banned, banned.Add(10 * time.Minute)}
	account := Account{"acme", 20000, AccountActive}
	user := User{"daffy", "acme", 20001, UserLocked, []string{}}
	disk := Disk{1, 2, 3, 4}
	slice := Slice{"acme", 6, 7}
	signIn := SignIn{"daffy", when}
	protocol, rootGB, dataGB, sessions, updates, total := 1, 20, 50, 14, 15, 16
	var (
		version, applied, memoryUsed, memoryAvailable, swapUsed, swapTotal int64   = 7, 6, 6, 7, 8, 9
		cpuPercent                                                         float64 = 5
	)
	state := DesiredState{protocol, "host01", version, "", "", rootGB, dataGB, []Account{account}, []User{user}, []string{"192.0.2.0/24"}, []string{}, "hosts.example.com", nil}
	report := Report{protocol, version, applied, "failure", "instance", "instance-type", "image", "os", "kernel", when, "digest", [3]float64{1, 2, 3}, cpuPercent, memoryUsed, memoryAvailable, swapUsed, swapTotal, disk, Disk{10, 11, 12, 13}, sessions, updates, true, []string{"unit"}, []Slice{slice}, []SignIn{signIn}, []Ban{ban}, total, []string{"192.0.2.0/24"}, "host01.hosts.example.com", nil, HostBackup{}, nil}
	for _, tt := range []struct {
		value any
		json  string
	}{
		{state, `{"protocol":1,"host":"host01","version":7,"daemon":"","data_volume":"","root_gb":20,"data_gb":50,"accounts":[{"slug":"acme","gid":20000,"state":"active"}],"users":[{"name":"daffy","account":"acme","uid":20001,"state":"locked","keys":[]}],"allowlist":["192.0.2.0/24"],"unban":[],"domain":"hosts.example.com","backup":null}`},
		{report, `{"protocol":1,"attempted":7,"applied":6,"error":"failure","instance_id":"instance","type":"instance-type","ami":"image","os":"os","kernel":"kernel","booted":"2026-11-17T12:13:14Z","daemon_version":"digest","load":[1,2,3],"cpu_percent":5,"memory_used":6,"memory_available":7,"swap_used":8,"swap_total":9,"root":{"used_bytes":1,"size_bytes":2,"inodes_used":3,"inodes_total":4},"data":{"used_bytes":10,"size_bytes":11,"inodes_used":12,"inodes_total":13},"sessions":14,"updates":15,"reboot":true,"failed_units":["unit"],"slices":[{"account":"acme","cpu_percent":6,"memory_bytes":7}],"sign_ins":[{"user":"daffy","at":"2026-11-17T12:13:14Z"}],"bans":[{"address":"192.0.2.77","banned":"2026-11-18T12:00:00Z","expires":"2026-11-18T12:10:00Z"}],"ban_total":16,"allowlist":["192.0.2.0/24"],"hostname":"host01.hosts.example.com"` + emptyReportBackups},
		{DesiredState{}, `{"protocol":0,"host":"","version":0,"daemon":"","data_volume":"","root_gb":0,"data_gb":0,"accounts":null,"users":null,"allowlist":null,"unban":null,"domain":"","backup":null}`},
		{account, `{"slug":"acme","gid":20000,"state":"active"}`},
		{user, `{"name":"daffy","account":"acme","uid":20001,"state":"locked","keys":[]}`},
		{disk, `{"used_bytes":1,"size_bytes":2,"inodes_used":3,"inodes_total":4}`},
		{slice, `{"account":"acme","cpu_percent":6,"memory_bytes":7}`},
		{signIn, `{"user":"daffy","at":"2026-11-17T12:13:14Z"}`},
		{ban, `{"address":"192.0.2.77","banned":"2026-11-18T12:00:00Z","expires":"2026-11-18T12:10:00Z"}`},
		{Report{}, `{"protocol":0,"attempted":0,"applied":0,"error":"","instance_id":"","type":"","ami":"","os":"","kernel":"","booted":"0001-01-01T00:00:00Z","daemon_version":"","load":[0,0,0],"cpu_percent":0,"memory_used":0,"memory_available":0,"swap_used":0,"swap_total":0,"root":{"used_bytes":0,"size_bytes":0,"inodes_used":0,"inodes_total":0},"data":{"used_bytes":0,"size_bytes":0,"inodes_used":0,"inodes_total":0},"sessions":0,"updates":0,"reboot":false,"failed_units":null,"slices":null,"sign_ins":null,"bans":null,"ban_total":0,"allowlist":null,"hostname":""` + emptyReportBackups},
	} {
		got, err := json.Marshal(tt.value)
		if err != nil || string(got) != tt.json {
			t.Fatalf("marshal %T: %s, %v; want %s", tt.value, got, err, tt.json)
		}
	}
}

// R-U6VF-GCXW.
func TestUnknownAndAbsentMembers(t *testing.T) {
	var state DesiredState
	rawState := `{"protocol":1,"host":"host01","version":3,"future":{"x":1},"accounts":[{"slug":"acme","gid":20000,"state":"active","quota":5}],"users":[{"name":"daffy","future":10}]}`
	stateWant := DesiredState{Protocol: 1, Host: "host01", Version: 3, Accounts: []Account{{Slug: "acme", GID: 20000, State: AccountActive}}, Users: []User{{Name: "daffy"}}}
	if err := json.Unmarshal([]byte(rawState), &state); err != nil || !reflect.DeepEqual(state, stateWant) {
		t.Fatalf("state = %+v, %v, want %+v", state, err, stateWant)
	}
	var report Report
	rawReport := `{"protocol":1,"future":true,"root":{"size_bytes":4,"future":true},"data":{"future":true},"slices":[{"account":"acme","future":true}],"sign_ins":[{"user":"daffy","future":true}],"bans":[{"address":"192.0.2.77","future":true}]}`
	reportWant := Report{Protocol: 1, Root: Disk{SizeBytes: 4}, Slices: []Slice{{Account: "acme"}}, SignIns: []SignIn{{User: "daffy"}}, Bans: []Ban{{Address: "192.0.2.77"}}}
	if err := json.Unmarshal([]byte(rawReport), &report); err != nil || !reflect.DeepEqual(report, reportWant) {
		t.Fatalf("report = %+v, %v, want %+v", report, err, reportWant)
	}
	var backupState DesiredState
	rawBackupState := `{"protocol":1,"host":"host01","version":4,"backup":{"bucket":"test","future":true,"settings":{"hourly":24,"future":true},"host":{"future":true},"accounts":[{"account":"acme","credentials":{"future":true},"future":true}],"restore":{"accounts":[{"slug":"acme","future":true}],"future":true},"restores":[{"kind":"files","future":true}]}}`
	wantBackupState := DesiredState{Protocol: 1, Host: "host01", Version: 4, Backup: &Backup{
		Bucket: "test", Settings: Settings{Hourly: 24}, Accounts: []AccountCredentials{{Account: "acme"}},
		Restore: &HostRestore{Accounts: []RestoreAccount{{Slug: "acme"}}}, Restores: []RestoreRequest{{Kind: RestoreFiles}},
	}}
	if err := json.Unmarshal([]byte(rawBackupState), &backupState); err != nil || !reflect.DeepEqual(backupState, wantBackupState) {
		t.Errorf("backup state = %+v, %v, want %+v", backupState, err, wantBackupState)
	}
	var backupReport Report
	rawBackupReport := `{"protocol":1,"backups":[{"account":"acme","future":true,"final":{"state":"done","future":true},"databases":[{"path":"data/acme.db","future":true}]}],"host_backup":{"future":true},"restores":[{"state":"pending","future":true}]}`
	wantBackupReport := Report{Protocol: 1, Backups: []AccountBackup{{Account: "acme", Final: Step{State: StepDone}, Databases: []Database{{Path: "data/acme.db"}}}}, Restores: []RestoreResult{{State: StepPending}}}
	if err := json.Unmarshal([]byte(rawBackupReport), &backupReport); err != nil || !reflect.DeepEqual(backupReport, wantBackupReport) {
		t.Errorf("backup report = %+v, %v, want %+v", backupReport, err, wantBackupReport)
	}
}

// R-U83B-U4OL, R-UBR0-ZFWO.
func TestDecoderInputBounds(t *testing.T) {
	stateInput := new(infiniteReader)
	state, err := DecodeState(stateInput)
	if !errors.Is(err, ErrTooLarge) || !reflect.DeepEqual(state, DesiredState{}) || stateInput.read != MaxStateBytes+1 {
		t.Fatalf("state oversize = %+v, %v, read %d", state, err, stateInput.read)
	}
	reportInput := new(infiniteReader)
	report, err := DecodeReport(reportInput)
	if !errors.Is(err, ErrTooLarge) || !reflect.DeepEqual(report, Report{}) || reportInput.read != MaxReportBytes+1 {
		t.Fatalf("report oversize = %+v, %v, read %d", report, err, reportInput.read)
	}
}

// R-G1G6-0R83, R-G3VY-SAPH.
func TestDecodeState(t *testing.T) {
	for _, tt := range []struct {
		json string
		err  error
		want DesiredState
	}{
		{json: "", err: ErrMalformed},
		{json: "[]", err: ErrMalformed},
		{json: "null", err: ErrMalformed},
		{json: `{"protocol":1,"host":"host01","version":1} {}`, err: ErrMalformed},
		{json: `{"protocol":1,"host":"","version":1}`, err: ErrMalformed},
		{json: `{"protocol":1,"host":"host01","version":0}`, err: ErrMalformed},
		{json: `{"protocol":"1","host":"host01","version":1}`, err: ErrMalformed},
		{json: `{"protocol":1,"host":"host01","version":1,"unban":"192.0.2.7"}`, err: ErrMalformed},
		{json: `{"protocol":1,"host":"host01","version":1,"backup":{"restore":{"accounts":[{"gid":"bad"}]}}}`, err: ErrMalformed},
		{json: `{"protocol":1,"host":"host01","version":1,"backup":{"restore":{"id":"replace","ID":null},"host":{"expires":"2026-11-18T12:00:00Z","EXPIRES":null}}}`, want: DesiredState{Protocol: 1, Host: "host01", Version: 1, Backup: &Backup{Restore: &HostRestore{}}}},
		{json: `{"protocol":1,"host":"host01","version":1,"allowlist":["192.0.2.0/24"],"unban":["198.51.100.7"],"domain":"hosts.example.com"}`, want: DesiredState{Protocol: 1, Host: "host01", Version: 1, Allowlist: []string{"192.0.2.0/24"}, Unban: []string{"198.51.100.7"}, Domain: "hosts.example.com"}},
		{json: `{"protocol":2,"host":"","version":0}`, err: ErrVersion},
		{json: `{"host":"host01","version":1}`, err: ErrVersion},
		{json: `{"protocol":null,"host":"host01","version":1}`, err: ErrVersion},
		{json: `{"protocol":1,"Protocol":null,"host":"host01","version":1}`, err: ErrVersion},
		{json: `{"protocol":1,"host":"host01","HOST":null,"version":1}`, err: ErrMalformed},
		{json: `{"protocol":1,"host":"host01","version":4,"accounts":[{"gid":42,"GID":null}],"users":[{"name":"daffy","NAME":null}]}`, want: DesiredState{Protocol: 1, Host: "host01", Version: 4, Accounts: []Account{{}}, Users: []User{{}}}},
		{json: "	" + `{"protocol":2,"Protocol":1,"HOST":"host01","version":1,"daemon":"sha256:ab"}` + "\n", want: DesiredState{Protocol: 1, Host: "host01", Version: 1, Daemon: "sha256:ab"}},
		{json: `{"protocol":1,"host":"host01","version":4,"daemon":"sha256:ab","accounts":[{"state":"future"}],"users":[{"name":"daffy","state":"suspended","keys":null}],"data_volume":null}`, want: DesiredState{Protocol: 1, Host: "host01", Version: 4, Daemon: "sha256:ab", Accounts: []Account{{State: "future"}}, Users: []User{{Name: "daffy", State: "suspended"}}}},
	} {
		got, err := DecodeState(strings.NewReader(tt.json))
		if !errors.Is(err, tt.err) || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode %q = %+v, %v; want %+v, %v", tt.json, got, err, tt.want, tt.err)
		}
	}
}

// R-G2O2-EIYS.
func TestDecodeReport(t *testing.T) {
	for _, tt := range []struct {
		json string
		err  error
		want Report
	}{
		{json: "false", err: ErrMalformed},
		{json: `{"protocol":1} null`, err: ErrMalformed},
		{json: `{"protocol":1,"booted":"yesterday"}`, err: ErrMalformed},
		{json: `{"protocol":1,"sign_ins":[{"at":"yesterday"}]}`, err: ErrMalformed},
		{json: `{"protocol":1,"bans":[{"expires":"yesterday"}]}`, err: ErrMalformed},
		{json: `{"protocol":1,"host_backup":{"final":{"at":"yesterday"}}}`, err: ErrMalformed},
		{json: `{"protocol":1,"backups":[{"account":"beta"},{"account":"acme"}],"restores":[{"id":"done","copy":"restored/example.txt","COPY":null}]}`, want: Report{Protocol: 1, Backups: []AccountBackup{{Account: "acme"}, {Account: "beta"}}, Restores: []RestoreResult{{ID: "done"}}}},
		{json: `{"protocol":1,"bans":[{"address":"198.51.100.7"},{"address":"192.0.2.77"}],"allowlist":["2001:db8::/48","192.0.2.0/24"]}`, want: Report{Protocol: 1, Bans: []Ban{{Address: "192.0.2.77"}, {Address: "198.51.100.7"}}, BanTotal: 2, Allowlist: []string{"192.0.2.0/24", "2001:db8::/48"}}},
		{json: `{"protocol":1,"attempted":"1"}`, err: ErrMalformed},
		{json: `{"protocol":2}`, err: ErrVersion},
		{json: `{"protocol":null}`, err: ErrVersion},
		{json: `{"protocol":1,"Protocol":null}`, err: ErrVersion},
		{json: `{"protocol":1,"root":{"size_bytes":1},"ROOT":{"used_bytes":2},"data":{"size_bytes":3},"DATA":null,"booted":"2026-11-17T12:13:14Z","BOOTED":null,"sign_ins":[{"user":"daffy","at":"2026-11-17T12:13:14Z","At":null}]}`, want: Report{Protocol: 1, Root: Disk{UsedBytes: 2}, SignIns: []SignIn{{User: "daffy"}}}},
		{json: `{}`, err: ErrVersion},
		{json: " \n" + `{"protocol":2,"Protocol":1,"Attempted":3,"booted":null,"failed_units":["z","a"],"hostname":"host01.hosts.example.com"}` + " 	", want: Report{Protocol: 1, Attempted: 3, FailedUnits: []string{"a", "z"}, Hostname: "host01.hosts.example.com"}},
	} {
		got, err := DecodeReport(strings.NewReader(tt.json))
		if !errors.Is(err, tt.err) || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode %q = %+v, %v; want %+v, %v", tt.json, got, err, tt.want, tt.err)
		}
	}
}

// R-GLHD-0XVJ.
func TestBounded(t *testing.T) {
	original := Report{Attempted: 7, Error: strings.Repeat("e", MaxErrorBytes-1) + "é", OS: strings.Repeat("a", 300)}
	want := Report{Attempted: 7, Error: strings.Repeat("e", MaxErrorBytes-1), OS: strings.Repeat("a", 254)}
	for i := MaxSignIns + 10; i >= 0; i-- {
		name := fmt.Sprintf("name%03d", i)
		original.FailedUnits = append(original.FailedUnits, name)
		original.Slices = append(original.Slices, Slice{Account: name})
		original.SignIns = append(original.SignIns, SignIn{User: name})
	}
	for i := range MaxSignIns {
		name := fmt.Sprintf("name%03d", i)
		if i < MaxFailedUnits {
			want.FailedUnits = append(want.FailedUnits, name)
		}
		if i < MaxSlices {
			want.Slices = append(want.Slices, Slice{Account: name})
		}
		want.SignIns = append(want.SignIns, SignIn{User: name})
	}
	const nBans = 250
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for i := range nBans {
		banned := start.Add(time.Duration(i) * time.Second)
		original.Bans = append(original.Bans, Ban{Address: fmt.Sprintf("192.0.2.%d", i), Banned: banned, Expires: banned.Add(time.Hour)})
	}
	for i := MaxAllowlist + 10; i >= 0; i-- {
		original.Allowlist = append(original.Allowlist, fmt.Sprintf("2001:db8::%x", i+16))
	}
	for i := range MaxAllowlist {
		want.Allowlist = append(want.Allowlist, fmt.Sprintf("2001:db8::%x", i+16))
	}
	for i := nBans - 1; i >= nBans-MaxBans; i-- {
		want.Bans = append(want.Bans, original.Bans[i])
	}
	want.BanTotal = nBans
	before, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	got := original.Bounded()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bounded report = %+v, want %+v", got, want)
	}
	after, err := json.Marshal(original)
	if err != nil || string(before) != string(after) {
		t.Fatal("Bounded mutated its receiver")
	}
	original.BanTotal = 300
	want.BanTotal = 300
	if got := original.Bounded(); !reflect.DeepEqual(got, want) {
		t.Errorf("bounded report with original total 300 = %+v, want %+v", got, want)
	}
	tied := Report{OS: strings.Repeat("<", 50), Bans: []Ban{{Address: "198.51.100.7", Banned: start}, {Address: "192.0.2.77", Banned: start}}}
	wantBans := []Ban{tied.Bans[1], tied.Bans[0]}
	if got := tied.Bounded(); !reflect.DeepEqual(got.Bans, wantBans) || got.OS != strings.Repeat("<", 42) {
		t.Errorf("bounded tied bans = %+v, want %+v, OS = %q", got.Bans, wantBans, got.OS)
	}
}

// R-UFEQ-4R4R, R-UHUI-WAM5.
func TestDigest(t *testing.T) {
	for _, data := range []string{"", strings.Repeat("!binaryData", 100_000)} {
		body := &digestReader{r: strings.NewReader(data)}
		got, err := Digest(body)
		want := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(data)))
		if err != nil || got != want || body.maxRead > DigestBuffer || body.writeToCalled {
			t.Fatalf("digest = %q, %v; want %q, max read %d, WriterTo %t", got, err, want, body.maxRead, body.writeToCalled)
		}
	}
	readError := errors.New("fixture read failure")
	body := &digestReader{r: strings.NewReader("abc"), readError: readError}
	if got, err := Digest(body); got != "" || !errors.Is(err, readError) {
		t.Fatalf("failed digest = %q, %v", got, err)
	}
}

type infiniteReader struct{ read int }

func (r *infiniteReader) Read(p []byte) (int, error) {
	clear(p)
	r.read += len(p)
	return len(p), nil
}

type digestReader struct {
	r             io.Reader
	maxRead       int
	writeToCalled bool
	readError     error
}

func (r *digestReader) Read(p []byte) (int, error) {
	r.maxRead = max(r.maxRead, len(p))
	n, err := r.r.Read(p)
	if err == io.EOF && r.readError != nil {
		err = r.readError
	}
	return n, err
}

func (r *digestReader) WriteTo(io.Writer) (int64, error) {
	r.writeToCalled = true
	return 0, errors.New("fixture WriterTo must not be used")
}

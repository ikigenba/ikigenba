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

// R-TZK1-5QHQ, R-U0RX-JI8F, R-U1ZT-X9Z4, R-U37Q-B1PT, R-U5NJ-2L77.
func TestWireObjects(t *testing.T) {
	when := time.Date(2026, 11, 17, 12, 13, 14, 0, time.UTC)
	account := Account{"acme", 20000, AccountActive}
	user := User{"daffy", "acme", 20001, UserLocked, []string{}}
	disk := Disk{1, 2, 3, 4}
	slice := Slice{"acme", 6, 7}
	signIn := SignIn{"daffy", when}
	state := DesiredState{1, "host01", 7, "", "", 20, 50, []Account{account}, []User{user}}
	report := Report{1, 7, 6, "failure", "instance", "instance-type", "image", "os", "kernel", when, "digest", [3]float64{1, 2, 3}, 5, 6, 7, 8, 9, disk, Disk{10, 11, 12, 13}, 14, 15, true, []string{"unit"}, []Slice{slice}, []SignIn{signIn}}
	for _, tt := range []struct {
		value any
		json  string
	}{
		{state, `{"protocol":1,"host":"host01","version":7,"daemon":"","data_volume":"","root_gb":20,"data_gb":50,"accounts":[{"slug":"acme","gid":20000,"state":"active"}],"users":[{"name":"daffy","account":"acme","uid":20001,"state":"locked","keys":[]}]}`},
		{report, `{"protocol":1,"attempted":7,"applied":6,"error":"failure","instance_id":"instance","type":"instance-type","ami":"image","os":"os","kernel":"kernel","booted":"2026-11-17T12:13:14Z","daemon_version":"digest","load":[1,2,3],"cpu_percent":5,"memory_used":6,"memory_available":7,"swap_used":8,"swap_total":9,"root":{"used_bytes":1,"size_bytes":2,"inodes_used":3,"inodes_total":4},"data":{"used_bytes":10,"size_bytes":11,"inodes_used":12,"inodes_total":13},"sessions":14,"updates":15,"reboot":true,"failed_units":["unit"],"slices":[{"account":"acme","cpu_percent":6,"memory_bytes":7}],"sign_ins":[{"user":"daffy","at":"2026-11-17T12:13:14Z"}]}`},
		{Report{}, `{"protocol":0,"attempted":0,"applied":0,"error":"","instance_id":"","type":"","ami":"","os":"","kernel":"","booted":"0001-01-01T00:00:00Z","daemon_version":"","load":[0,0,0],"cpu_percent":0,"memory_used":0,"memory_available":0,"swap_used":0,"swap_total":0,"root":{"used_bytes":0,"size_bytes":0,"inodes_used":0,"inodes_total":0},"data":{"used_bytes":0,"size_bytes":0,"inodes_used":0,"inodes_total":0},"sessions":0,"updates":0,"reboot":false,"failed_units":null,"slices":null,"sign_ins":null}`},
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
	rawReport := `{"protocol":1,"future":true,"root":{"size_bytes":4,"future":true},"data":{"future":true},"slices":[{"account":"acme","future":true}],"sign_ins":[{"user":"daffy","future":true}]}`
	reportWant := Report{Protocol: 1, Root: Disk{SizeBytes: 4}, Slices: []Slice{{Account: "acme"}}, SignIns: []SignIn{{User: "daffy"}}}
	if err := json.Unmarshal([]byte(rawReport), &report); err != nil || !reflect.DeepEqual(report, reportWant) {
		t.Fatalf("report = %+v, %v, want %+v", report, err, reportWant)
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
		{json: `{"protocol":1,"attempted":"1"}`, err: ErrMalformed},
		{json: `{"protocol":2}`, err: ErrVersion},
		{json: `{"protocol":null}`, err: ErrVersion},
		{json: `{"protocol":1,"Protocol":null}`, err: ErrVersion},
		{json: `{"protocol":1,"root":{"size_bytes":1},"ROOT":{"used_bytes":2},"data":{"size_bytes":3},"DATA":null,"booted":"2026-11-17T12:13:14Z","BOOTED":null,"sign_ins":[{"user":"daffy","at":"2026-11-17T12:13:14Z","At":null}]}`, want: Report{Protocol: 1, Root: Disk{UsedBytes: 2}, SignIns: []SignIn{{User: "daffy"}}}},
		{json: `{}`, err: ErrVersion},
		{json: " \n" + `{"protocol":2,"Protocol":1,"Attempted":3,"booted":null,"failed_units":["z","a"]}` + " 	", want: Report{Protocol: 1, Attempted: 3, FailedUnits: []string{"a", "z"}}},
	} {
		got, err := DecodeReport(strings.NewReader(tt.json))
		if !errors.Is(err, tt.err) || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("decode %q = %+v, %v; want %+v, %v", tt.json, got, err, tt.want, tt.err)
		}
	}
}

// R-UE6T-QZE2.
func TestBounded(t *testing.T) {
	original := Report{Attempted: 7, Error: strings.Repeat("e", MaxErrorBytes-1) + "é"}
	want := Report{Attempted: 7, Error: strings.Repeat("e", MaxErrorBytes-1)}
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

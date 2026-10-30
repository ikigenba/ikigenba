package host_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/host"
)

type stub struct{ path, argv, count, input string }

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func makeStub(t *testing.T, body string) stub {
	t.Helper()
	dir := t.TempDir()
	s := stub{filepath.Join(dir, "stub"), filepath.Join(dir, "argv"), filepath.Join(dir, "count"), filepath.Join(dir, "input")}
	script := "#!/bin/sh\nprintf '%s\\000' \"$@\" >> " + shellQuote(s.argv) + "\nprintf x >> " + shellQuote(s.count) + "\nif IFS= read -r value; then printf data > " + shellQuote(s.input) + "; else printf eof > " + shellQuote(s.input) + "; fi\n" + body + "\n"
	if err := os.WriteFile(s.path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := root.Chmod("stub", 0700); err != nil {
		t.Fatal(err)
	}
	return s
}
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, e := os.ReadFile(filepath.Clean(path))
	if e != nil {
		t.Fatal(e)
	}
	return data
}
func assertOnce(t *testing.T, s stub) {
	t.Helper()
	if got := string(readFile(t, s.count)); got != "x" {
		t.Fatalf("invocations %q", got)
	}
	if got := string(readFile(t, s.input)); got != "eof" {
		t.Fatalf("stdin %q", got)
	}
}

// R-HWRN-PRM5 R-W8ME-8X4M R-W9UA-MOVB R-I7QR-5PAE R-WCA3-E8CP R-IA6J-X8RS R-IBEG-B0IH
func TestCommandArgumentsAndSuccess(t *testing.T) {
	name, group, key := "A name\n'quoted'", "bad group; $value", "ssh-ed25519 blob comment \"quoted\"\n"
	for _, op := range invocations(background, name, group, key) {
		t.Run(op.name, func(t *testing.T) {
			body := "printf ignored; printf ignored >&2; exit 0"
			switch op.name {
			case "group list":
				body = "printf '%s' '{\"groups\":[]}'"
			case "group members":
				body = "printf '%s' " + shellQuote(`{"group":`+fmt.Sprintf("%q", group)+`,"members":[]}`)
			case "user list":
				body = "printf '%s' '{\"users\":[]}'"
			}
			s := makeStub(t, body)
			fields := []string{s.path, "leading \"value\""}
			c := host.NewCommand(fields)
			fields[0] = "/does-not-exist"
			fields[1] = "changed"
			var h host.Host = c
			_, err := op.run(h)
			requireOK(t, err)
			assertOnce(t, s)
			want := []string{"leading \"value\""}
			switch op.name {
			case "group add":
				want = append(want, "group", "add", group)
			case "group remove":
				want = append(want, "group", "remove", group)
			case "group list":
				want = append(want, "group", "list")
			case "group members":
				want = append(want, "group", "members", group)
			case "user add":
				want = append(want, "user", "add", name, group, key)
			case "user list":
				want = append(want, "user", "list")
			case "user key add":
				want = append(want, "user", "key", "add", name, key)
			case "user key remove":
				want = append(want, "user", "key", "remove", name, key)
			case "user join":
				want = append(want, "user", "join", name, group)
			case "user leave":
				want = append(want, "user", "leave", name, group)
			case "user lock":
				want = append(want, "user", "lock", name)
			case "user unlock":
				want = append(want, "user", "unlock", name)
			case "user delete":
				want = append(want, "user", "delete", name)
			}
			got := bytes.Split(readFile(t, s.argv), []byte{0})
			got = got[:len(got)-1]
			if len(got) != len(want) {
				t.Fatal(got, want)
			}
			for i := range want {
				if string(got[i]) != want[i] {
					t.Fatalf("arg %d %q want %q", i, got[i], want[i])
				}
			}
		})
	}
	// Resolve a command name through PATH while the only executed script is ours.
	s := makeStub(t, "exit 0")
	requireOK(t, host.NewCommand([]string{"sh", s.path, "-x"}).GroupAdd(background, "chosen"))
	assertOnce(t, s)
	if got := string(readFile(t, s.argv)); got != "-x\x00group\x00add\x00chosen\x00" {
		t.Fatalf("PATH argv %q", got)
	}
}

// R-I1N9-8UKX R-ICMC-OS96 R-IF25-GBQK R-IGA1-U3H9 R-WB27-0GM0 R-WCA3-E8CP
func TestCommandExitClassification(t *testing.T) {
	cases := []struct {
		status     int
		stderr     string
		refusal    bool
		diagnostic string
	}{
		{2, host.DiagnosticPrefix + "  chosen \r\n\ndetails", true, "  chosen \r"},
		{4, "without prefix\nmore", true, "without prefix"},
		{4, host.DiagnosticPrefix + host.DiagnosticPrefix + "twice", true, host.DiagnosticPrefix + "twice"},
		{2, "", false, ""}, {4, "\nsecond", false, ""}, {2, host.DiagnosticPrefix, false, ""},
		{1, host.DiagnosticPrefix + "diagnostic", false, ""}, {3, "problem", false, ""}, {127, "problem", false, ""},
	}
	for i, tc := range cases {
		for _, op := range invocations(background, "person", "business", publicKey(1)) {
			t.Run(fmt.Sprintf("%d/%s", i, op.name), func(t *testing.T) {
				s := makeStub(t, "printf '%s' 'ignored stdout'; printf '%s' "+shellQuote(tc.stderr)+" >&2; exit "+fmt.Sprint(tc.status))
				got, err := op.run(host.NewCommand([]string{s.path}))
				if tc.refusal {
					requireRefused(t, err, tc.status, tc.diagnostic)
				} else {
					requireFailed(t, err, tc.status)
				}
				if !reflect.DeepEqual(got, op.zero) {
					t.Fatal(got)
				}
				assertOnce(t, s)
			})
		}
	}
	nonexec := filepath.Join(t.TempDir(), "nonexec")
	if err := os.WriteFile(nonexec, []byte("chosen"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fields := range [][]string{nil, {}, {""}, {"/no/such/command"}, {"no-such-accounts-test-command"}, {nonexec}} {
		for _, op := range invocations(background, "person", "business", publicKey(1)) {
			got, err := op.run(host.NewCommand(fields))
			requireFailed(t, err, -1)
			if !reflect.DeepEqual(got, op.zero) {
				t.Fatal(got)
			}
		}
	}
	for _, op := range invocations(background, "person", "business", publicKey(1)) {
		s := makeStub(t, "kill -TERM $$")
		got, err := op.run(host.NewCommand([]string{s.path}))
		requireFailed(t, err, -1)
		if !reflect.DeepEqual(got, op.zero) {
			t.Fatal(got)
		}
		assertOnce(t, s)
	}
}

// R-IJXQ-ZEPC R-IL5N-D6G1 R-WEPW-5RU3
func TestCommandListDocuments(t *testing.T) {
	t.Run("groups", func(t *testing.T) {
		for _, tc := range []struct {
			data string
			want []host.Group
		}{
			{" \n" + `{"groups":[{"slug":"zeta","members":3,"future":true},{"slug":"Alpha","members":0}],"future":17}` + "\t", []host.Group{{"Alpha", 0}, {"zeta", 3}}},
			{`{"groups":[]}`, []host.Group{}}, {`{"groups":null}`, []host.Group{}}, {`{"future":0}`, []host.Group{}},
		} {
			s := makeStub(t, "printf '%s' "+shellQuote(tc.data))
			got, e := host.NewCommand([]string{s.path}).GroupList(background)
			requireOK(t, e)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatal(got, tc.want)
			}
		}
	})
	t.Run("members", func(t *testing.T) {
		for _, tc := range []struct {
			data string
			want []string
		}{
			{`{"group":"chosen","members":["zeta","Alpha","zeta",""],"future":false}`, []string{"", "Alpha", "zeta", "zeta"}},
			{`{"group":"chosen","members":[]}`, []string{}}, {`{"group":"chosen","members":null}`, []string{}}, {`{"group":"chosen"}`, []string{}},
		} {
			s := makeStub(t, "printf '%s' "+shellQuote(tc.data))
			got, e := host.NewCommand([]string{s.path}).GroupMembers(background, "chosen")
			requireOK(t, e)
			if !reflect.DeepEqual(got, host.Members{"chosen", tc.want}) {
				t.Fatal(got, tc.want)
			}
		}
	})
	t.Run("users", func(t *testing.T) {
		data := `{"users":[{"name":"zoe","groups":["zeta","Alpha"],"keys":["second","first"],"state":"locked","last_sign_in":"2026-10-29T09:03:07-05:00","future":1},{"name":"amy","state":"active","groups":null,"keys":null,"last_sign_in":null},{"name":"empty","state":"active"}],"future":true}`
		s := makeStub(t, "printf '%s' "+shellQuote(data))
		got, e := host.NewCommand([]string{s.path}).UserList(background)
		requireOK(t, e)
		want := []host.User{{"amy", []string{}, []string{}, host.StateActive, time.Time{}}, {"empty", []string{}, []string{}, host.StateActive, time.Time{}}, {"zoe", []string{"Alpha", "zeta"}, []string{"second", "first"}, host.StateLocked, time.Date(2026, 10, 29, 14, 3, 7, 0, time.UTC)}}
		if !reflect.DeepEqual(got, want) || got[2].LastSignIn.Location() != time.UTC {
			t.Fatal(got, want)
		}
		for _, data := range []string{`{"users":[]}`, `{"users":null}`, `{"future":0}`} {
			s := makeStub(t, "printf '%s' "+shellQuote(data))
			got, e := host.NewCommand([]string{s.path}).UserList(background)
			requireOK(t, e)
			if got == nil || len(got) != 0 {
				t.Fatal(got)
			}
		}
	})
}

// R-WFXS-JJKS
func TestCommandRejectsBadDocuments(t *testing.T) {
	common := []string{"", "garbage", "{} {}", "null", "[]", "true", "1", `"text"`}
	groups := []string{`{"groups":1}`, `{"groups":{}}`, `{"groups":"bad"}`, `{"groups":[null]}`, `{"groups":[1]}`, `{"groups":[{}]}`, `{"groups":[{"slug":"x"}]}`, `{"groups":[{"slug":null,"members":1}]}`, `{"groups":[{"slug":"","members":1}]}`, `{"groups":[{"slug":9,"members":1}]}`, `{"groups":[{"slug":"x","members":null}]}`, `{"groups":[{"slug":"x","members":-1}]}`, `{"groups":[{"slug":"x","members":1.5}]}`, `{"groups":[{"slug":"x","members":"1"}]}`}
	members := []string{`{}`, `{"group":null}`, `{"group":1}`, `{"group":"other"}`, `{"group":"chosen","members":1}`, `{"group":"chosen","members":{}}`, `{"group":"chosen","members":[null]}`, `{"group":"chosen","members":[1]}`}
	users := []string{`{"users":true}`, `{"users":{}}`, `{"users":[null]}`, `{"users":[{}]}`, `{"users":[{"name":null,"state":"active"}]}`, `{"users":[{"name":"","state":"active"}]}`, `{"users":[{"name":1,"state":"active"}]}`, `{"users":[{"name":"x"}]}`, `{"users":[{"name":"x","state":null}]}`, `{"users":[{"name":"x","state":"unknown"}]}`, `{"users":[{"name":"x","state":"active","groups":{}}]}`, `{"users":[{"name":"x","state":"active","groups":[null]}]}`, `{"users":[{"name":"x","state":"active","keys":[1]}]}`, `{"users":[{"name":"x","state":"active","keys":[null]}]}`, `{"users":[{"name":"x","state":"active","last_sign_in":"bad"}]}`, `{"users":[{"name":"x","state":"active","last_sign_in":1}]}`}
	for _, tc := range []struct {
		kind string
		data []string
	}{{"group list", groups}, {"group members", members}, {"user list", users}} {
		for _, data := range append(append([]string{}, common...), tc.data...) {
			s := makeStub(t, "printf '%s' "+shellQuote(data))
			c := host.NewCommand([]string{s.path})
			switch tc.kind {
			case "group list":
				got, e := c.GroupList(background)
				requireFailed(t, e, 0)
				if got != nil {
					t.Fatal(data, got)
				}
			case "group members":
				got, e := c.GroupMembers(background, "chosen")
				requireFailed(t, e, 0)
				if !reflect.DeepEqual(got, host.Members{}) {
					t.Fatal(data, got)
				}
			case "user list":
				got, e := c.UserList(background)
				requireFailed(t, e, 0)
				if got != nil {
					t.Fatal(data, got)
				}
			}
		}
	}
}

// R-WDHZ-S03E R-WCA3-E8CP
func TestCommandCancellation(t *testing.T) {
	cancelled, cancel := context.WithCancel(background)
	cancel()
	s := makeStub(t, "exit 0")
	for _, op := range invocations(cancelled, "person", "business", publicKey(1)) {
		got, e := op.run(host.NewCommand([]string{s.path}))
		requireFailed(t, e, -1)
		if !reflect.DeepEqual(got, op.zero) {
			t.Fatal(got)
		}
	}
	if _, e := os.Stat(s.count); !os.IsNotExist(e) {
		t.Fatal("cancelled command started", e)
	}
	for _, op := range invocations(background, "person", "business", publicKey(1)) {
		t.Run(op.name, func(t *testing.T) {
			dir := t.TempDir()
			ready, block := filepath.Join(dir, "ready"), filepath.Join(dir, "block")
			for _, path := range []string{ready, block} {
				if e := syscall.Mkfifo(path, 0600); e != nil {
					t.Fatal(e)
				}
			}
			// Holding both ends admits the child's read, which stays blocked until cleanup.
			hold, e := os.OpenFile(filepath.Clean(block), os.O_RDWR, 0600)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = hold.Close() }()
			s := makeStub(t, "if [ \"$1\" = child ]; then exec 3< "+shellQuote(block)+"; printf x > "+shellQuote(ready)+"; IFS= read -r value <&3; exit 0; fi\n/bin/sh \"$0\" child &\nwait")
			ctx, cancel := context.WithCancel(background)
			defer cancel()
			done := make(chan struct{})
			var got any
			var err error
			for _, call := range invocations(ctx, "person", "business", publicKey(1)) {
				if call.name == op.name {
					go func() { got, err = call.run(host.NewCommand([]string{s.path})); close(done) }()
					break
				}
			}
			signaled := make(chan error, 1)
			go func() {
				f, e := os.Open(filepath.Clean(ready))
				if e != nil {
					signaled <- e
					return
				}
				var b [1]byte
				_, e = f.Read(b[:])
				e2 := f.Close()
				if e == nil {
					e = e2
				}
				signaled <- e
			}()
			select {
			case e := <-signaled:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("stub did not become ready")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("cancelled command waited for descendant")
			}
			requireFailed(t, err, -1)
			if !reflect.DeepEqual(got, op.zero) {
				t.Fatal(got)
			}
		})
	}
}

// R-IBEG-B0IH
func TestCommandSuccessfulExitWithInheritedPipes(t *testing.T) {
	dir := t.TempDir()
	ready, block := filepath.Join(dir, "ready"), filepath.Join(dir, "block")
	for _, path := range []string{ready, block} {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	hold, err := os.OpenFile(filepath.Clean(block), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Close() }()
	s := makeStub(t, "if [ \"$1\" = child ]; then exec 3< "+shellQuote(block)+"; printf 'ready\\n' > "+shellQuote(ready)+"; IFS= read -r value <&3; exit 0; fi\n/bin/sh \"$0\" child &\nIFS= read -r value < "+shellQuote(ready)+"\nprintf ignored; printf ignored >&2; exit 0")
	requireOK(t, host.NewCommand([]string{s.path}).UserLock(background, "person"))
}

// R-IBEG-B0IH R-IJXQ-ZEPC R-IL5N-D6G1 R-WEPW-5RU3 R-ICMC-OS96
func TestCommandCancellationAfterDirectExit(t *testing.T) {
	for _, op := range invocations(background, "person", "business", publicKey(1)) {
		t.Run(op.name, func(t *testing.T) {
			output := "ignored"
			var want any
			switch op.name {
			case "group list":
				output = `{"groups":[{"slug":"business","members":1}]}`
				want = []host.Group{{"business", 1}}
			case "group members":
				output = `{"group":"business","members":["amy"]}`
				want = host.Members{"business", []string{"amy"}}
			case "user list":
				output = `{"users":[{"name":"amy","state":"active"}]}`
				want = []host.User{{"amy", []string{}, []string{}, host.StateActive, time.Time{}}}
			}
			got, err := callAfterDirectExit(t, op.name, output, "ignored", 0)
			requireOK(t, err)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("late cancellation result %v want %v", got, want)
			}
		})
	}
	for _, status := range []int{2, 4} {
		got, err := callAfterDirectExit(t, "group list", "ignored", host.DiagnosticPrefix+"chosen diagnostic\nmore", status)
		requireRefused(t, err, status, "chosen diagnostic")
		if got != nil && !reflect.DeepEqual(got, []host.Group(nil)) {
			t.Fatal(got)
		}
	}
}

func callAfterDirectExit(t *testing.T, method, output, diagnostic string, status int) (any, error) {
	t.Helper()
	dir := t.TempDir()
	ready, block := filepath.Join(dir, "ready"), filepath.Join(dir, "block")
	for _, path := range []string{ready, block} {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	hold, err := os.OpenFile(filepath.Clean(block), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Close() }()
	signal, err := os.OpenFile(filepath.Clean(ready), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = signal.Close() }()
	// The child signals only after kill -0 observes that the direct program was
	// reaped. It keeps both inherited output pipes open until this test releases it.
	s := makeStub(t, "if [ \"$1\" = child ]; then while kill -0 \"$2\" 2>/dev/null; do :; done; exec 3< "+shellQuote(block)+"; printf x > "+shellQuote(ready)+"; IFS= read -r value <&3; exit 0; fi\nprintf '%s' "+shellQuote(output)+"; printf '%s' "+shellQuote(diagnostic)+" >&2\n/bin/sh \"$0\" child \"$$\" &\nexit "+fmt.Sprint(status))
	ctx, cancel := context.WithCancel(background)
	defer cancel()
	var got any
	var resultErr error
	done := make(chan struct{})
	for _, op := range invocations(ctx, "person", "business", publicKey(1)) {
		if op.name == method {
			go func() { got, resultErr = op.run(host.NewCommand([]string{s.path})); close(done) }()
			break
		}
	}
	announced := make(chan error, 1)
	go func() { var b [1]byte; _, err := signal.Read(b[:]); announced <- err }()
	select {
	case err := <-announced:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("direct program was not reaped")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("output drain did not finish")
	}
	return got, resultErr
}

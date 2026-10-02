package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-EXKJ-SM9X R-YIB1-WSJJ R-S359-R7SE R-LO8H-1R7W R-S80V-AAR6
func TestLogsJournalCommand(t *testing.T) {
	cases := []struct{ args, want []string }{
		{[]string{"logs"}, []string{"--user", "--no-pager", "--output=short", "--lines=100", "--unit=sandbox-wip-nginx.service", "--unit=sandbox-wip-auth.socket", "--unit=sandbox-wip-auth.service", "--unit=sandbox-wip-dummy.socket", "--unit=sandbox-wip-dummy.service"}},
		{[]string{"logs", "auth"}, []string{"--user", "--no-pager", "--output=short", "--lines=100", "--unit=sandbox-wip-auth.socket", "--unit=sandbox-wip-auth.service"}},
		{[]string{"logs", "-n", "020", "-f", "dummy"}, []string{"--user", "--no-pager", "--output=short", "--lines=20", "--follow", "--unit=sandbox-wip-dummy.socket", "--unit=sandbox-wip-dummy.service"}},
		{[]string{"logs", "-n", "02147483647"}, []string{"--user", "--no-pager", "--output=short", "--lines=2147483647", "--unit=sandbox-wip-nginx.service", "--unit=sandbox-wip-auth.socket", "--unit=sandbox-wip-auth.service", "--unit=sandbox-wip-dummy.socket", "--unit=sandbox-wip-dummy.service"}},
	}
	for _, tc := range cases {
		t.Run(tc.args[len(tc.args)-1], func(t *testing.T) {
			f := newReportFixture(t, "wip", []string{"dummy", "auth"}, 7400)
			f.expect(f.run(context.Background(), tc.args, nil), 0, "", "")
			want := []seam.Cmd{{Path: "journalctl", Dir: "/", Args: tc.want}}
			if !reflect.DeepEqual(f.streams, want) || len(f.execs) != 1 || f.execs[0].Path != "git" {
				t.Fatalf("calls %#v %#v", f.execs, f.streams)
			}
		})
	}
}

// R-0WMO-FPD8 R-IZPL-JMUM
func TestLogsCommandEquivalence(t *testing.T) {
	f := newReportFixture(t, "wip", []string{"auth", "dummy"}, 7400)
	command := func(args ...string) seam.Cmd {
		t.Helper()
		f.expect(f.run(context.Background(), args, nil), 0, "", "")
		if len(f.streams) != 1 {
			t.Fatalf("streams %v", f.streams)
		}
		return f.streams[0]
	}
	groups := [][][]string{
		{{"logs", "-n", "20", "dummy"}, {"logs", "--lines", "20", "dummy"}, {"logs", "dummy", "-n", "20"}, {"logs", "-n", "5", "-n", "20", "dummy"}, {"logs", "-n", "020", "dummy"}},
		{{"logs"}, {"logs", "-n", "100"}},
		{{"logs", "-f"}, {"logs", "--follow"}},
	}
	for _, group := range groups {
		want := command(group[0]...)
		for _, args := range group[1:] {
			if got := command(args...); !reflect.DeepEqual(got, want) {
				t.Fatalf("different equivalent cmd %#v %#v", want, got)
			}
		}
	}
	for _, pair := range [][][]string{{{"logs", "-n", "20"}, {"logs", "-n", "21"}}, {{"logs", "-f"}, {"logs"}}, {{"logs", "dummy"}, {"logs", "auth"}}} {
		if reflect.DeepEqual(command(pair[0]...), command(pair[1]...)) {
			t.Fatalf("same different cmds %v", pair)
		}
	}
}

// R-LPGD-FIYL
func TestLogsUnknownApp(t *testing.T) {
	for _, app := range []string{"nope", "nginx", "a\nb", ""} {
		t.Run(app, func(t *testing.T) {
			f := newReportFixture(t, "wip", []string{"auth", "dummy"}, 7400)
			name := app
			if app == "a\nb" {
				name = "a\\x0ab"
			}
			f.expect(f.run(context.Background(), []string{"logs", app}, nil), 2, "", "sandbox: no app '"+name+"' in sandbox 'wip'\n")
			if len(f.execs) != 1 || len(f.streams) != 0 {
				t.Fatalf("calls %v %v", f.execs, f.streams)
			}
		})
	}
	f := newReportFixture(t, "wip", nil, 7400)
	if err := os.Remove(filepath.Join(f.root, "registry.json")); err != nil {
		t.Fatal(err)
	}
	f.expect(f.run(context.Background(), []string{"logs", "nope"}, nil), 2, "", "sandbox: no sandbox 'wip'\n\nrun 'sandbox up' to create it\n")
	if len(f.streams) != 0 {
		t.Fatalf("journalctl called")
	}
}

// R-S6SY-WJ0H
func TestLogsImmediateBytes(t *testing.T) {
	f := newReportFixture(t, "wip", nil, 7400)
	chunks := [][]byte{[]byte("first\n"), []byte("partial"), {0xff, 0x00, 0xc3}, []byte("last\n")}
	f.deps.Stream = func(_ context.Context, _ seam.Cmd, w io.Writer) (seam.Result, error) {
		var want []byte
		for _, chunk := range chunks {
			n, err := w.Write(chunk)
			if err != nil || n != len(chunk) {
				t.Fatalf("write %d %v", n, err)
			}
			want = append(want, chunk...)
			if !bytes.Equal(f.out.Bytes(), want) {
				t.Fatalf("bytes buffered or changed: %q", f.out.Bytes())
			}
		}
		return seam.Result{}, nil
	}
	var want []byte
	for _, chunk := range chunks {
		want = append(want, chunk...)
	}
	f.expect(f.run(context.Background(), []string{"logs"}, nil), 0, string(want), "")
}

// R-Y4HV-VLBH R-YP86-DOXA R-SCWG-TDPY
func TestLogsFailuresAndFollowCancellation(t *testing.T) {
	cases := []struct {
		follow, cancel bool
		result         seam.Result
		err            error
		output, want   string
		code           int
	}{
		{false, false, seam.Result{ExitCode: 1, Output: []byte("Failed to get journal access: Permission denied\n")}, nil, "", "sandbox: journalctl --user: exit status 1\n\n> Failed to get journal access: Permission denied\n", 1},
		{true, false, seam.Result{ExitCode: 2}, nil, "partial", "sandbox: journalctl --user: exit status 2\n", 1},
		{false, true, seam.Result{}, context.Canceled, "", "sandbox: journalctl --user: context canceled\n", 1},
		{true, false, seam.Result{}, errors.New("boom"), "", "sandbox: journalctl --user: boom\n", 1},
		{true, true, seam.Result{}, context.Canceled, "line\n", "", 0},
		{true, true, seam.Result{ExitCode: 130}, nil, "line\n", "", 0},
	}
	for n, tc := range cases {
		t.Run(string(rune('a'+n)), func(t *testing.T) {
			f := newReportFixture(t, "wip", nil, 7400)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.deps.Stream = func(_ context.Context, _ seam.Cmd, w io.Writer) (seam.Result, error) {
				if _, err := io.WriteString(w, tc.output); err != nil {
					t.Fatal(err)
				}
				if tc.cancel {
					cancel()
				}
				return tc.result, tc.err
			}
			args := []string{"logs"}
			if tc.follow {
				args = append(args, "-f")
			}
			f.expect(f.run(ctx, args, nil), tc.code, tc.output, tc.want)
		})
	}
}

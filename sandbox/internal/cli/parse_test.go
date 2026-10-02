package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/sandbox/internal/seam"
)

// R-08X8-B3J2: All command tests share this exit-domain assertion.
func runChecked(ctx context.Context, t testing.TB, args []string, stdin io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	t.Helper()
	code := Run(ctx, args, stdin, stdout, stderr, deps)
	if code < 0 || code > 3 {
		t.Fatalf("invalid exit code %d", code)
	}
	return code
}

type forbiddenReader struct{ t testing.TB }

func (f forbiddenReader) Read([]byte) (int, error) { f.t.Fatal("stdin read"); return 0, io.EOF }
func parseOnlyDeps(t testing.TB, euid int) seam.Deps {
	t.Helper()
	return seam.Deps{Dir: t.TempDir(), EUID: euid, Getenv: func(string) string { t.Fatal("environment lookup"); return "" }, Exec: func(context.Context, seam.Cmd) (seam.Result, error) { t.Fatal("exec"); return seam.Result{}, nil }, Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
		t.Fatal("stream")
		return seam.Result{}, nil
	}}
}
func parseOutput(t testing.TB, args []string, uid int) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	code := runChecked(context.Background(), t, args, forbiddenReader{t}, &out, &err, parseOnlyDeps(t, uid))
	return code, out.String(), err.String()
}

// R-Z3GN-AWBG R-Z4OJ-OO25 R-Z5WG-2FSU R-YD16-JZIC R-Z8C8-TZA8 R-Z9K5-7R0X R-YE92-XR91 R-YFGZ-BIZQ R-ZEFQ-QTZP R-ZFNN-4LQE R-ZGVJ-IDH3 R-YGOV-PAQF R-ZJBC-9WYH R-ZKJ8-NOP6 R-ZLR5-1GFV R-YHWS-32H4 R-YBTA-67RN R-041M-S0KA R-06HF-JK1O
func TestParseUsage(t *testing.T) {
	cases := []struct {
		args             []string
		message, command string
	}{
		{nil, "no command given", ""},
		{[]string{"up", "now"}, "up takes no arguments", "up"},
		{[]string{"url", "dummy"}, "url takes no arguments", "url"},
		{[]string{"ls", "wip"}, "ls takes no arguments", "ls"},
		{[]string{"status", "dummy"}, "status takes no arguments", "status"},
		{[]string{"version", "x"}, "version takes no arguments", "version"},
		{[]string{"down", "wip", "other"}, "down takes at most one name", "down"},
		{[]string{"wipe", "wip", "other"}, "wipe takes at most one name", "wipe"},
		{[]string{"token", "bogus"}, "unknown token subcommand 'bogus'", "token"},
		{[]string{"token", "set", "ikp_3f9a2c7e1b4d8f60a5c2e9b7d4f1a8c3"}, "token set takes no arguments", "token"},
	}
	for _, a := range []string{"bogus", "help", "start", "list", "bo\ngus"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{a}, "unknown command '" + expectedPrintedName(a) + "'", ""})
	}
	for _, a := range []string{"--bogus", "-v", "--lines", "-hV", "--", "-"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{a, "--help"}, "unknown option '" + a + "'", ""})
	}
	for _, c := range []string{"up", "down", "wipe", "ls", "url", "status", "logs", "token", "version"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{c, "--bogus"}, "unknown option '--bogus'", c})
	}
	for _, c := range []string{"up", "url", "ls", "status", "version"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{c, "now", "--help"}, c + " takes no arguments", c})
	}
	for _, c := range []string{"down", "wipe"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{c, "a", "b", "--help"}, c + " takes at most one name", c})
	}
	cases = append(cases,
		struct {
			args             []string
			message, command string
		}{[]string{"logs", "auth", "dummy"}, "logs takes at most one app", "logs"},
		struct {
			args             []string
			message, command string
		}{[]string{"token", "bogus", "--help"}, "unknown token subcommand 'bogus'", "token"},
		struct {
			args             []string
			message, command string
		}{[]string{"token", "a\x7f"}, "unknown token subcommand 'a\\x7f'", "token"},
		struct {
			args             []string
			message, command string
		}{[]string{"token", "set", "arbitrary-token"}, "token set takes no arguments", "token"},
		struct {
			args             []string
			message, command string
		}{[]string{"token", "set", "--bogus"}, "unknown option '--bogus'", "token"})
	for _, a := range []string{"-hV", "-V", "--version", "--x\ty"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{"up", a}, "unknown option '" + expectedPrintedName(a) + "'", "up"})
	}
	for _, a := range []string{"-n5", "--lines=5", "-fn", "--"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{"logs", a}, "unknown option '" + a + "'", "logs"})
	}
	for _, option := range []string{"-n", "--lines"} {
		cases = append(cases, struct {
			args             []string
			message, command string
		}{[]string{"logs", option}, option + " needs a value", "logs"})
		for _, v := range []string{"x", "0", "00", "-5", "+5", "1.5", " 5", "", "2147483648", "12345678901234567890", "--help", "1\n2"} {
			cases = append(cases, struct {
				args             []string
				message, command string
			}{[]string{"logs", option, v}, option + " needs a positive whole number, not '" + expectedPrintedName(v) + "'", "logs"})
		}
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.args), func(t *testing.T) {
			trailer := "sandbox --help"
			if c.command != "" {
				trailer = "sandbox " + c.command + " --help"
			}
			want := "sandbox: " + c.message + "\n\nsee '" + trailer + "' for usage\n"
			for _, uid := range []int{0, 1000} {
				code, out, err := parseOutput(t, c.args, uid)
				if code != 2 || out != "" || err != want {
					t.Fatalf("got %d %q %q want %q", code, out, err, want)
				}
			}
		})
	}
}

// R-ZPEU-6RNY R-ZQMQ-KJEN R-ZRUM-YB5C R-ZT2J-C2W1 R-ZUAF-PUMQ R-ZVIC-3MDF R-ZWQ8-HE44 R-ZZ61-8XLI R-00DX-MPC7 R-01LU-0H2W R-02TQ-E8TL R-041M-S0KA R-059J-5SAZ R-Z5WG-2FSU R-Z3GN-AWBG R-Z4OJ-OO25
func TestParseHelpVersion(t *testing.T) {
	for _, command := range []string{"", "up", "down", "wipe", "ls", "url", "status", "logs", "token", "version"} {
		want := expectedHelpText(command)
		for _, h := range []string{"-h", "--help"} {
			args := []string{h}
			if command != "" {
				args = []string{command, h}
			}
			forms := [][]string{args}
			if command == "token" {
				forms = append(forms, []string{"token", "set", h})
			}
			if command == "up" {
				forms = append(forms, []string{"up", h, "now"})
			}
			if command == "down" {
				forms = append(forms, []string{"down", "wip", h})
			}
			if command == "" {
				forms = append(forms, []string{h, "bogus"})
			}
			for _, args := range forms {
				for _, uid := range []int{0, 1000} {
					code, out, err := parseOutput(t, args, uid)
					if code != 0 || out != want || err != "" {
						t.Fatalf("%v: %d %q %q", args, code, out, err)
					}
				}
			}
		}
	}
	for _, args := range [][]string{{"version"}, {"-V"}, {"--version"}, {"-V", "--bogus"}} {
		for _, uid := range []int{0, 1000} {
			code, out, err := parseOutput(t, args, uid)
			if code != 0 || out != Version+"\n" || err != "" {
				t.Fatalf("%v: %d %q %q", args, code, out, err)
			}
		}
	}
}

// R-0Z2H-78UM R-YHWS-32H4
func TestRootRefusal(t *testing.T) {
	readable := t.TempDir()
	locked := t.TempDir()
	original, statErr := os.Stat(locked)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(locked, original.Mode().Perm()); err != nil {
			t.Error(err)
		}
	})
	for _, args := range [][]string{{"up"}, {"url"}, {"down"}, {"down", "wip"}, {"wipe"}, {"wipe", "wip"}, {"ls"}, {"status"}, {"logs"}, {"logs", "-n", "5", "-f", "dummy"}, {"token"}, {"token", "set"}, {"logs", "-n", "2147483647"}, {"logs", "-n", "02147483647"}} {
		for _, dir := range []string{readable, filepath.Join(readable, "missing"), locked} {
			deps := parseOnlyDeps(t, 0)
			deps.Dir = dir
			var out, err bytes.Buffer
			code := runChecked(context.Background(), t, args, forbiddenReader{t}, &out, &err, deps)
			if code != 3 || out.Len() != 0 || err.String() != "sandbox: refusing to run as root\n" {
				t.Fatalf("%v: %d %q %q", args, code, out.String(), err.String())
			}
		}
	}
}

func expectedPrintedName(s string) string {
	switch s {
	case "bo\ngus":
		return "bo\\x0agus"
	case "--x\ty":
		return "--x\\x09y"
	case "1\n2":
		return "1\\x0a2"
	case "a\x7f":
		return "a\\x7f"
	case "a\nb":
		return "a\\x0ab"
	case "chdir /tmp/a\nb: no such file or directory":
		return "chdir /tmp/a\\x0ab: no such file or directory"
	}
	return s
}

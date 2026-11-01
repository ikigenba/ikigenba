package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/turns"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

// R-QXXN-LZUW R-MCLB-IBK7 R-R0DG-DJCA R-R1LC-RB2Z R-R2T9-52TO R-R415-IUKD.
func TestArgumentDiagnosticRendering(t *testing.T) {
	allControls := "a"
	for b := byte(0); b < 0x20; b++ {
		allControls += string([]byte{b})
	}
	allControls += "\x7fb"
	unchanged := "\"\\'"
	for b := 0x80; b <= 0xff; b++ {
		unchanged += string([]byte{byte(b)})
	}
	for _, tc := range []struct {
		args             []string
		command, summary string
	}{
		{[]string{"a\nb"}, "", `unknown command 'a\nb'`},
		{[]string{allControls}, "", `unknown command 'a\x00\x01\x02\x03\x04\x05\x06\a\b\t\n\v\f\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x7fb'`},
		{[]string{"--x\t\x1b\x7f\x00"}, "", `unknown option '--x\t\x1b\x7f\x00'`},
		{[]string{"push", "--x\t\x1b\x7f\x00"}, "push", `unknown option '--x\t\x1b\x7f\x00'`},
		{[]string{"configure", "us.ikigenba.dev", unchanged}, "configure", "unexpected argument '" + unchanged + "'"},
	} {
		t.Run(fmt.Sprint(tc.args), func(t *testing.T) {
			inertCLI(t, tc.args, 1000, ExitUsage, "", usageDiagnostic(tc.command, tc.summary))
		})
	}
}

// R-QXXN-LZUW: another program's bytes remain exactly QuoteOutput's result.
func TestExternalDiagnosticPreservesControlBytes(t *testing.T) {
	deps, _ := syntheticDeps(t)
	output := "external\t\x1b\x00\xff\n\nlast\r\n"
	deps.Exec = func(context.Context, seam.Cmd) (seam.Result, error) {
		return seam.Result{ExitCode: 9, Stderr: []byte(output)}, nil
	}
	code, out, errText := runCLI(t, deps, []string{"install"}, nil)
	assertCLI(t, code, out, errText, ExitFailed, "", "turns: could not enable turns.timer\n\n"+seam.QuoteOutput(output)+"\n")
}

// R-VO3C-4I13 R-VQJ4-W1IH R-BWJB-ICLE R-BXR7-W4C3.
func TestFileFailureDiagnosticRendering(t *testing.T) {
	for _, command := range []string{"configure", "install"} {
		for _, failure := range []string{"parent", paths.TokenFile, paths.ConfigFile, turns.ServiceUnit, turns.TimerUnit} {
			if command == "configure" && (failure == turns.ServiceUnit || failure == turns.TimerUnit) || command == "install" && (failure == paths.TokenFile || failure == paths.ConfigFile) {
				continue
			}
			t.Run(command+"/"+failure, func(t *testing.T) {
				deps, root := syntheticDeps(t)
				xdg := filepath.Join(root, "x\ny")
				overrideEnv(&deps, "XDG_CONFIG_HOME", xdg)
				p := configPaths(t, deps)
				path := filepath.Join(p.Config, failure)
				if command == "install" {
					path = filepath.Join(p.UserUnits, failure)
				}
				action := "write"
				if failure == "parent" {
					writeFixture(t, xdg, "blocking parent", 0600)
					action = "create"
					path = p.Config
					if command == "install" {
						path = p.UserUnits
					}
				} else {
					writeFixture(t, filepath.Join(path, "child"), "blocking directory", 0600)
				}
				args := []string{command}
				if command == "configure" {
					args = append(args, "a.b", "--token=synthetic-token")
				}
				code, out, errText := runCLI(t, deps, args, nil)
				lines := strings.Split(strings.TrimSuffix(errText, "\n"), "\n")
				wantFirst := "turns: could not " + action + " '" + strings.ReplaceAll(path, "\n", `\n`) + "'"
				if code != ExitFailed || out != "" || len(lines) != 3 || lines[0] != wantFirst || lines[1] != "" || lines[2] == "" || strings.HasPrefix(lines[2], "> ") {
					t.Fatalf("got (%d, %q, %q), want three lines beginning %q", code, out, errText, wantFirst)
				}
			})
		}
	}
}

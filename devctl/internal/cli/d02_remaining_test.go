package cli

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const expectedDeployUsage = `Usage: devctl deploy <space> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
space's deploy/ prefix in the bucket and have opsctl on the space install it
from there. The app and tag (v<semver>) are read from the file name.
`

const expectedRestoreUsage = `Usage: devctl restore <space> <app> [--at <timestamp>]

Have opsctl on the space put <app> back from the space's own backups. The
app's etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s unit is stopped for the restore and
started again after it.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

const expectedApexUsage = `Usage: devctl apex <subcommand> [arguments]

Point the root domain at one app on one space, say where it points, or take
it away. The root is an A record at the space's address; the space's host
carries the root in its certificate and routes it to the app.

Subcommands:
  set <app>.<space>   make <app> on <space> answer at the root
  show                print the app and address the root points at
  clear               remove the root's record and the holder's apex configuration

Run 'devctl apex <subcommand> --help' for details.
`

func TestEveryCommandHelpIsExact(t *testing.T) {
	// R-OLKP-QGCW
	tests := []struct {
		command string
		want    string
	}{
		{command: "space", want: expectedD06Usage},
		{command: "secrets", want: expectedD05Usage},
		{command: "build", want: expectedBuildUsage},
		{command: "deploy", want: expectedDeployUsage},
		{command: "restore", want: expectedRestoreUsage},
		{command: "remove", want: wantRemoveUsage},
		{command: "apex", want: expectedApexUsage},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			for _, option := range []string{"--help", "-h"} {
				assertResult(t, invoke(test.command, option), 0, test.want, "")
			}
		})
	}
}

func TestCommandHelpPrecedesValidationAndExternalAccess(t *testing.T) {
	// R-C7IS-6V7A
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "space", args: []string{"space", "create", "--acme-email", "--bad", "--help"}, want: expectedCreateUsage},
		{name: "secrets", args: []string{"secrets", "push", "--bad", "--help"}, want: expectedD05PushUsage},
		{name: "build", args: []string{"build", "--bad", "--help"}, want: expectedBuildUsage},
		{name: "deploy", args: []string{"deploy", "--bad", "--help"}, want: expectedDeployUsage},
		{name: "restore", args: []string{"restore", "--at", "--bad", "--help"}, want: expectedRestoreUsage},
		{name: "remove", args: []string{"remove", "--bad", "--help"}, want: wantRemoveUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, help := range []string{"--help", "-h"} {
				args := append([]string(nil), test.args...)
				args[len(args)-1] = help
				external := 0
				result := invokeWithDeps(noExternalDeps(&external), args...)
				assertResult(t, result, 0, test.want, "")
				if external != 0 {
					t.Fatalf("external calls = %d, want zero", external)
				}
			}
		})
	}
}

func TestCommandOptionValuesFailBeforeExternalAccess(t *testing.T) {
	// R-C8QO-KMXZ
	tests := []struct {
		name   string
		args   []string
		option string
		help   string
	}{
		{name: "create missing", args: []string{"space", "create", "sbx1", "--acme-email"}, option: "--acme-email", help: "devctl space --help"},
		{name: "create empty", args: []string{"space", "create", "sbx1", "--acme-email="}, option: "--acme-email", help: "devctl space --help"},
		{name: "init opsctl missing", args: []string{"space", "init", "sbx1", "--opsctl"}, option: "--opsctl", help: "devctl space --help"},
		{name: "init opsctl empty", args: []string{"space", "init", "sbx1", "--opsctl="}, option: "--opsctl", help: "devctl space --help"},
		{name: "init email option-like", args: []string{"space", "init", "sbx1", "--acme-email", "--bad"}, option: "--acme-email", help: "devctl space --help"},
		{name: "restore missing", args: []string{"restore", "sbx1", "app", "--at"}, option: "--at", help: "devctl restore --help"},
		{name: "restore empty", args: []string{"restore", "sbx1", "app", "--at="}, option: "--at", help: "devctl restore --help"},
		{name: "restore option-like", args: []string{"restore", "sbx1", "app", "--at", "--bad"}, option: "--at", help: "devctl restore --help"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			external := 0
			result := invokeWithDeps(noExternalDeps(&external), test.args...)
			want := "devctl: option '" + test.option + "' requires a value\n\nsee '" + test.help + "' for usage\n"
			assertResult(t, result, 2, "", want)
			if external != 0 {
				t.Fatalf("external calls = %d, want zero", external)
			}
		})
	}

	cloudCalls := 0
	deps := checkoutDeps(t, `{"domain":"ikigenba.dev","region":"us-east-2"}`)
	deps.Cloud = func(context.Context, string, string) (cloud.Clients, error) {
		cloudCalls++
		return cloud.Clients{}, errors.New("reached cloud")
	}
	result := invokeWithDeps(deps,
		"space", "logs", "sbx1", "app", "--since", "-1h")
	assertResult(t, result, 1, "", "devctl: reached cloud\n")
	if cloudCalls != 1 {
		t.Fatalf("logs --since cloud calls = %d, want one after accepting -1h", cloudCalls)
	}
}

func noExternalDeps(calls *int) seam.Deps {
	unexpected := errors.New("unexpected external access")
	return seam.Deps{
		EUID: 1,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			(*calls)++
			return cloud.Clients{}, unexpected
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, unexpected
		},
		Stream: func(context.Context, seam.Cmd, io.Writer) (seam.Result, error) {
			(*calls)++
			return seam.Result{}, unexpected
		},
	}
}

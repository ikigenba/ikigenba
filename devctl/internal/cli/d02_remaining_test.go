package cli

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const expectedDeployUsage = `Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
`

const expectedRestoreUsage = `Usage: devctl --account <name> restore <domain> <app> [--at <timestamp>]

Have opsctl on <domain> put <app> back from <domain>'s own backups. The app's
etc/ and state/ come from the newest tarball, and its database, when it
declares one, from litestream. <app>'s unit is stopped for the restore and
started again after it.

Options:
  --at <timestamp>   restore the app as it was at this RFC 3339 moment

--at governs both halves: the files come from the newest tarball written at or
before that moment, and the database is rebuilt to the moment itself.
`

func TestEveryCommandHelpIsExact(t *testing.T) {
	// R-C07D-W8R4
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
		{name: "space", args: []string{"space", "create", "--acme-email", "--bad", "--help"}, want: `Usage: devctl --account <name> space create <domain> --acme-email <address>

Create the space with secrets, a role, an instance, an Elastic IP and DNS records.
Install the newest published opsctl, set its host configuration and run init.
When the account keeps backups, restore its own host backup before init.
Completed steps remain on failure; use space destroy to clean up.

Options:
  --acme-email <address>   ACME contact address stored on the host; required
`},
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
		{name: "create missing", args: []string{"--account", "work", "space", "create", "domain", "--acme-email"}, option: "--acme-email", help: "devctl space --help"},
		{name: "create empty", args: []string{"--account", "work", "space", "create", "domain", "--acme-email="}, option: "--acme-email", help: "devctl space --help"},
		{name: "create option-like", args: []string{"--account", "work", "space", "create", "domain", "--acme-email", "--bad"}, option: "--acme-email", help: "devctl space --help"},
		{name: "init opsctl missing", args: []string{"--account", "work", "space", "init", "domain", "--opsctl"}, option: "--opsctl", help: "devctl space --help"},
		{name: "init opsctl empty", args: []string{"--account", "work", "space", "init", "domain", "--opsctl="}, option: "--opsctl", help: "devctl space --help"},
		{name: "init email option-like", args: []string{"--account", "work", "space", "init", "domain", "--acme-email", "--bad"}, option: "--acme-email", help: "devctl space --help"},
		{name: "restore missing", args: []string{"--account", "work", "restore", "domain", "app", "--at"}, option: "--at", help: "devctl restore --help"},
		{name: "restore empty", args: []string{"--account", "work", "restore", "domain", "app", "--at="}, option: "--at", help: "devctl restore --help"},
		{name: "restore option-like", args: []string{"--account", "work", "restore", "domain", "app", "--at", "--bad"}, option: "--at", help: "devctl restore --help"},
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
	result := invokeWithDeps(seam.Deps{
		EUID: 1,
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, errors.New("reached cloud")
		},
	}, "--account", "work", "space", "logs", "domain", "app", "--since", "-1h")
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

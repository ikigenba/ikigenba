package space

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRunListHelp(t *testing.T) {
	// R-TG1T-SODG
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"list", option}, &stdout, seam.Deps{}, "sandbox")
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := stdout.String(); got != listUsage {
				t.Fatalf("stdout = %q, want %q", got, listUsage)
			}
		})
	}
}

func TestRunNeedsSubcommand(t *testing.T) {
	// R-TVEN-62JB
	assertRunUsageError(t, nil, "space needs <subcommand>")
}

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	// R-TWMJ-JUA0
	assertRunUsageError(t, []string{"frobnicate"}, "unknown subcommand 'frobnicate'")
}

func TestRunDomainSubcommandsNeedDomain(t *testing.T) {
	// R-TXUF-XM0P
	for _, subcommand := range []string{"destroy", "stop", "start", "status"} {
		t.Run(subcommand, func(t *testing.T) {
			assertRunUsageError(t, []string{subcommand}, "space "+subcommand+" needs <domain>")
		})
	}
}

func TestRunRejectsExtraOperands(t *testing.T) {
	// R-U0A8-P5I3
	for _, subcommand := range []string{"destroy", "stop", "start", "status"} {
		t.Run(subcommand, func(t *testing.T) {
			assertRunUsageError(t, []string{subcommand, "one.example", "two.example"},
				"space "+subcommand+" takes only <domain>")
		})
	}
	t.Run("list", func(t *testing.T) {
		assertRunUsageError(t, []string{"list", "operand"}, "space list takes no arguments")
	})
}

func TestSpaceSubcommandGrammar(t *testing.T) {
	// R-DJ2V-3X4D
	want := []string{"list", "create", "destroy", "stop", "start", "init", "status", "restart", "logs"}
	if len(subcommands) != len(want) {
		t.Fatalf("subcommand count = %d, want %d", len(subcommands), len(want))
	}
	for _, name := range want {
		if _, ok := subcommands[name]; !ok {
			t.Errorf("subcommand %q is missing", name)
		}
	}

	cases := []struct {
		args       []string
		domain     string
		noBackup   bool
		wantErrMsg string
	}{
		{args: []string{"list"}},
		{args: []string{"destroy", "foo.example"}, domain: "foo.example"},
		{args: []string{"destroy", "--no-backup", "foo.example"}, domain: "foo.example", noBackup: true},
		{args: []string{"destroy", "foo.example", "--no-backup"}, domain: "foo.example", noBackup: true},
		{args: []string{"stop", "foo.example"}, domain: "foo.example"},
		{args: []string{"start", "foo.example"}, domain: "foo.example"},
		{args: []string{"status", "foo.example"}, domain: "foo.example"},
	}
	for _, test := range cases {
		got, err := parseInvocation(test.args)
		if err != nil {
			t.Errorf("parseInvocation(%q) error = %v", test.args, err)
			continue
		}
		if got.domain != test.domain || got.noBackup != test.noBackup {
			t.Errorf("parseInvocation(%q) = domain %q, noBackup %t", test.args, got.domain, got.noBackup)
		}
	}
}

func TestRunRejectsUnknownOptions(t *testing.T) {
	// R-DLIN-VGLR
	cases := [][]string{
		{"--wat"},
		{"list", "--wat"},
		{"destroy", "foo.example", "--wat"},
		{"destroy", "--no-backup=true", "foo.example"},
		{"stop", "-x", "foo.example"},
		{"start", "foo.example", "--wat"},
		{"status", "--wat"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			option := "--wat"
			for _, argument := range args {
				if len(argument) > 0 && argument[0] == '-' {
					option = argument
					break
				}
			}
			assertRunUsageError(t, args, "unknown option '"+option+"'")
		})
	}
}

func assertRunUsageError(t *testing.T, args []string, message string) {
	t.Helper()
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, seam.Deps{}, "sandbox")
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	var usageError *UsageError
	if !errors.As(err, &usageError) {
		t.Fatalf("Run() error = %T %v, want *UsageError", err, err)
	}
	if usageError.Message != message {
		t.Errorf("Message = %q, want %q", usageError.Message, message)
	}
	if usageError.Help != spaceHelp {
		t.Errorf("Help = %q, want %q", usageError.Help, spaceHelp)
	}
	if usageError.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", usageError.ExitCode())
	}
	if got, want := usageError.Detail(), "see 'devctl space --help' for usage"; got != want {
		t.Errorf("Detail() = %q, want %q", got, want)
	}
}

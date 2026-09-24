package space

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestRunListHelp(t *testing.T) {
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"list", option}, &stdout, seam.Deps{})
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
	assertRunUsageError(t, nil, "space needs <subcommand>")
}

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	// R-TWMJ-JUA0
	assertRunUsageError(t, []string{"frobnicate"}, "unknown subcommand 'frobnicate'")
}

func TestRunDomainSubcommandsNeedDomain(t *testing.T) {
	for _, subcommand := range []string{"destroy", "stop", "start", "status"} {
		t.Run(subcommand, func(t *testing.T) {
			assertRunUsageError(t, []string{subcommand}, "space "+subcommand+" needs <space>")
		})
	}
}

func TestRunRejectsExtraOperands(t *testing.T) {
	for _, subcommand := range []string{"destroy", "stop", "start", "status"} {
		t.Run(subcommand, func(t *testing.T) {
			assertRunUsageError(t, []string{subcommand, "one.example", "two.example"},
				"space "+subcommand+" takes only <space>")
		})
	}
	t.Run("list", func(t *testing.T) {
		assertRunUsageError(t, []string{"list", "operand"}, "space list takes no arguments")
	})
	t.Run("list operand before help", func(t *testing.T) {
		assertRunUsageError(t, []string{"list", "operand", "--help"}, "space list takes no arguments")
	})
}

func TestSpaceSubcommandGrammar(t *testing.T) {
	// R-JF4H-OOXX
	want := []string{"list", "create", "destroy", "stop", "start", "init", "status", "restart", "disable", "enable", "logs"}
	if len(subcommands) != len(want) {
		t.Fatalf("subcommand count = %d, want %d", len(subcommands), len(want))
	}
	for _, name := range want {
		if _, ok := subcommands[name]; !ok {
			t.Errorf("subcommand %q is missing", name)
		}
	}

	cases := []struct {
		args          []string
		domain        string
		noBackup      bool
		deleteSecrets bool
		deleteBackups bool
	}{
		{args: []string{"list"}},
		{args: []string{"destroy", "foo.example"}, domain: "foo.example"},
		{args: []string{"destroy", "--no-backup", "foo.example"}, domain: "foo.example", noBackup: true},
		{args: []string{"destroy", "foo.example", "--no-backup"}, domain: "foo.example", noBackup: true},
		{args: []string{"destroy", "--delete-secrets", "foo.example"}, domain: "foo.example", deleteSecrets: true},
		{args: []string{"destroy", "foo.example", "--delete-backups"}, domain: "foo.example", deleteBackups: true},
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
		if got.domain != test.domain || got.noBackup != test.noBackup ||
			got.deleteSecrets != test.deleteSecrets || got.deleteBackups != test.deleteBackups {
			t.Errorf("parseInvocation(%q) = %#v", test.args, got)
		}
	}
}

func TestRunRejectsUnknownOptions(t *testing.T) {
	cases := [][]string{
		{"--wat"},
		{"--help", "--wat"},
		{"-h", "--wat"},
		{"list", "--wat"},
		{"list", "--wat", "--help"},
		{"destroy", "foo.example", "--wat"},
		{"destroy", "--no-backup=true", "foo.example"},
		{"destroy", "--delete-secrets=yes", "foo.example"},
		{"destroy", "--delete-backups=1", "foo.example"},
		{"stop", "foo.example", "--no-backup"},
		{"stop", "-x", "foo.example"},
		{"stop", "foo.example", "--wat", "--help"},
		{"start", "foo.example", "--wat"},
		{"status", "--wat"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			option := "--wat"
			for _, argument := range args {
				if len(argument) > 0 && argument[0] == '-' && argument != "--help" && argument != "-h" {
					option = argument
					break
				}
			}
			assertRunUsageError(t, args, "unknown option '"+option+"'")
		})
	}
}

func TestDestroyOptionsGrammar(t *testing.T) {
	// R-V3ZZ-EM0S
	for _, args := range [][]string{
		{"destroy", "--no-backup", "--delete-secrets", "--delete-backups", "foo.example"},
		{"destroy", "foo.example", "--delete-backups", "--no-backup", "--delete-secrets"},
		{"destroy", "--delete-secrets", "--no-backup", "foo.example", "--delete-backups", "--no-backup", "--delete-secrets", "--delete-backups"},
	} {
		got, err := parseInvocation(args)
		if err != nil || got.domain != "foo.example" || !got.noBackup || !got.deleteSecrets || !got.deleteBackups {
			t.Fatalf("parseInvocation(%q) = %#v, %v", args, got, err)
		}
	}
	for _, test := range []struct {
		option                                 string
		noBackup, deleteSecrets, deleteBackups bool
	}{
		{option: "--no-backup", noBackup: true},
		{option: "--delete-secrets", deleteSecrets: true},
		{option: "--delete-backups", deleteBackups: true},
	} {
		for _, args := range [][]string{
			{"destroy", test.option, "foo.example"},
			{"destroy", "foo.example", test.option},
			{"destroy", test.option, "foo.example", test.option},
		} {
			got, err := parseInvocation(args)
			if err != nil || got.domain != "foo.example" || got.noBackup != test.noBackup ||
				got.deleteSecrets != test.deleteSecrets || got.deleteBackups != test.deleteBackups {
				t.Fatalf("parseInvocation(%q) = %#v, %v", args, got, err)
			}
		}
	}
	for _, option := range []string{"--no-backup=true", "--delete-secrets=yes", "--delete-backups=1"} {
		assertRunUsageError(t, []string{"destroy", option, "foo.example"}, "unknown option '"+option+"'")
	}
}

func assertRunUsageError(t *testing.T, args []string, message string) {
	t.Helper()
	var stdout bytes.Buffer
	err := Run(context.Background(), args, &stdout, seam.Deps{})
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

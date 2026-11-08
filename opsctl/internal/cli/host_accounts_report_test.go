package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

func TestHostBackupReportsAccountsBeforeConfiguration(t *testing.T) {
	// R-UORG-349E
	for _, test := range []struct {
		name       string
		result     backup.HostBackupResult
		err        error
		out        string
		diagnostic string
		code       exitCode
	}{
		{
			name: "both archives",
			result: backup.HostBackupResult{
				Accounts: backup.FileResult{Service: "accounts", Object: "instant.accounts.tar.zst", Size: 1572864},
				Host:     backup.FileResult{Service: "host", Object: "instant.tar.zst", Size: 1792},
			},
			out:  "accounts: ok (instant.accounts.tar.zst, 1.5 MiB)\nhost: ok (instant.tar.zst, 1.8 KiB)\n",
			code: exitOK,
		},
		{
			name: "accounts unreadable",
			result: backup.HostBackupResult{
				Accounts: backup.FileResult{Service: "accounts", Err: errors.New("/home/alice/notes: input/output error")},
				Host:     backup.FileResult{Service: "host", Err: errors.New("accounts not written")},
			},
			out:        "accounts: failed: /home/alice/notes: input/output error\nhost: failed: accounts not written\n",
			diagnostic: "opsctl: host backup failed\n", code: exitFail,
		},
		{
			name: "configuration fails after accounts",
			result: backup.HostBackupResult{
				Accounts: backup.FileResult{Service: "accounts", Object: "instant.accounts.tar.zst", Size: 1048576},
				Host:     backup.FileResult{Service: "host", Err: errors.New("configuration upload refused")},
			},
			out:        "accounts: ok (instant.accounts.tar.zst, 1.0 MiB)\nhost: failed: configuration upload refused\n",
			diagnostic: "opsctl: host backup failed\n", code: exitFail,
		},
		{
			name:       "interrupted accounts capture appears once",
			result:     backup.HostBackupResult{Accounts: backup.FileResult{Service: "accounts", Err: &host.CommandError{Label: "getfacl", Result: host.Result{Stderr: []byte("metadata stopped\n")}, Err: context.Canceled}}},
			err:        context.Canceled,
			out:        "accounts: failed: getfacl: context canceled\n",
			diagnostic: "opsctl: host backup failed\n\n> metadata stopped\n", code: exitFail,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := renderHostBackupOutcome(&out, &diagnostic, test.result, test.err)
			if code != test.code || out.String() != test.out || diagnostic.String() != test.diagnostic {
				t.Fatalf("exit %d stdout %q stderr %q", code, out.String(), diagnostic.String())
			}
		})
	}
}

func TestHostRestoreRendersAccountsUsersAndSSHPhases(t *testing.T) {
	// R-UPZC-GW03
	result := backup.HostRestoreResult{
		Object: "host/instant.tar.zst", Size: 1536, SourceReady: true,
		AccountsObject: "host/instant.accounts.tar.zst", AccountsSize: 1572864, AccountsReady: true,
		Files: 7, FilesRestored: true,
		Groups: []string{"acme", "beta"}, GroupsRestored: true,
		Users: []string{"alice", "bob"}, Locked: []string{"bob"}, UsersRestored: true,
		HostKeys: 2, SSHRestored: true,
	}
	prefix := "source: ok (host/instant.tar.zst, 1.5 KiB)\naccounts: ok (host/instant.accounts.tar.zst, 1.5 MiB)\nfiles: ok (/etc/ikigenba, /etc/letsencrypt, 7 files)\ngroups: ok (acme, beta)\n"
	for _, timers := range []bool{false, true} {
		result.TimersInstalled = timers
		want := prefix + "users: ok (alice, bob locked"
		if !timers {
			want += "; turns.timer not installed, no timers started"
		}
		want += ")\nssh: ok (2 host keys, sshd reloaded)\n"
		var out, diagnostic bytes.Buffer
		if code := renderHostRestoreOutcome(&out, &diagnostic, result, nil); code != exitOK || out.String() != want || diagnostic.Len() != 0 {
			t.Fatalf("timers %t: exit %d stdout %q stderr %q", timers, code, out.String(), diagnostic.String())
		}
	}
	result.Groups = nil
	result.Users = nil
	result.Locked = nil
	result.TimersInstalled = false
	var out, diagnostic bytes.Buffer
	want := "source: ok (host/instant.tar.zst, 1.5 KiB)\naccounts: ok (host/instant.accounts.tar.zst, 1.5 MiB)\nfiles: ok (/etc/ikigenba, /etc/letsencrypt, 7 files)\ngroups: ok (none)\nusers: ok (none)\nssh: ok (2 host keys, sshd reloaded)\n"
	if code := renderHostRestoreOutcome(&out, &diagnostic, result, nil); code != exitOK || out.String() != want || diagnostic.Len() != 0 {
		t.Fatalf("empty accounts: exit %d stdout %q stderr %q", code, out.String(), diagnostic.String())
	}
}

func TestHostRestoreReportsOnlyCompletedAccountsPhasesOnFailure(t *testing.T) {
	// R-UPZC-GW03
	for _, test := range []struct {
		name       string
		result     backup.HostRestoreResult
		err        error
		out        string
		diagnostic string
	}{
		{
			name:       "manifest collision",
			result:     backup.HostRestoreResult{Object: "host/instant.tar.zst", Size: 1024, SourceReady: true, FailedStep: "accounts"},
			err:        errors.New("uid 1002 of bob is carol's on this host"),
			out:        "source: ok (host/instant.tar.zst, 1.0 KiB)\naccounts: failed: uid 1002 of bob is carol's on this host\n",
			diagnostic: "opsctl: host restore failed at accounts\n",
		},
		{
			name:       "sshd refuses keys",
			result:     backup.HostRestoreResult{SourceReady: true, Object: "host/instant.tar.zst", Size: 1024, AccountsReady: true, AccountsObject: "host/instant.accounts.tar.zst", FilesRestored: true, GroupsRestored: true, UsersRestored: true, FailedStep: "ssh"},
			err:        &host.CommandError{Label: "sshd -t", Result: host.Result{ExitCode: 255, Stderr: []byte("bad key\n")}},
			out:        "source: ok (host/instant.tar.zst, 1.0 KiB)\naccounts: ok (host/instant.accounts.tar.zst, 0.0 MiB)\nfiles: ok (/etc/ikigenba, /etc/letsencrypt, 0 files)\ngroups: ok (none)\nusers: ok (none)\nssh: failed: sshd -t: exit status 255\n",
			diagnostic: "opsctl: host restore failed at ssh\n\n> bad key\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			if code := renderHostRestoreOutcome(&out, &diagnostic, test.result, test.err); code != exitFail || out.String() != test.out || diagnostic.String() != test.diagnostic {
				t.Fatalf("exit %d stdout %q stderr %q", code, out.String(), diagnostic.String())
			}
		})
	}
}

func TestRetireReportsAccountsFailuresOnceWithCommandDetail(t *testing.T) {
	// R-V0YF-WTOC
	for _, step := range []string{"accounts", "host"} {
		result := backup.RetireResult{ServicesStopped: true, LitestreamStopped: true, FailedStep: step}
		failed := backup.FileResult{Service: step, Err: &host.CommandError{Label: "compress archive", Result: host.Result{Stderr: []byte("encoder stopped\n")}, Err: context.Canceled}}
		if step == "accounts" {
			result.Accounts = failed
		} else {
			result.Accounts = backup.FileResult{Service: "accounts", Object: "instant.accounts.tar.zst", Size: 1048576}
			result.Host = failed
		}
		want := "services: ok (none)\nlitestream: ok (stopped)\n"
		if step == "host" {
			want += "accounts: ok (instant.accounts.tar.zst, 1.0 MiB)\n"
		}
		want += step + ": failed: compress archive: context canceled\n"
		var out, diagnostic bytes.Buffer
		code := renderRetireOutcome(&out, &diagnostic, result, context.Canceled)
		if code != exitFail || out.String() != want || diagnostic.String() != "opsctl: retire failed at "+step+"\n\n> encoder stopped\n" {
			t.Fatalf("%s: exit %d stdout %q stderr %q", step, code, out.String(), diagnostic.String())
		}
	}
}

func TestRetireAccountsFindingUsesOnlyStdoutAndNoLaterSuccess(t *testing.T) {
	// R-UYIN-5A6Y R-V0YF-WTOC
	result := backup.RetireResult{
		ServicesStopped: true, LitestreamStopped: true,
		Accounts: backup.FileResult{Service: "accounts", Err: errors.New("metadata unreadable")},
		Host:     backup.FileResult{Service: "host", Err: errors.New("accounts not written")},
	}
	var out, diagnostic bytes.Buffer
	if code := renderRetireOutcome(&out, &diagnostic, result, nil); code != exitFail || out.String() != "services: ok (none)\nlitestream: ok (stopped)\naccounts: failed: metadata unreadable\nhost: failed: accounts not written\n" || diagnostic.Len() != 0 {
		t.Fatalf("finding: exit %d stdout %q stderr %q", code, out.String(), diagnostic.String())
	}
	out.Reset()
	result.Accounts = backup.FileResult{}
	result.Host = backup.FileResult{}
	if code := renderRetireOutcome(&out, &diagnostic, result, context.Canceled); code != exitFail || out.String() != "services: ok (none)\nlitestream: ok (stopped)\n" || diagnostic.String() != "opsctl: retire failed\n" {
		t.Fatalf("between steps: exit %d stdout %q stderr %q", code, out.String(), diagnostic.String())
	}
}

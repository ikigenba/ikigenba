package hostsetup

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

// R-GDIF-8NEP
var _ func(context.Context, host.Host) (string, error) = InstallLatest

// R-GEQB-MF5E
var _ func(context.Context, host.Host, string) error = Upgrade

// R-GFY8-06W3
var _ func(context.Context, host.Host) (string, error) = Version

func TestInstallLatestUsesSelectedPublishedRelease(t *testing.T) {
	// R-G3R8-6HH5
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		if command.Path == "curl" {
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v2.3.4","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://downloads.example/install.sh"}]}]`)}, nil
		}
		return seam.Result{}, nil
	}}

	got, err := InstallLatest(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps})
	if err != nil || got != "v2.3.4" {
		t.Fatalf("InstallLatest() = %q, %v", got, err)
	}
	want := []seam.Cmd{
		{Path: "curl", Args: []string{"-fsSL", ReleasesURL + "?per_page=100&page=1"}, Dir: "/work"},
		{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.10", "'curl' '-fsSL' '-o' '/tmp/opsctl-install' 'https://downloads.example/install.sh'"}, Dir: "/work"},
		{Path: "ssh", Args: []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "ec2-user@192.0.2.10", "'sudo' 'bash' '/tmp/opsctl-install' 'v2.3.4'"}, Dir: "/work"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestInstallLatestStopsAfterHostFailure(t *testing.T) {
	// R-G3R8-6HH5
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		if command.Path == "curl" {
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v2.3.4","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://downloads.example/install.sh"}]}]`)}, nil
		}
		return seam.Result{ExitCode: 22}, nil
	}}

	_, err := InstallLatest(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps})
	var commandErr *host.CommandError
	if !errors.As(err, &commandErr) || commandErr.Step != "opsctl" {
		t.Fatalf("InstallLatest() error = %#v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("executed %d commands after fetch failure, want 2", len(commands))
	}
}

func TestInstallLatestReturnsInstallerFailureAndStops(t *testing.T) {
	// R-G3R8-6HH5
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		switch len(commands) {
		case 1:
			return seam.Result{Stdout: []byte(`[{"tag_name":"opsctl/v2.3.4","published_at":"2026-09-17T00:00:00Z","assets":[{"name":"install.sh","browser_download_url":"https://downloads.example/install.sh"}]}]`)}, nil
		case 2:
			return seam.Result{}, nil
		case 3:
			return seam.Result{ExitCode: 23, Stderr: []byte("installer failed\n")}, nil
		default:
			t.Fatalf("unexpected command after installer failure: %#v", command)
			return seam.Result{}, nil
		}
	}}

	got, err := InstallLatest(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps})
	var commandErr *host.CommandError
	if got != "" || !errors.As(err, &commandErr) || commandErr.Step != "opsctl" || commandErr.Status != 23 || commandErr.Stderr != "installer failed\n" {
		t.Fatalf("InstallLatest() = %q, %#v", got, err)
	}
	if len(commands) != 3 || commands[2].Args[len(commands[2].Args)-1] != "'sudo' 'bash' '/tmp/opsctl-install' 'v2.3.4'" {
		t.Fatalf("InstallLatest commands = %#v", commands)
	}
}

func TestUpgradeFetchFailureStopsBeforeInstaller(t *testing.T) {
	// R-ZERJ-X644
	wantErr := errors.New("ssh start failed")
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
		commands = append(commands, command)
		return seam.Result{}, wantErr
	}}

	err := Upgrade(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps}, "v2.3.4")
	unwrapped := errors.Unwrap(err)
	if unwrapped == nil || reflect.ValueOf(unwrapped).Pointer() != reflect.ValueOf(wantErr).Pointer() || errors.Unwrap(unwrapped) != nil || err.Error() != "ssh: ssh start failed" {
		t.Fatalf("Upgrade() error = %#v, want host error unchanged", err)
	}
	if len(commands) != 1 || commands[0].Path != "ssh" || commands[0].Args[len(commands[0].Args)-1] != "'curl' '-fsSL' '-o' '"+InstallerPath+"' '"+DownloadURL+"/opsctl/v2.3.4/install.sh'" {
		t.Fatalf("Upgrade commands = %#v", commands)
	}
}

func TestUpgradeRunsRequestedInstallerAndReturnsFailure(t *testing.T) {
	// R-ZERJ-X644
	var commands []seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, cmd seam.Cmd) (seam.Result, error) {
		commands = append(commands, cmd)
		if len(commands) == 2 {
			return seam.Result{ExitCode: 9}, nil
		}
		return seam.Result{}, nil
	}}

	err := Upgrade(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps}, "v2.3.4")
	var commandErr *host.CommandError
	if reflect.TypeOf(err) != reflect.TypeOf(commandErr) || !errors.As(err, &commandErr) || commandErr.Step != "opsctl" || commandErr.Status != 9 {
		t.Fatalf("Upgrade() error = %#v", err)
	}
	want := []string{
		"'curl' '-fsSL' '-o' '" + InstallerPath + "' '" + DownloadURL + "/opsctl/v2.3.4/install.sh'",
		"'sudo' 'bash' '" + InstallerPath + "' 'v2.3.4'",
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %#v", commands)
	}
	for i, command := range commands {
		if command.Path != "ssh" || command.Args[len(command.Args)-1] != want[i] {
			t.Fatalf("command %d = %#v, want %q", i, command, want[i])
		}
	}
}

func TestVersionReturnsOpaqueTrimmedOutput(t *testing.T) {
	// R-G670-Y0YJ
	const reported = " \t unexpected installed text  build 7 \t "
	var command seam.Cmd
	deps := seam.Deps{Dir: "/work", Exec: func(_ context.Context, got seam.Cmd) (seam.Result, error) {
		command = got
		return seam.Result{Stdout: []byte(reported + "\r\n\n")}, nil
	}}

	got, err := Version(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps})
	if err != nil || got != reported {
		t.Fatalf("Version() = %q, %v", got, err)
	}
	if command.Args[len(command.Args)-1] != "'sudo' 'opsctl' 'version'" {
		t.Fatalf("Version command = %#v", command)
	}
}

func TestVersionReturnsHostErrorUnchanged(t *testing.T) {
	// R-G670-Y0YJ
	deps := seam.Deps{Dir: "/work", Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		return seam.Result{ExitCode: 17, Stdout: []byte("untrusted\n")}, nil
	}}

	got, err := Version(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps})
	var commandErr *host.CommandError
	if got != "" || reflect.TypeOf(err) != reflect.TypeOf(commandErr) || !errors.As(err, &commandErr) || commandErr.Step != "opsctl" || commandErr.Status != 17 {
		t.Fatalf("Version() = %q, %#v", got, err)
	}
}

func TestVersionReturnsProcessStartErrorUnchanged(t *testing.T) {
	// R-G670-Y0YJ
	wantErr := errors.New("ssh start failed")
	deps := seam.Deps{Dir: "/work", Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		return seam.Result{}, wantErr
	}}

	got, err := Version(context.Background(), host.Host{Address: "192.0.2.10", Deps: deps})
	unwrapped := errors.Unwrap(err)
	if got != "" || unwrapped == nil || reflect.ValueOf(unwrapped).Pointer() != reflect.ValueOf(wantErr).Pointer() || errors.Unwrap(unwrapped) != nil {
		t.Fatalf("Version() = %q, %#v", got, err)
	}
}

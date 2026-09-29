package apps_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// R-952L-UOU1
var _ func(context.Context, host.Env) error = apps.EnsureAccount

// R-96AI-8GKQ
func TestEnsureAccountProtocol(t *testing.T) {
	idUser := host.Command{Name: "id", Args: []string{"--user", "ikigenba"}}
	idGroup := host.Command{Name: "id", Args: []string{"--group", "--name", "ikigenba"}}
	useradd := host.Command{Name: "useradd", Args: []string{"--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "--user-group", "ikigenba"}}
	transport := errors.New("process unavailable")
	tests := []struct {
		name        string
		commands    []host.Command
		results     []host.Result
		errors      []error
		wantError   string
		wantCommand bool
		wantCause   error
	}{
		{name: "existing account", commands: []host.Command{idUser, idGroup}, results: []host.Result{{Stdout: []byte(" 998\n")}, {Stdout: []byte(" \tikigenba \n")}}},
		{name: "new account", commands: []host.Command{idUser, useradd}, results: []host.Result{{ExitCode: 1}, {}}},
		{name: "root account", commands: []host.Command{idUser}, results: []host.Result{{Stdout: []byte("0\n")}}, wantError: "must not be root"},
		{name: "wrong primary group", commands: []host.Command{idUser, idGroup}, results: []host.Result{{Stdout: []byte("998\n")}, {Stdout: []byte("other\n")}}, wantError: "primary group"},
		{name: "invalid uid", commands: []host.Command{idUser}, results: []host.Result{{Stdout: []byte("bad\n")}}, wantError: "invalid uid"},
		{name: "uid status", commands: []host.Command{idUser}, results: []host.Result{{ExitCode: 2}}, wantCommand: true},
		{name: "uid transport", commands: []host.Command{idUser}, results: []host.Result{{}}, errors: []error{transport}, wantCommand: true, wantCause: transport},
		{name: "useradd status", commands: []host.Command{idUser, useradd}, results: []host.Result{{ExitCode: 1}, {ExitCode: 3}}, wantCommand: true},
		{name: "useradd transport", commands: []host.Command{idUser, useradd}, results: []host.Result{{ExitCode: 1}, {}}, errors: []error{nil, transport}, wantCommand: true, wantCause: transport},
		{name: "group status", commands: []host.Command{idUser, idGroup}, results: []host.Result{{Stdout: []byte("998\n")}, {ExitCode: 4}}, wantCommand: true},
		{name: "group transport", commands: []host.Command{idUser, idGroup}, results: []host.Result{{Stdout: []byte("998\n")}, {}}, errors: []error{nil, transport}, wantCommand: true, wantCause: transport},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			ctx := t.Context()
			var calls []host.Command
			env := host.Env{Root: root, Execute: func(actualCtx context.Context, command host.Command) (host.Result, error) {
				if actualCtx != ctx {
					t.Fatal("Execute received a different context")
				}
				calls = append(calls, command)
				index := len(calls) - 1
				if index >= len(tc.results) {
					t.Fatalf("unexpected command: %#v", command)
				}
				var executeErr error
				if index < len(tc.errors) {
					executeErr = tc.errors[index]
				}
				return tc.results[index], executeErr
			}}
			err := apps.EnsureAccount(ctx, env)
			if !reflect.DeepEqual(calls, tc.commands) {
				t.Errorf("commands = %#v, want %#v", calls, tc.commands)
			}
			if tc.wantError == "" && !tc.wantCommand {
				if err != nil {
					t.Fatalf("EnsureAccount: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("EnsureAccount succeeded, want failure")
			}
			if tc.wantError != "" && !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("error = %q, want %q", err, tc.wantError)
			}
			if tc.wantCommand {
				var commandErr *host.CommandError
				if !errors.As(err, &commandErr) {
					t.Errorf("error %T does not wrap *host.CommandError", err)
				}
			}
			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Errorf("error %v does not wrap %v", err, tc.wantCause)
			}
		})
	}
}

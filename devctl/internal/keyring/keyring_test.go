package keyring

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

func TestLookupAPI(t *testing.T) {
	// R-VU0Q-MF0R
	if got, want := reflect.TypeOf(Lookup), reflect.TypeFor[func(context.Context, seam.Deps, string) (string, error)](); got != want {
		t.Fatalf("Lookup type = %v, want %v", got, want)
	}

	typ := reflect.TypeFor[NoValueError]()
	if typ.NumField() != 1 || typ.Field(0).Name != "Name" || typ.Field(0).Type.Kind() != reflect.String {
		t.Fatalf("NoValueError fields = %v, want only Name string", typ)
	}
	err := &NoValueError{Name: "CRM_API_KEY"}
	if got, want := err.Error(), "no value for 'CRM_API_KEY' in the keyring or the environment"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestLookupPrefersEnvironment(t *testing.T) {
	// R-WDJ4-QQVV
	var gotName string
	calledExec := false
	deps := seam.Deps{
		Getenv: func(name string) string {
			gotName = name
			return "environment-value\r\n\n"
		},
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			calledExec = true
			return seam.Result{}, nil
		},
	}
	got, err := Lookup(context.Background(), deps, "CRM_API_KEY")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != "environment-value" {
		t.Fatalf("Lookup value = %q, want %q", got, "environment-value")
	}
	if gotName != "CRM_API_KEY" {
		t.Fatalf("Getenv name = %q, want CRM_API_KEY", gotName)
	}
	if calledExec {
		t.Fatal("Lookup called Exec for a nonempty environment value")
	}
}

func TestLookupFallsBackToKeyring(t *testing.T) {
	// R-WER1-4IMK
	tests := []struct {
		name        string
		result      seam.Result
		runnerErr   error
		want        string
		wantNoValue bool
		wantWrapped error
	}{
		{name: "value", result: seam.Result{Stdout: []byte("keyring-value\n\n")}, want: "keyring-value"},
		{name: "empty output", result: seam.Result{}, wantNoValue: true},
		{name: "newline-only output", result: seam.Result{Stdout: []byte("\r\n")}, wantNoValue: true},
		{name: "nonzero exit", result: seam.Result{Stdout: []byte("ignored"), ExitCode: 1}, wantNoValue: true},
		{name: "runner error", runnerErr: errors.New("cannot execute"), wantWrapped: errors.New("cannot execute")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var commands []seam.Cmd
			deps := seam.Deps{
				Dir: "/checkout/app",
				Getenv: func(name string) string {
					if name != "CRM_API_KEY" {
						t.Fatalf("Getenv name = %q, want CRM_API_KEY", name)
					}
					return "\n"
				},
				Exec: func(_ context.Context, command seam.Cmd) (seam.Result, error) {
					commands = append(commands, command)
					if test.runnerErr != nil {
						return seam.Result{}, test.runnerErr
					}
					return test.result, nil
				},
			}

			got, err := Lookup(context.Background(), deps, "CRM_API_KEY")
			if got != test.want {
				t.Fatalf("Lookup value = %q, want %q", got, test.want)
			}
			if len(commands) != 1 {
				t.Fatalf("Exec calls = %d, want 1", len(commands))
			}
			wantCommand := seam.Cmd{
				Path: "secret-tool",
				Args: []string{"lookup", "name", "CRM_API_KEY"},
				Dir:  "/checkout/app",
			}
			if !reflect.DeepEqual(commands[0], wantCommand) {
				t.Fatalf("command = %#v, want %#v", commands[0], wantCommand)
			}

			var noValue *NoValueError
			switch {
			case test.wantNoValue:
				if !errors.As(err, &noValue) || noValue.Name != "CRM_API_KEY" {
					t.Fatalf("error = %v, want *NoValueError for CRM_API_KEY", err)
				}
			case test.wantWrapped != nil:
				if err == nil || !strings.Contains(err.Error(), "secret-tool") || !strings.Contains(err.Error(), "cannot execute") {
					t.Fatalf("error = %v, want wrapped secret-tool error", err)
				}
			case err != nil:
				t.Fatalf("Lookup: %v", err)
			}
		})
	}
}

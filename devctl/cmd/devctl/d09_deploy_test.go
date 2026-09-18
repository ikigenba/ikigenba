package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cli"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantDeployUsage = `Usage: devctl --account <name> deploy <domain> <file>

Upload <file>, an <app>/dist/<app>-<tag>.tar.xz written by build, to the
deploy/ prefix of <domain>'s backup bucket and have opsctl on <domain> install
it from there. The app and tag (v<semver>) are read from the file name.
`

func TestDeployUsageDiagnosticsAtCommandBoundary(t *testing.T) {
	// R-V2NL-65SU R-Z3B4-LFWL
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{
			name:    "no operands",
			args:    []string{"--account", "SelectedProfile", "deploy"},
			message: "deploy needs <domain> and <file>",
		},
		{
			name:    "one operand",
			args:    []string{"--account", "SelectedProfile", "deploy", "foo.example"},
			message: "deploy needs <domain> and <file>",
		},
		{
			name:    "extra operand",
			args:    []string{"--account", "SelectedProfile", "deploy", "foo.example", "crm-v0.1.0.tar.xz", "extra"},
			message: "deploy takes only <domain> and <file>",
		},
		{
			name:    "unknown option first",
			args:    []string{"--account", "SelectedProfile", "deploy", "--unknown", "foo.example", "crm-v0.1.0.tar.xz"},
			message: "unknown option '--unknown'",
		},
		{
			name:    "unknown option later",
			args:    []string{"--account", "SelectedProfile", "deploy", "foo.example", "--unknown"},
			message: "unknown option '--unknown'",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr, cloudCalls, execCalls := invokeDeployBoundary(t, test.args...)
			wantStderr := "devctl: " + test.message + "\n\nsee 'devctl deploy --help' for usage\n"
			if code != 2 || stdout != "" || stderr != wantStderr {
				t.Fatalf("result = code %d, stdout %q, stderr %q; want 2, empty, %q", code, stdout, stderr, wantStderr)
			}
			if cloudCalls != 0 || execCalls != 0 {
				t.Fatalf("external calls = cloud %d, exec %d; want none", cloudCalls, execCalls)
			}
		})
	}
}

func TestDeployHelpAtCommandBoundary(t *testing.T) {
	// R-F99Q-M7XS
	for _, args := range [][]string{
		{"deploy", "--help"},
		{"deploy", "-h"},
		{"--account", "SelectedProfile", "deploy", "foo.example", "crm-v0.1.0.tar.xz", "--help"},
		{"--account", "SelectedProfile", "deploy", "foo.example", "crm-v0.1.0.tar.xz", "-h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr, cloudCalls, execCalls := invokeDeployBoundary(t, args...)
			if code != 0 || stdout != wantDeployUsage || stderr != "" {
				t.Fatalf("result = code %d, stdout %q, stderr %q; want 0, exact help, empty", code, stdout, stderr)
			}
			if cloudCalls != 0 || execCalls != 0 {
				t.Fatalf("external calls = cloud %d, exec %d; want none", cloudCalls, execCalls)
			}
		})
	}
}

func invokeDeployBoundary(t *testing.T, args ...string) (int, string, string, int, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cloudCalls, execCalls := 0, 0
	deps := seam.Deps{
		EUID: 1,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			execCalls++
			return seam.Result{}, errors.New("unexpected process access")
		},
	}
	cloud := reflect.ValueOf(&deps).Elem().FieldByName("Cloud")
	cloudErr := func() error { return errors.New("unexpected cloud access") }()
	cloud.Set(reflect.MakeFunc(cloud.Type(), func([]reflect.Value) []reflect.Value {
		cloudCalls++
		return []reflect.Value{reflect.Zero(cloud.Type().Out(0)), reflect.ValueOf(&cloudErr).Elem()}
	}))
	code := cli.Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr, deps)
	return code, stdout.String(), stderr.String(), cloudCalls, execCalls
}

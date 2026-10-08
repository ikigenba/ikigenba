package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cli"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
)

const wantDeployUsage = `Usage: devctl deploy <space> <sha|tag>
       devctl deploy <space> <file>

Build the suite at <sha|tag> as build does, check that the space holds every
secret the release's manifests declare, copy dist/<sha>.tar.xz to the space's
host, unpack it into /opt/ikigenba/releases/<sha>/, and have that release's
opsctl activate it. A tag is the release's label, exactly as typed; a sha
gives none.

Upload <file>, an <app>/dist/<app>-<sha>.tar.xz written by build, to the
space's deploy/ prefix in the bucket and have opsctl on the space install it
from there. The app and commit sha (40 lowercase hex digits) are read from the
file name.

An argument that ends in .tar.xz is a <file>; any other is a <sha|tag>.
`

func TestDeployUsageDiagnosticsAtCommandBoundary(t *testing.T) {
	// R-VSGE-LJI8 R-VTOA-ZB8X
	tests := []struct {
		name    string
		args    []string
		message string
	}{
		{
			name:    "no operands",
			args:    []string{"deploy"},
			message: "deploy needs <space> and <sha|tag> or <file>",
		},
		{
			name:    "one operand",
			args:    []string{"deploy", "sbx1"},
			message: "deploy needs <space> and <sha|tag> or <file>",
		},
		{
			name:    "extra operand",
			args:    []string{"deploy", "sbx1", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz", "extra"},
			message: "deploy takes only <space> and one <sha|tag> or <file>",
		},
		{
			name:    "unknown option first",
			args:    []string{"deploy", "--unknown", "sbx1", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz"},
			message: "unknown option '--unknown'",
		},
		{
			name:    "unknown option later",
			args:    []string{"deploy", "sbx1", "--unknown"},
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
	// R-O48O-O06U R-VUW7-D2ZM
	for _, args := range [][]string{
		{"deploy", "--help"},
		{"deploy", "-h"},
		{"deploy", "sbx1", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz", "--help"},
		{"deploy", "sbx1", "crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz", "-h"},
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

func TestDeployMissingArtifactAtCommandBoundary(t *testing.T) {
	// R-08GB-YDTQ R-VXC0-4MH0
	code, stdout, stderr, cloudCalls, execCalls := invokeDeployBoundary(t,
		"deploy", "sbx1", "crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz",
	)
	const wantStderr = "devctl: no such file 'crm/dist/crm-4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a.tar.xz'\n"
	if code != 2 || stdout != "" || stderr != wantStderr {
		t.Fatalf("result = code %d, stdout %q, stderr %q; want 2, empty, %q", code, stdout, stderr, wantStderr)
	}
	if cloudCalls != 0 || execCalls != 0 {
		t.Fatalf("external calls = cloud %d, exec %d; want none", cloudCalls, execCalls)
	}
}

func TestDeployInvalidArtifactNameAtCommandBoundary(t *testing.T) {
	// R-VW43-QUQB R-VXC0-4MH0
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.tar.xz"), []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, cloudCalls, execCalls := invokeDeployBoundaryAt(t, dir,
		"deploy", "sbx1", "notes.tar.xz",
	)
	const wantStderr = "devctl: 'notes.tar.xz' is not an app file build wrote: name is not <app>-<sha>.tar.xz\n"
	if code != 2 || stdout != "" || stderr != wantStderr {
		t.Fatalf("result = code %d, stdout %q, stderr %q; want 2, empty, %q", code, stdout, stderr, wantStderr)
	}
	if cloudCalls != 0 || execCalls != 0 {
		t.Fatalf("external calls = cloud %d, exec %d; want none", cloudCalls, execCalls)
	}
}

func invokeDeployBoundary(t *testing.T, args ...string) (int, string, string, int, int) {
	t.Helper()
	return invokeDeployBoundaryAt(t, t.TempDir(), args...)
}

func invokeDeployBoundaryAt(t *testing.T, dir string, args ...string) (int, string, string, int, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cloudCalls, execCalls := 0, 0
	deps := seam.Deps{
		EUID: 1,
		Dir:  dir,
		Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
			execCalls++
			return seam.Result{}, errors.New("unexpected process access")
		},
		Cloud: func(context.Context, string, string) (cloud.Clients, error) {
			cloudCalls++
			return cloud.Clients{}, errors.New("unexpected cloud access")
		},
	}
	code := cli.Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr, deps)
	return code, stdout.String(), stderr.String(), cloudCalls, execCalls
}

func TestDeployReleaseArchiveRedirectAtBoundary(t *testing.T) {
	// R-VYJW-IE7P R-VZRS-W5YE
	sha := "4b22285f0c1d9e2a7b6c5d4e3f2a1b0c9d8e7f6a"
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0700); err != nil {
		t.Fatal(err)
	}
	operand := "dist/" + sha + ".tar.xz"
	for _, space := range []string{"sbx1", "sbx1.ikigenba.dev"} {
		code, out, diagnostic, cloud, exec := invokeDeployBoundaryAt(t, dir, "deploy", space, operand)
		if code != 2 || out != "" || diagnostic != "devctl: no such file '"+operand+"'\n" || cloud != 0 || exec != 0 {
			t.Fatalf("missing result %d %q %q %d %d", code, out, diagnostic, cloud, exec)
		}
		if err := os.WriteFile(filepath.Join(dir, operand), []byte("archive"), 0600); err != nil {
			t.Fatal(err)
		}
		code, out, diagnostic, cloud, exec = invokeDeployBoundaryAt(t, dir, "deploy", space, operand)
		want := "devctl: '" + operand + "' is a release, not an app file\n\nrun 'devctl deploy " + space + " " + sha + "'\n"
		if code != 2 || out != "" || diagnostic != want || cloud != 0 || exec != 0 {
			t.Fatalf("redirect result %d %q %q %d %d", code, out, diagnostic, cloud, exec)
		}
		if err := os.Remove(filepath.Join(dir, operand)); err != nil {
			t.Fatal(err)
		}
	}
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
)

func createKnownHosts(c seam.Cmd) string {
	for _, arg := range c.Args {
		if strings.HasPrefix(arg, "UserKnownHostsFile=") {
			return strings.TrimPrefix(arg, "UserKnownHostsFile=")
		}
	}
	return ""
}

func writeCreateKnownHost(t *testing.T, base string, c seam.Cmd) {
	t.Helper()
	known := createKnownHosts(c)
	if known == "" {
		return
	}
	dir := filepath.Dir(known)
	if filepath.Dir(dir) != base || !strings.HasPrefix(filepath.Base(dir), "create.") || filepath.Base(known) != "known_hosts.launch" {
		t.Fatal("known hosts outside test temporary directory", c)
	}
	if err := os.WriteFile(known, []byte("launch host key\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertCreateKnownHosts(t *testing.T, f *completeCreateFake) {
	t.Helper()
	if len(f.commands) < 2 || f.commands[0].Path != "ssh" || f.commands[0].Args[len(f.commands[0].Args)-1] != "'true'" || f.commands[1].Args[len(f.commands[1].Args)-1] != "'sudo' 'systemd-run' '--quiet' '--wait' '--pipe' '--collect' 'cloud-init' 'status' '--wait'" {
		t.Fatal("missing ordered wait and cloud-init", f.commands)
	}
	launch := ""
	restored := false
	probes, cloudInit, copies := 0, 0, 0
	for _, c := range f.commands {
		if c.Path != "ssh" && c.Path != "scp" {
			t.Fatal("unexpected transport", c)
		}
		if c.Dir != f.checkoutRoot || !strings.Contains(strings.Join(c.Args, " "), "ec2-user@"+f.domain()) {
			t.Fatal("incorrect host", c)
		}
		remote := c.Args[len(c.Args)-1]
		if remote == "'true'" {
			probes++
		}
		if strings.Contains(remote, "'cloud-init'") {
			cloudInit++
		}
		if c.Path == "scp" {
			copies++
			if c.Args[len(c.Args)-2] != filepath.Join(f.checkoutRoot, "dist", createSHA+".tar.xz") || remote != "ec2-user@"+f.domain()+":/tmp/upload" {
				t.Fatal("wrong release copy", c)
			}
		}
		if !f.backup && strings.HasSuffix(remote, "'init'") {
			restored = true
		}
		known := createKnownHosts(c)
		if restored {
			if known != "" {
				t.Fatal("launch key used after restore", c)
			}
		} else {
			if known == "" || launch != "" && known != launch {
				t.Fatal("launch key scope changed", c)
			}
			launch = known
		}
		if strings.HasSuffix(remote, "'host' 'restore'") {
			restored = true
		}
	}
	if launch == "" || !restored || probes != 1 || cloudInit != 1 || copies != 1 {
		t.Fatal("missing launch or restored connection", f.commands)
	}
	assertCreateTempEmpty(t, f.tmp)
}

func assertCreateTempEmpty(t *testing.T, base string) {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatalf("create temporary files remain: %v, %v", entries, err)
	}
}

func TestCreateTemporaryDirectoryCleanup(t *testing.T) {
	// R-3EX2-S3W6
	for _, stop := range []string{"", "launch", "restore"} {
		t.Run("stop="+stop, func(t *testing.T) {
			f := newCompleteCreateFake(t)
			f.backup = true
			f.fail = stop
			deps := f.deps()
			exec := deps.Exec
			deps.Exec = func(ctx context.Context, c seam.Cmd) (seam.Result, error) {
				if c.Path == "ssh" && strings.HasSuffix(c.Args[len(c.Args)-1], "'host' 'restore'") && stop == "restore" {
					writeCreateKnownHost(t, f.tmp, c)
					return seam.Result{ExitCode: 1, Stderr: []byte("restore failed")}, nil
				}
				return exec(ctx, c)
			}
			var out bytes.Buffer
			writer := &createDirectoryWriter{t: t, base: f.tmp, Writer: &out}
			err := spacecreate.Run(t.Context(), []string{"staging", "--acme-email=ops@ikigenba.dev"}, "test-version", writer, deps)
			if (err != nil) != (stop != "") || !writer.seen {
				t.Fatalf("Run error %v, directory observed %t", err, writer.seen)
			}
			assertCreateTempEmpty(t, f.tmp)
		})
	}
}

type createDirectoryWriter struct {
	t    *testing.T
	base string
	io.Writer
	seen bool
}

func (w *createDirectoryWriter) Write(p []byte) (int, error) {
	if !w.seen {
		entries, err := os.ReadDir(w.base)
		if err != nil || len(entries) != 1 || !entries[0].IsDir() || !strings.HasPrefix(entries[0].Name(), "create.") {
			w.t.Fatalf("first stdout preceded temporary directory: %v, %v", entries, err)
		}
		// Leave nested contents so cleanup must remove the entire directory.
		dir := filepath.Join(w.base, entries[0].Name(), "nested")
		if err := os.Mkdir(dir, 0700); err != nil {
			w.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sentinel"), []byte("temporary"), 0600); err != nil {
			w.t.Fatal(err)
		}
		w.seen = true
	}
	return w.Writer.Write(p)
}

func TestCreateTemporaryDirectoryRefusals(t *testing.T) {
	// R-3EX2-S3W6
	for _, whitespace := range []string{"a b", "a\tb", "a\nb", "a\u2003b"} {
		t.Run(whitespace, func(t *testing.T) {
			f := newCompleteCreateFake(t)
			base := f.tmp
			f.tmp = filepath.Join(base, whitespace)
			got := invokeWithDeps(f.deps(), "space", "create", "sbx1", "--acme-email=x")
			assertResult(t, got, 2, "", "devctl: temporary directory '"+f.tmp+"' holds whitespace\n")
			if len(f.mutations) != 0 || len(f.commands) != 0 {
				t.Fatal("mutation after refusal", f.mutations, f.commands)
			}
			assertCreateTempEmpty(t, base)
		})
	}
	t.Run("creation failure wraps filesystem error", func(t *testing.T) {
		f := newCompleteCreateFake(t)
		base := f.tmp
		f.tmp = filepath.Join(base, "missing", "directory")
		var out bytes.Buffer
		err := spacecreate.Run(t.Context(), []string{"sbx1", "--acme-email=x"}, "test-version", &out, f.deps())
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) || !errors.Is(err, os.ErrNotExist) || errors.Unwrap(err) == nil || out.Len() != 0 || len(f.mutations) != 0 || len(f.commands) != 0 {
			t.Fatalf("creation error %v, output %q, mutations %v", err, out.String(), f.mutations)
		}
		assertCreateTempEmpty(t, base)
	})
	t.Run("preflight precedes temporary work", func(t *testing.T) {
		f := newCompleteCreateFake(t)
		deps := f.deps()
		deps.Getenv = func(string) string {
			t.Fatal("environment read before preflight passed")
			return ""
		}
		f.write("infra/terraform.tfvars.json", `{}`)
		got := invokeWithDeps(deps, "space", "create", "sbx1", "--acme-email=x")
		if got.code != 2 || got.stdout != "" || len(f.mutations) != 0 {
			t.Fatal(got, f.mutations)
		}
		assertCreateTempEmpty(t, f.tmp)
	})
}

func (f *completeCreateFake) domain() string {
	if f.backup {
		return "staging.ikigenba.dev"
	}
	return testDomain
}

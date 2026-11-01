package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func syntheticDeps(t testing.TB) (seam.Deps, string) {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{"HOME": filepath.Join(root, "home"), "XDG_CONFIG_HOME": filepath.Join(root, "cfg"), "XDG_STATE_HOME": filepath.Join(root, "state")}
	for _, dir := range []string{env["HOME"], env["XDG_CONFIG_HOME"], env["XDG_STATE_HOME"], filepath.Join(root, "etc"), filepath.Join(root, "work")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	deps := seam.Deps{EUID: 1000, Getenv: func(key string) string { return env[key] }, Dir: filepath.Join(root, "work"), Arg0: filepath.Join(root, "bin", "turns"), Etc: filepath.Join(root, "etc"), Now: func() time.Time { return time.Date(2026, 11, 1, 14, 5, 6, 0, time.UTC) }, Hostname: func() (string, error) { return "fixture-host.example", nil }, HTTP: &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) {
		t.Errorf("unexpected HTTP request")
		return nil, fmt.Errorf("unexpected request")
	})}, Exec: func(context.Context, seam.Cmd) (seam.Result, error) {
		t.Errorf("unexpected process invocation")
		return seam.Result{}, fmt.Errorf("unexpected process")
	}}
	return deps, root
}

func runCLI(t testing.TB, deps seam.Deps, args []string, stdin io.Reader) (int, string, string) {
	t.Helper()
	return runCLIContext(context.Background(), t, deps, args, stdin)
}
func runCLIContext(ctx context.Context, t testing.TB, deps seam.Deps, args []string, stdin io.Reader) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	// R-V7QC-7CRL: exercise the exported full Run signature.
	api := struct {
		Run func(context.Context, []string, io.Reader, io.Writer, io.Writer, seam.Deps) int
	}{Run: Run}
	code := api.Run(ctx, args, stdin, &out, &errOut, deps)
	// R-VA64-YW8Z R-V8Y8-L4IA: every Run call uses only the five declared codes.
	switch code {
	case ExitOK, ExitFailed, ExitUsage, ExitRoot, ExitNotConfigured:
	default:
		t.Errorf("invalid exit code %d", code)
	}
	// R-CIE0-1WAF: ordinary behavioral cases always inject an ordinary uid.
	if deps.EUID == 0 && code != ExitRoot {
		t.Error("root call escaped root refusal")
	}
	stderr := errOut.String()
	// R-W9J8-28R4 R-WD6X-7JZ7 R-WEET-LBPW.
	if stderr != "" {
		lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
		if !strings.HasPrefix(lines[0], "turns: ") || !strings.HasSuffix(stderr, "\n") {
			t.Errorf("malformed diagnostic %q", stderr)
		}
		if len(lines) > 1 && (len(lines) < 3 || lines[1] != "" || lines[2] == "") {
			t.Errorf("malformed diagnostic detail %q", stderr)
		}
		for _, line := range lines {
			if strings.HasPrefix(line, "Usage: turns") {
				t.Errorf("usage on stderr: %q", stderr)
			}
		}
	}
	if code == ExitOK && stderr != "" {
		t.Errorf("successful invocation diagnosed %q", stderr)
	}
	// R-929V-QNBT: stdout holds product, without diagnostics or terminal escapes.
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "turns: ") || strings.ContainsRune(line, '\x1b') {
			t.Errorf("non-product stdout %q", out.String())
		}
	}
	return code, out.String(), stderr
}

type treeEntry struct {
	Mode fs.FileMode
	Data string
}

func snapshotTree(t testing.TB, root string) map[string]treeEntry {
	t.Helper()
	confined, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := confined.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	tree := map[string]treeEntry{}
	err = fs.WalkDir(confined.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		entry := treeEntry{Mode: info.Mode()}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, readErr := confined.Readlink(name)
			if readErr != nil {
				return readErr
			}
			entry.Data = target
		}
		if info.Mode().IsRegular() {
			data, readErr := confined.ReadFile(name)
			if readErr != nil {
				if info.Mode().Perm() == 0 {
					entry.Data = "unreadable"
				} else {
					return readErr
				}
			} else {
				entry.Data = string(data)
			}
		}
		key := ""
		if name != "." {
			key = string(filepath.Separator) + filepath.FromSlash(name)
		}
		tree[key] = entry
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertTreeEqual(t testing.TB, want, got map[string]treeEntry) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("tree changed\nwant %#v\ngot %#v", want, got)
	}
}

type recordingReader struct {
	Reader io.Reader
	Reads  int
}

func (r *recordingReader) Read(p []byte) (int, error) {
	r.Reads++
	if r.Reader == nil {
		return 0, io.EOF
	}
	return r.Reader.Read(p)
}
func writeFixture(t testing.TB, path, text string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

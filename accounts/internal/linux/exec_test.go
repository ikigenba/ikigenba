package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestResolveCommand(t *testing.T) {
	// R-XVSO-DMEJ
	const name = "probe"
	base := t.TempDir()
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	dirs := []string{"local-sbin", "local-bin", "sbin", "bin"}
	var searchDirs []string
	for _, dir := range dirs {
		if err := root.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		searchDirs = append(searchDirs, filepath.Join(base, dir))
	}
	path := strings.Join(searchDirs, ":")
	first, second := filepath.Join(dirs[0], name), filepath.Join(dirs[1], name)
	if err := root.Mkdir(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(second, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveCommand(name, path); err == nil {
		t.Fatalf("resolveCommand found ineligible candidate %q", got)
	}
	check := func(want string) {
		t.Helper()
		want = filepath.Join(base, want)
		if got, err := resolveCommand(name, path); err != nil || got != want {
			t.Fatalf("resolveCommand = %q, %v, want %q, nil", got, err, want)
		}
	}
	for _, dir := range []string{dirs[3], dirs[2]} {
		candidate := filepath.Join(dir, name)
		if err := root.WriteFile(candidate, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := root.Chmod(candidate, 0o700); err != nil {
			t.Fatal(err)
		}
		check(candidate)
	}
	if err := root.Chmod(second, 0o700); err != nil {
		t.Fatal(err)
	}
	check(second)
	if err := root.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile(first, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Chmod(first, 0o700); err != nil {
		t.Fatal(err)
	}
	check(first)
}

// A descendant holds stdin and stdout open until it reads the test's release.
// Cancellation must terminate it along with the direct command, closing stdin
// before the host can proceed to clone cleanup.
func TestExecCloneCancellationStopsDescendants(t *testing.T) {
	// R-WX4U-5OMG
	root := t.TempDir()
	script := filepath.Join(root, "clone.sh")
	body := `exec 3<&0
(
  printf 'ready\n'
  if IFS= read -r release; then
    printf '%s\n' "$release" > "$1"
  fi
) <&3 3<&- &
wait "$!"
`
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	input, release, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = release.Close()
	})
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), cloneProcessGroup{}, true))
	defer cancel()
	stdout := &cancelOnReady{cancel: cancel, input: input}
	_, err = Exec(ctx, Command{Name: "/bin/sh", Args: []string{script, filepath.Join(root, "continued")}, Stdin: input, Stdout: stdout})
	if !errors.Is(err, context.Canceled) || stdout.output != "ready\n" {
		t.Fatalf("Exec output %q, error %v, want readiness then context cancellation", stdout.output, err)
	}
	if _, err := release.WriteString("continued\n"); !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("the command's descendant did not close stdin: %v", err)
	}
}

type cancelOnReady struct {
	output string
	input  *os.File
	cancel context.CancelFunc
}

func (w *cancelOnReady) Write(p []byte) (int, error) {
	w.output += string(p)
	if w.output == "ready\n" {
		_ = w.input.Close()
		w.cancel()
	}
	return len(p), nil
}

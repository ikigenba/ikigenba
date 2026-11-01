package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/turns"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
)

func runInstall(ctx context.Context, _ []string, _ io.Reader, _ io.Writer, stderr io.Writer, deps seam.Deps) int {
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		return diagnostic(stderr, err.Error(), "")
	}
	host := false
	if p.HostUnits != "" {
		_, err = os.Stat(filepath.Join(p.HostUnits, turns.TimerUnit))
		host = err == nil
	}
	if !host {
		exe := installExecutable(deps)
		if exe == "" {
			return diagnostic(stderr, "cannot find the turns that is running", "run 'turns install' by its full path")
		}
		if !installSafePath(exe) {
			return diagnostic(stderr, "systemd cannot run turns from "+strconv.Quote(exe), "move turns to a path that holds no whitespace, quote, backslash, '%', '$', control character or invalid UTF-8, and run install again")
		}
		if err = installDirectory(p.UserUnits); err != nil {
			return diagnostic(stderr, fmt.Sprintf("could not create '%s'", p.UserUnits), err.Error())
		}
		unitRoot, rootErr := os.OpenRoot(p.UserUnits)
		if rootErr != nil {
			return diagnostic(stderr, fmt.Sprintf("could not create '%s'", p.UserUnits), rootErr.Error())
		}
		defer func() { _ = unitRoot.Close() }()
		for _, name := range []string{turns.ServiceUnit, turns.TimerUnit} {
			data, readErr := fs.ReadFile(turns.Units(), name)
			if readErr != nil {
				return diagnostic(stderr, "could not read embedded units", readErr.Error())
			}
			if name == turns.ServiceUnit {
				lines := strings.Split(string(data), "\n")
				for i, line := range lines {
					if strings.HasPrefix(line, "ExecStart=") {
						lines[i] = "ExecStart=" + exe + " push"
					}
				}
				data = []byte(strings.Join(lines, "\n"))
			}
			path := filepath.Join(p.UserUnits, name)
			if info, statErr := unitRoot.Lstat(name); statErr == nil && !info.IsDir() {
				err = unitRoot.Remove(name)
			} else if statErr != nil && !os.IsNotExist(statErr) {
				err = statErr
			} else {
				err = nil
			}
			if err == nil {
				err = unitRoot.WriteFile(name, data, 0644)
			}
			if err == nil {
				err = unitRoot.Chmod(name, 0644)
			}
			if err != nil {
				return diagnostic(stderr, fmt.Sprintf("could not write '%s'", path), err.Error())
			}
		}
		if code := installSystemctl(ctx, deps, stderr, []string{"--user", "daemon-reload"}); code != ExitOK {
			return code
		}
	}
	return installSystemctl(ctx, deps, stderr, []string{"--user", "enable", "--now", turns.TimerUnit})
}

func installExecutable(deps seam.Deps) string {
	if deps.Arg0 == "" {
		return ""
	}
	absolute := func(path string) string {
		if !filepath.IsAbs(path) {
			path = filepath.Join(deps.Dir, path)
		}
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			return ""
		}
		return path
	}
	if strings.Contains(deps.Arg0, "/") {
		return absolute(deps.Arg0)
	}
	if deps.Getenv("PATH") == "" {
		return ""
	}
	for _, entry := range strings.Split(deps.Getenv("PATH"), ":") {
		candidate := absolute(filepath.Join(entry, deps.Arg0))
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate
		}
	}
	return ""
}

func installSafePath(path string) bool {
	if !utf8.ValidString(path) {
		return false
	}
	for _, b := range []byte(path) {
		if b < 0x20 || b == 0x7f || strings.ContainsRune(" \"'\\%$", rune(b)) {
			return false
		}
	}
	return true
}

func installDirectory(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: fs.ErrExist}
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err = installDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	name := filepath.Base(path)
	if err = parent.Mkdir(name, 0755); err != nil {
		return err
	}
	return parent.Chmod(name, 0755)
}

func installSystemctl(ctx context.Context, deps seam.Deps, stderr io.Writer, args []string) int {
	result, err := deps.Exec(ctx, seam.Cmd{Path: "systemctl", Args: args, Dir: deps.Dir})
	if err != nil {
		return diagnostic(stderr, "could not enable turns.timer", err.Error())
	}
	if result.ExitCode == 0 {
		return ExitOK
	}
	detail := seam.QuoteOutput(string(result.Stderr))
	if detail == "" {
		detail = seam.QuoteOutput(string(result.Stdout))
	}
	return diagnosticOutput(stderr, "could not enable turns.timer", detail)
}

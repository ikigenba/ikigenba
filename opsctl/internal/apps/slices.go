package apps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

var memTotalLine = regexp.MustCompile(`^MemTotal: +([0-9]+) kB$`)

// SetupSlices sizes the suite's slice units and places nginx in the core slice.
func SetupSlices(ctx context.Context, env host.Env) error {
	filesystem, err := os.OpenRoot(env.Root)
	if err != nil {
		return fmt.Errorf("/proc/meminfo: %w", err)
	}
	defer func() { _ = filesystem.Close() }()
	total, err := sliceMemory(filesystem)
	if err != nil {
		return err
	}
	apps := (2*total + 1) / 3
	files := []struct{ name, data string }{
		{"ikigenba.slice", fmt.Sprintf("[Unit]\nDescription=Ikigenba suite\n\n[Slice]\nCPUWeight=100\nMemoryMax=%dM\n", total*64)},
		{"ikigenba-core.slice", "[Unit]\nDescription=Ikigenba core\n\n[Slice]\nCPUWeight=300\n"},
		{"ikigenba-apps.slice", fmt.Sprintf("[Unit]\nDescription=Ikigenba apps\n\n[Slice]\nCPUWeight=100\nMemoryMax=%dM\nMemoryHigh=%dM\n", apps*64, apps*60)},
		{"nginx.service.d/ikigenba.conf", "[Service]\nSlice=ikigenba-core.slice\nCPUWeight=100\nMemoryMax=128M\nMemoryLow=32M\n"},
	}
	changed, nginxChanged := false, false
	for i, file := range files {
		updated, writeErr := writeSliceFile(filesystem, "etc/systemd/system/"+file.name, []byte(file.data))
		if writeErr != nil {
			return writeErr
		}
		changed = changed || updated
		if i == len(files)-1 {
			nginxChanged = updated
		}
	}
	if changed {
		if err := sliceCommand(ctx, env, "daemon-reload"); err != nil {
			return err
		}
	}
	if nginxChanged {
		return sliceCommand(ctx, env, "restart", "nginx")
	}
	return nil
}

// sliceMemory returns the suite ceiling in 64 MiB units without overflowing
// even when MemTotal approaches the maximum signed byte count.
func sliceMemory(filesystem *os.Root) (int64, error) {
	data, err := filesystem.ReadFile("proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("/proc/meminfo: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		match := memTotalLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		count, parseErr := strconv.ParseInt(match[1], 10, 64)
		if parseErr != nil || count > 9223372036854775807/1024 {
			return 0, errors.New("/proc/meminfo: MemTotal byte count out of range")
		}
		memory := count * 1024
		const denominator int64 = 5 * 67108864
		total := 4*(memory/denominator) + (4*(memory%denominator)+denominator/2)/denominator
		if total == 0 {
			return 0, errors.New("/proc/meminfo: suite memory ceiling is zero")
		}
		return total, nil
	}
	return 0, errors.New("/proc/meminfo: no valid MemTotal line")
}

func writeSliceFile(filesystem *os.Root, name string, data []byte) (bool, error) {
	info, err := filesystem.Lstat(name)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("/%s is not a regular file", name)
		}
		old, readErr := filesystem.ReadFile(name)
		if readErr != nil {
			return false, readErr
		}
		if bytes.Equal(old, data) {
			if info.Mode() != 0o644 {
				return false, filesystem.Chmod(name, 0o644)
			}
			return false, nil
		}
	}
	directory := path.Dir(name)
	_, directoryErr := filesystem.Stat(directory)
	if directoryErr != nil && !errors.Is(directoryErr, fs.ErrNotExist) {
		return false, directoryErr
	}
	if err := filesystem.MkdirAll(directory, 0o755); err != nil {
		return false, err
	}
	if errors.Is(directoryErr, fs.ErrNotExist) {
		if err := filesystem.Chmod(directory, 0o755); err != nil {
			return false, err
		}
	}
	if err := filesystem.WriteFile(name, data, 0o644); err != nil {
		return false, err
	}
	if err := filesystem.Chmod(name, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func sliceCommand(ctx context.Context, env host.Env, args ...string) error {
	label := "systemctl " + strings.Join(args, " ")
	if env.Execute == nil {
		return &host.CommandError{Label: label, Err: errors.New("host execution dependency not set")}
	}
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: args})
	if err != nil || result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result, Err: err}
	}
	return nil
}

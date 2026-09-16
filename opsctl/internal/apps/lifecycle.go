package apps

import (
	"context"
	"encoding/binary"
	"os"
	"path"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// UninstallHooks connects app removal to CLI-owned reporting and configuration.
type UninstallHooks struct {
	Report    func(step, detail string, success bool) error
	Configure func(context.Context, Manifest) error
}

// StatusRow contains the observable host state of one service.
type StatusRow struct {
	Name        string
	Version     string
	State       string
	JournalMode string
}

// LifecycleError describes an app lifecycle failure and its process exit code.
type LifecycleError struct {
	Code    int
	Message string
	Cause   error
}

func (failure *LifecycleError) Error() string { return failure.Message }

func (failure *LifecycleError) Unwrap() error { return failure.Cause }

// Status reports the independently observable state of every discovered service.
func Status(ctx context.Context, env host.Env) ([]StatusRow, error) {
	services, err := Discover(env.Root)
	if err != nil {
		return nil, &LifecycleError{Code: 1, Message: "status failed", Cause: err}
	}

	rows := make([]StatusRow, 0, len(services))
	for _, service := range services {
		row := StatusRow{
			Name:        service.Name,
			Version:     serviceVersion(ctx, env, service.Name),
			State:       serviceState(ctx, env, service.Name),
			JournalMode: serviceJournalMode(env.Root, service),
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func serviceVersion(ctx context.Context, env host.Env, name string) string {
	if env.Execute == nil {
		return "-"
	}
	binary := rootedHostPath(env.Root, "opt", name, "bin", name)
	result, err := env.Execute(ctx, host.Command{Name: binary, Args: []string{"--version"}})
	if err != nil || result.ExitCode != 0 {
		return "-"
	}
	version := string(result.Stdout)
	if strings.HasSuffix(version, "\r\n") {
		version = strings.TrimSuffix(version, "\r\n")
	} else {
		version = strings.TrimSuffix(version, "\n")
	}
	if version == "" {
		return "-"
	}
	return version
}

func serviceState(ctx context.Context, env host.Env, name string) string {
	if ValidateName(name) != nil || env.Execute == nil {
		return "-"
	}
	result, err := env.Execute(ctx, host.Command{
		Name: "systemctl",
		Args: []string{"show", "--property=LoadState", "--property=ActiveState", appUnitName(name)},
	})
	if err != nil || result.ExitCode != 0 {
		return "-"
	}

	properties := make(map[string]string, 2)
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		key, value, found := strings.Cut(strings.TrimSuffix(line, "\r"), "=")
		if found {
			properties[key] = value
		}
	}
	if properties["LoadState"] != "loaded" {
		return "-"
	}
	if properties["ActiveState"] == "" {
		return "-"
	}
	return properties["ActiveState"]
}

func serviceJournalMode(root string, service Service) string {
	if service.ManifestError != nil || service.Manifest == nil || service.Manifest.Database == nil {
		return "-"
	}
	filesystem, err := os.OpenRoot(root)
	if err != nil {
		return "-"
	}
	defer func() { _ = filesystem.Close() }()

	databasePath := path.Join("opt", service.Name, service.Manifest.Database.Path)
	file, err := filesystem.Open(databasePath)
	if err != nil {
		return "-"
	}
	defer func() { _ = file.Close() }()
	return sqliteJournalMode(file)
}

func sqliteJournalMode(file *os.File) string {
	header := make([]byte, 100)
	if _, err := file.ReadAt(header, 0); err != nil {
		return "-"
	}
	pageSize := sqlitePageSize(binary.BigEndian.Uint16(header[16:18]))
	if string(header[:16]) != "SQLite format 3\x00" || pageSize == 0 {
		return "-"
	}
	info, err := file.Stat()
	if err != nil || info.Size() < pageSize || info.Size()%pageSize != 0 {
		return "-"
	}
	if header[21] != 64 || header[22] != 32 || header[23] != 32 {
		return "-"
	}
	if schema := binary.BigEndian.Uint32(header[44:48]); schema < 1 || schema > 4 {
		return "-"
	}
	if encoding := binary.BigEndian.Uint32(header[56:60]); encoding < 1 || encoding > 3 {
		return "-"
	}
	if header[18] != header[19] {
		return "-"
	}
	switch header[18] {
	case 1:
		return "delete"
	case 2:
		return "wal"
	default:
		return "-"
	}
}

func sqlitePageSize(encoded uint16) int64 {
	if encoded == 1 {
		return 65536
	}
	if encoded >= 512 && encoded <= 32768 && encoded&(encoded-1) == 0 {
		return int64(encoded)
	}
	return 0
}

var _ error = (*LifecycleError)(nil)
var _ interface{ Unwrap() error } = (*LifecycleError)(nil)

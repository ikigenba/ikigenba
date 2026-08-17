package opsctl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SeedState writes a rotating credential into an existing service tree. The
// credential is write-once unless force is explicitly requested.
func (o *Opsctl) SeedState(ctx context.Context, service, name string, value io.Reader, force bool) error {
	prefix := fmt.Sprintf("seed-state %s/%s", service, name)
	if o.Root == "" {
		return fmt.Errorf("%s: IKIGENBA_ROOT is unset", prefix)
	}
	if !pathComponent(service) || !pathComponent(name) {
		return fmt.Errorf("%s: service and credential must each be a single path component", prefix)
	}

	serviceDir := filepath.Join(o.Root, service)
	info, err := os.Stat(serviceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: unknown service %q", prefix, service)
		}
		return fmt.Errorf("%s: inspect service: %w", prefix, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: unknown service %q", prefix, service)
	}

	data, err := io.ReadAll(value)
	if err != nil {
		return fmt.Errorf("%s: read stdin: %w", prefix, err)
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	if len(data) == 0 {
		return fmt.Errorf("%s: stdin is empty", prefix)
	}

	target := filepath.Join(serviceDir, "state", name)
	if !force {
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("%s: credential already exists (use --force to replace it)", prefix)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("%s: inspect target: %w", prefix, err)
		}
	}
	if err := writeSeedAtomic(target, data, force); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s: credential already exists (use --force to replace it)", prefix)
		}
		return fmt.Errorf("%s: write credential: %w", prefix, err)
	}
	if err := o.System.ChownTree(ctx, service, service, target); err != nil {
		return fmt.Errorf("%s: set ownership: %w", prefix, err)
	}
	return nil
}

func pathComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`)
}

func writeSeedAtomic(path string, data []byte, force bool) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".seed-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if force {
		return os.Rename(tmpName, path)
	}
	// A hard link publishes the completed temp file without replacing an
	// existing target, preserving write-once behavior even across a race.
	return os.Link(tmpName, path)
}

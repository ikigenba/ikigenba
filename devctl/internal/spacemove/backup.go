package spacemove

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// ArchError reports a host architecture that cannot run the backup tool.
type ArchError struct{ Machine string }

func (e *ArchError) Error() string {
	return fmt.Sprintf("backup: host architecture '%s' is neither x86_64 nor aarch64", e.Machine)
}

// ExitCode returns an operation failure.
func (e *ArchError) ExitCode() int { return 1 }

func backup(ctx context.Context, stdout io.Writer, p preflightResult, h host.Host, dir string) error {
	output, err := h.Run(ctx, "backup", "uname", "-m")
	if err != nil {
		return err
	}
	machine := strings.TrimRight(output.Stdout, "\n")
	arch := ""
	switch machine {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	default:
		return &ArchError{Machine: machine}
	}
	local := filepath.Join(dir, "opsctl")
	if err := build.Tool(ctx, p.checkout, p.sha, "opsctl", arch, local); err != nil {
		return err
	}
	output, err = h.Run(ctx, "backup", "mktemp")
	if err != nil {
		return err
	}
	remote := strings.TrimRight(output.Stdout, "\n")
	name, valid := strings.CutPrefix(remote, "/tmp/")
	if !valid || name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("backup: mktemp gave '%s', not a file under /tmp", remote)
	}
	err = backupRemote(ctx, h, local, remote)
	if err != nil {
		return err
	}
	space.Step(stdout, "backup", "opsctl host backup, "+arch)
	return nil
}

func backupRemote(ctx context.Context, h host.Host, local, remote string) (err error) {
	defer func() {
		_, removeErr := h.Run(context.WithoutCancel(ctx), "backup", "rm", "-f", remote)
		if err == nil {
			err = removeErr
		}
	}()
	if err := h.Copy(ctx, "backup", local, remote); err != nil {
		return err
	}
	if _, err := h.Run(ctx, "backup", "chmod", "0700", remote); err != nil {
		return err
	}
	_, err = h.Sudo(ctx, "backup", remote, "host", "backup")
	return err
}

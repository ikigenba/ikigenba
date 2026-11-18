package linux

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

const incomingDaemon = "/usr/local/bin/.accountsd.new"

// InstallDaemon verifies a streamed update before replacing the daemon.
func (h *Host) InstallDaemon(ctx context.Context, body io.Reader, digest string) error {
	if err := h.mkdir(filepath.Dir(DaemonPath)); err != nil {
		return err
	}
	tmpPath := h.path(incomingDaemon)
	f, err := os.OpenFile(filepath.Clean(tmpPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		// A process killed mid-update may have left this staging file behind.
		if err := os.Remove(tmpPath); err != nil {
			return hostError(incomingDaemon, err)
		}
		f, err = os.OpenFile(filepath.Clean(tmpPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	if err != nil {
		return hostError(incomingDaemon, err)
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := f.Chmod(0o755); err != nil {
		return hostError(incomingDaemon, err)
	}
	if err := machine.VerifyDaemon(ctx, f, body, digest); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return hostError(incomingDaemon, err)
	}
	if err := f.Close(); err != nil {
		return hostError(incomingDaemon, err)
	}
	if err := os.Rename(tmpPath, h.path(DaemonPath)); err != nil {
		return hostError(DaemonPath, err)
	}
	return nil
}

package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// LockRegistryPort serializes tests that must bind a fixed registry port.
// The returned function releases the lock and must be called after the
// listener using the port has closed.
func LockRegistryPort(port int) (func() error, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("artifacts-registry-port-%d.lock", port))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open registry port lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock registry port: %w", err)
	}
	return func() error {
		unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		closeErr := file.Close()
		if unlockErr != nil {
			return fmt.Errorf("unlock registry port: %w", unlockErr)
		}
		return closeErr
	}, nil
}

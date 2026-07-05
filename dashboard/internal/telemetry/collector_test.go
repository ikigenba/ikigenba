package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunSamplesImmediatelyAtStartup(t *testing.T) {
	store := NewStore()
	cfg := collectorFixture(t)
	cfg.Interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, store, cfg, discardLogger())
	}()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	}()

	// R-FDAM-Q7U8
	waitForSamples(t, store, 1, SeriesSystemMem, SeriesSystemDisk, SeriesServiceMem("crm"), SeriesServiceDisk("crm"))
}

func TestRunSamplesAgainOnTickerTicks(t *testing.T) {
	store := NewStore()
	cfg := collectorFixture(t)
	cfg.Interval = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, store, cfg, discardLogger())
	}()

	// R-FEIJ-3ZKX
	waitForSamples(t, store, 3, SeriesSystemMem, SeriesSystemDisk, SeriesServiceMem("crm"), SeriesServiceDisk("crm"))
	cancel()
	if err := waitRunDone(done); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunReturnsNilWhenContextIsCancelled(t *testing.T) {
	store := NewStore()
	cfg := collectorFixture(t)
	cfg.Interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, store, cfg, discardLogger())
	}()
	waitForSamples(t, store, 1, SeriesSystemMem)
	cancel()

	// R-FFQF-HRBM
	if err := waitRunDone(done); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestCollectLogsSourceErrorAndContinuesSiblingSeries(t *testing.T) {
	store := NewStore()
	cfg := collectorFixture(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	restore := replaceCollectorReaders(t)
	readCgroupMemFn = func(root, svc string) (int64, error) {
		return 0, errors.New("boom")
	}
	defer restore()

	Collect(store, cfg, logger)
	snap := store.Snapshot()

	// R-FGYB-VJ2B
	if got := onlyValue(t, snap, SeriesServiceMem("crm")); got != 0 {
		t.Fatalf("failed service memory sample = %d, want 0", got)
	}
	if logText := logs.String(); !strings.Contains(logText, "collect service memory") || !strings.Contains(logText, "boom") {
		t.Fatalf("logs = %q, want service memory error with boom", logText)
	}
	if got := onlyValue(t, snap, SeriesSystemMem); got != 1234*1024 {
		t.Fatalf("system memory sample = %d, want %d", got, 1234*1024)
	}
	if got := onlyValue(t, snap, SeriesServiceDisk("crm")); got != 5 {
		t.Fatalf("service disk sample = %d, want 5", got)
	}
}

func collectorFixture(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	manifestRoot := filepath.Join(root, "manifests")
	writeManifest(t, manifestRoot, "crm", "APP=crm\nMCP=true\nMOUNT=/crm/\nPORT=12001\n")
	memInfoPath := filepath.Join(root, "meminfo")
	if err := os.WriteFile(memInfoPath, []byte("MemTotal: 8000 kB\nMemAvailable: 1234 kB\n"), 0o644); err != nil {
		t.Fatalf("write meminfo fixture: %v", err)
	}
	cgroupDir := filepath.Join(root, "cgroup", "system.slice", "crm.service")
	if err := os.MkdirAll(cgroupDir, 0o755); err != nil {
		t.Fatalf("create cgroup fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgroupDir, "memory.current"), []byte("77\n"), 0o644); err != nil {
		t.Fatalf("write cgroup fixture: %v", err)
	}
	diskPath := filepath.Join(root, "opt")
	svcDir := filepath.Join(diskPath, "crm")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatalf("create service disk fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, "data.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("write service disk fixture: %v", err)
	}
	return Config{
		ManifestRoot: manifestRoot,
		CgroupRoot:   filepath.Join(root, "cgroup"),
		DiskPath:     diskPath,
		MemInfoPath:  memInfoPath,
	}
}

func waitForSamples(t *testing.T, store *Store, want int, keys ...string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		snap := store.Snapshot()
		allReady := true
		for _, key := range keys {
			if len(snap.Series[key]) < want {
				allReady = false
				break
			}
		}
		if allReady {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d samples on %v; snapshot = %#v", want, keys, snap.Series)
		case <-tick.C:
		}
	}
}

func onlyValue(t *testing.T, snap Snapshot, key string) int64 {
	t.Helper()
	samples := snap.Series[key]
	if len(samples) != 1 {
		t.Fatalf("%s samples length = %d, want 1", key, len(samples))
	}
	return samples[0].Value
}

func waitRunDone(done <-chan error) error {
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		return errors.New("Run did not return after context cancellation")
	}
}

func replaceCollectorReaders(t *testing.T) func() {
	t.Helper()
	oldServices := servicesFn
	oldReadDiskFree := readDiskFreeFn
	oldReadCgroupMem := readCgroupMemFn
	oldDirSize := dirSizeFn
	return func() {
		servicesFn = oldServices
		readDiskFreeFn = oldReadDiskFree
		readCgroupMemFn = oldReadCgroupMem
		dirSizeFn = oldDirSize
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
}

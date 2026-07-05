package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	SeriesSystemMem  = "system.mem"
	SeriesSystemDisk = "system.disk"
)

var (
	servicesFn      = services
	readDiskFreeFn  = readDiskFree
	readCgroupMemFn = readCgroupMem
	dirSizeFn       = dirSize
)

// Config carries the injectable roots/interval so tests can drive collection
// against fixtures while production uses the on-box paths.
type Config struct {
	ManifestRoot string
	CgroupRoot   string
	DiskPath     string
	MemInfoPath  string
	Interval     time.Duration
}

// SeriesServiceMem returns the per-service cgroup memory series key.
func SeriesServiceMem(svc string) string {
	return "service." + svc + ".mem"
}

// SeriesServiceDisk returns the per-service /opt/<svc> disk usage series key.
func SeriesServiceDisk(svc string) string {
	return "service." + svc + ".disk"
}

// Collect takes one sample set and records 0 for unavailable or failed sources.
// Unexpected source errors are logged and do not abort sibling readings.
func Collect(store *Store, cfg Config, logger *slog.Logger) {
	if store == nil {
		return
	}
	cfg = cfg.withDefaults()
	logger = loggerOrDiscard(logger)
	now := time.Now()

	memAvail, memTotal := collectMem(cfg.MemInfoPath, logger)
	diskFree, diskTotal, err := readDiskFreeFn(cfg.DiskPath)
	if err != nil {
		logger.Warn("collect system disk", "path", cfg.DiskPath, "err", err)
		diskFree, diskTotal = 0, 0
	}
	store.SetCapacities(memTotal, diskTotal)
	store.Append(SeriesSystemMem, Sample{At: now, Value: memAvail})
	store.Append(SeriesSystemDisk, Sample{At: now, Value: diskFree})

	svcs, err := servicesFn(cfg.ManifestRoot)
	if err != nil {
		logger.Warn("collect service list", "manifest_root", cfg.ManifestRoot, "err", err)
		return
	}
	for _, svc := range svcs {
		mem, err := readCgroupMemFn(cfg.CgroupRoot, svc)
		if err != nil {
			logger.Warn("collect service memory", "service", svc, "err", err)
			mem = 0
		}
		store.Append(SeriesServiceMem(svc), Sample{At: now, Value: mem})

		disk, err := dirSizeFn(filepath.Join(cfg.DiskPath, svc))
		if err != nil {
			logger.Warn("collect service disk", "service", svc, "err", err)
			disk = 0
		}
		store.Append(SeriesServiceDisk(svc), Sample{At: now, Value: disk})
	}
}

// Run samples once immediately, then on each interval tick, until ctx is done.
func Run(ctx context.Context, store *Store, cfg Config, logger *slog.Logger) error {
	cfg = cfg.withDefaults()
	Collect(store, cfg, logger)
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			Collect(store, cfg, logger)
		}
	}
}

func (cfg Config) withDefaults() Config {
	if cfg.ManifestRoot == "" {
		cfg.ManifestRoot = "/opt"
	}
	if cfg.CgroupRoot == "" {
		cfg.CgroupRoot = "/sys/fs/cgroup"
	}
	if cfg.DiskPath == "" {
		cfg.DiskPath = "/opt"
	}
	if cfg.MemInfoPath == "" {
		cfg.MemInfoPath = "/proc/meminfo"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	return cfg
}

func collectMem(path string, logger *slog.Logger) (avail, total int64) {
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("collect system memory", "path", path, "err", err)
		}
		return 0, 0
	}
	defer f.Close()

	avail, total, err = readMemInfo(f)
	if err != nil {
		logger.Warn("collect system memory", "path", path, "err", err)
		return 0, 0
	}
	return avail, total
}

func loggerOrDiscard(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

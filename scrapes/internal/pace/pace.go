// Package pace regulates the writes of a single build phase.
package pace

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Pacing constants define the backoff floor, recovery increment and pressure polling.
const (
	FloorDivisor   = 16
	RestoreDivisor = 10
	PressurePoll   = 5 * time.Second
)

// Config supplies a phase's limits and time seams.
type Config struct {
	Budget   int64
	Limit    int64
	Pressure string
	Now      func() time.Time
	Sleep    func(ctx context.Context, d time.Duration)
}

// Phase measures and regulates a collect or load phase.
type Phase struct {
	cfg     Config
	started time.Time
	rate    int64
	bytes   int64
	paused  time.Duration
}

// Start begins a phase at its configured write budget.
func Start(cfg Config) *Phase {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
	}
	return &Phase{cfg: cfg, started: cfg.Now(), rate: cfg.Budget}
}

// Step paces the reported write and updates the next step's rate.
func (p *Phase) Step(ctx context.Context, n int64, took time.Duration) error {
	p.bytes += n
	if p.cfg.Budget >= 1 {
		expected := time.Duration(n * int64(time.Second) / p.rate)
		if took > expected {
			p.rate = max(p.rate/2, p.cfg.Budget/FloorDivisor, 1)
		} else {
			p.rate = min(p.cfg.Budget, p.rate+max(p.cfg.Budget/RestoreDivisor, 1))
		}
		if expected > took {
			if err := p.pause(ctx, expected-took); err != nil {
				return err
			}
		}
	}
	for p.cfg.Pressure != "" {
		pressure, ok := readPressure(p.cfg.Pressure)
		if !ok || pressure <= float64(p.cfg.Limit) {
			break
		}
		if err := p.pause(ctx, PressurePoll); err != nil {
			return err
		}
	}
	return nil
}

func (p *Phase) pause(ctx context.Context, d time.Duration) error {
	p.paused += d
	p.cfg.Sleep(ctx, d)
	return ctx.Err()
}

func readPressure(path string) (float64, bool) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return 0, false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "full" {
			continue
		}
		for _, field := range fields[1:] {
			if value, ok := strings.CutPrefix(field, "avg10="); ok {
				n, err := strconv.ParseFloat(value, 64)
				return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
			}
		}
	}
	return 0, false
}

// Totals reports write bytes, clock duration and requested pauses.
func (p *Phase) Totals() (bytes int64, elapsed, paused time.Duration) {
	return p.bytes, p.cfg.Now().Sub(p.started), p.paused
}

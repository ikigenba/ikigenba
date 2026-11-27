// Package sim runs the host daemon against the sandbox's cloud and handler.
package sim

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

// WatchInterval sets how often simulated instance state is checked.
const WatchInterval = 2 * time.Second

// ApplyDelay is how long a simulated machine takes to apply desired state.
const ApplyDelay = 3 * time.Second

// Config supplies the sandbox cloud, handler, and the simulator's clock and timers.
type Config struct {
	Cloud      *memory.Cloud
	Handler    http.Handler
	Binding    string
	Digest     func() string
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration)
	After      func(time.Duration) <-chan time.Time
	BanFile    string
	BoxDir     string
	SeedFile   string
	BackupFile string
}

// Sim supervises the sandbox's simulated hosts.
type Sim struct{ cfg Config }

// New constructs a simulator.
func New(cfg Config) *Sim { return &Sim{cfg: cfg} }

type loop struct {
	cancel  context.CancelFunc
	done    <-chan struct{}
	machine *machine.Memory
	box     *hostListener
}

// Run watches instances and joins all their loops when ctx ends.
func (s *Sim) Run(ctx context.Context) error {
	loops := make(map[string]loop)
	disks := machine.NewDisks()
	var boxDir string
	if s.cfg.BoxDir != "" {
		boxDir, _ = filepath.Abs(s.cfg.BoxDir)
	}
	defer func() {
		for _, l := range loops {
			l.stop()
		}
		for _, l := range loops {
			<-l.done
		}
		removeBoxListing(boxDir)
	}()
	for ctx.Err() == nil {
		if err := s.look(ctx, loops, boxDir, disks); err != nil && ctx.Err() == nil {
			return err
		}
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.cfg.After(WatchInterval):
		}
	}
	return nil
}

func (s *Sim) look(ctx context.Context, loops map[string]loop, boxDir string, disks *machine.Disks) error {
	running := make(map[string]string)
	tags := cloud.Tags{cloud.TagKind: cloud.KindHost}
	err := s.cfg.Cloud.FindInstances(ctx, tags, func(page []cloud.Instance) bool {
		for _, i := range page {
			if i.State == cloud.InstanceRunning {
				running[i.ID] = i.Tags[cloud.TagHost]
			}
		}
		return ctx.Err() == nil
	})
	if err != nil {
		return err
	}
	for id, l := range loops {
		select {
		case <-l.done:
			l.cancel()
			delete(loops, id)
		default:
			if _, up := running[id]; !up {
				l.stop()
			}
		}
	}
	for id := range running {
		if ctx.Err() != nil {
			break
		}
		if _, exists := loops[id]; !exists {
			loops[id] = s.start(ctx, id, boxDir, disks)
		}
	}
	s.consumeDrops(running, loops)
	if boxDir != "" {
		updateBoxSockets(boxDir, running, loops)
	}
	if s.cfg.SeedFile != "" {
		updateSeedListing(s.cfg.SeedFile, running, loops)
	}
	return nil
}

func (l loop) stop() {
	l.cancel()
	if l.box != nil {
		_ = l.box.Close()
	}
}

func (s *Sim) start(ctx context.Context, id, boxDir string, disks *machine.Disks) loop {
	cfg := s.cfg
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	m := machine.NewMemory(machine.MemoryConfig{
		ApplyDelay: ApplyDelay, Sleep: cfg.Sleep, Now: cfg.Now, Disks: disks, Put: cfg.Cloud.Put,
	})
	d := daemon.Config{
		BaseURL:   "http://" + cfg.Binding,
		HTTP:      &http.Client{Transport: transport{handler: cfg.Handler, binding: cfg.Binding}},
		Presigner: cfg.Cloud.Presigner(id),
		Metadata:  cfg.Cloud.Metadata(id),
		Machine:   m,
		Now:       cfg.Now,
		Sleep:     cfg.Sleep,
		Stderr:    io.Discard,
		After:     cfg.After,
	}
	var box *hostListener
	if cfg.BoxDir != "" {
		box = newHostListener(boxDir)
		d.Box, d.PeerUID = box, boxUID
	}
	go func() {
		defer close(done)
		if box != nil {
			defer func() { _ = box.Close() }()
		}
		if loopCtx.Err() != nil {
			return
		}
		d.Digest = cfg.Digest()
		if loopCtx.Err() == nil {
			_ = daemon.Run(loopCtx, d)
		}
	}()
	return loop{cancel: cancel, done: done, machine: m, box: box}
}

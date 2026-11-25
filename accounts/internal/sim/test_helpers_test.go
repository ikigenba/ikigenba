package sim

import (
	"math/rand/v2"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

type simClock struct {
	elapsed atomic.Int64
}

func (clock *simClock) advance(d time.Duration) { clock.elapsed.Add(int64(d)) }

func (clock *simClock) now() time.Time {
	return time.Date(2026, 10, 1, 3, 4, 5, 0, time.UTC).Add(time.Duration(clock.elapsed.Load()))
}

func sandboxCloud(t *testing.T, clock *simClock) (*memory.Cloud, cloud.Instance) {
	t.Helper()
	hosts := settings.SandboxHosts
	c, err := memory.New(memory.Config{
		Path: filepath.Join(t.TempDir(), "sandbox-cloud.json"),
		Now:  clock.now, Rand: rand.NewChaCha8([32]byte{13}), Hosts: hosts,
	})
	must(t, err)
	return c, launchSandboxHost(t, c, "host01")
}

func launchSandboxHost(t *testing.T, c *memory.Cloud, name string) cloud.Instance {
	t.Helper()
	hosts := settings.SandboxHosts
	i, err := c.RunInstance(t.Context(), cloud.Launch{
		Template: hosts.LaunchTemplate, InstanceProfile: hosts.InstanceProfile, Type: "t4g.medium", RootGB: 20,
		Tags: cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagHost: name},
	})
	must(t, err)
	return i
}

func watchTimer(t *testing.T, watchers chan<- chan time.Time) func(time.Duration) <-chan time.Time {
	t.Helper()
	return func(d time.Duration) <-chan time.Time {
		if d != WatchInterval {
			t.Errorf("watch delay = %v, want %v", d, WatchInterval)
		}
		tick := make(chan time.Time, 1)
		watchers <- tick
		return tick
	}
}

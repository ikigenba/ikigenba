// Package scrapers contains the fixed scraper registry.
package scrapers

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scrapes/internal/fmcsa"
	"github.com/ikigenba/ikigenba/scrapes/internal/mnparcels"
	"github.com/ikigenba/ikigenba/scrapes/internal/ppp"
	"github.com/ikigenba/ikigenba/scrapes/internal/settings"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Config supplies runtime hooks and source scope.
type Config struct {
	HTTP      *http.Client
	Telemetry *telemetry.Writer
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	UserAgent string
	Settings  settings.Settings
}

// Registry indexes the fixed scrapers by name.
type Registry struct{ list map[string]source.Scraper }

// New constructs a scraper with the configured runtime hooks.
func New(list ...source.Scraper) *Registry {
	r := &Registry{list: make(map[string]source.Scraper)}
	for _, sc := range list {
		n := sc.Name()
		if _, ok := r.list[n]; !ok {
			r.list[n] = sc
		}
	}
	return r
}

// Default constructs the service registry with the fixed scrapers.
func Default(cfg Config) *Registry {
	return New(
		fmcsa.New(fmcsa.Config{HTTP: cfg.HTTP, Telemetry: cfg.Telemetry, Now: cfg.Now, Sleep: cfg.Sleep, UserAgent: cfg.UserAgent, States: cfg.Settings.FMCSAStates, MinPowerUnits: cfg.Settings.FMCSAMinPowerUnits, Status: cfg.Settings.FMCSAStatus}),
		mnparcels.New(mnparcels.Config{HTTP: cfg.HTTP, Telemetry: cfg.Telemetry, Now: cfg.Now, Sleep: cfg.Sleep, UserAgent: cfg.UserAgent, Counties: cfg.Settings.MNParcelsCounties}),
		ppp.New(ppp.Config{HTTP: cfg.HTTP, Telemetry: cfg.Telemetry, Now: cfg.Now, Sleep: cfg.Sleep, UserAgent: cfg.UserAgent, Files: cfg.Settings.PPPFiles, States: cfg.Settings.PPPStates}),
	)
}

// Names trims and deduplicates names while preserving their order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.list))
	for n := range r.list {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Get looks up a scraper by name.
func (r *Registry) Get(name string) (source.Scraper, bool) { sc, ok := r.list[name]; return sc, ok }

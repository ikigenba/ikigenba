package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/backup"
	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
)

const initUsage = `Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  litestream                 found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  certificate  obtain the host's certificate, or renew it if it is due
  nginx.conf   generate /etc/nginx/conf.d/ikigenba.conf and reload nginx
  litestream   generate /etc/litestream.yml and enable litestream.service
  timers       write the backup and renewal units, enabling each backup timer
               whose period is set and the renewal timer always

Configuration keys:
  host.name  the fully-qualified name this host answers at, at or under a configured zone
`

func runInit(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return writeOut(stdout, initUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) != 0 {
		return writeInitUsageError(stderr, "init takes no arguments")
	}

	entries, err := (config.Store{Root: deps.Root}).List()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return initConfigErr(stderr, deps, err)
	}
	return runInitPreflight(stdout, stderr, deps, entries)
}

func initConfigErr(stderr io.Writer, deps Deps, err error) exitCode {
	if errors.Is(err, config.ErrCorrupt) {
		path := filepath.Join(deps.Root, filepath.FromSlash(strings.TrimPrefix(config.Dir, "/")), config.FileName)
		writeDiagnostic(stderr, errors.New(diagnosticArg(path)+" is corrupt"))
		return exitFail
	}
	return configErr(stderr, err)
}

func writeInitUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl init --help' for usage\n")
	return exitUsage
}

// runInitPreflight is the boundary between command handling and the checks.
// The preflight implementation consumes the configuration snapshot loaded
// before any output, so a store read failure can never produce partial stdout.
func runInitPreflight(stdout, stderr io.Writer, deps Deps, entries []config.Entry) exitCode {
	preflight := initPreflight{
		deps:   deps,
		store:  config.Store{Root: deps.Root},
		values: make(map[string]string, len(entries)),
		allOK:  true,
	}
	for _, entry := range entries {
		preflight.values[entry.Key] = entry.Value
	}
	preflight.checkTools()
	if err := preflight.checkDNSConfig(); err != nil {
		return initConfigErr(stderr, deps, err)
	}
	preflight.checkHostConfig()
	preflight.checkZones()
	preflight.checkHostZone()
	preflight.checkWildcard()
	return preflight.finish(stdout, stderr)
}

type initPreflight struct {
	deps       Deps
	store      config.Store
	values     map[string]string
	output     strings.Builder
	client     *dns.Client
	zones      []dns.Zone
	opened     dns.Provider
	provider   string
	host       string
	providerOK bool
	zonesOK    bool
	hostOK     bool
	allOK      bool
}

func (p *initPreflight) checkTools() {
	for _, name := range []string{"nginx", "certbot", "systemctl", "litestream"} {
		path, err := p.deps.lookPath(name)
		if err != nil {
			_, _ = fmt.Fprintf(&p.output, "%s: failed: not found on PATH\n", name)
			p.allOK = false
			continue
		}
		_, _ = fmt.Fprintf(&p.output, "%s: ok (%s)\n", name, diagnosticArg(path))
	}
}

func (p *initPreflight) checkDNSConfig() error {
	p.provider = p.values[dns.KeyProvider]
	zonesValue := p.values[dns.KeyZones]
	providerSet := p.provider != ""
	zonesSet := zonesValue != ""
	var openErr error
	if providerSet {
		if p.deps.DNS.Open == nil {
			openErr = fmt.Errorf("%w: %q", dns.ErrUnknownProvider, p.provider)
		} else {
			p.opened, openErr = p.deps.DNS.Open(context.Background(), p.provider)
		}
	}
	switch {
	case !providerSet:
		p.output.WriteString("dns.provider: failed: not set\n")
		p.allOK = false
	case openErr != nil:
		_, _ = fmt.Fprintf(&p.output, "dns.provider: failed: %s\n", diagnosticArg(openErr.Error()))
		p.allOK = false
	default:
		_, _ = fmt.Fprintf(&p.output, "dns.provider: ok (%s)\n", diagnosticArg(p.provider))
		p.providerOK = true
	}
	parsedZones, malformedZone, malformed := parseInitZones(zonesValue)
	p.zones = parsedZones
	switch {
	case !zonesSet:
		p.output.WriteString("dns.zones: failed: not set\n")
		p.allOK = false
	case malformed:
		_, _ = fmt.Fprintf(&p.output, "dns.zones: failed: dns.zones malformed: %q\n", malformedZone)
		p.allOK = false
	default:
		names := make([]string, len(p.zones))
		for i, zone := range p.zones {
			names[i] = diagnosticArg(zone.Name)
		}
		_, _ = fmt.Fprintf(&p.output, "dns.zones: ok (%s)\n", strings.Join(names, ","))
		p.zonesOK = true
	}
	if p.providerOK && p.zonesOK {
		p.client, openErr = dns.Open(context.Background(), p.store, dns.Env{
			Open:     func(context.Context, string) (dns.Provider, error) { return p.opened, nil },
			LookupNS: p.deps.DNS.LookupNS,
		})
		if openErr != nil {
			return openErr
		}
	}
	return nil
}

func (p *initPreflight) checkHostConfig() {
	hostValue := p.values["host.name"]
	p.host = normaliseInitName(hostValue)
	p.hostOK = hostValue != ""
	if !p.hostOK {
		p.output.WriteString("host.name: failed: not set\n")
		p.allOK = false
	} else {
		_, _ = fmt.Fprintf(&p.output, "host.name: ok (%s)\n", diagnosticArg(p.host))
	}
}

func (p *initPreflight) checkZones() {
	if !p.providerOK || !p.zonesOK || p.client == nil {
		return
	}
	var report strings.Builder
	if dnsCheck(&report, p.client, p.provider) != exitOK {
		p.allOK = false
	}
	for line := range strings.Lines(report.String()) {
		p.output.WriteString("zone ")
		p.output.WriteString(line)
	}
}

func (p *initPreflight) checkHostZone() {
	if !p.zonesOK || !p.hostOK {
		return
	}
	zone, err := (&dns.Client{Zones: p.zones}).ZoneFor(p.host)
	hostName := diagnosticArg(p.host)
	switch {
	case errors.Is(err, dns.ErrNoZone):
		_, _ = fmt.Fprintf(&p.output, "host %s: failed: no configured zone contains it\n", hostName)
		p.allOK = false
	case err != nil:
		_, _ = fmt.Fprintf(&p.output, "host %s: failed: %s\n", hostName, diagnosticArg(err.Error()))
		p.allOK = false
	default:
		_, _ = fmt.Fprintf(&p.output, "host %s: ok (zone %s)\n",
			hostName, diagnosticArg(zone.Name))
	}
}

func (p *initPreflight) checkWildcard() {
	if !p.hostOK {
		return
	}
	probe := "_opsctl-preflight." + p.host
	addresses, addressErr := p.deps.lookupHost(context.Background(), p.host)
	probeAddresses, probeErr := p.deps.lookupHost(context.Background(), probe)
	left, leftErr := renderInitAddresses(addresses)
	if addressErr == nil {
		addressErr = leftErr
	}
	right, rightErr := renderInitAddresses(probeAddresses)
	if probeErr == nil {
		probeErr = rightErr
	}
	hostName := diagnosticArg(p.host)
	switch {
	case addressErr != nil:
		_, _ = fmt.Fprintf(&p.output, "wildcard %s: failed: %s\n", hostName, diagnosticArg(addressErr.Error()))
		p.allOK = false
	case probeErr != nil:
		_, _ = fmt.Fprintf(&p.output, "wildcard %s: failed: %s\n", hostName, diagnosticArg(probeErr.Error()))
		p.allOK = false
	default:
		p.compareWildcard(probe, left, right)
	}
}

func (p *initPreflight) compareWildcard(probe, left, right string) {
	hostName := diagnosticArg(p.host)
	probeName := diagnosticArg(probe)
	if left == "" || left != right {
		_, _ = fmt.Fprintf(&p.output, "wildcard %s: failed: %s resolves to %s but %s resolves to %s\n",
			hostName, hostName, left, probeName, right)
		p.allOK = false
		return
	}
	_, _ = fmt.Fprintf(&p.output, "wildcard %s: ok (%s)\n", hostName, left)
}

func (p *initPreflight) finish(stdout, stderr io.Writer) exitCode {
	if code := writeOut(stdout, p.output.String()); code != exitOK {
		return code
	}
	if !p.allOK {
		return exitUsage
	}
	ctx := context.Background()
	env := host.Env{Root: p.deps.Root, Getenv: p.deps.Getenv, Execute: p.deps.Execute, Now: p.deps.Now}
	email, err := p.store.Get("acme.email")
	if errors.Is(err, config.ErrNotSet) {
		email = ""
		err = nil
	}
	apexApp := ""
	if err == nil {
		apexApp, err = p.store.Get("host.apex")
		if errors.Is(err, config.ErrNotSet) {
			apexApp = ""
			err = nil
		}
	}
	if err == nil {
		err = cert.Obtain(ctx, env, p.host, email, apexApp != "")
	}
	if err == nil {
		err = nginx.Apply(ctx, env, p.host, apexApp)
	}
	if err == nil {
		err = backup.SetupReplication(ctx, env, p.store)
	}
	if err == nil {
		err = backup.SetupTimers(ctx, env, p.store)
	}
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	return exitOK
}

func safeInitToken(value string) string {
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(value)
}

func parseInitZones(value string) ([]dns.Zone, string, bool) {
	entries := strings.Split(value, ",")
	zones := make([]dns.Zone, 0, len(entries))
	for _, entry := range entries {
		name, id, ok := strings.Cut(entry, ":")
		name = normaliseInitName(strings.TrimSpace(name))
		id = strings.TrimSpace(id)
		if !ok || name == "" || id == "" {
			return nil, entry, true
		}
		zones = append(zones, dns.Zone{Name: name, ID: id})
	}
	return zones, "", false
}

func normaliseInitName(name string) string {
	return strings.TrimSuffix(strings.ToLower(name), ".")
}

func uniqueSorted(values []string) []string {
	values = append([]string(nil), values...)
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func renderInitAddresses(values []string) (string, error) {
	canonical := make([]string, len(values))
	for i, value := range values {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return "", fmt.Errorf("invalid address %q", safeInitToken(value))
		}
		canonical[i] = address.Unmap().String()
	}
	return strings.Join(uniqueSorted(canonical), ","), nil
}

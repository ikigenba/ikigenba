package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

const initUsage = `Usage: opsctl init

Run the setup sequence behind one preflight. Every check is evaluated and
reported, one line per check, before anything runs; when any check fails,
nothing runs and init exits 2. Safe to re-run.

Checks, in order:
  nginx, certbot, systemctl  each found on PATH
  dns.provider, dns.zones    set, and the provider opens (see 'opsctl dns --help')
  host.name                  set
  zone NAME                  every configured zone is reachable and delegated
  host NAME                  host.name lies at or under a configured zone
  wildcard NAME              host.name and _opsctl-preflight.host.name resolve alike

Sequence:
  none yet; each setup command adds itself here when it is designed

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
		return configErr(stderr, err)
	}
	return runInitPreflight(stdout, deps, entries)
}

func writeInitUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl init --help' for usage\n")
	return exitUsage
}

// runInitPreflight is the boundary between command handling and the checks.
// The preflight implementation consumes the configuration snapshot loaded
// before any output, so a corrupt store can never produce partial stdout.
func runInitPreflight(stdout io.Writer, deps Deps, entries []config.Entry) exitCode {
	preflight := initPreflight{
		deps:   deps,
		values: make(map[string]string, len(entries)),
		allOK:  true,
	}
	for _, entry := range entries {
		preflight.values[entry.Key] = safeInitToken(entry.Value)
	}
	preflight.checkTools()
	preflight.checkDNSConfig()
	preflight.checkHostConfig()
	preflight.checkZones()
	preflight.checkHostZone()
	preflight.checkWildcard()
	return preflight.finish(stdout)
}

type initPreflight struct {
	deps       Deps
	values     map[string]string
	output     strings.Builder
	client     *dns.Client
	provider   string
	host       string
	providerOK bool
	zonesOK    bool
	hostOK     bool
	allOK      bool
}

func (p *initPreflight) checkTools() {
	for _, name := range []string{"nginx", "certbot", "systemctl"} {
		path, err := p.deps.lookPath(name)
		if err != nil {
			_, _ = fmt.Fprintf(&p.output, "%s: failed: not found on PATH\n", name)
			p.allOK = false
			continue
		}
		_, _ = fmt.Fprintf(&p.output, "%s: ok (%s)\n", name, path)
	}
}

func (p *initPreflight) checkDNSConfig() {
	p.provider = p.values[dns.KeyProvider]
	zonesValue := p.values[dns.KeyZones]
	providerSet := p.provider != ""
	zonesSet := zonesValue != ""
	var openErr error
	if providerSet && zonesSet {
		p.client, openErr = dns.Open(context.Background(), config.Store{Root: p.deps.Root}, p.deps.DNS)
	}
	switch {
	case !providerSet:
		p.output.WriteString("dns.provider: failed: not set\n")
		p.allOK = false
	case openErr != nil && !errors.Is(openErr, dns.ErrNotConfigured):
		_, _ = fmt.Fprintf(&p.output, "dns.provider: failed: %v\n", openErr)
		p.allOK = false
	default:
		_, _ = fmt.Fprintf(&p.output, "dns.provider: ok (%s)\n", p.provider)
		p.providerOK = true
	}
	parsedZones, malformedZone, malformed := parseInitZones(zonesValue)
	switch {
	case !zonesSet:
		p.output.WriteString("dns.zones: failed: not set\n")
		p.allOK = false
	case providerSet && openErr != nil && errors.Is(openErr, dns.ErrNotConfigured):
		_, _ = fmt.Fprintf(&p.output, "dns.zones: failed: %v\n", openErr)
		p.allOK = false
	case malformed:
		_, _ = fmt.Fprintf(&p.output, "dns.zones: failed: dns.zones malformed: %q\n", malformedZone)
		p.allOK = false
	default:
		if p.client != nil {
			for i := range p.client.Zones {
				p.client.Zones[i].Name = safeInitToken(p.client.Zones[i].Name)
				p.client.Zones[i].ID = safeInitToken(p.client.Zones[i].ID)
			}
			parsedZones = p.client.Zones
		}
		names := make([]string, len(parsedZones))
		for i, zone := range parsedZones {
			names[i] = zone.Name
		}
		_, _ = fmt.Fprintf(&p.output, "dns.zones: ok (%s)\n", strings.Join(names, ","))
		p.zonesOK = true
	}
}

func (p *initPreflight) checkHostConfig() {
	hostValue := p.values["host.name"]
	p.host = normaliseInitName(hostValue)
	p.hostOK = hostValue != ""
	if !p.hostOK {
		p.output.WriteString("host.name: failed: not set\n")
		p.allOK = false
	} else {
		_, _ = fmt.Fprintf(&p.output, "host.name: ok (%s)\n", p.host)
	}
}

func (p *initPreflight) checkZones() {
	if !p.providerOK || !p.zonesOK {
		return
	}
	for _, zone := range p.client.Zones {
		result, err := p.client.Check(context.Background(), zone)
		switch {
		case err != nil:
			_, _ = fmt.Fprintf(&p.output, "zone %s: failed: %v\n", zone.Name, err)
			p.allOK = false
		case result.ZoneName != zone.Name:
			_, _ = fmt.Fprintf(&p.output, "zone %s: failed: provider reports zone %s\n", zone.Name, safeInitToken(result.ZoneName))
			p.allOK = false
		case !result.Delegated:
			_, _ = fmt.Fprintf(&p.output, "zone %s: failed: nameservers are not delegated\n", zone.Name)
			p.allOK = false
		default:
			_, _ = fmt.Fprintf(&p.output, "zone %s: ok (%s %s, %d nameservers delegated)\n",
				zone.Name, p.provider, zone.ID, len(result.Nameservers))
		}
	}
}

func (p *initPreflight) checkHostZone() {
	if !p.providerOK || !p.zonesOK || !p.hostOK {
		return
	}
	zone, err := p.client.ZoneFor(p.host)
	switch {
	case errors.Is(err, dns.ErrNoZone):
		_, _ = fmt.Fprintf(&p.output, "host %s: failed: no configured zone contains it\n", p.host)
		p.allOK = false
	case err != nil:
		_, _ = fmt.Fprintf(&p.output, "host %s: failed: %v\n", p.host, err)
		p.allOK = false
	default:
		_, _ = fmt.Fprintf(&p.output, "host %s: ok (zone %s)\n", p.host, zone.Name)
	}
}

func (p *initPreflight) checkWildcard() {
	if !p.hostOK {
		return
	}
	probe := "_opsctl-preflight." + p.host
	addresses, addressErr := p.deps.lookupHost(context.Background(), p.host)
	probeAddresses, probeErr := p.deps.lookupHost(context.Background(), probe)
	switch {
	case addressErr != nil:
		_, _ = fmt.Fprintf(&p.output, "wildcard %s: failed: %v\n", p.host, addressErr)
		p.allOK = false
	case probeErr != nil:
		_, _ = fmt.Fprintf(&p.output, "wildcard %s: failed: %v\n", p.host, probeErr)
		p.allOK = false
	default:
		p.compareWildcard(probe, addresses, probeAddresses)
	}
}

func (p *initPreflight) compareWildcard(probe string, addresses, probeAddresses []string) {
	left := renderInitAddresses(addresses)
	right := renderInitAddresses(probeAddresses)
	if left != right {
		_, _ = fmt.Fprintf(&p.output, "wildcard %s: failed: %s resolves to %s but %s resolves to %s\n",
			p.host, p.host, left, probe, right)
		p.allOK = false
		return
	}
	_, _ = fmt.Fprintf(&p.output, "wildcard %s: ok (%s)\n", p.host, left)
}

func (p *initPreflight) finish(stdout io.Writer) exitCode {
	if code := writeOut(stdout, p.output.String()); code != exitOK {
		return code
	}
	if !p.allOK {
		return exitUsage
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
		name = strings.TrimSpace(name)
		id = strings.TrimSpace(id)
		if !ok || name == "" || id == "" {
			return nil, entry, true
		}
		zones = append(zones, dns.Zone{Name: normaliseInitName(name), ID: id})
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

func renderInitAddresses(values []string) string {
	values = uniqueSorted(values)
	for i := range values {
		values[i] = safeInitToken(values[i])
	}
	return strings.Join(values, ",")
}

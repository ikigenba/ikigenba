package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/dns"
)

const dnsUsage = `Usage: opsctl dns <subcommand> [options] [arguments]

Manage DNS records in the zones opsctl owns, through the configured provider.

Subcommands:
  list ZONE               print every record in ZONE, one line per value
  add NAME TYPE VALUE     add VALUE to the record NAME/TYPE, creating it if absent
  remove NAME TYPE VALUE  remove VALUE from the record NAME/TYPE; succeeds if absent
  check                   verify every configured zone is reachable and delegated
  acme-auth               certbot --manual-auth-hook: add the DNS-01 challenge record
  acme-cleanup            certbot --manual-cleanup-hook: remove the challenge record

Options (add, remove, acme-auth, acme-cleanup):
  --timeout DURATION  how long to wait for the change to be live (default 2m)
  --ttl SECONDS       TTL when add creates a record (default 300; acme-auth uses 60)

Configuration keys:
  dns.provider                the active provider; only 'route53' is supported
  dns.zones                   comma-separated zones opsctl owns
  dns.<provider>.zone.<zone>  the provider's id for <zone>

NAME is mapped to a zone by longest suffix match against dns.zones.
`

func runDNS(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if isCommandHelp(args) {
		return writeOut(stdout, dnsUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	if len(args) == 0 {
		return writeDNSUsageError(stderr, "no dns subcommand given")
	}

	subcommand, ok := dnsSubcommandFor(args[0])
	if !ok {
		return writeDNSUsageError(stderr, "unknown dns subcommand '"+diagnosticArg(subcommand.name)+"'")
	}

	store := config.Store{Root: deps.Root}
	client, err := dns.Open(context.Background(), store, deps.DNS)
	if err != nil {
		return dnsOpenError(stderr, err)
	}

	command := dnsCommand{stdout: stdout, stderr: stderr, deps: deps, store: store, client: client}
	return subcommand.run(args[1:], command)
}

type dnsSubcommand struct {
	name string
	run  func(args []string, command dnsCommand) exitCode
}

type dnsCommand struct {
	stdout io.Writer
	stderr io.Writer
	deps   Deps
	store  config.Store
	client *dns.Client
}

func dnsSubcommandFor(name string) (dnsSubcommand, bool) {
	switch name {
	case "list":
		return dnsSubcommand{name: name, run: runDNSList}, true
	case "add":
		return dnsSubcommand{name: name, run: runDNSAdd}, true
	case "remove":
		return dnsSubcommand{name: name, run: runDNSRemove}, true
	case "check":
		return dnsSubcommand{name: name, run: runDNSCheck}, true
	case "acme-auth":
		return dnsSubcommand{name: name, run: runDNSACMEAuth}, true
	case "acme-cleanup":
		return dnsSubcommand{name: name, run: runDNSACMECleanup}, true
	default:
		return dnsSubcommand{name: name}, false
	}
}

func runDNSList(args []string, command dnsCommand) exitCode {
	return dnsList(args, command.stdout, command.stderr, command.client)
}

func runDNSAdd(args []string, command dnsCommand) exitCode {
	return dnsChange(args, command.stderr, command.client, true)
}

func runDNSRemove(args []string, command dnsCommand) exitCode {
	return dnsChange(args, command.stderr, command.client, false)
}

func runDNSCheck(args []string, command dnsCommand) exitCode {
	provider, err := command.store.Get(dns.KeyProvider)
	if err != nil {
		return dnsError(command.stderr, err)
	}
	return dnsCheck(args, command.stdout, command.stderr, command.client, provider)
}

func runDNSACMEAuth(args []string, command dnsCommand) exitCode {
	return dnsACME(args, command.stderr, command.client, command.deps, true)
}

func runDNSACMECleanup(args []string, command dnsCommand) exitCode {
	return dnsACME(args, command.stderr, command.client, command.deps, false)
}

func writeDNSUsageError(stderr io.Writer, message string) exitCode {
	_, _ = io.WriteString(stderr, "opsctl: "+message+"\n\nsee 'opsctl dns --help' for usage\n")
	return exitUsage
}

func dnsOpenError(stderr io.Writer, err error) exitCode {
	if errors.Is(err, dns.ErrNotConfigured) {
		key := dnsConfigKey(err.Error())
		_, _ = fmt.Fprintf(stderr, "opsctl: %s not set\n", key)
		return exitFail
	}
	return dnsError(stderr, err)
}

func dnsConfigKey(message string) string {
	for _, field := range strings.Fields(message) {
		candidate := strings.Trim(field, "'\"(),:;")
		if strings.HasPrefix(candidate, "dns.") {
			return candidate
		}
	}
	return "dns configuration"
}

func dnsError(stderr io.Writer, err error) exitCode {
	_, _ = fmt.Fprintf(stderr, "opsctl: %v\n", err)
	return exitFail
}

type dnsRecordLine struct {
	name  string
	typ   string
	ttl   int
	value string
}

func dnsList(args []string, stdout, stderr io.Writer, client *dns.Client) exitCode {
	if len(args) != 1 {
		return writeDNSUsageError(stderr, "dns list requires ZONE")
	}
	zone, ok := configuredZone(client.Zones, args[0])
	if !ok {
		return dnsListNoZone(stderr, args[0], client.Zones)
	}
	records, err := client.Records(context.Background(), zone)
	if err != nil {
		return dnsError(stderr, err)
	}
	return writeDNSRecordLines(stdout, flattenAndSortDNSRecords(records))
}

func flattenAndSortDNSRecords(records []dns.Record) []dnsRecordLine {
	var lines []dnsRecordLine
	for _, record := range records {
		for _, value := range record.Values {
			lines = append(lines, dnsRecordLine{record.Name, record.Type, record.TTL, value})
		}
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].name != lines[j].name {
			return lines[i].name < lines[j].name
		}
		if lines[i].typ != lines[j].typ {
			return lines[i].typ < lines[j].typ
		}
		return lines[i].value < lines[j].value
	})
	return lines
}

func writeDNSRecordLines(stdout io.Writer, lines []dnsRecordLine) exitCode {
	var output strings.Builder
	for _, item := range lines {
		_, _ = fmt.Fprintf(&output, "%s %s %d %s\n", item.name, item.typ, item.ttl, item.value)
	}
	return writeOut(stdout, output.String())
}

func configuredZone(zones []dns.Zone, name string) (dns.Zone, bool) {
	normalized := strings.ToLower(strings.TrimSuffix(name, "."))
	for _, zone := range zones {
		if zone.Name == normalized {
			return zone, true
		}
	}
	return dns.Zone{}, false
}

func dnsNoZone(stderr io.Writer, name string, zones []dns.Zone) exitCode {
	names := make([]string, len(zones))
	for i, zone := range zones {
		names[i] = zone.Name
	}
	_, _ = fmt.Fprintf(stderr, "opsctl: no configured zone contains '%s'\n\nconfigured zones: %s\n",
		diagnosticArg(name), strings.Join(names, ","))
	return exitUsage
}

func dnsListNoZone(stderr io.Writer, name string, zones []dns.Zone) exitCode {
	names := make([]string, len(zones))
	for i, zone := range zones {
		names[i] = zone.Name
	}
	_, _ = fmt.Fprintf(stderr, "opsctl: zone not configured: %s\n\nconfigured zones: %s\n",
		diagnosticArg(name), strings.Join(names, ","))
	return exitUsage
}

type dnsTimeout struct {
	duration time.Duration
	label    string
}

func (v *dnsTimeout) String() string {
	if v.label == "" {
		return "2m"
	}
	return v.label
}

func (v *dnsTimeout) Set(raw string) error {
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return errors.New("timeout must be a positive duration")
	}
	v.duration = duration
	v.label = raw
	return nil
}

type dnsChangeRequest struct {
	name    string
	typ     string
	value   string
	ttl     int
	timeout dnsTimeout
}

func dnsChange(args []string, stderr io.Writer, client *dns.Client, add bool) exitCode {
	request, code := parseDNSChange(args, stderr, add)
	if code != exitOK {
		return code
	}
	if code := checkDNSZone(stderr, client, request.name); code != exitOK {
		return code
	}
	return applyDNSChange(stderr, client, request, add)
}

func parseDNSChange(args []string, stderr io.Writer, add bool) (dnsChangeRequest, exitCode) {
	name := "remove"
	if add {
		name = "add"
	}
	fs := flag.NewFlagSet("opsctl dns "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := dnsTimeout{duration: 2 * time.Minute, label: "2m"}
	fs.Var(&timeout, "timeout", "")
	ttl := 300
	if add {
		fs.IntVar(&ttl, "ttl", 300, "")
	}
	if err := fs.Parse(args); err != nil {
		return dnsChangeRequest{}, writeDNSUsageError(stderr, "invalid dns "+name+" options")
	}
	if ttl <= 0 {
		return dnsChangeRequest{}, writeDNSUsageError(stderr, "--ttl must be a positive integer")
	}
	positionals := fs.Args()
	if len(positionals) != 3 {
		return dnsChangeRequest{}, writeDNSUsageError(stderr, "dns "+name+" requires NAME TYPE VALUE")
	}
	return dnsChangeRequest{
		name: positionals[0], typ: positionals[1], value: positionals[2], ttl: ttl, timeout: timeout,
	}, exitOK
}

func checkDNSZone(stderr io.Writer, client *dns.Client, name string) exitCode {
	if _, err := client.ZoneFor(name); err != nil {
		if errors.Is(err, dns.ErrNoZone) {
			return dnsNoZone(stderr, name, client.Zones)
		}
		return dnsError(stderr, err)
	}
	return exitOK
}

func applyDNSChange(stderr io.Writer, client *dns.Client, request dnsChangeRequest, add bool) exitCode {
	ctx, cancel := context.WithTimeout(context.Background(), request.timeout.duration)
	defer cancel()
	var err error
	if add {
		err = client.Add(ctx, request.name, request.typ, request.ttl, request.value)
	} else {
		err = client.Remove(ctx, request.name, request.typ, request.value)
	}
	return dnsChangeResult(stderr, err, request.name, request.timeout.label)
}

func dnsChangeResult(stderr io.Writer, err error, name, timeout string) exitCode {
	if err == nil {
		return exitOK
	}
	if errors.Is(err, context.DeadlineExceeded) {
		_, _ = fmt.Fprintf(stderr, "opsctl: change to %s not confirmed within %s\n",
			diagnosticArg(name), diagnosticArg(timeout))
		return exitFail
	}
	return dnsError(stderr, err)
}

func dnsCheck(args []string, stdout, stderr io.Writer, client *dns.Client, provider string) exitCode {
	if len(args) != 0 {
		return writeDNSUsageError(stderr, "dns check takes no arguments")
	}
	var output strings.Builder
	allOK := true
	for _, zone := range client.Zones {
		result, err := client.Check(context.Background(), zone)
		if err != nil {
			_, _ = fmt.Fprintf(&output, "%s: failed: %v\n", zone.Name, err)
			allOK = false
			continue
		}
		switch {
		case result.ZoneName != zone.Name:
			_, _ = fmt.Fprintf(&output, "%s: failed: provider reports zone %s\n", zone.Name, result.ZoneName)
			allOK = false
		case !result.Delegated:
			_, _ = fmt.Fprintf(&output, "%s: failed: nameservers are not delegated\n", zone.Name)
			allOK = false
		default:
			_, _ = fmt.Fprintf(&output, "%s: ok (%s %s, %d nameservers delegated)\n",
				zone.Name, provider, zone.ID, len(result.Nameservers))
		}
	}
	if code := writeOut(stdout, output.String()); code != exitOK {
		return code
	}
	if allOK {
		return exitOK
	}
	return exitFail
}

func dnsACME(args []string, stderr io.Writer, client *dns.Client, deps Deps, auth bool) exitCode {
	request, code := parseDNSACME(args, stderr, deps, auth)
	if code != exitOK {
		return code
	}
	if code := checkDNSZone(stderr, client, request.name); code != exitOK {
		return code
	}
	return applyDNSACME(stderr, client, request, auth)
}

type dnsACMERequest struct {
	name       string
	validation string
	timeout    dnsTimeout
}

func parseDNSACME(args []string, stderr io.Writer, deps Deps, auth bool) (dnsACMERequest, exitCode) {
	subcommand := "acme-cleanup"
	hook := "--manual-cleanup-hook"
	if auth {
		subcommand = "acme-auth"
		hook = "--manual-auth-hook"
	}
	fs := flag.NewFlagSet("opsctl dns "+subcommand, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := dnsTimeout{duration: 2 * time.Minute, label: "2m"}
	fs.Var(&timeout, "timeout", "")
	if err := fs.Parse(args); err != nil {
		return dnsACMERequest{}, writeDNSUsageError(stderr, "invalid dns "+subcommand+" options")
	}
	if len(fs.Args()) != 0 {
		return dnsACMERequest{}, writeDNSUsageError(stderr, "dns "+subcommand+" takes no arguments")
	}
	domain := deps.getenv("CERTBOT_DOMAIN")
	validation := deps.getenv("CERTBOT_VALIDATION")
	if domain == "" || validation == "" {
		_, _ = fmt.Fprintf(stderr, "opsctl: %s must be run by certbot as %s\n", subcommand, hook)
		return dnsACMERequest{}, exitUsage
	}
	return dnsACMERequest{
		name: "_acme-challenge." + domain, validation: validation, timeout: timeout,
	}, exitOK
}

func applyDNSACME(stderr io.Writer, client *dns.Client, request dnsACMERequest, auth bool) exitCode {
	ctx, cancel := context.WithTimeout(context.Background(), request.timeout.duration)
	defer cancel()
	var err error
	if auth {
		err = client.Add(ctx, request.name, "TXT", 60, request.validation)
	} else {
		err = client.Remove(ctx, request.name, "TXT", request.validation)
	}
	return dnsChangeResult(stderr, err, request.name, request.timeout.label)
}

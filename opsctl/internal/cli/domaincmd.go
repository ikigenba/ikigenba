package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/accounts"
	"github.com/ikigenba/ikigenba/opsctl/internal/cert"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/domain"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/nginx"
	"github.com/ikigenba/ikigenba/opsctl/internal/services"
)

const domainUsage = `Usage: opsctl domain <subcommand> [arguments]

Manage the host's custom domains: names outside host.name whose A record points
at this host. A primary name is served by the sites app, which decides the site;
a redirect name answers 301 to its primary. Each name has a certificate of its
own, obtained by HTTP-01 from /var/www/acme.

Subcommands:
  add NAME     obtain NAME's certificate, add NAME, and apply nginx
  remove NAME  remove NAME, apply nginx, and delete NAME's certificate; refused
               while a redirect name points at NAME
  list         print each name, and a redirect name's primary

Options (add):
  --redirect PRIMARY  make NAME redirect to PRIMARY, a listed primary name

Configuration keys:
  acme.email  the address the CA sends expiry warnings to
  host.name   the fully-qualified name this host answers at
  host.apex   the app that answers at the parent of host.name; unset means none

NAME is a host name in lower-case ASCII; an internationalized name is given in
its xn-- form. It may not be host.name or a name under it, nor, while host.apex
is set, the parent of host.name. Renewal is certbot's: the timer 'opsctl init'
writes, ikigenba-renew-certificate.timer, renews every name's certificate.
`

type domainInvocation struct{ sub, name, primary string }

func parseDomain(args []string) (domainInvocation, error) {
	var in domainInvocation
	if len(args) == 0 {
		return in, errors.New("no domain subcommand given")
	}
	in.sub = args[0]
	if in.sub != "add" && in.sub != "remove" && in.sub != "list" {
		return in, fmt.Errorf("unknown domain subcommand '%s'", diagnosticArg(in.sub))
	}
	var operands []string
	redirect := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case in.sub == "add" && arg == "--redirect":
			if redirect {
				return in, errors.New("option '--redirect' given twice")
			}
			redirect = true
			if i+1 == len(args) {
				return in, errors.New("option '--redirect' needs a value")
			}
			i++
			in.primary = args[i]
		case strings.HasPrefix(arg, "-"):
			return in, fmt.Errorf("unknown option '%s'", diagnosticArg(arg))
		default:
			operands = append(operands, arg)
		}
	}
	if in.sub == "list" {
		if len(operands) != 0 {
			return in, errors.New("domain list takes no arguments")
		}
		return in, nil
	}
	if len(operands) == 0 {
		return in, fmt.Errorf("domain %s needs NAME", in.sub)
	}
	if len(operands) != 1 {
		return in, fmt.Errorf("domain %s takes one NAME", in.sub)
	}
	in.name = operands[0]
	if !domain.ValidName(in.name) {
		return in, fmt.Errorf("invalid name: %s", diagnosticArg(in.name))
	}
	if redirect && !domain.ValidName(in.primary) {
		return in, fmt.Errorf("invalid name: %s", diagnosticArg(in.primary))
	}
	return in, nil
}

func runDomain(args []string, stdout, stderr io.Writer, deps Deps) exitCode {
	if len(args) == 1 && isCommandHelp(args) {
		return writeOut(stdout, domainUsage)
	}
	if code := requireRoot(deps, stderr); code != exitOK {
		return code
	}
	in, err := parseDomain(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "opsctl: %s\n\nsee 'opsctl domain --help' for usage\n", err)
		return exitUsage
	}
	store := config.Store{Root: deps.Root}
	var hostName, email, apexApp string
	if in.sub != "list" {
		hostName, email, apexApp, err = domainConfig(store, in.sub == "add")
		if err != nil {
			writeDiagnostic(stderr, err)
			return exitFail
		}
	}
	list, err := domain.List(store)
	if err != nil {
		writeDiagnostic(stderr, err)
		return exitFail
	}
	if in.sub == "list" {
		for _, d := range list {
			line := d.Name
			if d.Primary != "" {
				line += " " + d.Primary
			}
			if code := writeOut(stdout, line+"\n"); code != exitOK {
				return code
			}
		}
		return exitOK
	}
	env := host.Env{Root: deps.Root, Getenv: deps.Getenv, Execute: deps.Execute, Now: deps.Now}
	if in.sub == "add" {
		err = addDomain(context.Background(), env, store, in, list, hostName, email, apexApp)
	} else {
		err = removeDomain(context.Background(), env, store, in.name, list, hostName, apexApp)
	}
	if err != nil {
		var refused *accounts.RefusedError
		if errors.As(err, &refused) {
			return accountActionError(stderr, err)
		}
		var detail *domainDetailError
		if errors.As(err, &detail) {
			writeDiagnostic(stderr, detail.err)
			_, _ = fmt.Fprintf(stderr, "\n%s\n", detail.detail)
			return exitFail
		}
		return accountActionError(stderr, err)
	}
	return exitOK
}

func domainConfig(store config.Store, add bool) (hostName, email, apexApp string, err error) {
	hostName, err = store.Get("host.name")
	if err != nil && !errors.Is(err, config.ErrNotSet) {
		return
	}
	hostName = host.NormalizeName(hostName)
	if hostName == "" {
		err = errors.New("host.name not set")
		return
	}
	if add {
		email, err = store.Get("acme.email")
		if err != nil && !errors.Is(err, config.ErrNotSet) {
			return
		}
		if email == "" {
			err = errors.New("acme.email not set")
			return
		}
	}
	apexApp, err = store.Get("host.apex")
	if errors.Is(err, config.ErrNotSet) {
		apexApp = ""
		err = nil
	}
	if err == nil && apexApp != "" {
		_, err = host.Apex(hostName)
	}
	return
}

func hostOwnsName(name, hostName string) bool {
	return name == hostName || strings.HasSuffix(name, "."+hostName)
}
func domainRefusal(reason string) error { return &accounts.RefusedError{Reason: reason} }

func addDomain(ctx context.Context, env host.Env, store config.Store, in domainInvocation, list []domain.Domain, hostName, email, apexApp string) error {
	if hostOwnsName(in.name, hostName) {
		return domainRefusal("name belongs to the host: " + in.name)
	}
	if apexApp != "" {
		apex, _ := host.Apex(hostName)
		if in.name == apex {
			return domainRefusal("name is the apex: " + in.name)
		}
	}
	for _, d := range list {
		if d.Name == in.name {
			return domainRefusal("domain exists: " + in.name)
		}
	}
	if in.primary != "" {
		found := false
		for _, d := range list {
			if d.Name == in.primary {
				found = true
				if d.Primary != "" {
					return domainRefusal("not a primary name: " + in.primary)
				}
			}
		}
		if !found {
			return domainRefusal("no domain '" + in.primary + "'")
		}
	}
	if err := cert.ObtainDomain(ctx, env, in.name, email); err != nil {
		return err
	}
	next := append(append([]domain.Domain{}, list...), domain.Domain{Name: in.name, Primary: in.primary})
	return publishDomains(ctx, env, store, next, list, hostName, apexApp)
}

func publishDomains(ctx context.Context, env host.Env, store config.Store, next, prior []domain.Domain, hostName, apexApp string) error {
	if err := domain.Save(store, next); err != nil {
		return err
	}
	if err := nginx.Apply(ctx, env, hostName, apexApp); err != nil {
		if restoreErr := domain.Save(store, prior); restoreErr != nil {
			return &domainDetailError{err: err, detail: restoreErr.Error()}
		}
		return err
	}
	_, err := services.Write(ctx, env, hostName)
	return err
}

type domainDetailError struct {
	err    error
	detail string
}

func (e *domainDetailError) Error() string { return e.err.Error() }
func (e *domainDetailError) Unwrap() error { return e.err }

func removeDomain(ctx context.Context, env host.Env, store config.Store, name string, list []domain.Domain, hostName, apexApp string) error {
	if hostOwnsName(name, hostName) {
		return domainRefusal("name belongs to the host: " + name)
	}
	index := -1
	for i, d := range list {
		if d.Name == name {
			index = i
		}
	}
	if index >= 0 {
		for _, d := range list {
			if d.Primary == name {
				return domainRefusal("domain has redirects: " + name)
			}
		}
	}
	lineage, statErr := os.Stat(filepath.Join(env.Root, "etc/letsencrypt/live", name))
	hasCertificate := statErr == nil && lineage.IsDir()
	if index < 0 && !hasCertificate {
		return domainRefusal("no domain '" + name + "'")
	}
	if index >= 0 {
		next := append([]domain.Domain{}, list[:index]...)
		next = append(next, list[index+1:]...)
		if err := publishDomains(ctx, env, store, next, list, hostName, apexApp); err != nil {
			return err
		}
	}
	if hasCertificate {
		if err := cert.DeleteDomain(ctx, env, name); err != nil {
			return &domainDetailError{err: err, detail: fmt.Sprintf("the certificate for %s remains; run 'opsctl domain remove %s' again to delete it", name, name)}
		}
	}
	return nil
}

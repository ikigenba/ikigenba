package cert

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// Webroot is the host directory serving custom-domain HTTP-01 challenges.
const Webroot = "/var/www/acme"

// ObtainDomain obtains or renews a single-name HTTP-01 lineage.
func ObtainDomain(ctx context.Context, env host.Env, name, email string) error {
	if email == "" {
		return errors.New("acme.email not set")
	}
	if name == "" {
		return errors.New("domain name not set")
	}
	args := []string{
		"certonly", "--non-interactive", "--agree-tos", "--email", email,
		"--webroot", "--webroot-path", filepath.Join(env.Root, Webroot),
		"--deploy-hook", deployHook, "--cert-name", name, "-d", name,
		"--keep-until-expiring",
	}
	return executeDomain(ctx, env, "certbot certonly", append(args, domainDirectories(env.Root)...))
}

// DeleteDomain removes a single custom-domain lineage and its renewal record.
func DeleteDomain(ctx context.Context, env host.Env, name string) error {
	if name == "" {
		return errors.New("domain name not set")
	}
	args := []string{"delete", "--non-interactive", "--cert-name", name}
	return executeDomain(ctx, env, "certbot delete", append(args, domainDirectories(env.Root)...))
}

func domainDirectories(root string) []string {
	return []string{
		"--config-dir", filepath.Join(root, "etc/letsencrypt"),
		"--work-dir", filepath.Join(root, "var/lib/letsencrypt"),
		"--logs-dir", filepath.Join(root, "var/log/letsencrypt"),
	}
}

func executeDomain(ctx context.Context, env host.Env, label string, args []string) error {
	if env.Execute == nil {
		return errors.New("certificate operation: host execution is not configured")
	}
	result, err := env.Execute(ctx, host.Command{Name: "certbot", Args: args})
	if err != nil || result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result, Err: err}
	}
	return nil
}

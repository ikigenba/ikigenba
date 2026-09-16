// Package cert provides certificate inspection and obtaining.
package cert

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const deployHook = "if systemctl is-active --quiet nginx; then systemctl reload nginx; fi"

// Obtain obtains the apex and wildcard certificate for hostName, or renews it
// when certbot considers it due.
func Obtain(ctx context.Context, env host.Env, hostName, email string) error {
	if hostName == "" {
		return errors.New("host.name not set")
	}
	if email == "" {
		return errors.New("acme.email not set")
	}

	rooted := func(path string) string {
		return filepath.Join(env.Root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	}
	command := host.Command{
		Name: "certbot",
		Args: []string{
			"certonly",
			"--non-interactive",
			"--agree-tos",
			"--email", email,
			"--manual",
			"--preferred-challenges", "dns",
			"--manual-auth-hook", "opsctl dns acme-auth",
			"--manual-cleanup-hook", "opsctl dns acme-cleanup",
			"--deploy-hook", deployHook,
			"--cert-name", hostName,
			"-d", hostName,
			"-d", "*." + hostName,
			"--keep-until-expiring",
			"--config-dir", rooted("/etc/letsencrypt"),
			"--work-dir", rooted("/var/lib/letsencrypt"),
			"--logs-dir", rooted("/var/log/letsencrypt"),
		},
	}
	result, err := env.Execute(ctx, command)
	if err != nil || result.ExitCode != 0 {
		return &host.CommandError{
			Label:  "certbot certonly",
			Result: result,
			Err:    err,
		}
	}
	return nil
}

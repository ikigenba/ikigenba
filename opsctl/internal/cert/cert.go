// Package cert provides certificate inspection and obtaining.
package cert

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

const deployHook = "systemctl try-reload-or-restart nginx"

// ErrNotFound identifies an absent certificate lineage.
var ErrNotFound = errors.New("certificate not found")

// Info describes the certificate served by the host.
type Info struct {
	Names   []string
	Issuer  string
	Expires time.Time
}

type notFoundError struct {
	hostName string
}

func (e notFoundError) Error() string {
	return "no certificate for " + diagnosticHostName(e.hostName)
}

func (e notFoundError) Unwrap() error {
	return ErrNotFound
}

// Inspect reads the host's certificate lineage.
func Inspect(env host.Env, hostName string) (Info, error) {
	root, err := os.OpenRoot(env.Root)
	if err != nil {
		return Info{}, fmt.Errorf("open certificate root: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()

	fullchain := filepath.Join("etc", "letsencrypt", "live", hostName, "fullchain.pem")
	contents, err := root.ReadFile(fullchain)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Info{}, notFoundError{hostName: hostName}
		}
		return Info{}, fmt.Errorf("read certificate: %w", err)
	}

	var block *pem.Block
	for len(contents) > 0 {
		block, contents = pem.Decode(contents)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			certificate, parseErr := x509.ParseCertificate(block.Bytes)
			if parseErr != nil {
				return Info{}, fmt.Errorf("parse certificate: %w", parseErr)
			}

			issuer := certificate.Issuer.CommonName
			if issuer == "" {
				issuer = certificate.Issuer.String()
			}
			return Info{
				Names:   certificate.DNSNames,
				Issuer:  issuer,
				Expires: certificate.NotAfter,
			}, nil
		}
	}

	return Info{}, errors.New("parse certificate: no PEM certificate block")
}

func diagnosticHostName(hostName string) string {
	return strings.Map(func(character rune) rune {
		switch {
		case character >= 'a' && character <= 'z':
			return character
		case character >= 'A' && character <= 'Z':
			return character
		case character >= '0' && character <= '9':
			return character
		case character == '.', character == '-':
			return character
		default:
			return '?'
		}
	}, hostName)
}

// Obtain obtains the host and wildcard certificate, plus the parent domain
// when apex is set, or renews it when certbot considers it due.
func Obtain(ctx context.Context, env host.Env, hostName, email string, apex bool) error {
	if hostName == "" {
		return errors.New("host.name not set")
	}
	if email == "" {
		return errors.New("acme.email not set")
	}
	var apexName string
	if apex {
		var err error
		apexName, err = host.Apex(hostName)
		if err != nil {
			return err
		}
	}
	if env.Execute == nil {
		return errors.New("obtain certificate: host execution is not configured")
	}

	rooted := func(path string) string {
		return filepath.Join(env.Root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
	}
	args := []string{
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
	}
	if apex {
		args = append(args, "-d", apexName)
	}
	args = append(args,
		"--keep-until-expiring",
		"--config-dir", rooted("/etc/letsencrypt"),
		"--work-dir", rooted("/var/lib/letsencrypt"),
		"--logs-dir", rooted("/var/log/letsencrypt"),
	)
	command := host.Command{
		Name: "certbot",
		Args: args,
	}
	result, err := env.Execute(ctx, command)
	if err != nil {
		var commandErr *host.CommandError
		if errors.As(err, &commandErr) {
			return err
		}
		return &host.CommandError{
			Label:  "certbot certonly",
			Result: result,
			Err:    err,
		}
	}
	if result.ExitCode != 0 {
		return &host.CommandError{
			Label:  "certbot certonly",
			Result: result,
		}
	}
	return nil
}

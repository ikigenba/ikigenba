package spacecreate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/ikigenba/ikigenba/devctl/internal/apex"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func finishCreate(ctx context.Context, stdout io.Writer, deps seam.Deps, inv invocation, p preflightResult, l Launch, r Restored, address string) error {
	if err := knownHosts(ctx, stdout, deps, p.sp.Domain, r.Backup); err != nil {
		return err
	}
	holds, err := configureApex(ctx, stdout, inv, l, r)
	if err != nil {
		return err
	}
	var holder cloud.Space
	if holds {
		holder, err = apex.CheckHolder(ctx, p.session.Clients.Route53, p.session.Clients.EC2, p.zone, p.root.Domain, p.sp.Domain)
		if err != nil {
			return err
		}
		if err = apex.GrantApex(ctx, stdout, p.session.Clients.IAM, p.root.Domain, p.zone, p.sp); err != nil {
			return err
		}
	}
	if err = FinishHost(ctx, stdout, deps, p.session.Clients.S3, l, r); err != nil {
		return err
	}
	if holds {
		if err = apex.PointRoot(ctx, stdout, deps, p.session.Clients.Route53, p.zone, p.root.Domain, address); err != nil {
			return err
		}
		if err = apex.ClearPrevious(ctx, stdout, deps, p.session.Clients.IAM, p.root.Domain, p.zone, holder); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "%s %s\n", p.sp.Domain, address)
	return err
}

func configureApex(ctx context.Context, stdout io.Writer, inv invocation, l Launch, r Restored) (bool, error) {
	switch {
	case inv.apex != "":
		if _, err := r.Host.Sudo(ctx, "apex", release.Opsctl(l.SHA), "config", "set", hostsetup.KeyHostApex+"="+inv.apex); err != nil {
			return false, err
		}
		space.Step(stdout, "apex", hostsetup.KeyHostApex+"="+inv.apex)
		return true, nil
	case inv.noApex:
		if _, err := r.Host.Sudo(ctx, "apex", release.Opsctl(l.SHA), "config", "del", hostsetup.KeyHostApex); err != nil {
			return false, err
		}
		space.Step(stdout, "apex", hostsetup.KeyHostApex+" unset")
		return false, nil
	case r.Backup:
		_, err := r.Host.Sudo(ctx, "apex", release.Opsctl(l.SHA), "config", "get", hostsetup.KeyHostApex)
		var command *host.CommandError
		if errors.As(err, &command) && command.Status == 1 {
			return false, nil
		}
		return err == nil, err
	default:
		return false, nil
	}
}

func knownHosts(ctx context.Context, stdout io.Writer, deps seam.Deps, domain string, restored bool) error {
	if restored {
		space.Step(stdout, "known_hosts", "kept")
		return nil
	}
	path := deps.Getenv("HOME") + "/.ssh/known_hosts"
	_, err := deps.Stat(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &knownHostsStatError{path: path, err: err}
	}
	if err == nil {
		result, err := deps.Exec(ctx, seam.Cmd{Path: "ssh-keygen", Args: []string{"-R", domain}, Dir: deps.Dir})
		if err != nil {
			return fmt.Errorf("known_hosts: ssh-keygen: %w", err)
		}
		if result.ExitCode != 0 {
			return &KnownHostsError{Domain: domain, Status: result.ExitCode, Stderr: string(result.Stderr)}
		}
	}
	space.Step(stdout, "known_hosts", "removed "+domain)
	return nil
}

type knownHostsStatError struct {
	path string
	err  error
}

func (e *knownHostsStatError) Error() string {
	return fmt.Sprintf("known_hosts: %s: %v", e.path, e.err)
}
func (e *knownHostsStatError) Unwrap() error { return e.err }
func (e *knownHostsStatError) ExitCode() int { return 1 }

// Package spacemove moves a space to a new host while retaining its old resources.
package spacemove

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ikigenba/ikigenba/devctl/internal/apex"
	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
)

// NoHostBackupError reports the absence of the backup needed for a move.
type NoHostBackupError struct{ Bucket, Prefix string }

func (e *NoHostBackupError) Error() string {
	return "restore: no host backup under s3://" + e.Bucket + "/" + e.Prefix
}

// ExitCode returns an operation failure.
func (e *NoHostBackupError) ExitCode() int { return 1 }

// Run executes a space move command.
func Run(ctx context.Context, args []string, version string, stdout io.Writer, deps seam.Deps) error {
	inv, err := parse(args)
	if err != nil {
		return err
	}
	if inv.help {
		_, err = io.WriteString(stdout, usageText)
		return err
	}
	deps = deps.Defaults()
	p, err := preflight(ctx, deps, inv)
	if err != nil {
		return err
	}
	base := deps.Getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	if strings.ContainsFunc(base, unicode.IsSpace) {
		return &spacecreate.RefusedError{Message: fmt.Sprintf("temporary directory '%s' holds whitespace", base)}
	}
	dir, err := os.MkdirTemp(base, "move.")
	if err != nil {
		return fmt.Errorf("move temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	space.Step(stdout, "account", fmt.Sprintf("%s, %s, %s", p.root.Domain, p.root.Region, p.session.AccountID))
	space.Step(stdout, "domain", fmt.Sprintf("zone %s %s", p.zone.Name, p.zone.ID))
	built, err := build.Suite(ctx, p.checkout, p.sha, version)
	if err != nil {
		return err
	}
	detail := built.File
	if p.label != "" {
		detail = p.label + ", " + detail
	}
	space.Step(stdout, "build", detail)
	if err := retire(ctx, stdout, deps, p); err != nil {
		return err
	}
	instance, address, err := launch(ctx, stdout, deps, p)
	if err != nil {
		return err
	}
	h := host.Host{Address: address.IP, Deps: deps, KnownHosts: filepath.Join(dir, "known_hosts.launch")}
	if err := restoreHost(ctx, stdout, p, built, h, instance.ID, filepath.Join(dir, "known_hosts")); err != nil {
		return err
	}
	if p.apex.Holder == p.sp.Domain {
		if err := apex.PutRecord(ctx, deps, p.session.Clients.Route53, p.zone, p.root.Domain, address.IP); err != nil {
			return err
		}
		space.Step(stdout, "apex", apex.RecordDetail(p.root.Domain, address.IP))
	}
	_, err = fmt.Fprintf(stdout, "%s %s %s\n", p.sp.Domain, instance.ID, address.IP)
	return err
}

func retire(ctx context.Context, stdout io.Writer, deps seam.Deps, p preflightResult) error {
	ec2 := p.session.Clients.EC2
	h := host.Host{Address: p.old.Address, Deps: deps}
	if p.old.State == cloud.StateStopped {
		if err := ec2.StartInstance(ctx, p.old.ID); err != nil {
			return err
		}
		running, err := space.WaitState(ctx, deps, ec2, p.old.ID, cloud.StateRunning)
		if err != nil {
			return err
		}
		if err := space.WaitChecks(ctx, deps, ec2, p.old.ID); err != nil {
			return err
		}
		h.Address = running.Address
		if err := h.Wait(ctx); err != nil {
			return err
		}
		space.Step(stdout, "start", p.old.ID+" running, status checks passed")
	}
	if _, err := h.Sudo(ctx, "retire", "opsctl", "retire"); err != nil {
		return err
	}
	space.Step(stdout, "retire", "opsctl retire")
	if err := ec2.StopInstance(ctx, p.old.ID); err != nil {
		return err
	}
	if _, err := space.WaitState(ctx, deps, ec2, p.old.ID, cloud.StateStopped); err != nil {
		return err
	}
	space.Step(stdout, "stop", p.old.ID+" stopped")
	ids := []string{p.old.ID}
	detail := p.old.ID
	if p.hasAddress {
		ids = append(ids, p.address.AllocationID)
		detail += " and elastic ip " + p.address.IP
	}
	value := cloud.RetiredValue(deps.Now())
	if err := ec2.TagRetired(ctx, ids, value); err != nil {
		return err
	}
	space.Step(stdout, "tag", detail+" Retired="+value)
	return nil
}

func launch(ctx context.Context, stdout io.Writer, deps seam.Deps, p preflightResult) (cloud.Instance, cloud.Address, error) {
	ec2 := p.session.Clients.EC2
	instance, err := ec2.RunInstance(ctx, cloud.LaunchSpec{LaunchTemplateID: p.template, InstanceProfile: space.RoleName(p.sp.Domain), Domain: p.root.Domain, Space: p.sp.Domain})
	if err != nil {
		return cloud.Instance{}, cloud.Address{}, err
	}
	instance, err = space.WaitState(ctx, deps, ec2, instance.ID, cloud.StateRunning)
	if err != nil {
		return cloud.Instance{}, cloud.Address{}, err
	}
	space.Step(stdout, "instance", instance.ID+" running, "+instance.Address)
	address, err := ec2.AllocateAddress(ctx, p.root.Domain, p.sp.Domain)
	if err != nil {
		return cloud.Instance{}, cloud.Address{}, err
	}
	if err := space.WaitAssociate(ctx, deps, ec2, address.AllocationID, instance.ID); err != nil {
		return cloud.Instance{}, cloud.Address{}, err
	}
	space.Step(stdout, "address", "elastic ip "+address.IP+" associated")
	return instance, address, nil
}

// Package spacereplace replaces a space's instance while preserving its address.
package spacereplace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
	"github.com/ikigenba/ikigenba/devctl/internal/spacecreate"
)

// Run executes space replace, retaining completed steps on failure.
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
	dir, err := os.MkdirTemp(base, "replace.")
	if err != nil {
		return fmt.Errorf("replace temporary directory: %w", err)
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
	if err := start(ctx, stdout, deps, p, inv.noBackup); err != nil {
		return err
	}
	eip, err := addressOld(ctx, stdout, deps, p, inv.noBackup)
	if err != nil {
		return err
	}
	if err := retire(ctx, stdout, deps, p, inv.noBackup); err != nil {
		return err
	}
	if err := disassociate(ctx, stdout, p); err != nil {
		return err
	}
	instance, err := launch(ctx, stdout, deps, p)
	if err != nil {
		return err
	}
	if err := space.WaitAssociate(ctx, deps, p.session.Clients.EC2, eip.AllocationID, instance.ID); err != nil {
		return err
	}
	space.Step(stdout, "address", "elastic ip "+eip.IP+" associated")
	if err := spacecreate.Resolve(ctx, stdout, deps, p.sp.Domain, eip.IP); err != nil {
		return err
	}
	l := spacecreate.Launch{InstanceID: instance.ID, SHA: p.sha, Label: p.label, File: p.checkout.Path(built.File), Manifests: built.Manifests, Config: hostsetup.Config{Root: p.root.Domain, Region: p.root.Region, ZoneID: p.zone.ID, Space: p.sp, Opsctl: release.Opsctl(p.sha)}, KnownHosts: filepath.Join(dir, "known_hosts.launch"), RequireHostBackup: true}
	restored, err := spacecreate.SetUpHost(ctx, stdout, deps, p.session.Clients.EC2, p.session.Clients.S3, l)
	if err != nil {
		return err
	}
	if err := spacecreate.FinishHost(ctx, stdout, deps, p.session.Clients.S3, l, restored); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s %s %s\n", p.sp.Domain, instance.ID, eip.IP)
	return err
}

func start(ctx context.Context, stdout io.Writer, deps seam.Deps, p preflightResult, noBackup bool) error {
	if noBackup || p.old.State != cloud.StateStopped {
		return nil
	}
	ec2 := p.session.Clients.EC2
	if err := ec2.StartInstance(ctx, p.old.ID); err != nil {
		return err
	}
	if _, err := space.WaitState(ctx, deps, ec2, p.old.ID, cloud.StateRunning); err != nil {
		return err
	}
	if err := space.WaitChecks(ctx, deps, ec2, p.old.ID); err != nil {
		return err
	}
	if p.old.ElasticIP.AllocationID != "" {
		if err := (host.Host{Domain: p.sp.Domain, Deps: deps}).Wait(ctx); err != nil {
			return err
		}
	}
	space.Step(stdout, "start", p.old.ID+" running, status checks passed")
	return nil
}

func addressOld(ctx context.Context, stdout io.Writer, deps seam.Deps, p preflightResult, noBackup bool) (cloud.Address, error) {
	if p.old.ElasticIP.AllocationID != "" {
		return p.old.ElasticIP, nil
	}
	eip, err := p.session.Clients.EC2.AllocateAddress(ctx, p.root.Domain, p.sp.Domain)
	if err != nil {
		return cloud.Address{}, err
	}
	if err := space.WaitAssociate(ctx, deps, p.session.Clients.EC2, eip.AllocationID, p.old.ID); err != nil {
		return cloud.Address{}, err
	}
	space.Step(stdout, "address", "elastic ip "+eip.IP+" associated")
	if err := space.PutRecords(ctx, deps, p.session.Clients.Route53, p.zone, p.sp.Domain, eip.IP); err != nil {
		return cloud.Address{}, err
	}
	space.Step(stdout, "records", "created "+strings.Join(space.RecordNames(p.sp.Domain), ", ")+" -> "+eip.IP+", INSYNC")
	if err := spacecreate.Resolve(ctx, stdout, deps, p.sp.Domain, eip.IP); err != nil {
		return cloud.Address{}, err
	}
	if !noBackup && p.old.State == cloud.StateStopped {
		if err := (host.Host{Domain: p.sp.Domain, Deps: deps}).Wait(ctx); err != nil {
			return cloud.Address{}, err
		}
	}
	return eip, nil
}

func retire(ctx context.Context, stdout io.Writer, deps seam.Deps, p preflightResult, noBackup bool) error {
	if p.old.ID == "" {
		space.Step(stdout, "retire", "already gone")
		return nil
	}
	if noBackup {
		_, err := io.WriteString(stdout, "retire: skipped (--no-backup)\n")
		return err
	}
	if _, err := (host.Host{Domain: p.sp.Domain, Deps: deps}).Sudo(ctx, "retire", "opsctl", "retire"); err != nil {
		return err
	}
	space.Step(stdout, "retire", "opsctl retire")
	return nil
}

func disassociate(ctx context.Context, stdout io.Writer, p preflightResult) error {
	a := p.old.ElasticIP
	if a.AllocationID == "" {
		return nil
	}
	detail := "not associated"
	if a.AssociationID != "" {
		if err := p.session.Clients.EC2.DisassociateAddress(ctx, a.AssociationID); err != nil {
			return err
		}
		detail = "elastic ip " + a.IP + " disassociated from " + a.InstanceID
	}
	space.Step(stdout, "address", detail)
	return nil
}

func launch(ctx context.Context, stdout io.Writer, deps seam.Deps, p preflightResult) (cloud.Instance, error) {
	ec2 := p.session.Clients.EC2
	if p.old.ID != "" {
		if err := ec2.TerminateInstance(ctx, p.old.ID); err != nil {
			return cloud.Instance{}, err
		}
		if _, err := space.WaitState(ctx, deps, ec2, p.old.ID, cloud.StateTerminated); err != nil {
			return cloud.Instance{}, err
		}
	}
	n, err := ec2.RunInstance(ctx, cloud.LaunchSpec{LaunchTemplateID: p.template, InstanceProfile: space.RoleName(p.sp.Domain), Domain: p.root.Domain, Space: p.sp.Domain})
	if err != nil {
		return cloud.Instance{}, err
	}
	n, err = space.WaitState(ctx, deps, ec2, n.ID, cloud.StateRunning)
	if err != nil {
		return cloud.Instance{}, err
	}
	detail := n.ID + " running, " + n.Address
	if p.old.ID != "" {
		detail = p.old.ID + " terminated, " + detail
	}
	space.Step(stdout, "instance", detail)
	return n, nil
}

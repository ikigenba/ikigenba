package spacemove

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func restoreHost(ctx context.Context, stdout io.Writer, p preflightResult, built build.Release, h host.Host, instanceID, knownHosts string) error {
	clients := p.session.Clients
	if err := space.WaitChecks(ctx, h.Deps, clients.EC2, instanceID); err != nil {
		return err
	}
	if err := h.Wait(ctx); err != nil {
		return err
	}
	if _, err := h.Sudo(ctx, "host", "cloud-init", "status", "--wait"); err != nil {
		return err
	}
	space.Step(stdout, "host", "status checks passed, cloud-init done")
	if err := release.Put(ctx, h, stdout, p.checkout.Path(built.File), p.sha); err != nil {
		return err
	}
	cfg := hostsetup.Config{Root: p.root.Domain, Region: p.root.Region, ZoneID: p.zone.ID, Space: p.sp, Opsctl: release.Opsctl(p.sha)}
	count, err := hostsetup.Configure(ctx, h, "opsctl", cfg)
	if err != nil {
		return err
	}
	space.Step(stdout, "opsctl", fmt.Sprintf("%d keys set", count))
	prefix := space.BackupPrefix(p.sp.Label) + "host/"
	objects, err := clients.S3.ListObjects(ctx, p.root.Domain, prefix)
	if err != nil {
		return err
	}
	selected, found := space.HostBackup(objects, prefix)
	if !found {
		return &NoHostBackupError{Bucket: p.root.Domain, Prefix: prefix}
	}
	h, count, err = hostsetup.Restore(ctx, h, "restore", cfg, knownHosts)
	if err != nil {
		return err
	}
	space.Step(stdout, "restore", fmt.Sprintf("%s, %d keys set again", strings.TrimPrefix(selected.Key, space.BackupPrefix(p.sp.Label)), count))
	if err := space.PutRecords(ctx, h.Deps, clients.Route53, p.zone, p.sp.Domain, h.Address); err != nil {
		return err
	}
	space.Step(stdout, "records", "updated "+strings.Join(space.RecordNames(p.sp.Domain), ", ")+" -> "+h.Address+", INSYNC")
	if _, err := h.Sudo(ctx, "init", release.Opsctl(p.sha), "init"); err != nil {
		return err
	}
	space.Step(stdout, "init", "")
	for _, manifest := range built.Manifests {
		objects, err := clients.S3.ListObjects(ctx, p.root.Domain, space.BackupPrefix(p.sp.Label)+manifest.App+"/")
		if err != nil {
			return err
		}
		detail := "no backup of " + manifest.App
		if len(objects) > 0 {
			if _, err := h.Sudo(ctx, "restore", release.Opsctl(p.sha), "restore", manifest.App); err != nil {
				return err
			}
			detail = "opsctl restore " + manifest.App
		}
		space.Step(stdout, "restore", detail)
	}
	return release.Activate(ctx, h, stdout, p.sha, p.label)
}

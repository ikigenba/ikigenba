package spacecreate

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/build"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

func runHostSteps(ctx context.Context, stdout io.Writer, deps seam.Deps, result preflightResult, address, instanceID, acmeEmail string, built build.Release) error {
	if err := space.WaitChecks(ctx, deps, result.session.Clients.EC2, instanceID); err != nil {
		return err
	}
	target := host.Host{Address: address, Deps: deps}
	if err := target.Wait(ctx); err != nil {
		return err
	}
	if _, err := target.Sudo(ctx, "host", "cloud-init", "status", "--wait"); err != nil {
		return err
	}
	space.Step(stdout, "host", "status checks passed, cloud-init done")

	if err := release.Put(ctx, target, stdout, result.checkout.Path(built.File), result.sha); err != nil {
		return err
	}
	periods := hostsetup.DefaultBackupPeriods()
	cfg := hostsetup.Config{Root: result.root.Domain, Region: result.root.Region, ZoneID: result.zone.ID, Space: result.sp, Email: acmeEmail, Periods: &periods, Opsctl: release.Opsctl(result.sha)}
	configured, err := hostsetup.Configure(ctx, target, "opsctl", cfg)
	if err != nil {
		return err
	}
	space.Step(stdout, "opsctl", fmt.Sprintf("%d keys set", configured))

	prefix := space.BackupPrefix(result.sp.Label) + "host/"
	objects, err := result.session.Clients.S3.ListObjects(ctx, result.root.Domain, prefix)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		space.Step(stdout, "restore", "no host backup")
	} else {
		selected := newestObject(objects)
		if _, err := target.Sudo(ctx, "restore", release.Opsctl(result.sha), "host", "restore"); err != nil {
			return err
		}
		configured, err = hostsetup.Configure(ctx, target, "restore", cfg)
		if err != nil {
			return err
		}
		if err := deleteApex(ctx, target, result.sha); err != nil {
			return err
		}
		space.Step(stdout, "restore", fmt.Sprintf("%s, %d keys set again", strings.TrimPrefix(selected.Key, space.BackupPrefix(result.sp.Label)), configured))
	}

	if _, err := target.Sudo(ctx, "init", release.Opsctl(result.sha), "init"); err != nil {
		return err
	}
	space.Step(stdout, "init", "")
	if len(objects) != 0 {
		for _, manifest := range built.Manifests {
			objects, err := result.session.Clients.S3.ListObjects(ctx, result.root.Domain, space.BackupPrefix(result.sp.Label)+manifest.App+"/")
			if err != nil {
				return err
			}
			detail := "no backup of " + manifest.App
			if len(objects) != 0 {
				if _, err := target.Sudo(ctx, "restore", release.Opsctl(result.sha), "restore", manifest.App); err != nil {
					return err
				}
				detail = "opsctl restore " + manifest.App
			}
			space.Step(stdout, "restore", detail)
		}
	}
	return release.Activate(ctx, target, stdout, result.sha, result.label)
}
func deleteApex(ctx context.Context, h host.Host, sha string) error {
	_, err := h.Sudo(ctx, "restore", release.Opsctl(sha), "config", "del", hostsetup.KeyHostApex)
	return err
}

func newestObject(objects []cloud.Object) cloud.Object {
	copyOf := append([]cloud.Object(nil), objects...)
	sort.Slice(copyOf, func(i, j int) bool {
		if copyOf[i].Modified.Equal(copyOf[j].Modified) {
			return copyOf[i].Key > copyOf[j].Key
		}
		return copyOf[i].Modified.After(copyOf[j].Modified)
	})
	return copyOf[0]
}

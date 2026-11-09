package spacecreate

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ikigenba/ikigenba/devctl/internal/checkout"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/host"
	"github.com/ikigenba/ikigenba/devctl/internal/hostsetup"
	"github.com/ikigenba/ikigenba/devctl/internal/release"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

// Launch describes the release and host configuration shared by create and replace.
type Launch struct {
	InstanceID        string
	SHA               string
	Label             string
	File              string
	Manifests         []checkout.Manifest
	Config            hostsetup.Config
	KnownHosts        string
	RequireHostBackup bool
}

// Restored records the host and whether a host backup was restored.
type Restored struct {
	Host   host.Host
	Backup bool
}

// Resolve waits for the system resolver to identify the space's address.
func Resolve(ctx context.Context, stdout io.Writer, deps seam.Deps, domain, address string) error {
	if err := space.WaitResolves(ctx, deps, domain, address); err != nil {
		return err
	}
	space.Step(stdout, "resolve", domain+" -> "+address)
	return nil
}

// SetUpHost installs the release and restores the host, when a backup exists.
func SetUpHost(ctx context.Context, stdout io.Writer, deps seam.Deps, ec2 cloud.EC2, s3 cloud.S3, l Launch) (Restored, error) {
	if err := space.WaitChecks(ctx, deps, ec2, l.InstanceID); err != nil {
		return Restored{}, err
	}
	h := host.Host{Domain: l.Config.Space.Domain, Deps: deps, KnownHosts: l.KnownHosts}
	if err := h.Wait(ctx); err != nil {
		return Restored{}, err
	}
	if _, err := h.Sudo(ctx, "host", "cloud-init", "status", "--wait"); err != nil {
		return Restored{}, err
	}
	space.Step(stdout, "host", "status checks passed, cloud-init done")
	if err := release.Put(ctx, h, stdout, l.File, l.SHA); err != nil {
		return Restored{}, err
	}
	count, err := hostsetup.Configure(ctx, h, "opsctl", l.Config)
	if err != nil {
		return Restored{}, err
	}
	space.Step(stdout, "opsctl", fmt.Sprintf("%d keys set", count))
	prefix := space.BackupPrefix(l.Config.Space.Label) + "host/"
	var newest cloud.Object
	found := false
	err = s3.ListObjects(ctx, l.Config.Root, prefix, func(page []cloud.Object) bool {
		newest, found = space.HostBackup(newest, found, page, prefix)
		return true
	})
	if err != nil {
		return Restored{}, err
	}
	if !found {
		if l.RequireHostBackup {
			return Restored{}, &NoHostBackupError{Bucket: l.Config.Root, Prefix: prefix}
		}
		h.KnownHosts = ""
		space.Step(stdout, "restore", "no host backup")
		return Restored{Host: h}, nil
	}
	h, count, err = hostsetup.Restore(ctx, h, "restore", l.Config, "")
	if err != nil {
		return Restored{}, err
	}
	space.Step(stdout, "restore", fmt.Sprintf("%s, %d keys set again", strings.TrimPrefix(newest.Key, space.BackupPrefix(l.Config.Space.Label)), count))
	return Restored{Host: h, Backup: true}, nil
}

// FinishHost initializes the restored host, restores each app, and activates the release.
func FinishHost(ctx context.Context, stdout io.Writer, _ seam.Deps, s3 cloud.S3, l Launch, r Restored) error {
	if _, err := r.Host.Sudo(ctx, "init", release.Opsctl(l.SHA), "init"); err != nil {
		return err
	}
	space.Step(stdout, "init", "")
	if r.Backup {
		for _, manifest := range l.Manifests {
			found := false
			err := s3.ListObjects(ctx, l.Config.Root, space.BackupPrefix(l.Config.Space.Label)+manifest.App+"/", func(page []cloud.Object) bool { found = len(page) > 0; return false })
			if err != nil {
				return err
			}
			detail := "no backup of " + manifest.App
			if found {
				if _, err := r.Host.Sudo(ctx, "restore", release.Opsctl(l.SHA), "restore", manifest.App); err != nil {
					return err
				}
				detail = "opsctl restore " + manifest.App
			}
			space.Step(stdout, "restore", detail)
		}
	}
	return release.Activate(ctx, r.Host, stdout, l.SHA, l.Label)
}

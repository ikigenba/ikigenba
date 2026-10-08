package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
	"github.com/ikigenba/ikigenba/opsctl/internal/release"
)

func ensureInitOpsctl(deps Deps) error {
	fail := func(err error) error { return fmt.Errorf("%s: %w", release.OpsctlLink, err) }
	fs, err := os.OpenRoot(deps.Root)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = fs.Close() }()
	_, err = fs.Lstat(strings.TrimPrefix(release.OpsctlLink, "/"))
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	layout, err := apps.ReadLayout(deps.Root)
	if err != nil {
		return fail(err)
	}
	target := release.CurrentOpsctl
	if layout != apps.Released {
		path, err := deps.Executable()
		if err != nil {
			return fail(err)
		}
		resolved, err := release.Resolve(deps.Root, path)
		if err != nil {
			return fail(err)
		}
		absolute, err := filepath.Abs(deps.Root)
		if err != nil {
			return fail(err)
		}
		relative, err := filepath.Rel(absolute, resolved)
		if err != nil {
			return fail(err)
		}
		target = "/" + filepath.ToSlash(relative)
	}
	if err = release.LinkOpsctl(deps.Root, target); err != nil {
		return fail(err)
	}
	return nil
}

type initAppFiles struct {
	service         apps.Service
	data            []byte
	oldEnv, oldUnit []byte
}

func setupInitApps(ctx context.Context, env host.Env, store config.Store, remote cloud.Env, hostName string) error {
	layout, err := apps.ReadLayout(env.Root)
	if err != nil {
		return err
	}
	if layout == apps.Fresh {
		return nil
	}
	if layout == apps.PerApp {
		return apps.SetupTimeouts(ctx, env, store)
	}
	region, err := store.Get("aws.region")
	if errors.Is(err, config.ErrNotSet) || err == nil && region == "" {
		return errors.New("aws.region not set")
	}
	if err != nil {
		return err
	}
	r, _, err := release.Current(env.Root)
	if err != nil {
		return err
	}
	timing, err := apps.ReadTimeouts(store)
	if err != nil {
		return err
	}
	discovered, err := apps.Discover(env.Root)
	if err != nil {
		return err
	}
	fs, err := os.OpenRoot(env.Root)
	if err != nil {
		return err
	}
	defer func() { _ = fs.Close() }()
	var files []initAppFiles
	for _, s := range discovered {
		if s.Dir == "" {
			continue
		}
		if s.ManifestError != nil {
			return s.ManifestError
		}
		if s.Manifest == nil {
			return fmt.Errorf("%s: no manifest", s.Name)
		}
		secrets, err := apps.ReadSecrets(ctx, remote, region, hostName, *s.Manifest)
		if err != nil {
			return err
		}
		data, err := apps.ReleaseEnv(*s.Manifest, secrets, timing, r)
		if err != nil {
			return err
		}
		oldEnv, _ := fs.ReadFile(filepath.Join(strings.TrimPrefix(apps.EnvRoot, "/"), s.Name, "env"))
		oldUnit, _ := fs.ReadFile("etc/systemd/system/ikigenba-" + s.Name + ".service")
		files = append(files, initAppFiles{s, data, oldEnv, oldUnit})
	}
	oldUnits := map[string]string{}
	for _, f := range files {
		for _, kind := range []string{"service", "socket"} {
			p := "etc/systemd/system/ikigenba-" + f.service.Name + "." + kind
			b, _ := fs.ReadFile(p)
			oldUnits[p] = string(b)
		}
	}
	servicesPath := "etc/systemd/system/" + apps.ServicesUnit
	old, _ := fs.ReadFile(servicesPath)
	oldUnits[servicesPath] = string(old)
	for _, f := range files {
		if err := apps.WriteEnv(env, f.service.Name, f.data); err != nil {
			return err
		}
		if err := apps.WriteReleaseUnits(env, f.service.Name, *f.service.Manifest, timing); err != nil {
			return err
		}
	}
	if err := apps.WriteServicesUnit(env); err != nil {
		return err
	}
	changed := false
	for p, b := range oldUnits {
		now, err := fs.ReadFile(p)
		if err != nil {
			return err
		}
		if string(now) != b {
			changed = true
		}
	}
	execute := func(args ...string) (host.Result, error) {
		if env.Execute == nil {
			return host.Result{}, errors.New("process executor unavailable")
		}
		result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: args})
		if err != nil || result.ExitCode != 0 {
			return result, &host.CommandError{Label: "systemctl " + strings.Join(args, " "), Result: result, Err: err}
		}
		return result, nil
	}
	if changed {
		if _, err := execute("daemon-reload"); err != nil {
			return err
		}
	}
	if _, err := execute("enable", apps.ServicesUnit); err != nil {
		return err
	}
	for _, f := range files {
		unit, _ := fs.ReadFile("etc/systemd/system/ikigenba-" + f.service.Name + ".service")
		if string(f.data) == string(f.oldEnv) && string(unit) == string(f.oldUnit) {
			continue
		}
		disabled, err := apps.Disabled(ctx, env, f.service.Name)
		if err != nil {
			return err
		}
		if disabled {
			continue
		}
		result, err := execute("show", "--property=ActiveState", "ikigenba-"+f.service.Name+".socket")
		if err != nil {
			return err
		}
		for line := range strings.Lines(string(result.Stdout)) {
			if strings.TrimSuffix(line, "\n") == "ActiveState=active" {
				if _, err := execute("restart", "ikigenba-"+f.service.Name+".service"); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

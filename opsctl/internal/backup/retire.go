package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/opsctl/internal/apps"
	"github.com/ikigenba/ikigenba/opsctl/internal/cloud"
	"github.com/ikigenba/ikigenba/opsctl/internal/config"
	"github.com/ikigenba/ikigenba/opsctl/internal/host"
)

// RetireResult records the completed effects of a host retirement.
type RetireResult struct {
	Services          []string
	ServicesStopped   bool
	LitestreamStopped bool
	SyncedDatabases   []string
	Files             []FileResult
	Host              FileResult
	FailedStep        string
}

// Retire stops service writers and replication before taking the final file
// archives. It never restarts or otherwise reconciles units after a failure.
func Retire(ctx context.Context, env host.Env, cloudEnv cloud.Env, store config.Store) (RetireResult, error) {
	var outcome RetireResult
	_, region, err := fileBackupConfiguration(store, "")
	if err != nil {
		return outcome, err
	}
	if err := ctx.Err(); err != nil {
		return outcome, fmt.Errorf("retire host: %w", err)
	}
	if env.Now == nil {
		return outcome, errors.New("retire host: host time is not configured")
	}
	if env.Execute == nil {
		return outcome, errors.New("retire host: host execution is not configured")
	}
	if cloudEnv.Open == nil {
		return outcome, errors.New("retire host: cloud access is not configured")
	}
	client, err := cloudEnv.Open(ctx, region)
	if err != nil {
		return outcome, fmt.Errorf("open backup storage: %w", err)
	}
	if client == nil {
		return outcome, errors.New("open backup storage: cloud client is not configured")
	}
	if err := ctx.Err(); err != nil {
		return outcome, fmt.Errorf("retire host: %w", err)
	}

	services, err := apps.Discover(env.Root)
	if err != nil {
		outcome.FailedStep = "services"
		return outcome, fmt.Errorf("discover retirement services: %w", err)
	}
	for _, service := range services {
		if err := apps.ValidateName(service.Name); err != nil {
			outcome.FailedStep = "services"
			return outcome, fmt.Errorf("validate retirement service %q: %w", service.Name, err)
		}
	}

	fixed := env.Now()
	fixedEnv := env
	fixedEnv.Now = func() time.Time { return fixed }
	cachedCloud := cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}

	for _, service := range services {
		exists, inspectErr := retirementUnitExists(ctx, env, service.Name)
		if inspectErr != nil {
			outcome.FailedStep = "services"
			return outcome, inspectErr
		}
		if !exists {
			continue
		}
		if stopErr := retirementSystemctl(ctx, env, "stop "+service.Name+" service", "stop", "ikigenba-"+service.Name+".service"); stopErr != nil {
			outcome.FailedStep = "services"
			return outcome, stopErr
		}
		outcome.Services = append(outcome.Services, service.Name)
	}
	outcome.ServicesStopped = true

	if err := retirementSystemctl(ctx, env, "stop litestream.service", "stop", "litestream.service"); err != nil {
		outcome.FailedStep = "litestream"
		return outcome, err
	}
	outcome.LitestreamStopped = true

	outcome.Files, err = Files(ctx, fixedEnv, cachedCloud, store, "")
	if err != nil {
		if len(outcome.Files) > 0 && outcome.Files[len(outcome.Files)-1].Err != nil {
			outcome.FailedStep = outcome.Files[len(outcome.Files)-1].Service
		}
		return outcome, err
	}

	outcome.Host, err = HostBackup(ctx, fixedEnv, cachedCloud, store)
	if err != nil {
		if outcome.Host.Service != "" {
			outcome.FailedStep = "host"
		}
		return outcome, err
	}
	return outcome, nil
}

func retirementUnitExists(ctx context.Context, env host.Env, service string) (bool, error) {
	unit := "ikigenba-" + service + ".service"
	result, err := env.Execute(ctx, host.Command{
		Name: "systemctl",
		Args: []string{"show", "--property=LoadState", unit},
	})
	label := "inspect " + unit
	if err != nil {
		return false, retirementCommandError(label, result, err)
	}
	if result.ExitCode != 0 {
		return false, &host.CommandError{Label: label, Result: result}
	}
	loadState := ""
	for line := range strings.SplitSeq(strings.ReplaceAll(string(result.Stdout), "\r\n", "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && key == "LoadState" {
			loadState = value
		}
	}
	if loadState == "" {
		return false, fmt.Errorf("%s: response omitted LoadState", label)
	}
	return loadState != "not-found", nil
}

func retirementSystemctl(ctx context.Context, env host.Env, label string, args ...string) error {
	result, err := env.Execute(ctx, host.Command{Name: "systemctl", Args: args})
	if err != nil {
		return retirementCommandError(label, result, err)
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result}
	}
	return nil
}

func retirementCommandError(label string, result host.Result, err error) error {
	var commandErr *host.CommandError
	if errors.As(err, &commandErr) {
		return err
	}
	return &host.CommandError{Label: label, Result: result, Err: err}
}

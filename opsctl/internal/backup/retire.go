package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
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
	Disabled          []string
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
	// Capture every unit's initial state before the first stop. This also
	// prevents a socket stop from changing the state used for disabled reporting.
	type appUnits struct {
		name            string
		socket, service retirementUnitState
		disabled        bool
	}
	units := make([]appUnits, 0, len(services))
	for _, discovered := range services {
		socket, inspectErr := retirementInspectUnit(ctx, env, "ikigenba-"+discovered.Name+".socket")
		if inspectErr != nil {
			outcome.FailedStep = "services"
			return outcome, inspectErr
		}
		service, inspectErr := retirementInspectUnit(ctx, env, "ikigenba-"+discovered.Name+".service")
		if inspectErr != nil {
			outcome.FailedStep = "services"
			return outcome, inspectErr
		}
		disabled, inspectErr := apps.Disabled(ctx, env, discovered.Name)
		if inspectErr != nil {
			outcome.FailedStep = "services"
			return outcome, fmt.Errorf("inspect disabled app %q: %w", discovered.Name, inspectErr)
		}
		units = append(units, appUnits{name: discovered.Name, socket: socket, service: service, disabled: disabled})
	}
	stoppedSockets := make(map[string]bool, len(units))
	stoppedServices := make(map[string]bool, len(units))
	verified := make(map[string]bool, len(units))
	postStopInspection := false
	recordStopped := func() {
		for _, unit := range units {
			if (!unit.socket.exists || stoppedSockets[unit.name]) && (!unit.service.exists || stoppedServices[unit.name]) && (unit.socket.exists || unit.service.exists) && (!postStopInspection || verified[unit.name]) {
				outcome.Services = append(outcome.Services, unit.name)
				if unit.disabled && unit.socket.activeState == "inactive" && unit.service.activeState == "inactive" {
					outcome.Disabled = append(outcome.Disabled, unit.name)
				}
			}
		}
	}

	fixed := env.Now()
	fixedEnv := env
	fixedEnv.Now = func() time.Time { return fixed }
	cachedCloud := cloud.Env{Open: func(context.Context, string) (cloud.Client, error) { return client, nil }}

	for _, unit := range units {
		if !unit.socket.exists {
			continue
		}
		name := "ikigenba-" + unit.name + ".socket"
		if stopErr := retirementSystemctl(ctx, env, "stop "+name, "stop", name); stopErr != nil {
			outcome.FailedStep = "services"
			recordStopped()
			return outcome, stopErr
		}
		stoppedSockets[unit.name] = true
	}
	for _, unit := range units {
		if !unit.service.exists {
			continue
		}
		name := "ikigenba-" + unit.name + ".service"
		if stopErr := retirementSystemctl(ctx, env, "stop "+name, "stop", name); stopErr != nil {
			outcome.FailedStep = "services"
			recordStopped()
			return outcome, stopErr
		}
		stoppedServices[unit.name] = true
	}
	postStopInspection = true
	for _, unit := range units {
		if !unit.socket.exists && !unit.service.exists {
			continue
		}
		for _, entry := range []struct {
			exists bool
			suffix string
		}{{unit.socket.exists, ".socket"}, {unit.service.exists, ".service"}} {
			if !entry.exists {
				continue
			}
			name := "ikigenba-" + unit.name + entry.suffix
			state, inspectErr := retirementInspectUnit(ctx, env, name)
			if inspectErr != nil {
				outcome.FailedStep = "services"
				recordStopped()
				return outcome, inspectErr
			}
			if state.activeState != "inactive" {
				outcome.FailedStep = "services"
				recordStopped()
				return outcome, fmt.Errorf("%s is %s after stop", name, state.activeState)
			}
		}
		verified[unit.name] = true
	}
	recordStopped()
	outcome.ServicesStopped = true

	var syncErr error
	for _, service := range services {
		if service.Manifest == nil || service.Manifest.Database == nil {
			continue
		}
		databasePath := path.Join(env.Root, "/opt", service.Name, service.Manifest.Database.Path)
		if err := retirementSyncDatabase(ctx, env, databasePath); err != nil {
			syncErr = err
			break
		}
		outcome.SyncedDatabases = append(outcome.SyncedDatabases, path.Base(service.Manifest.Database.Path))
	}
	stopErr := retirementSystemctl(ctx, env, "stop litestream.service", "stop", "litestream.service")
	if stopErr == nil {
		var state retirementUnitState
		state, stopErr = retirementInspectUnit(ctx, env, "litestream.service")
		if stopErr == nil && state.activeState != "inactive" {
			stopErr = fmt.Errorf("litestream.service is %s after stop", state.activeState)
		}
	}
	if stopErr == nil {
		outcome.LitestreamStopped = true
	}
	if syncErr != nil {
		outcome.FailedStep = "litestream"
		return outcome, errors.Join(syncErr, stopErr)
	}
	if stopErr != nil {
		outcome.FailedStep = "litestream"
		return outcome, stopErr
	}

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

func retirementSyncDatabase(ctx context.Context, env host.Env, databasePath string) error {
	socketPath := path.Join(env.Root, "/var/run/litestream.sock")
	result, err := env.Execute(ctx, host.Command{
		Name: "litestream",
		Args: []string{"sync", "-wait", "-timeout", "60", "-socket", socketPath, "-json", databasePath},
	})
	label := "sync " + databasePath
	if err != nil {
		return retirementCommandError(label, result, err)
	}
	if result.ExitCode != 0 {
		return &host.CommandError{Label: label, Result: result}
	}
	if err := validateRetirementSyncProof(result.Stdout, databasePath); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

func validateRetirementSyncProof(output []byte, databasePath string) error {
	decoder := json.NewDecoder(bytes.NewReader(output))
	var proof map[string]json.RawMessage
	if err := decoder.Decode(&proof); err != nil || proof == nil {
		if err == nil {
			err = errors.New("proof is not a JSON object")
		}
		return fmt.Errorf("invalid synchronization proof: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("invalid synchronization proof: %w", err)
	}

	var provedPath string
	if raw, ok := proof["db_path"]; !ok || json.Unmarshal(raw, &provedPath) != nil || provedPath != databasePath {
		return errors.New("invalid synchronization proof: db_path does not match")
	}
	txid, err := retirementProofUint(proof, "txid")
	if err != nil {
		return err
	}
	replicaTxid, err := retirementProofUint(proof, "replica_txid")
	if err != nil {
		return err
	}
	if txid != replicaTxid {
		return errors.New("invalid synchronization proof: txid and replica_txid differ")
	}
	return nil
}

func retirementProofUint(proof map[string]json.RawMessage, field string) (uint64, error) {
	raw, ok := proof[field]
	if !ok {
		return 0, fmt.Errorf("invalid synchronization proof: %s is missing", field)
	}
	value, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid synchronization proof: %s is not an unsigned integer", field)
	}
	return value, nil
}

type retirementUnitState struct {
	exists      bool
	activeState string
}

func retirementInspectUnit(ctx context.Context, env host.Env, unit string) (retirementUnitState, error) {
	result, err := env.Execute(ctx, host.Command{
		Name: "systemctl",
		Args: []string{"show", "--property=LoadState", "--property=ActiveState", unit},
	})
	label := "inspect " + unit
	if err != nil {
		return retirementUnitState{}, retirementCommandError(label, result, err)
	}
	if result.ExitCode != 0 {
		return retirementUnitState{}, &host.CommandError{Label: label, Result: result}
	}
	loadState, activeState := "", ""
	for line := range strings.SplitSeq(strings.ReplaceAll(string(result.Stdout), "\r\n", "\n"), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			switch key {
			case "LoadState":
				loadState = value
			case "ActiveState":
				activeState = value
			}
		}
	}
	if loadState == "" || activeState == "" {
		return retirementUnitState{}, fmt.Errorf("%s: response omitted unit state", label)
	}
	return retirementUnitState{exists: loadState != "not-found", activeState: activeState}, nil
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

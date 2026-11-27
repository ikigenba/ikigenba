package cli_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
)

const backupAWSAccount = "111122223333"

type issuedBackupRole struct {
	request cloud.RoleRequest
	at      time.Time
	keys    cloud.Credentials
}

type backupWiringCloud struct {
	*memory.Cloud
	clock *drivenClock
	mu    sync.Mutex
	roles []issuedBackupRole
}

func (c *backupWiringCloud) Identity(context.Context) (cloud.Caller, error) {
	return cloud.Caller{Account: backupAWSAccount, Arn: "arn:aws:sts::" + backupAWSAccount + ":assumed-role/one-space/one"}, nil
}

func (c *backupWiringCloud) Account(context.Context) (string, error) {
	return backupAWSAccount, nil
}

func (c *backupWiringCloud) CallerOf(ctx context.Context, raw, binding string) (cloud.Caller, error) {
	caller, err := (boxCloud{c.Cloud}).CallerOf(ctx, raw, binding)
	if err != nil {
		return cloud.Caller{}, err
	}
	instance := filepath.Base(caller.Arn)
	caller.Account = backupAWSAccount
	caller.Arn = "arn:aws:sts::" + backupAWSAccount + ":assumed-role/one-hosts/" + instance
	return caller, nil
}

func (c *backupWiringCloud) AssumeRole(_ context.Context, req cloud.RoleRequest) (cloud.Credentials, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock.now()
	keys := cloud.Credentials{
		AccessKeyID: fmt.Sprintf("fixture-key-%d", len(c.roles)+1), SecretAccessKey: "fixture-secret", SessionToken: "fixture-session",
		Expires: now.Add(req.Duration),
	}
	c.roles = append(c.roles, issuedBackupRole{request: req, at: now, keys: keys})
	return keys, nil
}

func (c *backupWiringCloud) issuance(t *testing.T, id string) issuedBackupRole {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, issued := range c.roles {
		if issued.keys.AccessKeyID == id {
			return issued
		}
	}
	t.Fatalf("no AssumeRole call issued %q", id)
	return issuedBackupRole{}
}

func (c *backupWiringCloud) roleCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.roles)
}

// R-PLAM-5U68 (AWS)
func TestRunIssuesAndRefreshesAWSBackupCredentials(t *testing.T) {
	clock := newDrivenClock()
	hosts := settings.SandboxHosts
	hosts.Region, hosts.Domain, hosts.InstanceProfile = "us-west-2", "hosts.one.test", "one-hosts"
	mc, err := memory.New(memory.Config{
		Path: filepath.Join(t.TempDir(), "cloud.json"), Hosts: hosts, Now: clock.now, Rand: rand.NewChaCha8([32]byte{9}),
	})
	if err != nil {
		t.Fatal(err)
	}
	c := &backupWiringCloud{Cloud: mc, clock: clock}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "accountsd"), []byte("daemon fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "services")
	writeAccountsURL(t, path, "https://accounts.one.test")
	run := startIntegration(t, integrationOptions{
		now: clock.now, after: clock.after, sleep: clock.sleep, noSandbox: true, path: path, env: hostEnvironment(hosts),
		executable: func() (string, error) { return filepath.Join(bin, "accounts"), nil },
		cloud:      func(context.Context, string) (cloud.Cloud, error) { return c, nil },
	})
	run.call(tools.ToolCreateHost, struct{}{}, nil)
	args := tools.HostArgs{Host: "host01"}
	var host tools.HostRead
	driveUntil(t, clock, func() bool {
		run.call(tools.ToolHost, args, &host)
		return host.State == store.HostRunning && host.InstanceID != ""
	})
	get := func(signature int) *protocol.Backup {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, run.base+protocol.StatePath, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(hostauth.HeaderName, fakeHostIdentity(hosts.Region, host.InstanceID, clock.now(), signature))
		res, err := run.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("state request answered %d", res.StatusCode)
		}
		state, err := protocol.DecodeState(res.Body)
		if err != nil || state.Backup == nil {
			t.Fatalf("state omitted backup configuration: %v", err)
		}
		return state.Backup
	}
	before := get(1)
	issued := c.issuance(t, before.Host.AccessKeyID)
	if before.Bucket != "hosts-one-test-hosts-backups" || before.Region != "us-west-2" ||
		issued.request.RoleARN != "arn:aws:iam::111122223333:role/one-hosts-backup" {
		t.Fatalf("bucket %q, region %q, host role %q", before.Bucket, before.Region, issued.request.RoleARN)
	}
	initialCalls := c.roleCalls()
	cached := get(2)
	if calls := c.roleCalls(); cached.Host != before.Host || calls != initialCalls {
		t.Fatalf("state before refresh carried key %q and had %d AssumeRole calls, want key %q and %d calls",
			cached.Host.AccessKeyID, calls, before.Host.AccessKeyID, initialCalls)
	}
	clock.advance(access.ReissueAfter + time.Second)
	after := get(3)
	refreshed := c.issuance(t, after.Host.AccessKeyID)
	if after.Host.AccessKeyID == before.Host.AccessKeyID || refreshed.at.Before(clock.now()) {
		t.Fatalf("state after refresh carried key %q issued at %s, want a new key issued at %s", after.Host.AccessKeyID, refreshed.at, clock.now())
	}
}

// R-PLAM-5U68 (sandbox) R-3W54-93QJ
func TestRunSandboxBackupCredentialsAndDeletion(t *testing.T) {
	clock, run := sandboxWithBackupAccount(t, "")
	activeAt := clock.now()
	clock.advance(time.Duration(store.DefaultBackupSettings().EveryMinutes+1) * time.Minute)
	var backups tools.BackupStatusList
	driveUntil(t, clock, func() bool {
		run.call(tools.ToolBackupStatus, tools.BackupStatusArgs{}, &backups)
		return slices.ContainsFunc(backups.Accounts, func(backup tools.AccountBackups) bool {
			snapshot, err := time.Parse(time.RFC3339, backup.Snapshot)
			return backup.Account == "acme" && backup.Health == store.BackupHealthy && err == nil && snapshot.After(activeAt)
		})
	})
	account := tools.AccountArgs{Account: "acme"}
	placement := tools.CreateAccountArgs{Account: "acme", Host: "host01"}
	run.call(tools.ToolRemoveAccount, account, nil)
	driveUntil(t, clock, func() bool {
		var accounts tools.AccountList
		run.call(tools.ToolAccounts, struct{}{}, &accounts)
		return !slices.ContainsFunc(accounts.Accounts, func(entry tools.Account) bool { return entry.Account == "acme" })
	})
	run.refuse(tools.ToolCreateAccount, placement, tools.NameHasBackups)
	var deletion tools.BackupsDeletion
	run.call(tools.ToolDeleteAccountBackups, account, &deletion)
	if deletion.State != store.DeleteBackupsDeleting {
		t.Fatalf("backup deletion answered %+v", deletion)
	}
	driveUntil(t, clock, func() bool {
		run.call(tools.ToolBackupStatus, tools.BackupStatusArgs{}, &backups)
		return !slices.ContainsFunc(backups.Accounts, func(backup tools.AccountBackups) bool { return backup.Account == "acme" })
	})
	run.call(tools.ToolCreateAccount, placement, nil)
}

// R-PMII-JLWX
func TestRunSandboxBackupFileReachesRestorePoints(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	watch := watchSeedDirectory(t, stateDir)
	clock, run := sandboxWithBackupAccount(t, dir)
	const filename = "sandbox-backups"
	assertNoSeedCreation(t, watch, filename)
	path := filepath.Join(stateDir, filename)
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sandbox created the backups input path: %v", err)
	}
	const database = "data/app.db"
	dropped := clock.now()
	if err := os.WriteFile(path, []byte("host01 acme database "+database+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	driveUntil(t, clock, func() bool {
		var points tools.RestorePoints
		run.call(tools.ToolRestorePoints, tools.AccountArgs{Account: "acme"}, &points)
		return slices.ContainsFunc(points.Databases, func(point tools.DatabasePoint) bool {
			oldest, oldestErr := time.Parse(time.RFC3339, point.Oldest)
			synced, syncedErr := time.Parse(time.RFC3339, point.Synced)
			return point.Path == database && oldestErr == nil && syncedErr == nil &&
				!oldest.Before(dropped) && !synced.Before(oldest)
		})
	})
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sim did not remove the backups input path: %v", err)
	}
}

func sandboxWithBackupAccount(t *testing.T, dir string) (*drivenClock, *integrationRun) {
	t.Helper()
	clock := newDrivenClock()
	run := startIntegration(t, integrationOptions{dir: dir, now: clock.now, after: clock.after, sleep: clock.sleep})
	run.call(tools.ToolCreateHost, struct{}{}, nil)
	driveUntil(t, clock, func() bool {
		var host tools.HostRead
		run.call(tools.ToolHost, tools.HostArgs{Host: "host01"}, &host)
		return host.State == store.HostRunning && host.Health == store.HealthSync
	})
	run.call(tools.ToolCreateAccount, tools.CreateAccountArgs{Host: "host01", Account: "acme"}, nil)
	driveUntil(t, clock, func() bool {
		var accounts tools.AccountList
		run.call(tools.ToolAccounts, struct{}{}, &accounts)
		return slices.ContainsFunc(accounts.Accounts, func(entry tools.Account) bool {
			return entry.Account == "acme" && entry.State == store.AccountActive
		})
	})
	return clock, run
}

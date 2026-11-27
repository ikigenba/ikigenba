package linux

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"golang.org/x/sys/unix"
)

const streamUnitPath = "/etc/systemd/system/" + LitestreamUnit

const streamUnit = "[Unit]\nRequiresMountsFor=/srv\n\n[Service]\n" +
	"EnvironmentFile=/var/lib/accountsd/backup/%i.env\n" +
	"ExecStart=/usr/local/bin/litestream replicate -config /var/lib/accountsd/backup/%i.yml -no-expand-env\n" +
	"Restart=always\nRestartSec=5\nTimeoutStopSec=60\n"

func streamInstance(slug string) string { return "accountsd-litestream@" + slug + ".service" }

func backupLitestreamYAML(b *protocol.Backup, slug string, paths []string) []byte {
	var config strings.Builder
	fmt.Fprintf(&config, "snapshot:\n  interval: 24h\n  retention: %dh\nshutdown-sync-timeout: %ds\ndbs:\n", 24*b.Settings.WindowDays, ShutdownSyncTimeout/time.Second)
	for _, p := range paths {
		lines := []string{"/srv/" + slug + "/" + p, BackupDir + "/meta/" + slug + "/" + p, b.Bucket, protocol.DataPrefix(slug) + "/" + strings.TrimPrefix(p, "data/"), b.Region}
		quoted := make([][]byte, len(lines))
		for i, line := range lines {
			quoted[i], _ = json.Marshal(line)
		}
		fmt.Fprintf(&config, "  - path: %s\n    meta-path: %s\n    replica:\n      type: s3\n      bucket: %s\n      path: %s\n      region: %s\n", quoted[0], quoted[1], quoted[2], quoted[3], quoted[4])
	}
	return []byte(config.String())
}

func backupAWSEnv(c protocol.Credentials) []string {
	return []string{"AWS_ACCESS_KEY_ID=" + c.AccessKeyID, "AWS_SECRET_ACCESS_KEY=" + c.SecretAccessKey, "AWS_SESSION_TOKEN=" + c.SessionToken}
}

func backupLitestreamEnv(c protocol.Credentials, region string) []string {
	return append(backupAWSEnv(c), "AWS_REGION="+region)
}

func (h *Host) litestreamCommand(ctx context.Context, c protocol.Credentials, region string, args []string, stdout io.Writer) (Result, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	return h.query(ctx, Command{Name: h.path(LitestreamPath), Args: args, Env: backupLitestreamEnv(c, region), Stdout: stdout}, 0)
}

func streamEnv(c protocol.Credentials, region, digest string) string {
	return "# expires " + c.Expires.UTC().Format(time.RFC3339) + "\n# litestream " + digest + "\n" + strings.Join(backupLitestreamEnv(c, region), "\n") + "\n"
}

func streamCredentialsChanged(old, newEnv string) bool {
	current := strings.SplitN(old, "\n", 6)
	next := strings.SplitN(newEnv, "\n", 6)
	return len(current) < 6 || !slices.Equal(current[2:5], next[2:5])
}

func restartStreamEnv(old string, newEnv string, now time.Time) bool {
	oldExpiry, oldRest, _ := strings.Cut(old, "\n")
	oldStamp, _, _ := strings.Cut(oldRest, "\n")
	_, newRest, _ := strings.Cut(newEnv, "\n")
	newStamp, _, _ := strings.Cut(newRest, "\n")
	if oldStamp != newStamp {
		return true
	}
	stamp, found := strings.CutPrefix(oldExpiry, "# expires ")
	expires, err := time.Parse(time.RFC3339, stamp)
	return found && err == nil && expires.Before(now.Add(RestartBefore)) && streamCredentialsChanged(old, newEnv)
}

func walPaths(walk backupWalk) []string {
	var paths []string
	for _, db := range walk.Databases {
		if db.WAL {
			paths = append(paths, db.Path)
		}
	}
	return paths
}

// Stream discovers SQLite replicas, reconciles their units, and records replica health.
func (h *Host) Stream(ctx context.Context, s protocol.DesiredState) error {
	ctx, pruneErr := h.beginBackupFacts(ctx, s)
	b := s.Backup
	ctx = withBackupDiagnosticRedactor(ctx, b)
	dirErr := h.privateBackupDir(BackupDir)
	if !backupReady(b) {
		if dirErr == nil {
			dirErr = h.cleanStreamAccounts(ctx, nil, nil)
		}
		if err := firstBackupError(dirErr, pruneErr); err != nil && h.env.Stderr != nil {
			_, _ = fmt.Fprintf(h.env.Stderr, "accountsd: %s\n", redactBackupDiagnostic(backupDiagnosticRedactor(ctx), err))
		}
		return nil
	}
	installErr := h.ensureLitestream(ctx)
	passErr := errors.Join(pruneErr, dirErr, installErr)
	if dirErr == nil {
		passErr = firstBackupError(passErr, h.updateHostBackup(func(facts *backupHostFacts) {
			facts.LitestreamInstallError = h.litestreamInstall.reportError("")
		}))
	}
	retain := make(map[string]bool, len(s.Accounts))
	accountErrors := make(map[string]error, len(s.Accounts))
	var unitWritten bool
	var unitErr error
	accounts := backupActiveAccounts(s)
	for _, account := range accounts {
		slug := account.Slug
		walk, walkErr := h.walkBackupDatabases(slug)
		walk, saveErr := h.recordBackupWalk(ctx, slug, walk, dirErr == nil)
		passErr = firstBackupError(passErr, firstBackupError(walkErr, saveErr))
		creds, ok := accountBackupCredentials(b, slug)
		paths := walPaths(walk)
		retain[slug] = !ok || len(paths) != 0
		if !ok {
			continue
		}
		accountErrors[slug] = walkErr
		if len(paths) == 0 {
			continue
		}
		config := BackupDir + "/" + slug + ".yml"
		changed, configErr := h.writeBackupFile(config, backupLitestreamYAML(b, slug, paths))
		if configErr != nil {
			accountErrors[slug] = firstBackupError(walkErr, hostError(config, configErr))
			continue
		}
		if installErr != nil {
			continue
		}
		if !unitWritten {
			unitWritten = true
			unitErr = h.prepareStreamUnit(ctx)
			passErr = firstBackupError(passErr, unitErr)
		}
		if unitErr == nil {
			accountErr, otherErr := h.streamAccount(ctx, b, account, creds, config, changed, walk, saveErr == nil)
			accountErrors[slug] = firstBackupError(walkErr, accountErr)
			passErr = firstBackupError(passErr, otherErr)
		}
	}
	if dirErr == nil {
		passErr = firstBackupError(passErr, h.cleanStreamAccounts(ctx, retain, accountErrors))
	}
	for _, account := range accounts {
		if accountErr, attempted := accountErrors[account.Slug]; attempted {
			accountErr = redactBackupDiagnostic(backupDiagnosticRedactor(ctx), accountErr)
			passErr = firstBackupError(passErr, accountErr)
			passErr = firstBackupError(passErr, h.recordBackupPassError(ctx, account.Slug, true, accountErr))
		}
	}
	return passErr
}

func (h *Host) prepareStreamUnit(ctx context.Context) error {
	if err := h.backupDirectory(filepath.Dir(streamUnitPath), "", true); err != nil {
		return err
	}
	changed, err := h.writeAtomic(streamUnitPath, []byte(streamUnit), 0o644)
	if err != nil || !changed {
		return err
	}
	_, err = h.run(ctx, "systemctl", "daemon-reload")
	return err
}

func streamRestartPath(slug string) string { return BackupDir + "/" + slug + ".restart-gid" }

func (h *Host) streamAccount(ctx context.Context, b *protocol.Backup, account protocol.Account, c protocol.Credentials, config string, changed bool, walk backupWalk, kept bool) (accountErr, otherErr error) {
	slug := account.Slug
	restartPath := streamRestartPath(slug)
	lastGID, _ := h.readBounded(restartPath)
	previousGID, parseErr := strconv.ParseInt(strings.TrimSuffix(string(lastGID), "\n"), 10, 64)
	gidChanged := parseErr == nil && previousGID != account.GID
	active, err := h.query(ctx, Command{Name: "systemctl", Args: []string{"is-active", streamInstance(slug)}})
	if err != nil {
		return streamCommandFailure("systemctl is-active", active, err), nil
	}
	stamp, err := h.readBounded(BackupDir + "/litestream.digest")
	if err != nil {
		return nil, err
	}
	digest, _, _ := strings.Cut(string(stamp), "\n")
	newEnv := streamEnv(c, b.Region, digest)
	envPath := BackupDir + "/" + slug + ".env"
	previous, readErr := h.readBounded(envPath)
	if gidChanged {
		if err := h.privateBackupDir(BackupDir + "/meta"); err != nil {
			return hostError(BackupDir+"/meta/"+slug, err), nil
		}
		if err := h.removeStreamMeta(slug); err != nil {
			return err, nil
		}
	} else if err := h.privateBackupDir(BackupDir + "/meta/" + slug); err != nil {
		return hostError(BackupDir+"/meta/"+slug, err), nil
	}
	if active.ExitCode != 0 || changed || readErr != nil || restartStreamEnv(string(previous), newEnv, h.env.Now()) || gidChanged {
		if _, err := h.writeBackupFile(envPath, []byte(newEnv)); err != nil {
			return hostError(envPath, err), nil
		}
		if result, err := h.run(ctx, "systemctl", "restart", streamInstance(slug)); err != nil {
			return streamCommandFailure("systemctl restart", result, err), nil
		}
		_, otherErr = h.writeBackupFile(restartPath, []byte(strconv.FormatInt(account.GID, 10)+"\n"))
	}
	if walk.Error == machine.DataTooLarge {
		return nil, otherErr
	}
	for i := range walk.Databases {
		db := &walk.Databases[i]
		if db.WAL {
			accountErr = firstBackupError(accountErr, h.streamDatabase(ctx, b, slug, c, config, db))
		}
	}
	if kept {
		otherErr = firstBackupError(otherErr, h.saveStreamHealth(ctx, slug, walk))
	}
	return accountErr, otherErr
}

func (h *Host) cleanStreamAccounts(ctx context.Context, retain map[string]bool, accountErrors map[string]error) error {
	dir, err := openBackupDirectory(unix.AT_FDCWD, h.path(BackupDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	var passErr error
	for {
		entries, err := dir.ReadDir(ReadBatch)
		for _, entry := range entries {
			slug, ok := strings.CutSuffix(entry.Name(), ".yml")
			if ok && !retain[slug] && valid.Slug(slug) == nil && !entry.IsDir() {
				accountErr, otherErr := h.removeStreamAccount(ctx, slug)
				if earlier, backedUp := accountErrors[slug]; backedUp {
					accountErrors[slug] = firstBackupError(earlier, accountErr)
				}
				passErr = firstBackupError(passErr, firstBackupError(accountErr, otherErr))
			}
		}
		if errors.Is(err, io.EOF) {
			return passErr
		}
		if err != nil {
			return firstBackupError(passErr, err)
		}
	}
}

func (h *Host) removeStreamAccount(ctx context.Context, slug string) (accountErr, otherErr error) {
	active, err := h.query(ctx, Command{Name: "systemctl", Args: []string{"is-active", streamInstance(slug)}})
	if err != nil {
		accountErr = streamCommandFailure("systemctl is-active", active, err)
	} else if active.ExitCode == 0 {
		result, err := h.run(ctx, "systemctl", "stop", streamInstance(slug))
		accountErr = streamCommandFailure("systemctl stop", result, err)
	}
	_, ymlErr := h.remove(BackupDir + "/" + slug + ".yml")
	_, envErr := h.remove(BackupDir + "/" + slug + ".env")
	metaErr := h.removeStreamMeta(slug)
	_, restartErr := h.remove(streamRestartPath(slug))
	return firstBackupError(accountErr, firstBackupError(ymlErr, firstBackupError(envErr, metaErr))), restartErr
}

func streamCommandFailure(label string, result Result, err error) error {
	if err == nil {
		return nil
	}
	var failed *CommandError
	if errors.As(err, &failed) {
		err = failed.Err
	}
	return backupCommandFailure(label, result, err)
}

func (h *Host) removeStreamMeta(slug string) error {
	meta := BackupDir + "/meta"
	info, err := os.Lstat(h.path(meta))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return hostError(meta, err)
	}
	if !info.IsDir() {
		return nil
	}
	path := filepath.Join(meta, slug)
	if err := os.RemoveAll(h.path(path)); err != nil {
		return hostError(path, err)
	}
	return nil
}

func (h *Host) streamDatabase(ctx context.Context, b *protocol.Backup, slug string, c protocol.Credentials, config string, db *backupDatabase) error {
	now := h.env.Now()
	hostPath := "/srv/" + slug + "/" + db.Path
	configPath, dbPath := h.path(config), h.path(hostPath)
	var local uint64
	var localOK bool
	status := newBackupJSONArray([]string{"database", "status", "local_txid"}, func(row map[string]string) {
		if row["database"] == hostPath && row["status"] == "ok" {
			local, localOK = backupTxID(row["local_txid"])
		}
	})
	statusResult, statusErr := h.litestreamCommand(ctx, c, b.Region, []string{"status", "-config", configPath, "-no-expand-env", "-json", dbPath}, status)
	status.finish()

	var written, oldest time.Time
	var newestTX uint64
	var hasTX bool
	level0 := newBackupJSONArray([]string{"timestamp", "max_txid"}, func(row map[string]string) {
		if at, err := time.Parse(time.RFC3339, row["timestamp"]); err == nil && at.After(written) {
			written = at
		}
		if tx, ok := backupTxID(row["max_txid"]); ok {
			newestTX, hasTX = max(newestTX, tx), true
		}
	})
	level0Result, level0Err := h.litestreamCommand(ctx, c, b.Region, []string{"ltx", "-config", configPath, "-no-expand-env", "-level", "0", "-json", dbPath}, level0)
	level0.finish()
	level9 := newBackupJSONArray([]string{"timestamp"}, func(row map[string]string) {
		if at, err := time.Parse(time.RFC3339, row["timestamp"]); err == nil && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
	})
	level9Result, level9Err := h.litestreamCommand(ctx, c, b.Region, []string{"ltx", "-config", configPath, "-no-expand-env", "-level", "9", "-json", dbPath}, level9)
	level9.finish()
	switch {
	case statusErr != nil:
		return streamCommandFailure("litestream status", statusResult, statusErr)
	case level0Err != nil:
		return streamCommandFailure("litestream ltx", level0Result, level0Err)
	case level9Err != nil:
		return streamCommandFailure("litestream ltx", level9Result, level9Err)
	}
	if !written.IsZero() {
		db.Written = written
	}
	db.Oldest = oldest
	if localOK && hasTX && newestTX >= local {
		db.Synced = now
	}
	return nil
}

func backupTxID(s string) (uint64, bool) {
	id, err := strconv.ParseUint(s, 16, 64)
	return id, err == nil
}

func (h *Host) saveStreamHealth(ctx context.Context, slug string, walk backupWalk) error {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	placement := h.currentBackupPlacement(ctx, slug)
	if placement == nil || placement.walk.blocked {
		return nil
	}
	facts, _ := h.loadAccountBackup(placement.account)
	if !facts.matchesWalk(placement.walk.identity) {
		return nil
	}
	for _, updated := range walk.Databases {
		i, ok := slices.BinarySearchFunc(facts.Walk.Databases, updated.Path, func(d backupDatabase, path string) int { return cmp.Compare(d.Path, path) })
		if ok && facts.Walk.Databases[i].WAL {
			facts.Walk.Databases[i] = updated
		}
	}
	return h.keepBackupWalkHealth(placement, facts)
}

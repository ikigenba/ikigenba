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
	slugs := backupAccountSlugs(s)
	h.pruneBackupWalks(slugs)
	b := s.Backup
	dirErr := h.privateBackupDir(BackupDir)
	if !backupReady(b) {
		if dirErr == nil {
			_ = h.cleanStreamAccounts(ctx, nil)
		}
		return nil
	}
	ctx = withBackupDiagnosticRedactor(ctx, b)
	installErr := h.ensureLitestream(ctx)
	passErr := errors.Join(dirErr, installErr)
	if dirErr == nil {
		passErr = firstBackupError(passErr, h.updateHostBackup(func(facts *backupHostFacts) {
			facts.LitestreamInstallError = boundBackupError(h.litestreamInstall.errorText())
		}))
	}
	retain := make(map[string]bool, len(s.Accounts))
	var unitWritten bool
	var unitErr error
	for _, slug := range slugs {
		walk, walkErr := h.walkBackupDatabases(slug)
		saveErr := h.recordBackupWalk(slug, walk, dirErr == nil)
		passErr = firstBackupError(passErr, firstBackupError(walkErr, saveErr))
		if dirErr != nil {
			continue
		}
		if walk.Error == machine.DataTooLarge {
			facts, state, _ := h.accountBackupForPass(slug)
			if state != walkKept {
				retain[slug] = true
				continue
			}
			walk = facts.Walk
		}
		creds, ok := accountBackupCredentials(b, slug)
		paths := walPaths(walk)
		retain[slug] = !ok || len(paths) != 0
		if !ok || len(paths) == 0 || installErr != nil {
			continue
		}
		if !unitWritten {
			unitWritten = true
			unitErr = h.prepareStreamUnit(ctx)
			passErr = firstBackupError(passErr, unitErr)
		}
		if unitErr == nil {
			passErr = firstBackupError(passErr, h.streamAccount(ctx, b, slug, creds, paths, walk.Error != machine.DataTooLarge))
		}
	}
	if dirErr != nil {
		return passErr
	}
	return firstBackupError(passErr, h.cleanStreamAccounts(ctx, retain))
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

func (h *Host) streamAccount(ctx context.Context, b *protocol.Backup, slug string, c protocol.Credentials, paths []string, refresh bool) error {
	config := BackupDir + "/" + slug + ".yml"
	if err := h.privateBackupDir(BackupDir + "/meta/" + slug); err != nil {
		return err
	}
	changed, err := h.writeBackupFile(config, backupLitestreamYAML(b, slug, paths))
	if err != nil {
		return err
	}
	active, err := h.query(ctx, Command{Name: "systemctl", Args: []string{"is-active", streamInstance(slug)}})
	if err != nil {
		return err
	}
	stamp, err := h.readBounded(BackupDir + "/litestream.digest")
	if err != nil {
		return err
	}
	digest, _, _ := strings.Cut(string(stamp), "\n")
	newEnv := streamEnv(c, b.Region, digest)
	envPath := BackupDir + "/" + slug + ".env"
	previous, readErr := h.readBounded(envPath)
	if active.ExitCode != 0 || changed || readErr != nil || restartStreamEnv(string(previous), newEnv, h.env.Now()) {
		if _, err := h.writeBackupFile(envPath, []byte(newEnv)); err != nil {
			return err
		}
		if _, err := h.run(ctx, "systemctl", "restart", streamInstance(slug)); err != nil {
			return err
		}
	}
	if !refresh {
		return nil
	}
	facts := h.readAccountBackup(slug)
	identity, err := facts.walkIdentity()
	if err != nil {
		return err
	}
	var passErr error
	for i := range facts.Walk.Databases {
		db := &facts.Walk.Databases[i]
		if db.WAL {
			passErr = firstBackupError(passErr, h.streamDatabase(ctx, b, slug, c, config, db))
		}
	}
	return firstBackupError(passErr, h.saveStreamHealth(slug, facts.Walk, identity))
}

func (h *Host) cleanStreamAccounts(ctx context.Context, retain map[string]bool) error {
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
				passErr = firstBackupError(passErr, h.removeStreamAccount(ctx, slug))
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

func (h *Host) removeStreamAccount(ctx context.Context, slug string) error {
	active, err := h.query(ctx, Command{Name: "systemctl", Args: []string{"is-active", streamInstance(slug)}})
	if err == nil && active.ExitCode == 0 {
		_, err = h.run(ctx, "systemctl", "stop", streamInstance(slug))
	}
	_, ymlErr := h.remove(BackupDir + "/" + slug + ".yml")
	_, envErr := h.remove(BackupDir + "/" + slug + ".env")
	meta := BackupDir + "/meta"
	info, metaErr := os.Lstat(h.path(meta))
	if metaErr == nil && info.IsDir() {
		metaErr = os.RemoveAll(h.path(filepath.Join(meta, slug)))
	}
	if errors.Is(metaErr, fs.ErrNotExist) {
		metaErr = nil
	}
	return errors.Join(err, ymlErr, envErr, metaErr)
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
	_, statusErr := h.litestreamCommand(ctx, c, b.Region, []string{"status", "-config", configPath, "-no-expand-env", "-json", dbPath}, status)
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
	_, level0Err := h.litestreamCommand(ctx, c, b.Region, []string{"ltx", "-config", configPath, "-no-expand-env", "-level", "0", "-json", dbPath}, level0)
	level0.finish()
	level9 := newBackupJSONArray([]string{"timestamp"}, func(row map[string]string) {
		if at, err := time.Parse(time.RFC3339, row["timestamp"]); err == nil && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
	})
	_, level9Err := h.litestreamCommand(ctx, c, b.Region, []string{"ltx", "-config", configPath, "-no-expand-env", "-level", "9", "-json", dbPath}, level9)
	level9.finish()
	if err := errors.Join(statusErr, level0Err, level9Err); err != nil {
		return err
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

func (h *Host) saveStreamHealth(slug string, walk backupWalk, identity backupWalkIdentity) error {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	facts := h.readAccountBackup(slug)
	if identity != h.backupWalks[slug].identity || !facts.matchesWalk(identity) {
		return nil
	}
	for _, updated := range walk.Databases {
		i, ok := slices.BinarySearchFunc(facts.Walk.Databases, updated.Path, func(d backupDatabase, path string) int { return cmp.Compare(d.Path, path) })
		if ok && facts.Walk.Databases[i].WAL {
			facts.Walk.Databases[i] = updated
		}
	}
	return h.keepBackupWalkHealth(slug, facts)
}

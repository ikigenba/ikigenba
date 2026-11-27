package linux

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

const backupStatusDir = BackupDir + "/status"

type backupDatabase struct {
	protocol.Database
	WAL bool
}

type backupWalk struct {
	Databases []backupDatabase
	Total     int
	Error     string
}

type backupAccountFacts struct {
	Snapshot     time.Time
	Snapshots    []int64
	Checked      time.Time
	Error        string
	Walk         backupWalk
	WalkRecorded bool
	Operation    backupOperationFacts
}

type backupHostFacts struct {
	Snapshot               time.Time
	Error                  string
	KeyDigests             [2]string
	ResticInstallError     string
	LitestreamInstallError string
	Operation              backupOperationFacts
}

func backupReady(b *protocol.Backup) bool {
	return b != nil && b.Settings.EveryMinutes > 0 && len(b.Bucket) >= 3 && len(b.Bucket) <= 63 &&
		onlyBytes(b.Bucket, "abcdefghijklmnopqrstuvwxyz0123456789.-") &&
		b.Bucket[0] != '.' && b.Bucket[0] != '-' && b.Bucket[len(b.Bucket)-1] != '.' && b.Bucket[len(b.Bucket)-1] != '-' &&
		onlyBytes(b.Region, "abcdefghijklmnopqrstuvwxyz0123456789-")
}

func onlyBytes(s, chars string) bool {
	for i := range len(s) {
		if strings.IndexByte(chars, s[i]) < 0 {
			return false
		}
	}
	return s != ""
}

func usableBackupCredentials(c protocol.Credentials) bool {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/="
	return onlyBytes(c.AccessKeyID, chars) && onlyBytes(c.SecretAccessKey, chars) && onlyBytes(c.SessionToken, chars)
}

func accountBackupCredentials(b *protocol.Backup, slug string) (protocol.Credentials, bool) {
	if b != nil {
		for _, a := range b.Accounts {
			if a.Account == slug && usableBackupCredentials(a.Credentials) {
				return a.Credentials, true
			}
		}
	}
	return protocol.Credentials{}, false
}

func hostBackupCredentials(b *protocol.Backup) bool {
	return b != nil && b.HostPrefix != "" && usableBackupCredentials(b.Host)
}

func (h *Host) privateBackupDir(path string) error {
	info, err := os.Lstat(h.path(path))
	if errors.Is(err, fs.ErrNotExist) {
		if path == BackupDir || path == ResticCache {
			if err := h.mkdir(filepath.Dir(path)); err != nil {
				return err
			}
		} else if err := h.privateBackupDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := os.Mkdir(h.path(path), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return hostError(path, err)
		}
		info, err = os.Lstat(h.path(path))
	}
	if err != nil {
		return hostError(path, err)
	}
	if !info.IsDir() {
		return hostError(path, fs.ErrInvalid)
	}
	return h.mode(path, 0o700)
}

func (h *Host) writeBackupFile(path string, value []byte) (bool, error) {
	if err := h.privateBackupDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	return h.writeAtomic(path, value, 0o600)
}

func readBackupFacts[T any](h *Host, path string) T {
	var facts T
	data, err := h.readBounded(path)
	if err != nil || json.Unmarshal(data, &facts) != nil {
		var zero T
		return zero
	}
	return facts
}

func (h *Host) readAccountBackup(slug string) backupAccountFacts {
	if valid.Slug(slug) != nil {
		return backupAccountFacts{}
	}
	return readBackupFacts[backupAccountFacts](h, backupStatusDir+"/"+slug+".json")
}

func (h *Host) readHostBackup() backupHostFacts {
	return readBackupFacts[backupHostFacts](h, backupStatusDir+"/.host.json")
}

func saveBackupFacts(h *Host, path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = h.writeBackupFile(path, data)
	return err
}

func (h *Host) updateAccountBackup(slug string, update func(*backupAccountFacts)) error {
	if valid.Slug(slug) != nil {
		return machine.ErrInvalidState
	}
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	facts := h.readAccountBackup(slug)
	update(&facts)
	return saveBackupFacts(h, backupStatusDir+"/"+slug+".json", facts)
}

func (h *Host) updateHostBackup(update func(*backupHostFacts)) error {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	facts := h.readHostBackup()
	update(&facts)
	return saveBackupFacts(h, backupStatusDir+"/.host.json", facts)
}

func backupTime(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

func boundBackupError(message string) string {
	report := protocol.Report{HostBackup: protocol.HostBackup{Error: message}}
	return report.Bounded().HostBackup.Error
}

func backupAccountSlugs(s protocol.DesiredState) []string {
	var slugs []string
	for _, a := range s.Accounts {
		if a.State == protocol.AccountActive && valid.Slug(a.Slug) == nil {
			slugs = append(slugs, a.Slug)
		}
	}
	slices.Sort(slugs)
	return slices.Compact(slugs)
}

func (h *Host) backupReport(s protocol.DesiredState, out *protocol.Report) {
	slugs := backupAccountSlugs(s)
	if s.Backup != nil && s.Backup.Restore != nil {
		slugs = nil
		for _, a := range s.Backup.Restore.Accounts {
			slugs = append(slugs, a.Slug)
		}
		slices.Sort(slugs)
	}
	host := h.readHostBackup()
	out.HostBackup = protocol.HostBackup{Snapshot: backupTime(host.Snapshot), Error: host.Error}
	if !backupReady(s.Backup) || !hostBackupCredentials(s.Backup) {
		out.HostBackup.Error = machine.NoCredentials
	}
	accounts := make(map[string]backupAccountFacts, min(len(slugs), protocol.MaxBackupAccounts))
	resticError, litestreamError := host.ResticInstallError, host.LitestreamInstallError
	if current := h.resticInstall.errorText(); current != "" {
		resticError = boundBackupError(current)
	}
	if current := h.litestreamInstall.errorText(); current != "" {
		litestreamError = boundBackupError(current)
	}
	for i, slug := range slugs {
		var facts backupAccountFacts
		if i < protocol.MaxBackupAccounts {
			facts = h.readAccountBackup(slug)
			accounts[slug] = facts
		}
		_, credentials := accountBackupCredentials(s.Backup, slug)
		out.Backups = append(out.Backups, accountBackupReport(slug, facts, backupReady(s.Backup) && credentials, resticError, litestreamError))
	}
	h.operationBackupReport(s, out, host, accounts)
}

func accountBackupReport(slug string, facts backupAccountFacts, credentials bool, resticError, litestreamError string) protocol.AccountBackup {
	entry := protocol.AccountBackup{
		Account: slug, Snapshot: backupTime(facts.Snapshot), Snapshots: facts.Snapshots, Checked: backupTime(facts.Checked),
		DatabaseTotal: facts.Walk.Total, DatabaseErrors: max(0, facts.Walk.Total-len(facts.Walk.Databases)),
	}
	switch {
	case !credentials:
		entry.Error = machine.NoCredentials
	case facts.Walk.Error != "":
		entry.Error = facts.Walk.Error
	case resticError != "":
		entry.Error = resticError
	default:
		entry.Error = facts.Error
	}
	for _, db := range facts.Walk.Databases {
		current := db.Database
		current.Oldest, current.Written, current.Synced = backupTime(db.Oldest), backupTime(db.Written), backupTime(db.Synced)
		if db.WAL {
			current.Error = litestreamError
			if !current.Synced.IsZero() && (entry.Synced.IsZero() || current.Synced.Before(entry.Synced)) {
				entry.Synced = current.Synced
			}
			if current.Written.After(entry.Written) {
				entry.Written = current.Written
			}
		} else {
			current.Error = machine.NotWAL
		}
		if current.Error != "" {
			entry.DatabaseErrors++
		}
		entry.Databases = append(entry.Databases, current)
	}
	return entry
}

// firstBackupError retains one representative failure while the pass continues
// running each independent account and host repository.
func firstBackupError(previous, current error) error {
	if previous != nil {
		return previous
	}
	return current
}

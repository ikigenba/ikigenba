package linux

import (
	"cmp"
	"crypto/sha256"
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
	WalkGID      *int64 `json:",omitempty"`
	WalkToken    string `json:",omitempty"`
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
	privateRoot := BackupDir
	if path == ResticCache || strings.HasPrefix(path, ResticCache+"/") {
		privateRoot = ResticCache
	}
	return h.backupDirectory(path, privateRoot, true)
}

// backupDirectory checks each component before a pass can write through it.
// Existing public parents are left alone. Only a private tree gets mode repairs.
func (h *Host) backupDirectory(path, privateRoot string, create bool) error {
	parent := filepath.Dir(path)
	if parent == path {
		return nil
	}
	if err := h.backupDirectory(parent, privateRoot, create); err != nil {
		return err
	}
	private := privateRoot != "" && (path == privateRoot || strings.HasPrefix(path, privateRoot+"/"))
	mode := fs.FileMode(0o755)
	if private {
		mode = 0o700
	}
	full := h.path(path)
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return nil
		}
		err = os.Mkdir(full, mode)
		if err == nil {
			return h.mode(path, mode)
		}
		if errors.Is(err, fs.ErrExist) {
			info, err = os.Lstat(full)
		}
	}
	if err != nil {
		return hostError(path, err)
	}
	if !info.IsDir() {
		return hostError(path, fs.ErrInvalid)
	}
	if private {
		return h.mode(path, mode)
	}
	return nil
}

func (h *Host) writeBackupFile(path string, value []byte) (bool, error) {
	if err := h.privateBackupDir(filepath.Dir(path)); err != nil {
		return false, err
	}
	return h.writeAtomic(path, value, 0o600)
}

func readBackupFacts[T any](h *Host, path string) T {
	facts, _ := loadBackupFacts[T](h, path)
	return facts
}

func loadBackupFacts[T any](h *Host, path string) (T, error) {
	var facts T
	data, err := h.readBounded(path)
	if err != nil {
		return facts, err
	}
	if err := json.Unmarshal(data, &facts); err != nil {
		var zero T
		return zero, hostError(path, err)
	}
	return facts, nil
}

func (h *Host) readAccountBackup(slug string) backupAccountFacts {
	if valid.Slug(slug) != nil {
		return backupAccountFacts{}
	}
	return readBackupFacts[backupAccountFacts](h, backupStatusDir+"/"+slug+".json")
}

type backupWalkState uint8

const (
	walkNotAttempted backupWalkState = iota
	walkUnrecorded
	walkKept
)

type backupWalkIdentity struct {
	token  string
	digest backupWalkDigest
}

type backupWalkDigest [sha256.Size]byte

type backupWalkPublication struct {
	identity     backupWalkIdentity
	readiness    backupWalkReadiness
	gid          int64
	inactive     bool
	blocked      bool
	ignoreLegacy bool
}

// All WAL databases must be synced after the lock. Save only their oldest
// sync time here so rejected status cannot supply paths or close readiness.
type backupWalkReadiness struct {
	known bool
	wal   bool
	sync  time.Time
}

func (walk backupWalk) readiness() backupWalkReadiness {
	r := backupWalkReadiness{known: true}
	for _, db := range walk.Databases {
		if db.WAL && (!r.wal || db.Synced.Before(r.sync)) {
			r.wal, r.sync = true, db.Synced
		}
	}
	return r
}

func (r backupWalkReadiness) syncedAfter(at time.Time) bool {
	return r.known && (!r.wal || r.sync.After(at))
}

func (facts backupAccountFacts) walkIdentity() (backupWalkIdentity, error) {
	digest, err := facts.Walk.digest()
	return backupWalkIdentity{token: facts.WalkToken, digest: digest}, err
}

func (walk backupWalk) digest() (backupWalkDigest, error) {
	data, err := json.Marshal(walk)
	if err != nil {
		return backupWalkDigest{}, err
	}
	return sha256.Sum256(data), nil
}

// Read the walk and its publication state together. Only the current pass's
// read of the saved walk may supply a manifest or exclusions.
func (h *Host) accountBackupForPass(account protocol.Account) (backupAccountFacts, backupWalkState, backupWalkReadiness) {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	facts := h.readAccountBackup(account.Slug)
	facts.clearWalkForOtherPlacement(account.GID)
	if facts.WalkGID == nil && !h.legacyWalkBelongsTo(account) {
		facts.clearWalk()
	}
	kept, attempted := h.backupWalks[account.Slug]
	if !attempted {
		if facts.WalkRecorded {
			return facts, walkNotAttempted, facts.Walk.readiness()
		}
		return facts, walkNotAttempted, backupWalkReadiness{}
	}
	if kept.inactive || kept.gid != account.GID {
		return facts, walkNotAttempted, backupWalkReadiness{}
	}
	if kept.blocked || !facts.matchesWalk(kept.identity) {
		return facts, walkUnrecorded, kept.readiness
	}
	return facts, walkKept, kept.readiness
}

func (facts *backupAccountFacts) clearWalkForOtherPlacement(gid int64) {
	// A legacy status file has no GID. Use it until the next publication
	// binds the walk to an account placement.
	if facts.WalkGID != nil && *facts.WalkGID != gid {
		facts.clearWalk()
	}
}

func (facts *backupAccountFacts) clearWalk() {
	facts.Walk, facts.WalkRecorded, facts.WalkGID, facts.WalkToken = backupWalk{}, false, nil, ""
}

func (facts backupAccountFacts) matchesWalk(kept backupWalkIdentity) bool {
	if kept.token == "" || !facts.WalkRecorded || facts.WalkToken != kept.token {
		return false
	}
	identity, err := facts.walkIdentity()
	return err == nil && identity == kept
}

// keepBackupWalkHealth updates the authenticated publication under backupFactsMu.
func (h *Host) keepBackupWalkHealth(slug string, facts backupAccountFacts) error {
	identity, err := facts.walkIdentity()
	if err != nil {
		return err
	}
	if err := saveBackupFacts(h, backupStatusDir+"/"+slug+".json", facts); err != nil {
		return err
	}
	publication := h.backupWalks[slug]
	publication.identity, publication.readiness = identity, facts.Walk.readiness()
	h.backupWalks[slug] = publication
	return nil
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

func backupActiveAccounts(s protocol.DesiredState) []protocol.Account {
	var accounts []protocol.Account
	for _, account := range s.Accounts {
		if account.State == protocol.AccountActive && valid.Slug(account.Slug) == nil {
			accounts = append(accounts, account)
		}
	}
	slices.SortFunc(accounts, func(a, b protocol.Account) int { return cmp.Compare(a.Slug, b.Slug) })
	return slices.CompactFunc(accounts, func(a, b protocol.Account) bool { return a.Slug == b.Slug })
}

func (h *Host) backupReport(s protocol.DesiredState, out *protocol.Report) {
	selected := backupActiveAccounts(s)
	if s.Backup != nil && s.Backup.Restore != nil {
		selected = make([]protocol.Account, 0, len(s.Backup.Restore.Accounts))
		for _, a := range s.Backup.Restore.Accounts {
			selected = append(selected, protocol.Account{Slug: a.Slug, GID: a.GID})
		}
		slices.SortFunc(selected, func(a, b protocol.Account) int { return cmp.Compare(a.Slug, b.Slug) })
	}
	host := h.readHostBackup()
	out.HostBackup = protocol.HostBackup{Snapshot: backupTime(host.Snapshot), Error: host.Error}
	if !backupReady(s.Backup) || !hostBackupCredentials(s.Backup) {
		out.HostBackup.Error = machine.NoCredentials
	}
	accounts := make(map[string]backupAccountFacts, min(len(selected), protocol.MaxBackupAccounts))
	resticError, litestreamError := host.ResticInstallError, host.LitestreamInstallError
	if current := h.resticInstall.errorText(); current != "" {
		resticError = boundBackupError(current)
	}
	if current := h.litestreamInstall.errorText(); current != "" {
		litestreamError = boundBackupError(current)
	}
	for i, account := range selected {
		var facts backupAccountFacts
		if i < protocol.MaxBackupAccounts {
			facts = h.readAccountBackup(account.Slug)
			facts.clearWalkForOtherPlacement(account.GID)
			h.clearLegacyWalkForOtherPlacement(account, &facts)
			accounts[account.Slug] = facts
		}
		_, credentials := accountBackupCredentials(s.Backup, account.Slug)
		out.Backups = append(out.Backups, accountBackupReport(account.Slug, facts, backupReady(s.Backup) && credentials, resticError, litestreamError))
	}
	h.operationBackupReport(s, out, host, accounts)
}

func (h *Host) clearLegacyWalkForOtherPlacement(account protocol.Account, facts *backupAccountFacts) {
	if facts.WalkGID != nil || !facts.WalkRecorded && facts.Walk.Error == "" {
		return
	}
	h.backupFactsMu.Lock()
	belongs := h.legacyWalkBelongsTo(account)
	h.backupFactsMu.Unlock()
	if !belongs {
		facts.clearWalk()
	}
}

// legacyWalkBelongsTo reads placement history under backupFactsMu.
func (h *Host) legacyWalkBelongsTo(account protocol.Account) bool {
	if h.backupInitialGID != nil && account.GID > *h.backupInitialGID {
		return false
	}
	publication, attempted := h.backupWalks[account.Slug]
	return !attempted || publication.gid == account.GID && !publication.ignoreLegacy
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

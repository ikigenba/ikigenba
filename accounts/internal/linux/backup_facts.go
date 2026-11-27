package linux

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

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
	GID          *int64 `json:",omitempty"`
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
	if err := h.backupDirectory(filepath.Dir(path), "", false); err != nil {
		return facts, err
	}
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

func accountBackupStatusPath(slug string) string { return backupStatusDir + "/" + slug + ".json" }

func (h *Host) loadAccountBackup(account protocol.Account) (backupAccountFacts, error) {
	if valid.Slug(account.Slug) != nil {
		return backupAccountFacts{}, machine.ErrInvalidState
	}
	facts, err := loadBackupFacts[backupAccountFacts](h, accountBackupStatusPath(account.Slug))
	switch {
	case err != nil:
		return backupAccountFacts{}, err
	case facts.GID != nil && *facts.GID == account.GID:
		return facts, nil
	case facts.GID == nil:
		gid, known := h.backupLegacyGIDs[account.Slug]
		if known && gid == account.GID {
			// The predecessor bound its walk separately. Other facts stay
			// eligible for first-state adoption.
			if facts.WalkGID != nil && *facts.WalkGID != gid {
				facts.Walk, facts.WalkRecorded, facts.WalkToken = backupWalk{}, false, ""
			}
			return facts, nil
		}
		return backupAccountFacts{}, nil
	default:
		return backupAccountFacts{}, nil
	}
}

func (h *Host) readAccountBackupFor(account protocol.Account) backupAccountFacts {
	facts, _ := h.loadAccountBackup(account)
	return facts
}

type backupPlacement struct {
	account protocol.Account
	walk    backupWalkPublication
}

type backupPlacementsKey struct{}

func backupFactAccounts(s protocol.DesiredState) map[string]protocol.Account {
	accounts := make(map[string]protocol.Account)
	if s.Backup != nil && s.Backup.Restore != nil {
		for _, restore := range s.Backup.Restore.Accounts {
			if valid.Slug(restore.Slug) == nil {
				accounts[restore.Slug] = protocol.Account{Slug: restore.Slug, GID: restore.GID}
			}
		}
	}
	for _, account := range s.Accounts {
		_, restoring := accounts[account.Slug]
		if valid.Slug(account.Slug) == nil && (account.State == protocol.AccountActive || restoring) {
			accounts[account.Slug] = account
		}
	}
	return accounts
}

func (h *Host) rememberLegacyBackupGIDs(s protocol.DesiredState) {
	h.backupLegacyOnce.Do(func() {
		accounts := backupFactAccounts(s)
		h.backupLegacyGIDs = make(map[string]int64, len(accounts))
		for slug, account := range accounts {
			h.backupLegacyGIDs[slug] = account.GID
		}
	})
}

// beginBackupFacts reconciles placement lifetimes before any pass work. Both
// workers share a placement while its slug and gid stay in the desired state.
func (h *Host) beginBackupFacts(ctx context.Context, s protocol.DesiredState) (context.Context, error) {
	h.rememberLegacyBackupGIDs(s)
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()

	accounts := backupFactAccounts(s)
	placements := make(map[string]*backupPlacement, len(accounts))
	for slug, account := range accounts {
		if previous := h.backupPlacements[slug]; previous != nil && previous.account.GID == account.GID {
			placements[slug] = previous
		} else {
			placements[slug] = &backupPlacement{account: account}
		}
	}
	h.backupPlacements = placements
	ctx = context.WithValue(ctx, backupPlacementsKey{}, placements)
	return ctx, errors.Join(
		h.pruneBackupFactDirectory(backupStatusDir, ".json", placements),
		h.pruneBackupFactDirectory(BackupDir+"/manifest", "", placements),
	)
}

// currentBackupPlacement is called under backupFactsMu before publishing.
// Pointer identity also rejects writers from a previous lifetime of the same gid.
func (h *Host) currentBackupPlacement(ctx context.Context, slug string) *backupPlacement {
	placements, _ := ctx.Value(backupPlacementsKey{}).(map[string]*backupPlacement)
	placement := placements[slug]
	if h.backupPlacements[slug] != placement {
		return nil
	}
	return placement
}

func (h *Host) pruneBackupFactDirectory(path, suffix string, placements map[string]*backupPlacement) error {
	if err := h.backupDirectory(filepath.Dir(path), "", false); err != nil {
		return err
	}
	dir, err := openBackupDirectory(unix.AT_FDCWD, h.path(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return hostError(path, err)
	}
	defer func() { _ = dir.Close() }()
	var removalErr error
	for {
		names, err := dir.Readdirnames(ReadBatch)
		for _, name := range names {
			slug, ok := strings.CutSuffix(name, suffix)
			if ok && valid.Slug(slug) == nil && placements[slug] == nil {
				removeErr := unix.Unlinkat(int(dir.Fd()), name, 0)
				if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) && removalErr == nil {
					removalErr = hostError(filepath.Join(path, name), removeErr)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return removalErr
		}
		if err != nil {
			return firstBackupError(removalErr, hostError(path, err))
		}
	}
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
	identity  backupWalkIdentity
	readiness backupWalkReadiness
	attempted bool
	blocked   bool
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
func (h *Host) accountBackupForPass(ctx context.Context, account protocol.Account) (backupAccountFacts, backupWalkState, backupWalkReadiness) {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	placement := h.currentBackupPlacement(ctx, account.Slug)
	if placement == nil {
		return backupAccountFacts{}, walkNotAttempted, backupWalkReadiness{}
	}
	facts := h.readAccountBackupFor(placement.account)
	kept := placement.walk
	if !kept.attempted {
		if facts.WalkRecorded {
			return facts, walkNotAttempted, facts.Walk.readiness()
		}
		return facts, walkNotAttempted, backupWalkReadiness{}
	}
	if kept.blocked || !facts.matchesWalk(kept.identity) {
		return facts, walkUnrecorded, kept.readiness
	}
	return facts, walkKept, kept.readiness
}

func (facts backupAccountFacts) matchesWalk(kept backupWalkIdentity) bool {
	if kept.token == "" || !facts.WalkRecorded || facts.WalkToken != kept.token {
		return false
	}
	identity, err := facts.walkIdentity()
	return err == nil && identity == kept
}

// keepBackupWalkHealth updates the authenticated publication under backupFactsMu.
func (h *Host) keepBackupWalkHealth(placement *backupPlacement, facts backupAccountFacts) error {
	identity, err := facts.walkIdentity()
	if err != nil {
		return err
	}
	if err := h.saveAccountBackup(placement, facts); err != nil {
		return err
	}
	placement.walk.identity, placement.walk.readiness = identity, facts.Walk.readiness()
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

func (h *Host) readAccountBackupForPass(ctx context.Context, slug string) backupAccountFacts {
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	if placement := h.currentBackupPlacement(ctx, slug); placement != nil {
		return h.readAccountBackupFor(placement.account)
	}
	return backupAccountFacts{}
}

func (h *Host) updateAccountBackup(ctx context.Context, slug string, update func(*backupAccountFacts)) error {
	if valid.Slug(slug) != nil {
		return machine.ErrInvalidState
	}
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	placement := h.currentBackupPlacement(ctx, slug)
	if placement == nil {
		return nil
	}
	facts := h.readAccountBackupFor(placement.account)
	update(&facts)
	return h.saveAccountBackup(placement, facts)
}

func (h *Host) saveAccountBackup(placement *backupPlacement, facts backupAccountFacts) error {
	facts.GID, facts.WalkGID = &placement.account.GID, nil
	return saveBackupFacts(h, accountBackupStatusPath(placement.account.Slug), facts)
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
	h.rememberLegacyBackupGIDs(s)
	selected := backupActiveAccounts(s)
	if s.Backup != nil && s.Backup.Restore != nil {
		selected = make([]protocol.Account, 0, len(s.Backup.Restore.Accounts))
		for _, a := range s.Backup.Restore.Accounts {
			account := protocol.Account{Slug: a.Slug, GID: a.GID}
			if i := slices.IndexFunc(s.Accounts, func(a protocol.Account) bool { return a.Slug == account.Slug }); i >= 0 {
				account.GID = s.Accounts[i].GID
			}
			selected = append(selected, account)
		}
		slices.SortFunc(selected, func(a, b protocol.Account) int { return cmp.Compare(a.Slug, b.Slug) })
	}
	host := h.readHostBackup()
	out.HostBackup = protocol.HostBackup{Snapshot: backupTime(host.Snapshot), Error: host.Error}
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
			facts = h.readAccountBackupFor(account)
			accounts[account.Slug] = facts
		}
		_, credentials := accountBackupCredentials(s.Backup, account.Slug)
		out.Backups = append(out.Backups, accountBackupReport(account.Slug, facts, backupReady(s.Backup) && credentials, resticError, litestreamError))
	}
	h.operationBackupReport(s, out, host, accounts)
	if !backupReady(s.Backup) || !hostBackupCredentials(s.Backup) {
		out.HostBackup.Error = machine.NoCredentials
	}
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

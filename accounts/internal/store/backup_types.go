package store

import (
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

// Replacement modes, volume retention causes, and backup states.
const (
	ReplaceVolume = "volume"
	ReplaceBackup = "backup"
	CauseDeleted  = "deleted"
	CauseReplaced = "replaced"

	BackupHealthy         = "healthy"
	BackupStale           = "stale"
	BackupError           = "error"
	BackupNone            = "none"
	BackupsRemoved        = "removed"
	DeleteBackupsDeleting = "deleting"
	DeleteBackupsFailed   = "failed"
)

// Preconditions for backup, replacement, and restore operations.
var (
	ErrNotClosed        = fmt.Errorf("host is not closed: %w", ErrConflict)
	ErrNoDataVolume     = fmt.Errorf("host has no data volume: %w", ErrConflict)
	ErrAccountNotActive = fmt.Errorf("account is not active: %w", ErrConflict)
	ErrTooManyRestores  = fmt.Errorf("host pending restore limit reached: %w", ErrConflict)
	ErrAccountExists    = fmt.Errorf("account exists: %w", ErrConflict)
	ErrBackupsDeleting  = fmt.Errorf("account backups are being deleted: %w", ErrConflict)
	ErrNoAddress        = fmt.Errorf("host has no allocated address: %w", ErrConflict)
	ErrNotRestorable    = fmt.Errorf("database is not restorable at that time: %w", ErrConflict)
	ErrNoDatabase       = fmt.Errorf("database not found: %w", ErrNotFound)
	ErrNoSnapshot       = fmt.Errorf("snapshot not found: %w", ErrNotFound)
	ErrNoUser           = fmt.Errorf("restore user not found: %w", ErrNotFound)
)

// BackupSettings controls schedules, retention, and health thresholds.
type BackupSettings struct {
	EveryMinutes, Hourly, Daily, Monthly, WindowDays, FilesStaleMinutes, DatabaseStaleMinutes, CheckDays int
}

// AccountBackup contains the latest recorded backup facts for an account.
type AccountBackup struct {
	Account, Host, State      string
	Snapshot, Checked         time.Time
	Error                     string
	Databases, DatabaseErrors int
	Synced, Written           time.Time
	Final                     string
	FinalAt                   time.Time
	Restore                   string
	Reported                  time.Time
	Delete, DeleteReason      string
}

// Restore records a requested partial restore and its outcome.
type Restore struct {
	ID, Host, Account, Kind, Path, User string
	AsOf                                time.Time
	Copy, State, Reason                 string
	Asked                               time.Time
	CreatedBy                           string
}

// RestoreRequest describes the data and time to restore for an account.
type RestoreRequest struct {
	Account, Kind, Path, User string
	AsOf                      time.Time
}

// BackupsDelete records the deletion work for a removed account's backups.
type BackupsDelete struct {
	Account, State, Reason string
	Attempts               int
	NextTry                time.Time
	Caller                 identity.Caller
}

// BackupStep records a host or account checkpoint of a backup operation.
type BackupStep struct {
	Account, State string
	At             time.Time
	Code, Reason   string
}

// Replacement records the resources and daemon visibility of an operation.
type Replacement struct {
	Operation, Mode, OldInstance, OldVolume string
	Retired, Admitted                       bool
}

// DefaultBackupSettings returns the service defaults for backup policy.
func DefaultBackupSettings() BackupSettings {
	return BackupSettings{EveryMinutes: 60, Hourly: 24, Daily: 7, Monthly: 1, WindowDays: 35,
		FilesStaleMinutes: 120, DatabaseStaleMinutes: 10, CheckDays: 7}
}

// BackupHealth classifies the account's combined file and database backup facts.
func BackupHealth(b AccountBackup, s BackupSettings, now time.Time) string {
	switch {
	case b.Error != "" || b.DatabaseErrors > 0:
		return BackupError
	case FilesStale(b, s, now), olderThan(b.Synced, s.DatabaseStaleMinutes, now):
		return BackupStale
	case b.Snapshot.IsZero():
		return BackupNone
	default:
		return BackupHealthy
	}
}

// FilesStale reports whether the latest recorded snapshot has aged past its threshold.
func FilesStale(b AccountBackup, s BackupSettings, now time.Time) bool {
	return olderThan(b.Snapshot, s.FilesStaleMinutes, now)
}

// DatabaseHealth classifies the database's latest sync.
func DatabaseHealth(d protocol.Database, s BackupSettings, now time.Time) string {
	switch {
	case d.Error != "":
		return BackupError
	case d.Synced.IsZero():
		return BackupNone
	case olderThan(d.Synced, s.DatabaseStaleMinutes, now):
		return BackupStale
	default:
		return BackupHealthy
	}
}

func olderThan(at time.Time, minutes int, now time.Time) bool {
	return !at.IsZero() && now.Sub(at) > time.Duration(minutes)*time.Minute
}

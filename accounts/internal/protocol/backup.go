package protocol

import (
	"cmp"
	"strconv"
	"time"
	"unicode/utf8"
)

// Backup report bounds, measured after JSON encoding for strings.
const (
	MaxBackupAccounts = 50
	MaxDatabases      = 50
	MaxSnapshots      = 32
	MaxBackupError    = 256
	MaxDatabasePath   = 256
	MaxRestores       = 10
	MaxCopyPath       = 640
)

// Backup step states, error codes, and partial restore kinds.
const (
	StepPending     = "pending"
	StepDone        = "done"
	StepFailed      = "failed"
	CodeNotEmpty    = "not_empty"
	CodeFailed      = "failed"
	RestoreDatabase = "database"
	RestoreFiles    = "files"
)

// AccountPrefix returns the backup prefix shared by an account's repositories.
func AccountPrefix(slug string) string { return "accounts/" + slug + "/" }

// FilesPrefix returns the account's file repository prefix.
func FilesPrefix(slug string) string { return AccountPrefix(slug) + "files" }

// DataPrefix returns the account's database replicas prefix.
func DataPrefix(slug string) string { return AccountPrefix(slug) + "data" }

// HostPrefix returns the backup prefix for one incarnation of a host.
func HostPrefix(host string, seq int64) string {
	return "hosts/" + host + "/" + strconv.FormatInt(seq, 10) + "/"
}

// Backup carries the host's backup configuration and pending work.
type Backup struct {
	Bucket     string               `json:"bucket"`
	Region     string               `json:"region"`
	Settings   Settings             `json:"settings"`
	Host       Credentials          `json:"host"`
	Accounts   []AccountCredentials `json:"accounts"`
	HostPrefix string               `json:"host_prefix"`
	Close      string               `json:"close"`
	Restore    *HostRestore         `json:"restore"`
	Restores   []RestoreRequest     `json:"restores"`
}

// Settings controls the backup schedule, retention, and freshness thresholds.
type Settings struct {
	EveryMinutes         int `json:"every_minutes"`
	Hourly               int `json:"hourly"`
	Daily                int `json:"daily"`
	Weekly               int `json:"weekly"`
	Monthly              int `json:"monthly"`
	WindowDays           int `json:"window_days"`
	FilesStaleMinutes    int `json:"files_stale_minutes"`
	DatabaseStaleMinutes int `json:"database_stale_minutes"`
	CheckDays            int `json:"check_days"`
}

// Credentials is a short-lived backup session grant.
type Credentials struct {
	AccessKeyID     string    `json:"access_key_id"`
	SecretAccessKey string    `json:"secret_access_key"`
	SessionToken    string    `json:"session_token"`
	Expires         time.Time `json:"expires"`
}

// AccountCredentials grants access to one account's backups.
type AccountCredentials struct {
	Account     string      `json:"account"`
	Credentials Credentials `json:"credentials"`
}

// HostRestore describes the accounts to reconstitute in a whole-host restore.
type HostRestore struct {
	ID       string           `json:"id"`
	Accounts []RestoreAccount `json:"accounts"`
}

// RestoreAccount identifies one account in a whole-host restore.
type RestoreAccount struct {
	Slug      string   `json:"slug"`
	GID       int64    `json:"gid"`
	Databases []string `json:"databases"`
}

// RestoreRequest describes one partial restore.
type RestoreRequest struct {
	ID      string    `json:"id"`
	Account string    `json:"account"`
	Kind    string    `json:"kind"`
	Path    string    `json:"path"`
	User    string    `json:"user"`
	AsOf    time.Time `json:"as_of"`
}

// AccountBackup reports the health and available history of one account's backups.
type AccountBackup struct {
	Account        string     `json:"account"`
	Snapshot       time.Time  `json:"snapshot"`
	Snapshots      []int64    `json:"snapshots"`
	Checked        time.Time  `json:"checked"`
	Error          string     `json:"error"`
	Databases      []Database `json:"databases"`
	DatabaseTotal  int        `json:"database_total"`
	DatabaseErrors int        `json:"database_errors"`
	Synced         time.Time  `json:"synced"`
	Written        time.Time  `json:"written"`
	Final          Step       `json:"final"`
	Restored       Step       `json:"restored"`
}

// Database reports one database replica's state and recoverable history.
type Database struct {
	Path    string    `json:"path"`
	Oldest  time.Time `json:"oldest"`
	Written time.Time `json:"written"`
	Synced  time.Time `json:"synced"`
	Error   string    `json:"error"`
}

// Step reports the status of one account's or host key's part of an operation.
type Step struct {
	ID    string    `json:"id"`
	State string    `json:"state"`
	At    time.Time `json:"at"`
	Code  string    `json:"code"`
}

// HostBackup reports the host key repository's status.
type HostBackup struct {
	Snapshot time.Time `json:"snapshot"`
	Error    string    `json:"error"`
	Final    Step      `json:"final"`
	Restored Step      `json:"restored"`
}

// RestoreResult reports the outcome and the copy path for a partial restore.
type RestoreResult struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Copy  string `json:"copy"`
	Error string `json:"error"`
}

func (r *Report) boundBackups() {
	r.Backups = smallest(r.Backups, MaxBackupAccounts, func(a, b AccountBackup) int { return cmp.Compare(a.Account, b.Account) })
	remaining := MaxDatabases
	for i := range r.Backups {
		account := &r.Backups[i]
		account.Snapshots = smallest(account.Snapshots, MaxSnapshots, func(a, b int64) int { return cmp.Compare(b, a) })
		account.DatabaseTotal = max(account.DatabaseTotal, len(account.Databases))
		account.Databases = boundDatabases(account.Databases, remaining)
		remaining -= len(account.Databases)
		account.Error = jsonStringPrefix(account.Error, MaxBackupError)
	}
	r.HostBackup.Error = jsonStringPrefix(r.HostBackup.Error, MaxBackupError)
	r.Restores = smallest(r.Restores, MaxRestores, func(a, b RestoreResult) int { return cmp.Compare(a.ID, b.ID) })
	for i := range r.Restores {
		result := &r.Restores[i]
		if jsonStringPrefix(result.Copy, MaxCopyPath) != result.Copy {
			result.Copy = ""
			result.State = StepFailed
		}
		result.Error = jsonStringPrefix(result.Error, MaxBackupError)
	}
}

func boundDatabases(databases []Database, limit int) []Database {
	if databases == nil {
		return nil
	}
	kept := make([]Database, 0, min(limit, len(databases)))
	for _, database := range databases {
		if jsonStringPrefix(database.Path, MaxDatabasePath) == database.Path {
			kept = insertSmallest(kept, database, limit, func(a, b Database) int { return cmp.Compare(a.Path, b.Path) })
		}
	}
	for i := range kept {
		kept[i].Error = jsonStringPrefix(kept[i].Error, MaxBackupError)
	}
	return kept
}

// jsonStringPrefix uses encoding/json's default escaping and counts both quotes.
func jsonStringPrefix(value string, limit int) string {
	remaining := limit - 2
	for i := 0; i < len(value); {
		r, width := utf8.DecodeRuneInString(value[i:])
		size := width
		switch {
		case r == '"' || r == '\\' || r == '\b' || r == '\f' || r == '\n' || r == '\r' || r == '\t':
			size = 2
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029' || (r == utf8.RuneError && width == 1):
			size = 6
		}
		if size > remaining {
			return value[:i]
		}
		remaining -= size
		i += width
	}
	return value
}

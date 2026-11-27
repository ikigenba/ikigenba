package store_test

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

// These assignments use the public values as unnamed structs, so field order
// and field types have to agree for the tests to compile.

// R-4JN4-V4H6
var _ struct {
	Name                                                                     string
	Seq                                                                      int64
	State, Reason, Type                                                      string
	RootGB, DataGB                                                           int
	InstanceID, RootVolume, DataVolume, AllocationID, AssociationID, EIP, AZ string
	Version, Applied, Attempted                                              int64
	ReportError                                                              bool
	Alarm                                                                    string
	RunningSince, Reported                                                   time.Time
	Operation                                                                string
	Accounts                                                                 int
	Created                                                                  time.Time
	CreatedBy                                                                string
	Allowlist                                                                []string
	Change, RecordReason                                                     string
	OnDelete                                                                 string
	Closed                                                                   time.Time
	Replace                                                                  string
} = store.Host{}

// R-4KV1-8W7V
var _ struct {
	Volume, Host  string
	SizeGB        int
	Deleted       time.Time
	State, Reason string
	Attempts      int
	NextTry       time.Time
	Caller        identity.Caller
	Cause         string
} = store.KeptVolume{}

// R-4PQM-RZ6N
var _ interface {
	DeleteHost(context.Context, string, bool, bool, identity.Caller) (store.Host, store.Operation, error)
	KeptVolume(context.Context, string) (store.KeptVolume, error)
	ListKeptVolumes(context.Context, time.Time, string, int) ([]store.KeptVolume, error)
	ListKeptVolumesBefore(context.Context, time.Time, string, int) ([]store.KeptVolume, error)
	LastKeptVolumes(context.Context, int) ([]store.KeptVolume, error)
	DeleteKeptVolume(context.Context, string, identity.Caller) (store.KeptVolume, error)
	DueVolumeDeletes(context.Context, time.Time, string, int) ([]store.KeptVolume, error)
	DeferVolumeDelete(context.Context, string, time.Time, int) error
	FinishVolumeDelete(context.Context, string) error
	FailVolumeDelete(context.Context, string, string) error
	AdoptKept(context.Context, int) (int, error)
} = (*store.Store)(nil)

// R-DAH3-45G3
var _ interface {
	RecordHostChange(context.Context, string, string, string) error
	NoteHostRecord(context.Context, string, string, string) error
} = (*store.Store)(nil)

// R-A2QY-1NUK
var _ interface {
	DeletedHosts(context.Context, int64, int) ([]store.Host, error)
	SettleDeleted(context.Context, string) error
} = (*store.Store)(nil)

// R-1XKC-EUZQ
var _ interface {
	Unban(context.Context, string, string) (store.Host, error)
	UnbanRunning(context.Context, string) (int, error)
	Allow(context.Context, string, string) (store.Host, error)
	Disallow(context.Context, string, string) (store.Host, error)
} = (*store.Store)(nil)

// R-GPDB-0YYF
type resizeShape = struct {
	Type           string
	RootGB, DataGB int
}

var (
	_ resizeShape = store.HostSpec{}
	_ resizeShape = store.Target{}
)

// R-GQL7-EQP4
var _ struct {
	ID, Host, Account, Kind, Step, Status   string
	Generation                              int
	Target                                  store.Target
	ResumeState, Error                      string
	Attempts                                int
	Started, StepStarted, NextTry, Finished time.Time
	Supersedes                              string
	Caller                                  identity.Caller
} = store.Operation{}

// R-4OIQ-E7FY
var _ struct {
	Step                                                                           string
	Generation                                                                     int
	InstanceID, RootVolume, DataVolume, AllocationID, AssociationID, EIP, AZ, Type string
	RootGB, DataGB                                                                 int
	HostState                                                                      string
} = store.Update{}

// R-BSCO-Q7FI
var _ struct {
	Slug, Host, State, Reason    string
	GID, Version, RemovedVersion int64
	Change, Operation            string
	Users, Invites               int
	Created                      time.Time
	CreatedBy                    string
} = store.Account{}

// R-BTKL-3Z67
var (
	_ struct{ Type, Body, Line string } = store.Key{}
	_ struct {
		Name, Account       string
		Admin               bool
		State               string
		UID, Version        int64
		Pending             bool
		Keys                []store.Key
		LastSignIn, Created time.Time
		CreatedBy           string
	} = store.User{}
)

// R-GVGS-XTNW
var (
	_ struct {
		Host     string
		Received time.Time
		Body     protocol.Report
	} = store.Report{}
	_ struct {
		Before, After       string
		Synced, ApplyFailed bool
		Applied, Attempted  int64
	} = store.Transition{}
)

type namedString string

const (
	// R-4NAU-0FP9
	_ namedString = store.HostProvisioning
	_ namedString = store.HostRunning
	_ namedString = store.HostStopping
	_ namedString = store.HostStopped
	_ namedString = store.HostStarting
	_ namedString = store.HostResizing
	_ namedString = store.HostDeleting
	_ namedString = store.HostFailed
	_ namedString = store.HostDeleted
	_ namedString = store.HostClosing
	_ namedString = store.HostOpening
	_ namedString = store.HostReplacing
	_ namedString = store.HostRestoring

	// R-0K9R-765T
	_ namedString = store.AccountPending
	_ namedString = store.AccountActive
	_ namedString = store.AccountFailed
	_ namedString = store.AccountRemoving
	_ namedString = store.UserActive
	_ namedString = store.UserLocked
	_ namedString = store.UserRemoving

	// R-GZ4I-34VZ
	_ namedString = store.HealthSync
	_ namedString = store.HealthPending
	_ namedString = store.HealthError
	_ namedString = store.HealthSilent
	_ namedString = store.HealthNone
	_ namedString = store.AlarmNone
	_ namedString = store.AlarmSilent
	_ namedString = store.AlarmError

	// R-4M2X-MNYK
	_ namedString = store.OpRunning
	_ namedString = store.OpFailed
	_ namedString = store.OpDone
	_ namedString = store.OpCancelled
	_ namedString = store.OpCreate
	_ namedString = store.OpStop
	_ namedString = store.OpStart
	_ namedString = store.OpResize
	_ namedString = store.OpDelete
	_ namedString = store.OpPlace
	_ namedString = store.OpUnplace
	_ namedString = store.OpClose
	_ namedString = store.OpOpen
	_ namedString = store.OpReplace
	_ namedString = store.OpRestore

	// R-6VI4-NAW9
	_ namedString = store.OnDeleteKeep
	_ namedString = store.OnDeleteDelete
	_ namedString = store.VolumeKept
	_ namedString = store.VolumeDeleting
	_ namedString = store.VolumeDeleted
)

// R-BUSH-HQWW
var _ struct {
	ID, Account      string
	Admin            bool
	Created, Expires time.Time
	CreatedBy        string
} = store.Invite{}

type namedInt int

const (
	// R-BW0D-VINL
	_ namedString = store.InvitePrefix
	_ namedString = store.TokenPrefix
	_ namedInt    = store.MaxInvites
)

// R-BX8A-9AEA
var (
	_ func(string) bool = store.ValidInviteID
	_ func(string) bool = store.ValidToken
)

// R-BZO3-0TVO
var _ interface {
	CreateInvite(context.Context, string, bool, identity.Caller) (store.Invite, string, error)
	Invites(context.Context, string) ([]store.Invite, error)
	RevokeInvite(context.Context, string) (store.Invite, error)
	InviteByToken(context.Context, string) (store.Invite, error)
	NameHeld(context.Context, string) (bool, error)
	Enroll(context.Context, string, string, store.Key) (store.Invite, store.User, error)
	SetAdmin(context.Context, string, string, bool) (store.User, bool, error)
} = (*store.Store)(nil)

// R-RHUH-IBVS
var _ interface {
	UserNamed(context.Context, string) (store.User, error)
	ActiveUserByUID(context.Context, string, int64) (store.User, error)
	AddKeyAs(context.Context, string, store.Key) (store.User, error)
	RemoveKeyAs(context.Context, string, store.Key) (store.User, int, error)
	AdminCheck(context.Context, string) error
	UsersAs(context.Context, string) ([]store.User, error)
	LockAs(context.Context, string, string) (store.User, error)
	UnlockAs(context.Context, string, string) (store.User, error)
	DeleteAs(context.Context, string, string) (store.User, error)
	SetAdminAs(context.Context, string, string, bool) (store.User, bool, error)
	CreateInviteAs(context.Context, string, identity.Caller) (store.Invite, string, error)
	InvitesAs(context.Context, string) ([]store.Invite, error)
	RevokeInviteAs(context.Context, string, string) (store.Invite, error)
} = (*store.Store)(nil)

// R-5814-IJB2 R-5990-WB1R
const (
	_ namedString = store.ReplaceVolume
	_ namedString = store.ReplaceBackup
	_ namedString = store.CauseDeleted
	_ namedString = store.CauseReplaced
	_ namedString = store.BackupHealthy
	_ namedString = store.BackupStale
	_ namedString = store.BackupError
	_ namedString = store.BackupNone
	_ namedString = store.BackupsRemoved
	_ namedString = store.DeleteBackupsDeleting
	_ namedString = store.DeleteBackupsFailed
)

// R-5AGX-A2SG
var _ struct {
	EveryMinutes, Hourly, Daily, Monthly, WindowDays, FilesStaleMinutes, DatabaseStaleMinutes, CheckDays int
} = store.BackupSettings{}

var _ func() store.BackupSettings = store.DefaultBackupSettings

// R-GV56-GB7L
var _ struct {
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
} = store.AccountBackup{}

// R-5FCI-T5R8
var (
	_ struct {
		ID, Host, Account, Kind, Path, User string
		AsOf                                time.Time
		Copy, State, Reason                 string
		Asked                               time.Time
		CreatedBy                           string
	} = store.Restore{}
	_ struct {
		Account, Kind, Path, User string
		AsOf                      time.Time
	} = store.RestoreRequest{}
)

// R-5GKF-6XHX
var (
	_ struct {
		Account, State, Reason string
		Attempts               int
		NextTry                time.Time
		Caller                 identity.Caller
	} = store.BackupsDelete{}
	_ struct {
		Account, State string
		At             time.Time
		Code, Reason   string
	} = store.BackupStep{}
	_ struct {
		Operation, Mode, OldInstance, OldVolume string
		Retired, Admitted                       bool
	} = store.Replacement{}
)

// R-5K84-C8Q0
var (
	_ func(store.AccountBackup, store.BackupSettings, time.Time) string = store.BackupHealth
	_ func(store.AccountBackup, store.BackupSettings, time.Time) bool   = store.FilesStale
	_ func(protocol.Database, store.BackupSettings, time.Time) string   = store.DatabaseHealth
)

// R-XB00-JMCY
var _ interface {
	CloseHost(context.Context, string, identity.Caller) (store.Host, store.Operation, error)
	OpenHost(context.Context, string, identity.Caller) (store.Host, store.Operation, error)
	ReplaceHost(context.Context, string, string, store.Target, identity.Caller) (store.Host, store.Operation, []string, error)
	Replacement(context.Context, string) (store.Replacement, error)
	Retire(context.Context, string) error
	Admit(context.Context, string) error
	BackupSteps(context.Context, string) ([]store.BackupStep, error)
} = (*store.Store)(nil)

// R-GPL0-Q7DY
var _ interface {
	RequestRestore(context.Context, store.RestoreRequest, identity.Caller) (store.Restore, store.Operation, error)
	Restore(context.Context, string) (store.Restore, error)
	RestoresOn(context.Context, string, int) ([]store.Restore, int, error)
	BackupSettings(context.Context) (store.BackupSettings, error)
	BackupOf(context.Context, string) (store.AccountBackup, error)
	BackupsOn(context.Context, string) ([]store.AccountBackup, error)
	ListBackups(context.Context, string, string, int) ([]store.AccountBackup, error)
	DeleteAccountBackups(context.Context, string, identity.Caller) (store.BackupsDelete, error)
	BackupsDeleteOf(context.Context, string) (store.BackupsDelete, error)
	DueBackupDeletes(context.Context, time.Time, string, int) ([]store.BackupsDelete, error)
	DeferBackupDelete(context.Context, string, time.Time, int) error
	FinishBackupDelete(context.Context, string) error
	FailBackupDelete(context.Context, string, string) error
} = (*store.Store)(nil)

// R-PKKG-LOF5
var _ interface {
	PlaceAccount(context.Context, string, string, identity.Caller) (store.Account, store.Operation, error)
	RemoveAccount(context.Context, string, identity.Caller) (store.Account, store.Operation, []string, error)
	Account(context.Context, string) (store.Account, error)
	ListAccounts(context.Context, string, int) ([]store.Account, error)
	ListAccountsBefore(context.Context, string, int) ([]store.Account, error)
	LastAccounts(context.Context, int) ([]store.Account, error)
	AccountsOn(context.Context, string) ([]store.Account, error)
} = (*store.Store)(nil)

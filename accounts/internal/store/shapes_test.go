package store_test

import (
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

// These assignments use the public values as unnamed structs, so field order
// and field types have to agree for the tests to compile.

// R-GO5E-N77Q
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
} = store.Host{}

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

// R-GRT3-SIFT
var _ struct {
	Step                                                                           string
	Generation                                                                     int
	InstanceID, RootVolume, DataVolume, AllocationID, AssociationID, EIP, AZ, Type string
	RootGB, DataGB                                                                 int
} = store.Update{}

// R-0J1U-TEF4
var _ struct {
	Slug, Host, State, Reason    string
	GID, Version, RemovedVersion int64
	Change, Operation            string
	Users                        int
	Created                      time.Time
	CreatedBy                    string
} = store.Account{}

// R-GU8W-K1X7
var (
	_ struct{ Type, Body, Line string } = store.Key{}
	_ struct {
		Name, Account, State string
		UID, Version         int64
		Pending              bool
		Keys                 []store.Key
		LastSignIn, Created  time.Time
		CreatedBy            string
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
	// R-GWOP-BLEL
	_ namedString = store.HostProvisioning
	_ namedString = store.HostRunning
	_ namedString = store.HostStopping
	_ namedString = store.HostStopped
	_ namedString = store.HostStarting
	_ namedString = store.HostResizing
	_ namedString = store.HostDeleting
	_ namedString = store.HostFailed
	_ namedString = store.HostDeleted

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

	// R-H0CE-GWMO
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
)

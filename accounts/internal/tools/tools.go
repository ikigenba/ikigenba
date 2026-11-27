package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// Register installs the SSH hosting tools in catalog order.
func Register(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[HostsArgs, HostList]{Name: ToolHosts, Description: DescribeHosts, Effect: mcp.Read, Handler: read(cfg, cfg.hosts)})
	mcp.AddTool(srv, mcp.Tool[HostArgs, HostRead]{Name: ToolHost, Description: DescribeHost, Effect: mcp.Read, Handler: read(cfg, cfg.host)})
	mcp.AddTool(srv, mcp.Tool[CreateHostArgs, HostChange]{Name: ToolCreateHost, Description: DescribeCreateHost, Effect: mcp.Additive, Handler: change(cfg, ToolCreateHost, cfg.createHost)})
	mcp.AddTool(srv, mcp.Tool[HostArgs, HostChange]{Name: ToolStopHost, Description: DescribeStopHost, Effect: mcp.Additive, Handler: change(cfg, ToolStopHost, cfg.hostOperation(store.OpStop))})
	mcp.AddTool(srv, mcp.Tool[HostArgs, HostChange]{Name: ToolStartHost, Description: DescribeStartHost, Effect: mcp.Additive, Handler: change(cfg, ToolStartHost, cfg.hostOperation(store.OpStart))})
	mcp.AddTool(srv, mcp.Tool[ResizeHostArgs, HostResize]{Name: ToolResizeHost, Description: DescribeResizeHost, Effect: mcp.Additive, Handler: change(cfg, ToolResizeHost, cfg.resizeHost)})
	mcp.AddTool(srv, mcp.Tool[HostArgs, HostChange]{Name: ToolRetryHost, Description: DescribeRetryHost, Effect: mcp.Additive, Handler: change(cfg, ToolRetryHost, cfg.retryHost)})
	mcp.AddTool(srv, mcp.Tool[DeleteHostArgs, HostChange]{Name: ToolDeleteHost, Description: DescribeDeleteHost, Effect: mcp.Destructive, Handler: change(cfg, ToolDeleteHost, cfg.deleteHost)})
	mcp.AddTool(srv, mcp.Tool[KeptVolumesArgs, KeptVolumeList]{Name: ToolKeptVolumes, Description: DescribeKeptVolumes, Effect: mcp.Read, Handler: read(cfg, cfg.keptVolumes)})
	mcp.AddTool(srv, mcp.Tool[KeptVolumeArgs, KeptVolume]{Name: ToolDeleteKeptVolume, Description: DescribeDeleteKeptVolume, Effect: mcp.Destructive, Handler: change(cfg, ToolDeleteKeptVolume, cfg.deleteKeptVolume)})
	mcp.AddTool(srv, mcp.Tool[AccountsArgs, AccountList]{Name: ToolAccounts, Description: DescribeAccounts, Effect: mcp.Read, Handler: read(cfg, cfg.accounts)})
	mcp.AddTool(srv, mcp.Tool[AccountArgs, AccountRead]{Name: ToolAccount, Description: DescribeAccount, Effect: mcp.Read, Handler: read(cfg, cfg.account)})
	mcp.AddTool(srv, mcp.Tool[CreateAccountArgs, Account]{Name: ToolCreateAccount, Description: DescribeCreateAccount, Effect: mcp.Additive, Handler: change(cfg, ToolCreateAccount, cfg.createAccount)})
	mcp.AddTool(srv, mcp.Tool[AccountArgs, Account]{Name: ToolRemoveAccount, Description: DescribeRemoveAccount, Effect: mcp.Destructive, Handler: change(cfg, ToolRemoveAccount, cfg.removeAccount)})
	mcp.AddTool(srv, mcp.Tool[UsersArgs, UserList]{Name: ToolUsers, Description: DescribeUsers, Effect: mcp.Read, Handler: read(cfg, cfg.users)})
	mcp.AddTool(srv, mcp.Tool[UserKeyArgs, UserChange]{Name: ToolAddUser, Description: DescribeAddUser, Effect: mcp.Additive, Handler: change(cfg, ToolAddUser, cfg.addUser)})
	mcp.AddTool(srv, mcp.Tool[UserKeyArgs, UserChange]{Name: ToolAddKey, Description: DescribeAddKey, Effect: mcp.Additive, Handler: change(cfg, ToolAddKey, cfg.addKey)})
	mcp.AddTool(srv, mcp.Tool[UserKeyArgs, KeyRemoved]{Name: ToolRemoveKey, Description: DescribeRemoveKey, Effect: mcp.Destructive, Handler: change(cfg, ToolRemoveKey, cfg.removeKey)})
	mcp.AddTool(srv, mcp.Tool[UserArgs, UserChange]{Name: ToolLockUser, Description: DescribeLockUser, Effect: mcp.Additive, Handler: change(cfg, ToolLockUser, cfg.userOperation(cfg.Store.Lock))})
	mcp.AddTool(srv, mcp.Tool[UserArgs, UserChange]{Name: ToolUnlockUser, Description: DescribeUnlockUser, Effect: mcp.Additive, Handler: change(cfg, ToolUnlockUser, cfg.userOperation(cfg.Store.Unlock))})
	mcp.AddTool(srv, mcp.Tool[UserArgs, UserChange]{Name: ToolDeleteUser, Description: DescribeDeleteUser, Effect: mcp.Destructive, Handler: change(cfg, ToolDeleteUser, cfg.userOperation(cfg.Store.Delete))})
	mcp.AddTool(srv, mcp.Tool[SetAdminArgs, UserChange]{Name: ToolSetAdmin, Description: DescribeSetAdmin, Effect: mcp.Additive, Handler: change(cfg, ToolSetAdmin, cfg.setAdmin)})
	mcp.AddTool(srv, mcp.Tool[CreateInviteArgs, InviteMade]{Name: ToolCreateInvite, Description: DescribeCreateInvite, Effect: mcp.Additive, Handler: change(cfg, ToolCreateInvite, cfg.createInvite)})
	mcp.AddTool(srv, mcp.Tool[AccountArgs, InviteList]{Name: ToolListInvites, Description: DescribeListInvites, Effect: mcp.Read, Handler: read(cfg, cfg.listInvites)})
	mcp.AddTool(srv, mcp.Tool[InviteArgs, Invite]{Name: ToolRevokeInvite, Description: DescribeRevokeInvite, Effect: mcp.Destructive, Handler: change(cfg, ToolRevokeInvite, cfg.revokeInvite)})
	mcp.AddTool(srv, mcp.Tool[UnbanArgs, Unbanned]{Name: ToolUnbanIP, Description: DescribeUnbanIP, Effect: mcp.Additive, Handler: change(cfg, ToolUnbanIP, cfg.unbanIP)})
	mcp.AddTool(srv, mcp.Tool[HostIPArgs, AllowlistChange]{Name: ToolAllowIP, Description: DescribeAllowIP, Effect: mcp.Additive, Handler: change(cfg, ToolAllowIP, cfg.allowlistOperation(cfg.Store.Allow))})
	mcp.AddTool(srv, mcp.Tool[HostIPArgs, AllowlistChange]{Name: ToolDisallowIP, Description: DescribeDisallowIP, Effect: mcp.Destructive, Handler: change(cfg, ToolDisallowIP, cfg.allowlistOperation(cfg.Store.Disallow))})
	mcp.AddTool(srv, mcp.Tool[HostArgs, HostChange]{Name: ToolCloseHost, Description: DescribeCloseHost, Effect: mcp.Additive, Handler: change(cfg, ToolCloseHost, cfg.backupHostOperation(cfg.Store.CloseHost))})
	mcp.AddTool(srv, mcp.Tool[HostArgs, HostChange]{Name: ToolOpenHost, Description: DescribeOpenHost, Effect: mcp.Additive, Handler: change(cfg, ToolOpenHost, cfg.backupHostOperation(cfg.Store.OpenHost))})
	mcp.AddTool(srv, mcp.Tool[ReplaceHostArgs, HostChange]{Name: ToolReplaceHost, Description: DescribeReplaceHost, Effect: mcp.Destructive, Handler: change(cfg, ToolReplaceHost, cfg.replaceHost)})
	mcp.AddTool(srv, mcp.Tool[BackupStatusArgs, BackupStatusList]{Name: ToolBackupStatus, Description: DescribeBackupStatus, Effect: mcp.Read, Handler: read(cfg, cfg.backupStatus)})
	mcp.AddTool(srv, mcp.Tool[AccountArgs, RestorePoints]{Name: ToolRestorePoints, Description: DescribeRestorePoints, Effect: mcp.Read, Handler: read(cfg, cfg.restorePoints)})
	mcp.AddTool(srv, mcp.Tool[RestoreDatabaseArgs, RestoreMade]{Name: ToolRestoreDatabase, Description: DescribeRestoreDatabase, Effect: mcp.Additive, Handler: change(cfg, ToolRestoreDatabase, cfg.restoreDatabase)})
	mcp.AddTool(srv, mcp.Tool[RestoreFilesArgs, RestoreMade]{Name: ToolRestoreFiles, Description: DescribeRestoreFiles, Effect: mcp.Additive, Handler: change(cfg, ToolRestoreFiles, cfg.restoreFiles)})
	mcp.AddTool(srv, mcp.Tool[AccountArgs, BackupsDeletion]{Name: ToolDeleteAccountBackups, Description: DescribeDeleteAccountBackups, Effect: mcp.Destructive, Handler: change(cfg, ToolDeleteAccountBackups, cfg.deleteAccountBackups)})
}

func read[A, R any](cfg Config, handle func(context.Context, A) (R, error)) func(context.Context, identity.Caller, A) (R, error) {
	return func(ctx context.Context, _ identity.Caller, a A) (R, error) {
		if !cfg.Configured {
			var zero R
			return zero, errors.New(NotConfigured)
		}
		return handle(ctx, a)
	}
}

type changeRecord struct {
	host, account, user, ip string
	ipErr                   error
	before, after           string
	onDelete, volume        string
	operation               store.Operation
	cancelledRestores       []string
	backupDeleteStarted     bool
}

func change[A, R any](cfg Config, name string, handle func(context.Context, identity.Caller, A, *changeRecord) (R, error)) func(context.Context, identity.Caller, A) (R, error) {
	return func(ctx context.Context, c identity.Caller, a A) (R, error) {
		// Record input fields without reading the store, even on disabled spaces.
		r := changeNames(a)
		var result R
		err := errors.New(NotConfigured)
		if cfg.Configured {
			result, err = handle(ctx, c, a, &r)
		}
		cfg.tellChange(ctx, name, r, err)
		return result, err
	}
}

func changeNames(args any) changeRecord {
	switch a := args.(type) {
	case HostArgs:
		return changeRecord{host: a.Host}
	case DeleteHostArgs:
		return changeRecord{host: a.Host}
	case ResizeHostArgs:
		return changeRecord{host: a.Host}
	case ReplaceHostArgs:
		return changeRecord{host: a.Host}
	case AccountArgs:
		return changeRecord{account: a.Account}
	case RestoreDatabaseArgs:
		return changeRecord{account: a.Account}
	case RestoreFilesArgs:
		return changeRecord{account: a.Account}
	case CreateAccountArgs:
		return changeRecord{host: a.Host, account: a.Account}
	case SetAdminArgs:
		return changeRecord{account: a.Account, user: a.User}
	case CreateInviteArgs:
		return changeRecord{account: a.Account}
	case UserArgs:
		return changeRecord{account: a.Account, user: a.User}
	case UserKeyArgs:
		return changeRecord{account: a.Account, user: a.User}
	case UnbanArgs:
		ip, err := valid.Address(a.IP)
		return changeRecord{host: value(a.Host, ""), ip: ip, ipErr: err}
	case HostIPArgs:
		ip, err := valid.AllowEntry(a.IP)
		return changeRecord{host: a.Host, ip: ip, ipErr: err}
	}
	return changeRecord{}
}

func (cfg Config) tellChange(ctx context.Context, name string, r changeRecord, err error) {
	op := r.operation
	if op.Supersedes != "" {
		switch op.Kind {
		case store.OpUnplace:
			trail.OperationFinished(ctx, cfg.Telemetry, op.Supersedes, store.OpPlace, r.host, r.account, store.OpCancelled, "", "")
		case store.OpOpen:
			if name == ToolOpenHost {
				trail.OperationFinished(ctx, cfg.Telemetry, op.Supersedes, store.OpClose, r.host, "", store.OpCancelled, "", "")
			}
		}
	}
	for _, id := range r.cancelledRestores {
		account := r.account
		if account == "" {
			restore, err := cfg.Store.Operation(ctx, id)
			if err != nil {
				continue
			}
			account = restore.Account
		}
		trail.OperationFinished(ctx, cfg.Telemetry, id, store.OpRestore, r.host, account, store.OpCancelled, "", "")
	}
	if op.ID != "" {
		if r.account == "" {
			trail.HostChanged(ctx, cfg.Telemetry, r.host, r.before, r.after, op.ID)
		} else if op.Kind != store.OpRestore && r.before != r.after {
			trail.AccountChanged(ctx, cfg.Telemetry, r.account, r.host, r.before, r.after)
		}
		trail.OperationStarted(ctx, cfg.Telemetry, op.ID, op.Kind, r.host, r.account, r.onDelete)
	}
	if r.volume != "" {
		trail.VolumeChanged(ctx, cfg.Telemetry, r.volume, r.host, store.VolumeKept, store.VolumeDeleting, "")
	}
	result := trail.ResultOK
	if err != nil {
		result = trail.ResultRefused
		if err.Error() == CloudFailed || err.Error() == store.Unreachable {
			result = trail.ResultFailed
		}
	}
	trail.Change(ctx, cfg.Telemetry, name, r.host, r.account, r.user, r.ip, result)
	if op.ID != "" || r.volume != "" || r.backupDeleteStarted {
		cfg.Wake()
	}
}

var storeRefusals = []struct {
	err  error
	text string
}{
	{store.ErrTaken, NameTaken},
	{store.ErrBusy, HostBusy},
	{store.ErrHostFailed, HostFailed},
	{store.ErrNotFailed, HostNotFailed},
	{store.ErrNotRunning, HostNotRunning},
	{store.ErrNotStopped, HostNotStopped},
	{store.ErrHasAccounts, HostHasAccounts},
	{store.ErrHostFull, HostFull},
	{store.ErrHostGone, HostGone},
	{store.ErrNothingToChange, NothingToChange},
	{store.ErrShrinks, SizeShrinks},
	{store.ErrGrowLimit, GrowLimit},
	{store.ErrHasUsers, AccountHasUsers},
	{store.ErrAccountRemoving, AccountRemoving},
	{store.ErrAccountFull, AccountFull},
	{store.ErrTooManyKeys, TooManyKeys},
	{store.ErrTooManyInvites, TooManyInvites},
	{store.ErrNotLocked, UserNotLocked},
	{store.ErrUserRemoving, UserRemoving},
	{store.ErrNotInAccount, NotInAccount},
	{store.ErrKeyNotHeld, KeyNotHeld},
	{store.ErrTooManyUnbans, TooManyUnbans},
	{store.ErrAllowlistFull, AllowlistFull},
	{store.ErrVolumeDeleting, VolumeDeleting},
	{store.ErrNotClosed, HostNotClosed},
	{store.ErrNoDataVolume, NoDataVolume},
	{store.ErrNoAddress, NoAddress},
	{store.ErrAccountNotActive, AccountNotActive},
	{store.ErrNoDatabase, NoDatabase},
	{store.ErrNoSnapshot, NoSnapshot},
	{store.ErrNoUser, NoUser},
	{store.ErrNotRestorable, NotRestorable},
	{store.ErrTooManyRestores, TooManyRestores},
	{store.ErrAccountExists, AccountExists},
	{store.ErrBackupsDeleting, BackupsDeleting},
}

func storeError(err error, missing string) error {
	if err == nil {
		return nil
	}
	for _, refusal := range storeRefusals {
		if errors.Is(err, refusal.err) {
			return errors.New(refusal.text)
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		return errors.New(missing)
	}
	return errors.New(store.Unreachable)
}

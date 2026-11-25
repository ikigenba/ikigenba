package box

import (
	"context"
	"errors"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// RegisterUser installs the tools available to every SSH user.
func RegisterUser(srv *mcp.Server, cfg Config) {
	mcp.AddTool(srv, mcp.Tool[NoArgs, KeyList]{Name: ToolKeys, Description: DescribeKeys, Effect: mcp.Read, Handler: cfg.keys})
	mcp.AddTool(srv, mcp.Tool[KeyArgs, tools.UserChange]{Name: tools.ToolAddKey, Description: DescribeAddKey, Effect: mcp.Additive, Handler: change(cfg, tools.ToolAddKey, cfg.addKey)})
	mcp.AddTool(srv, mcp.Tool[KeyArgs, tools.KeyRemoved]{Name: tools.ToolRemoveKey, Description: DescribeRemoveKey, Effect: mcp.Destructive, Handler: change(cfg, tools.ToolRemoveKey, cfg.removeKey)})
}

// RegisterAdmin installs the user tools followed by the account admin tools.
func RegisterAdmin(srv *mcp.Server, cfg Config) {
	RegisterUser(srv, cfg)
	mcp.AddTool(srv, mcp.Tool[NoArgs, UserList]{Name: tools.ToolUsers, Description: DescribeUsers, Effect: mcp.Read, Handler: cfg.users})
	mcp.AddTool(srv, mcp.Tool[UserArgs, tools.UserChange]{Name: tools.ToolLockUser, Description: DescribeLockUser, Effect: mcp.Additive, Handler: change(cfg, tools.ToolLockUser, userOperation(cfg.Store.LockAs))})
	mcp.AddTool(srv, mcp.Tool[UserArgs, tools.UserChange]{Name: tools.ToolUnlockUser, Description: DescribeUnlockUser, Effect: mcp.Additive, Handler: change(cfg, tools.ToolUnlockUser, userOperation(cfg.Store.UnlockAs))})
	mcp.AddTool(srv, mcp.Tool[UserArgs, tools.UserChange]{Name: tools.ToolDeleteUser, Description: DescribeDeleteUser, Effect: mcp.Destructive, Handler: change(cfg, tools.ToolDeleteUser, userOperation(cfg.Store.DeleteAs))})
	mcp.AddTool(srv, mcp.Tool[SetAdminArgs, tools.UserChange]{Name: tools.ToolSetAdmin, Description: DescribeSetAdmin, Effect: mcp.Additive, Handler: change(cfg, tools.ToolSetAdmin, cfg.setAdmin)})
	mcp.AddTool(srv, mcp.Tool[NoArgs, tools.InviteMade]{Name: tools.ToolCreateInvite, Description: DescribeCreateInvite, Effect: mcp.Additive, Handler: change(cfg, tools.ToolCreateInvite, cfg.createInvite)})
	mcp.AddTool(srv, mcp.Tool[NoArgs, tools.InviteList]{Name: tools.ToolListInvites, Description: DescribeListInvites, Effect: mcp.Read, Handler: cfg.listInvites})
	mcp.AddTool(srv, mcp.Tool[tools.InviteArgs, tools.Invite]{Name: tools.ToolRevokeInvite, Description: DescribeRevokeInvite, Effect: mcp.Destructive, Handler: change(cfg, tools.ToolRevokeInvite, cfg.revokeInvite)})
}

func actor(c identity.Caller) string {
	name, ok := strings.CutPrefix(c.UserID, ActorPrefix)
	if !ok {
		return ""
	}
	return name
}

// changeRecord identifies the actor and records the change's trail fields.
// Only the guarded store methods decide whether the actor can act.
type changeRecord struct {
	actor, host, account, user string
	key                        store.Key
}

func change[A, R any](cfg Config, name string, run func(context.Context, identity.Caller, A, changeRecord) (R, error)) func(context.Context, identity.Caller, A) (R, error) {
	return func(ctx context.Context, c identity.Caller, args A) (R, error) {
		record := changeRecord{actor: actor(c)}
		var out R
		err := record.prepare(args)
		if err == nil {
			err = cfg.locate(ctx, &record)
		}
		if err == nil {
			out, err = run(ctx, c, args, record)
		}
		result := trail.ResultOK
		if err != nil {
			result = trail.ResultRefused
			if err.Error() == store.Unreachable {
				result = trail.ResultFailed
			}
		}
		trail.Change(ctx, cfg.Telemetry, name, record.host, record.account, record.user, "", result)
		return out, err
	}
}

func (r *changeRecord) prepare(args any) error {
	switch a := args.(type) {
	case KeyArgs:
		r.user = r.actor
		key, err := valid.ParseKey(a.Key)
		if err != nil {
			return errors.New(tools.InvalidKey)
		}
		if r.actor == "" {
			return errors.New(tools.NoUser)
		}
		r.key = store.Key{Type: key.Type, Body: key.Body, Line: key.Line()}
	case UserArgs:
		r.user = a.User
	case SetAdminArgs:
		r.user = a.User
	}
	if r.actor == "" {
		return errors.New(OnlyAdmin)
	}
	return nil
}

func (cfg Config) locate(ctx context.Context, r *changeRecord) error {
	u, err := cfg.Store.UserNamed(ctx, r.actor)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return errors.New(store.Unreachable)
	}
	a, err := cfg.Store.Account(ctx, u.Account)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return errors.New(store.Unreachable)
	}
	r.account, r.host = a.Slug, a.Host
	return nil
}

func storeError(err error, missing string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotActive):
		return errors.New(tools.NoUser)
	case errors.Is(err, store.ErrNotAdmin):
		return errors.New(OnlyAdmin)
	case errors.Is(err, store.ErrSelf):
		return errors.New(NotSelf)
	case errors.Is(err, store.ErrLastKey):
		return errors.New(LastKey)
	case errors.Is(err, store.ErrKeyNotHeld):
		return errors.New(tools.KeyNotHeld)
	case errors.Is(err, store.ErrTooManyKeys):
		return errors.New(tools.TooManyKeys)
	case errors.Is(err, store.ErrNotLocked):
		return errors.New(tools.UserNotLocked)
	case errors.Is(err, store.ErrUserRemoving):
		return errors.New(tools.UserRemoving)
	case errors.Is(err, store.ErrTooManyInvites):
		return errors.New(tools.TooManyInvites)
	case errors.Is(err, store.ErrAccountRemoving):
		return errors.New(tools.AccountRemoving)
	case errors.Is(err, store.ErrNotFound):
		return errors.New(missing)
	default:
		return errors.New(store.Unreachable)
	}
}

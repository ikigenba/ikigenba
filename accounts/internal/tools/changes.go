// Package tools exposes the MCP catalog for SSH hosting.
package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func checkSize(kind *string, root, data *int) error {
	if kind != nil && valid.Type(*kind) != nil {
		return errors.New(TypeNotAllowed)
	}
	if root != nil && valid.RootGB(*root) != nil || data != nil && valid.DataGB(*data) != nil {
		return errors.New(SizeOutOfRange)
	}
	return nil
}

func (r *changeRecord) hostResult(h store.Host, op store.Operation, before string) HostChange {
	r.host, r.before, r.after, r.operation = h.Name, before, h.State, op
	return HostChange{Host: h.Name, State: h.State, Operation: op.ID}
}

func (cfg Config) createHost(ctx context.Context, c identity.Caller, a CreateHostArgs, r *changeRecord) (HostChange, error) {
	if err := checkSize(a.Type, a.RootGB, a.DataGB); err != nil {
		return HostChange{}, err
	}
	spec := store.HostSpec{
		Type: value(a.Type, valid.DefaultType), RootGB: value(a.RootGB, valid.DefaultRootGB),
		DataGB: value(a.DataGB, valid.DefaultDataGB),
	}
	h, op, err := cfg.Store.CreateHost(ctx, spec, c)
	if err != nil {
		return HostChange{}, storeError(err, NoHost)
	}
	return r.hostResult(h, op, ""), nil
}

func (cfg Config) hostOperation(kind string) func(context.Context, identity.Caller, HostArgs, *changeRecord) (HostChange, error) {
	return func(ctx context.Context, c identity.Caller, a HostArgs, r *changeRecord) (HostChange, error) {
		if _, err := cfg.Store.Host(ctx, a.Host); err != nil {
			return HostChange{}, storeError(err, NoHost)
		}
		h, op, err := cfg.Store.StartHostOp(ctx, a.Host, kind, store.Target{}, c)
		if err != nil {
			return HostChange{}, storeError(err, NoHost)
		}
		return r.hostResult(h, op, op.ResumeState), nil
	}
}

func (cfg Config) retryHost(ctx context.Context, c identity.Caller, a HostArgs, r *changeRecord) (HostChange, error) {
	if _, err := cfg.Store.Host(ctx, a.Host); err != nil {
		return HostChange{}, storeError(err, NoHost)
	}
	h, op, err := cfg.Store.RetryHost(ctx, a.Host, c)
	if err != nil {
		return HostChange{}, storeError(err, NoHost)
	}
	return r.hostResult(h, op, store.HostFailed), nil
}

func (cfg Config) resizeHost(ctx context.Context, c identity.Caller, a ResizeHostArgs, r *changeRecord) (HostResize, error) {
	if a.Type == nil && a.RootGB == nil && a.DataGB == nil {
		return HostResize{}, errors.New(NothingToChange)
	}
	if err := checkSize(a.Type, a.RootGB, a.DataGB); err != nil {
		return HostResize{}, err
	}
	if _, err := cfg.Store.Host(ctx, a.Host); err != nil {
		return HostResize{}, storeError(err, NoHost)
	}
	target := store.Target{Type: value(a.Type, ""), RootGB: value(a.RootGB, 0), DataGB: value(a.DataGB, 0)}
	h, op, err := cfg.Store.StartHostOp(ctx, a.Host, store.OpResize, target, c)
	if err != nil {
		return HostResize{}, storeError(err, NoHost)
	}
	answer := r.hostResult(h, op, op.ResumeState)
	return HostResize{
		Host: answer.Host, State: answer.State, Operation: answer.Operation,
		Restarts: op.ResumeState == store.HostRunning && op.Target.Type != "",
	}, nil
}

func nameError(err error) error {
	if errors.Is(err, valid.ErrReserved) {
		return errors.New(ReservedName)
	}
	return errors.New(InvalidName)
}

func (cfg Config) ensureNameFree(ctx context.Context, name string) error {
	if _, err := cfg.Store.Account(ctx, name); err == nil {
		return errors.New(NameTaken)
	} else if !errors.Is(err, store.ErrNotFound) {
		return storeError(err, NoAccount)
	}
	// User looks up the global user name first. A user in any other account
	// still occupies the namespace.
	_, err := cfg.Store.User(ctx, name, name)
	switch {
	case err == nil || errors.Is(err, store.ErrNotInAccount):
		return errors.New(NameTaken)
	case errors.Is(err, store.ErrNotFound):
		return nil
	default:
		return storeError(err, NoUser)
	}
}

func (cfg Config) createAccount(ctx context.Context, c identity.Caller, a CreateAccountArgs, r *changeRecord) (Account, error) {
	if err := valid.Slug(a.Account); err != nil {
		return Account{}, nameError(err)
	}
	h, err := cfg.Store.Host(ctx, a.Host)
	if err != nil {
		return Account{}, storeError(err, NoHost)
	}
	if h.State == store.HostDeleting {
		return Account{}, errors.New(HostGone)
	}
	if err := cfg.ensureNameFree(ctx, a.Account); err != nil {
		return Account{}, err
	}
	if h.Accounts >= valid.MaxAccountsPerHost {
		return Account{}, errors.New(HostFull)
	}
	if err := cfg.checkDNS(ctx, a.Account); err != nil {
		return Account{}, err
	}
	account, op, err := cfg.Store.PlaceAccount(ctx, a.Account, a.Host, c)
	if err != nil {
		return Account{}, storeError(err, NoHost)
	}
	r.after, r.operation = account.State, op
	return cfg.accountAnswer(account), nil
}

func (cfg Config) checkDNS(ctx context.Context, slug string) error {
	check, cancel := context.WithTimeout(ctx, DNSCheckTimeout)
	defer cancel()
	records, err := cfg.Cloud.FindRecords(check, cfg.Hosts.ZoneID, slug+"."+cfg.Hosts.Domain)
	if err != nil {
		return errors.New(CloudFailed)
	}
	if len(records) != 0 {
		return errors.New(NameHasRecord)
	}
	return nil
}

func (cfg Config) removeAccount(ctx context.Context, c identity.Caller, a AccountArgs, r *changeRecord) (Account, error) {
	before, err := cfg.Store.Account(ctx, a.Account)
	if err != nil {
		return Account{}, storeError(err, NoAccount)
	}
	r.host = before.Host
	after, op, err := cfg.Store.RemoveAccount(ctx, a.Account, c)
	if err != nil {
		return Account{}, storeError(err, NoAccount)
	}
	r.host, r.before, r.after, r.operation = after.Host, op.ResumeState, after.State, op
	return cfg.accountAnswer(after), nil
}

func (cfg Config) readUserAccount(ctx context.Context, r *changeRecord) error {
	a, err := cfg.Store.Account(ctx, r.account)
	if err != nil {
		return storeError(err, NoAccount)
	}
	r.host = a.Host
	if r.user == "" {
		return errors.New(NoUser)
	}
	return nil
}

func parseKey(line string) (store.Key, error) {
	k, err := valid.ParseKey(line)
	if err != nil {
		return store.Key{}, errors.New(InvalidKey)
	}
	return store.Key{Type: k.Type, Body: k.Body, Line: k.Line()}, nil
}

func userChange(u store.User) UserChange {
	return UserChange{User: u.Name, Account: u.Account, State: u.State, Pending: u.Pending}
}

func (cfg Config) addUser(ctx context.Context, c identity.Caller, a UserKeyArgs, r *changeRecord) (UserChange, error) {
	if err := valid.Name(a.User); err != nil {
		return UserChange{}, nameError(err)
	}
	key, err := parseKey(a.Key)
	if err != nil {
		return UserChange{}, err
	}
	if err := cfg.readUserAccount(ctx, r); err != nil {
		return UserChange{}, err
	}
	u, err := cfg.Store.AddUser(ctx, a.Account, a.User, key, c)
	if err != nil {
		return UserChange{}, storeError(err, NoAccount)
	}
	return userChange(u), nil
}

func (cfg Config) addKey(ctx context.Context, _ identity.Caller, a UserKeyArgs, r *changeRecord) (UserChange, error) {
	key, err := parseKey(a.Key)
	if err != nil {
		return UserChange{}, err
	}
	if err := cfg.readUserAccount(ctx, r); err != nil {
		return UserChange{}, err
	}
	u, err := cfg.Store.AddKey(ctx, a.Account, a.User, key)
	if err != nil {
		return UserChange{}, storeError(err, NoUser)
	}
	return userChange(u), nil
}

func (cfg Config) removeKey(ctx context.Context, _ identity.Caller, a UserKeyArgs, r *changeRecord) (KeyRemoved, error) {
	key, err := parseKey(a.Key)
	if err != nil {
		return KeyRemoved{}, err
	}
	if err := cfg.readUserAccount(ctx, r); err != nil {
		return KeyRemoved{}, err
	}
	u, left, err := cfg.Store.RemoveKey(ctx, a.Account, a.User, key)
	if err != nil {
		return KeyRemoved{}, storeError(err, NoUser)
	}
	return KeyRemoved{User: u.Name, Account: u.Account, State: u.State, Pending: u.Pending, KeysLeft: left}, nil
}

func (cfg Config) userOperation(operation func(context.Context, string, string) (store.User, error)) func(context.Context, identity.Caller, UserArgs, *changeRecord) (UserChange, error) {
	return func(ctx context.Context, _ identity.Caller, a UserArgs, r *changeRecord) (UserChange, error) {
		if err := cfg.readUserAccount(ctx, r); err != nil {
			return UserChange{}, err
		}
		u, err := operation(ctx, a.Account, a.User)
		if err != nil {
			return UserChange{}, storeError(err, NoUser)
		}
		return userChange(u), nil
	}
}

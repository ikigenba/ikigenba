package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

func value[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

func window(token paging.Token, page *string, limit *int, after ...any) (int, error) {
	size, err := paging.Window(limit)
	if err != nil {
		return 0, err
	}
	if p := value(page, ""); p != "" {
		if err := token.Decode(p, after...); err != nil {
			return 0, errors.New(paging.BadPage)
		}
	}
	return size, nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func hostAnswer(h store.Host, now time.Time) Host {
	return Host{
		Host: h.Name, State: h.State, Reason: h.Reason, Health: store.Health(h, now),
		Type: h.Type, RootGB: h.RootGB, DataGB: h.DataGB, EIP: h.EIP,
		Accounts: h.Accounts, Reported: stamp(h.Reported),
	}
}

func (cfg Config) accountAnswer(a store.Account) Account {
	return Account{
		Account: a.Slug, Host: a.Host, DNSName: a.Slug + "." + cfg.Hosts.Domain,
		State: a.State, Reason: a.Reason, Users: a.Users, Created: stamp(a.Created), CreatedBy: a.CreatedBy,
	}
}

func userAnswer(u store.User) User {
	keys := make([]string, 0, len(u.Keys))
	for _, k := range u.Keys {
		keys = append(keys, k.Line)
	}
	return User{
		User: u.Name, Account: u.Account, State: u.State, Pending: u.Pending,
		Keys: keys, LastSignIn: stamp(u.LastSignIn), Created: stamp(u.Created), CreatedBy: u.CreatedBy,
	}
}

func (cfg Config) hosts(ctx context.Context, a HostsArgs) (HostList, error) {
	token := paging.Token{Tool: ToolHosts}
	var after int64
	size, err := window(token, a.Page, a.Limit, &after)
	if err != nil {
		return HostList{}, err
	}
	rows, err := cfg.Store.ListHosts(ctx, after, size+1)
	if err != nil {
		return HostList{}, storeError(err, NoHost)
	}
	var out HostList
	if len(rows) > size {
		next := token.Encode(rows[size-1].Seq)
		out.Next, rows = &next, rows[:size]
	}
	out.Hosts = make([]Host, 0, len(rows))
	now := cfg.Telemetry.Now()
	for _, h := range rows {
		out.Hosts = append(out.Hosts, hostAnswer(h, now))
	}
	return out, nil
}

func (cfg Config) accounts(ctx context.Context, a AccountsArgs) (AccountList, error) {
	token := paging.Token{Tool: ToolAccounts}
	var after string
	size, err := window(token, a.Page, a.Limit, &after)
	if err != nil {
		return AccountList{}, err
	}
	rows, err := cfg.Store.ListAccounts(ctx, after, size+1)
	if err != nil {
		return AccountList{}, storeError(err, NoAccount)
	}
	var out AccountList
	if len(rows) > size {
		next := token.Encode(rows[size-1].Slug)
		out.Next, rows = &next, rows[:size]
	}
	out.Accounts = make([]Account, 0, len(rows))
	for _, a := range rows {
		out.Accounts = append(out.Accounts, cfg.accountAnswer(a))
	}
	return out, nil
}

func (cfg Config) users(ctx context.Context, a UsersArgs) (UserList, error) {
	token := paging.Token{Tool: ToolUsers}
	var afterAccount, afterName string
	size, err := window(token, a.Page, a.Limit, &afterAccount, &afterName)
	if err != nil {
		return UserList{}, err
	}
	account := value(a.Account, "")
	if account != "" {
		if _, err := cfg.Store.Account(ctx, account); err != nil {
			return UserList{}, storeError(err, NoAccount)
		}
	}
	rows, err := cfg.Store.ListUsers(ctx, account, afterAccount, afterName, size+1)
	if err != nil {
		return UserList{}, storeError(err, NoAccount)
	}
	var out UserList
	if len(rows) > size {
		last := rows[size-1]
		next := token.Encode(last.Account, last.Name)
		out.Next, rows = &next, rows[:size]
	}
	out.Users = make([]User, 0, len(rows))
	for _, u := range rows {
		out.Users = append(out.Users, userAnswer(u))
	}
	return out, nil
}

func (cfg Config) operation(ctx context.Context, id string) (*Operation, error) {
	if id == "" {
		return nil, nil
	}
	op, err := cfg.Store.Operation(ctx, id)
	if err != nil {
		return nil, errors.New(store.Unreachable)
	}
	if op.Status != store.OpRunning && op.Status != store.OpFailed {
		return nil, nil
	}
	return &Operation{
		Operation: op.ID, Kind: op.Kind, Step: op.Step, Status: op.Status,
		Error: op.Error, Started: stamp(op.Started),
	}, nil
}

func (cfg Config) host(ctx context.Context, a HostArgs) (HostRead, error) {
	h, err := cfg.Store.Host(ctx, a.Host)
	if err != nil {
		return HostRead{}, storeError(err, NoHost)
	}
	accounts, err := cfg.Store.AccountsOn(ctx, h.Name)
	if err != nil {
		return HostRead{}, storeError(err, NoHost)
	}
	names := make([]string, 0, len(accounts))
	for _, a := range accounts {
		names = append(names, a.Slug)
	}
	answer := hostAnswer(h, cfg.Telemetry.Now())
	out := HostRead{
		Host: answer.Host, State: answer.State, Reason: answer.Reason, Health: answer.Health,
		Type: answer.Type, RootGB: answer.RootGB, DataGB: answer.DataGB, EIP: answer.EIP,
		Accounts: answer.Accounts, Reported: answer.Reported,
		InstanceID: h.InstanceID, DataVolume: h.DataVolume, Version: h.Version, Applied: h.Applied,
		Created: stamp(h.Created), CreatedBy: h.CreatedBy, AccountNames: names, Allowlist: append([]string{}, h.Allowlist...),
	}
	switch h.State {
	case store.HostRunning, store.HostStopped:
	default:
		out.Operation, err = cfg.operation(ctx, h.Operation)
		if err != nil {
			return HostRead{}, err
		}
	}
	report, err := cfg.Store.Report(ctx, h.Name)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return HostRead{}, errors.New(store.Unreachable)
	}
	if report != nil {
		out.Report, err = json.Marshal(report.Body)
		if err != nil {
			return HostRead{}, errors.New(store.Unreachable)
		}
	}
	return out, nil
}

func (cfg Config) account(ctx context.Context, args AccountArgs) (AccountRead, error) {
	a, err := cfg.Store.Account(ctx, args.Account)
	if err != nil {
		return AccountRead{}, storeError(err, NoAccount)
	}
	users, err := cfg.Store.UsersOf(ctx, a.Slug)
	if err != nil {
		return AccountRead{}, storeError(err, NoAccount)
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Name)
	}
	op, err := cfg.operation(ctx, a.Operation)
	if err != nil {
		return AccountRead{}, err
	}
	answer := cfg.accountAnswer(a)
	return AccountRead{
		Account: answer.Account, Host: answer.Host, DNSName: answer.DNSName, State: answer.State,
		Reason: answer.Reason, Users: answer.Users, Created: answer.Created, CreatedBy: answer.CreatedBy,
		UserNames: names, Operation: op,
	}, nil
}

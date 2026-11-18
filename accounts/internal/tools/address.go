package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

func (cfg Config) unbanIP(ctx context.Context, _ identity.Caller, _ UnbanArgs, r *changeRecord) (Unbanned, error) {
	if r.ipErr != nil {
		return Unbanned{}, errors.New(InvalidAddress)
	}
	if r.host == "" {
		n, err := cfg.Store.UnbanRunning(ctx, r.ip)
		if err != nil {
			return Unbanned{}, storeError(err, NoHost)
		}
		return Unbanned{IP: r.ip, Hosts: n}, nil
	}
	_, err := cfg.Store.Unban(ctx, r.host, r.ip)
	if errors.Is(err, store.ErrHostGone) {
		return Unbanned{}, errors.New(NoHost)
	}
	if err != nil {
		return Unbanned{}, storeError(err, NoHost)
	}
	return Unbanned{IP: r.ip, Host: r.host, Hosts: 1}, nil
}

func (cfg Config) allowlistOperation(operation func(context.Context, string, string) (store.Host, error)) func(context.Context, identity.Caller, HostIPArgs, *changeRecord) (AllowlistChange, error) {
	return func(ctx context.Context, _ identity.Caller, _ HostIPArgs, r *changeRecord) (AllowlistChange, error) {
		if errors.Is(r.ipErr, valid.ErrPrefix) {
			return AllowlistChange{}, errors.New(PrefixTooWide)
		}
		if r.ipErr != nil {
			return AllowlistChange{}, errors.New(InvalidAddress)
		}
		if r.host == "" {
			_, err := cfg.Store.Host(ctx, r.host)
			return AllowlistChange{}, storeError(err, NoHost)
		}
		host, err := operation(ctx, r.host, r.ip)
		if errors.Is(err, store.ErrHostGone) {
			// The store uses the same guard for deleting and deleted hosts.
			// Its live-host lookup distinguishes the two for the tool answer.
			if _, lookupErr := cfg.Store.Host(ctx, r.host); lookupErr != nil {
				return AllowlistChange{}, storeError(lookupErr, NoHost)
			}
		}
		if err != nil {
			return AllowlistChange{}, storeError(err, NoHost)
		}
		return AllowlistChange{
			Host: host.Name, IP: r.ip, Allowlist: append([]string{}, host.Allowlist...),
			Pending: host.Version > host.Applied,
		}, nil
	}
}

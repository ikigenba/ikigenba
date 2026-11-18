package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

func scanString(r scanner) (string, error) {
	var value string
	err := r.Scan(&value)
	return value, err
}

func allowlist(q query, host string) ([]string, error) {
	return collect(q, scanString, "SELECT entry FROM host_allowlist WHERE host = ? ORDER BY entry", host)
}

// Unban queues an address for the next successful apply on a running host.
func (s *Store) Unban(ctx context.Context, host, ip string) (Host, error) {
	canonical, err := valid.Address(ip)
	return write(ctx, s, host != "" && err == nil && canonical == ip, func(w *writing) (Host, error) {
		h, err := getHost(w.query, host)
		if err != nil {
			return Host{}, err
		}
		switch h.State {
		case HostDeleted:
			return Host{}, ErrHostGone
		case HostRunning:
		default:
			return Host{}, ErrNotRunning
		}
		h.Version, err = recordUnban(w, h.Name, ip, h.Version)
		return h, err
	})
}

func recordUnban(w *writing, host, ip string, version int64) (int64, error) {
	var count int
	var held bool
	err := w.row("SELECT count(*), coalesce(max(address = ?), 0) FROM host_unbans WHERE host = ?", ip, host).Scan(&count, &held)
	if err != nil || held {
		return version, err
	}
	if count >= valid.MaxUnbansPerHost {
		return 0, ErrTooManyUnbans
	}
	version, err = w.bump(host)
	if err != nil {
		return 0, err
	}
	return version, w.exec("INSERT INTO host_unbans (host, address, version) VALUES (?, ?, ?)", host, ip, version)
}

// UnbanRunning queues an address across all running hosts or changes none.
func (s *Store) UnbanRunning(ctx context.Context, ip string) (int, error) {
	canonical, err := valid.Address(ip)
	return write(ctx, s, err == nil && canonical == ip, func(w *writing) (int, error) {
		var seq int64
		count := 0
		for {
			var host string
			var version int64
			err := w.row("SELECT name, seq, version FROM hosts WHERE seq > ? AND state = ? ORDER BY seq LIMIT 1",
				seq, HostRunning).Scan(&host, &seq, &version)
			if errors.Is(err, sql.ErrNoRows) {
				return count, nil
			}
			if err != nil {
				return 0, err
			}
			if _, err := recordUnban(w, host, ip, version); err != nil {
				return 0, err
			}
			count++
		}
	})
}

// Allow adds a canonical entry to the host's desired allowlist.
func (s *Store) Allow(ctx context.Context, host, entry string) (Host, error) {
	return s.editAllowlist(ctx, host, entry, true)
}

// Disallow removes a canonical entry from the host's desired allowlist.
func (s *Store) Disallow(ctx context.Context, host, entry string) (Host, error) {
	return s.editAllowlist(ctx, host, entry, false)
}

func (s *Store) editAllowlist(ctx context.Context, host, entry string, add bool) (Host, error) {
	canonical, err := valid.AllowEntry(entry)
	return write(ctx, s, host != "" && err == nil && canonical == entry, func(w *writing) (Host, error) {
		h, err := getHost(w.query, host)
		if err != nil {
			return Host{}, err
		}
		if h.State == HostDeleting || h.State == HostDeleted {
			return Host{}, ErrHostGone
		}
		pos, held := slices.BinarySearch(h.Allowlist, entry)
		if held == add {
			return h, nil
		}
		if add {
			if len(h.Allowlist) >= valid.MaxAllowlistPerHost {
				return Host{}, ErrAllowlistFull
			}
			h.Allowlist = slices.Insert(h.Allowlist, pos, entry)
			err = w.exec("INSERT INTO host_allowlist (host, entry) VALUES (?, ?)", host, entry)
		} else {
			h.Allowlist = slices.Delete(h.Allowlist, pos, pos+1)
			err = w.exec("DELETE FROM host_allowlist WHERE host = ? AND entry = ?", host, entry)
		}
		if err != nil {
			return Host{}, err
		}
		h.Version, err = w.bump(host)
		return h, err
	})
}

package pages

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/appkit/paging"
)

type direction int

const (
	invalid direction = iota
	first
	after
	before
	last
)

type position struct {
	value     string
	direction direction
}

func decode(name, value string, key ...any) position {
	switch {
	case value == "":
		return position{direction: first}
	case (paging.Token{Tool: name}).Decode(value, key...) == nil:
		return position{value, after}
	case (paging.Token{Tool: name + "-before"}).Decode(value, key...) == nil:
		return position{value, before}
	case value == "last":
		return position{value, last}
	default:
		return position{value, invalid}
	}
}

type listPositions struct {
	hosts, accounts, users, volumes position
	hostSeq                         int64
	slug, userAccount, userName     string
	volume                          volumeKey
}

const (
	hostsPage    = "hosts_page"
	accountsPage = "accounts_page"
	usersPage    = "users_page"
	volumesPage  = "volumes_page"
)

func readPositions(query url.Values) listPositions {
	var p listPositions
	p.hosts = decode(tools.ToolHosts, query.Get(hostsPage), &p.hostSeq)
	p.accounts = decode(tools.ToolAccounts, query.Get(accountsPage), &p.slug)
	p.users = decode(tools.ToolUsers, query.Get(usersPage), &p.userAccount, &p.userName)
	var deleted int64
	p.volumes = decode(tools.ToolKeptVolumes, query.Get(volumesPage), &deleted, &p.volume.id)
	if p.volumes.direction == before || p.volumes.direction == after {
		p.volume.deleted = time.Unix(0, deleted)
	}
	return p
}

func (p listPositions) valid() bool {
	return p.hosts.direction != invalid && p.accounts.direction != invalid && p.users.direction != invalid && p.volumes.direction != invalid
}

type window struct {
	position
	prev, next string
	underflow  bool
}

func (w window) canonical(parameter string) string {
	if w.value == "" || w.underflow {
		return ""
	}
	return parameter + "=" + url.QueryEscape(w.value)
}

func canonicalLanding(hosts, accounts, users, volumes window) string {
	var parameters []string
	for _, encoded := range []string{hosts.canonical(hostsPage), accounts.canonical(accountsPage), users.canonical(usersPage), volumes.canonical(volumesPage)} {
		if encoded != "" {
			parameters = append(parameters, encoded)
		}
	}
	if len(parameters) == 0 {
		return "/"
	}
	return "/?" + strings.Join(parameters, "&")
}

type volumeKey struct {
	deleted time.Time
	id      string
}

func volumeListing(st *store.Snapshot) listing[store.KeptVolume, volumeKey] {
	return listing[store.KeptVolume, volumeKey]{
		name: tools.ToolKeptVolumes,
		key:  func(v store.KeptVolume) volumeKey { return volumeKey{v.Deleted, v.Volume} },
		encode: func(t paging.Token, k volumeKey) string {
			return t.Encode(k.deleted.UnixNano(), k.id)
		},
		forward: func(ctx context.Context, k volumeKey, n int) ([]store.KeptVolume, error) {
			return st.ListKeptVolumes(ctx, k.deleted, k.id, n)
		},
		backward: func(ctx context.Context, k volumeKey, n int) ([]store.KeptVolume, error) {
			return st.ListKeptVolumesBefore(ctx, k.deleted, k.id, n)
		},
		last: st.LastKeptVolumes,
	}
}

type listing[T, K any] struct {
	name              string
	key               func(T) K
	encode            func(paging.Token, K) string
	forward, backward func(context.Context, K, int) ([]T, error)
	last              func(context.Context, int) ([]T, error)
}

func (q listing[T, K]) read(ctx context.Context, p position, k K) ([]T, window, error) {
	const limit = paging.DefaultLimit
	var entries []T
	var err error
	switch p.direction {
	case before:
		entries, err = q.backward(ctx, k, limit+1)
	case last:
		entries, err = q.last(ctx, limit+1)
	case first, after:
		entries, err = q.forward(ctx, k, limit+1)
	}
	w := window{position: p}
	if err != nil || len(entries) == 0 {
		w.underflow = p.direction == before || p.direction == last
		return entries, w, err
	}
	extra := len(entries) > limit
	if p.direction == before || p.direction == last {
		w.underflow = !extra
		if extra {
			clear(entries[:1])
			entries = entries[1:]
		}
	} else if extra {
		entries = entries[:limit]
	}
	if p.direction != first && !w.underflow {
		w.prev = q.encode(paging.Token{Tool: q.name + "-before"}, q.key(entries[0]))
	}
	end := q.key(entries[len(entries)-1])
	if p.direction == before && !w.underflow {
		// The position may have disappeared, so check for a greater key.
		following, err := q.forward(ctx, end, 1)
		if err != nil {
			return nil, w, err
		}
		if len(following) != 0 {
			w.next = q.encode(paging.Token{Tool: q.name}, end)
		}
	} else if (p.direction == first || p.direction == after) && extra {
		w.next = q.encode(paging.Token{Tool: q.name}, end)
	}
	return entries, w, nil
}

func hostListing(st *store.Snapshot) listing[store.Host, int64] {
	return listing[store.Host, int64]{
		name: tools.ToolHosts, key: func(h store.Host) int64 { return h.Seq },
		encode:  func(t paging.Token, seq int64) string { return t.Encode(seq) },
		forward: st.ListHosts, backward: st.ListHostsBefore, last: st.LastHosts,
	}
}

func accountListing(st *store.Snapshot) listing[store.Account, string] {
	return listing[store.Account, string]{
		name: tools.ToolAccounts, key: func(a store.Account) string { return a.Slug },
		encode:  func(t paging.Token, slug string) string { return t.Encode(slug) },
		forward: st.ListAccounts, backward: st.ListAccountsBefore, last: st.LastAccounts,
	}
}

type userKey struct{ account, name string }

func userListing(st *store.Snapshot) listing[store.User, userKey] {
	return listing[store.User, userKey]{
		name: tools.ToolUsers, key: func(u store.User) userKey { return userKey{u.Account, u.Name} },
		encode: func(t paging.Token, k userKey) string { return t.Encode(k.account, k.name) },
		forward: func(ctx context.Context, k userKey, n int) ([]store.User, error) {
			return st.ListUsers(ctx, "", k.account, k.name, n)
		},
		backward: func(ctx context.Context, k userKey, n int) ([]store.User, error) {
			return st.ListUsersBefore(ctx, "", k.account, k.name, n)
		},
		last: func(ctx context.Context, n int) ([]store.User, error) { return st.LastUsers(ctx, "", n) },
	}
}

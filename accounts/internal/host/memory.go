package host

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"sort"
	"strings"
	"sync"
)

// Memory is an isolated account host used by the sandbox.
type Memory struct {
	mu     sync.Mutex
	groups map[string]bool
	users  map[string]User
}

// NewMemory returns an empty host.
func NewMemory() *Memory { return &Memory{groups: make(map[string]bool), users: make(map[string]User)} }

func validName(n string) bool {
	if len(n) == 0 || len(n) > 32 {
		return false
	}
	for i := range len(n) {
		b := n[i]
		if b >= 'a' && b <= 'z' || b == '_' || i > 0 && (b >= '0' && b <= '9' || b == '-') {
			continue
		}
		return false
	}
	return true
}
func validKey(key string) bool {
	if strings.ContainsAny(key, "\r\n") {
		return false
	}
	f := strings.Fields(key)
	if len(f) < 2 || f[0] != "ssh-ed25519" {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(f[1])
	return err == nil && len(b) == 51 && binary.BigEndian.Uint32(b[:4]) == 11 && string(b[4:15]) == "ssh-ed25519" && binary.BigEndian.Uint32(b[15:19]) == 32
}
func check(ctx context.Context, names ...string) error {
	if ctx.Err() != nil {
		return &FailedError{-1}
	}
	for _, n := range names {
		if !validName(n) {
			return &RefusedError{2, RefuseInvalidName}
		}
	}
	return nil
}
func checkKey(key string) error {
	if !validKey(key) {
		return &RefusedError{2, RefuseInvalidKey}
	}
	return nil
}
func refusal(diagnostic string) error { return &RefusedError{4, diagnostic} }
func sameKey(a, b string) bool {
	x, y := strings.Fields(a), strings.Fields(b)
	return x[0] == y[0] && x[1] == y[1]
}
func cloneUser(u User) User {
	u.Groups = append([]string{}, u.Groups...)
	u.Keys = append([]string{}, u.Keys...)
	return u
}

// GroupAdd creates an empty group when no group or user holds its name.
func (m *Memory) GroupAdd(ctx context.Context, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, slug); err != nil {
		return err
	}
	if _, ok := m.users[slug]; ok || m.groups[slug] {
		return refusal(RefuseNameTaken)
	}
	m.groups[slug] = true
	return nil
}

// GroupRemove removes a group only when it has no members.
func (m *Memory) GroupRemove(ctx context.Context, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, slug); err != nil {
		return err
	}
	if !m.groups[slug] {
		return refusal(RefuseNoGroup)
	}
	for _, u := range m.users {
		for _, g := range u.Groups {
			if g == slug {
				return refusal(RefuseGroupNotEmpty)
			}
		}
	}
	delete(m.groups, slug)
	return nil
}

// GroupList lists all groups and their current membership counts.
func (m *Memory) GroupList(ctx context.Context) ([]Group, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx); err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(m.groups))
	for slug := range m.groups {
		count := 0
		for _, u := range m.users {
			for _, g := range u.Groups {
				if g == slug {
					count++
				}
			}
		}
		out = append(out, Group{slug, count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// GroupMembers lists the current members of one group.
func (m *Memory) GroupMembers(ctx context.Context, slug string) (Members, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, slug); err != nil {
		return Members{}, err
	}
	if !m.groups[slug] {
		return Members{}, refusal(RefuseNoGroup)
	}
	out := Members{slug, []string{}}
	for _, u := range m.users {
		for _, g := range u.Groups {
			if g == slug {
				out.Members = append(out.Members, u.Name)
			}
		}
	}
	sort.Strings(out.Members)
	return out, nil
}

// UserAdd creates an active user with one group and one key.
func (m *Memory) UserAdd(ctx context.Context, name, group, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, name, group); err != nil {
		return err
	}
	if err := checkKey(key); err != nil {
		return err
	}
	if _, ok := m.users[name]; ok || m.groups[name] {
		return refusal(RefuseNameTaken)
	}
	if !m.groups[group] {
		return refusal(RefuseNoGroup)
	}
	m.users[name] = User{Name: name, Groups: []string{group}, Keys: []string{key}, State: StateActive}
	return nil
}

// UserList returns independent snapshots of the users in name order.
func (m *Memory) UserList(ctx context.Context) ([]User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx); err != nil {
		return nil, err
	}
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		u = cloneUser(u)
		sort.Strings(u.Groups)
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (m *Memory) key(ctx context.Context, name, key string, add bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, name); err != nil {
		return err
	}
	if err := checkKey(key); err != nil {
		return err
	}
	u, ok := m.users[name]
	if !ok {
		return refusal(RefuseNoUser)
	}
	if add {
		for _, k := range u.Keys {
			if sameKey(k, key) {
				return nil
			}
		}
		u.Keys = append(u.Keys, key)
	} else {
		kept := make([]string, 0, len(u.Keys))
		for _, k := range u.Keys {
			if !sameKey(k, key) {
				kept = append(kept, k)
			}
		}
		u.Keys = kept
	}
	m.users[name] = u
	return nil
}

// UserKeyAdd adds a previously absent key identity.
func (m *Memory) UserKeyAdd(ctx context.Context, name, key string) error {
	return m.key(ctx, name, key, true)
}

// UserKeyRemove removes a key identity, keeping the user even without keys.
func (m *Memory) UserKeyRemove(ctx context.Context, name, key string) error {
	return m.key(ctx, name, key, false)
}
func (m *Memory) membership(ctx context.Context, name, group string, join bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, name, group); err != nil {
		return err
	}
	u, ok := m.users[name]
	if !ok {
		return refusal(RefuseNoUser)
	}
	if !m.groups[group] {
		return refusal(RefuseNoGroup)
	}
	kept := make([]string, 0, len(u.Groups)+1)
	for _, g := range u.Groups {
		if g != group {
			kept = append(kept, g)
		}
	}
	if join {
		kept = append(kept, group)
	}
	u.Groups = kept
	m.users[name] = u
	return nil
}

// UserJoin ensures that the user belongs to the group.
func (m *Memory) UserJoin(ctx context.Context, name, group string) error {
	return m.membership(ctx, name, group, true)
}

// UserLeave ensures that the user does not belong to the group.
func (m *Memory) UserLeave(ctx context.Context, name, group string) error {
	return m.membership(ctx, name, group, false)
}
func (m *Memory) state(ctx context.Context, name, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, name); err != nil {
		return err
	}
	u, ok := m.users[name]
	if !ok {
		return refusal(RefuseNoUser)
	}
	u.State = state
	m.users[name] = u
	return nil
}

// UserLock locks a user's access.
func (m *Memory) UserLock(ctx context.Context, name string) error {
	return m.state(ctx, name, StateLocked)
}

// UserUnlock activates a user's access.
func (m *Memory) UserUnlock(ctx context.Context, name string) error {
	return m.state(ctx, name, StateActive)
}

// UserDelete deletes a locked user and all of their memberships.
func (m *Memory) UserDelete(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := check(ctx, name); err != nil {
		return err
	}
	u, ok := m.users[name]
	if !ok {
		return refusal(RefuseNoUser)
	}
	if u.State != StateLocked {
		return refusal(RefuseUserNotLocked)
	}
	delete(m.users, name)
	return nil
}

package machine

import (
	"context"
	"io"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// Fixed facts reported by a simulated host.
const (
	MemoryOS                = "Simulated Amazon Linux 2023"
	MemoryKernel            = "simulated"
	MemoryUsed        int64 = 512 << 20
	MemoryAvailable   int64 = 3 << 30
	MemoryDiskUsed    int64 = 1 << 30
	MemoryInodesUsed  int64 = 50000
	MemoryInodesTotal int64 = 10000000
	MemoryBanTime           = 10 * time.Minute
)

// MemoryConfig controls timing on a simulated host.
type MemoryConfig struct {
	ApplyDelay time.Duration
	Sleep      func(context.Context, time.Duration)
	Now        func() time.Time
}

// Account is the account identity present on a simulated host.
type Account struct {
	Slug string
	GID  int64
}

// User is a login account on a simulated host.
type User struct {
	Name, Account string
	UID           int64
	Locked        bool
	Keys          []string
}

// Snapshot describes the current contents of a simulated host.
type Snapshot struct {
	Accounts  []Account
	Users     []User
	Folders   []string
	Daemon    string
	Applies   int
	Allowlist []string
	Bans      []protocol.Ban
}

// Memory is an in-memory machine for tests and sandbox hosts.
type Memory struct {
	mu        sync.Mutex
	cfg       MemoryConfig
	booted    time.Time
	accounts  map[string]Account
	users     map[string]User
	folders   map[string]struct{}
	daemon    string
	applies   int
	failure   error
	allowlist []string
	bans      map[string]protocol.Ban
}

// NewMemory makes an empty simulated host.
func NewMemory(cfg MemoryConfig) *Memory {
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, duration time.Duration) {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
		}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Memory{
		cfg: cfg, booted: cfg.Now().UTC().Truncate(time.Second),
		accounts: make(map[string]Account), users: make(map[string]User),
		folders: make(map[string]struct{}), bans: make(map[string]protocol.Ban),
	}
}

// State returns independent copies of the simulated host's records.
func (m *Memory) State() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Snapshot{
		Daemon: m.daemon, Applies: m.applies, Allowlist: slices.Clone(m.allowlist), Bans: m.activeBans(),
	}
	for _, account := range m.accounts {
		out.Accounts = append(out.Accounts, account)
	}
	for _, user := range m.users {
		user.Keys = slices.Clone(user.Keys)
		out.Users = append(out.Users, user)
	}
	for slug := range m.folders {
		out.Folders = append(out.Folders, slug)
	}
	sort.Slice(out.Accounts, func(i, j int) bool { return out.Accounts[i].Slug < out.Accounts[j].Slug })
	sort.Slice(out.Users, func(i, j int) bool { return out.Users[i].Name < out.Users[j].Name })
	slices.Sort(out.Folders)
	return out
}

// RangeUnlockedUsers visits the name and uid of each unlocked applied user.
// The callback runs under the machine's lock and must not call this machine.
func (m *Memory) RangeUnlockedUsers(visit func(name string, uid int64)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, user := range m.users {
		if !user.Locked {
			visit(user.Name, user.UID)
		}
	}
}

// Ban records a simulated jail ban when the address is eligible.
func (m *Memory) Ban(address string) {
	if canonical, err := valid.Address(address); err != nil || canonical != address {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.allowed(address) {
		return
	}
	now := m.cfg.Now()
	m.expireBans(now)
	if _, exists := m.bans[address]; !exists && len(m.bans) >= protocol.MaxBans {
		return
	}
	start := now.UTC().Truncate(time.Second)
	m.bans[address] = protocol.Ban{Address: address, Banned: start, Expires: start.Add(MemoryBanTime)}
}

func (m *Memory) allowed(address string) bool {
	for _, entry := range m.allowlist {
		if valid.Covers(entry, address) {
			return true
		}
	}
	return false
}

func (m *Memory) expireBans(now time.Time) {
	for address, ban := range m.bans {
		if !ban.Expires.After(now) {
			delete(m.bans, address)
		}
	}
}

func (m *Memory) activeBans() []protocol.Ban {
	m.expireBans(m.cfg.Now())
	var bans []protocol.Ban
	for _, ban := range m.bans {
		bans = append(bans, ban)
	}
	sort.Slice(bans, func(i, j int) bool {
		if bans[i].Banned.Equal(bans[j].Banned) {
			return bans[i].Address < bans[j].Address
		}
		return bans[i].Banned.After(bans[j].Banned)
	})
	return bans
}

// SetFailure sets the error returned by later Apply calls.
func (m *Memory) SetFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failure = err
}

// Apply makes the simulated host match the requested state.
func (m *Memory) Apply(ctx context.Context, s protocol.DesiredState) error {
	m.cfg.Sleep(ctx, m.cfg.ApplyDelay)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.failure != nil {
		return m.failure
	}
	if s.DataVolume == "" && (len(s.Accounts) != 0 || len(s.Users) != 0) {
		return ErrNoDataVolume
	}
	for _, account := range s.Accounts {
		switch account.State {
		case protocol.AccountActive:
			m.accounts[account.Slug] = Account{Slug: account.Slug, GID: account.GID}
			m.folders[account.Slug] = struct{}{}
		case protocol.AccountRemoved:
			delete(m.accounts, account.Slug)
		}
	}
	named := make(map[string]struct{}, len(s.Users))
	for _, user := range s.Users {
		named[user.Name] = struct{}{}
		switch user.State {
		case protocol.UserActive, protocol.UserLocked:
			m.users[user.Name] = User{Name: user.Name, Account: user.Account, UID: user.UID, Locked: user.State == protocol.UserLocked, Keys: slices.Clone(user.Keys)}
		case protocol.UserDeleted:
			delete(m.users, user.Name)
		}
	}
	for name, user := range m.users {
		if _, ok := named[name]; !ok {
			user.Locked = true
			m.users[name] = user
		}
	}
	m.allowlist = slices.Clone(s.Allowlist)
	slices.Sort(m.allowlist)
	for _, address := range s.Unban {
		delete(m.bans, address)
	}
	for address := range m.bans {
		if m.allowed(address) {
			delete(m.bans, address)
		}
	}
	m.applies++
	return nil
}

// Inspect returns fixed simulated usage and the current set of account slices.
func (m *Memory) Inspect(_ context.Context, s protocol.DesiredState) (protocol.Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	disk := func(gb int) protocol.Disk {
		return protocol.Disk{UsedBytes: MemoryDiskUsed, SizeBytes: int64(gb) << 30, InodesUsed: MemoryInodesUsed, InodesTotal: MemoryInodesTotal}
	}
	report := protocol.Report{
		OS: MemoryOS, Kernel: MemoryKernel, Booted: m.booted,
		MemoryUsed: MemoryUsed, MemoryAvailable: MemoryAvailable, Root: disk(s.RootGB),
		Allowlist: slices.Clone(m.allowlist), Bans: m.activeBans(),
	}
	report.BanTotal = len(report.Bans)
	if s.DataVolume != "" {
		report.Data = disk(s.DataGB)
	}
	for slug := range m.accounts {
		report.Slices = append(report.Slices, protocol.Slice{Account: slug})
	}
	sort.Slice(report.Slices, func(i, j int) bool { return report.Slices[i].Account < report.Slices[j].Account })
	return report, nil
}

// InstallDaemon verifies the body and records its digest on success.
func (m *Memory) InstallDaemon(ctx context.Context, body io.Reader, digest string) error {
	if err := VerifyDaemon(ctx, nil, body, digest); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.daemon = digest
	return nil
}

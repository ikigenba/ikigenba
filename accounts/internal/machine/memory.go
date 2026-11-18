package machine

import (
	"context"
	"io"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
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
	Accounts []Account
	Users    []User
	Folders  []string
	Daemon   string
	Applies  int
}

// Memory is an in-memory machine for tests and sandbox hosts.
type Memory struct {
	mu       sync.Mutex
	cfg      MemoryConfig
	booted   time.Time
	accounts map[string]Account
	users    map[string]User
	folders  map[string]struct{}
	daemon   string
	applies  int
	failure  error
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
		folders: make(map[string]struct{}),
	}
}

// State returns independent copies of the simulated host's records.
func (m *Memory) State() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Snapshot{Daemon: m.daemon, Applies: m.applies}
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
	}
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

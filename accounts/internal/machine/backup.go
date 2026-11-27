package machine

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const backupFailed = "simulated backup failure"

type memoryBackup struct {
	facts         protocol.AccountBackup
	finalVersion  int64
	retrySnapshot bool
}

func (b *memoryBackup) clearError() {
	b.facts.Error = ""
	b.retrySnapshot = false
}

// backupSelection retains only what State needs to project the last pass's facts.
type backupSelection struct {
	accounts []string
	requests []string
	close    string
}

func selectBackups(s protocol.DesiredState) backupSelection {
	var selection backupSelection
	for _, a := range s.Accounts {
		if a.State == protocol.AccountActive {
			selection.accounts = append(selection.accounts, a.Slug)
		}
	}
	if b := s.Backup; b != nil {
		selection.close = b.Close
		if b.Restore != nil {
			for _, a := range b.Restore.Accounts {
				selection.accounts = append(selection.accounts, a.Slug)
			}
		}
		for _, q := range b.Restores {
			selection.requests = append(selection.requests, q.ID)
		}
	}
	slices.Sort(selection.accounts)
	slices.Sort(selection.requests)
	selection.accounts = slices.Compact(selection.accounts)
	selection.requests = slices.Compact(selection.requests)
	return selection
}

func (m *Memory) backupReport(selection backupSelection) ([]protocol.AccountBackup, []protocol.RestoreResult) {
	if m.disk != nil {
		m.disk.mu.Lock()
		defer m.disk.mu.Unlock()
	}
	var backups []protocol.AccountBackup
	for _, slug := range selection.accounts {
		entry := protocol.AccountBackup{Account: slug}
		if b := m.backups[slug]; b != nil {
			entry = b.facts
			entry.Snapshots = slices.Clone(entry.Snapshots)
		}
		if selection.close == "" || entry.Final.ID != selection.close {
			entry.Final = protocol.Step{}
		}
		if m.disk != nil {
			for _, db := range m.disk.databases[slug] {
				entry.Databases = append(entry.Databases, db.facts)
				if db.facts.Error != "" {
					entry.DatabaseErrors++
				}
				if t := db.facts.Synced; !t.IsZero() && (entry.Synced.IsZero() || t.Before(entry.Synced)) {
					entry.Synced = t
				}
				if t := db.facts.Written; t.After(entry.Written) {
					entry.Written = t
				}
			}
		}
		entry.DatabaseTotal = len(entry.Databases)
		slices.SortFunc(entry.Databases, func(a, b protocol.Database) int { return cmp.Compare(a.Path, b.Path) })
		backups = append(backups, entry)
	}
	var restores []protocol.RestoreResult
	for _, id := range selection.requests {
		if result := m.requests[id].result; result.State != "" {
			restores = append(restores, result)
		}
	}
	return backups, restores
}

func (m *Memory) accountBackup(slug string) *memoryBackup {
	b := m.backups[slug]
	if b == nil {
		b = &memoryBackup{facts: protocol.AccountBackup{Account: slug}}
		m.backups[slug] = b
	}
	return b
}

func backupReady(b *protocol.Backup) bool {
	return b != nil && b.Bucket != "" && b.Settings.EveryMinutes != 0
}

func credentialsFor(b *protocol.Backup, slug string) protocol.Credentials {
	for _, a := range b.Accounts {
		if a.Account == slug {
			return a.Credentials
		}
	}
	return protocol.Credentials{}
}

func (m *Memory) noCredentials(s protocol.DesiredState, withholdingSnapshot bool) {
	for _, a := range s.Accounts {
		if a.State == protocol.AccountActive {
			backup := m.accountBackup(a.Slug)
			backup.facts.Error = NoCredentials
			backup.retrySnapshot = backup.retrySnapshot || withholdingSnapshot
		}
	}
}

// put runs the cloud hook without holding the state lock. The pass's own mutex
// serializes writes of its kind without holding up reconciliation or reports.
func (m *Memory) put(c protocol.Credentials, key string) error {
	m.mu.Unlock()
	defer m.mu.Lock()
	return m.cfg.Put(c, key)
}

// Stream updates the simulated databases' replica health.
func (m *Memory) Stream(ctx context.Context, s protocol.DesiredState) error {
	m.streamMu.Lock()
	defer m.streamMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPass = selectBackups(s)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !backupReady(s.Backup) {
		m.noCredentials(s, false)
		return nil
	}
	disk := m.disk
	now := m.cfg.Now().UTC().Truncate(time.Second)
	failing := m.passFailures()
	for _, a := range s.Accounts {
		if a.State != protocol.AccountActive {
			continue
		}
		backup := m.accountBackup(a.Slug)
		c := credentialsFor(s.Backup, a.Slug)
		if c.AccessKeyID == "" {
			backup.facts.Error = NoCredentials
			continue
		}
		if backup.facts.Error == NoCredentials && !backup.retrySnapshot {
			backup.clearError()
		}
		if disk != nil {
			m.streamAccount(disk, a.Slug, c, now, failing[a.Slug])
		}
	}
	return nil
}

func (m *Memory) streamAccount(disk *memoryDisk, slug string, c protocol.Credentials, now time.Time, failing bool) {
	disk.mu.Lock()
	var paths []string
	for path := range disk.databases[slug] {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	defer disk.mu.Unlock()
	for _, path := range paths {
		db := disk.databases[slug][path]
		if !db.wal {
			db.facts.Error = NotWAL
			continue
		}
		if !db.started {
			disk.mu.Unlock()
			err := func() error {
				defer disk.mu.Lock()
				return m.put(c, protocol.AccountPrefix(slug)+path)
			}()
			if disk.databases[slug][path] != db {
				continue // The database was replaced while the put was in flight.
			}
			if !db.started {
				if err != nil {
					db.facts.Error = err.Error()
					continue
				}
				db.started = true
				db.facts.Oldest = now
				db.facts.Error = ""
			}
		}
		if !failing {
			db.facts.Synced, db.facts.Written, db.facts.Error = now, now, ""
		}
	}
}

func (m *Memory) passFailures() map[string]bool {
	failing := make(map[string]bool, len(m.failing))
	for slug := range m.failing {
		failing[slug] = true
	}
	return failing
}

// Backup simulates the scheduled snapshots and operation steps.
func (m *Memory) Backup(ctx context.Context, s protocol.DesiredState) error {
	m.backupMu.Lock()
	defer m.backupMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPass = selectBackups(s)
	if err := ctx.Err(); err != nil {
		return err
	}
	for id := range m.requests {
		if _, present := slices.BinarySearch(m.lastPass.requests, id); !present {
			delete(m.requests, id)
		}
	}
	if !backupReady(s.Backup) {
		m.noCredentials(s, true)
		return nil
	}
	now := m.cfg.Now()
	at := now.UTC().Truncate(time.Second)
	disk := m.disk
	failing := m.passFailures()
	if s.Backup.Restore != nil {
		m.restoreHost(disk, s.Backup, failing, now, at)
		return nil
	}
	m.takeSnapshots(s, failing, now, at)
	m.finishClose(s, failing, now, at)
	m.restoreCopies(disk, s, failing, now)
	return nil
}

func (m *Memory) takeSnapshots(s protocol.DesiredState, failing map[string]bool, now, at time.Time) {
	b := s.Backup
	interval := time.Duration(b.Settings.EveryMinutes) * time.Minute
	checks := time.Duration(b.Settings.CheckDays) * 24 * time.Hour
	for _, a := range s.Accounts {
		if a.State != protocol.AccountActive {
			continue
		}
		backup := m.accountBackup(a.Slug)
		facts := &backup.facts
		c := credentialsFor(b, a.Slug)
		switch {
		case c.AccessKeyID == "":
			facts.Error = NoCredentials
			backup.retrySnapshot = true
		case failing[a.Slug]:
			facts.Error = backupFailed
			backup.retrySnapshot = true
		default:
			if facts.Error != "" || due(facts.Snapshot, now, interval) {
				m.takeSnapshot(backup, c, at)
			}
			if due(facts.Checked, now, checks) {
				facts.Checked = at
			}
		}
	}
	if b.Host.AccessKeyID == "" {
		m.hostBackup.Error = NoCredentials
	} else if due(m.hostBackup.Snapshot, now, interval) {
		if err := m.put(b.Host, b.HostPrefix+"config"); err != nil {
			m.hostBackup.Error = err.Error()
		} else {
			m.hostBackup.Snapshot, m.hostBackup.Error = at, ""
		}
	}
}

func due(last, now time.Time, interval time.Duration) bool {
	return last.IsZero() || !now.Before(last.Add(interval))
}

func (m *Memory) takeSnapshot(backup *memoryBackup, c protocol.Credentials, at time.Time) bool {
	facts := &backup.facts
	if err := m.put(c, protocol.FilesPrefix(facts.Account)+"/config"); err != nil {
		facts.Error = err.Error()
		backup.retrySnapshot = true
		return false
	}
	facts.Snapshot = at
	backup.clearError()
	facts.Snapshots = slices.Insert(facts.Snapshots[:min(len(facts.Snapshots), protocol.MaxSnapshots-1)], 0, at.Unix())
	return true
}

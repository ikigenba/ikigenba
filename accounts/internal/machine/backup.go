package machine

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

const backupFailed = "simulated backup failure"

type memoryBackup struct {
	gid           int64
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
	accounts map[string]int64
	requests []string
	close    string
}

func selectBackups(s protocol.DesiredState) backupSelection {
	selection := backupSelection{accounts: make(map[string]int64)}
	if b := s.Backup; b != nil {
		selection.close = b.Close
		if b.Restore != nil {
			for _, a := range b.Restore.Accounts {
				selection.accounts[a.Slug] = a.GID
			}
		}
		for _, q := range b.Restores {
			selection.requests = append(selection.requests, q.ID)
		}
	}
	for _, a := range s.Accounts {
		if _, restoring := selection.accounts[a.Slug]; restoring || a.State == protocol.AccountActive {
			selection.accounts[a.Slug] = a.GID
		}
	}
	slices.Sort(selection.requests)
	selection.requests = slices.Compact(selection.requests)
	return selection
}

func (m *Memory) backupReport(selection backupSelection) ([]protocol.AccountBackup, []protocol.RestoreResult) {
	if m.disk != nil {
		m.disk.mu.Lock()
		defer m.disk.mu.Unlock()
	}
	var backups []protocol.AccountBackup
	for _, slug := range slices.Sorted(maps.Keys(selection.accounts)) {
		gid := selection.accounts[slug]
		entry := protocol.AccountBackup{Account: slug}
		if b := m.backups[slug]; b != nil && b.gid == gid {
			entry = b.facts
			entry.Snapshots = slices.Clone(entry.Snapshots)
		}
		if selection.close == "" || entry.Final.ID != selection.close {
			entry.Final = protocol.Step{}
		}
		if m.disk != nil {
			replicas := m.disk.replicas[slug]
			for path := range m.disk.databases[slug] {
				facts := protocol.Database{Path: path}
				if replicas != nil && replicas.gid == gid {
					if replica := replicas.databases[path]; replica != nil {
						facts = replica.facts
					}
				}
				entry.Databases = append(entry.Databases, facts)
				if facts.Error != "" {
					entry.DatabaseErrors++
				}
				if t := facts.Synced; !t.IsZero() && (entry.Synced.IsZero() || t.Before(entry.Synced)) {
					entry.Synced = t
				}
				if t := facts.Written; t.After(entry.Written) {
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

func (m *Memory) beginBackupPass(s protocol.DesiredState) map[string]*memoryBackup {
	m.lastPass = selectBackups(s)
	accounts := m.lastPass.accounts
	kept := make(map[string]*memoryBackup, len(accounts))
	for slug, gid := range accounts {
		b := m.backups[slug]
		if b == nil || b.gid != gid {
			b = &memoryBackup{gid: gid, facts: protocol.AccountBackup{Account: slug}}
		}
		kept[slug] = b
	}
	m.backups = kept
	if m.disk != nil {
		m.disk.pruneReplicas(accounts)
	}
	return kept
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

func noCredentials(s protocol.DesiredState, backups map[string]*memoryBackup, withholdingSnapshot bool) {
	for _, a := range s.Accounts {
		if a.State == protocol.AccountActive {
			backup := backups[a.Slug]
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
	backups := m.beginBackupPass(s)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !backupReady(s.Backup) {
		noCredentials(s, backups, false)
		return nil
	}
	disk := m.disk
	now := m.cfg.Now().UTC().Truncate(time.Second)
	failing := m.passFailures()
	for _, a := range s.Accounts {
		if a.State != protocol.AccountActive {
			continue
		}
		backup := backups[a.Slug]
		if m.backups[a.Slug] != backup {
			continue // A concurrent pass retired this account's facts.
		}
		c := credentialsFor(s.Backup, a.Slug)
		if c.AccessKeyID == "" {
			backup.facts.Error = NoCredentials
			continue
		}
		if backup.facts.Error == NoCredentials && !backup.retrySnapshot {
			backup.clearError()
		}
		if disk != nil {
			m.streamAccount(disk, a.Slug, backup.gid, c, now, failing[a.Slug])
		}
	}
	return nil
}

func (m *Memory) streamAccount(disk *memoryDisk, slug string, gid int64, c protocol.Credentials, now time.Time, failing bool) {
	disk.mu.Lock()
	defer disk.mu.Unlock()
	replicas := disk.replicas[slug]
	if replicas == nil || replicas.gid != gid {
		replicas = &memoryReplicas{gid: gid, databases: make(map[string]*memoryReplica)}
		disk.replicas[slug] = replicas
	}
	var paths []string
	for path := range disk.databases[slug] {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for _, path := range paths {
		db := disk.databases[slug][path]
		replica := replicas.databases[path]
		if replica == nil {
			replica = &memoryReplica{facts: protocol.Database{Path: path}}
			replicas.databases[path] = replica
		}
		if !db.wal {
			replica.facts.Error = NotWAL
			continue
		}
		if !replica.started {
			disk.mu.Unlock()
			err := func() error {
				defer disk.mu.Lock()
				return m.put(c, protocol.AccountPrefix(slug)+path)
			}()
			if disk.replicas[slug] != replicas {
				return // A concurrent pass retired this account's replica facts.
			}
			if disk.databases[slug][path] != db {
				continue // The database was replaced while the put was in flight.
			}
			if !replica.started {
				if err != nil {
					replica.facts.Error = err.Error()
					continue
				}
				replica.started = true
				replica.facts.Oldest = now
				replica.facts.Error = ""
			}
		}
		if !failing {
			replica.facts.Synced, replica.facts.Written, replica.facts.Error = now, now, ""
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
	backups := m.beginBackupPass(s)
	if err := ctx.Err(); err != nil {
		return err
	}
	for id := range m.requests {
		if _, present := slices.BinarySearch(m.lastPass.requests, id); !present {
			delete(m.requests, id)
		}
	}
	if !backupReady(s.Backup) {
		noCredentials(s, backups, true)
		return nil
	}
	now := m.cfg.Now()
	at := now.UTC().Truncate(time.Second)
	disk := m.disk
	failing := m.passFailures()
	if s.Backup.Restore != nil {
		m.restoreHost(disk, s.Backup, backups, failing, now, at)
		return nil
	}
	m.takeSnapshots(s, backups, failing, now, at)
	m.finishClose(s, backups, failing, now, at)
	m.restoreCopies(disk, s, backups, failing, now)
	return nil
}

func (m *Memory) takeSnapshots(s protocol.DesiredState, backups map[string]*memoryBackup, failing map[string]bool, now, at time.Time) {
	b := s.Backup
	interval := time.Duration(b.Settings.EveryMinutes) * time.Minute
	checks := time.Duration(b.Settings.CheckDays) * 24 * time.Hour
	for _, a := range s.Accounts {
		if a.State != protocol.AccountActive {
			continue
		}
		backup := backups[a.Slug]
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

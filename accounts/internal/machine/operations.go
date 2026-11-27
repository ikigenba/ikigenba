package machine

import (
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

type passDelay struct {
	id      string
	started bool
	first   time.Time
}

func (p *passDelay) ready(id string, now time.Time, delay time.Duration) bool {
	if p.id != id || !p.started {
		*p = passDelay{id: id, started: true, first: now}
	}
	return !now.Before(p.first.Add(delay))
}

func pending(id string) protocol.Step {
	return protocol.Step{ID: id, State: protocol.StepPending}
}

func done(id string, at time.Time) protocol.Step {
	return protocol.Step{ID: id, State: protocol.StepDone, At: at}
}

func failed(id string, at time.Time, code string) protocol.Step {
	return protocol.Step{ID: id, State: protocol.StepFailed, At: at, Code: code}
}

func shouldFinish(step protocol.Step, id string, failedVersion, version int64) bool {
	return step.ID != id || step.State == protocol.StepPending || (step.State == protocol.StepFailed && version > failedVersion)
}

func (m *Memory) finishClose(s protocol.DesiredState, failing map[string]bool, now, at time.Time) {
	b := s.Backup
	id := b.Close
	if id == "" {
		m.close = passDelay{}
		m.hostBackup.Final = protocol.Step{}
		for _, saved := range m.backups {
			saved.facts.Final = protocol.Step{}
		}
		return
	}
	locked := m.lockedClose == id
	version := m.lockedVersion
	ready := locked && m.close.ready(id, now, m.cfg.ApplyDelay)
	for _, a := range s.Accounts {
		if a.State != protocol.AccountActive {
			continue
		}
		saved := m.accountBackup(a.Slug)
		if shouldFinish(saved.facts.Final, id, saved.finalVersion, version) {
			m.finishAccount(saved, credentialsFor(b, a.Slug), id, version, ready, failing[a.Slug], at)
		}
	}
	if !shouldFinish(m.hostBackup.Final, id, m.hostFinalVersion, version) {
		return
	}
	switch {
	case !ready:
		m.hostBackup.Final = pending(id)
	case b.Host.AccessKeyID == "":
		m.hostBackup.Final = failed(id, at, protocol.CodeFailed)
		m.hostBackup.Error = NoCredentials
		m.hostFinalVersion = version
	default:
		m.hostBackup.Final = done(id, at)
	}
}

func (m *Memory) finishAccount(b *memoryBackup, c protocol.Credentials, id string, version int64, ready, failing bool, at time.Time) {
	if !ready {
		b.facts.Final = pending(id)
		return
	}
	switch {
	case c.AccessKeyID == "":
		b.facts.Error = NoCredentials
	case failing:
		b.facts.Error = backupFailed
	case m.takeSnapshot(&b.facts, c, at):
		b.facts.Final = done(id, at)
		return
	}
	if b.facts.Error == "" {
		b.facts.Error = backupFailed
	}
	b.facts.Final = failed(id, at, protocol.CodeFailed)
	b.finalVersion = version
}

func (m *Memory) restoreHost(disk *memoryDisk, b *protocol.Backup, failing map[string]bool, now, at time.Time) {
	q := b.Restore
	id := q.ID
	if disk == nil || disk.owner != m.identity {
		m.restore = passDelay{}
		m.hostBackup.Restored = pending(id)
		for _, a := range q.Accounts {
			m.accountBackup(a.Slug).facts.Restored = pending(id)
		}
		return
	}
	if disk.restoreID != id {
		m.restore = passDelay{}
		m.hostBackup.Restored = failed(id, at, protocol.CodeNotEmpty)
		return
	}
	if !m.restore.ready(id, now, m.cfg.ApplyDelay) {
		m.hostBackup.Restored = pending(id)
		for _, a := range q.Accounts {
			m.accountBackup(a.Slug).facts.Restored = pending(id)
		}
		return
	}
	if m.hostBackup.Restored.ID != id || m.hostBackup.Restored.State != protocol.StepDone {
		m.hostBackup.Restored = done(id, at)
	}
	disk.mu.Lock()
	defer disk.mu.Unlock()
	for _, a := range q.Accounts {
		facts := &m.accountBackup(a.Slug).facts
		if facts.Restored.ID == id && facts.Restored.State == protocol.StepDone {
			if _, restored := disk.restored[a.Slug]; restored {
				continue
			}
		}
		switch {
		case credentialsFor(b, a.Slug).AccessKeyID == "":
			facts.Restored = pending(id)
		case failing[a.Slug]:
			facts.Restored = failed(id, at, protocol.CodeFailed)
			facts.Error = backupFailed
		default:
			disk.folders[a.Slug] = struct{}{}
			disk.restored[a.Slug] = struct{}{}
			for _, path := range a.Databases {
				disk.addDatabase(a.Slug, path, true)
			}
			facts.Restored = done(id, at)
			facts.Error = ""
		}
	}
}

type memoryRequest struct {
	first  time.Time
	result protocol.RestoreResult
}

func (m *Memory) restoreCopies(disk *memoryDisk, s protocol.DesiredState, failing map[string]bool, now time.Time) {
	active := make(map[string]bool)
	for _, account := range s.Accounts {
		active[account.Slug] = account.State == protocol.AccountActive
	}
	for _, q := range s.Backup.Restores {
		r, seen := m.requests[q.ID]
		if !seen {
			r.first = now
			m.requests[q.ID] = r
		}
		if r.result.State != "" || now.Before(r.first.Add(m.cfg.ApplyDelay)) || !active[q.Account] || credentialsFor(s.Backup, q.Account).AccessKeyID == "" {
			continue
		}
		r.result = m.restoreCopy(disk, q, failing[q.Account])
		m.requests[q.ID] = r
	}
}

func (m *Memory) restoreCopy(disk *memoryDisk, q protocol.RestoreRequest, failing bool) protocol.RestoreResult {
	result := protocol.RestoreResult{ID: q.ID, State: protocol.StepFailed}
	if failing {
		result.Error = backupFailed
		return result
	}
	if disk == nil {
		result.Error = ErrNoDataVolume.Error()
		return result
	}
	disk.mu.Lock()
	defer disk.mu.Unlock()
	var databases map[string]*memoryDatabase
	scope := copyScope{account: q.Account}
	switch q.Kind {
	case protocol.RestoreDatabase:
		databases = disk.databases[q.Account]
		if databases[q.Path] == nil {
			result.Error = ErrNotSynced.Error()
			return result
		}
	case protocol.RestoreFiles:
		if !slices.Contains(m.accountBackup(q.Account).facts.Snapshots, q.AsOf.Unix()) {
			result.Error = ErrNoSnapshot.Error()
			return result
		}
		if q.User != "" {
			scope = copyScope{user: q.User}
		}
	default:
		result.Error = ErrInvalidState.Error()
		return result
	}
	copies := disk.copies[scope]
	if copies == nil {
		copies = &memoryCopies{paths: make(map[string]struct{}), fileDirectories: make(map[string]struct{})}
		disk.copies[scope] = copies
	}
	for attempt := 1; ; attempt++ {
		name := CopyName(q, attempt)
		if q.Kind == protocol.RestoreFiles {
			directory := strings.TrimSuffix(name, "/"+q.Path)
			if _, used := copies.fileDirectories[directory]; used {
				continue
			}
			copies.fileDirectories[directory] = struct{}{}
		} else if _, used := copies.paths[name]; used || databases[name] != nil {
			continue
		}
		copies.paths[name] = struct{}{}
		result.State, result.Copy = protocol.StepDone, name
		return result
	}
}

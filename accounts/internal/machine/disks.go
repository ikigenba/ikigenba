package machine

import (
	"cmp"
	"slices"
	"sync"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Disks holds the volumes shared by a sandbox's simulated machines.
type Disks struct {
	mu      sync.Mutex
	volumes map[string]*memoryDisk
}

// NewDisks makes an empty set of simulated volumes.
func NewDisks() *Disks {
	return &Disks{volumes: make(map[string]*memoryDisk)}
}

type memoryDisk struct {
	mu        sync.Mutex
	owner     *byte
	restoreID string
	folders   map[string]struct{}
	seeds     map[string]Seed
	databases map[string]map[string]*memoryDatabase
	restored  map[string]struct{}
	copies    map[copyScope]*memoryCopies
}

type memoryDatabase struct {
	wal     bool
	started bool
	facts   protocol.Database
}

type copyScope struct {
	account string
	user    string
}

type memoryCopies struct {
	paths           map[string]struct{}
	fileDirectories map[string]struct{}
}

func (d *Disks) disk(volume string, owner *byte, restoreID string) *memoryDisk {
	if volume == "" {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	disk := d.volumes[volume]
	if disk == nil {
		disk = &memoryDisk{
			owner: owner, restoreID: restoreID, folders: make(map[string]struct{}), seeds: make(map[string]Seed),
			databases: make(map[string]map[string]*memoryDatabase), restored: make(map[string]struct{}),
			copies: make(map[copyScope]*memoryCopies),
		}
		d.volumes[volume] = disk
	}
	return disk
}

func (d *memoryDisk) snapshot(out *Snapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for slug := range d.folders {
		out.Folders = append(out.Folders, slug)
	}
	for _, seed := range d.seeds {
		seed.Files = slices.Clone(seed.Files)
		out.Seeds = append(out.Seeds, seed)
	}
	for slug := range d.restored {
		out.Restored = append(out.Restored, slug)
	}
	slices.Sort(out.Folders)
	slices.SortFunc(out.Seeds, func(a, b Seed) int { return cmp.Compare(a.Account, b.Account) })
	slices.Sort(out.Restored)
}

func (d *memoryDisk) addDatabase(account, path string, wal bool) {
	if d.databases[account] == nil {
		d.databases[account] = make(map[string]*memoryDatabase)
	}
	d.databases[account][path] = &memoryDatabase{wal: wal, facts: protocol.Database{Path: path}}
}

// AddDatabase replaces the database at path on the current volume.
func (m *Memory) AddDatabase(account, path string, wal bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.disk == nil {
		return
	}
	m.disk.mu.Lock()
	defer m.disk.mu.Unlock()
	m.disk.addDatabase(account, path, wal)
}

// FailBackups controls failures on subsequent passes.
func (m *Memory) FailBackups(account string, fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if fail {
		m.failing[account] = true
	} else {
		delete(m.failing, account)
	}
}

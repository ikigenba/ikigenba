package linux

import (
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
)

type backupWalkLink struct{ fs.DirEntry }

func (backupWalkLink) Name() string      { return "link" }
func (backupWalkLink) IsDir() bool       { return false }
func (backupWalkLink) Type() fs.FileMode { return fs.ModeSymlink }

type backupWalkEntries struct {
	remaining int
	batch     [ReadBatch]fs.DirEntry
	requested int
	maxBatch  int
	read      int
}

func (*backupWalkEntries) Fd() uintptr { return 0 }

func (d *backupWalkEntries) ReadDir(count int) ([]fs.DirEntry, error) {
	d.requested = count
	d.maxBatch = max(d.maxBatch, count)
	actual := min(count, d.remaining)
	d.remaining -= actual
	d.read += actual
	if actual == 0 {
		return nil, io.EOF
	}
	return d.batch[:actual], nil
}

func TestBackupWalkReadsOnlyOneEntryOverVisitLimit(t *testing.T) {
	// R-80UO-4U1G
	for _, count := range []int{MaxVisit, MaxVisit + ReadBatch} {
		dir := backupWalkEntries{remaining: count}
		for i := range dir.batch {
			dir.batch[i] = backupWalkLink{}
		}
		var walker backupWalker
		err := walker.directory(&dir, "data", 0)
		if dir.maxBatch > ReadBatch {
			t.Fatalf("largest directory batch = %d, limit %d", dir.maxBatch, ReadBatch)
		}
		if count == MaxVisit {
			if err != nil || dir.requested != 1 || dir.read != MaxVisit {
				t.Fatalf("walk at limit read %d entries and last requested %d, error %v", dir.read, dir.requested, err)
			}
		} else if !errors.Is(err, machine.ErrTooLarge) || dir.read != MaxVisit+1 || dir.requested != MaxVisit%ReadBatch+1 {
			t.Fatalf("walk over limit read %d entries and last requested %d, error %v", dir.read, dir.requested, err)
		}
	}
}

// Package proc reads process facts from an injected filesystem.
package proc

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const ticksPerSecond = 100

// FileID identifies a file by its device and inode as represented in /proc/locks.
type FileID struct {
	Major, Minor uint32
	Inode        uint64
}

// Start returns the process start time using the kernel boot time and start ticks.
func Start(root fs.FS, pid int) (time.Time, error) {
	ticks, err := StartTicks(root, pid)
	if err != nil {
		return time.Time{}, err
	}
	bootData, err := fs.ReadFile(root, "proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	var boot uint64
	found := false
	for _, line := range strings.Split(string(bootData), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "btime" {
			value, ok := decimal(fields[1])
			if ok && value <= uint64(1<<63-1) {
				boot = value
				found = true
				break
			}
		}
	}
	if !found {
		return time.Time{}, errors.New("invalid proc/stat boot time")
	}
	seconds := ticks / ticksPerSecond
	if seconds > uint64(1<<63-1)-boot {
		return time.Time{}, errors.New("process start time overflows int64")
	}
	startSeconds, _ := strconv.ParseInt(strconv.FormatUint(boot+seconds, 10), 10, 64)
	return time.Unix(startSeconds, int64(ticks%ticksPerSecond)*int64(time.Second/ticksPerSecond)), nil
}

// StartTicks returns field 22 of the process stat record.
func StartTicks(root fs.FS, pid int) (uint64, error) {
	if pid <= 0 {
		return 0, errors.New("invalid pid")
	}
	data, err := fs.ReadFile(root, "proc/"+strconv.Itoa(pid)+"/stat")
	if err != nil {
		return 0, err
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 {
		return 0, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	if len(fields) < 20 {
		return 0, errors.New("short process stat")
	}
	ticks, ok := decimal(fields[19])
	if !ok {
		return 0, errors.New("invalid process start ticks")
	}
	return ticks, nil
}

// Cwd reads the process working-directory link.
func Cwd(root fs.FS, pid int) (string, error) {
	return fs.ReadLink(root, "proc/"+strconv.Itoa(pid)+"/cwd")
}

// FileIDOf extracts Linux device and inode numbers from file metadata.
func FileIDOf(fi fs.FileInfo) (FileID, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return FileID{}, false
	}
	d := st.Dev
	major := ((d & 0x00000000000fff00) >> 8) | ((d & 0xfffff00000000000) >> 32)
	minor := (d & 0x00000000000000ff) | ((d & 0x00000ffffff00000) >> 12)
	if major > 1<<32-1 || minor > 1<<32-1 {
		return FileID{}, false
	}
	return FileID{Major: uint32(major), Minor: uint32(minor), Inode: st.Ino}, true
}

// LockHolders maps files with held write locks to their first listed owner.
func LockHolders(root fs.FS) (map[FileID]int, error) {
	data, err := fs.ReadFile(root, "proc/locks")
	if err != nil {
		if pathErr, ok := directPathError(err); ok {
			return nil, pathErr.Err
		}
		return nil, err
	}
	holders := make(map[FileID]int)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || !strings.HasSuffix(fields[0], ":") || fields[1] == "->" || fields[3] != "WRITE" {
			continue
		}
		owner, ok := decimal(fields[4])
		if !ok || owner == 0 || owner > uint64(int(^uint(0)>>1)) {
			continue
		}
		parts := strings.Split(fields[5], ":")
		if len(parts) != 3 {
			continue
		}
		major, majorOK := hexadecimal(parts[0])
		minor, minorOK := hexadecimal(parts[1])
		inode, inodeOK := decimal(parts[2])
		if !majorOK || !minorOK || !inodeOK || major > 1<<32-1 || minor > 1<<32-1 {
			continue
		}
		id := FileID{Major: uint32(major), Minor: uint32(minor), Inode: inode}
		if _, exists := holders[id]; !exists {
			holders[id] = int(owner)
		}
	}
	return holders, nil
}

func decimal(s string) (uint64, bool) {
	value, err := strconv.ParseUint(s, 10, 64)
	return value, err == nil
}

func hexadecimal(s string) (uint64, bool) {
	value, err := strconv.ParseUint(s, 16, 64)
	return value, err == nil
}

func directPathError(value any) (*fs.PathError, bool) {
	pathErr, ok := value.(*fs.PathError)
	return pathErr, ok
}

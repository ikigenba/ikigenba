package sim

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// MaxBanFileBytes caps how many bytes a look reads from the bans file.
const MaxBanFileBytes = 64 << 10

// MaxBackupFileBytes caps how many bytes a look reads from the backups file.
const MaxBackupFileBytes = 64 << 10

func (s *Sim) consumeDrops(running map[string]string, loops map[string]loop) {
	if s.cfg.BanFile == "" && s.cfg.BackupFile == "" {
		return
	}
	machines := make(map[string]*machine.Memory, len(running))
	for id, host := range running {
		if l, ok := loops[id]; ok && host != "" {
			select {
			case <-l.done:
			default:
				machines[host] = l.machine
			}
		}
	}
	consumeDrop(s.cfg.BanFile, MaxBanFileBytes, func(line []byte) { applyBanLine(line, machines) })
	consumeDrop(s.cfg.BackupFile, MaxBackupFileBytes, func(line []byte) { applyBackupLine(line, machines) })
}

func consumeDrop(path string, capBytes int64, apply func([]byte)) {
	if path == "" {
		return
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	// Nonblocking open also covers a replacement with a FIFO between the two checks.
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return
	}
	readDrop(f, capBytes, apply)
	// Leave a replacement at the drop path for the next look.
	current, err := os.Lstat(path)
	if err == nil && os.SameFile(info, current) {
		_ = os.Remove(path)
	}
}

func readDrop(f *os.File, capBytes int64, apply func([]byte)) {
	limit := &io.LimitedReader{R: f, N: capBytes}
	r := bufio.NewReader(limit)
	var line []byte
	for {
		part, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			line = append(line, part...)
			continue
		}
		if err == nil {
			apply(joinDropLine(line, part[:len(part)-1]))
			line = nil
			continue
		}
		if err == io.EOF && len(line)+len(part) > 0 {
			complete := limit.N > 0
			if !complete {
				// Distinguish EOF at the cap from a cut line without reading past it.
				info, statErr := f.Stat()
				complete = statErr == nil && info.Size() <= capBytes
			}
			if complete {
				apply(joinDropLine(line, part))
			}
		}
		return
	}
}

func joinDropLine(line, part []byte) []byte {
	if line == nil {
		return part
	}
	return append(line, part...)
}

func applyBackupLine(line []byte, machines map[string]*machine.Memory) {
	var fields [4][]byte
	n := 0
	for field := range bytes.FieldsFuncSeq(line, func(c rune) bool { return c == ' ' || c == '\t' }) {
		if n == len(fields) {
			return
		}
		fields[n] = field
		n++
	}
	if n < 3 {
		return
	}
	m, slug := machines[string(fields[0])], string(fields[1])
	if m == nil || valid.Slug(slug) != nil {
		return
	}
	switch command := string(fields[2]); command {
	case "database", "plain":
		if path := string(fields[3]); n == 4 && valid.DatabasePath(path) == nil {
			m.AddDatabase(slug, path, command == "database")
		}
	case "fail", "heal":
		if n == 3 {
			m.FailBackups(slug, command == "fail")
		}
	}
}

func applyBanLine(line []byte, machines map[string]*machine.Memory) {
	line = bytes.Trim(line, " \t")
	split := bytes.IndexAny(line, " \t")
	if split < 0 {
		return
	}
	address := bytes.TrimLeft(line[split+1:], " \t")
	if m := machines[string(line[:split])]; m != nil && !bytes.ContainsAny(address, " \t") {
		if canonical, err := valid.Address(string(address)); err == nil {
			m.Ban(canonical)
		}
	}
}

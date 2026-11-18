package sim

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// MaxBanFileBytes caps how many bytes a look reads from the bans file.
const MaxBanFileBytes = 64 << 10

func (s *Sim) consumeBans(running map[string]string, loops map[string]loop) {
	path := s.cfg.BanFile
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
	readBans(f, machines)
	// Leave a replacement at the drop path for the next look.
	current, err := os.Lstat(path)
	if err == nil && os.SameFile(info, current) {
		_ = os.Remove(path)
	}
}

func readBans(f *os.File, machines map[string]*machine.Memory) {
	limit := &io.LimitedReader{R: f, N: MaxBanFileBytes}
	r := bufio.NewReaderSize(limit, MaxBanFileBytes)
	for {
		line, err := r.ReadSlice('\n')
		if err == nil {
			applyBanLine(line[:len(line)-1], machines)
			continue
		}
		if err == io.EOF && limit.N > 0 {
			applyBanLine(line, machines)
		} else if len(line) > 0 && limit.N == 0 {
			// A full buffer can end at EOF or cut a line. Check without reading past the cap.
			if info, err := f.Stat(); err == nil && info.Size() <= MaxBanFileBytes {
				applyBanLine(line, machines)
			}
		}
		return
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

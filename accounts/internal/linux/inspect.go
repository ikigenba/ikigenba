package linux

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

type cpuTimes struct{ total, idle uint64 }

func (h *Host) boundedText(hostPath string) string {
	f, err := os.Open(h.path(hostPath))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	text, err := io.ReadAll(io.LimitReader(f, MaxCapture))
	if err != nil {
		return ""
	}
	return string(text)
}

func parseFloat(s string) float64 {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0
	}
	return n
}

func firstField(s string) string {
	for field := range strings.FieldsSeq(s) {
		return field
	}
	return ""
}

func (h *Host) readCPU() cpuTimes {
	for line := range strings.SplitSeq(h.boundedText("/proc/stat"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var times cpuTimes
		for i, value := range fields[1:] {
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return cpuTimes{}
			}
			times.total += n
			if i == 3 || i == 4 {
				times.idle += n
			}
		}
		return times
	}
	return cpuTimes{}
}

func (h *Host) systemFacts(out *protocol.Report, now time.Time) {
	for line := range strings.SplitSeq(h.boundedText("/etc/os-release"), "\n") {
		if name, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			if unquoted, err := strconv.Unquote(name); err == nil {
				name = unquoted
			}
			out.OS = strings.Trim(name, "\"'")
			break
		}
	}
	out.Kernel = strings.TrimSpace(h.boundedText("/proc/sys/kernel/osrelease"))
	if uptime := firstField(h.boundedText("/proc/uptime")); uptime != "" {
		if seconds, err := strconv.ParseFloat(uptime, 64); err == nil && seconds >= 0 && seconds < float64(math.MaxInt64)/float64(time.Second) {
			out.Booted = now.Add(-time.Duration(seconds * float64(time.Second))).UTC().Truncate(time.Second)
		}
	}
	i := 0
	for value := range strings.FieldsSeq(h.boundedText("/proc/loadavg")) {
		out.Load[i] = parseFloat(value)
		i++
		if i == len(out.Load) {
			break
		}
	}
	var memTotal, swapFree int64
	var availableRead, swapFreeRead bool
	for line := range strings.SplitSeq(h.boundedText("/proc/meminfo"), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		kb, err := strconv.ParseInt(firstField(value), 10, 64)
		if err != nil || kb < 0 || kb > math.MaxInt64/1024 {
			continue
		}
		switch name {
		case "MemTotal":
			memTotal = kb * 1024
		case "MemAvailable":
			out.MemoryAvailable = kb * 1024
			availableRead = true
		case "SwapTotal":
			out.SwapTotal = kb * 1024
		case "SwapFree":
			swapFree = kb * 1024
			swapFreeRead = true
		}
	}
	if availableRead && memTotal > out.MemoryAvailable {
		out.MemoryUsed = memTotal - out.MemoryAvailable
	}
	if swapFreeRead && out.SwapTotal > swapFree {
		out.SwapUsed = out.SwapTotal - swapFree
	}
	cpu := h.readCPU()
	if h.previousCPU.total > 0 && cpu.total > h.previousCPU.total && cpu.idle >= h.previousCPU.idle {
		out.CPUPercent = 100 * (1 - float64(cpu.idle-h.previousCPU.idle)/float64(cpu.total-h.previousCPU.total))
	}
	h.previousCPU = cpu
}

func filesystem(path string) (protocol.Disk, uint64, bool) {
	var info syscall.Stat_t
	var stat syscall.Statfs_t
	if syscall.Stat(path, &info) != nil || syscall.Statfs(path, &stat) != nil {
		return protocol.Disk{}, 0, false
	}
	if stat.Blocks > math.MaxInt64 || stat.Bfree > math.MaxInt64 || stat.Bfree > stat.Blocks ||
		stat.Files > math.MaxInt64 || stat.Ffree > math.MaxInt64 || stat.Ffree > stat.Files {
		return protocol.Disk{}, 0, false
	}
	blocks, free, blockSize := int64(stat.Blocks), int64(stat.Bfree), stat.Bsize
	files, freeFiles := int64(stat.Files), int64(stat.Ffree)
	return protocol.Disk{
		UsedBytes: (blocks - free) * blockSize, SizeBytes: blocks * blockSize,
		InodesUsed: files - freeFiles, InodesTotal: files,
	}, info.Dev, true
}

func scopeCount(ctx context.Context, path string) int {
	if ctx.Err() != nil {
		return 0
	}
	dir, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0
	}
	defer func() { _ = dir.Close() }()
	count := 0
	for {
		entries, err := dir.ReadDir(32)
		for _, entry := range entries {
			if entry.IsDir() {
				if strings.HasSuffix(entry.Name(), ".scope") {
					count++
				}
				count += scopeCount(ctx, filepath.Join(path, entry.Name()))
			}
		}
		if err != nil || ctx.Err() != nil {
			return count
		}
	}
}

const cgroupPool = "/sys/fs/cgroup/" + PoolSlice

func (h *Host) sliceFacts(s protocol.DesiredState, now time.Time) (out []protocol.Slice) {
	current := make(map[string]int64)
	elapsed := now.Sub(h.previousInspect).Microseconds()
	for _, a := range s.Accounts {
		if a.State != protocol.AccountActive {
			continue
		}
		info := protocol.Slice{Account: a.Slug}
		if valid.Slug(a.Slug) != nil {
			out = append(out, info)
			continue
		}
		path := filepath.Join(cgroupPool, AccountSlice(a.Slug))
		if dir, err := os.Stat(h.path(path)); err == nil && dir.IsDir() {
			var usage int64
			var usageRead bool
			for line := range strings.SplitSeq(h.boundedText(filepath.Join(path, "cpu.stat")), "\n") {
				if value, ok := strings.CutPrefix(line, "usage_usec "); ok {
					if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
						usage, usageRead = parsed, true
						current[a.Slug] = usage
					}
					break
				}
			}
			if h.inspected {
				info.MemoryBytes, _ = strconv.ParseInt(strings.TrimSpace(h.boundedText(filepath.Join(path, "memory.current"))), 10, 64)
				if previous, ok := h.previousSlices[a.Slug]; ok && usageRead && usage >= previous && elapsed > 0 {
					info.CPUPercent = float64(usage-previous) / float64(elapsed) * 100
				}
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	h.previousSlices = current
	return out
}

type nonemptyLines struct {
	count   int
	content bool
}

func (w *nonemptyLines) Write(p []byte) (int, error) {
	for _, b := range p {
		switch b {
		case '\n':
			if w.content {
				w.count++
			}
			w.content = false
		default:
			w.content = true
		}
	}
	return len(p), nil
}

func (h *Host) unitFacts(ctx context.Context, now time.Time, out *protocol.Report) {
	failed, err := h.run(ctx, "systemctl", "list-units", "--state=failed", "--plain", "--no-legend", "--no-pager")
	if err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(failed.Stdout))
		for i := 0; i < protocol.MaxFailedUnits && scanner.Scan(); i++ {
			if unit := firstField(scanner.Text()); unit != "" {
				out.FailedUnits = append(out.FailedUnits, unit)
			}
		}
		sort.Strings(out.FailedUnits)
	}
	if !h.updatesRead || now.Sub(h.updateTime) >= UpdatesInterval {
		var lines nonemptyLines
		cmd := Command{Name: "dnf", Args: []string{"-q", "updateinfo", "list", "--security", "--available"}, Stdout: &lines}
		h.pendingUpdates = 0
		if _, err := h.query(ctx, cmd, 0); err == nil {
			h.pendingUpdates = lines.count
			if lines.content {
				h.pendingUpdates++
			}
		}
		result, err := h.query(ctx, Command{Name: "dnf", Args: []string{"needs-restarting", "-r"}}, 0, 1)
		h.reboot = err == nil && result.ExitCode == 1
		h.updateTime, h.updatesRead = now, true
	}
	out.Updates, out.Reboot = h.pendingUpdates, h.reboot
}

func (h *Host) journalFacts(ctx context.Context, s protocol.DesiredState, out *protocol.Report) {
	named := make(map[string]time.Time, len(s.Users))
	for _, user := range s.Users {
		named[user.Name] = h.signIns[user.Name]
	}
	cursor := h.cursor
	writer := lineWriter{limit: MaxJournalLine, emit: func(line []byte) {
		var entry struct {
			Cursor  string `json:"__CURSOR"`
			Message string `json:"MESSAGE"`
			Stamp   string `json:"__REALTIME_TIMESTAMP"`
		}
		if json.Unmarshal(line, &entry) != nil {
			return
		}
		if entry.Cursor != "" {
			cursor = entry.Cursor
		}
		message, ok := strings.CutPrefix(entry.Message, "Accepted publickey for ")
		user, _, hasFrom := strings.Cut(message, " from ")
		last, listed := named[user]
		if !ok || !hasFrom || !listed {
			return
		}
		if micros, err := strconv.ParseInt(entry.Stamp, 10, 64); err == nil {
			if at := time.UnixMicro(micros).UTC().Truncate(time.Second); at.After(last) {
				named[user] = at
			}
		}
	}}
	cmd := Command{Name: "journalctl", Args: []string{"--unit=sshd.service", "--output=json", "--no-pager", "--quiet", "--grep=^Accepted publickey for "}, Stdout: &writer}
	if h.cursor != "" {
		cmd.Args = append(cmd.Args, "--after-cursor="+h.cursor)
	}
	_, err := h.query(ctx, cmd, 0, 1)
	if err == nil {
		writer.finish()
		h.cursor = cursor
	} else {
		for user := range named {
			named[user] = h.signIns[user]
		}
	}
	h.signIns = named
	for user, at := range named {
		if !at.IsZero() {
			out.SignIns = append(out.SignIns, protocol.SignIn{User: user, At: at})
		}
	}
	sort.Slice(out.SignIns, func(i, j int) bool { return out.SignIns[i].User < out.SignIns[j].User })
}

// Inspect reads host facts without letting one unavailable source hide the others.
func (h *Host) Inspect(ctx context.Context, s protocol.DesiredState) (protocol.Report, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Report{}, err
	}
	now := h.env.Now()
	var out protocol.Report
	h.systemFacts(&out, now)
	root, device, ok := filesystem(h.path("/"))
	out.Root = root
	if data, dataDevice, dataOK := filesystem(h.path(DataDir)); ok && dataOK && dataDevice != device {
		out.Data = data
	}
	out.Sessions = scopeCount(ctx, h.path(cgroupPool))
	out.Slices = h.sliceFacts(s, now)
	h.unitFacts(ctx, now, &out)
	h.journalFacts(ctx, s, &out)
	out.Bans, out.BanTotal = h.banFacts(ctx, now.Location())
	out.Allowlist = h.appliedAllowlist()
	h.previousInspect, h.inspected = now, true
	return out, ctx.Err()
}

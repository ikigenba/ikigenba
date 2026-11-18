package linux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// Session helper paths and scope placement timing.
const (
	SessionHelper = "/usr/local/libexec/accountsd-session"
	SessionName   = "accountsd-session"
	SessionPoll   = 10 * time.Millisecond
	SessionWait   = 5 * time.Second

	pamPath        = "/etc/pam.d/sshd"
	pamMemberTest  = "session [success=ignore default=1] pam_succeed_if.so quiet user ingroup shell"
	pamHelperLine  = "session required pam_exec.so type=open_session " + SessionHelper
	pamSkipSystemd = "session [success=1 default=ignore] pam_succeed_if.so quiet user ingroup shell"
	pamHook        = pamMemberTest + "\n" + pamHelperLine + "\n"
)

func (h *Host) unitFiles(visit func(string) error) error {
	dir, err := os.Open(h.path(unitDirectory))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return hostError(unitDirectory, err)
	}
	defer func() { _ = dir.Close() }()
	for {
		entries, err := dir.ReadDir(32)
		for _, entry := range entries {
			if !entry.IsDir() {
				if err := visit(entry.Name()); err != nil {
					return err
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return hostError(unitDirectory, err)
		}
	}
}

func isPersonSlice(name, user string) bool {
	tail := "-" + escapeSlice(user) + ".slice"
	head, ok := strings.CutSuffix(name, tail)
	account, prefixed := strings.CutPrefix(head, "acct-")
	return ok && prefixed && account != "" && !strings.Contains(account, "-")
}

// Session places the sshd process opening a customer session in its own scope.
func Session(ctx context.Context, env Env, getenv func(string) string, parent int) error {
	if getenv("PAM_TYPE") != "open_session" {
		return nil
	}
	user := getenv("PAM_USER")
	if user == "" {
		return nil
	}
	h := New(env)
	var member bool
	if err := h.scan("/etc/group", func(line string) {
		fields := strings.SplitN(line, ":", 4)
		if len(fields) == 4 && fields[0] == ShellGroup {
			member = slices.Contains(strings.Split(fields[3], ","), user)
		}
	}); err != nil || !member {
		return nil
	}
	var target string
	if err := h.unitFiles(func(name string) error {
		if isPersonSlice(name, user) && (target == "" || name < target) {
			target = name
		}
		return nil
	}); err != nil || target == "" {
		return nil
	}
	pid := strconv.Itoa(parent)
	scope := SessionName + "-" + pid + ".scope"
	if _, err := h.run(ctx, "busctl", "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "StartTransientUnit", "ssa(sv)a(sa(sv))",
		scope, "fail", "2", "PIDs", "au", "1", pid, "Slice", "s", target, "0"); err != nil {
		return err
	}
	return h.waitForScope("/proc/"+pid+"/cgroup", "/"+target+"/"+scope)
}

const sessionPlacementTimeout = "session scope did not appear"

func (h *Host) waitForScope(path, suffix string) error {
	start := h.env.Now()
	for {
		placed := false
		err := h.scan(path, func(line string) {
			if cgroup, ok := strings.CutPrefix(line, "0::"); ok && strings.HasSuffix(cgroup, suffix) {
				placed = true
			}
		})
		if err == nil && placed {
			return nil
		}
		if h.env.Now().Sub(start) >= SessionWait {
			break
		}
		h.env.Sleep(SessionPoll)
		if h.env.Now().Sub(start) >= SessionWait {
			break
		}
	}
	return hostError(path, errors.New(sessionPlacementTimeout))
}

const (
	poolSliceContents    = "[Slice]\nCPUWeight=50\n"
	accountSliceContents = "[Slice]\nCPUWeight=100\nMemoryHigh=1.5G\nMemoryMax=2G\nTasksMax=512\n"
	personSliceContents  = "[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n"
	sharedPAMPath        = "/etc/pam.d/password-auth"
)

func (h *Host) slices(ctx context.Context, s protocol.DesiredState) error {
	change, err := h.write(unitDirectory+PoolSlice, []byte(poolSliceContents), 0o644)
	if err != nil {
		return err
	}
	reload := change.changed
	for _, a := range s.Accounts {
		path := unitDirectory + AccountSlice(a.Slug)
		switch a.State {
		case protocol.AccountActive:
			change, err = h.write(path, []byte(accountSliceContents), 0o644)
			reload = reload || change.changed
		case protocol.AccountRemoved:
			var removed bool
			removed, err = h.remove(path)
			reload = reload || removed
		}
		if err != nil {
			return err
		}
	}
	for _, u := range s.Users {
		// Deleted users need not reference a current desired account.
		if valid.Slug(u.Account) != nil {
			continue
		}
		path := unitDirectory + PersonSlice(u.Account, u.Name)
		switch u.State {
		case protocol.UserActive, protocol.UserLocked:
			change, err = h.write(path, []byte(personSliceContents), 0o644)
			reload = reload || change.changed
		case protocol.UserDeleted:
			var removed bool
			removed, err = h.remove(path)
			reload = reload || removed
		}
		if err != nil {
			return err
		}
	}
	if reload {
		if _, err := h.run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	return h.installSession()
}

func (h *Host) installSession() error {
	if err := h.installPAM(pamPath); err != nil {
		return err
	}
	if err := h.installPAM(sharedPAMPath); err != nil {
		return err
	}

	path := h.path(SessionHelper)
	if target, err := os.Readlink(path); err == nil && target == DaemonPath {
		return nil
	}
	if err := h.mkdir(filepath.Dir(SessionHelper)); err != nil {
		return hostError(SessionHelper, err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".accountsd-session-")
	if err != nil {
		return hostError(SessionHelper, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := f.Close(); err != nil {
		return hostError(SessionHelper, err)
	}
	if err := os.Remove(tmp); err != nil {
		return hostError(SessionHelper, err)
	}
	if err := os.Symlink(DaemonPath, tmp); err != nil {
		return hostError(SessionHelper, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return hostError(SessionHelper, err)
	}
	return nil
}

func (h *Host) installPAM(path string) error {
	f, err := os.Open(h.path(path))
	if errors.Is(err, fs.ErrNotExist) {
		if path != pamPath {
			return nil
		}
		_, err := h.write(path, []byte(pamHook), 0o644)
		return err
	}
	if err != nil {
		return hostError(path, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return hostError(path, err)
	}
	size := info.Size()
	insertion := int64(-1)
	if path == pamPath {
		insertion, err = pamInsertion(f, size)
		if err != nil {
			return hostError(path, err)
		}
	}
	return h.writeStream(path, 0o644, func(dst io.Writer) (bool, error) {
		return rewritePAM(f, size, dst, insertion)
	})
}

type pamLineKind uint8

const (
	pamOtherLine pamLineKind = iota
	pamSkipLine
	pamMemberLine
	pamExecLine
	pamSystemdLine
)

// Linux-PAM replaces a continuation with a space and skips intervening
// blank or comment-only lines. The matcher leaves the original bytes on disk.
const pamContinuation = `\\[ \t]*\n(?:[ \t]*(?:#[^\n]*)?\n)*`
const pamSpace = `(?:[ \t\r]|` + pamContinuation + `)`

// Match the module field after a module control. The include and substack
// controls name another PAM file.
var pamSystemdModule = regexp.MustCompile(
	`^` + pamSpace + `*(?i:-?session)` + pamSpace + `+` +
		`(?:(?i:required|requisite|sufficient|optional)` + pamSpace + `+|\[(?:` +
		pamContinuation + `|\\\]|[^#\\\]\n])*\]` + pamSpace + `*)` +
		`(?:[^[:space:]#]*/)?pam_systemd\.so(?:` + pamSpace + `|[#\n]|$)`)

type pamLine struct {
	size       int64
	terminated bool
	kind       pamLineKind
}

// Long lines are matched directly on a section of the input file. Keeping only
// the offset and a fixed reader buffer lets us preserve the original bytes.
func readPAMLine(r *bufio.Reader, input io.ReaderAt, start int64) (pamLine, error) {
	first, err := readPAMPhysicalLine(r)
	if err != nil {
		return pamLine{}, err
	}
	line := pamLine{size: first.size, terminated: first.terminated}
	for continued := first.continued; continued; {
		next, err := readPAMPhysicalLine(r)
		if errors.Is(err, io.EOF) {
			line.terminated = false
			break
		}
		if err != nil {
			return pamLine{}, err
		}
		line.size += next.size
		line.terminated = next.terminated
		continued = next.continued || next.ignored
	}
	if first.continued || first.size > int64(len(first.fragment)) {
		section := bufio.NewReader(io.NewSectionReader(input, start, line.size))
		if pamSystemdModule.MatchReader(section) {
			line.kind = pamSystemdLine
		}
		return line, nil
	}
	switch string(bytes.TrimSuffix(first.fragment, []byte{'\n'})) {
	case pamSkipSystemd:
		line.kind = pamSkipLine
	case pamMemberTest:
		line.kind = pamMemberLine
	case pamHelperLine:
		line.kind = pamExecLine
	default:
		if pamSystemdModule.Match(first.fragment) {
			line.kind = pamSystemdLine
		}
	}
	return line, nil
}

type pamPhysicalLine struct {
	fragment   []byte
	size       int64
	terminated bool
	continued  bool
	ignored    bool
}

func readPAMPhysicalLine(r *bufio.Reader) (pamPhysicalLine, error) {
	var line pamPhysicalLine
	var first, last byte
	var found, comment bool
	for {
		fragment, err := r.ReadSlice('\n')
		line.size += int64(len(fragment))
		if content := bytes.Trim(fragment, " \t\n"); len(content) > 0 {
			if !found {
				first, found = content[0], true
			}
			last = content[len(content)-1]
			comment = comment || bytes.IndexByte(content, '#') >= 0
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && (!errors.Is(err, io.EOF) || line.size == 0) {
			return pamPhysicalLine{}, err
		}
		line.fragment = fragment
		line.terminated = err == nil
		line.continued = !comment && last == '\\'
		line.ignored = !found || first == '#'
		return line, nil
	}
}

// Insert at the end when possible. Keep an unterminated last line byte-identical
// and keep an existing skip immediately before the module it skips.
func pamInsertion(input io.ReaderAt, size int64) (int64, error) {
	r := bufio.NewReader(io.NewSectionReader(input, 0, size))
	var previous, start int64
	preceding := pamOtherLine
	for start < size {
		line, err := readPAMLine(r, input, start)
		if err != nil {
			return 0, err
		}
		if preceding == pamMemberLine && line.kind == pamExecLine {
			return -1, nil
		}
		if !line.terminated {
			if preceding == pamSkipLine {
				return previous, nil
			}
			return start, nil
		}
		previous, start = start, start+line.size
		preceding = line.kind
	}
	return size, nil
}

func rewritePAM(input io.ReaderAt, size int64, dst io.Writer, insertion int64) (bool, error) {
	r := bufio.NewReader(io.NewSectionReader(input, 0, size))
	var start, pending int64
	var changed, precedingSkip bool
	for start < size {
		line, err := readPAMLine(r, input, start)
		if err != nil {
			return false, err
		}
		var insert string
		if start == insertion {
			insert = pamHook
		}
		if !precedingSkip && line.kind == pamSystemdLine {
			insert += pamSkipSystemd + "\n"
		}
		if insert != "" {
			if err := editConfig(input, dst, pending, start, insert); err != nil {
				return false, err
			}
			pending, changed = start, true
		}
		precedingSkip = line.kind == pamSkipLine
		start += line.size
	}
	var insert string
	if insertion == size {
		insert = pamHook
	}
	return changed || insert != "", editConfig(input, dst, pending, size, insert)
}

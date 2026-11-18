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
	"slices"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

// SessionHelper and SessionName identify the PAM session helper.
const (
	SessionHelper = "/usr/local/libexec/accountsd-session"
	SessionName   = "accountsd-session"

	pamPath        = "/etc/pam.d/sshd"
	pamMemberTest  = "session [success=ignore default=1] pam_succeed_if.so quiet user ingroup shell"
	pamHelperLine  = "session required pam_exec.so type=open_session " + SessionHelper
	pamSkipSystemd = "session [success=1 default=ignore] pam_succeed_if.so quiet user ingroup shell"
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
	_, err := h.run(ctx, "busctl", "call", "org.freedesktop.systemd1", "/org/freedesktop/systemd1",
		"org.freedesktop.systemd1.Manager", "StartTransientUnit", "ssa(sv)a(sa(sv))",
		SessionName+"-"+pid+".scope", "fail", "2", "PIDs", "au", "1", pid, "Slice", "s", target, "0")
	return err
}

const (
	poolSliceContents    = "[Slice]\nCPUWeight=50\n"
	accountSliceContents = "[Slice]\nCPUWeight=100\nMemoryHigh=1.5G\nMemoryMax=2G\nTasksMax=512\n"
	personSliceContents  = "[Slice]\nMemoryHigh=600M\nMemoryMax=1G\nTasksMax=256\n"
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
	if err := h.installSession(); err != nil {
		return err
	}
	if reload {
		_, err := h.run(ctx, "systemctl", "daemon-reload")
		return err
	}
	return nil
}

func (h *Host) installSession() error {
	if err := h.installPAM(); err != nil {
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

func (h *Host) installPAM() error {
	const hook = pamMemberTest + "\n" + pamHelperLine + "\n"
	f, err := os.Open(h.path(pamPath))
	if errors.Is(err, fs.ErrNotExist) {
		_, err := h.write(pamPath, []byte(hook), 0o644)
		return err
	}
	if err != nil {
		return hostError(pamPath, err)
	}
	defer func() { _ = f.Close() }()
	return h.writeStream(pamPath, 0o644, func(dst io.Writer) (bool, error) {
		r := bufio.NewReader(f)
		var changed, managed, precedingSkip, precedingMemberTest, partial, needsLF bool
		for {
			fragment, err := r.ReadSlice('\n')
			if len(fragment) > 0 {
				skip, memberTest := false, false
				if !partial && !errors.Is(err, bufio.ErrBufferFull) {
					line := bytes.TrimSuffix(fragment, []byte{'\n'})
					skip = string(line) == pamSkipSystemd
					memberTest = string(line) == pamMemberTest
					managed = managed || precedingMemberTest && string(line) == pamHelperLine
					fields := bytes.Fields(line)
					if !precedingSkip && len(fields) >= 3 &&
						(string(fields[0]) == "session" || string(fields[0]) == "-session") &&
						string(fields[2]) == "pam_systemd.so" {
						if _, err := io.WriteString(dst, pamSkipSystemd+"\n"); err != nil {
							return false, err
						}
						changed = true
					}
				}
				precedingSkip, precedingMemberTest = skip, memberTest
				if _, err := dst.Write(fragment); err != nil {
					return false, err
				}
				needsLF = fragment[len(fragment)-1] != '\n'
			}
			if errors.Is(err, io.EOF) {
				break
			}
			partial = errors.Is(err, bufio.ErrBufferFull)
			if err != nil && !partial {
				return false, err
			}
		}
		if !managed {
			if needsLF {
				if _, err := io.WriteString(dst, "\n"); err != nil {
					return false, err
				}
			}
			if _, err := io.WriteString(dst, hook); err != nil {
				return false, err
			}
			changed = true
		}
		return changed, nil
	})
}

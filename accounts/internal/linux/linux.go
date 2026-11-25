// Package linux implements host operations on Amazon Linux.
package linux

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// Shared host paths, limits and systemd names.
const (
	ShellGroup      = "shell"
	KeysDir         = "/etc/ssh/authorized_keys"
	SkelDir         = "/var/lib/accountsd/skel"
	DataDir         = "/data"
	HostKeyDir      = "/data/ssh"
	DaemonPath      = "/usr/local/bin/accountsd"
	PoolSlice       = "acct.slice"
	MaxCapture      = 64 << 10
	ChownBatch      = 100
	MaxJournalLine  = 64 << 10
	MaxBanLine      = 512
	UpdatesInterval = time.Hour
)

// Env supplies the host's filesystem, processes, clock, download and diagnostics.
type Env struct {
	Root    string
	Execute func(context.Context, Command) (Result, error)
	Now     func() time.Time
	Sleep   func(time.Duration)
	Fetch   func(context.Context, string) (io.ReadCloser, error)
	Stderr  io.Writer
}

// Command is a direct process invocation.
type Command struct {
	Name   string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
}

// Result holds the completed process's output and exit status.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
}

// CommandError describes a process that failed to run or returned an unexpected status.
type CommandError struct {
	Label  string
	Result Result
	Err    error
}

func (e *CommandError) Error() string {
	if e.Err != nil {
		return e.Label + ": " + e.Err.Error()
	}
	return e.Label + ": exit status " + strconv.Itoa(e.Result.ExitCode)
}

func (e *CommandError) Unwrap() error { return e.Err }

type capture struct{ prefix []byte }

func (w *capture) Write(p []byte) (int, error) {
	w.prefix = append(w.prefix, p[:min(len(p), MaxCapture-len(w.prefix))]...)
	return len(p), nil
}

const commandPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin"

func resolveCommand(name, path string) (string, error) {
	if strings.ContainsRune(name, '/') {
		return name, nil
	}
	for dir := range strings.SplitSeq(path, ":") {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// Exec runs a command without a shell, bounding any output retained in memory.
func Exec(ctx context.Context, c Command) (Result, error) {
	program, err := resolveCommand(c.Name, commandPath)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var stdout, stderr capture
	out := c.Stdout
	if out == nil {
		out = &stdout
	}
	cmd := &exec.Cmd{
		Path: program, Args: append([]string{c.Name}, c.Args...),
		Env:   append(os.Environ(), "PATH="+commandPath),
		Stdin: c.Stdin, Stdout: out, Stderr: &stderr, WaitDelay: time.Second,
	}
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
	err = cmd.Wait()
	stop()
	result := Result{Stdout: stdout.prefix, Stderr: stderr.prefix, ExitCode: cmd.ProcessState.ExitCode()}
	if ctx.Err() != nil && result.ExitCode == -1 {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		err = nil
	}
	return result, err
}

// Real targets the host's own filesystem, programs and clock.
func Real() Env {
	return Env{Root: "/", Execute: Exec, Now: time.Now, Sleep: time.Sleep, Fetch: Get, Stderr: os.Stderr}
}

// AccountSlice is the systemd unit for an account.
func AccountSlice(account string) string { return "acct-" + escapeSlice(account) + ".slice" }

// PersonSlice is the systemd unit for a person's sessions.
func PersonSlice(account, user string) string {
	return "acct-" + escapeSlice(account) + "-" + escapeSlice(user) + ".slice"
}

func escapeSlice(name string) string { return strings.ReplaceAll(name, "-", `\x2d`) }

// Host reconciles and observes one Linux installation.
type Host struct {
	env          Env
	rootSize     string
	dataVolumeID string
	dataSize     string
	written      map[string]struct{}
	backups      []string
	claudeFailed bool
	claudeFailAt time.Time

	previousCPU     cpuTimes
	previousSlices  map[string]int64
	previousInspect time.Time
	inspected       bool
	updateTime      time.Time
	updatesRead     bool
	pendingUpdates  int
	reboot          bool
	cursor          string
	signIns         map[string]time.Time
}

// New builds a host using the injected environment.
func New(env Env) *Host {
	return &Host{env: env, written: make(map[string]struct{}), signIns: make(map[string]time.Time)}
}

// Apply brings the host to the desired state in fail-fast order.
func (h *Host) Apply(ctx context.Context, s protocol.DesiredState) error {
	if err := validateState(s); err != nil {
		return err
	}
	defer h.clearBackups()
	clear(h.written)
	if err := h.locks(ctx, s); err != nil {
		return err
	}
	for _, part := range []func(context.Context) error{
		h.packages, h.imds, h.journal, h.updates,
	} {
		if err := part(ctx); err != nil {
			return err
		}
	}
	started, err := h.fail2ban(ctx, s.Allowlist)
	if err != nil {
		return err
	}
	if err := h.liftBans(ctx, s, started); err != nil {
		return err
	}
	if err := h.shell(ctx); err != nil {
		return err
	}
	if err := h.setHostname(ctx, s); err != nil {
		return err
	}
	if err := h.growRoot(ctx); err != nil {
		return err
	}
	hasData, err := h.data(ctx, s)
	if err != nil {
		return err
	}
	if !hasData {
		if len(s.Accounts) != 0 || len(s.Users) != 0 {
			return machine.ErrNoDataVolume
		}
		return h.slices(ctx, s)
	}
	if err := h.hostKey(ctx); err != nil {
		return err
	}
	if err := h.sshd(ctx); err != nil {
		return err
	}
	for _, part := range []func(context.Context, protocol.DesiredState) error{
		h.slices, h.accounts, h.users, h.removedAccounts, h.isolation,
	} {
		if err := part(ctx, s); err != nil {
			return err
		}
	}
	h.applyClaude(ctx)
	return nil
}

func (h *Host) run(ctx context.Context, program string, args ...string) (Result, error) {
	return h.query(ctx, Command{Name: program, Args: args}, 0)
}

func (h *Host) query(ctx context.Context, c Command, allowed ...int) (Result, error) {
	res, err := h.env.Execute(ctx, c)
	if err == nil {
		if len(allowed) == 0 {
			return res, nil
		}
		for _, status := range allowed {
			if status == res.ExitCode {
				return res, nil
			}
		}
	}
	label := c.Name
	if (c.Name == "systemctl" || c.Name == "dnf") && len(c.Args) > 0 {
		label += " " + c.Args[0]
	}
	return res, &CommandError{Label: label, Result: res, Err: err}
}

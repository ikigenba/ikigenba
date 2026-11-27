package linux

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ikigenba/ikigenba/accounts/internal/machine"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
)

var (
	errBackupSummary         = errors.New("no valid snapshot summary")
	errResticWalkUnavailable = errors.New("restic walk unavailable")
	errResticFinalNotSynced  = errors.New("restic final not synced")
)

type backupRestic struct {
	h           *Host
	repo        string
	cache       string
	env         []string
	restoreOnly bool
	prepare     func([]string) ([]string, error)
}

func (h *Host) resticCommand(b *protocol.Backup, c protocol.Credentials, prefix, cacheName string) backupRestic {
	cache := ResticCache + "/" + cacheName
	return backupRestic{
		h: h, repo: "s3:s3." + b.Region + ".amazonaws.com/" + b.Bucket + "/" + strings.TrimRight(prefix, "/"), cache: cache,
		env: append(backupAWSEnv(c), "AWS_DEFAULT_REGION="+b.Region, "RESTIC_CACHE_DIR="+h.path(cache)),
	}
}

func (r backupRestic) command(args []string, stdout io.Writer) Command {
	if stdout == nil {
		stdout = io.Discard
	}
	return Command{
		Name: r.h.path(ResticPath), Args: append([]string{"-r", r.repo, "--insecure-no-password"}, args...),
		Stdout: stdout, Env: r.env,
	}
}

func (r backupRestic) run(ctx context.Context, args []string, stdout io.Writer, allowed ...int) (Result, error) {
	if err := r.h.lockedBackupRepos[r.repo]; !r.restoreOnly && err != nil {
		return Result{}, err
	}
	if err := r.h.privateBackupDir(r.cache); err != nil {
		return Result{}, err
	}
	prepared, err := r.prepareArgs(args)
	if err != nil {
		return Result{}, err
	}
	result, err := r.h.query(ctx, r.command(prepared, stdout), allowed...)
	if err == nil {
		return result, nil
	}
	var failed *CommandError
	_ = errors.As(err, &failed)
	failure := resticFailure(args[0], result, failed.Err)
	if !r.restoreOnly && result.ExitCode == 11 && failed.Err == nil {
		if unlock, err := r.prepareArgs([]string{"unlock"}); err == nil {
			_, _ = r.h.query(ctx, r.command(unlock, nil))
		}
		r.h.lockedBackupRepos[r.repo] = errors.New(boundBackupError(failure.Error()))
	}
	return result, failure
}

func (r backupRestic) prepareArgs(args []string) ([]string, error) {
	if r.prepare == nil {
		return args, nil
	}
	return r.prepare(args)
}

func resticFailure(verb string, result Result, err error) error {
	failure := &CommandError{Label: "restic " + verb, Result: result, Err: err}
	var last string
	for line := range strings.SplitSeq(string(result.Stderr), "\n") {
		if strings.Trim(line, " \t\r") != "" {
			last = line
		}
	}
	if last == "" {
		return failure
	}
	return fmt.Errorf("%w: %s", failure, strings.ReplaceAll(last, "\r", " "))
}

func (r backupRestic) ready(ctx context.Context) error {
	result, err := r.run(ctx, []string{"cat", "config"}, nil, 0, 10)
	if err == nil && result.ExitCode == 10 {
		_, err = r.run(ctx, []string{"init"}, nil, 0)
	}
	return err
}

func (r backupRestic) snapshot(ctx context.Context, host string, args []string) (at time.Time, partial error, fatal error) {
	arguments := append([]string{"backup", "--json", "--host", host}, args...)
	var validEnd bool
	writer := lineWriter{limit: MaxJournalLine, emit: func(line []byte) {
		var row struct {
			Type string          `json:"message_type"`
			ID   string          `json:"snapshot_id"`
			End  json.RawMessage `json:"backup_end"`
		}
		if json.Unmarshal(line, &row) != nil || row.Type != "summary" || row.ID == "" {
			return
		}
		var end string
		if json.Unmarshal(row.End, &end) != nil {
			validEnd = false
			return
		}
		var err error
		at, err = time.Parse(time.RFC3339Nano, end)
		validEnd = err == nil
	}}
	result, err := r.run(ctx, arguments, &writer, 0, 3)
	if err != nil {
		return time.Time{}, nil, err
	}
	writer.finish()
	if !validEnd {
		return time.Time{}, nil, resticFailure("backup", result, errBackupSummary)
	}
	if result.ExitCode == 3 {
		partial = resticFailure("backup", result, nil)
	}
	return at, partial, nil
}

func (r backupRestic) forget(ctx context.Context, name string, s protocol.Settings) error {
	_, err := r.run(ctx, []string{"forget", "--host", name, "--group-by", "host", "--keep-hourly", strconv.Itoa(s.Hourly),
		"--keep-daily", strconv.Itoa(s.Daily), "--keep-monthly", strconv.Itoa(s.Monthly), "--prune"}, nil, 0)
	return err
}

func (r backupRestic) snapshots(ctx context.Context, slug string) ([]int64, error) {
	snapshots := make([]int64, 0, protocol.MaxSnapshots)
	writer := newBackupJSONArray([]string{"time"}, func(row map[string]string) {
		at, err := time.Parse(time.RFC3339Nano, row["time"])
		if err != nil {
			return
		}
		second := at.Unix()
		i, _ := slices.BinarySearchFunc(snapshots, second, func(a, b int64) int { return cmp.Compare(b, a) })
		if i < protocol.MaxSnapshots {
			if len(snapshots) == protocol.MaxSnapshots {
				snapshots = snapshots[:len(snapshots)-1]
			}
			snapshots = slices.Insert(snapshots, i, second)
		}
	})
	_, err := r.run(ctx, []string{"snapshots", "--host", slug, "--json"}, writer, 0)
	writer.finish()
	return snapshots, err
}

func backupDue(last, now time.Time, count int, unit time.Duration) bool {
	if last.IsZero() {
		return true
	}
	elapsed := now.Sub(last)
	return elapsed >= 0 && int64(elapsed/unit) >= int64(count)
}

func backupCheckDue(facts backupAccountFacts, now time.Time, settings protocol.Settings) bool {
	return settings.CheckDays > 0 && backupDue(facts.Checked, now, settings.CheckDays, 24*time.Hour)
}

// Backup takes due restic snapshots and checks, or executes a requested restore.
func (h *Host) Backup(ctx context.Context, s protocol.DesiredState) error {
	ctx, pruneErr := h.beginBackupFacts(ctx, s)
	b := s.Backup
	dirErr := h.privateBackupDir(BackupDir)
	cacheErr := h.privateBackupDir(ResticCache)
	if b != nil && dirErr == nil {
		pruneErr = errors.Join(pruneErr, h.prunePartialRestores(s))
	}
	if !backupReady(b) {
		if b != nil && b.Restore == nil {
			_ = h.clearWholeRestores()
		}
		return nil
	}
	ctx = withBackupDiagnosticRedactor(ctx, b)
	installErr := errors.Join(h.ensureRestic(ctx), dirErr, cacheErr)
	h.lockedBackupRepos = make(map[string]error)
	defer func() { h.lockedBackupRepos = nil }()
	if dirErr == nil {
		installErr = errors.Join(installErr, h.updateHostBackup(func(facts *backupHostFacts) {
			facts.ResticInstallError = boundBackupError(h.resticInstall.errorText())
		}))
	}
	if b.Restore != nil {
		if installErr != nil {
			return firstBackupError(installErr, pruneErr)
		}
		return firstBackupError(pruneErr, h.backupWholeRestore(ctx, s))
	}
	cleanupErr := h.clearWholeRestores()
	if installErr != nil {
		return firstBackupError(installErr, firstBackupError(pruneErr, cleanupErr))
	}
	finals, passErr := h.backupFinals(ctx, s)
	passErr = firstBackupError(passErr, firstBackupError(pruneErr, cleanupErr))
	for _, account := range backupActiveAccounts(s) {
		slug := account.Slug
		creds, ok := accountBackupCredentials(b, slug)
		if !ok || finals.Accounts[slug] {
			continue
		}
		facts, walk, _ := h.accountBackupForPass(ctx, account)
		if walk == walkNotAttempted {
			continue
		}
		now := h.env.Now()
		snapshot := backupDue(facts.Snapshot, now, b.Settings.EveryMinutes, time.Minute)
		check := backupCheckDue(facts, now, b.Settings)
		if !snapshot && !check {
			continue
		}
		if walk == walkUnrecorded {
			passErr = firstBackupError(passErr, h.recordUnkeptWalk(ctx, slug))
			continue
		}
		_, err := h.backupAccount(ctx, s, account, creds, facts, snapshot, check, nil)
		passErr = firstBackupError(passErr, err)
	}
	if !finals.Host {
		passErr = firstBackupError(passErr, h.backupHostKeys(ctx, s, false))
	}
	return firstBackupError(passErr, h.backupPartialRestores(ctx, s))
}

func (h *Host) recordUnkeptWalk(ctx context.Context, slug string) error {
	return h.updateAccountBackup(ctx, slug, func(facts *backupAccountFacts) { facts.Error = machine.WalkUnrecorded })
}

func (h *Host) backupAccount(ctx context.Context, s protocol.DesiredState, account protocol.Account, c protocol.Credentials, facts backupAccountFacts, snapshot, check bool, final *backupCloseLock) (interrupted error, result error) {
	slug := account.Slug
	repo := h.resticCommand(s.Backup, c, protocol.FilesPrefix(slug), slug)
	var ran bool
	repo.prepare = func(args []string) ([]string, error) {
		prepared, err := h.prepareAccountRestic(ctx, s, account, args, final)
		switch {
		case errors.Is(err, errResticWalkUnavailable):
			interrupted = errResticWalkUnavailable
		case errors.Is(err, errResticFinalNotSynced):
			interrupted = errResticFinalNotSynced
		case err == nil:
			ran = true
		}
		return prepared, err
	}
	var partial error
	err := repo.ready(ctx)
	if err == nil && snapshot {
		partial, err = h.backupAccountSnapshot(ctx, s, slug, &facts, repo)
	}
	if err == nil && check {
		_, err = repo.run(ctx, []string{"check", "--read-data-subset=" + CheckSubset}, nil, 0)
		if err == nil {
			facts.Checked = h.env.Now()
		}
	}
	switch {
	case errors.Is(interrupted, errResticWalkUnavailable):
		facts.Error = machine.WalkUnrecorded
	case errors.Is(interrupted, errResticFinalNotSynced):
		if ran {
			facts.Error = ""
		}
	case err != nil:
		facts.Error = boundBackupError(err.Error())
		h.lockedBackupRepos[repo.repo] = errors.New(facts.Error)
	case partial != nil:
		facts.Error = boundBackupError(partial.Error())
	case ran:
		facts.Error = ""
	}
	if interrupted != nil && errors.Is(err, interrupted) {
		err = nil
	}
	saveErr := h.updateAccountBackup(ctx, slug, func(current *backupAccountFacts) {
		current.Snapshot, current.Snapshots, current.Checked, current.Error = facts.Snapshot, facts.Snapshots, facts.Checked, facts.Error
	})
	return interrupted, errors.Join(partial, err, saveErr)
}

func (h *Host) prepareAccountRestic(ctx context.Context, s protocol.DesiredState, account protocol.Account, args []string, final *backupCloseLock) ([]string, error) {
	slug := account.Slug
	for {
		facts, walk, readiness := h.accountBackupForPass(ctx, account)
		if walk != walkKept {
			return nil, unavailableResticWalk(final, readiness)
		}
		if args[0] != "backup" {
			return args, nil
		}
		if final != nil && !facts.syncedAfter(final.at) {
			return nil, errResticFinalNotSynced
		}
		manifest, paths, err := h.backupManifest(ctx, slug, facts.Walk)
		if err != nil {
			return nil, err
		}
		prepared := slices.Concat(args, h.backupAccountArgs(s, slug, manifest, paths))
		// A new walk can finish while the manifest is being written. Use
		// that walk before launching the snapshot, without locking restic.
		latest, walk, readiness := h.accountBackupForPass(ctx, account)
		if walk != walkKept {
			return nil, unavailableResticWalk(final, readiness)
		}
		if latest.WalkToken == facts.WalkToken {
			if final != nil && !latest.syncedAfter(final.at) {
				return nil, errResticFinalNotSynced
			}
			return prepared, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

func unavailableResticWalk(final *backupCloseLock, readiness backupWalkReadiness) error {
	if final != nil && !readiness.syncedAfter(final.at) {
		return errResticFinalNotSynced
	}
	return errResticWalkUnavailable
}

func (h *Host) backupAccountSnapshot(ctx context.Context, s protocol.DesiredState, slug string, facts *backupAccountFacts, repo backupRestic) (partial, fatal error) {
	at, partial, err := repo.snapshot(ctx, slug, nil)
	if err != nil {
		return partial, err
	}
	facts.Snapshot = at
	if err := repo.forget(ctx, slug, s.Backup.Settings); err != nil {
		return partial, err
	}
	snapshots, err := repo.snapshots(ctx, slug)
	if err == nil {
		facts.Snapshots = snapshots
	}
	return partial, err
}

func (h *Host) backupManifest(ctx context.Context, slug string, walk backupWalk) (string, []string, error) {
	manifest := BackupDir + "/manifest/" + slug
	var body strings.Builder
	var paths []string
	for _, p := range walPaths(walk) {
		info, err := os.Lstat(h.path("/srv/" + slug + "/" + p))
		if err != nil {
			continue
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		fmt.Fprintf(&body, "%d %d %04o %s\n", stat.Uid, stat.Gid, stat.Mode&0o7777, p)
		paths = append(paths, p)
	}
	h.backupFactsMu.Lock()
	defer h.backupFactsMu.Unlock()
	if h.currentBackupPlacement(ctx, slug) == nil {
		return "", nil, errResticWalkUnavailable
	}
	_, err := h.writeBackupFile(manifest, []byte(body.String()))
	return manifest, paths, err
}

var resticPatternEscape = strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[")

func escapeResticPattern(s string) string { return resticPatternEscape.Replace(s) }

func (h *Host) backupAccountArgs(s protocol.DesiredState, slug, manifest string, paths []string) []string {
	var args []string
	for _, path := range paths {
		pattern := h.path("/srv/"+slug) + "/" + escapeResticPattern(path)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			args = append(args, "--exclude", pattern+suffix)
		}
	}
	var homes []string
	for _, user := range s.Users {
		if user.Account == slug && (user.State == protocol.UserActive || user.State == protocol.UserLocked) && valid.Name(user.Name) == nil {
			home := h.path("/home/" + user.Name)
			if info, err := os.Lstat(home); err == nil && info.IsDir() {
				homes = append(homes, home)
			}
		}
	}
	slices.Sort(homes)
	for _, home := range homes {
		for _, suffix := range []string{"/.cache", "/.npm", "/.local/share/claude", "/.local/state/claude"} {
			args = append(args, "--exclude", home+suffix)
		}
	}
	args = append(args, h.path("/srv/"+slug))
	args = append(args, homes...)
	return append(args, h.path(manifest))
}

func (h *Host) backupHostKeys(ctx context.Context, s protocol.DesiredState, force bool) error {
	if !hostBackupCredentials(s.Backup) {
		return nil
	}
	digests, err := h.backupHostDigests()
	if err != nil {
		return errors.Join(err, h.updateHostBackup(func(facts *backupHostFacts) { facts.Error = boundBackupError(err.Error()) }))
	}
	if digests == [2]string{} {
		if force {
			return hostError(HostKeyDir, fs.ErrNotExist)
		}
		return nil
	}
	facts := h.readHostBackup()
	if !force && facts.KeyDigests == digests && !backupDue(facts.Snapshot, h.env.Now(), s.Backup.Settings.EveryMinutes, time.Minute) {
		return nil
	}
	repo := h.resticCommand(s.Backup, s.Backup.Host, s.Backup.HostPrefix, ".host")
	if err = repo.ready(ctx); err == nil {
		var at time.Time
		var partial error
		at, partial, err = repo.snapshot(ctx, s.Host, []string{h.path(HostKeyDir)})
		if err == nil {
			facts.Snapshot, facts.KeyDigests = at, digests
			err = repo.forget(ctx, s.Host, s.Backup.Settings)
			if err == nil {
				err = partial
			}
		}
	}
	facts.Error = ""
	if err != nil {
		facts.Error = boundBackupError(err.Error())
	}
	return errors.Join(err, h.updateHostBackup(func(current *backupHostFacts) {
		current.Snapshot, current.KeyDigests, current.Error = facts.Snapshot, facts.KeyDigests, facts.Error
	}))
}

func (h *Host) backupHostDigests() ([2]string, error) {
	var digests [2]string
	for i, suffix := range []string{"", ".pub"} {
		path := HostKeyDir + "/" + hostKeyName + suffix
		digest, err := h.backupKeyDigest(path)
		if errors.Is(err, fs.ErrNotExist) {
			return [2]string{}, nil
		}
		if err != nil {
			return [2]string{}, err
		}
		digests[i] = digest
	}
	return digests, nil
}

func (h *Host) backupKeyDigest(path string) (string, error) {
	full := h.path(path)
	fd, err := unix.Open(full, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ELOOP) {
		return "", fs.ErrNotExist
	}
	if err != nil {
		return "", hostError(path, err)
	}
	f := os.NewFile(uintptr(fd), full)
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", hostError(path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fs.ErrNotExist
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, f); err != nil {
		return "", hostError(path, err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

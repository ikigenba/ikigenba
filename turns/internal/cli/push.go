package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/turns/internal/config"
	"github.com/ikigenba/ikigenba/turns/internal/local"
	"github.com/ikigenba/ikigenba/turns/internal/paths"
	"github.com/ikigenba/ikigenba/turns/internal/push"
	"github.com/ikigenba/ikigenba/turns/internal/seam"
	"github.com/ikigenba/ikigenba/turns/internal/session"
	"github.com/ikigenba/ikigenba/turns/internal/space"
)

func runPush(ctx context.Context, args []string, _ io.Reader, stdout, stderr io.Writer, deps seam.Deps) int {
	dry := slices.Contains(args, "--dry-run")
	p, err := paths.Resolve(deps.Getenv, deps.Etc)
	if err != nil {
		return diagnostic(stderr, err.Error(), "")
	}
	cfg, err := config.Load(p)
	if err != nil {
		return diagnostic(stderr, "could not read the configuration", err.Error())
	}
	if !cfg.Configured() {
		hint := "run 'turns configure <space>'"
		if cfg.HostSpace != "" {
			hint = "run 'turns configure'"
		}
		return diagnosticCode(stderr, "not configured", hint, ExitNotConfigured)
	}
	if !dry {
		lockPath := filepath.Join(p.State, paths.LockFile)
		lock, lockErr := pushLock(p.State)
		if lockErr != nil {
			return diagnostic(stderr, fmt.Sprintf("could not lock '%s'", lockPath), lockErr.Error())
		}
		defer func() { _ = lock.Close() }()
	}
	host, err := deps.Hostname()
	if err != nil {
		return diagnostic(stderr, "could not read the host name", err.Error())
	}
	host, _, _ = strings.Cut(host, ".")
	if host == "" {
		return diagnostic(stderr, "could not read the host name", "")
	}
	found := local.Read(p)
	client := space.New(cfg.Space, cfg.Token, deps.HTTP)
	var held []space.Held
	for start := 0; start < len(found.Sessions); start += space.MaxLatest {
		end := min(start+space.MaxLatest, len(found.Sessions))
		refs := make([]session.Ref, 0, end-start)
		for _, s := range found.Sessions[start:end] {
			refs = append(refs, s.Ref)
		}
		answer, readErr := client.Latest(ctx, refs)
		if readErr != nil {
			if !dry {
				pushReport(stdout, nil, found.Skipped, false)
			}
			return pushFailure(stderr, readErr)
		}
		held = append(held, answer...)
	}
	batches := push.Batches(push.Select(found.Sessions, held))
	if dry {
		pushReport(stdout, batches, found.Skipped, true)
		return ExitOK
	}
	for i, batch := range batches {
		sessions := make([]session.Session, 0, len(batch))
		for _, pending := range batch {
			sessions = append(sessions, pending.Session)
		}
		if _, err = client.Push(ctx, host, sessions); err != nil {
			pushReport(stdout, batches[:i], found.Skipped, false)
			return pushFailure(stderr, err)
		}
	}
	pushReport(stdout, batches, found.Skipped, false)
	lastPath := filepath.Join(p.State, paths.LastPushFile)
	if err = os.WriteFile(lastPath, []byte(deps.Now().UTC().Format(time.RFC3339)+"\n"), 0600); err != nil {
		return diagnostic(stderr, fmt.Sprintf("could not write '%s'", lastPath), err.Error())
	}
	return ExitOK
}

func pushLock(state string) (*os.File, error) {
	if err := os.MkdirAll(state, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(state)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.OpenFile(paths.LockFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func pushFailure(stderr io.Writer, err error) int {
	var failure *space.Error
	if !errors.As(err, &failure) {
		return diagnostic(stderr, "push failed: "+err.Error(), "")
	}
	detail := failure.Detail
	if failure.Quoted {
		detail = seam.QuoteOutput(detail)
	}
	return diagnostic(stderr, "push failed: "+failure.Reason, detail)
}

func pushReport(stdout io.Writer, batches [][]push.Pending, skipped map[session.Tool]int, dry bool) {
	counts := push.Count(batches)
	action := "pushed"
	if dry {
		action = "to push"
	}
	for _, tool := range session.Tools() {
		tally := counts[tool]
		_, _ = fmt.Fprintf(stdout, "%s: %d %s, %d %s %s\n", tool, tally.Sessions, pushPlural(tally.Sessions, "session"), tally.Turns, pushPlural(tally.Turns, "turn"), action)
		if n := skipped[tool]; n > 0 {
			_, _ = fmt.Fprintf(stdout, "%s: %d %s skipped\n", tool, n, pushPlural(n, "session"))
		}
	}
}

func pushPlural(n int, singular string) string {
	if n == 1 {
		return singular
	}
	return singular + "s"
}

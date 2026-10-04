package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/git"
	"github.com/ikigenba/ikigenba/sites/internal/limits"
	"github.com/ikigenba/ikigenba/sites/internal/pages"
	"github.com/ikigenba/ikigenba/sites/internal/server"
	"github.com/ikigenba/ikigenba/sites/internal/settings"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/web"
)

type runStream struct {
	mu     sync.Mutex
	out    io.Writer
	closed bool
}

func (s *runStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return len(p), nil
	}
	return s.out.Write(p)
}
func (s *runStream) finish(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		_, _ = s.out.Write(p)
		s.closed = true
	}
}
func (s *runStream) close() { s.mu.Lock(); s.closed = true; s.mu.Unlock() }

type runRandom struct {
	mu     sync.Mutex
	source io.Reader
}

func (r *runRandom) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.source.Read(p)
}

func runServe(ctx context.Context, p Process) int {
	stderr := &runStream{out: p.Stderr}
	defer stderr.close()
	diagnostic := func(message string, exit int) int { stderr.finish([]byte("sites: " + message + "\n")); return exit }
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		return diagnostic(err.Error(), ExitUsage)
	}
	pid, pidSet := p.LookupEnv("LISTEN_PID")
	count, countSet := p.LookupEnv("LISTEN_FDS")
	digits := countSet && count != ""
	for _, c := range count {
		if c < '0' || c > '9' {
			digits = false
		}
	}
	normalized := strings.TrimLeft(count, "0")
	if !pidSet || pid != strconv.Itoa(p.Pid) || !digits || normalized == "" {
		return diagnostic("no socket was passed in\n\nrun it under systemd, with a listening socket passed in", ExitUsage)
	}
	if normalized != "1" {
		return diagnostic(count+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in", ExitUsage)
	}
	if p.Unsetenv != nil {
		for _, key := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(key)
		}
	}
	inherit := p.Inherit
	if inherit == nil {
		inherit = func(fd uintptr) (net.Listener, error) {
			f := os.NewFile(fd, "sites-listener")
			if f == nil {
				return nil, fmt.Errorf("cannot take file descriptor %d", fd)
			}
			defer func() { _ = f.Close() }()
			ln, e := net.FileListener(f)
			if u, ok := ln.(*net.UnixListener); ok {
				u.SetUnlinkOnClose(false)
			}
			return ln, e
		}
	}
	ln, err := inherit(3)
	if err != nil {
		return diagnostic(err.Error(), ExitServerFailed)
	}
	defer func() { _ = ln.Close() }()
	path, pathSet := p.LookupEnv("PATH")
	if !pathSet {
		path = ""
	}
	g, err := git.Find(path, p.Environ)
	if err != nil {
		return diagnostic("git not found on PATH", ExitServerFailed)
	}
	if ctx.Err() != nil {
		return ExitSuccess
	}
	random := p.Rand
	if random == nil {
		random = rand.Reader
	}
	guarded := &runRandom{source: random}
	database := p.Database
	if database == "" {
		database = filepath.Join(p.Dir, "state", "sites.db")
	}
	if p.Database != "" && p.Database != ":memory:" {
		// Only the explicit catalog and its SQLite sidecars may be created; its
		// parent is not one of the startup entries declared for this run.
		_, err = os.Stat(filepath.Dir(database))
		if ctx.Err() != nil {
			return ExitSuccess
		}
		if err != nil {
			return diagnostic("cannot open database state/sites.db: "+err.Error(), ExitServerFailed)
		}
	}
	if p.Database == "" {
		err = startupContainedPath(p.Dir, database)
		if ctx.Err() != nil {
			return ExitSuccess
		}
		if err != nil {
			return diagnostic("cannot open database state/sites.db: "+err.Error(), ExitServerFailed)
		}
	}
	catalog, err := store.Open(ctx, store.Config{Source: database, Now: p.Now, Rand: guarded})
	if ctx.Err() != nil {
		if catalog != nil {
			_ = catalog.Close()
		}
		return ExitSuccess
	}
	if err != nil {
		return diagnostic("cannot open database state/sites.db: "+err.Error(), ExitServerFailed)
	}
	defer func() { _ = catalog.Close() }()
	lim := limits.New(s, limits.Clock{After: p.After})
	defer lim.Halt()
	repos := s.ReposDir
	if !filepath.IsAbs(repos) {
		repos = filepath.Join(p.Dir, repos)
	}
	treesRoot := filepath.Join(p.Dir, "cache", "sites")
	err = startupContainedPath(p.Dir, treesRoot)
	if ctx.Err() != nil {
		return ExitSuccess
	}
	if err != nil {
		return diagnostic("cannot create directory cache/sites: "+err.Error(), ExitServerFailed)
	}
	trees, err := cache.Open(cache.Config{Root: treesRoot, Repos: repos, Git: g, Limits: lim})
	if ctx.Err() != nil {
		return ExitSuccess
	}
	if err != nil {
		return diagnostic("cannot create directory cache/sites: "+err.Error(), ExitServerFailed)
	}
	services, servicesSet := p.LookupEnv("IKIGENBA_SERVICES")
	if !servicesSet {
		services = ""
	}
	if ctx.Err() != nil {
		return ExitSuccess
	}
	address, addressSet := p.LookupEnv("NOTIFY_SOCKET")
	if ctx.Err() != nil {
		return ExitSuccess
	}
	if addressSet && address != "" {
		conn, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
		if e == nil {
			_, e = conn.Write([]byte("READY=1"))
			_ = conn.Close()
		}
		if e != nil {
			return diagnostic(e.Error(), ExitServerFailed)
		}
	}
	writer := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: Version, Sink: p.Sink, Stderr: stderr, Now: p.Now, Sleep: p.Sleep, Rand: guarded})
	handler := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(writer), ServicesPath: services, Store: catalog, Cache: trees, Limits: lim, Telemetry: writer, Rand: guarded})
	if (!addressSet || address == "") && ctx.Err() != nil {
		// Shutdown would record service.stopping before this run became ready.
		return ExitSuccess
	}
	writer.Ready()
	serving, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	defer cancel(context.Canceled)
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			lim.Drain()
			cancel(context.Cause(ctx))
		case <-watchDone:
		}
	}()
	duration := time.Duration(math.MaxInt64)
	if s.DrainSeconds <= math.MaxInt64/int64(time.Second) {
		duration = time.Duration(s.DrainSeconds) * time.Second
	}
	stopped := false
	err = server.Serve(serving, ln, handler, duration, func(stopCtx context.Context) {
		writer.Shutdown(stopCtx, context.Cause(ctx).Error())
		stopped = true
	})
	lim.Halt()
	if err != nil {
		// Accept failures still flush the writer before the final diagnostic.
		if !stopped {
			ended, end := context.WithCancel(context.Background())
			end()
			writer.Shutdown(ended, err.Error())
		}
		return diagnostic(err.Error(), ExitServerFailed)
	}
	return ExitSuccess
}

// startupPhysicalPath follows symlinks component by component, retaining the
// physical meaning of .. in their targets and resolving absent leaf entries.
func startupPhysicalPath(path string) (string, error) {
	current := string(os.PathSeparator)
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd + string(os.PathSeparator) + path
	}
	remaining := strings.Split(path, string(os.PathSeparator))
	links := 0
	for len(remaining) > 0 {
		part := remaining[0]
		remaining = remaining[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				current = next
				continue
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			current = next
			if !info.IsDir() && len(remaining) > 0 {
				return filepath.Join(append([]string{current}, remaining...)...), nil
			}
			continue
		}
		links++
		if links > 40 {
			_, err = os.Stat(path)
			return "", err
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(target) {
			current = string(os.PathSeparator)
		}
		remaining = append(strings.Split(target, string(os.PathSeparator)), remaining...)
	}
	return current, nil
}
func startupContainedPath(dir, path string) error {
	root, err := startupPhysicalPath(dir)
	if err != nil {
		return err
	}
	target, err := startupPhysicalPath(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil
	}
	boundary, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = boundary.Close() }()
	_, err = boundary.Stat(relative)
	return err
}

// Package cli implements the command and its injected process boundary.
package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/prompts"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/pages"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/server"
	"github.com/ikigenba/ikigenba/candidates/internal/settings"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/trail"
	"github.com/ikigenba/ikigenba/candidates/internal/web"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// SweepInterval is the delay between pass retention sweeps.
const SweepInterval = time.Hour

// Process carries the dependencies of one command invocation.
type Process struct {
	Args      []string
	LookupEnv func(string) (string, bool)
	Unsetenv  func(string) error
	Pid       int
	Stdout    io.Writer
	Stderr    io.Writer
	Version   string
	Inherit   func(uintptr) (net.Listener, error)
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration)
	After     func(time.Duration) <-chan time.Time
	Rand      io.Reader
	Dir       string
	Sink      telemetry.Sink
	EventSink events.Sink
	Banner    func(page.User) page.Banner
	MCP       func(*telemetry.Writer) *mcp.Server
	HTTP      *http.Client
}
type lockedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (r *lockedReader) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Read(b)
}

type telemetryGate struct {
	sink telemetry.Sink
	gate context.Context
}

func (s telemetryGate) Deliver(ctx context.Context, e telemetry.Event) error {
	if err := s.gate.Err(); err != nil {
		return err
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.gate, cancel)
	defer stop()
	return s.sink.Deliver(call, e)
}

type eventsGate struct {
	sink events.Sink
	gate context.Context
}

func (s eventsGate) Deliver(ctx context.Context, e events.Event) error {
	if err := s.gate.Err(); err != nil {
		return err
	}
	call, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.gate, cancel)
	defer stop()
	return s.sink.Deliver(call, e)
}

func diagnostic(p Process, s string) {
	_, _ = p.Stderr.Write([]byte("candidates: " + strings.ReplaceAll(s, "\n", " ") + "\n"))
}

// Run executes a command or serves with the supplied process dependencies.
func Run(ctx context.Context, p Process) int {
	if len(p.Args) > 0 {
		return command(ctx, p)
	}
	s, err := settings.Read(p.LookupEnv)
	if err != nil {
		_, _ = p.Stderr.Write([]byte("candidates: " + err.Error() + "\n"))
		return ExitUsage
	}
	pid, _ := p.LookupEnv("LISTEN_PID")
	fds, _ := p.LookupEnv("LISTEN_FDS")
	n, _ := strconv.ParseUint(fds, 10, 64)
	if pid != strconv.Itoa(p.Pid) || n != 1 {
		msg := "no socket was passed in"
		if pid == strconv.Itoa(p.Pid) && n > 1 {
			msg = fds + " sockets were passed in, expected 1"
		}
		_, _ = p.Stderr.Write([]byte("candidates: " + msg + "\n\nrun it under systemd, with a listening socket passed in\n"))
		return ExitUsage
	}
	if p.Unsetenv != nil {
		for _, k := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
			_ = p.Unsetenv(k)
		}
	}
	ln, err := p.Inherit(3)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitServerFailed
	}
	defer func() { _ = ln.Close() }()
	bg := context.Background()
	d, err := db.Open(bg, db.Config{Path: filepath.Join(p.Dir, "state", "candidates.db"), Migrations: candidates.Migrations(), Now: p.Now, Service: pages.ServiceName, Stderr: p.Stderr})
	if err != nil {
		diagnostic(p, "cannot open database state/candidates.db: "+err.Error())
		return ExitServerFailed
	}
	defer func() { _ = d.Close() }()
	gate, gateCancel := context.WithCancel(bg)
	defer gateCancel()
	deadlineReady := make(chan context.Context, 1)
	go func() {
		select {
		case <-ctx.Done():
			dc, cancel := context.WithTimeout(bg, s.Drain())
			deadlineReady <- dc
			select {
			case <-dc.Done():
				gateCancel()
			case <-gate.Done():
			}
			cancel()
		case <-gate.Done():
		}
	}()
	sink := p.Sink
	if sink == nil {
		sink = telemetry.NewSocketSink()
	}
	eventSink := p.EventSink
	if eventSink == nil {
		eventSink = events.NewSocketSink()
	}
	rnd := &lockedReader{r: p.Rand}
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Version: p.Version, Sink: telemetryGate{sink: sink, gate: gate}, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: rnd})
	em := events.New(events.Config{Service: pages.ServiceName, Sink: eventsGate{sink: eventSink, gate: gate}, Stderr: p.Stderr, Now: p.Now, Sleep: p.Sleep, Rand: rnd, Telemetry: w, Emits: trail.Emits()})
	path, _ := p.LookupEnv(services.Variable)
	list, _ := services.Read(path)
	var sc *scrapes.Client
	if e, ok := list.Find("scrapes"); ok {
		sc = scrapes.NewClient(scrapes.ClientConfig{Service: e, Telemetry: w})
	}
	var pc *prompts.Client
	if e, ok := list.Find("prompts"); ok {
		pc = prompts.NewClient(prompts.ClientConfig{Service: e, Telemetry: w})
	}
	sleep := p.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
	}
	st := store.New(d, store.Config{Now: p.Now, Rand: rnd, Origin: s.RouteOrigin, Sleep: sleep})
	var checker *website.Checker
	client := p.HTTP
	if _, sandbox := p.LookupEnv("IKIGENBA_SANDBOX"); sandbox {
		stub, err := website.NewStub()
		if err != nil {
			diagnostic(p, err.Error())
			return ExitServerFailed
		}
		defer func() { _ = stub.Close() }()
		client = stub.Client()
	}
	if client != nil {
		checker = &website.Checker{Client: client, After: p.After}
	}
	var router *routes.Client
	var nearby *places.Client
	if s.MapsAPIKey != "" {
		router = &routes.Client{HTTP: p.HTTP, Key: s.MapsAPIKey, After: p.After}
		placesHTTP := p.HTTP
		if _, sandbox := p.LookupEnv("IKIGENBA_SANDBOX"); sandbox {
			placesHTTP = &http.Client{Transport: places.Fake{}}
		}
		nearby = &places.Client{HTTP: placesHTTP, Key: s.MapsAPIKey, After: p.After}
	}
	eng := passes.New(passes.Config{Stderr: p.Stderr, Routes: router, Places: nearby, Website: checker, Prompts: pc, After: p.After, Store: st, Sources: sources.New(fmcsa.New(), ppp.New(), form5500.New(), oshaita.New()), Scrapes: sc, Telemetry: w, Events: em})
	w.Ready()
	err = st.ReplaceZipDrive(bg, candidates.ZipDrive())
	if err == nil {
		err = eng.Recover(bg)
	}
	if err != nil {
		diagnostic(p, store.Unreachable)
		dc, cancel := context.WithTimeout(bg, s.Drain())
		defer cancel()
		em.Shutdown(dc)
		_ = w.Flush(dc)
		return ExitServerFailed
	}
	prune(bg, st, s.KeepPasses)
	startCtx, startCancel := context.WithCancel(ctx)
	defer startCancel()
	startDone := make(chan struct{})
	requestID := ""
	if sc != nil {
		var draw [16]byte
		_, _ = io.ReadFull(rnd, draw[:])
		requestID = hex.EncodeToString(draw[:])
	}
	filled := eng.StartFill(startCtx)
	refetched := eng.StartRefetch(startCtx)
	go func() {
		defer close(startDone)
		<-filled
		if sc != nil {
			_, _ = eng.StartParks(startCtx, requestID)
		}
	}()
	h := web.Handler(web.Config{Banner: p.Banner, MCP: p.MCP(w), LegacyMCP: p.MCP(w), ServicesPath: path, Store: st, Passes: eng, Telemetry: w, Events: em, Website: checker})
	notify, _ := p.LookupEnv("NOTIFY_SOCKET")
	if notify != "" {
		if c, e := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: notify, Net: "unixgram"}); e == nil {
			_, _ = c.Write([]byte("READY=1"))
			_ = c.Close()
		}
	}
	after := p.After
	if after == nil {
		after = time.After
	}
	sweepCtx, sweepCancel := context.WithCancel(ctx)
	sweepDone := make(chan struct{})
	go func() {
		defer close(sweepDone)
		for {
			if sweepCtx.Err() != nil {
				return
			}
			select {
			case <-sweepCtx.Done():
				return
			case <-after(SweepInterval):
				if sweepCtx.Err() == nil {
					prune(bg, st, s.KeepPasses)
				}
			}
		}
	}()
	err = server.Serve(ctx, ln, h, s.Drain(), func(dc context.Context) {
		sweepCancel()
		<-sweepDone
		startCancel()
		eng.Drain(dc)
		<-startDone
		<-refetched
	})
	sweepCancel()
	<-sweepDone
	var dc context.Context
	var cancel context.CancelFunc
	if ctx.Err() != nil {
		dc = <-deadlineReady
	} else {
		dc, cancel = context.WithTimeout(bg, s.Drain())
		defer cancel()
	}
	startCancel()
	eng.Drain(dc)
	<-startDone
	<-refetched
	_ = em.Flush(dc)
	reason := "stopped"
	if cause := context.Cause(ctx); cause != nil {
		reason = cause.Error()
	}
	w.Shutdown(dc, reason)
	em.Shutdown(dc)
	if err != nil {
		diagnostic(p, err.Error())
		return ExitServerFailed
	}
	return ExitSuccess
}
func command(ctx context.Context, p Process) int {
	a := p.Args
	if len(a) == 1 {
		switch a[0] {
		case "--version":
			_, _ = fmt.Fprintln(p.Stdout, p.Version)
			return ExitSuccess
		case "manifest":
			_, _ = io.WriteString(p.Stdout, Manifest)
			return ExitSuccess
		case "--help":
			_, _ = io.WriteString(p.Stdout, Usage)
			return ExitSuccess
		}
	}
	if len(a) == 2 && a[0] == "db" && a[1] == "status" {
		err := db.Status(ctx, db.Config{Path: filepath.Join(p.Dir, "state", "candidates.db"), Migrations: candidates.Migrations()}, p.Stdout)
		if err != nil {
			diagnostic(p, err.Error())
			return ExitServerFailed
		}
		return ExitSuccess
	}
	arg := a[0]
	if len(a) > 1 {
		switch a[0] {
		case "--version", "manifest", "--help", "db":
			arg = a[1]
			if a[0] == "db" && a[1] == "status" && len(a) > 2 {
				arg = a[2]
			}
		}
	}
	kind := "command"
	if strings.HasPrefix(arg, "-") {
		kind = "option"
	}
	_, _ = p.Stderr.Write([]byte("candidates: unknown " + kind + " '" + arg + "'\n\nsee 'candidates --help' for usage\n"))
	return ExitUsage
}

func prune(ctx context.Context, st *store.Store, keep int64) {
	for {
		n, err := st.PrunePasses(ctx, keep, passes.BatchSize)
		if err != nil || n == 0 {
			return
		}
	}
}

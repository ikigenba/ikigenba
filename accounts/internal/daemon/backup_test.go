package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/boxmcp"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/daemon"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

// R-8VN0-EM52 R-8WUW-SDVR
func TestBackupConstants(t *testing.T) {
	const streamTick = daemon.StreamTick
	const backupTick = daemon.BackupTick
	const diagBackup string = daemon.DiagBackup
	if streamTick != 30*time.Second || backupTick != time.Minute {
		t.Errorf("stream tick = %v, backup tick = %v", streamTick, backupTick)
	}
	other := []string{daemon.DiagArguments, daemon.DiagURL, daemon.DiagSetup, daemon.DiagFetch,
		daemon.DiagUpdate, daemon.DiagApply, daemon.DiagReport, daemon.DiagSocket, daemon.DiagForward, ""}
	for _, message := range other {
		if diagBackup == message {
			t.Errorf("backup diagnostic duplicates %q", message)
		}
	}
}

type passCall struct {
	state   protocol.DesiredState
	release chan error
}

type passDriver struct {
	calls    chan passCall
	timers   chan chan time.Time
	active   atomic.Int32
	started  atomic.Int32
	interval time.Duration
}

func newPassDriver(interval time.Duration) *passDriver {
	return &passDriver{calls: make(chan passCall, 16), timers: make(chan chan time.Time, 16), interval: interval}
}

func (p *passDriver) fake(t *testing.T) func(context.Context, protocol.DesiredState) error {
	t.Helper()
	return func(ctx context.Context, state protocol.DesiredState) error {
		if ctx.Err() != nil {
			t.Errorf("pass for %v began with a canceled context", p.interval)
		}
		if p.active.Add(1) != 1 {
			t.Errorf("overlapping calls for %v", p.interval)
		}
		defer p.active.Add(-1)
		p.started.Add(1)
		call := passCall{state: state, release: make(chan error, 1)}
		select {
		case p.calls <- call:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case err := <-call.release:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *passDriver) timer() <-chan time.Time {
	tick := make(chan time.Time, 4)
	p.timers <- tick
	return tick
}

func (p *passDriver) expect(t *testing.T, state protocol.DesiredState) (passCall, chan time.Time) {
	t.Helper()
	call, tick := waitBox(t, p.calls), waitBox(t, p.timers)
	if !reflect.DeepEqual(call.state, state) {
		t.Errorf("%v pass got version %d backup %+v, want version %d backup %+v",
			p.interval, call.state.Version, call.state.Backup, state.Version, state.Backup)
	}
	return call, tick
}

func (p *passDriver) expectCount(t *testing.T, want int32) {
	t.Helper()
	if got := p.started.Load(); got != want {
		t.Errorf("%v passes started = %d, want %d", p.interval, got, want)
	}
}

// R-92YE-P8L8 R-946B-30BX R-95E7-GS2M R-96M3-UJTB R-97U0-8BK0 R-991W-M3AP
func TestBackupWorkersFollowAppliedStateAndKeepTheCycleIndependent(t *testing.T) {
	synctest.Test(t, testBackupWorkersFollowAppliedStateAndKeepTheCycleIndependent)
}

func testBackupWorkersFollowAppliedStateAndKeepTheCycleIndependent(t *testing.T) {
	s := newBoxScenario(t)
	stream, backup := newPassDriver(daemon.StreamTick), newPassDriver(daemon.BackupTick)
	boxAfter := s.cfg.After
	s.cfg.After = func(interval time.Duration) <-chan time.Time {
		switch interval {
		case daemon.StreamTick:
			return stream.timer()
		case daemon.BackupTick:
			return backup.timer()
		default:
			return boxAfter(interval)
		}
	}
	var sleeps, requests, metadata atomic.Int64
	sleep := s.cfg.Sleep
	s.cfg.Sleep = func(ctx context.Context, duration time.Duration) {
		sleeps.Add(1)
		sleep(ctx, duration)
	}
	s.cfg.Metadata = metadataFunc(func(context.Context) (cloud.InstanceFacts, error) {
		metadata.Add(1)
		return cloud.InstanceFacts{InstanceID: "fake-host"}, nil
	})
	reports := make(chan protocol.Report, 4)
	transport := s.cfg.HTTP.Transport
	s.cfg.HTTP.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		if req.URL.Path == protocol.ReportPath {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			defer func() { _ = body.Close() }()
			var report protocol.Report
			if err := json.NewDecoder(body).Decode(&report); err != nil {
				return nil, err
			}
			reports <- report
		}
		return transport.RoundTrip(req)
	})
	applyStarted := make(chan int64, 2)
	applyRelease := make(chan error)
	applyErr := errors.New("second apply failed")
	installStarted := make(chan string, 1)
	installRelease := make(chan struct{})
	s.cfg.Machine = machineProbe{
		apply: func(ctx context.Context, state protocol.DesiredState) error {
			applyStarted <- state.Version
			select {
			case err := <-applyRelease:
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		inspect: func(context.Context, protocol.DesiredState) (protocol.Report, error) {
			return protocol.Report{OS: "test facts"}, nil
		},
		install: func(ctx context.Context, _ io.Reader, digest string) error {
			installStarted <- digest
			select {
			case <-installRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
		stream: stream.fake(t), backup: backup.fake(t),
	}
	const message = `{"result":{}}`
	s.onBox = func(*http.Request) (*http.Response, error) { return boxHTTPReply(http.StatusOK, message), nil }
	s.onBinary = func() *http.Response { return boxHTTPReply(http.StatusOK, "new daemon") }
	s.start(t)
	first := activeBoxState(1, "")
	first.Backup = &protocol.Backup{Bucket: "test-bucket", Host: protocol.Credentials{AccessKeyID: "first-session"}}
	second := activeBoxState(2, "")
	waitBox(t, s.polls)
	s.state(t, first)
	if version := waitBox(t, applyStarted); version != first.Version {
		t.Fatalf("first Apply got version %d", version)
	}
	synctest.Wait()
	stream.expectCount(t, 0)
	backup.expectCount(t, 0)
	sendBox(t, applyRelease, nil)
	firstStream, streamTick := stream.expect(t, first)
	firstBackup, backupTick := backup.expect(t, first)
	if report := waitBox(t, reports); report.Attempted != 1 || report.Applied != 1 || report.Error != "" || report.OS != "test facts" || report.InstanceID != "fake-host" {
		t.Errorf("first report = %+v", report)
	}
	waitBox(t, s.polls)

	// A pass that returns early needs a tick, and it can run while the
	// other pass and the next long poll both remain in progress.
	firstBackup.release <- nil
	synctest.Wait()
	backup.expectCount(t, 1)
	backupTick <- time.Time{}
	secondBackup, backupTick := backup.expect(t, first)
	stream.expectCount(t, 1)
	client, _ := s.dial(t, nil)
	socketLine(t, client, boxRequest(protocol.MethodToolsCall, "1")+"\n")
	expectBoxAnswer(t, client, boxmcp.Answer{Message: json.RawMessage(message)})

	// Several deliveries on a running pass's channel count as one tick.
	// The next pass starts while the next Apply is still blocked.
	s.state(t, second)
	if version := waitBox(t, applyStarted); version != second.Version {
		t.Fatalf("second Apply got version %d", version)
	}
	streamTick <- time.Time{}
	streamTick <- time.Time{}
	backupTick <- time.Time{}
	backupTick <- time.Time{}
	synctest.Wait()
	stream.expectCount(t, 1)
	backup.expectCount(t, 2)
	firstStream.release <- nil
	secondStream, streamTick := stream.expect(t, first)
	synctest.Wait()
	stream.expectCount(t, 2)
	sendBox(t, applyRelease, applyErr)
	if report := waitBox(t, reports); report.Attempted != 2 || report.Applied != 1 || report.Error != applyErr.Error() {
		t.Errorf("report after failed Apply and pass = %+v", report)
	}
	waitBox(t, s.polls)
	sendBox(t, s.states, reply{http.StatusServiceUnavailable, ""})
	waitBox(t, s.polls)
	httpBefore, metadataBefore, signBefore, sleepBefore := requests.Load(), metadata.Load(), s.signs.Load(), sleeps.Load()
	backupErr := errors.New("backup failed")
	secondBackup.release <- backupErr
	thirdBackup, backupTick := backup.expect(t, second)
	streamErr := errors.New("stream\r\nfailed")
	secondStream.release <- streamErr
	synctest.Wait()
	stream.expectCount(t, 2)
	streamTick <- time.Time{}
	thirdStream, streamTick := stream.expect(t, second)
	synctest.Wait()
	if requests.Load() != httpBefore || metadata.Load() != metadataBefore || s.signs.Load() != signBefore || sleeps.Load() != sleepBefore {
		t.Errorf("worker activity changed HTTP calls, metadata calls, signatures or pauses: got %d, %d, %d, %d, want %d, %d, %d, %d",
			requests.Load(), metadata.Load(), s.signs.Load(), sleeps.Load(), httpBefore, metadataBefore, signBefore, sleepBefore)
	}

	third := activeBoxState(3, "")
	third.Backup = &protocol.Backup{Bucket: "test-bucket", Host: protocol.Credentials{AccessKeyID: "refreshed-session"}}
	s.state(t, third)
	if version := waitBox(t, applyStarted); version != third.Version {
		t.Fatalf("third Apply got version %d", version)
	}
	sendBox(t, applyRelease, nil)
	if report := waitBox(t, reports); report.Attempted != 3 || report.Applied != 3 || report.Error != "" {
		t.Errorf("report after a successful Apply and failed passes = %+v", report)
	}
	waitBox(t, s.polls)

	const digest = "new-daemon"
	s.state(t, activeBoxState(4, digest))
	if got := waitBox(t, installStarted); got != digest {
		t.Errorf("InstallDaemon got %q, want %q", got, digest)
	}
	thirdBackup.release <- nil
	thirdStream.release <- nil
	synctest.Wait()
	backup.expectCount(t, 3)
	stream.expectCount(t, 3)
	backupTick <- time.Time{}
	streamTick <- time.Time{}
	backup.expect(t, third)
	stream.expect(t, third)
	close(installRelease)
	waitBox(t, s.done)
	if s.result != nil {
		t.Errorf("Run = %v", s.result)
	}
	stream.expectCount(t, 4)
	backup.expectCount(t, 4)
	if len(stream.timers) != 0 || len(backup.timers) != 0 {
		t.Errorf("timers without a pass: stream %d, backup %d", len(stream.timers), len(backup.timers))
	}
	lines := strings.Split(strings.TrimSuffix(s.diagnostics.String(), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "accountsd: "+daemon.DiagApply+": "+applyErr.Error() ||
		!strings.HasPrefix(lines[1], "accountsd: "+daemon.DiagFetch+": ") ||
		lines[2] != "accountsd: "+daemon.DiagBackup+": "+backupErr.Error() ||
		lines[3] != "accountsd: "+daemon.DiagBackup+": stream  failed" {
		t.Errorf("diagnostics = %q", &s.diagnostics)
	}
}

// R-90IL-XP3U R-92YE-P8L8
func TestRunStartsBackupWorkersWithoutBoxAfterFirstApplyFails(t *testing.T) {
	s := newBoxScenario(t)
	s.cfg.Box, s.cfg.PeerUID = nil, nil
	stream, backup := newPassDriver(daemon.StreamTick), newPassDriver(daemon.BackupTick)
	s.cfg.After = func(interval time.Duration) <-chan time.Time {
		switch interval {
		case daemon.StreamTick:
			return stream.timer()
		case daemon.BackupTick:
			return backup.timer()
		default:
			t.Errorf("unexpected timer %v", interval)
			return nil
		}
	}
	s.cfg.Machine = machineProbe{
		Machine: s.cfg.Machine,
		apply:   func(context.Context, protocol.DesiredState) error { return errors.New("first apply failed") },
		stream:  stream.fake(t),
		backup:  backup.fake(t),
	}
	s.start(t)
	first := activeBoxState(1, "")
	waitBox(t, s.polls)
	s.state(t, first)
	stream.expect(t, first)
	backup.expect(t, first)
	waitBox(t, s.polls)
}

// R-92YE-P8L8
func TestBackupWorkersDoNotStartWithoutAnApply(t *testing.T) {
	for _, reason := range []string{"failed fetch", "self-update"} {
		t.Run(reason, func(t *testing.T) {
			s := newBoxScenario(t)
			s.cfg.Box, s.cfg.PeerUID = nil, nil
			var applies, passes, timers atomic.Int32
			unexpectedPass := func(context.Context, protocol.DesiredState) error {
				passes.Add(1)
				return nil
			}
			s.cfg.After = func(time.Duration) <-chan time.Time {
				timers.Add(1)
				return nil
			}
			s.cfg.Machine = machineProbe{
				apply: func(context.Context, protocol.DesiredState) error {
					applies.Add(1)
					return nil
				},
				inspect: func(context.Context, protocol.DesiredState) (protocol.Report, error) {
					return protocol.Report{}, nil
				},
				install: func(context.Context, io.Reader, string) error { return nil },
				stream:  unexpectedPass,
				backup:  unexpectedPass,
			}
			s.onBinary = func() *http.Response { return boxHTTPReply(http.StatusOK, "new daemon") }
			s.start(t)
			waitBox(t, s.polls)
			if reason == "failed fetch" {
				sendBox(t, s.states, reply{http.StatusBadGateway, ""})
				waitBox(t, s.polls)
				s.cancel()
			} else {
				s.state(t, activeBoxState(1, "new-daemon"))
			}
			waitBox(t, s.done)
			if applies.Load() != 0 || passes.Load() != 0 || timers.Load() != 0 {
				t.Errorf("without an applied state: got %d applies, %d passes and %d timers", applies.Load(), passes.Load(), timers.Load())
			}
		})
	}
}

// R-95E7-GS2M R-96M3-UJTB R-97U0-8BK0 R-9BHP-DMS3
func TestRunCancelsAndDrainsBothBackupPasses(t *testing.T) {
	for _, reason := range []string{"cancellation", "self-update"} {
		for _, last := range []string{"stream", "backup"} {
			t.Run(reason+"/"+last+" last", func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) { testRunCancelsAndDrainsBothBackupPasses(t, reason, last) })
			})
		}
	}
}

func testRunCancelsAndDrainsBothBackupPasses(t *testing.T, reason, last string) {
	s := newBoxScenario(t)
	ticks := make(chan chan time.Time, 4)
	s.cfg.After = func(time.Duration) <-chan time.Time {
		tick := make(chan time.Time, 1)
		ticks <- tick
		return tick
	}
	started := make(chan int64, 4)
	stopped := make(chan struct{}, 4)
	finished := make(chan struct{}, 4)
	releaseStream, releaseBackup := make(chan struct{}, 1), make(chan struct{}, 1)
	defer close(releaseStream)
	defer close(releaseBackup)
	pass := func(release <-chan struct{}) func(context.Context, protocol.DesiredState) error {
		return func(ctx context.Context, state protocol.DesiredState) error {
			started <- state.Version
			<-ctx.Done()
			stopped <- struct{}{}
			<-release
			finished <- struct{}{}
			return ctx.Err()
		}
	}
	installed := make(chan string, 1)
	s.cfg.Machine = machineProbe{
		Machine: s.cfg.Machine,
		stream:  pass(releaseStream),
		backup:  pass(releaseBackup),
		install: func(_ context.Context, _ io.Reader, digest string) error {
			installed <- digest
			return nil
		},
	}
	s.onBinary = func() *http.Response { return boxHTTPReply(http.StatusOK, "new daemon") }
	s.start(t)
	s.prime(t)
	for range 2 {
		if v := waitBox(t, started); v != 1 {
			t.Errorf("pass began with version %d, want 1", v)
		}
	}
	streamTick, backupTick := waitBox(t, ticks), waitBox(t, ticks)
	var wantErr error
	if reason == "self-update" {
		const digest = "new-daemon"
		s.state(t, activeBoxState(2, digest))
		if got := waitBox(t, installed); got != digest {
			t.Errorf("InstallDaemon got %q, want %q", got, digest)
		}
	} else {
		s.cancel()
		wantErr = context.Canceled
	}
	waitBox(t, stopped)
	waitBox(t, stopped)
	waitBox(t, s.listener.closed)
	streamTick <- time.Time{}
	backupTick <- time.Time{}
	synctest.Wait()
	select {
	case <-s.done:
		t.Error("Run returned with both passes still in progress")
	default:
	}
	firstRelease, lastRelease := releaseStream, releaseBackup
	if last == "stream" {
		firstRelease, lastRelease = lastRelease, firstRelease
	}
	firstRelease <- struct{}{}
	waitBox(t, finished)
	synctest.Wait()
	select {
	case <-s.done:
		t.Errorf("Run returned with %s still in progress", last)
	default:
	}
	lastRelease <- struct{}{}
	waitBox(t, s.done)
	if !errors.Is(s.result, wantErr) || len(finished) != 1 || len(started) != 0 || len(ticks) != 0 || s.diagnostics.Len() != 0 {
		t.Errorf("Run = %v, finished = %d, new passes = %d, new timers = %d, stderr = %q",
			s.result, len(finished)+1, len(started), len(ticks), &s.diagnostics)
	}
}

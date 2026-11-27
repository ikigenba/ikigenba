package access_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

const (
	host      = "host01"
	partition = "aws-cn"
	accountID = "123456789012"
)

var (
	epoch      = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	hosts      = settings.Hosts{Domain: "example.test", InstanceProfile: "hosts-role"}
	prefix     = protocol.HostPrefix(host, 7)
	caller     = cloud.Caller{Account: accountID, Arn: "arn:" + partition + ":sts::" + accountID + ":assumed-role/accounts/i-instance"}
	errRefused = errors.New("role refused")
)

type clock struct{ nanos atomic.Int64 }

func (c *clock) now() time.Time           { return epoch.Add(time.Duration(c.nanos.Load())) }
func (c *clock) advance(dt time.Duration) { c.nanos.Add(int64(dt)) }

type stsCall struct {
	ctx      context.Context
	role     cloud.RoleRequest
	isAssume bool
}

type fakeSTS struct {
	cloud.STS
	mu     sync.Mutex
	calls  []stsCall
	who    func(context.Context) (cloud.Caller, error)
	assume func(context.Context, cloud.RoleRequest) (cloud.Credentials, error)
}

func (f *fakeSTS) Identity(ctx context.Context) (cloud.Caller, error) {
	f.mu.Lock()
	f.calls = append(f.calls, stsCall{ctx: ctx})
	f.mu.Unlock()
	if f.who != nil {
		return f.who(ctx)
	}
	return caller, nil
}

func (f *fakeSTS) AssumeRole(ctx context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
	f.mu.Lock()
	f.calls = append(f.calls, stsCall{ctx: ctx, role: r, isAssume: true})
	f.mu.Unlock()
	if f.assume != nil {
		return f.assume(ctx, r)
	}
	return credentials(r.SessionName, epoch.Add(time.Hour)), nil
}

func (f *fakeSTS) snapshot() []stsCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func credentials(key string, expires time.Time) cloud.Credentials {
	return cloud.Credentials{
		AccessKeyID: "key-" + key, SecretAccessKey: "secret-" + key, SessionToken: "token-" + key,
		Expires: expires,
	}
}

func setup() (*access.Access, *fakeSTS, *clock) {
	fake := &fakeSTS{}
	c := &clock{}
	return access.New(access.Config{STS: fake, Hosts: hosts, Now: c.now}), fake, c
}

type sets struct {
	host     protocol.Credentials
	accounts []protocol.AccountCredentials
	err      error
}

func issue(ctx context.Context, a *access.Access, host, prefix string, slugs ...string) sets {
	h, ac, err := a.Issue(ctx, host, prefix, slugs)
	return sets{host: h, accounts: ac, err: err}
}

func sessions(calls []stsCall) []string {
	var names []string
	for _, call := range calls {
		if call.isAssume {
			names = append(names, call.role.SessionName)
		}
	}
	slices.Sort(names)
	return names
}

func checkIssued(t *testing.T, got sets) {
	t.Helper()
	if got.err != nil || got.host == (protocol.Credentials{}) {
		t.Fatalf("missing host credentials or unexpected error %v", got.err)
	}
	for _, account := range got.accounts {
		if account.Credentials == (protocol.Credentials{}) {
			t.Errorf("no credentials for %s", account.Account)
		}
	}
}

// R-WGWC-PVFZ
var (
	_ func(access.Config) *access.Access                                                                                           = access.New
	_ func(*access.Access, context.Context, string, string, []string) (protocol.Credentials, []protocol.AccountCredentials, error) = (*access.Access).Issue
	_ func(*access.Access) int                                                                                                     = (*access.Access).Cached
	_ func(string, string, string) string                                                                                          = access.Policy
	_ func(string, string, settings.Hosts) string                                                                                  = access.RoleARN
	_ func(string, string) string                                                                                                  = access.SessionName

	_ = struct {
		STS   cloud.STS
		Hosts settings.Hosts
		Now   func() time.Time
	}(access.Config{})
)

// R-WGWC-PVFZ
func TestAccessAPI(t *testing.T) {
	cfg := access.Config{STS: &fakeSTS{}, Hosts: hosts, Now: func() time.Time { return epoch }}
	a := access.New(cfg)
	var issuer interface {
		Issue(context.Context, string, string, []string) (protocol.Credentials, []protocol.AccountCredentials, error)
		Cached() int
	} = a
	if h, entries, err := issuer.Issue(t.Context(), host, prefix, []string{"acme"}); err != nil || h == (protocol.Credentials{}) || len(entries) != 1 || issuer.Cached() != 2 {
		t.Fatalf("could not use Access via its declared API, err = %v", err)
	}
	if access.Policy("b", partition, prefix) == "" || access.RoleARN(partition, accountID, hosts) == "" || access.SessionName(host, "") == "" {
		t.Fatal("could not use access helpers via their declared API")
	}
	for field := range reflect.TypeFor[access.Access]().Fields() {
		if field.IsExported() {
			t.Errorf("Access exposes field %s", field.Name)
		}
	}
}

// R-X7Q5-4TR9
func TestNewIsIdle(t *testing.T) {
	f := &fakeSTS{}
	var nowCalls int
	a := access.New(access.Config{STS: f, Hosts: hosts, Now: func() time.Time {
		nowCalls++
		return epoch
	}})
	if nowCalls != 0 || len(f.snapshot()) != 0 {
		t.Fatal("New consulted STS or the clock")
	}
	checkIssued(t, issue(t.Context(), a, host, prefix))
	if nowCalls == 0 || len(f.snapshot()) == 0 {
		t.Fatal("Issue did not consult STS and the clock")
	}
}

// R-WO7R-0HW5
func TestAccountEntries(t *testing.T) {
	for _, status := range []string{"issued", "identity fails", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			a, f, _ := setup()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch status {
			case "identity fails":
				f.who = func(context.Context) (cloud.Caller, error) { return cloud.Caller{}, errRefused }
			case "cancelled":
				cancel()
			}
			slugs := []string{"zeta", "acme", "zeta", "", "ZETA", "é"}
			got := issue(ctx, a, host, prefix, slugs...)
			var names []string
			for _, entry := range got.accounts {
				names = append(names, entry.Account)
			}
			if want := []string{"ZETA", "acme", "zeta", "é"}; !slices.Equal(names, want) {
				t.Errorf("account entries = %v, want %v", names, want)
			}
		})
	}
}

// R-WQNJ-S1DJ R-WT3C-JKUX
func TestRequestsAndAnswers(t *testing.T) {
	a, f, _ := setup()
	hostPrefix := protocol.HostPrefix(host, 12)
	got := issue(t.Context(), a, host, hostPrefix, "zeta", "acme", "zeta")
	checkIssued(t, got)
	if got.host != protocol.Credentials(credentials(access.SessionName(host, ""), epoch.Add(time.Hour))) {
		t.Error("host credentials differ from the STS answer")
	}
	for _, entry := range got.accounts {
		if entry.Credentials != protocol.Credentials(credentials(access.SessionName(host, entry.Account), epoch.Add(time.Hour))) {
			t.Errorf("credentials for %s differ from the STS answer", entry.Account)
		}
	}
	want := map[string]string{
		access.SessionName(host, ""):     hostPrefix,
		access.SessionName(host, "acme"): protocol.AccountPrefix("acme"),
		access.SessionName(host, "zeta"): protocol.AccountPrefix("zeta"),
	}
	for _, call := range f.snapshot() {
		if !call.isAssume {
			continue
		}
		name := call.role.SessionName
		rolePrefix, ok := want[name]
		expect := cloud.RoleRequest{
			RoleARN: access.RoleARN(partition, accountID, hosts), SessionName: name,
			Policy: access.Policy(hosts.BackupBucket(), partition, rolePrefix), Duration: cloud.RoleSessionDuration,
		}
		if !ok || call.role != expect {
			t.Errorf("unexpected AssumeRole request %+v", call.role)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("missing AssumeRole requests %v", want)
	}
}

// R-WRVG-5T48
func TestEmptyHostPrefix(t *testing.T) {
	a, f, _ := setup()
	got := issue(t.Context(), a, host, "", "acme")
	if got.err != nil || got.host != (protocol.Credentials{}) || len(got.accounts) != 1 ||
		got.accounts[0].Credentials == (protocol.Credentials{}) {
		t.Fatalf("unexpected sets for empty host prefix, err = %v", got.err)
	}
	if got := sessions(f.snapshot()); !slices.Equal(got, []string{access.SessionName(host, "acme")}) {
		t.Errorf("AssumeRole sessions = %v, want only the account set", got)
	}
}

// R-WVJ5-B4CB
func TestCacheReuse(t *testing.T) {
	a, f, c := setup()
	f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		return credentials(r.SessionName+"/"+c.now().String(), c.now().Add(time.Hour)), nil
	}

	first := issue(t.Context(), a, host, prefix, "acme")
	checkIssued(t, first)
	c.advance(14 * time.Minute)
	hit := issue(t.Context(), a, host, prefix, "acme")
	checkIssued(t, hit)
	if hit.host != first.host || !slices.Equal(hit.accounts, first.accounts) || len(sessions(f.snapshot())) != 2 {
		t.Fatal("fresh sets were not reused")
	}

	changedPrefix := issue(t.Context(), a, host, protocol.HostPrefix(host, 8), "acme")
	checkIssued(t, changedPrefix)
	if changedPrefix.host == first.host || !slices.Equal(changedPrefix.accounts, first.accounts) || len(sessions(f.snapshot())) != 3 {
		t.Fatal("changing only the host prefix did not refresh only the host set")
	}

	other := issue(t.Context(), a, "host02", prefix, "acme")
	checkIssued(t, other)
	if other.host == first.host || slices.Equal(other.accounts, first.accounts) || len(sessions(f.snapshot())) != 5 {
		t.Fatal("another host reused cached sets")
	}

	c.advance(time.Minute)
	due := issue(t.Context(), a, host, prefix, "acme")
	checkIssued(t, due)
	if due.host == first.host || slices.Equal(due.accounts, first.accounts) || len(sessions(f.snapshot())) != 7 {
		t.Fatal("sets aged exactly ReissueAfter were not renewed")
	}
}

// R-WVJ5-B4CB
func TestNearExpiryRenewsWithinReuseAge(t *testing.T) {
	a, f, c := setup()
	f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		return credentials(r.SessionName, c.now().Add(20*time.Minute)), nil
	}
	first := issue(t.Context(), a, host, prefix)
	checkIssued(t, first)
	c.advance(4 * time.Minute)
	hit := issue(t.Context(), a, host, prefix)
	checkIssued(t, hit)
	if hit.host != first.host || len(sessions(f.snapshot())) != 1 {
		t.Fatal("set with more than ReissueAfter left was not reused")
	}
	c.advance(time.Minute)
	due := issue(t.Context(), a, host, prefix)
	checkIssued(t, due)
	if due.host == first.host || len(sessions(f.snapshot())) != 2 {
		t.Fatal("set with exactly ReissueAfter left was not renewed")
	}
}

// R-WWR1-OW30 R-X5AC-DA9V
func TestPartialFailureAndRetry(t *testing.T) {
	a, f, _ := setup()
	var fail atomic.Bool
	fail.Store(true)
	f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		if r.SessionName == access.SessionName(host, "acme") && fail.Load() {
			return cloud.Credentials{}, errRefused
		}
		return credentials(r.SessionName, epoch.Add(time.Hour)), nil
	}
	got := issue(t.Context(), a, host, prefix, "zeta", "acme")
	if got.err == nil || got.host == (protocol.Credentials{}) || len(got.accounts) != 2 ||
		got.accounts[0].Credentials != (protocol.Credentials{}) || got.accounts[1].Credentials == (protocol.Credentials{}) {
		t.Fatalf("failure did not preserve independent sets, err = %v", got.err)
	}
	for _, c := range []protocol.Credentials{got.host, got.accounts[1].Credentials} {
		for _, secret := range []string{c.SecretAccessKey, c.SessionToken} {
			if strings.Contains(got.err.Error(), secret) {
				t.Fatal("Issue error reveals secret credential material")
			}
		}
	}
	firstCalls := len(f.snapshot())
	fail.Store(false)
	checkIssued(t, issue(t.Context(), a, host, prefix, "zeta", "acme"))
	if got := sessions(f.snapshot()[firstCalls:]); !slices.Equal(got, []string{access.SessionName(host, "acme")}) {
		t.Errorf("AssumeRole sessions = %v, want only the failed account", got)
	}
}

// R-WWR1-OW30
func TestRenewalFailureUsesUnexpiredSet(t *testing.T) {
	a, f, c := setup()
	var fail atomic.Bool
	f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		if fail.Load() {
			return cloud.Credentials{}, errRefused
		}
		return credentials(r.SessionName, epoch.Add(time.Hour)), nil
	}
	first := issue(t.Context(), a, host, prefix)
	checkIssued(t, first)
	fail.Store(true)
	c.advance(access.ReissueAfter)
	for range 2 {
		got := issue(t.Context(), a, host, prefix)
		if got.host != first.host {
			t.Fatal("reissue failure did not return the unexpired cached set")
		}
	}
	if got := len(sessions(f.snapshot())); got != 3 {
		t.Errorf("AssumeRole called %d times, want initial issue and two failed renewals", got)
	}
	c.advance(time.Hour - access.ReissueAfter)
	if got := issue(t.Context(), a, host, prefix); got.host != (protocol.Credentials{}) {
		t.Fatal("reissue failure returned a set at its expiration time")
	}
}

// R-666W-VELB R-X42F-ZIJ6
func TestCacheCountsAndDrops(t *testing.T) {
	for _, elapsed := range []time.Duration{59 * time.Minute, time.Hour, 61 * time.Minute} {
		t.Run(elapsed.String(), func(t *testing.T) {
			a, f, c := setup()
			f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
				if r.SessionName == access.SessionName(host, "failed") {
					return cloud.Credentials{}, errRefused
				}
				return credentials(r.SessionName, epoch.Add(10*time.Minute)), nil
			}
			if a.Cached() != 0 {
				t.Fatal("unused cache has entries")
			}
			issue(t.Context(), a, host, prefix, "acme", "zeta", "failed")
			if got := a.Cached(); got != 3 {
				t.Fatalf("Cached after one failure and three successes = %d, want 3", got)
			}
			c.advance(elapsed)
			if got := a.Cached(); got != 3 {
				t.Fatalf("Cached after entries expired without a new Issue = %d, want 3", got)
			}
			issue(t.Context(), a, "host02", protocol.HostPrefix("host02", 9), "acme")
			want := 2
			if elapsed < time.Hour {
				want += 3
			}
			if got := a.Cached(); got != want {
				t.Fatalf("Cached after a new Issue = %d, want %d", got, want)
			}
			if elapsed >= time.Hour {
				before := len(f.snapshot())
				issue(t.Context(), a, host, prefix, "acme", "zeta")
				if got := len(sessions(f.snapshot()[before:])); got != 3 {
					t.Errorf("AssumeRole calls for dropped entries = %d, want 3", got)
				}
			}
		})
	}
}

// R-X42F-ZIJ6
func TestCacheUsesLastRequestTime(t *testing.T) {
	a, _, c := setup()
	checkIssued(t, issue(t.Context(), a, host, prefix, "acme", "zeta"))
	c.advance(14 * time.Minute)
	checkIssued(t, issue(t.Context(), a, host, prefix, "acme"))
	c.advance(46 * time.Minute)
	issue(t.Context(), a, "host02", "", "beta")
	if got := a.Cached(); got != 3 {
		t.Errorf("Cached = %d, want host, acme, and beta", got)
	}
	c.advance(14 * time.Minute)
	issue(t.Context(), a, host, "", "acme")
	if got := a.Cached(); got != 2 {
		t.Errorf("Cached = %d, want acme and beta at the drop boundary", got)
	}
}

// R-62J7-Q3D8
func TestOneIssueDeadline(t *testing.T) {
	a, f, _ := setup()
	checkIssued(t, issue(t.Context(), a, host, prefix, fiftyAccounts()...))
	end := time.Now()
	calls := f.snapshot()
	if len(calls) != 52 {
		t.Fatalf("STS received %d calls, want identity and 51 sets", len(calls))
	}
	first, ok := calls[0].ctx.Deadline()
	if !ok || first.After(end.Add(access.IssueBudget)) {
		t.Fatalf("first deadline %v outside one Issue budget", first)
	}
	for _, call := range calls[1:] {
		if got, ok := call.ctx.Deadline(); !ok || !got.Equal(first) {
			t.Errorf("deadline for %s = %v, want %v", call.role.SessionName, got, first)
		}
	}
}

func fiftyAccounts() []string {
	slugs := make([]string, 50)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("a%02d", i)
	}
	return slugs
}

// R-64Z0-HMUM
func TestIssueUsesUpToEightConcurrentAssumes(t *testing.T) {
	a, f, _ := setup()
	started := make(chan chan struct{}, 51)
	stop := make(chan struct{})
	defer close(stop)
	var countMu sync.Mutex
	var active, peak int
	counts := func() (int, int) {
		countMu.Lock()
		defer countMu.Unlock()
		return active, peak
	}
	f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		countMu.Lock()
		active++
		peak = max(peak, active)
		countMu.Unlock()
		release := make(chan struct{})
		started <- release
		select {
		case <-release:
		case <-stop:
		}
		countMu.Lock()
		active--
		countMu.Unlock()
		return credentials(r.SessionName, epoch.Add(time.Hour)), nil
	}
	done := make(chan sets, 1)
	go func() { done <- issue(t.Context(), a, host, prefix, fiftyAccounts()...) }()
	var held []chan struct{}
	waitForStart := func() {
		t.Helper()
		select {
		case release := <-started:
			held = append(held, release)
		case got := <-done:
			t.Fatalf("Issue returned before the next AssumeRole started, err = %v", got.err)
		}
	}
	for range access.IssueParallel {
		waitForStart()
	}
	for range 51 - access.IssueParallel {
		close(held[0])
		held = held[1:]
		waitForStart()
		if got, _ := counts(); got != access.IssueParallel {
			t.Fatalf("AssumeRole holds %d calls with entries outstanding, want %d", got, access.IssueParallel)
		}
	}
	for _, release := range held {
		close(release)
	}
	got := <-done
	checkIssued(t, got)
	if _, largest := counts(); largest != access.IssueParallel || len(got.accounts) != 50 {
		t.Errorf("received %d accounts with peak %d AssumeRole calls, want 50 accounts and peak %d",
			len(got.accounts), largest, access.IssueParallel)
	}
}

// R-62J7-Q3D8 R-63R4-3V3X
func TestCancelIssueReturnsWhileAssumeRoleIsBlocked(t *testing.T) {
	a, f, _ := setup()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	acmeStarted := make(chan context.Context, 1)
	lastWave := make(chan struct{}, 2)
	f.assume = func(ctx context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		switch r.SessionName {
		case access.SessionName(host, "acme"):
			acmeStarted <- ctx
			<-release
		case access.SessionName(host, "zulu"), access.SessionName(host, "zulu2"):
			lastWave <- struct{}{}
			<-ctx.Done()
		case access.SessionName(host, ""), access.SessionName(host, "zeta"):
		default:
			<-ctx.Done()
		}
		return credentials(r.SessionName, epoch.Add(time.Hour)), nil
	}
	done := make(chan sets, 1)
	slugs := []string{"acme", "b01", "b02", "b03", "b04", "b05", "zeta", "zulu", "zulu2"}
	go func() { done <- issue(ctx, a, host, prefix, slugs...) }()

	var stsCtx context.Context
	select {
	case stsCtx = <-acmeStarted:
	case got := <-done:
		t.Fatalf("Issue returned before acme started, err = %v", got.err)
	}
	for range 2 {
		select {
		case <-lastWave:
		case got := <-done:
			t.Fatalf("Issue returned before the last wave started, err = %v", got.err)
		}
	}
	// Only host and zeta could have freed the two slots for the last wave.
	cancel()
	select {
	case <-stsCtx.Done():
	default:
		t.Fatal("caller cancellation did not end the STS context")
	}
	select {
	case got := <-done:
		if got.host == (protocol.Credentials{}) || len(got.accounts) != len(slugs) ||
			got.accounts[0].Credentials != (protocol.Credentials{}) || got.accounts[6].Credentials == (protocol.Credentials{}) {
			t.Fatalf("Issue did not preserve completed sets and leave the blocked set zero, err = %v", got.err)
		}
	case <-time.After(access.IssueBudget):
		t.Fatal("Issue waited for acme's AssumeRole even though its context had ended")
	}
}

// R-63R4-3V3X
func TestCancelIssueReturnsWhileIdentityIsBlocked(t *testing.T) {
	a, f, _ := setup()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	f.who = func(context.Context) (cloud.Caller, error) {
		close(started)
		<-release
		return caller, nil
	}
	done := make(chan sets, 1)
	go func() { done <- issue(ctx, a, host, prefix, "acme") }()
	select {
	case <-started:
	case got := <-done:
		t.Fatalf("Issue returned before Identity started, err = %v", got.err)
	}
	cancel()
	select {
	case got := <-done:
		if got.host != (protocol.Credentials{}) || len(got.accounts) != 1 ||
			got.accounts[0].Credentials != (protocol.Credentials{}) {
			t.Fatalf("credentials issued before identity was known, err = %v", got.err)
		}
	case <-time.After(access.IssueBudget):
		t.Fatal("Issue waited for Identity even though its context had ended")
	}
}

// R-X6I8-R20K
func TestSharedAccess(t *testing.T) {
	a, f, _ := setup()
	checkIssued(t, issue(t.Context(), a, host, prefix, "acme"))

	const workers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			<-start
			name := fmt.Sprintf("host%02d", i+2)
			ownPrefix := protocol.HostPrefix(name, 1)
			wantHost := protocol.Credentials(credentials(access.SessionName(name, ""), epoch.Add(time.Hour)))
			wantAcme := protocol.Credentials(credentials(access.SessionName(name, "acme"), epoch.Add(time.Hour)))
			for range 2 {
				got := issue(t.Context(), a, name, ownPrefix, "acme")
				if got.err != nil || got.host != wantHost || len(got.accounts) != 1 ||
					got.accounts[0].Credentials != wantAcme {
					t.Errorf("issue for %s did not return its own sets, err = %v", name, got.err)
				}
				if count := a.Cached(); count < 2 || count > 2*(workers+1) {
					t.Errorf("Cached = %d outside the number of possible successes", count)
				}
			}
		})
	}
	close(start)
	wg.Wait()
	if count := a.Cached(); count != 2*(workers+1) {
		t.Errorf("Cached = %d, want %d", count, 2*(workers+1))
	}
	if got := len(sessions(f.snapshot())); got != 2*(workers+1) {
		t.Errorf("AssumeRole calls = %d, want %d", got, 2*(workers+1))
	}
}

// R-X5AC-DA9V
func TestLocalIssueErrorTextCannotMatchCredentialMaterial(t *testing.T) {
	a, f, _ := setup()
	good := credentials("good", epoch.Add(time.Hour))
	good.SecretAccessKey = "STS answered no credentials"
	good.SessionToken = context.Canceled.Error()
	f.assume = func(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
		if r.SessionName == access.SessionName(host, "good") {
			return good, nil
		}
		return cloud.Credentials{}, nil
	}
	check := func(err error) {
		t.Helper()
		if err == nil {
			t.Fatal("expected an issuance error")
		}
		if strings.Contains(err.Error(), good.SecretAccessKey) || strings.Contains(err.Error(), good.SessionToken) {
			t.Fatal("local issuance error matches previously issued credential material")
		}
	}
	issued := issue(t.Context(), a, host, "", "blank", "good")
	if len(issued.accounts) != 2 || issued.accounts[1].Credentials != protocol.Credentials(good) {
		t.Fatal("credential markers did not reach the issued set")
	}
	check(issued.err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	failed := issue(ctx, a, host, "", "blank")
	check(failed.err)
	if !errors.Is(failed.err, context.Canceled) {
		t.Fatal("local issuance error did not preserve the cancellation cause")
	}
}

// R-63R4-3V3X R-WWR1-OW30
func TestCancelRenewalReturnsUnexpiredSetAndRetries(t *testing.T) {
	a, f, c := setup()
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	var attempts atomic.Int32
	old := credentials("initial", epoch.Add(time.Hour))
	fresh := credentials("renewed", epoch.Add(2*time.Hour))
	f.assume = func(context.Context, cloud.RoleRequest) (cloud.Credentials, error) {
		switch attempts.Add(1) {
		case 1:
			return old, nil
		case 2:
			close(started)
			<-release
		}
		return fresh, nil
	}
	first := issue(t.Context(), a, host, "", "acme")
	if first.err != nil || len(first.accounts) != 1 || first.accounts[0].Credentials != protocol.Credentials(old) {
		t.Fatalf("initial credential issue failed, err = %v", first.err)
	}

	c.advance(access.ReissueAfter)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan sets, 1)
	go func() { done <- issue(ctx, a, host, "", "acme") }()
	select {
	case <-started:
	case got := <-done:
		t.Fatalf("Issue returned before renewal started, err = %v", got.err)
	}
	cancel()
	select {
	case got := <-done:
		if !slices.Equal(got.accounts, first.accounts) {
			t.Fatal("cancelled renewal did not retain the unexpired cached set")
		}
	case <-time.After(access.IssueBudget):
		t.Fatal("Issue waited for the blocked renewal after cancellation")
	}

	retried := issue(t.Context(), a, host, "", "acme")
	if retried.err != nil || len(retried.accounts) != 1 || retried.accounts[0].Credentials != protocol.Credentials(fresh) ||
		attempts.Load() != 3 {
		t.Fatalf("next Issue did not retry the cancelled renewal, err = %v", retried.err)
	}
}

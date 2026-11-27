package hostauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const (
	region     = "us-east-2"
	role       = "hosts-role"
	accountID  = "000000000000"
	instanceID = "i-0abc12345def67890"
	binding    = "accounts.example:443"
	dateLayout = "20060102T150405Z"
)

var fixtureTime = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func caller() cloud.Caller {
	return cloud.Caller{
		Account: accountID,
		Arn:     "arn:aws:sts::" + accountID + ":assumed-role/" + role + "/" + instanceID,
		UserID:  "AROAEXAMPLE:" + instanceID,
	}
}

type stsFake struct {
	callerOf func(context.Context, string, string) (cloud.Caller, error)
	account  func(context.Context) (string, error)
}

func (s stsFake) CallerOf(ctx context.Context, raw, bound string) (cloud.Caller, error) {
	if s.callerOf == nil {
		return caller(), nil
	}
	return s.callerOf(ctx, raw, bound)
}

func (s stsFake) Account(ctx context.Context) (string, error) {
	if s.account == nil {
		return accountID, nil
	}
	return s.account(ctx)
}

func (stsFake) Identity(context.Context) (cloud.Caller, error) {
	panic("unexpected backup identity call")
}

func (stsFake) AssumeRole(context.Context, cloud.RoleRequest) (cloud.Credentials, error) {
	panic("unexpected backup assume-role call")
}

type presignFunc func(context.Context, string, time.Duration) (string, error)

func (f presignFunc) PresignCallerIdentity(ctx context.Context, bound string, expires time.Duration) (string, error) {
	return f(ctx, bound, expires)
}

func rawURL(now time.Time, code int, edit func(url.Values)) string {
	values := url.Values{
		"Action":              {"GetCallerIdentity"},
		"Version":             {"2011-06-15"},
		"X-Amz-Algorithm":     {"AWS4-HMAC-SHA256"},
		"X-Amz-SignedHeaders": {"host;" + strings.ToLower(BindingHeader)},
		"X-Amz-Expires":       {"60"},
		"X-Amz-Date":          {now.Format(dateLayout)},
		"X-Amz-Signature":     {fmt.Sprintf("%064x", code)},
	}
	if edit != nil {
		edit(values)
	}
	return "https://sts." + region + ".amazonaws.com/?" + values.Encode()
}

func encode(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func header(now time.Time, code int) string {
	return encode(rawURL(now, code, nil))
}

func reorderQuery(raw string) string {
	prefix, query, _ := strings.Cut(raw, "?")
	first, remaining, _ := strings.Cut(query, "&")
	return prefix + "?" + remaining + "&" + first
}

func verifier(sts cloud.STS, now func() time.Time) *Verifier {
	return NewVerifier(VerifierConfig{region, role, func() (string, error) { return binding, nil }, sts, now})
}

func assertVerify(ctx context.Context, t *testing.T, v *Verifier, input string, want error) {
	t.Helper()
	id, err := v.Verify(ctx, input)
	wantID := instanceID
	if want != nil {
		wantID = ""
	}
	if !errors.Is(err, want) || id != wantID {
		t.Fatalf("Verify = (%q, %v), want (%q, %v)", id, err, wantID, want)
	}
}

// R-4HDB-U1RB, R-4IL8-7TI0, R-AREW-TGZ5, R-4L10-ZCZE.
func TestHeaderAndContract(t *testing.T) {
	_ = struct {
		header func(context.Context, cloud.Presigner, string) (string, error)
		new    func(VerifierConfig) *Verifier
		verify func(*Verifier, context.Context, string) (string, error)
	}{Header, NewVerifier, (*Verifier).Verify}
	if HeaderName != "X-Accountsd-Identity" || BindingHeader != cloud.BindingHeader ||
		STSTimeout != cloud.CallerTimeout ||
		MaxHeaderBytes != 8192 || PresignExpires != 60*time.Second || MaxExpires != 300 ||
		Freshness != 5*time.Minute || MaxSkew != time.Minute || ReplayCapacity != 10000 || VerifyRate != 20 || VerifyBurst != 40 {
		t.Fatal("unexpected verifier constants")
	}
	seen := make(map[error]bool)
	for _, err := range []error{ErrMalformed, ErrStale, ErrReplayed, ErrRejected, ErrUnavailable, ErrBusy} {
		if err == nil || seen[err] {
			t.Fatalf("error is nil or duplicated: %v", err)
		}
		seen[err] = true
	}
	presignErr := errors.New("fixture presign failure")
	for _, failure := range []error{nil, presignErr} {
		raw := rawURL(fixtureTime, 1, nil)
		calls := 0
		ctx := t.Context()
		p := presignFunc(func(gotCtx context.Context, gotBinding string, gotExpires time.Duration) (string, error) {
			calls++
			if gotCtx != ctx || gotBinding != binding || gotExpires != PresignExpires {
				t.Fatalf("wrong presign input: %v, %q, %v", gotCtx, gotBinding, gotExpires)
			}
			return raw, failure
		})
		got, err := Header(ctx, p, binding)
		want := encode(raw)
		if failure != nil {
			want = ""
		}
		if got != want || !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("Header = (%q, %v), want (%q, %v); %d presign calls", got, err, want, failure, calls)
		}
	}
}

// R-4M8X-D4Q3, R-G53V-62G6, R-G6BR-JU6V.
func TestRefuseBeforeBindingAndSTS(t *testing.T) {
	cfg := VerifierConfig{Region: region, Role: role, Now: func() time.Time { return fixtureTime }}
	cfg.Binding = func() (string, error) {
		t.Fatal("invalid request reached Binding")
		return "", nil
	}
	v := NewVerifier(cfg)
	raw := rawURL(fixtureTime, 0xabcdef, nil)
	for _, bad := range []string{
		"", "?", strings.Repeat("A", MaxHeaderBytes+1),
		encode("%zz"),
		encode(strings.Replace(raw, "https:", "http:", 1)),
		encode(strings.Replace(raw, "https://", "https://user@", 1)),
		encode(strings.Replace(raw, ".com/", ".com.evil.example/", 1)),
		encode(strings.Replace(raw, ".com/", ".com:443/", 1)),
		encode(strings.Replace(raw, ".com/", ".com/elsewhere", 1)),
		encode(raw + "#fragment"), encode(raw + "#"), encode(raw + "&Action=GetCallerIdentity"),
	} {
		assertVerify(t.Context(), t, v, bad, ErrMalformed)
	}
	for _, tt := range []struct{ name, value string }{
		{"Action", "Other"}, {"Version", "2020-01-01"}, {"X-Amz-Algorithm", "Other"},
		{"X-Amz-SignedHeaders", "host"}, {"X-Amz-SignedHeaders", strings.ToLower(BindingHeader)},
		{"X-Amz-Expires", "0"}, {"X-Amz-Expires", "301"}, {"X-Amz-Expires", "+1"},
		{"X-Amz-Date", "2026-11-17"}, {"X-Amz-Signature", fmt.Sprintf("%064X", 0xabcdef)},
		{"X-Amz-Signature", "abc"},
	} {
		altered := rawURL(fixtureTime, 0xabcdef, func(q url.Values) { q.Set(tt.name, tt.value) })
		assertVerify(t.Context(), t, v, encode(altered), ErrMalformed)
		missing := rawURL(fixtureTime, 0xabcdef, func(q url.Values) { q.Del(tt.name) })
		assertVerify(t.Context(), t, v, encode(missing), ErrMalformed)
		duplicated := rawURL(fixtureTime, 0xabcdef, func(q url.Values) { q.Add(tt.name, q.Get(tt.name)) })
		assertVerify(t.Context(), t, v, encode(duplicated), ErrMalformed)
	}
	assertVerify(t.Context(), t, v, header(fixtureTime.Add(-Freshness-time.Second), 1), ErrStale)
	assertVerify(t.Context(), t, v, header(fixtureTime.Add(MaxSkew+time.Second), 2), ErrStale)
	boundary := verifier(stsFake{}, cfg.Now)
	assertVerify(t.Context(), t, boundary, header(fixtureTime.Add(-Freshness), 1), nil)
	assertVerify(t.Context(), t, boundary, header(fixtureTime.Add(MaxSkew), 2), nil)
}

// R-G8RK-BDO9, R-K9KD-L96S, R-L59R-JAGU, R-4X80-T2EC, R-4YFX-6U51, R-LA5D-2DFM.
func TestSTSOutcomesAndReplayOrder(t *testing.T) {
	raw := rawURL(fixtureTime, 1, func(q url.Values) { q.Set(BindingHeader, "elsewhere.example") })
	now := fixtureTime
	bindingCalls, callerCalls, accountCalls := 0, 0, 0
	accountErr := errors.New("fixture account lookup failed")
	v := NewVerifier(VerifierConfig{
		Region: region,
		Role:   role,
		Binding: func() (string, error) {
			bindingCalls++
			return binding, nil
		},
		STS: stsFake{
			callerOf: func(_ context.Context, gotRaw, gotBinding string) (cloud.Caller, error) {
				callerCalls++
				if gotBinding != binding || (callerCalls <= 3 && gotRaw != raw) {
					t.Fatalf("CallerOf received URL %q, binding %q", gotRaw, gotBinding)
				}
				switch callerCalls {
				case 1:
					return cloud.Caller{}, fmt.Errorf("fixture wrapper: %w", &cloud.Error{Class: cloud.ClassTransient})
				case 2:
					return cloud.Caller{}, &cloud.Error{Class: cloud.ClassRefused}
				default:
					identity := caller()
					if callerCalls == 4 {
						identity.Account = "999999999999"
					}
					return identity, nil
				}
			},
			account: func(context.Context) (string, error) {
				accountCalls++
				if accountCalls == 1 {
					return "", accountErr
				}
				return accountID, nil
			},
		},
		Now: func() time.Time { return now },
	})
	ctx := t.Context()
	assertVerify(ctx, t, v, encode(raw), ErrUnavailable)
	assertVerify(ctx, t, v, encode(raw), ErrRejected)
	assertVerify(ctx, t, v, encode(raw), ErrUnavailable)

	assertVerify(ctx, t, v, encode(reorderQuery(raw)), ErrReplayed)
	assertVerify(ctx, t, v, header(now, 2), ErrRejected)
	assertVerify(ctx, t, v, header(now, 2), ErrReplayed)
	assertVerify(ctx, t, v, header(now, 3), nil)
	assertVerify(ctx, t, v, header(now.Add(-Freshness-time.Second), 3), ErrStale)

	now = now.Add(Freshness + time.Second)
	assertVerify(ctx, t, v, encode(raw), ErrStale)
	if bindingCalls != 5 || callerCalls != 5 || accountCalls != 2 {
		t.Fatalf("calls to Binding, CallerOf, Account = %d, %d, %d; want 5, 5, 2", bindingCalls, callerCalls, accountCalls)
	}
}

// R-L8XG-OLOX.
func TestReplayExpiresWhileCallerOfRuns(t *testing.T) {
	now := fixtureTime
	calls := 0
	v := verifier(stsFake{callerOf: func(context.Context, string, string) (cloud.Caller, error) {
		calls++
		if calls == 1 {
			now = now.Add(Freshness + time.Second)
		}
		return caller(), nil
	}}, func() time.Time { return now })
	assertVerify(t.Context(), t, v, header(fixtureTime, 1), nil)

	// Move the clock back so admission itself cannot expire the original date.
	now = fixtureTime
	assertVerify(t.Context(), t, v, header(now.Add(time.Second), 1), nil)
	if calls != 2 {
		t.Fatalf("CallerOf calls = %d after the age-out during the first call, want 2", calls)
	}
}

// R-K9KD-L96S, R-KAS9-Z0XH, R-L8XG-OLOX.
func TestReplayExpiresByAcceptedDate(t *testing.T) {
	now := fixtureTime
	calls := 0
	v := verifier(stsFake{callerOf: func(context.Context, string, string) (cloud.Caller, error) {
		calls++
		return caller(), nil
	}}, func() time.Time { return now })
	ctx := t.Context()
	original := rawURL(fixtureTime, 1, nil)
	assertVerify(ctx, t, v, encode(original), nil)
	assertVerify(ctx, t, v, encode(reorderQuery(original)), ErrReplayed)

	now = fixtureTime.Add(Freshness)
	changed := header(now, 1)
	assertVerify(ctx, t, v, changed, ErrReplayed)
	if calls != 1 {
		t.Fatalf("CallerOf called %d times before the accepted date expired", calls)
	}
	now = now.Add(time.Second)
	assertVerify(ctx, t, v, changed, nil)
	if calls != 2 {
		t.Fatalf("CallerOf called %d times after the accepted date expired", calls)
	}
}

// R-50VP-YDMF.
func TestCallerShape(t *testing.T) {
	for _, tt := range []struct {
		edit func(*cloud.Caller)
		err  error
	}{
		{edit: func(c *cloud.Caller) { c.Arn = strings.Replace(c.Arn, role, "other-role", 1) }, err: ErrRejected},
		{edit: func(c *cloud.Caller) { c.Arn = strings.Replace(c.Arn, instanceID, "bob", 1) }, err: ErrRejected},
		{edit: func(c *cloud.Caller) { c.Arn = "arn:aws:iam::" + accountID + ":user/fixture" }, err: ErrRejected},
		{edit: func(c *cloud.Caller) { c.UserID = "AROAEXAMPLE:i-abcdef00" }, err: ErrRejected},
		{edit: func(c *cloud.Caller) { c.UserID = ":" + instanceID }, err: ErrRejected},
		{edit: func(c *cloud.Caller) { c.UserID = "AROAEXAMPLE:other:" + instanceID }, err: ErrRejected},
		{edit: func(*cloud.Caller) {}},
	} {
		v := verifier(stsFake{callerOf: func(context.Context, string, string) (cloud.Caller, error) {
			c := caller()
			tt.edit(&c)
			return c, nil
		}}, func() time.Time { return fixtureTime })
		assertVerify(t.Context(), t, v, header(fixtureTime, 1), tt.err)
	}
}

// R-KAS9-Z0XH, R-L41V-5IQ5, R-53BI-PX3T.
func TestSingleFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	releaseCaller := sync.OnceFunc(func() { close(release) })
	defer releaseCaller()
	var calls atomic.Int64
	v := verifier(stsFake{callerOf: func(context.Context, string, string) (cloud.Caller, error) {
		if calls.Add(1) != 1 {
			return cloud.Caller{}, errors.New("duplicate request reached CallerOf")
		}
		close(started)
		<-release
		return caller(), nil
	}}, func() time.Time { return fixtureTime })
	ctx := t.Context()
	raw := rawURL(fixtureTime, 1, nil)
	leader := make(chan error, 1)
	go func() {
		id, err := v.Verify(ctx, encode(raw))
		if id != instanceID {
			t.Errorf("leader instance id = %q, want %q", id, instanceID)
		}
		leader <- err
	}()
	await(t, started, "leader to reach CallerOf")

	waiters := make([]chan error, 9)
	for i := range waiters {
		waiter := make(chan error, 1)
		waiters[i] = waiter
		waitCtx := &waitObservedContext{Context: ctx, waiting: make(chan struct{})}
		go func() {
			id, err := v.Verify(waitCtx, encode(reorderQuery(raw)))
			if id != "" {
				t.Errorf("waiter instance id = %q, want empty", id)
			}
			waiter <- err
		}()
		awaitPending(t, waitCtx, waiter)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	waitCtx := &waitObservedContext{Context: cancelCtx, waiting: make(chan struct{})}
	canceled := make(chan error, 1)
	go func() {
		id, err := v.Verify(waitCtx, encode(raw))
		if id != "" {
			t.Errorf("canceled waiter instance id = %q, want empty", id)
		}
		canceled <- err
	}()
	awaitPending(t, waitCtx, canceled)
	cancel()
	if err := await(t, canceled, "canceled waiter"); !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled waiter error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls while blocked = %d, want 1", got)
	}
	releaseCaller()
	if err := await(t, leader, "leader"); err != nil {
		t.Errorf("leader verification error = %v", err)
	}
	for _, waiter := range waiters {
		if err := await(t, waiter, "replayed waiter"); !errors.Is(err, ErrReplayed) {
			t.Errorf("waiter verification error = %v, want ErrReplayed", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("CallerOf calls = %d, want 1", got)
	}
}

// R-L41V-5IQ5.
func TestWaiterRetriesAfterFailedLeader(t *testing.T) {
	ctx := t.Context()
	good := rawURL(fixtureTime, 1, nil)
	bad := good + "&X-Amz-Credential=other"
	started := make(chan struct{})
	release := make(chan struct{})
	releaseCaller := sync.OnceFunc(func() { close(release) })
	defer releaseCaller()
	v := verifier(stsFake{callerOf: func(_ context.Context, raw string, _ string) (cloud.Caller, error) {
		if raw == bad {
			close(started)
			<-release
			return cloud.Caller{}, &cloud.Error{Class: cloud.ClassRefused}
		}
		return caller(), nil
	}}, func() time.Time { return fixtureTime })
	result := make(chan error, 1)
	go func() {
		_, err := v.Verify(ctx, encode(bad))
		result <- err
	}()
	await(t, started, "leader to reach CallerOf")
	goodResult := make(chan error, 1)
	waitCtx := &waitObservedContext{Context: ctx, waiting: make(chan struct{})}
	go func() {
		id, err := v.Verify(waitCtx, encode(good))
		if id != instanceID {
			t.Errorf("waiter instance id = %q, want %q", id, instanceID)
		}
		goodResult <- err
	}()
	awaitPending(t, waitCtx, goodResult)
	releaseCaller()
	if err := await(t, result, "failed leader"); !errors.Is(err, ErrRejected) {
		t.Errorf("failed leader = %v", err)
	}
	if err := await(t, goodResult, "retrying waiter"); err != nil {
		t.Errorf("waiter with valid URL = %v", err)
	}
}

// R-L41V-5IQ5.
func TestWaiterCancellationBeforeSettlement(t *testing.T) {
	for _, tt := range []struct {
		name      string
		callerErr error
		leaderErr error
	}{
		{name: "accepted"},
		{name: "refused", callerErr: &cloud.Error{Class: cloud.ClassRefused}, leaderErr: ErrRejected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// The select may choose either ready channel. Exercise it repeatedly.
			for range 32 {
				ctx := t.Context()
				started, settle := make(chan struct{}), make(chan struct{})
				settleCaller := sync.OnceFunc(func() { close(settle) })
				t.Cleanup(settleCaller)
				var calls atomic.Int64
				v := verifier(stsFake{callerOf: func(context.Context, string, string) (cloud.Caller, error) {
					if calls.Add(1) == 1 {
						close(started)
						<-settle
					}
					return caller(), tt.callerErr
				}}, func() time.Time { return fixtureTime })
				h := header(fixtureTime, 1)
				leader := make(chan error, 1)
				go func() {
					_, err := v.Verify(ctx, h)
					leader <- err
				}()
				await(t, started, "leader to reach CallerOf")

				cancelCtx, cancel := context.WithCancel(ctx)
				t.Cleanup(cancel)
				resume := make(chan struct{})
				resumeWaiter := sync.OnceFunc(func() { close(resume) })
				t.Cleanup(resumeWaiter)
				waitCtx := &waitObservedContext{Context: cancelCtx, waiting: make(chan struct{}), resume: resume}
				result := make(chan error, 1)
				go func() {
					id, err := v.Verify(waitCtx, h)
					if id != "" {
						t.Errorf("canceled waiter instance id = %q, want empty", id)
					}
					result <- err
				}()
				awaitPending(t, waitCtx, result)
				cancel()
				settleCaller()
				err := await(t, leader, "leader")
				resumeWaiter()
				if !errors.Is(err, tt.leaderErr) {
					t.Errorf("leader verification error = %v, want %v", err, tt.leaderErr)
				}
				if err := await(t, result, "canceled waiter"); !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled waiter error = %v", err)
				}
				if got := calls.Load(); got != 1 {
					t.Fatalf("CallerOf calls = %d, want 1", got)
				}
			}
		})
	}
}

// R-L59R-JAGU, R-L6HN-X27J, R-LA5D-2DFM.
func TestTokenBucket(t *testing.T) {
	now := fixtureTime
	configured := ""
	bindingErr := error(nil)
	bindingCalls, callerCalls, accountCalls := 0, 0, 0
	v := NewVerifier(VerifierConfig{
		Region: region,
		Role:   role,
		Binding: func() (string, error) {
			bindingCalls++
			return configured, bindingErr
		},
		STS: stsFake{
			callerOf: func(context.Context, string, string) (cloud.Caller, error) {
				callerCalls++
				return caller(), nil
			},
			account: func(context.Context) (string, error) {
				accountCalls++
				return accountID, nil
			},
		},
		Now: func() time.Time { return now },
	})
	assertVerify(t.Context(), t, v, header(now, 1), ErrUnavailable)
	bindingErr = errors.New("fixture binding error")
	assertVerify(t.Context(), t, v, header(now, 1), ErrUnavailable)
	assertVerify(t.Context(), t, v, "", ErrMalformed)
	assertVerify(t.Context(), t, v, header(now.Add(-Freshness-time.Second), 1), ErrStale)
	if bindingCalls != 2 || callerCalls != 0 || accountCalls != 0 {
		t.Fatalf("calls to Binding, CallerOf, Account = %d, %d, %d; want 2, 0, 0", bindingCalls, callerCalls, accountCalls)
	}
	configured, bindingErr = binding, nil
	for i := range VerifyBurst {
		assertVerify(t.Context(), t, v, header(now, i), nil)
	}
	assertVerify(t.Context(), t, v, header(now, VerifyBurst), ErrBusy)
	configured = ""
	assertVerify(t.Context(), t, v, header(now, 0), ErrReplayed)
	assertVerify(t.Context(), t, v, header(now, VerifyBurst), ErrUnavailable)
	configured = binding
	now = now.Add(49 * time.Millisecond)
	assertVerify(t.Context(), t, v, header(now, VerifyBurst), ErrBusy)
	now = now.Add(time.Millisecond)
	assertVerify(t.Context(), t, v, header(now, VerifyBurst), nil)
	if callerCalls != VerifyBurst+1 {
		t.Fatalf("CallerOf calls after one token refills = %d, want %d", callerCalls, VerifyBurst+1)
	}

	now = now.Add(time.Hour)
	for i := range VerifyBurst {
		assertVerify(t.Context(), t, v, header(now, VerifyBurst+1+i), nil)
	}
	assertVerify(t.Context(), t, v, header(now, 2*VerifyBurst+1), ErrBusy)
	if callerCalls != 2*VerifyBurst+1 {
		t.Fatalf("CallerOf calls after the bucket refills = %d, want %d", callerCalls, 2*VerifyBurst+1)
	}
}

// R-L7PK-ATY8, R-L6HN-X27J.
func TestInFlightLimit(t *testing.T) {
	var elapsed atomic.Int64
	now := func() time.Time { return fixtureTime.Add(time.Duration(elapsed.Load())) }
	started := make(chan struct{}, VerifyBurst+1)
	release := make(chan struct{})
	releaseCalls := sync.OnceFunc(func() { close(release) })
	defer releaseCalls()
	v := verifier(stsFake{callerOf: func(context.Context, string, string) (cloud.Caller, error) {
		started <- struct{}{}
		<-release
		return caller(), nil
	}}, now)
	var done sync.WaitGroup
	for i := range VerifyBurst {
		done.Go(func() {
			if _, err := v.Verify(t.Context(), header(fixtureTime, i)); err != nil {
				t.Errorf("verify request %d: %v", i, err)
			}
		})
	}
	for range VerifyBurst {
		await(t, started, "CallerOf to start")
	}
	elapsed.Store(int64(50 * time.Millisecond))
	full := make(chan error, 1)
	go func() {
		_, err := v.Verify(t.Context(), header(fixtureTime, VerifyBurst))
		full <- err
	}()
	select {
	case err := <-full:
		if !errors.Is(err, ErrBusy) {
			t.Errorf("full verifier = %v, want ErrBusy", err)
		}
	case <-started:
		t.Error("extra request reached STS while all calls were in flight")
	case <-time.After(waitLimit):
		t.Error("admission did not refuse the extra request")
	}
	releaseCalls()
	allDone := make(chan struct{})
	go func() {
		done.Wait()
		close(allDone)
	}()
	await(t, allDone, "CallerOf calls to finish")
	assertVerify(t.Context(), t, v, header(fixtureTime, VerifyBurst+1), nil)
}

// R-L8XG-OLOX.
func TestReplaySetExpiresWithClock(t *testing.T) {
	now := fixtureTime
	v := verifier(stsFake{}, func() time.Time { return now })
	for i := range 2 * ReplayCapacity {
		assertVerify(t.Context(), t, v, header(now, i), nil)
		now = now.Add(100 * time.Millisecond)
	}
}

// R-L8XG-OLOX.
func TestReplaySetCeilingUnderClockCorrections(t *testing.T) {
	// Validate against a clock repeatedly corrected backwards at admission.
	// This reaches the hard ceiling without expiring the accepted dates.
	signed := fixtureTime.Add(time.Hour)
	admission := fixtureTime
	var clock, calls atomic.Int64
	leaderStarted, releaseLeader := make(chan struct{}), make(chan struct{})
	releaseCaller := sync.OnceFunc(func() { close(releaseLeader) })
	defer releaseCaller()
	leaderURL := rawURL(signed, ReplayCapacity-1, nil)
	v := NewVerifier(VerifierConfig{
		Region: region,
		Role:   role,
		Binding: func() (string, error) {
			clock.Store(admission.UnixNano())
			return binding, nil
		},
		STS: stsFake{callerOf: func(_ context.Context, raw, _ string) (cloud.Caller, error) {
			calls.Add(1)
			if raw == leaderURL {
				close(leaderStarted)
				<-releaseLeader
				return cloud.Caller{}, &cloud.Error{Class: cloud.ClassRefused}
			}
			return caller(), nil
		}},
		Now: func() time.Time { return time.Unix(0, clock.Load()).UTC() },
	})
	for i := range ReplayCapacity - 1 {
		clock.Store(signed.UnixNano())
		assertVerify(t.Context(), t, v, header(signed, i), nil)
		admission = admission.Add(100 * time.Millisecond)
	}

	ctx := t.Context()
	clock.Store(signed.UnixNano())
	leader := make(chan error, 1)
	go func() {
		_, err := v.Verify(ctx, encode(leaderURL))
		leader <- err
	}()
	await(t, leaderStarted, "leader to reach CallerOf")

	clock.Store(signed.UnixNano())
	waiter := make(chan error, 1)
	waitCtx := &waitObservedContext{Context: ctx, waiting: make(chan struct{})}
	go func() {
		_, err := v.Verify(waitCtx, header(signed, ReplayCapacity))
		waiter <- err
	}()
	awaitPending(t, waitCtx, waiter)
	if got := calls.Load(); got != ReplayCapacity {
		t.Errorf("CallerOf called %d times before the reserved slot cleared", got)
	}
	releaseCaller()
	if err := await(t, leader, "leader"); !errors.Is(err, ErrRejected) {
		t.Fatalf("reserved call = %v, want rejection", err)
	}
	if err := await(t, waiter, "waiter"); err != nil {
		t.Fatalf("waiting call did not acquire the slot: %v", err)
	}

	clock.Store(signed.UnixNano())
	assertVerify(t.Context(), t, v, header(signed, ReplayCapacity+1), ErrBusy)
	clock.Store(signed.UnixNano())
	assertVerify(t.Context(), t, v, header(signed, 0), ErrReplayed)
	if got := calls.Load(); got != ReplayCapacity+1 {
		t.Fatalf("CallerOf called %d times at the replay ceiling", got)
	}
}

type waitObservedContext struct {
	context.Context
	waiting chan struct{}
	resume  <-chan struct{}
	once    sync.Once
}

const waitLimit = 5 * time.Second

func await[T any](t *testing.T, ch <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(waitLimit):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

func awaitPending(t *testing.T, ctx *waitObservedContext, result <-chan error) {
	t.Helper()
	select {
	case <-ctx.waiting:
	case err := <-result:
		t.Fatalf("waiter returned before the in-flight call settled: %v", err)
	case <-time.After(waitLimit):
		t.Fatal("timed out waiting for the verifier to block on the in-flight call")
	}
}

func (c *waitObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	if c.resume != nil {
		<-c.resume
	}
	return c.Context.Done()
}

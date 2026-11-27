package daemonapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

const backupPartition = "aws"

type backupSTS struct {
	fixtureSTS
	identityErr error
	assume      func(cloud.RoleRequest) (cloud.Credentials, error)
	calls       atomic.Int64
}

func (s *backupSTS) Identity(context.Context) (cloud.Caller, error) {
	s.calls.Add(1)
	return cloud.Caller{Account: fixtureAccount, Arn: "arn:" + backupPartition + ":iam::" + fixtureAccount + ":role/space"}, s.identityErr
}

func (s *backupSTS) AssumeRole(_ context.Context, r cloud.RoleRequest) (cloud.Credentials, error) {
	s.calls.Add(1)
	return s.assume(r)
}

func stateResponse(t *testing.T, w *httptest.ResponseRecorder) protocol.DesiredState {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != protocol.ContentJSON || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("state response = %d, %v, %s", w.Code, w.Header(), w.Body)
	}
	got, err := protocol.DecodeState(bytes.NewReader(w.Body.Bytes()))
	if err != nil || got.Backup == nil {
		t.Fatalf("state response has no valid backup overlay: %v, %s", err, w.Body)
	}
	return got
}

type desiredBackup struct {
	*store.Store
	backup *protocol.Backup
}

func (s desiredBackup) Desired(ctx context.Context, name string) (protocol.DesiredState, error) {
	state, err := s.Store.Desired(ctx, name)
	state.Backup = s.backup
	return state, err
}

// R-KWPE-A1KF, R-L1KZ-T4J7, R-OGXL-J7GO.
func TestBackupOverlayWithoutAccess(t *testing.T) {
	d, st, host := fixtureStore(t)
	ctx := t.Context()
	addBoxAccount(t, st, host.Name, "acme")
	stamps := new(requests)
	hosts := settings.SandboxHosts
	wait, tick := make(chan time.Duration, 1), make(chan time.Time, 1)
	s := server(t, Config{
		Store: st, Configured: true, Hosts: hosts, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		After: func(d time.Duration) <-chan time.Time { wait <- d; return tick },
	})
	get := func() *httptest.ResponseRecorder {
		return invoke(s, stamps.signed(ctx, http.MethodGet, protocol.StatePath, nil))
	}
	res := get()
	before := stateResponse(t, res)
	want := desired(t, st, host.Name, s.Digest())
	want.Backup.Bucket = "hosts-test-hosts-backups"
	want.Backup.Region = "sandbox"
	want.Backup.HostPrefix = protocol.HostPrefix(host.Name, host.Seq)
	expectState(t, res, want)

	host.Version = before.Version
	pending := startPoll(t, s, stamps.signed(ctx, http.MethodGet, pollingRequest(host, s.Digest()), nil), wait)
	row := store.BackupSettings{EveryMinutes: 15, Hourly: 25, Daily: 8, Monthly: 2, WindowDays: 36, FilesStaleMinutes: 121, DatabaseStaleMinutes: 11, CheckDays: 9}
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO backup_settings VALUES ('', ?, ?, ?, ?, ?, ?, ?, ?)",
			row.EveryMinutes, row.Hourly, row.Daily, row.Monthly, row.WindowDays, row.FilesStaleMinutes, row.DatabaseStaleMinutes, row.CheckDays)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tick <- fixtureNow
	res = <-pending
	after := stateResponse(t, res)
	want.Backup.Settings = protocol.Settings(row)
	expectState(t, res, want)
	if before.Version != after.Version {
		t.Fatalf("settings changed desired version from %d to %d", before.Version, after.Version)
	}

	s.store = desiredBackup{Store: st}
	want.Backup.HostPrefix = ""
	want.Backup.Restores = nil
	expectState(t, get(), want)

	restored := &protocol.Backup{
		Host:       protocol.Credentials{SessionToken: "unexpected-host-credential"},
		Accounts:   []protocol.AccountCredentials{{Account: "acme"}},
		HostPrefix: protocol.HostPrefix(host.Name, host.Seq),
		Close:      "close-id",
		Restore:    &protocol.HostRestore{ID: "replace-id", Accounts: []protocol.RestoreAccount{{Slug: "acme"}}},
		Restores:   []protocol.RestoreRequest{{ID: "restore-id", Account: "acme"}},
	}
	s.store = desiredBackup{Store: st, backup: restored}
	want.Backup.HostPrefix = restored.HostPrefix
	want.Backup.Close = restored.Close
	want.Backup.Restore = restored.Restore
	want.Backup.Restores = restored.Restores
	expectState(t, get(), want)
}

// R-OFPP-5FPZ.
func TestBackupCredentialsFollowActiveAndRestoringAccounts(t *testing.T) {
	_, st, host := fixtureStore(t)
	ctx := t.Context()
	for _, slug := range []string{"zebra", "bravo", "acme"} {
		addBoxAccount(t, st, host.Name, slug)
	}
	owner := identity.Caller{UserID: "owner"}
	if _, _, _, err := st.RemoveAccount(ctx, "bravo", owner); err != nil {
		t.Fatal(err)
	}
	hosts := settings.SandboxHosts
	fake := &backupSTS{assume: func(r cloud.RoleRequest) (cloud.Credentials, error) {
		return cloud.Credentials{AccessKeyID: "test-id", SecretAccessKey: "test-secret", SessionToken: r.Policy, Expires: fixtureNow.Add(time.Hour)}, nil
	}}
	acc := access.New(access.Config{STS: fake, Hosts: hosts, Now: func() time.Time { return fixtureNow }})
	stamps := new(requests)
	s := server(t, Config{Store: st, Configured: true, Hosts: hosts, Access: acc, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})})
	get := func() protocol.DesiredState {
		return stateResponse(t, invoke(s, stamps.signed(ctx, http.MethodGet, protocol.StatePath, nil)))
	}
	grant := func(prefix string) protocol.Credentials {
		return protocol.Credentials{AccessKeyID: "test-id", SecretAccessKey: "test-secret",
			SessionToken: access.Policy(hosts.BackupBucket(), backupPartition, prefix), Expires: fixtureNow.Add(time.Hour)}
	}
	want := []protocol.AccountCredentials{
		{Account: "acme", Credentials: grant(protocol.AccountPrefix("acme"))},
		{Account: "zebra", Credentials: grant(protocol.AccountPrefix("zebra"))},
	}
	check := func(b *protocol.Backup) {
		t.Helper()
		if b.Host != grant(protocol.HostPrefix(host.Name, host.Seq)) || !reflect.DeepEqual(b.Accounts, want) {
			t.Fatal("wrong scoped backup credentials")
		}
	}
	running := get()
	check(running.Backup)
	if len(running.Accounts) != 3 || running.Accounts[1].Slug != "bravo" || running.Accounts[1].State != protocol.AccountRemoved {
		t.Fatalf("fixture did not expose the removed account: %+v", running.Accounts)
	}

	_, replace, _, err := st.ReplaceHost(ctx, host.Name, store.ReplaceBackup, store.Target{}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Retire(ctx, replace.ID); err != nil {
		t.Fatal(err)
	}
	restoring := get()
	if len(restoring.Accounts) != 0 || restoring.Backup.Restore == nil || restoring.Backup.Restore.ID != replace.ID ||
		len(restoring.Backup.Restore.Accounts) != 2 || restoring.Backup.Restore.Accounts[0].Slug != "acme" || restoring.Backup.Restore.Accounts[1].Slug != "zebra" {
		t.Fatalf("state did not expose the retired host's restore: %+v", restoring.Backup.Restore)
	}
	check(restoring.Backup)

	// Exercise an overlapping issue set too. The production store withholds ordinary
	// accounts during a whole-host restore.
	s.store = desiredBackup{Store: st, backup: restoring.Backup}
	if err := st.Admit(ctx, replace.ID); err != nil {
		t.Fatal(err)
	}
	overlap := get()
	if len(overlap.Accounts) != 3 || overlap.Backup.Restore == nil {
		t.Fatal("fixture did not overlap active and restoring slugs")
	}
	check(overlap.Backup)
}

// R-GD3T-4WKA.
func TestBackupCredentialFailuresDoNotFailState(t *testing.T) {
	errSTS := errors.New("test STS error")
	grant := protocol.Credentials{AccessKeyID: "test-id", SecretAccessKey: "test-secret", SessionToken: "test-token", Expires: fixtureNow.Add(time.Hour)}
	toCloud := func(c protocol.Credentials) cloud.Credentials {
		return cloud.Credentials{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, SessionToken: c.SessionToken, Expires: c.Expires}
	}
	for _, tt := range []struct {
		name          string
		identityErr   error
		failSlug      string
		failOnReissue bool
		token         string
		want          [3]protocol.Credentials // host, acme, bravo
	}{
		{name: "one role fails", failSlug: "bravo", want: [3]protocol.Credentials{grant, grant, {}}},
		{name: "cached set survives failed renewal", failSlug: "acme", failOnReissue: true, want: [3]protocol.Credentials{grant, grant, grant}},
		{name: "identity fails", identityErr: errSTS},
		{name: "raw token too large", token: strings.Repeat("x", MaxCredentialBytes)},
		{name: "escaped token too large", token: strings.Repeat(`"`, 1500)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, st, host := fixtureStore(t)
			addBoxAccount(t, st, host.Name, "acme")
			addBoxAccount(t, st, host.Name, "bravo")
			stamps := new(requests)
			var elapsed atomic.Int64
			now := func() time.Time { return fixtureNow.Add(time.Duration(elapsed.Load())) }
			failedRenewals := new(atomic.Int64)
			hosts := settings.SandboxHosts
			fake := &backupSTS{identityErr: tt.identityErr, assume: func(r cloud.RoleRequest) (cloud.Credentials, error) {
				if tt.failSlug != "" && strings.Contains(r.Policy, protocol.AccountPrefix(tt.failSlug)) && (!tt.failOnReissue || now().After(fixtureNow)) {
					failedRenewals.Add(1)
					return cloud.Credentials{}, errSTS
				}
				response := grant
				if tt.token != "" {
					response.SessionToken = tt.token
				}
				return toCloud(response), nil
			}}
			acc := access.New(access.Config{STS: fake, Hosts: hosts, Now: now})
			s := server(t, Config{Store: st, Configured: true, Hosts: hosts, Access: acc, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})})
			check := func() {
				t.Helper()
				state := stateResponse(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, protocol.StatePath, nil)))
				b := state.Backup
				if len(b.Accounts) != 2 || b.Accounts[0].Account != "acme" || b.Accounts[1].Account != "bravo" {
					t.Fatalf("missing account entries: %+v", b.Accounts)
				}
				if got := ([3]protocol.Credentials{b.Host, b.Accounts[0].Credentials, b.Accounts[1].Credentials}); got != tt.want {
					t.Fatal("unexpected credentials after STS response")
				}
			}
			check()
			if tt.failOnReissue {
				elapsed.Store(int64(20 * time.Minute))
				check()
				if failedRenewals.Load() != 1 {
					t.Fatal("expected one failed renewal")
				}
			}
		})
	}
}

// R-OKLA-OIOR.
func TestBackupSettingsFailurePrecedesIssuance(t *testing.T) {
	d, st, _ := fixtureStore(t)
	ctx := t.Context()
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DROP TABLE backup_settings")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	stamps := new(requests)
	fake := new(backupSTS)
	s := server(t, Config{Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Access: access.New(access.Config{STS: fake})})
	expectError(t, invoke(s, stamps.signed(ctx, http.MethodGet, protocol.StatePath, nil)), http.StatusServiceUnavailable, store.Unreachable)
	if fake.calls.Load() != 0 {
		t.Fatal("backup settings failure made an STS request")
	}
}

// R-LA4A-HIQ2, R-ON13-G265.
func TestBackupCredentialsStayInSuccessfulStateBodies(t *testing.T) {
	const hostSecret, hostToken = "test-host-only-secret", "test-host-only-token"
	const accountSecret, accountToken = "test-account-only-secret", "test-account-only-token"
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	d, st, host := fixtureStore(t)
	ctx := t.Context()
	const slug = "acme"
	addBoxAccount(t, st, host.Name, slug)
	host, err := st.Host(ctx, host.Name)
	if err != nil {
		t.Fatal(err)
	}
	stamps := new(requests)
	writer, capture := recorder(t)
	after := make(chan time.Duration, 1)
	fake := &backupSTS{assume: func(req cloud.RoleRequest) (cloud.Credentials, error) {
		cred := cloud.Credentials{AccessKeyID: "test-id", SecretAccessKey: hostSecret, SessionToken: hostToken, Expires: fixtureNow.Add(time.Hour)}
		if req.SessionName == access.SessionName(host.Name, slug) {
			cred.SecretAccessKey, cred.SessionToken = accountSecret, accountToken
		}
		return cred, nil
	}}
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Telemetry: writer,
		Access: access.New(access.Config{STS: fake, Now: func() time.Time { return fixtureNow }}),
		After:  func(delay time.Duration) <-chan time.Time { after <- delay; return nil },
	})
	state := func(path string) *httptest.ResponseRecorder {
		return invoke(s, stamps.signed(ctx, http.MethodGet, path, nil))
	}
	poll := pollingRequest(host, "")
	pending := startPoll(t, s, stamps.signed(ctx, http.MethodGet, poll, nil), after)
	s.Stop()
	success := []*httptest.ResponseRecorder{<-pending, state(protocol.StatePath)}
	for _, res := range success {
		b := stateResponse(t, res).Backup
		if b.Host.SecretAccessKey != hostSecret || b.Host.SessionToken != hostToken ||
			len(b.Accounts) != 1 || b.Accounts[0].Account != slug ||
			b.Accounts[0].Credentials.SecretAccessKey != accountSecret || b.Accounts[0].Credentials.SessionToken != accountToken {
			t.Fatal("successful state omitted the host or account backup grant")
		}
	}
	busy := state(poll)
	expectError(t, busy, http.StatusServiceUnavailable, Busy)
	report := invoke(s, stamps.signed(ctx, http.MethodPost, protocol.ReportPath,
		strings.NewReader(fmt.Sprintf(`{"protocol":%d,"attempted":%d,"error":"test failure"}`, protocol.Version, host.Version))))
	if report.Code != http.StatusNoContent {
		t.Fatalf("report status = %d, body = %s", report.Code, report.Body)
	}
	d.SetFailing(true)
	failed := state(protocol.StatePath)
	expectError(t, failed, http.StatusServiceUnavailable, store.Unreachable)
	if err := writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	events := capture.Events()
	if len(events) == 0 {
		t.Fatal("report did not produce an event")
	}
	reject := func(label, content string) {
		t.Helper()
		for _, marker := range []string{hostSecret, hostToken, accountSecret, accountToken} {
			if strings.Contains(content, marker) {
				t.Errorf("%s contains a backup credential", label)
			}
		}
	}
	other := []*httptest.ResponseRecorder{busy, report, failed}
	for _, res := range slices.Concat(success, other) {
		wire := res.Result()
		reject("response headers", fmt.Sprint(wire.Header))
		reject("response trailers", fmt.Sprint(wire.Trailer))
	}
	for _, res := range other {
		reject("response body", res.Body.String())
	}
	reject("event trail", fmt.Sprint(events))
	reject("standard logger", logs.String())
}

// R-OEHS-RNZA.
func TestLargestStateAnswerFitsWireBudget(t *testing.T) {
	_, st, host := fixtureStore(t)
	ctx := t.Context()
	owner := identity.Caller{UserID: "owner"}
	slugs := make([]string, valid.MaxAccountsPerHost)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("a%031d", i)
		if err := valid.Slug(slugs[i]); err != nil {
			t.Fatal(err)
		}
		addBoxAccount(t, st, host.Name, slugs[i])
	}
	keys := make([]store.Key, valid.MaxKeysPerUser)
	for i := range keys {
		keys[i] = boxKey(t, i)
		keys[i].Line = strings.ReplaceAll(keys[i].Line, "<", `"`)
		if _, err := valid.ParseKey(keys[i].Line); err != nil || len(keys[i].Line) != valid.MaxKeyBytes {
			t.Fatalf("worst-case key has length %d, validation %v", len(keys[i].Line), err)
		}
	}
	for i := range valid.MaxUsersPerHost {
		slug, name := slugs[i%len(slugs)], fmt.Sprintf("u%031d", i)
		if err := valid.Name(name); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddUser(ctx, slug, name, keys[0], owner); err != nil {
			t.Fatal(err)
		}
		for _, key := range keys[1:] {
			if _, err := st.AddKey(ctx, slug, name, key); err != nil {
				t.Fatal(err)
			}
		}
	}
	allowlist := make([]string, valid.MaxAllowlistPerHost)
	for i := range allowlist {
		allowlist[i] = fmt.Sprintf("1234:5678:9abc:def0:1234:5678:9abc:%04x/127", 0x1000+2*i)
		if _, err := st.Allow(ctx, host.Name, allowlist[i]); err != nil {
			t.Fatal(err)
		}
	}
	unbans := make([]string, valid.MaxUnbansPerHost)
	for i := range unbans {
		unbans[i] = fmt.Sprintf("1234:5678:9abc:def0:1234:5678:9abc:%04x", 0x2000+i)
		if _, err := st.Unban(ctx, host.Name, unbans[i]); err != nil {
			t.Fatal(err)
		}
	}
	asOf := fixtureNow.Add(-time.Hour)
	report := protocol.Report{Protocol: protocol.Version}
	for _, slug := range slugs[:valid.MaxRestoresPerHost] {
		report.Backups = append(report.Backups, protocol.AccountBackup{Account: slug, Snapshots: []int64{asOf.Unix()}})
	}
	if _, err := st.RecordReport(ctx, host.Name, report, fixtureNow); err != nil {
		t.Fatal(err)
	}
	path := strings.Repeat(`"`, (valid.MaxRestorePath-2)/2)
	encodedPath, err := json.Marshal(path)
	if err != nil || len(encodedPath) != valid.MaxRestorePath || valid.RestorePath(path) != nil {
		t.Fatalf("worst-case restore path has encoded length %d, marshal error %v", len(encodedPath), err)
	}
	restores := make(map[string]protocol.RestoreRequest, valid.MaxRestoresPerHost)
	for i, slug := range slugs[:valid.MaxRestoresPerHost] {
		user := fmt.Sprintf("u%031d", i)
		r, _, err := st.RequestRestore(ctx, store.RestoreRequest{Account: slug, Kind: protocol.RestoreFiles, Path: path, User: user, AsOf: asOf}, owner)
		if err != nil {
			t.Fatal(err)
		}
		restores[r.ID] = protocol.RestoreRequest{ID: r.ID, Account: slug, Kind: protocol.RestoreFiles, Path: path, User: user, AsOf: asOf}
	}
	_, closeOp, err := st.CloseHost(ctx, host.Name, owner)
	if err != nil {
		t.Fatal(err)
	}
	grant := cloud.Credentials{
		AccessKeyID: strings.Repeat("A", 40), SecretAccessKey: strings.Repeat("B", 40),
		SessionToken: strings.Repeat(`"`, (MaxCredentialBytes-2*40-6)/2), Expires: fixtureNow.Add(time.Hour),
	}
	var n int
	for _, value := range []string{grant.AccessKeyID, grant.SecretAccessKey, grant.SessionToken} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		n += len(raw)
	}
	if n != MaxCredentialBytes {
		t.Fatalf("worst-case credentials use %d bytes, want %d", n, MaxCredentialBytes)
	}
	fake := &backupSTS{assume: func(cloud.RoleRequest) (cloud.Credentials, error) { return grant, nil }}
	domain := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	stamps := new(requests)
	hosts := settings.SandboxHosts
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Domain: domain, Hosts: hosts,
		Access: access.New(access.Config{STS: fake, Hosts: hosts, Now: func() time.Time { return fixtureNow }}),
	})
	res := invoke(s, stamps.signed(ctx, http.MethodGet, protocol.StatePath, nil))
	if res.Body.Len() > protocol.MaxStateBytes {
		t.Fatalf("state body has %d bytes, maximum %d", res.Body.Len(), protocol.MaxStateBytes)
	}
	state := stateResponse(t, res)
	b := state.Backup
	if state.Domain != domain || len(state.Accounts) != valid.MaxAccountsPerHost || len(state.Users) != valid.MaxUsersPerHost ||
		!slices.Equal(state.Allowlist, allowlist) || !slices.Equal(state.Unban, unbans) ||
		b.Close != closeOp.ID || len(b.Restores) != valid.MaxRestoresPerHost {
		t.Fatal("state did not carry the full worst-case fixture")
	}
	for _, got := range b.Restores {
		if want, ok := restores[got.ID]; !ok || got != want {
			t.Fatalf("state restore %q lost boundary-sized request data", got.ID)
		}
		delete(restores, got.ID)
	}
	wantKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		wantKeys = append(wantKeys, key.Line)
	}
	slices.Sort(wantKeys)
	for i, user := range state.Users {
		if user.Name != fmt.Sprintf("u%031d", i) || user.Account != slugs[i%len(slugs)] {
			t.Fatalf("state user %q lost boundary-sized account or user name", user.Name)
		}
		gotKeys := slices.Clone(user.Keys)
		slices.Sort(gotKeys)
		if !slices.Equal(gotKeys, wantKeys) {
			t.Fatalf("state user %q did not carry each boundary-sized key", user.Name)
		}
	}
	wantGrant := protocol.Credentials{AccessKeyID: grant.AccessKeyID, SecretAccessKey: grant.SecretAccessKey, SessionToken: grant.SessionToken, Expires: grant.Expires}
	if b.Host != wantGrant || len(b.Accounts) != len(slugs) {
		t.Fatal("state did not carry the host and accounts' boundary-size backup grants")
	}
	for i, entry := range b.Accounts {
		a := state.Accounts[i]
		if a.Slug != slugs[i] || a.State != protocol.AccountActive || entry.Account != slugs[i] || entry.Credentials != wantGrant {
			t.Fatalf("state did not carry the boundary-size grant for %q", slugs[i])
		}
	}
}

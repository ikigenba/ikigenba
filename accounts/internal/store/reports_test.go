package store_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-GVGS-XTNW R-HBBH-WUAX R-IXUO-9TW9 R-IZ2K-NLMY R-PKAD-JM2E
// R-J0AH-1DDN R-J1ID-F54C R-J2Q9-SWV1 R-J6DY-Y834 R-IQJ9-Z7G3 R-50PQ-7WUW
func TestDesiredReportAndWatch(t *testing.T) {
	_, s, now := fixture(t)
	h, provision := create(t, s)
	b, _ := create(t, s)
	a, placeOp := place(t, s, "home", h.Name)
	away, _ := place(t, s, "away", b.Name)
	user := value(t, result(s.AddUser(ctx, a.Slug, "amy", firstKey, caller)))
	visitor := value(t, result(s.AddUser(ctx, away.Slug, "visitor", firstKey, caller)))
	tombstone := value(t, result(s.AddUser(ctx, a.Slug, "tina", firstKey, caller)))
	tombstone = value(t, result(s.Lock(ctx, a.Slug, tombstone.Name)))
	tombstone = value(t, result(s.Delete(ctx, a.Slug, tombstone.Name)))
	checkErr(t, s.Finish(ctx, provision.ID, store.Update{DataVolume: "data-volume"}), nil)
	h = value(t, result(s.Host(ctx, h.Name)))
	d := value(t, result(s.Desired(ctx, h.Name)))
	eq(t, d, protocol.DesiredState{Protocol: protocol.Version, Host: h.Name, Version: h.Version,
		DataVolume: h.DataVolume, RootGB: h.RootGB, DataGB: h.DataGB,
		TemplateURL: protocol.DefaultTemplateURL, TemplateRef: protocol.DefaultTemplateRef,
		Allowlist: []string{}, Unban: []string{},
		Backup:   &protocol.Backup{HostPrefix: protocol.HostPrefix(h.Name, h.Seq), Restores: []protocol.RestoreRequest{}},
		Accounts: []protocol.Account{{Slug: a.Slug, GID: a.GID, State: protocol.AccountActive}},
		Users: []protocol.User{
			{Name: user.Name, Account: a.Slug, UID: user.UID, State: protocol.UserActive, Keys: []string{firstKey.Line}},
			{Name: tombstone.Name, Account: a.Slug, UID: tombstone.UID, State: protocol.UserDeleted, Keys: []string{firstKey.Line}},
		}})
	var wire struct {
		Protocol   int                `json:"protocol"`
		Host       string             `json:"host"`
		Version    int64              `json:"version"`
		Daemon     *string            `json:"daemon"`
		DataVolume string             `json:"data_volume"`
		RootGB     int                `json:"root_gb"`
		DataGB     int                `json:"data_gb"`
		Accounts   []protocol.Account `json:"accounts"`
		Users      []protocol.User    `json:"users"`
		Allowlist  []string           `json:"allowlist"`
		Unban      []string           `json:"unban"`
	}
	checkErr(t, json.Unmarshal(value(t, result(json.Marshal(d))), &wire), nil)
	eq(t, wire.Protocol, protocol.Version)
	eq(t, wire.Host, h.Name)
	eq(t, wire.Version, h.Version)
	eq(t, wire.Daemon, new(string))
	eq(t, wire.DataVolume, h.DataVolume)
	eq(t, wire.RootGB, h.RootGB)
	eq(t, wire.DataGB, h.DataGB)
	eq(t, wire.Accounts, d.Accounts)
	eq(t, wire.Users, d.Users)
	eq(t, wire.Allowlist, []string{})
	eq(t, wire.Unban, []string{})

	eq(t, value(t, result(s.Report(ctx, h.Name))), (*store.Report)(nil))
	_, err := s.RecordReport(ctx, "missing", protocol.Report{}, *now)
	checkErr(t, err, store.ErrNotFound)
	_, err = s.Report(ctx, "missing")
	checkErr(t, err, store.ErrNotFound)
	_, err = s.Desired(ctx, "missing")
	checkErr(t, err, store.ErrNotFound)
	checkErr(t, s.Finish(ctx, placeOp.ID, store.Update{}), nil)
	eq(t, value(t, result(s.Desired(ctx, h.Name))), d)
	ch, release := s.Watch(h.Name)
	defer release()

	r := protocol.Report{Protocol: protocol.Version, Applied: h.Version - 2, Attempted: h.Version - 1,
		SignIns: []protocol.SignIn{{User: user.Name, At: now.Add(time.Hour)}, {User: visitor.Name, At: *now},
			{User: user.Name, At: now.Add(-time.Hour)}, {User: "unknown", At: *now}},
		Slices: []protocol.Slice{{Account: a.Slug, CPUPercent: 10}, {Account: away.Slug}, {Account: a.Slug, CPUPercent: 20}}}
	tr := value(t, result(s.RecordReport(ctx, h.Name, r, *now)))
	eq(t, tr, store.Transition{Before: store.HealthNone, After: store.HealthPending, Applied: r.Applied, Attempted: r.Attempted})
	got := value(t, result(s.Report(ctx, h.Name)))
	filtered := r
	filtered.SignIns = []protocol.SignIn{r.SignIns[0], r.SignIns[2]}
	filtered.Slices = []protocol.Slice{r.Slices[0], r.Slices[2]}
	body := got.Body
	for i := range body.SignIns {
		// JSON keeps an offset, whereas the supplied time has a named zone.
		eq(t, body.SignIns[i].At.Equal(filtered.SignIns[i].At), true)
		body.SignIns[i].At = filtered.SignIns[i].At
	}
	eq(t, got.Host, h.Name)
	eq(t, got.Received, now.UTC().Truncate(time.Second))
	eq(t, body, filtered)
	post := value(t, result(s.Host(ctx, h.Name)))
	eq(t, post.Reported, now.UTC().Truncate(time.Second))
	eq(t, post.ReportError, false)
	eq(t, post.Attempted, r.Attempted)
	eq(t, value(t, result(s.User(ctx, a.Slug, user.Name))).LastSignIn, now.UTC().Truncate(time.Second))
	eq(t, value(t, result(s.User(ctx, away.Slug, visitor.Name))).LastSignIn, time.Time{})
	eq(t, value(t, result(s.User(ctx, a.Slug, tombstone.Name))), tombstone)
	_, err = s.AddUser(ctx, a.Slug, tombstone.Name, firstKey, caller)
	checkErr(t, err, store.ErrTaken)

	*now = now.Add(time.Minute)
	checkpoint := protocol.Report{Protocol: protocol.Version, Applied: 1, Attempted: h.Version}
	tr = value(t, result(s.RecordReport(ctx, h.Name, checkpoint, *now)))
	eq(t, tr.Applied, r.Applied)
	checkpoint.Error, checkpoint.Applied = "gave up", h.Version+10
	tr = value(t, result(s.RecordReport(ctx, h.Name, checkpoint, *now)))
	eq(t, tr, store.Transition{Before: store.HealthPending, After: store.HealthError, ApplyFailed: true,
		Applied: r.Applied, Attempted: checkpoint.Attempted})
	tr = value(t, result(s.RecordReport(ctx, h.Name, checkpoint, *now)))
	eq(t, tr.ApplyFailed, false)
	checkpoint.Error = ""
	tr = value(t, result(s.RecordReport(ctx, h.Name, checkpoint, *now)))
	eq(t, tr, store.Transition{Before: store.HealthError, After: store.HealthSync, Synced: true,
		Applied: h.Version, Attempted: checkpoint.Attempted})
	eq(t, value(t, result(s.Report(ctx, h.Name))).Body, checkpoint)
	eq(t, value(t, result(s.Host(ctx, h.Name))).Alarm, store.AlarmNone)
	tr = value(t, result(s.RecordReport(ctx, h.Name, checkpoint, *now)))
	eq(t, tr.Synced, false)
	_, err = s.User(ctx, a.Slug, tombstone.Name)
	checkErr(t, err, store.ErrNotFound)
	d.Users = d.Users[:1]
	eq(t, value(t, result(s.Desired(ctx, h.Name))), d)
	closed(t, ch, false)
	value(t, result(s.AddUser(ctx, away.Slug, "other", firstKey, caller)))
	closed(t, ch, false)
	recreated := value(t, result(s.AddUser(ctx, a.Slug, tombstone.Name, firstKey, caller)))
	eq(t, recreated.UID > tombstone.UID, true)
	eq(t, recreated.Keys, []store.Key{firstKey})
	closed(t, ch, true)
	release()
	release()

	// Moving an account to removing changes Desired; finishing only drops its tombstone.
	z, _ := place(t, s, "empty", h.Name)
	_, removal := remove(t, s, z.Slug)
	d = value(t, result(s.Desired(ctx, h.Name)))
	eq(t, d.Accounts[0], protocol.Account{Slug: z.Slug, GID: z.GID, State: protocol.AccountRemoved})
	checkErr(t, s.Finish(ctx, removal.ID, store.Update{}), nil)
	d.Accounts = d.Accounts[1:]
	eq(t, value(t, result(s.Desired(ctx, h.Name))), d)
}

func closed(t *testing.T, ch <-chan struct{}, want bool) {
	t.Helper()
	select {
	case <-ch:
		eq(t, true, want)
	default:
		eq(t, false, want)
	}
}

// R-H57Z-ZZLG R-J3Y6-6OLQ R-J562-KGCF R-J1ID-F54C
func TestHealthAndSilentSweep(t *testing.T) {
	_, s, now := fixture(t)
	stamp := now.UTC().Truncate(time.Second)
	*now = stamp
	h, one := create(t, s)
	i, two := create(t, s)
	k, three := create(t, s)
	for _, op := range []store.Operation{one, two, three} {
		checkErr(t, s.Finish(ctx, op.ID, store.Update{}), nil)
	}
	_, stop := start(t, s, k.Name, store.OpStop, store.Target{})
	checkErr(t, s.Finish(ctx, stop.ID, store.Update{}), nil)
	checkpoint := protocol.Report{Applied: 1}
	value(t, result(s.RecordReport(ctx, k.Name, checkpoint, *now)))
	value(t, result(s.RecordReport(ctx, i.Name, protocol.Report{Error: "failed"}, *now)))
	h = value(t, result(s.Host(ctx, h.Name)))
	eq(t, store.Health(h, now.Add(4*time.Minute)), store.HealthNone)
	eq(t, store.Health(h, now.Add(5*time.Minute)), store.HealthNone)
	eq(t, store.Health(h, now.Add(6*time.Minute)), store.HealthSilent)
	k = value(t, result(s.Host(ctx, k.Name)))
	eq(t, store.Health(k, now.Add(24*time.Hour)), store.HealthSync)
	i = value(t, result(s.Host(ctx, i.Name)))
	eq(t, store.Health(i, *now), store.HealthError)
	eq(t, store.Health(i, now.Add(6*time.Minute)), store.HealthSilent)
	eq(t, value(t, result(s.MarkSilent(ctx, now.Add(store.SilentAfter), "", 1))), []store.Host{})
	*now = now.Add(store.SilentAfter + time.Nanosecond)
	h.Alarm, i.Alarm = store.AlarmSilent, store.AlarmSilent
	eq(t, value(t, result(s.MarkSilent(ctx, *now, "", 1))), []store.Host{h})
	eq(t, value(t, result(s.MarkSilent(ctx, *now, h.Name, 1))), []store.Host{i})
	eq(t, value(t, result(s.MarkSilent(ctx, *now, "", 10))), []store.Host{})
	tr := value(t, result(s.RecordReport(ctx, h.Name, protocol.Report{}, *now)))
	eq(t, tr.After, store.HealthPending)
	eq(t, tr.Synced, false)
	eq(t, value(t, result(s.Host(ctx, h.Name))).Alarm, store.AlarmSilent)
	tr = value(t, result(s.RecordReport(ctx, h.Name, checkpoint, *now)))
	eq(t, tr.After, store.HealthSync)
	eq(t, tr.Synced, true)
}

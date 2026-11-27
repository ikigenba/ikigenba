package ops_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/ops"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

type backupDeleteCloud struct {
	cloud.Cloud
	f          *fixture
	keys       map[string][]string
	limit      int
	listed     []string
	lastPrefix string
	page       []string
	onList     func(context.Context, string) error
	onDelete   func(context.Context, string) error
}

func (c *backupDeleteCloud) ListKeys(ctx context.Context, bucket, prefix string, each func([]string) bool) error {
	c.f.t.Helper()
	equal(c.f.t, bucket, c.f.cfg.Hosts.BackupBucket())
	c.listed = append(c.listed, prefix)
	c.lastPrefix = prefix
	c.page = nil
	if c.onList != nil {
		if err := c.onList(ctx, prefix); err != nil {
			return err
		}
	}
	keys := c.keys[prefix]
	c.page = keys[:min(len(keys), c.limit)]
	if len(c.page) != 0 && each(c.page) {
		c.f.t.Fatalf("listing continued past its first page for %s", prefix)
	}
	return nil
}

func (c *backupDeleteCloud) DeleteKeys(ctx context.Context, bucket string, keys []string) error {
	c.f.t.Helper()
	equal(c.f.t, bucket, c.f.cfg.Hosts.BackupBucket())
	equal(c.f.t, len(keys) > 0 && len(keys) <= cloud.MaxKeysPerPage, true)
	equal(c.f.t, slices.Equal(keys, c.page), true)
	if c.onDelete != nil {
		if err := c.onDelete(ctx, c.lastPrefix); err != nil {
			return err
		}
	}
	c.keys[c.lastPrefix] = c.keys[c.lastPrefix][len(keys):]
	return nil
}

func backupKeys(prefix string, count int) []string {
	keys := make([]string, count)
	for i := range keys {
		keys[i] = fmt.Sprintf("%skey/%04d", prefix, i)
	}
	return keys
}

// R-CLBC-UJ37 R-3539-TPS4 R-DRZU-8I1I R-DQRX-UQAT R-DN48-PF2Q
func TestHostBackupDeletePagesAndPrefix(t *testing.T) {
	type stepName string
	var deleteBackups stepName = ops.StepDeleteBackups
	equal(t, deleteBackups, "delete_backups")
	for _, keep := range []bool{true, false} {
		t.Run(fmt.Sprintf("keep=%t", keep), func(t *testing.T) {
			f := setup(t)
			h, create := f.create()
			ok(t, f.st.Finish(f.ctx, create.ID, store.Update{}))
			d := f.deleteHost(h, keep, author)
			live, old := protocol.HostPrefix(h.Name, h.Seq), protocol.HostPrefix(h.Name, h.Seq-1)
			account := protocol.AccountPrefix("example")
			const total = 2500
			c := &backupDeleteCloud{Cloud: f.memory, f: f, limit: cloud.MaxKeysPerPage, keys: map[string][]string{
				live: backupKeys(live, total), old: backupKeys(old, 1), account: backupKeys(account, 1),
			}}
			c.onDelete = func(_ context.Context, prefix string) error {
				equal(t, prefix, live)
				o := f.operation(d.ID)
				equal(t, o.Step, ops.StepDeleteBackups)
				equal(t, o.Generation, d.Generation)
				return nil
			}
			f.cfg.Cloud = c
			e := ops.New(f.cfg)
			if keep {
				for range 20 {
					o := f.step(e, d.ID)
					equal(t, o.Step == ops.StepDeleteBackups, false)
					if o.Status == store.OpDone {
						break
					}
					equal(t, o.Status, store.OpRunning)
				}
				equal(t, f.operation(d.ID).Status, store.OpDone)
				equal(t, len(c.listed), 0)
				equal(t, len(c.keys[live]), total)
			} else {
				for range 20 {
					o := f.step(e, d.ID)
					if o.Step == ops.StepDeleteBackups {
						break
					}
					equal(t, o.Status, store.OpRunning)
				}
				equal(t, f.operation(d.ID).Step, ops.StepDeleteBackups)
				equal(t, len(c.listed), 0)
				const attempts = 3
				ok(t, f.st.Defer(f.ctx, d.ID, f.now, attempts))
				for _, want := range []int{1500, 500, 0} {
					ok(t, e.Pass(f.ctx))
					o := f.operation(d.ID)
					equal(t, o.Status, store.OpRunning)
					equal(t, o.NextTry, f.now)
					equal(t, o.Attempts, attempts)
					equal(t, len(c.keys[live]), want)
					// A restart uses the stored checkpoint.
					e = ops.New(f.cfg)
				}
				ok(t, e.Pass(f.ctx))
				equal(t, f.operation(d.ID).Status, store.OpDone)
				equal(t, slices.Equal(c.listed, []string{live, live, live, live}), true)
			}
			equal(t, len(c.keys[old]), 1)
			equal(t, len(c.keys[account]), 1)
		})
	}
}

// R-3539-TPS4
func TestHostBackupDeleteShortPageAndMissingBucket(t *testing.T) {
	for _, missing := range []string{"list", "delete"} {
		t.Run(missing, func(t *testing.T) {
			f := setup(t)
			h, create := f.create()
			ok(t, f.st.Finish(f.ctx, create.ID, store.Update{}))
			d := f.deleteHost(h, false, author)
			ok(t, f.st.Advance(f.ctx, d.ID, store.Update{Step: ops.StepDeleteBackups}))
			prefix := protocol.HostPrefix(h.Name, h.Seq)
			c := &backupDeleteCloud{Cloud: f.memory, f: f, limit: 400, keys: map[string][]string{prefix: backupKeys(prefix, 1000)}}
			f.cfg.Cloud = c
			e := ops.New(f.cfg)
			equal(t, f.step(e, d.ID).Status, store.OpRunning)
			equal(t, len(c.keys[prefix]), 600)
			gone := func(context.Context, string) error { return cloudErr(cloud.ClassNotFound) }
			if missing == "list" {
				c.onList = gone
			} else {
				c.onDelete = gone
			}
			equal(t, f.step(e, d.ID).Status, store.OpDone)
			equal(t, len(c.listed), 2)
		})
	}
}

// R-DT7Q-M9S7
func TestRemovedBackupDeleteVisitsEveryDueRowOnce(t *testing.T) {
	f := setup(t)
	c := &backupDeleteCloud{Cloud: f.memory, f: f, limit: cloud.MaxKeysPerPage, keys: map[string][]string{}}
	f.cfg.Cloud = c
	e := ops.New(f.cfg)
	var want []string
	for i := range 120 {
		slug := fmt.Sprintf("removed-%03d", i)
		prefix := protocol.AccountPrefix(slug)
		_, err := f.st.DeleteAccountBackups(f.ctx, slug, author)
		ok(t, err)
		c.keys[prefix] = backupKeys(prefix, 1)
		want = append(want, prefix)
	}
	_, err := f.st.DeleteAccountBackups(f.ctx, "future", author)
	ok(t, err)
	ok(t, f.st.DeferBackupDelete(f.ctx, "future", f.now.Add(time.Minute), 0))
	suffix := fmt.Sprintf("removed-%03d-next", store.BatchSize-1)
	suffixPrefix := protocol.AccountPrefix(suffix)
	lastOnFirstPage := want[store.BatchSize-1]
	c.onDelete = func(_ context.Context, prefix string) error {
		if prefix == lastOnFirstPage {
			_, err := f.st.DeleteAccountBackups(f.ctx, suffix, author)
			ok(t, err)
			c.keys[suffixPrefix] = backupKeys(suffixPrefix, 1)
		}
		return nil
	}
	want = slices.Insert(want, store.BatchSize, suffixPrefix)
	ok(t, e.Pass(f.ctx))
	equal(t, slices.Equal(c.listed, want), true)
	for _, prefix := range want {
		equal(t, len(c.keys[prefix]), 0)
	}
}

// R-36B6-7HIT R-DQRX-UQAT
func TestRemovedBackupDeletePagesBackoffAndRefusal(t *testing.T) {
	f := setup(t)
	slug, short, nearby, later := "acme", "short", "acme2", "later"
	for _, name := range []string{slug, short, "denied", "missing", later, "retry"} {
		_, err := f.st.DeleteAccountBackups(f.ctx, name, author)
		ok(t, err)
	}
	prefix, other, shortPrefix := protocol.AccountPrefix(slug), protocol.AccountPrefix(nearby), protocol.AccountPrefix(short)
	c := &backupDeleteCloud{Cloud: f.memory, f: f, limit: cloud.MaxKeysPerPage, keys: map[string][]string{
		prefix: backupKeys(prefix, 2500), shortPrefix: backupKeys(shortPrefix, 1000), other: backupKeys(other, 1),
	}}
	c.onList = func(_ context.Context, prefix string) error {
		c.limit = cloud.MaxKeysPerPage
		switch prefix {
		case shortPrefix:
			if len(c.keys[prefix]) > 600 {
				c.limit = 400
			}
		case protocol.AccountPrefix("denied"):
			return cloudErr(cloud.ClassRefused)
		case protocol.AccountPrefix("missing"):
			return cloudErr(cloud.ClassNotFound)
		case protocol.AccountPrefix("retry"):
			return cloudErr(cloud.ClassTransient)
		}
		return nil
	}
	ok(t, f.st.DeferBackupDelete(f.ctx, "retry", f.now, 64))
	ok(t, f.st.DeferBackupDelete(f.ctx, slug, f.now, 3))
	f.cfg.Cloud = c
	e := ops.New(f.cfg)
	for pass, want := range []int{1500, 500, 0, 0} {
		ok(t, e.Pass(f.ctx))
		equal(t, len(c.keys[prefix]), want)
		row, err := f.st.BackupsDeleteOf(f.ctx, slug)
		equal(t, errors.Is(err, store.ErrNotFound), pass == 3)
		if pass != 3 {
			ok(t, err)
			equal(t, row.Attempts, 3)
			equal(t, row.NextTry, f.now)
		}
		if pass == 0 {
			equal(t, len(c.keys[shortPrefix]), 600)
		} else {
			equal(t, len(c.keys[shortPrefix]), 0)
		}
	}
	for _, finished := range []string{short, "missing", later} {
		_, err := f.st.BackupsDeleteOf(f.ctx, finished)
		equal(t, errors.Is(err, store.ErrNotFound), true)
	}
	denied, err := f.st.BackupsDeleteOf(f.ctx, "denied")
	ok(t, err)
	equal(t, denied.State, store.DeleteBackupsFailed)
	equal(t, denied.Reason, cloudErr(cloud.ClassRefused).Error())
	retry, err := f.st.BackupsDeleteOf(f.ctx, "retry")
	ok(t, err)
	equal(t, retry.Attempts, 65)
	equal(t, retry.NextTry, f.now.Add(ops.MaxBackoff))
	f.now = f.now.Add(time.Hour)
	c.listed = nil
	ok(t, e.Pass(f.ctx))
	equal(t, slices.Contains(c.listed, protocol.AccountPrefix("denied")), false)
	_, err = f.st.DeleteAccountBackups(f.ctx, "denied", author)
	ok(t, err)
	c.onList = nil
	ok(t, e.Pass(f.ctx))
	_, err = f.st.BackupsDeleteOf(f.ctx, "denied")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	equal(t, len(c.keys[other]), 1)
}

// R-DVNJ-DT9L
func TestRemovedBackupDeleteStoreErrorsAndRestart(t *testing.T) {
	for _, failure := range []string{"conflict", "not-found", "unreachable", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			f := setup(t)
			for _, slug := range []string{"alpha", "beta"} {
				_, err := f.st.DeleteAccountBackups(f.ctx, slug, author)
				ok(t, err)
			}
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			c := &backupDeleteCloud{Cloud: f.memory, f: f, limit: cloud.MaxKeysPerPage, keys: map[string][]string{}}
			c.onList = func(_ context.Context, prefix string) error {
				if prefix != protocol.AccountPrefix("alpha") {
					return nil
				}
				switch failure {
				case "conflict":
					ok(t, f.st.FailBackupDelete(f.ctx, "alpha", "another writer's reason"))
				case "not-found":
					ok(t, f.st.FinishBackupDelete(f.ctx, "alpha"))
				case "unreachable":
					f.db.SetFailing(true)
				case "cancelled":
					cancel()
					return cloudErr(cloud.ClassRefused)
				}
				return nil
			}
			f.cfg.Cloud = c
			err := ops.New(f.cfg).Pass(ctx)
			switch failure {
			case "conflict", "not-found":
				ok(t, err)
				equal(t, len(c.listed), 2)
			case "unreachable":
				f.db.SetFailing(false)
				equal(t, errors.Is(err, store.ErrUnreachable), true)
			case "cancelled":
				equal(t, errors.Is(err, context.Canceled), true)
			}
			if failure == "unreachable" || failure == "cancelled" {
				row, err := f.st.BackupsDeleteOf(f.ctx, "alpha")
				ok(t, err)
				equal(t, row.State, store.DeleteBackupsDeleting)
				equal(t, row.Reason, "")
				c.onList = nil
				restart, stop := context.WithCancel(f.ctx)
				f.cfg.After = func(time.Duration) <-chan time.Time { stop(); return nil }
				ok(t, ops.New(f.cfg).Run(restart))
				_, err = f.st.BackupsDeleteOf(f.ctx, "alpha")
				equal(t, errors.Is(err, store.ErrNotFound), true)
			}
			equal(t, len(f.events()), 0)
		})
	}
}

// R-36B6-7HIT
func TestRemovedBackupDeleteClassifiesDeleteKeysErrors(t *testing.T) {
	for _, class := range []cloud.Class{cloud.ClassNotFound, cloud.ClassTransient, cloud.ClassRefused, cloud.ClassMismatch} {
		t.Run(fmt.Sprint(class), func(t *testing.T) {
			f := setup(t)
			const slug = "tenant"
			_, err := f.st.DeleteAccountBackups(f.ctx, slug, author)
			ok(t, err)
			_, err = f.st.DeleteAccountBackups(f.ctx, "z-following", author)
			ok(t, err)
			prefix := protocol.AccountPrefix(slug)
			c := &backupDeleteCloud{Cloud: f.memory, f: f, limit: cloud.MaxKeysPerPage, keys: map[string][]string{prefix: backupKeys(prefix, 1)}}
			failure := cloudErr(class)
			c.onDelete = func(_ context.Context, actual string) error {
				equal(t, actual, prefix)
				return failure
			}
			f.cfg.Cloud = c
			e := ops.New(f.cfg)
			ok(t, e.Pass(f.ctx))
			row, err := f.st.BackupsDeleteOf(f.ctx, slug)
			switch class {
			case cloud.ClassNotFound:
				equal(t, errors.Is(err, store.ErrNotFound), true)
			case cloud.ClassTransient:
				ok(t, err)
				equal(t, row.State, store.DeleteBackupsDeleting)
				equal(t, row.Attempts, 1)
				equal(t, row.NextTry, f.now.Add(ops.PollInterval))
				equal(t, row.Reason, "")
			default:
				ok(t, err)
				equal(t, row.State, store.DeleteBackupsFailed)
				equal(t, row.Reason, failure.Error())
			}
			_, err = f.st.BackupsDeleteOf(f.ctx, "z-following")
			equal(t, errors.Is(err, store.ErrNotFound), true)
			equal(t, len(f.events()), 0)
		})
	}
}

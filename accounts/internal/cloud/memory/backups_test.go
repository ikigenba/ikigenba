package memory_test

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/access"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
)

func backupRoleRequest(f *fixture, prefix string) cloud.RoleRequest {
	return cloud.RoleRequest{
		RoleARN:     access.RoleARN("aws", memory.Account, f.cfg.Hosts),
		SessionName: access.SessionName("host01", "acme"),
		Policy:      access.Policy(f.cfg.Hosts.BackupBucket(), "aws", prefix),
		Duration:    cloud.RoleSessionDuration,
	}
}

func backupCredential(t *testing.T, f *fixture, prefix string) protocol.Credentials {
	t.Helper()
	c, err := f.c.AssumeRole(t.Context(), backupRoleRequest(f, prefix))
	ok(t, err)
	return protocol.Credentials{
		AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, SessionToken: c.SessionToken, Expires: c.Expires,
	}
}

func TestBackupIdentity(t *testing.T) {
	// R-CTTE-SQFC
	f := setup(t)
	var s cloud.STS = f.c
	caller, err := s.Identity(t.Context())
	ok(t, err)
	equal(t, caller, cloud.Caller{Account: memory.Account,
		Arn: "arn:aws:sts::" + memory.Account + ":assumed-role/sandbox-accounts/sandbox", UserID: "AROASANDBOX:sandbox"})
}

func TestAssumeBackupRole(t *testing.T) {
	// R-CV1B-6I61
	f := setup(t)
	f.now = f.now.In(time.FixedZone("other", 7200)).Add(750 * time.Millisecond)
	stream := append(make([]byte, 32), bytes.Repeat([]byte{0xaf, 0x12}, 4)...)
	stream = append(stream, bytes.Repeat([]byte{0xfd, 0x91}, 4)...)
	stream = append(stream, bytes.Repeat([]byte{0x23, 0xce}, 4)...)
	f.cfg.Rand = bytes.NewReader(stream)
	var err error
	f.c, err = memory.New(f.cfg)
	ok(t, err)

	req := backupRoleRequest(f, protocol.AccountPrefix("acme"))
	got, err := f.c.AssumeRole(t.Context(), req)
	ok(t, err)
	equal(t, got.AccessKeyID, "ASIAAF12AF12AF12AF12")
	equal(t, got.Expires, f.now.UTC().Truncate(time.Second).Add(time.Hour))
	if got.SecretAccessKey == "" || got.SessionToken == "" {
		t.Fatal("role credentials lack secret or token")
	}

	req.Policy = access.Policy(f.cfg.Hosts.BackupBucket(), "aws", "hosts/host01/1/")
	req.SessionName = strings.Repeat("a", 54) + "Z09_+=,.@-"
	req.Duration = 900 * time.Second
	got, err = f.c.AssumeRole(t.Context(), req)
	ok(t, err)
	equal(t, got.AccessKeyID, "ASIAFD91FD91FD91FD91")
	equal(t, got.Expires, f.now.UTC().Truncate(time.Second).Add(900*time.Second))
	if got.SecretAccessKey == "" || got.SessionToken == "" {
		t.Fatal("role credentials lack secret or token")
	}

	bucket := f.cfg.Hosts.BackupBucket()
	prefix := "accounts/"
	base := access.Policy(bucket, "aws", prefix)
	prefix += strings.Repeat("x", (cloud.MaxPolicyBytes-len(base))/2-1) + "/"
	req.Policy = access.Policy(bucket, "aws", prefix)
	req.SessionName = "a0"
	got, err = f.c.AssumeRole(t.Context(), req)
	ok(t, err)
	equal(t, got.AccessKeyID, "ASIA23CE23CE23CE23CE")
	equal(t, got.Expires, f.now.UTC().Truncate(time.Second).Add(900*time.Second))
	if got.SecretAccessKey == "" || got.SessionToken == "" {
		t.Fatal("role credentials lack secret or token")
	}
}

func TestAssumeBackupRoleRefusals(t *testing.T) {
	// R-CW97-K9WQ R-DJFA-TWZX
	f := setup(t)
	bucket, prefix := f.cfg.Hosts.BackupBucket(), protocol.AccountPrefix("acme")
	for _, tc := range []struct {
		name, code string
		mutate     func(*cloud.RoleRequest)
	}{
		{"role before other validation", "AccessDenied", func(r *cloud.RoleRequest) { r.RoleARN, r.SessionName = "other", "" }},
		{"session too short", "ValidationError", func(r *cloud.RoleRequest) { r.SessionName = "a" }},
		{"session too long", "ValidationError", func(r *cloud.RoleRequest) { r.SessionName = strings.Repeat("x", 65) }},
		{"session punctuation", "ValidationError", func(r *cloud.RoleRequest) { r.SessionName = "a/b" }},
		{"session non-ASCII", "ValidationError", func(r *cloud.RoleRequest) { r.SessionName = "éa" }},
		{"duration too short", "ValidationError", func(r *cloud.RoleRequest) { r.Duration = 899 * time.Second }},
		{"duration too long", "ValidationError", func(r *cloud.RoleRequest) { r.Duration = 3601 * time.Second }},
		{"duration fraction", "ValidationError", func(r *cloud.RoleRequest) { r.Duration = 900*time.Second + time.Millisecond }},
		{"policy too long", "ValidationError", func(r *cloud.RoleRequest) { r.Policy = strings.Repeat("x", cloud.MaxPolicyBytes+1) }},
		{"policy empty", "MalformedPolicyDocument", func(r *cloud.RoleRequest) { r.Policy = "" }},
		{"policy extra space", "MalformedPolicyDocument", func(r *cloud.RoleRequest) { r.Policy += " " }},
		{"policy wrong partition", "MalformedPolicyDocument", func(r *cloud.RoleRequest) { r.Policy = access.Policy(bucket, "aws-us-gov", prefix) }},
		{"policy wrong bucket", "MalformedPolicyDocument", func(r *cloud.RoleRequest) { r.Policy = access.Policy("other", "aws", prefix) }},
		{"policy wrong prefix", "MalformedPolicyDocument", func(r *cloud.RoleRequest) { r.Policy = access.Policy(bucket, "aws", "other/") }},
		{"policy prefix not terminated", "MalformedPolicyDocument", func(r *cloud.RoleRequest) { r.Policy = access.Policy(bucket, "aws", "accounts/acme") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := backupRoleRequest(f, prefix)
			tc.mutate(&r)
			got, err := f.c.AssumeRole(t.Context(), r)
			equal(t, got, cloud.Credentials{})
			checkErr(t, err, "sts", "AssumeRole", "", tc.code, cloud.ClassRefused)
		})
	}
}

func TestBackupSTSLeavesDiskAndHeapUnchanged(t *testing.T) {
	// R-CXH3-Y1NF
	f := setup(t)
	f.instance(t)
	before, err := os.ReadFile(f.cfg.Path)
	ok(t, err)
	r := backupRoleRequest(f, protocol.AccountPrefix("acme"))
	var initial, final runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initial)
	for range 10_000 {
		_, err := f.c.Identity(t.Context())
		ok(t, err)
		_, err = f.c.AssumeRole(t.Context(), r)
		ok(t, err)
	}
	runtime.GC()
	runtime.ReadMemStats(&final)
	if final.HeapInuse >= initial.HeapInuse+(1<<20) {
		t.Fatalf("heap in use grew by %d bytes", final.HeapInuse-initial.HeapInuse)
	}
	after, err := os.ReadFile(f.cfg.Path)
	ok(t, err)
	equal(t, after, before)
}

func TestPutWithScopedCredential(t *testing.T) {
	// R-CSLI-EYON R-CYP0-BTE4 R-DJFA-TWZX
	f := setup(t)
	bucket, prefix := f.cfg.Hosts.BackupBucket(), protocol.AccountPrefix("acme")
	key := prefix + "files/config"
	c := backupCredential(t, f, prefix)
	var writer interface {
		Put(protocol.Credentials, string) error
	} = f.c
	claimed := c
	claimed.Expires = time.Time{}
	ok(t, writer.Put(claimed, key))
	ok(t, writer.Put(c, key))
	checkDenied := func(c protocol.Credentials, key string) {
		t.Helper()
		checkErr(t, writer.Put(c, key), "s3", "PutObject", bucket, "AccessDenied", cloud.ClassRefused)
	}
	checkDenied(c, "accounts/acmf/files/config")
	checkDenied(c, "hosts/host01/1/x")
	withChange := c
	withChange.AccessKeyID = flipLastByte(c.AccessKeyID)
	checkDenied(withChange, prefix+"different-id")
	withChange = c
	withChange.SecretAccessKey = flipLastByte(c.SecretAccessKey)
	checkDenied(withChange, prefix+"different-secret")
	withChange = c
	withChange.SessionToken = flipLastByte(c.SessionToken)
	checkDenied(withChange, prefix+"different-token")

	f.now = c.Expires.Add(-time.Nanosecond)
	ok(t, writer.Put(c, key))
	f.now = c.Expires
	claimed.Expires = f.now.Add(time.Hour)
	for _, name := range []string{prefix + "late", "other/not-in-scope"} {
		checkErr(t, writer.Put(claimed, name), "s3", "PutObject", bucket, "ExpiredToken", cloud.ClassRefused)
	}
	checkDenied(withChange, prefix+"invalid-expired")
	equal(t, listBackupKeys(t, f.c, bucket, ""), []string{key})
}

func flipLastByte(s string) string {
	return s[:len(s)-1] + string(s[len(s)-1]^1)
}

func listBackupKeys(t *testing.T, c cloud.S3, bucket, prefix string) []string {
	t.Helper()
	var all []string
	ok(t, c.ListKeys(t.Context(), bucket, prefix, func(keys []string) bool {
		all = append(all, keys...)
		return true
	}))
	return all
}

func TestBackupBucketPagesAndPrefixes(t *testing.T) {
	// R-DI7E-G598 R-D14T-3CVI R-DJFA-TWZX
	f := setup(t)
	ctx := t.Context()
	bucket, prefix := f.cfg.Hosts.BackupBucket(), protocol.AccountPrefix("acme")
	other := protocol.AccountPrefix("acmf")
	p := backupCredential(t, f, prefix)
	pOther := backupCredential(t, f, other)

	ok(t, f.c.ListKeys(ctx, bucket, "", func([]string) bool {
		t.Fatal("listing an empty bucket called the callback")
		return false
	}))
	has, err := f.c.HasKeys(ctx, bucket, prefix)
	ok(t, err)
	equal(t, has, false)

	total := cloud.MaxKeysPerPage + 1
	want := make([]string, total)
	for i := total - 1; i >= 0; i-- {
		want[i] = fmt.Sprintf("%sfiles/%05d", prefix, i)
		ok(t, f.c.Put(p, want[i]))
	}
	ok(t, f.c.Put(pOther, other+"file"))
	var sizes []int
	var got []string
	ok(t, f.c.ListKeys(ctx, bucket, prefix, func(page []string) bool {
		sizes = append(sizes, len(page))
		got = append(got, page...)
		return true
	}))
	equal(t, sizes, []int{cloud.MaxKeysPerPage, 1})
	equal(t, got, want)

	var calls int
	ok(t, f.c.ListKeys(ctx, bucket, prefix, func(page []string) bool {
		calls++
		equal(t, page, want[:cloud.MaxKeysPerPage])
		return false
	}))
	equal(t, calls, 1)
	for _, name := range []string{"", prefix, other, want[0]} {
		has, err := f.c.HasKeys(ctx, bucket, name)
		ok(t, err)
		equal(t, has, true)
	}
	has, err = f.c.HasKeys(ctx, bucket, prefix+"missing")
	ok(t, err)
	equal(t, has, false)
	missing := "nonexistent"
	err = f.c.ListKeys(ctx, missing, prefix, func([]string) bool {
		t.Fatal("listing another bucket called the callback")
		return false
	})
	checkErr(t, err, "s3", "ListObjectsV2", missing, "NoSuchBucket", cloud.ClassNotFound)
	_, err = f.c.HasKeys(ctx, missing, prefix)
	checkErr(t, err, "s3", "ListObjectsV2", missing, "NoSuchBucket", cloud.ClassNotFound)
	checkErr(t, f.c.DeleteKeys(ctx, missing, []string{want[0]}), "s3", "DeleteObjects", missing, "NoSuchBucket", cloud.ClassNotFound)
	equal(t, listBackupKeys(t, f.c, bucket, prefix), want)
}

func TestDeleteBackupKeysAndReopen(t *testing.T) {
	// R-D2CP-H4M7 R-D3KL-UWCW R-DJFA-TWZX
	f := setup(t)
	ctx := t.Context()
	bucket, prefix := f.cfg.Hosts.BackupBucket(), protocol.AccountPrefix("acme")
	c := backupCredential(t, f, prefix)
	first, second, third := prefix+"first", prefix+"second\xff", prefix+"third"
	ok(t, f.c.Put(c, second))
	ok(t, f.c.Put(c, first))
	ok(t, f.c.Put(c, third))
	tooMany := make([]string, cloud.MaxKeysPerPage+1)
	tooMany[0] = first
	for _, name := range []string{bucket, "missing"} {
		for _, keys := range [][]string{nil, tooMany} {
			checkErr(t, f.c.DeleteKeys(ctx, name, keys), "s3", "DeleteObjects", name, "MalformedXML", cloud.ClassRefused)
		}
	}
	equal(t, listBackupKeys(t, f.c, bucket, prefix), []string{first, second, third})

	keys := make([]string, cloud.MaxKeysPerPage)
	keys[0], keys[1], keys[2] = first, third, first
	ok(t, f.c.DeleteKeys(ctx, bucket, keys))
	ok(t, f.c.DeleteKeys(ctx, bucket, []string{first, third, prefix + "missing"}))
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	for _, cc := range []cloud.S3{f.c, reopened} {
		equal(t, listBackupKeys(t, cc, bucket, prefix), []string{second})
		for _, key := range []string{first, second, third} {
			has, err := cc.HasKeys(ctx, bucket, key)
			ok(t, err)
			equal(t, has, key == second)
		}
	}
}

func TestReopenedBackupBucketNeedsCredentialsFromNewReceiver(t *testing.T) {
	// R-CYP0-BTE4 R-D3KL-UWCW
	f := setup(t)
	ctx := t.Context()
	bucket, prefix := f.cfg.Hosts.BackupBucket(), protocol.AccountPrefix("acme")
	draw := bytes.Repeat([]byte{0xa3}, 8)
	f.cfg.Rand = bytes.NewReader(append(make([]byte, 32), draw...))
	var err error
	f.c, err = memory.New(f.cfg)
	ok(t, err)
	old := backupCredential(t, f, prefix)
	key := prefix + "kept"
	ok(t, f.c.Put(old, key))

	cfg := f.cfg
	cfg.Rand = bytes.NewReader(draw)
	reopened, err := memory.New(cfg)
	ok(t, err)
	equal(t, listBackupKeys(t, reopened, bucket, prefix), []string{key})
	checkOld := func() {
		t.Helper()
		checkErr(t, reopened.Put(old, prefix+"refused"), "s3", "PutObject", bucket, "AccessDenied", cloud.ClassRefused)
	}
	checkOld()
	got, err := reopened.AssumeRole(ctx, backupRoleRequest(f, prefix))
	ok(t, err)
	equal(t, got.AccessKeyID, old.AccessKeyID)
	checkOld()
	newKey := prefix + "new"
	ok(t, reopened.Put(protocol.Credentials{
		AccessKeyID: got.AccessKeyID, SecretAccessKey: got.SecretAccessKey, SessionToken: got.SessionToken,
	}, newKey))
	equal(t, listBackupKeys(t, reopened, bucket, prefix), []string{key, newKey})
}

func TestBackupBucketRollsBackOnFailedWrite(t *testing.T) {
	// R-GV9R-543N
	f := setup(t)
	ctx := t.Context()
	bucket, prefix := f.cfg.Hosts.BackupBucket(), protocol.AccountPrefix("acme")
	cred := backupCredential(t, f, prefix)
	kept, added := prefix+"kept", prefix+"added"
	ok(t, f.c.Put(cred, kept))
	backup := f.cfg.Path + ".backup"
	ok(t, os.Rename(f.cfg.Path, backup))
	ok(t, os.Mkdir(f.cfg.Path, 0700)) // Block the final rename after the temporary write.
	checkErr(t, f.c.Put(cred, added), "s3", "PutObject", bucket, cloud.CodeNoResponse, cloud.ClassTransient)
	equal(t, listBackupKeys(t, f.c, bucket, prefix), []string{kept})
	checkErr(t, f.c.DeleteKeys(ctx, bucket, []string{kept}), "s3", "DeleteObjects", bucket, cloud.CodeNoResponse, cloud.ClassTransient)
	equal(t, listBackupKeys(t, f.c, bucket, prefix), []string{kept})
	ok(t, os.Remove(f.cfg.Path))
	ok(t, os.Rename(backup, f.cfg.Path))
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	equal(t, listBackupKeys(t, reopened, bucket, prefix), []string{kept})
}

func TestCloudErrorSubjects(t *testing.T) {
	// R-DJFA-TWZX
	f := setup(t)
	ctx := t.Context()
	_, err := f.c.RunInstance(ctx, cloud.Launch{})
	checkEC2(t, err, "RunInstances", "", "UnauthorizedOperation", cloud.ClassRefused)
	_, err = f.c.AllocateAddress(ctx, nil)
	checkEC2(t, err, "AllocateAddress", "", cloud.CodeTagContract, cloud.ClassRefused)
	_, err = f.c.CreateVolume(ctx, cloud.VolumeSpec{})
	checkEC2(t, err, "CreateVolume", "", cloud.CodeTagContract, cloud.ClassRefused)

	checkEC2(t, f.c.FindInstances(ctx, nil, nil), "DescribeInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
	checkEC2(t, f.c.FindVolumes(ctx, nil, nil), "DescribeVolumes", "", cloud.CodeTagContract, cloud.ClassRefused)
	checkEC2(t, f.c.FindAddresses(ctx, nil, nil), "DescribeAddresses", "", cloud.CodeTagContract, cloud.ClassRefused)
	_, err = f.c.DescribeInstance(ctx, "i-unknown")
	checkEC2(t, err, "DescribeInstances", "i-unknown", "InvalidInstanceID.NotFound", cloud.ClassNotFound)
	_, err = f.c.DescribeVolume(ctx, "vol-unknown")
	checkEC2(t, err, "DescribeVolumes", "vol-unknown", "InvalidVolume.NotFound", cloud.ClassNotFound)
	_, err = f.c.AssociateAddress(ctx, "alloc-unknown", "i-unknown")
	checkEC2(t, err, "AssociateAddress", "alloc-unknown", "InvalidInstanceID.NotFound", cloud.ClassNotFound)
	_, err = f.c.Metadata("i-unknown").Instance(ctx)
	checkEC2(t, err, "DescribeInstances", "i-unknown", "InvalidInstanceID.NotFound", cloud.ClassNotFound)

	_, err = f.c.FindRecords(ctx, "wrong-zone", "lookup.domain")
	checkErr(t, err, "route53", "ListResourceRecordSets", "lookup.domain", "NoSuchHostedZone", cloud.ClassRefused)
	_, err = f.c.ChangeRecord(ctx, "wrong-zone", cloud.RecordChange{Name: "change.domain"})
	checkErr(t, err, "route53", "ChangeResourceRecordSets", "change.domain", "NoSuchHostedZone", cloud.ClassRefused)
	_, err = f.c.ChangeInsync(ctx, "C-unknown")
	checkErr(t, err, "route53", "GetChange", "C-unknown", "NoSuchChange", cloud.ClassNotFound)
	_, err = f.c.CallerOf(ctx, "invalid", "binding")
	checkErr(t, err, "sts", "GetCallerIdentity", "", "HTTP 403", cloud.ClassRefused)
}

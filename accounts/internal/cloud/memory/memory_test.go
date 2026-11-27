package memory_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
)

type fixture struct {
	c   *memory.Cloud
	cfg memory.Config
	now time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 1, 4, 3, 2, 1, 0, time.UTC)}
	f.cfg = memory.Config{Path: filepath.Join(t.TempDir(), "cloud.json"), Now: func() time.Time { return f.now },
		Rand: rand.NewChaCha8([32]byte{42}), Hosts: settings.SandboxHosts}
	c, err := memory.New(f.cfg)
	ok(t, err)
	f.c = c
	return f
}

func (f *fixture) launch() cloud.Launch {
	return cloud.Launch{Template: f.cfg.Hosts.LaunchTemplate, InstanceProfile: f.cfg.Hosts.InstanceProfile,
		Type: "t4g.medium", RootGB: 24, Tags: cloud.Tags{cloud.TagKind: cloud.KindHost, "X": "shared"},
		InstanceTags: cloud.Tags{cloud.TagHost: "host", "X": "instance"}}
}

func (f *fixture) spec() cloud.VolumeSpec {
	return cloud.VolumeSpec{AZ: f.cfg.Hosts.Region + "a", SizeGB: 35, Tags: cloud.Tags{cloud.TagKind: cloud.KindHostData}}
}

func (f *fixture) instance(t *testing.T) cloud.Instance {
	t.Helper()
	i, err := f.c.RunInstance(t.Context(), f.launch())
	ok(t, err)
	return i
}

func (f *fixture) volume(t *testing.T) cloud.Volume {
	t.Helper()
	v, err := f.c.CreateVolume(t.Context(), f.spec())
	ok(t, err)
	return v
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func checkErr(t *testing.T, err error, service, operation, subject, code string, class cloud.Class) {
	t.Helper()
	var got *cloud.Error
	if !errors.As(err, &got) || got.Service != service || got.Operation != operation || got.Subject != subject || got.Code != code || got.Class != class {
		t.Fatalf("error = %#v (%v), want %s %s %s %s class %v", got, err, service, operation, subject, code, class)
	}
}

func checkEC2(t *testing.T, err error, operation, subject, code string, class cloud.Class) {
	t.Helper()
	checkErr(t, err, "ec2", operation, subject, code, class)
}

func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDeclarationsAndEmptyLoad(t *testing.T) {
	// R-UAOO-HI8J R-UBWK-V9Z8 R-UD4H-91PX
	f := setup(t)
	var c cloud.Cloud = f.c
	p := f.c.Presigner("i-name")
	m := f.c.Metadata("i-name")
	if p == nil || m == nil {
		t.Fatal("missing signer or metadata")
	}
	equal(t, []time.Duration{memory.BootDelay, memory.StopDelay, memory.TerminateDelay, memory.VolumeDelay, memory.ModifyDelay, memory.ChangeDelay},
		[]time.Duration{20 * time.Second, 10 * time.Second, 10 * time.Second, 5 * time.Second, 10 * time.Second, 5 * time.Second})
	equal(t, memory.PageSize, 50)
	equal(t, memory.MaxModifications, 4)
	equal(t, memory.Account, "000000000000")
	equal(t, memory.AMI, "ami-sandbox")
	empty := func() bool {
		t.Error("missing file yielded a resource")
		return false
	}
	ctx := t.Context()
	ok(t, c.FindInstances(ctx, f.launch().Tags, func([]cloud.Instance) bool { return empty() }))
	ok(t, c.FindAddresses(ctx, f.launch().Tags, func([]cloud.Address) bool { return empty() }))
	ok(t, c.FindVolumes(ctx, f.spec().Tags, func([]cloud.Volume) bool { return empty() }))
	records, err := c.FindRecords(ctx, f.cfg.Hosts.ZoneID, "a."+f.cfg.Hosts.Domain)
	ok(t, err)
	equal(t, len(records), 0)
	cfg := f.cfg
	cfg.Path = t.TempDir() // Opening succeeds, but reading an enclosing directory does not.
	if c, err := memory.New(cfg); err == nil || c != nil {
		t.Fatalf("directory load = %v, %v", c, err)
	}
	ok(t, os.WriteFile(f.cfg.Path, []byte("not JSON"), 0600))
	if c, err := memory.New(f.cfg); err == nil || c != nil {
		t.Fatalf("invalid file load = %v, %v", c, err)
	}
}

func TestPersistenceAndFailedWrite(t *testing.T) {
	// R-GV9R-543N
	f := setup(t)
	ctx := t.Context()
	l, spec := f.launch(), f.spec()
	l.ClientToken, spec.ClientToken = "launch", "volume"
	i, err := f.c.RunInstance(ctx, l)
	ok(t, err)
	v, err := f.c.CreateVolume(ctx, spec)
	ok(t, err)
	a, err := f.c.AllocateAddress(ctx, l.Tags)
	ok(t, err)
	f.now = f.now.Add(memory.BootDelay)
	ok(t, f.c.AttachVolume(ctx, v.ID, i.ID, "/dev/sdf"))
	ok(t, f.c.ModifyVolume(ctx, v.ID, 70))
	assoc, err := f.c.AssociateAddress(ctx, a.AllocationID, i.ID)
	ok(t, err)
	name, zone := "a."+f.cfg.Hosts.Domain, f.cfg.Hosts.ZoneID
	change, err := f.c.ChangeRecord(ctx, zone, cloud.RecordChange{Action: cloud.RecordCreate, Name: name, IP: a.IP, TTL: 60})
	ok(t, err)
	f.now = f.now.Add(memory.VolumeDelay)
	presigned, err := f.c.Presigner(i.ID).PresignCallerIdentity(ctx, "accounts.test", time.Minute)
	ok(t, err)
	record, err := f.c.FindRecords(ctx, zone, name)
	ok(t, err)
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	described, err := reopened.DescribeInstance(ctx, i.ID)
	ok(t, err)
	original, err := f.c.DescribeInstance(ctx, i.ID)
	ok(t, err)
	equal(t, described, original)
	retry, err := reopened.RunInstance(ctx, l)
	ok(t, err)
	equal(t, retry, original)
	attached, err := reopened.DescribeVolume(ctx, v.ID)
	ok(t, err)
	before, err := f.c.DescribeVolume(ctx, v.ID)
	ok(t, err)
	equal(t, attached, before)
	retryVolume, err := reopened.CreateVolume(ctx, spec)
	ok(t, err)
	equal(t, retryVolume, before)
	mod, err := reopened.VolumeModification(ctx, v.ID)
	ok(t, err)
	equal(t, mod, cloud.Modification{State: cloud.ModificationModifying, TargetGB: 70})
	associated, err := reopened.DescribeAddress(ctx, a.AllocationID)
	ok(t, err)
	equal(t, associated.AssociationID, assoc)
	equal(t, associated.InstanceID, i.ID)
	again, err := reopened.FindRecords(ctx, zone, name)
	ok(t, err)
	equal(t, again, record)
	synced, err := reopened.ChangeInsync(ctx, change)
	ok(t, err)
	equal(t, synced, true)
	ok(t, reopened.FindInstances(ctx, l.Tags, func(page []cloud.Instance) bool {
		// The instance overrides one shared key.
		t.Errorf("filter matched %+v", page)
		return false
	}))
	ok(t, reopened.FindInstances(ctx, l.InstanceTags, func(page []cloud.Instance) bool {
		equal(t, page, []cloud.Instance{original})
		return true
	}))
	ok(t, reopened.FindVolumes(ctx, spec.Tags, func(page []cloud.Volume) bool {
		equal(t, page, []cloud.Volume{before})
		return true
	}))
	ok(t, reopened.FindAddresses(ctx, l.Tags, func(page []cloud.Address) bool {
		equal(t, page, []cloud.Address{associated})
		return true
	}))
	presignedAgain, err := reopened.Presigner(i.ID).PresignCallerIdentity(ctx, "accounts.test", time.Minute)
	ok(t, err)
	equal(t, presignedAgain, presigned)

	backup := f.cfg.Path + ".backup"
	ok(t, os.Rename(f.cfg.Path, backup))
	ok(t, os.Mkdir(f.cfg.Path, 0700)) // Prevent the final rename, after creating a temp file.
	checkEC2(t, reopened.StopInstance(ctx, i.ID), "StopInstances", i.ID, cloud.CodeNoResponse, cloud.ClassTransient)
	described, err = reopened.DescribeInstance(ctx, i.ID)
	ok(t, err)
	equal(t, described, original)
	checkEC2(t, reopened.TagResource(ctx, v.ID, cloud.Tags{"rollback": "lost"}), "CreateTags", v.ID, cloud.CodeNoResponse, cloud.ClassTransient)
	attached, err = reopened.DescribeVolume(ctx, v.ID)
	ok(t, err)
	equal(t, attached, before)
	files, err := os.ReadDir(filepath.Dir(f.cfg.Path))
	ok(t, err)
	equal(t, len(files), 2)
	ok(t, os.Remove(f.cfg.Path))
	ok(t, os.Rename(backup, f.cfg.Path))
	ok(t, reopened.StopInstance(ctx, i.ID))
	saved, err := memory.New(f.cfg)
	ok(t, err)
	state, err := saved.DescribeInstance(ctx, i.ID)
	ok(t, err)
	equal(t, state.State, cloud.InstanceStopping)
}

func TestLaunchValidation(t *testing.T) {
	// R-KI3O-9NDN
	f := setup(t)
	ctx := t.Context()
	valid := f.launch()
	valid.ClientToken = "used"
	original, err := f.c.RunInstance(ctx, valid)
	ok(t, err)
	for _, test := range []struct {
		name string
		code string
		edit func(*cloud.Launch)
	}{
		{name: "template", code: "UnauthorizedOperation", edit: func(l *cloud.Launch) { l.Template = "other" }},
		{name: "profile", code: "UnauthorizedOperation", edit: func(l *cloud.Launch) { l.InstanceProfile = "other" }},
		{name: "type", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { l.Type = "r8g.large" }},
		{name: "no kind", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { delete(l.Tags, cloud.TagKind) }},
		{name: "extra kind", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { l.InstanceTags[cloud.TagKind] = "" }},
		{name: "shared space", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { l.Tags["Space"] = "" }},
		{name: "shared domain", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { l.Tags["Domain"] = "" }},
		{name: "instance space", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { l.InstanceTags["Space"] = "" }},
		{name: "instance domain", code: cloud.CodeTagContract, edit: func(l *cloud.Launch) { l.InstanceTags["Domain"] = "" }},
		{name: "bad template and type", code: "UnauthorizedOperation", edit: func(l *cloud.Launch) {
			l.Template = "other"
			l.Type = "r8g.large"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, token := range []string{"unused", valid.ClientToken} {
				l := f.launch()
				l.ClientToken = token
				test.edit(&l)
				_, err := f.c.RunInstance(ctx, l)
				checkEC2(t, err, "RunInstances", "", test.code, cloud.ClassRefused)
			}
		})
	}
	var instances []cloud.Instance
	ok(t, f.c.FindInstances(ctx, cloud.Tags{"X": valid.InstanceTags["X"]}, func(page []cloud.Instance) bool {
		instances = append(instances, page...)
		return true
	}))
	equal(t, instances, []cloud.Instance{original})
	var volumeIDs []string
	ok(t, f.c.FindVolumes(ctx, cloud.Tags{"X": valid.Tags["X"]}, func(page []cloud.Volume) bool {
		for _, v := range page {
			volumeIDs = append(volumeIDs, v.ID)
		}
		return true
	}))
	equal(t, volumeIDs, []string{original.RootVolume})
	again, err := f.c.RunInstance(ctx, valid)
	ok(t, err)
	equal(t, again, original)
}

func TestLaunchContractAndTokens(t *testing.T) {
	// R-UMVO-B7NH R-KJBK-NF4C
	f := setup(t)
	ctx := t.Context()
	l := f.launch()
	l.ClientToken = "token"
	i, err := f.c.RunInstance(ctx, l)
	ok(t, err)
	equal(t, i.State, cloud.InstancePending)
	equal(t, i.Type, l.Type)
	equal(t, i.AZ, f.cfg.Hosts.Region+"a")
	equal(t, i.Tags, cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagHost: "host", "X": "instance"})
	root, err := f.c.DescribeVolume(ctx, i.RootVolume)
	ok(t, err)
	equal(t, root.SizeGB, l.RootGB)
	equal(t, root.Tags, l.Tags)
	equal(t, root.Attachment, cloud.Attachment{InstanceID: i.ID, Device: "/dev/xvda", State: cloud.AttachmentAttached})
	f.now = f.now.Add(memory.BootDelay)
	again, err := f.c.RunInstance(ctx, l)
	ok(t, err)
	equal(t, again.State, cloud.InstanceRunning)
	equal(t, again.ID, i.ID)
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	saved, err := reopened.RunInstance(ctx, l)
	ok(t, err)
	equal(t, saved, again)
	for _, edit := range []func(*cloud.Launch){
		func(l *cloud.Launch) { l.Type = "m7g.large" },
		func(l *cloud.Launch) { l.RootGB++ },
		func(l *cloud.Launch) { l.Tags["X"] = "different" },
		func(l *cloud.Launch) { l.InstanceTags["X"] = "different" },
	} {
		changed := f.launch()
		changed.ClientToken = l.ClientToken
		edit(&changed)
		_, err := reopened.RunInstance(ctx, changed)
		checkEC2(t, err, "RunInstances", "", "IdempotentParameterMismatch", cloud.ClassMismatch)
	}
	for _, edit := range []func(*settings.Hosts){
		func(hosts *settings.Hosts) { hosts.LaunchTemplate += "-changed" },
		func(hosts *settings.Hosts) { hosts.InstanceProfile += "-changed" },
	} {
		cfg := f.cfg
		edit(&cfg.Hosts)
		reopened, err := memory.New(cfg)
		ok(t, err)
		_, err = reopened.RunInstance(ctx, l)
		// R-KI3O-9NDN: The old parameters match the persisted token but are
		// unauthorized under the current configuration.
		checkEC2(t, err, "RunInstances", "", "UnauthorizedOperation", cloud.ClassRefused)
		changed := l
		changed.Template, changed.InstanceProfile = cfg.Hosts.LaunchTemplate, cfg.Hosts.InstanceProfile
		_, err = reopened.RunInstance(ctx, changed)
		checkEC2(t, err, "RunInstances", "", "IdempotentParameterMismatch", cloud.ClassMismatch)
	}
	var found []cloud.Instance
	persisted, err := memory.New(f.cfg)
	ok(t, err)
	ok(t, persisted.FindInstances(ctx, cloud.Tags{cloud.TagKind: cloud.KindHost}, func(page []cloud.Instance) bool {
		found = append(found, page...)
		return true
	}))
	equal(t, found, []cloud.Instance{again})
	var roots []cloud.Volume
	ok(t, persisted.FindVolumes(ctx, l.Tags, func(page []cloud.Volume) bool {
		roots = append(roots, page...)
		return true
	}))
	equal(t, roots, []cloud.Volume{root})
}

func TestLifecycleAndTerminatedResources(t *testing.T) {
	// R-UFKA-0L7B R-URR9-UAM9 R-GYXG-AFBQ R-FWC7-9WDS
	f := setup(t)
	ctx := t.Context()
	i := f.instance(t)
	v := f.volume(t)
	a, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	instanceState := func(want string) {
		t.Helper()
		actual, err := f.c.DescribeInstance(ctx, i.ID)
		ok(t, err)
		equal(t, actual.State, want)
	}
	volumeState := func(want string, attachment cloud.Attachment) {
		t.Helper()
		actual, err := f.c.DescribeVolume(ctx, v.ID)
		ok(t, err)
		equal(t, actual.State, want)
		equal(t, actual.Attachment, attachment)
	}
	step := time.Nanosecond
	f.now = f.now.Add(memory.VolumeDelay - step)
	volumeState(cloud.VolumeCreating, cloud.Attachment{})
	f.now = f.now.Add(step)
	volumeState(cloud.VolumeAvailable, cloud.Attachment{})
	ok(t, f.c.StartInstance(ctx, i.ID))
	checkEC2(t, f.c.StopInstance(ctx, i.ID), "StopInstances", i.ID, "IncorrectInstanceState", cloud.ClassTransient)
	checkEC2(t, f.c.SetInstanceType(ctx, i.ID, "m7g.medium"), "ModifyInstanceAttribute", i.ID, "IncorrectInstanceState", cloud.ClassTransient)
	f.now = f.now.Add(memory.BootDelay - memory.VolumeDelay - step)
	instanceState(cloud.InstancePending)
	f.now = f.now.Add(step)
	instanceState(cloud.InstanceRunning)
	ok(t, f.c.StartInstance(ctx, i.ID))
	ok(t, f.c.AttachVolume(ctx, v.ID, i.ID, "/dev/sdf"))
	assoc, err := f.c.AssociateAddress(ctx, a.AllocationID, i.ID)
	ok(t, err)
	f.now = f.now.Add(memory.VolumeDelay - step)
	volumeState(cloud.VolumeInUse, cloud.Attachment{InstanceID: i.ID, Device: "/dev/sdf", State: cloud.AttachmentAttaching})
	f.now = f.now.Add(step)
	volumeState(cloud.VolumeInUse, cloud.Attachment{InstanceID: i.ID, Device: "/dev/sdf", State: cloud.AttachmentAttached})
	ok(t, f.c.StopInstance(ctx, i.ID))
	f.now = f.now.Add(memory.StopDelay - step)
	ok(t, f.c.StopInstance(ctx, i.ID))
	checkEC2(t, f.c.StartInstance(ctx, i.ID), "StartInstances", i.ID, "IncorrectInstanceState", cloud.ClassTransient)
	instanceState(cloud.InstanceStopping)
	f.now = f.now.Add(step)
	instanceState(cloud.InstanceStopped)
	ok(t, f.c.StopInstance(ctx, i.ID))
	checkEC2(t, f.c.SetInstanceType(ctx, i.ID, "r8g.medium"), "ModifyInstanceAttribute", i.ID, cloud.CodeTagContract, cloud.ClassRefused)
	ok(t, f.c.SetInstanceType(ctx, i.ID, "r7g.medium"))
	facts, err := f.c.Metadata(i.ID).Instance(ctx)
	ok(t, err)
	equal(t, facts.Type, "r7g.medium")
	ok(t, f.c.StartInstance(ctx, i.ID))
	instanceState(cloud.InstancePending)
	f.now = f.now.Add(memory.BootDelay - step)
	instanceState(cloud.InstancePending)
	f.now = f.now.Add(step)
	instanceState(cloud.InstanceRunning)
	ok(t, f.c.TerminateInstance(ctx, i.ID))
	f.now = f.now.Add(memory.TerminateDelay - step)
	ok(t, f.c.TerminateInstance(ctx, i.ID))
	instanceState(cloud.InstanceShuttingDown)
	_, err = f.c.DescribeVolume(ctx, i.RootVolume)
	ok(t, err)
	volumeState(cloud.VolumeInUse, cloud.Attachment{InstanceID: i.ID, Device: "/dev/sdf", State: cloud.AttachmentAttached})
	current, err := f.c.DescribeAddress(ctx, a.AllocationID)
	ok(t, err)
	equal(t, current.AssociationID, assoc)
	f.now = f.now.Add(step)
	instanceState(cloud.InstanceTerminated)
	ok(t, f.c.TerminateInstance(ctx, i.ID))
	_, err = f.c.DescribeVolume(ctx, i.RootVolume)
	checkEC2(t, err, "DescribeVolumes", i.RootVolume, "InvalidVolume.NotFound", cloud.ClassNotFound)
	volumeState(cloud.VolumeAvailable, cloud.Attachment{})
	current, err = f.c.DescribeAddress(ctx, a.AllocationID)
	ok(t, err)
	equal(t, current.AssociationID, "")
	equal(t, current.InstanceID, "")
	noInstance := func([]cloud.Instance) bool { t.Error("terminated instance was listed"); return false }
	ok(t, f.c.FindInstances(ctx, i.Tags, noInstance))
	noRoot := func([]cloud.Volume) bool { t.Error("deleted volume was listed"); return false }
	ok(t, f.c.FindVolumes(ctx, f.launch().Tags, noRoot))
}

func TestMissingInstances(t *testing.T) {
	// R-UO3K-OZE6
	f := setup(t)
	ctx := t.Context()
	a, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	const missing = "i-absent"
	_, describe := f.c.DescribeInstance(ctx, missing)
	_, assoc := f.c.AssociateAddress(ctx, a.AllocationID, missing)
	for _, tc := range []struct {
		op, subject string
		err         error
	}{
		{"DescribeInstances", missing, describe},
		{"AssociateAddress", a.AllocationID, assoc},
		{"StartInstances", missing, f.c.StartInstance(ctx, missing)},
		{"StopInstances", missing, f.c.StopInstance(ctx, missing)},
		{"TerminateInstances", missing, f.c.TerminateInstance(ctx, missing)},
		{"ModifyInstanceAttribute", missing, f.c.SetInstanceType(ctx, missing, "m7g.medium")},
	} {
		checkEC2(t, tc.err, tc.op, tc.subject, "InvalidInstanceID.NotFound", cloud.ClassNotFound)
	}
}

func TestTagResource(t *testing.T) {
	// R-QKH7-6IC3
	f := setup(t)
	ctx := t.Context()
	i := f.instance(t)
	spec := f.spec()
	spec.Tags["X"] = "volume"
	spec.Tags["Z"] = "retained"
	v, err := f.c.CreateVolume(ctx, spec)
	ok(t, err)
	a, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	checkTags := func(wantI, wantV, wantA cloud.Tags) {
		t.Helper()
		actualI, err := f.c.DescribeInstance(ctx, i.ID)
		ok(t, err)
		equal(t, actualI.Tags, wantI)
		actualV, err := f.c.DescribeVolume(ctx, v.ID)
		ok(t, err)
		equal(t, actualV.Tags, wantV)
		actualA, err := f.c.DescribeAddress(ctx, a.AllocationID)
		ok(t, err)
		equal(t, actualA.Tags, wantA)
	}
	for _, tc := range []struct {
		held, missing, code string
	}{
		{i.ID, "i-missing", "InvalidInstanceID.NotFound"},
		{v.ID, "vol-missing", "InvalidVolume.NotFound"},
		{a.AllocationID, "eipalloc-missing", "InvalidAllocationID.NotFound"},
	} {
		for _, key := range []string{cloud.TagKind, "Space", "Domain"} {
			tags := cloud.Tags{key: "", "X": "rejected", "Y": "rejected"}
			for _, id := range []string{tc.held, tc.missing} {
				checkEC2(t, f.c.TagResource(ctx, id, tags), "CreateTags", id, cloud.CodeTagContract, cloud.ClassRefused)
				checkTags(i.Tags, v.Tags, a.Tags)
			}
		}
		checkEC2(t, f.c.TagResource(ctx, tc.missing, cloud.Tags{"X": "value"}), "CreateTags", tc.missing, tc.code, cloud.ClassNotFound)
	}

	extra := cloud.Tags{"X": "new", "Y": "another"}
	for _, id := range []string{i.ID, v.ID, a.AllocationID} {
		ok(t, f.c.TagResource(ctx, id, extra))
	}
	wantTags := func(original cloud.Tags) cloud.Tags {
		want := maps.Clone(original)
		maps.Copy(want, extra)
		return want
	}
	checkTags(wantTags(i.Tags), wantTags(v.Tags), wantTags(a.Tags))

	f.now = f.now.Add(memory.VolumeDelay)
	ok(t, f.c.DeleteVolume(ctx, v.ID))
	f.now = f.now.Add(memory.VolumeDelay - time.Nanosecond)
	ok(t, f.c.TagResource(ctx, v.ID, cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue}))
	wantV := wantTags(v.Tags)
	wantV[cloud.TagOrphaned] = cloud.OrphanedTrue
	checkTags(wantTags(i.Tags), wantV, wantTags(a.Tags))
}

func TestConcurrentIDsAndPaginatedFiltering(t *testing.T) {
	// R-UGS6-ECY0 R-GXPJ-WNL1
	f := setup(t)
	ctx := t.Context()
	tags := cloud.Tags{"test": "same", "empty": ""}
	const count = memory.PageSize + 3
	var group sync.WaitGroup
	for n := range count {
		group.Go(func() {
			l, spec := f.launch(), f.spec()
			maps.Copy(l.Tags, tags)
			maps.Copy(spec.Tags, tags)
			if n == count-1 {
				delete(l.Tags, "empty")
				delete(spec.Tags, "empty")
			}
			_, launchErr := f.c.RunInstance(ctx, l)
			_, volumeErr := f.c.CreateVolume(ctx, spec)
			_, addrErr := f.c.AllocateAddress(ctx, l.Tags)
			if err := errors.Join(launchErr, volumeErr, addrErr); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	checkPages(t, f.c.FindInstances, tags, func(i cloud.Instance) string { return i.ID }, "i-")
	checkPages(t, f.c.FindVolumes, cloud.Tags{cloud.TagKind: cloud.KindHostData, "test": "same", "empty": ""}, func(v cloud.Volume) string { return v.ID }, "vol-")
	checkPages(t, f.c.FindAddresses, tags, func(a cloud.Address) string { return a.AllocationID }, "eipalloc-")
	i := f.instance(t)
	f.now = f.now.Add(memory.BootDelay)
	a, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	association, err := f.c.AssociateAddress(ctx, a.AllocationID, i.ID)
	ok(t, err)
	if !regexp.MustCompile("^eipassoc-[0-9a-f]{17}$").MatchString(association) {
		t.Fatalf("bad association id: %s", association)
	}
	id, err := f.c.ChangeRecord(ctx, f.cfg.Hosts.ZoneID, cloud.RecordChange{Action: cloud.RecordCreate, Name: "a." + f.cfg.Hosts.Domain, IP: a.IP, TTL: 60})
	ok(t, err)
	if !regexp.MustCompile("^C[0-9A-F]{17}$").MatchString(id) {
		t.Fatalf("bad change id: %s", id)
	}
	unique := cloud.Tags{"callback-test": i.ID}
	ok(t, f.c.TagResource(ctx, i.ID, unique))
	ok(t, f.c.FindInstances(ctx, unique, func(page []cloud.Instance) bool {
		for _, found := range page {
			ok(t, f.c.TagResource(ctx, found.ID, cloud.Tags{"inside-listing": "written"}))
		}
		return false
	}))
	tagged, err := f.c.DescribeInstance(ctx, i.ID)
	ok(t, err)
	equal(t, tagged.Tags["inside-listing"], "written")
}

func checkPages[T any](t *testing.T, find func(context.Context, cloud.Tags, func([]T) bool) error, filter cloud.Tags, id func(T) string, prefix string) {
	t.Helper()
	pattern := regexp.MustCompile("^" + prefix + "[0-9a-f]{17}$")
	var ids []string
	var sizes []int
	ctx := t.Context()
	ok(t, find(ctx, filter, func(page []T) bool {
		sizes = append(sizes, len(page))
		for _, item := range page {
			ids = append(ids, id(item))
			if !pattern.MatchString(id(item)) {
				t.Errorf("bad id %s", id(item))
			}
		}
		return true
	}))
	equal(t, sizes, []int{memory.PageSize, 2})
	if !slices.IsSorted(ids) || len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Fatalf("unsorted or repeated ids: %v", ids)
	}
	calls := 0
	ok(t, find(ctx, filter, func(page []T) bool {
		calls++
		equal(t, len(page), memory.PageSize)
		return false
	}))
	equal(t, calls, 1)
	for _, tags := range []cloud.Tags{nil, {"Space": ""}, {"Domain": ""}} {
		err := find(ctx, tags, func([]T) bool { t.Error("callback called for invalid filter"); return true })
		var typed *cloud.Error
		if !errors.As(err, &typed) || typed.Code != cloud.CodeTagContract || typed.Class != cloud.ClassRefused {
			t.Fatalf("filter %+v error = %v", tags, err)
		}
	}
}

func TestAddresses(t *testing.T) {
	// R-FYS0-1FV6 R-UWMV-DDL1
	f := setup(t)
	ctx := t.Context()
	for _, tags := range []cloud.Tags{nil, {cloud.TagKind: cloud.KindHost, "Space": ""}, {cloud.TagKind: cloud.KindHost, "Domain": ""}} {
		_, err := f.c.AllocateAddress(ctx, tags)
		checkEC2(t, err, "AllocateAddress", "", cloud.CodeTagContract, cloud.ClassRefused)
	}
	tags := f.launch().Tags
	var first cloud.Address
	for n := 1; n <= 254; n++ {
		a, err := f.c.AllocateAddress(ctx, tags)
		ok(t, err)
		equal(t, a.IP, fmt.Sprintf("192.0.2.%d", n))
		equal(t, a.Tags, tags)
		if n == 1 {
			first = a
		}
	}
	_, err := f.c.AllocateAddress(ctx, tags)
	checkEC2(t, err, "AllocateAddress", "", "AddressLimitExceeded", cloud.ClassRefused)
	i, other := f.instance(t), f.instance(t)
	_, err = f.c.AssociateAddress(ctx, first.AllocationID, i.ID)
	checkEC2(t, err, "AssociateAddress", first.AllocationID, "IncorrectInstanceState", cloud.ClassTransient)
	f.now = f.now.Add(memory.BootDelay)
	assoc, err := f.c.AssociateAddress(ctx, first.AllocationID, i.ID)
	ok(t, err)
	same, err := f.c.AssociateAddress(ctx, first.AllocationID, i.ID)
	ok(t, err)
	equal(t, same, assoc)
	checkEC2(t, f.c.ReleaseAddress(ctx, first.AllocationID), "ReleaseAddress", first.AllocationID, "InvalidIPAddress.InUse", cloud.ClassRefused)
	replacement, err := f.c.AssociateAddress(ctx, first.AllocationID, other.ID)
	ok(t, err)
	if replacement == assoc {
		t.Fatal("reused association id")
	}
	ok(t, f.c.DisassociateAddress(ctx, assoc))
	a, err := f.c.DescribeAddress(ctx, first.AllocationID)
	ok(t, err)
	equal(t, a.AssociationID, replacement)
	equal(t, a.InstanceID, other.ID)
	ok(t, f.c.DisassociateAddress(ctx, replacement))
	a, err = f.c.DescribeAddress(ctx, first.AllocationID)
	ok(t, err)
	equal(t, a.AssociationID, "")
	equal(t, a.InstanceID, "")
	ok(t, f.c.ReleaseAddress(ctx, first.AllocationID))
	ok(t, f.c.ReleaseAddress(ctx, first.AllocationID))
	_, err = f.c.DescribeAddress(ctx, first.AllocationID)
	checkEC2(t, err, "DescribeAddresses", first.AllocationID, "InvalidAllocationID.NotFound", cloud.ClassNotFound)
	a, err = f.c.AllocateAddress(ctx, tags)
	ok(t, err)
	equal(t, a.IP, first.IP)
}

func TestVolumeContractAndAttachments(t *testing.T) {
	// R-UZ2O-4X2F R-KKJH-16V1
	f := setup(t)
	ctx := t.Context()
	spec := f.spec()
	spec.ClientToken = "volume-token"
	spec.Tags["test"] = "original"
	v, err := f.c.CreateVolume(ctx, spec)
	ok(t, err)
	equal(t, v.State, cloud.VolumeCreating)
	equal(t, v.SizeGB, spec.SizeGB)
	equal(t, v.AZ, spec.AZ)
	equal(t, v.Tags, spec.Tags)
	same, err := f.c.CreateVolume(ctx, spec)
	ok(t, err)
	equal(t, same, v)
	i, other := f.instance(t), f.instance(t)
	checkEC2(t, f.c.AttachVolume(ctx, v.ID, i.ID, "/dev/sdy"), "AttachVolume", v.ID, "IncorrectState", cloud.ClassTransient)
	f.now = f.now.Add(memory.VolumeDelay)
	checkEC2(t, f.c.AttachVolume(ctx, v.ID, i.ID, "/dev/sdy"), "AttachVolume", v.ID, "IncorrectInstanceState", cloud.ClassTransient)
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	for _, tags := range []cloud.Tags{
		{"test": "original"},
		{cloud.TagKind: cloud.KindHost, "test": "original"},
		{cloud.TagKind: cloud.KindHostData, "test": "original", "Space": ""},
		{cloud.TagKind: cloud.KindHostData, "test": "original", "Domain": ""},
	} {
		for _, token := range []string{"unused", spec.ClientToken} {
			changed := spec
			changed.Tags, changed.ClientToken = tags, token
			_, err := reopened.CreateVolume(ctx, changed)
			checkEC2(t, err, "CreateVolume", "", cloud.CodeTagContract, cloud.ClassRefused)
		}
	}
	again, err := reopened.CreateVolume(ctx, spec)
	ok(t, err)
	equal(t, again.ID, v.ID)
	equal(t, again.State, cloud.VolumeAvailable)
	for _, edit := range []func(*cloud.VolumeSpec){
		func(spec *cloud.VolumeSpec) { spec.AZ = "other-zone" },
		func(spec *cloud.VolumeSpec) { spec.SizeGB++ },
		func(spec *cloud.VolumeSpec) { spec.Tags["test"] = "new" },
	} {
		changed := spec
		changed.Tags = maps.Clone(spec.Tags)
		edit(&changed)
		_, err := reopened.CreateVolume(ctx, changed)
		checkEC2(t, err, "CreateVolume", "", "IdempotentParameterMismatch", cloud.ClassMismatch)
	}
	var found []cloud.Volume
	ok(t, reopened.FindVolumes(ctx, cloud.Tags{"test": "original"}, func(page []cloud.Volume) bool {
		found = append(found, page...)
		return true
	}))
	equal(t, found, []cloud.Volume{again})
	wrong := f.spec()
	wrong.AZ = "other-zone"
	foreign, err := reopened.CreateVolume(ctx, wrong)
	ok(t, err)
	f.now = f.now.Add(memory.BootDelay)
	checkEC2(t, reopened.AttachVolume(ctx, foreign.ID, i.ID, "/dev/sdx"), "AttachVolume", foreign.ID, "InvalidParameterValue", cloud.ClassRefused)
	ok(t, reopened.StopInstance(ctx, i.ID))
	f.now = f.now.Add(memory.StopDelay)
	ok(t, reopened.AttachVolume(ctx, v.ID, i.ID, "/dev/sdy"))
	ok(t, reopened.AttachVolume(ctx, v.ID, i.ID, "/dev/ignored"))
	checkEC2(t, reopened.AttachVolume(ctx, v.ID, other.ID, "/dev/sdy"), "AttachVolume", v.ID, "VolumeInUse", cloud.ClassRefused)
	f.now = f.now.Add(memory.VolumeDelay)
	ok(t, reopened.AttachVolume(ctx, v.ID, i.ID, "/dev/ignored"))
	attached, err := reopened.DescribeVolume(ctx, v.ID)
	ok(t, err)
	equal(t, attached.Attachment, cloud.Attachment{InstanceID: i.ID, Device: "/dev/sdy", State: cloud.AttachmentAttached})

	const missing = "vol-missing"
	_, describe := reopened.DescribeVolume(ctx, missing)
	_, modification := reopened.VolumeModification(ctx, missing)
	for op, err := range map[string]error{
		"DescribeVolumes":              describe,
		"DescribeVolumesModifications": modification,
		"AttachVolume":                 reopened.AttachVolume(ctx, missing, i.ID, "/dev/sdy"),
		"ModifyVolume":                 reopened.ModifyVolume(ctx, missing, 45),
	} {
		checkEC2(t, err, op, missing, "InvalidVolume.NotFound", cloud.ClassNotFound)
	}
}

func TestModificationAndRateLimit(t *testing.T) {
	// R-V0AK-IOT4 and the modification transitions of R-UFKA-0L7B.
	f := setup(t)
	ctx := t.Context()
	v := f.volume(t)
	initial, err := f.c.VolumeModification(ctx, v.ID)
	ok(t, err)
	equal(t, initial, cloud.Modification{})
	f.now = f.now.Add(memory.VolumeDelay)
	started := f.now
	size := v.SizeGB
	checkEC2(t, f.c.ModifyVolume(ctx, v.ID, size), "ModifyVolume", v.ID, "InvalidParameterValue", cloud.ClassRefused)
	for range memory.MaxModifications {
		target := size + 1
		ok(t, f.c.ModifyVolume(ctx, v.ID, target))
		checkEC2(t, f.c.ModifyVolume(ctx, v.ID, target+1), "ModifyVolume", v.ID, "IncorrectModificationState", cloud.ClassRefused)
		checkPhase := func(state string, wantSize int32) {
			t.Helper()
			m, err := f.c.VolumeModification(ctx, v.ID)
			ok(t, err)
			equal(t, m, cloud.Modification{State: state, TargetGB: target})
			current, err := f.c.DescribeVolume(ctx, v.ID)
			ok(t, err)
			equal(t, current.SizeGB, wantSize)
		}
		f.now = f.now.Add(memory.ModifyDelay - time.Nanosecond)
		checkPhase(cloud.ModificationModifying, size)
		f.now = f.now.Add(time.Nanosecond)
		checkPhase(cloud.ModificationOptimizing, target)
		f.now = f.now.Add(memory.ModifyDelay - time.Nanosecond)
		checkPhase(cloud.ModificationOptimizing, target)
		f.now = f.now.Add(time.Nanosecond)
		checkPhase(cloud.ModificationCompleted, target)
		size = target
	}
	f.now = started.Add(24*time.Hour - time.Nanosecond)
	checkEC2(t, f.c.ModifyVolume(ctx, v.ID, size+1), "ModifyVolume", v.ID, "VolumeModificationRateExceeded", cloud.ClassTransient)
	f.now = f.now.Add(time.Nanosecond)
	ok(t, f.c.ModifyVolume(ctx, v.ID, size+1))
}

func TestRecords(t *testing.T) {
	// R-V2QD-A8AI R-H05C-O72F and the DNS transition of R-UFKA-0L7B.
	f := setup(t)
	ctx := t.Context()
	zone := f.cfg.Hosts.ZoneID
	name := "team." + f.cfg.Hosts.Domain
	record := cloud.RecordChange{Action: cloud.RecordCreate, Name: name, IP: "192.0.2.1", TTL: 90}
	_, err := f.c.FindRecords(ctx, "other", name)
	checkErr(t, err, "route53", "ListResourceRecordSets", name, "NoSuchHostedZone", cloud.ClassRefused)
	_, err = f.c.ChangeRecord(ctx, "other", record)
	checkErr(t, err, "route53", "ChangeResourceRecordSets", name, "NoSuchHostedZone", cloud.ClassRefused)
	badChange := func(change cloud.RecordChange) {
		t.Helper()
		_, err := f.c.ChangeRecord(ctx, zone, change)
		checkErr(t, err, "route53", "ChangeResourceRecordSets", change.Name, "InvalidChangeBatch", cloud.ClassRefused)
	}
	for _, badName := range []string{f.cfg.Hosts.Domain, "." + f.cfg.Hosts.Domain, name + ".other", name + ".."} {
		bad := record
		bad.Name = badName
		badChange(bad)
	}
	bad := record
	bad.Action = "UPSERT"
	badChange(bad)
	id, err := f.c.ChangeRecord(ctx, zone, record)
	ok(t, err)
	bad = record
	bad.Name = strings.ToUpper(name) + "."
	badChange(bad)
	records, err := f.c.FindRecords(ctx, zone, bad.Name)
	ok(t, err)
	equal(t, records, []cloud.Record{{Type: "A", Name: bad.Name, Values: []string{record.IP}, TTL: record.TTL}})
	records, err = f.c.FindRecords(ctx, zone, "other."+name)
	ok(t, err)
	equal(t, len(records), 0)
	bad.Action = cloud.RecordDelete
	bad.TTL++
	badChange(bad)
	bad.TTL = record.TTL
	bad.IP = "192.0.2.156"
	badChange(bad)
	f.now = f.now.Add(memory.ChangeDelay - time.Nanosecond)
	isSync, err := f.c.ChangeInsync(ctx, id)
	ok(t, err)
	equal(t, isSync, false)
	f.now = f.now.Add(time.Nanosecond)
	isSync, err = f.c.ChangeInsync(ctx, id)
	ok(t, err)
	equal(t, isSync, true)
	bad.IP = record.IP
	deletion, err := f.c.ChangeRecord(ctx, zone, bad)
	ok(t, err)
	if deletion == id {
		t.Fatal("record deletion reused create's id")
	}
	isSync, err = f.c.ChangeInsync(ctx, deletion)
	ok(t, err)
	equal(t, isSync, false)
	records, err = f.c.FindRecords(ctx, zone, name)
	ok(t, err)
	equal(t, len(records), 0)
	badChange(bad)
	_, err = f.c.ChangeInsync(ctx, "unknown")
	checkErr(t, err, "route53", "GetChange", "unknown", "NoSuchChange", cloud.ClassNotFound)
}

func TestIdentity(t *testing.T) {
	// R-V566-1RRW R-V6E2-FJIL R-V7LY-TB9A
	f := setup(t)
	ctx := t.Context()
	i, other := f.instance(t), f.instance(t)
	f.now = f.now.In(time.FixedZone("west", -7*3600))
	account, err := f.c.Account(ctx)
	ok(t, err)
	equal(t, account, memory.Account)
	facts, err := f.c.Metadata(i.ID).Instance(ctx)
	ok(t, err)
	equal(t, facts, cloud.InstanceFacts{InstanceID: i.ID, Type: i.Type, AMI: memory.AMI, Region: f.cfg.Hosts.Region})
	_, err = f.c.Metadata("i-missing").Instance(ctx)
	checkEC2(t, err, "DescribeInstances", "i-missing", "InvalidInstanceID.NotFound", cloud.ClassNotFound)
	p := f.c.Presigner(i.ID)
	const binding = "accounts.test:1234"
	const expires = 2*time.Minute + 750*time.Millisecond
	makeURL := func(p cloud.Presigner, b string, d time.Duration) *url.URL {
		t.Helper()
		raw, err := p.PresignCallerIdentity(ctx, b, d)
		ok(t, err)
		u, err := url.Parse(raw)
		ok(t, err)
		return u
	}
	u := makeURL(p, binding, expires)
	equal(t, u.Scheme, "https")
	equal(t, u.Host, "sts."+f.cfg.Hosts.Region+".amazonaws.com")
	equal(t, u.Path, "/")
	for k, want := range map[string]string{
		"Action": "GetCallerIdentity", "Version": "2011-06-15", "X-Amz-Algorithm": "AWS4-HMAC-SHA256",
		"X-Amz-Date": f.now.UTC().Format("20060102T150405Z"), "X-Amz-Expires": "120",
		"X-Amz-SignedHeaders": "host;x-ikigenba-accounts",
	} {
		equal(t, u.Query()[k], []string{want})
	}
	credential := u.Query().Get("X-Amz-Credential")
	if !strings.Contains(credential, i.ID) {
		t.Fatalf("credential %q does not identify %s", credential, i.ID)
	}
	signature := u.Query().Get("X-Amz-Signature")
	if !regexp.MustCompile("^[0-9a-f]{64}$").MatchString(signature) {
		t.Fatalf("invalid signature: %q", signature)
	}
	f.now = f.now.Add(time.Nanosecond)
	equal(t, makeURL(p, binding, expires).String(), u.String())
	for _, changed := range []*url.URL{
		makeURL(f.c.Presigner(other.ID), binding, expires),
		makeURL(p, binding+".other", expires),
		makeURL(p, binding, time.Minute),
		makeURL(p, binding, expires-time.Nanosecond),
	} {
		if changed.Query().Get("X-Amz-Signature") == signature {
			t.Fatalf("signature repeated after a parameter changed: %s", changed)
		}
	}
	f.now = f.now.Add(time.Second)
	if makeURL(p, binding, expires).Query().Get("X-Amz-Signature") == signature {
		t.Fatal("signature repeated after date changed")
	}
	caller, err := f.c.CallerOf(ctx, u.String(), binding)
	ok(t, err)
	equal(t, caller, cloud.Caller{
		Account: memory.Account, Arn: "arn:aws:sts::" + memory.Account + ":assumed-role/" + f.cfg.Hosts.InstanceProfile + "/" + i.ID, UserID: "AROASANDBOX:" + i.ID,
	})
	refuse := func(raw, b string) {
		t.Helper()
		_, err := f.c.CallerOf(ctx, raw, b)
		checkErr(t, err, "sts", "GetCallerIdentity", "", "HTTP 403", cloud.ClassRefused)
	}
	refuse(u.String(), "another")
	refuse(u.String()+"&extra=1", binding)
	refuse(strings.Replace(u.String(), "https:", "http:", 1), binding)
	refuse(makeURL(f.c.Presigner("i-missing"), binding, expires).String(), binding)
	f.now = f.now.Add(-2 * time.Second)
	refuse(u.String(), binding)
	f.now = f.now.Add(2*time.Second + 2*time.Minute)
	refuse(u.String(), binding)
	u = makeURL(p, binding, expires)
	ok(t, f.c.TerminateInstance(ctx, i.ID))
	f.now = f.now.Add(memory.TerminateDelay - time.Nanosecond)
	_, err = f.c.CallerOf(ctx, u.String(), binding)
	ok(t, err)
	f.now = f.now.Add(time.Nanosecond)
	refuse(u.String(), binding)
}

func TestIDsUseInjectedRandomSourceAndRetryCollisions(t *testing.T) {
	// R-CQ5P-NF79
	f := setup(t)
	stream := bytes.Repeat([]byte{1}, 32)
	for _, draw := range []byte{
		0xaa, 0xbb, // First instance and root volume.
		0xaa, 0xcc, 0xbb, 0xdd, // Instance and volume collisions.
		0xdd, 0xee, // Data volume collides with the second root volume.
		0x12, 0x12, 0x13, // Address allocation collision.
		0x34, 0x34, 0x35, // Address association collision.
		0xfa, 0xfa, 0xfb, // Record change collision.
	} {
		stream = append(stream, bytes.Repeat([]byte{draw}, 9)...)
	}
	f.cfg.Rand = bytes.NewReader(stream)
	c, err := memory.New(f.cfg)
	ok(t, err)
	f.c = c
	first := f.instance(t)
	equal(t, first.ID, "i-"+strings.Repeat("a", 17))
	equal(t, first.RootVolume, "vol-"+strings.Repeat("b", 17))
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	f.c = reopened
	second := f.instance(t)
	equal(t, second.ID, "i-"+strings.Repeat("c", 17))
	equal(t, second.RootVolume, "vol-"+strings.Repeat("d", 17))
	v := f.volume(t)
	equal(t, v.ID, "vol-"+strings.Repeat("e", 17))
	ctx := t.Context()
	a, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	equal(t, a.AllocationID, "eipalloc-2"+strings.Repeat("12", 8))
	b, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	equal(t, b.AllocationID, "eipalloc-3"+strings.Repeat("13", 8))
	f.now = f.now.Add(memory.BootDelay)
	assoc, err := f.c.AssociateAddress(ctx, a.AllocationID, first.ID)
	ok(t, err)
	equal(t, assoc, "eipassoc-4"+strings.Repeat("34", 8))
	assoc, err = f.c.AssociateAddress(ctx, b.AllocationID, second.ID)
	ok(t, err)
	equal(t, assoc, "eipassoc-5"+strings.Repeat("35", 8))
	change, err := f.c.ChangeRecord(ctx, f.cfg.Hosts.ZoneID, cloud.RecordChange{
		Action: cloud.RecordCreate, Name: "team." + f.cfg.Hosts.Domain, IP: a.IP, TTL: 90,
	})
	ok(t, err)
	equal(t, change, "CA"+strings.Repeat("FA", 8))
	change, err = f.c.ChangeRecord(ctx, f.cfg.Hosts.ZoneID, cloud.RecordChange{
		Action: cloud.RecordCreate, Name: "other." + f.cfg.Hosts.Domain, IP: b.IP, TTL: 90,
	})
	ok(t, err)
	equal(t, change, "CB"+strings.Repeat("FB", 8))
}

func TestCapacityKeepsPreviousState(t *testing.T) {
	// R-GV9R-543N, capacity exhaustion is a failed persistence attempt.
	f := setup(t)
	ctx := t.Context()
	a, err := f.c.AllocateAddress(ctx, f.launch().Tags)
	ok(t, err)
	oversize := cloud.Tags{"too-much": strings.Repeat("x", 128<<10)}
	checkEC2(t, f.c.TagResource(ctx, a.AllocationID, oversize), "CreateTags", a.AllocationID, cloud.CodeNoResponse, cloud.ClassTransient)
	before, err := f.c.DescribeAddress(ctx, a.AllocationID)
	ok(t, err)
	equal(t, before, a)

	// Each requested mutation fits alone. The strings expand in JSON, so
	// their accumulated state crosses the file limit after several saves.
	const writes = 13
	data := strings.Repeat("<", 120_000)
	failed := false
	for n := range writes {
		tag := cloud.Tags{fmt.Sprintf("batch-%02d", n): data}
		err := f.c.TagResource(ctx, a.AllocationID, tag)
		if err == nil {
			maps.Copy(before.Tags, tag)
			continue
		}
		checkEC2(t, err, "CreateTags", a.AllocationID, cloud.CodeNoResponse, cloud.ClassTransient)
		failed = true
		break
	}
	if !failed || len(before.Tags) == len(a.Tags) {
		t.Fatal("expected successful writes followed by capacity exhaustion")
	}
	actual, err := f.c.DescribeAddress(ctx, a.AllocationID)
	ok(t, err)
	equal(t, actual, before)
	reopened, err := memory.New(f.cfg)
	ok(t, err)
	actual, err = reopened.DescribeAddress(ctx, a.AllocationID)
	ok(t, err)
	equal(t, actual, before)
	ok(t, reopened.ReleaseAddress(ctx, a.AllocationID))
	_, err = reopened.DescribeAddress(ctx, a.AllocationID)
	checkEC2(t, err, "DescribeAddresses", a.AllocationID, "InvalidAllocationID.NotFound", cloud.ClassNotFound)
}

func TestLoadRejectsOversizeStateBeforeDecode(t *testing.T) {
	// R-UD4H-91PX, a valid document outside the sandbox's storage capacity.
	f := setup(t)
	f.instance(t)
	file, err := os.OpenFile(f.cfg.Path, os.O_WRONLY|os.O_APPEND, 0600)
	ok(t, err)
	spaces := strings.Repeat(" ", 32<<10)
	for range (8 << 20) / len(spaces) {
		_, err := file.WriteString(spaces)
		ok(t, err)
	}
	ok(t, file.Close())
	if c, err := memory.New(f.cfg); err == nil || c != nil {
		t.Fatalf("oversize state load = %v, %v", c, err)
	}
}

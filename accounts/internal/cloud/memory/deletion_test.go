package memory_test

import (
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud/memory"
)

func TestDeleteVolumeGuards(t *testing.T) {
	// R-7JI7-3XP8
	f := setup(t)
	ctx := t.Context()
	c := f.c
	host := f.instance(t)
	attached := f.volume(t)
	f.now = f.now.Add(memory.BootDelay)
	ok(t, c.AttachVolume(ctx, attached.ID, host.ID, "/dev/sdf"))
	creating := f.volume(t)

	const missing = "vol-missing"
	_, err := c.DescribeVolume(ctx, missing)
	checkEC2(t, err, "DescribeVolumes", missing, "InvalidVolume.NotFound", cloud.ClassNotFound)
	checkEC2(t, c.DeleteVolume(ctx, missing), "DeleteVolume", missing, "InvalidVolume.NotFound", cloud.ClassNotFound)

	for _, tc := range []struct {
		id, code string
		class    cloud.Class
	}{
		{host.RootVolume, "UnauthorizedOperation", cloud.ClassRefused},
		{attached.ID, "VolumeInUse", cloud.ClassRefused},
		{creating.ID, "IncorrectState", cloud.ClassTransient},
	} {
		before, err := c.DescribeVolume(ctx, tc.id)
		ok(t, err)
		checkEC2(t, c.DeleteVolume(ctx, tc.id), "DeleteVolume", tc.id, tc.code, tc.class)
		after, err := c.DescribeVolume(ctx, tc.id)
		ok(t, err)
		equal(t, after, before)
	}

	f.now = f.now.Add(memory.VolumeDelay)
	ok(t, c.DeleteVolume(ctx, creating.ID))
	deleting, err := c.DescribeVolume(ctx, creating.ID)
	ok(t, err)
	equal(t, deleting.State, cloud.VolumeDeleting)
	f.now = f.now.Add(memory.VolumeDelay - time.Second)
	ok(t, c.DeleteVolume(ctx, creating.ID))
	again, err := c.DescribeVolume(ctx, creating.ID)
	ok(t, err)
	equal(t, again, deleting)
	f.now = f.now.Add(time.Second)
	checkEC2(t, c.DeleteVolume(ctx, creating.ID), "DeleteVolume", creating.ID, "InvalidVolume.NotFound", cloud.ClassNotFound)
}

func TestDeleteVolumeVisibility(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	spec := f.spec()
	spec.Tags[cloud.TagHost] = "host01"
	v, err := f.c.CreateVolume(ctx, spec)
	ok(t, err)
	f.now = f.now.Add(memory.VolumeDelay)
	ok(t, f.c.ModifyVolume(ctx, v.ID, spec.SizeGB+1))
	f.now = f.now.Add(memory.ModifyDelay - time.Second)
	before, err := f.c.DescribeVolume(ctx, v.ID)
	ok(t, err)
	ok(t, f.c.DeleteVolume(ctx, v.ID))
	want := before
	want.State = cloud.VolumeDeleting

	reopened, err := memory.New(f.cfg)
	ok(t, err)
	f.now = f.now.Add(memory.VolumeDelay - time.Second)
	filter := cloud.Tags{cloud.TagHost: "host01"}
	for _, c := range []*memory.Cloud{f.c, reopened} {
		described, err := c.DescribeVolume(ctx, v.ID)
		ok(t, err)
		equal(t, described, want)
		var found []cloud.Volume
		ok(t, c.FindVolumes(ctx, filter, func(volumes []cloud.Volume) bool {
			found = append(found, volumes...)
			return true
		}))
		equal(t, found, []cloud.Volume{want})
	}

	f.now = f.now.Add(time.Second)
	for _, c := range []*memory.Cloud{f.c, reopened} {
		_, describe := c.DescribeVolume(ctx, v.ID)
		_, modification := c.VolumeModification(ctx, v.ID)
		for op, err := range map[string]error{
			"DescribeVolumes":              describe,
			"AttachVolume":                 c.AttachVolume(ctx, v.ID, "i-test", "/dev/sdf"),
			"ModifyVolume":                 c.ModifyVolume(ctx, v.ID, spec.SizeGB+2),
			"DescribeVolumesModifications": modification,
			"CreateTags":                   c.TagResource(ctx, v.ID, cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue}),
			"DeleteVolume":                 c.DeleteVolume(ctx, v.ID),
		} {
			checkEC2(t, err, op, v.ID, "InvalidVolume.NotFound", cloud.ClassNotFound)
		}
		ok(t, c.FindVolumes(ctx, filter, func(volumes []cloud.Volume) bool {
			t.Fatalf("found deleted volume: %+v", volumes)
			return false
		}))
	}
}

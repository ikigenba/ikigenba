package awssdk_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

func TestEC2OperationMappings(t *testing.T) {
	// R-L0U6-OEYH
	ctx := t.Context()
	hosts := cloud.Tags{cloud.TagKind: cloud.KindHost}
	data := cloud.Tags{cloud.TagKind: cloud.KindHostData}
	tests := []struct {
		op, subject string
		call        func(c cloud.Cloud) error
	}{
		{"RunInstances", "", func(c cloud.Cloud) error {
			_, err := c.RunInstance(ctx, cloud.Launch{Type: "t4g.small", Tags: hosts})
			return err
		}},
		{"DescribeInstances", "i-test", func(c cloud.Cloud) error { _, err := c.DescribeInstance(ctx, "i-test"); return err }},
		{"DescribeInstances", "", func(c cloud.Cloud) error {
			return c.FindInstances(ctx, hosts, func([]cloud.Instance) bool { t.Fatal("unexpected callback"); return false })
		}},
		{"StopInstances", "i-test", func(c cloud.Cloud) error { return c.StopInstance(ctx, "i-test") }},
		{"StartInstances", "i-test", func(c cloud.Cloud) error { return c.StartInstance(ctx, "i-test") }},
		{"TerminateInstances", "i-test", func(c cloud.Cloud) error { return c.TerminateInstance(ctx, "i-test") }},
		{"ModifyInstanceAttribute", "i-test", func(c cloud.Cloud) error { return c.SetInstanceType(ctx, "i-test", "m7g.medium") }},
		{"AllocateAddress", "", func(c cloud.Cloud) error { _, err := c.AllocateAddress(ctx, hosts); return err }},
		{"DescribeAddresses", "eip-test", func(c cloud.Cloud) error { _, err := c.DescribeAddress(ctx, "eip-test"); return err }},
		{"DescribeAddresses", "", func(c cloud.Cloud) error {
			return c.FindAddresses(ctx, hosts, func([]cloud.Address) bool { t.Fatal("unexpected callback"); return false })
		}},
		{"AssociateAddress", "eip-test", func(c cloud.Cloud) error { _, err := c.AssociateAddress(ctx, "eip-test", "i-test"); return err }},
		{"DisassociateAddress", "assoc-test", func(c cloud.Cloud) error { return c.DisassociateAddress(ctx, "assoc-test") }},
		{"ReleaseAddress", "eip-test", func(c cloud.Cloud) error { return c.ReleaseAddress(ctx, "eip-test") }},
		{"CreateVolume", "", func(c cloud.Cloud) error {
			_, err := c.CreateVolume(ctx, cloud.VolumeSpec{AZ: "us-east-2a", SizeGB: 12, Tags: data})
			return err
		}},
		{"DescribeVolumes", "vol-test", func(c cloud.Cloud) error { _, err := c.DescribeVolume(ctx, "vol-test"); return err }},
		{"DescribeVolumes", "", func(c cloud.Cloud) error {
			return c.FindVolumes(ctx, data, func([]cloud.Volume) bool { t.Fatal("unexpected callback"); return false })
		}},
		{"AttachVolume", "vol-test", func(c cloud.Cloud) error { return c.AttachVolume(ctx, "vol-test", "i-test", "/dev/sdf") }},
		{"ModifyVolume", "vol-test", func(c cloud.Cloud) error { return c.ModifyVolume(ctx, "vol-test", 100) }},
		{"DescribeVolumesModifications", "vol-test", func(c cloud.Cloud) error { _, err := c.VolumeModification(ctx, "vol-test"); return err }},
		{"DeleteVolume", "vol-test", func(c cloud.Cloud) error { return c.DeleteVolume(ctx, "vol-test") }},
		{"CreateTags", "vol-test", func(c cloud.Cloud) error {
			return c.TagResource(ctx, "vol-test", cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue})
		}},
	}
	for _, test := range tests {
		t.Run(test.op+"/"+test.subject, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				form(t, r, test.op)
				if auth := r.Header.Get("Authorization"); !strings.Contains(auth, "/"+region+"/ec2/aws4_request") {
					t.Error("request was not signed for EC2 in the configured region")
				}
				return xmlResponse(403, failureXML("UnauthorizedOperation")), nil
			})
			err := test.call(c)
			checkError(t, err, "ec2", test.op, test.subject, "UnauthorizedOperation", cloud.ClassRefused)
			if calls != 1 {
				t.Errorf("requests = %d, want 1", calls)
			}
		})
	}
}

func TestDeleteVolume(t *testing.T) {
	// R-7KQ3-HPFX
	const id = "vol-to-delete"
	for _, tc := range []struct {
		name, code string
		status     int
		class      cloud.Class
	}{
		{name: "accepted", status: http.StatusOK},
		{name: "gone", code: "InvalidVolume.NotFound", status: http.StatusBadRequest, class: cloud.ClassNotFound},
		{name: "attached", code: "VolumeInUse", status: http.StatusBadRequest, class: cloud.ClassRefused},
		{name: "transient", code: "IncorrectState", status: http.StatusBadRequest, class: cloud.ClassTransient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				checkForm(t, form(t, r, "DeleteVolume"), map[string]string{"VolumeId": id})
				if tc.code != "" {
					return xmlResponse(tc.status, failureXML(tc.code)), nil
				}
				return xmlResponse(tc.status, "<DeleteVolumeResponse><return>true</return></DeleteVolumeResponse>"), nil
			})
			err := c.DeleteVolume(t.Context(), id)
			if calls != 1 {
				t.Errorf("requests = %d, want 1", calls)
			}
			if tc.code == "" {
				if err != nil {
					t.Fatalf("accepted delete: %v", err)
				}
				return
			}
			checkError(t, err, "ec2", "DeleteVolume", id, tc.code, tc.class)
			if want := "ec2 DeleteVolume " + id + ": " + tc.code; err.Error() != want {
				t.Errorf("delete error = %q, want %q", err, want)
			}
		})
	}
}

func TestTagContract(t *testing.T) {
	// R-XLCQ-KDUB
	ctx := t.Context()
	c := open(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("a refused call reached the transport")
		return nil, nil
	})
	run := func(common, instance cloud.Tags, typ string) error {
		_, err := c.RunInstance(ctx, cloud.Launch{Tags: common, InstanceTags: instance, Type: typ})
		return err
	}
	findDenied := func(tags cloud.Tags) {
		t.Helper()
		checkError(t, c.FindInstances(ctx, tags, func([]cloud.Instance) bool {
			t.Fatal("unexpected instance callback")
			return false
		}), "ec2", "DescribeInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
		checkError(t, c.FindVolumes(ctx, tags, func([]cloud.Volume) bool {
			t.Fatal("unexpected volume callback")
			return false
		}), "ec2", "DescribeVolumes", "", cloud.CodeTagContract, cloud.ClassRefused)
		checkError(t, c.FindAddresses(ctx, tags, func([]cloud.Address) bool {
			t.Fatal("unexpected address callback")
			return false
		}), "ec2", "DescribeAddresses", "", cloud.CodeTagContract, cloud.ClassRefused)
	}
	hosts := cloud.Tags{cloud.TagKind: cloud.KindHost}
	for _, key := range []string{"Space", "Domain"} {
		withKind := cloud.Tags{cloud.TagKind: cloud.KindHost, key: ""}
		forbidden := cloud.Tags{key: ""}
		checkError(t, run(withKind, nil, "t4g.small"), "ec2", "RunInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
		checkError(t, run(hosts, forbidden, "t4g.small"), "ec2", "RunInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
		_, err := c.AllocateAddress(ctx, withKind)
		checkError(t, err, "ec2", "AllocateAddress", "", cloud.CodeTagContract, cloud.ClassRefused)
		_, err = c.CreateVolume(ctx, cloud.VolumeSpec{Tags: cloud.Tags{cloud.TagKind: cloud.KindHostData, key: ""}})
		checkError(t, err, "ec2", "CreateVolume", "", cloud.CodeTagContract, cloud.ClassRefused)
		checkError(t, c.TagResource(ctx, "vol-test", forbidden), "ec2", "CreateTags", "vol-test", cloud.CodeTagContract, cloud.ClassRefused)
		findDenied(forbidden)
	}

	findDenied(nil)
	checkError(t, run(nil, nil, "t4g.small"), "ec2", "RunInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
	checkError(t, run(hosts, cloud.Tags{cloud.TagKind: ""}, "t4g.small"), "ec2", "RunInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
	checkError(t, run(hosts, nil, "t4g-small"), "ec2", "RunInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
	checkError(t, run(hosts, nil, "t3.medium"), "ec2", "RunInstances", "", cloud.CodeTagContract, cloud.ClassRefused)
	_, err := c.AllocateAddress(ctx, cloud.Tags{cloud.TagKind: cloud.KindHostData})
	checkError(t, err, "ec2", "AllocateAddress", "", cloud.CodeTagContract, cloud.ClassRefused)
	_, err = c.CreateVolume(ctx, cloud.VolumeSpec{Tags: hosts})
	checkError(t, err, "ec2", "CreateVolume", "", cloud.CodeTagContract, cloud.ClassRefused)
	checkError(t, c.TagResource(ctx, "vol-test", cloud.Tags{cloud.TagKind: ""}), "ec2", "CreateTags", "vol-test", cloud.CodeTagContract, cloud.ClassRefused)
}

func TestAllowedLaunchFamilies(t *testing.T) {
	// R-XLCQ-KDUB
	c := open(t, func(r *http.Request) (*http.Response, error) {
		form(t, r, "RunInstances")
		return xmlResponse(200, `<RunInstancesResponse><instancesSet><item><instanceId>i-test</instanceId></item></instancesSet></RunInstancesResponse>`), nil
	})
	for _, typ := range []string{"t4g.small", "m7g.large", "c7g.medium", "r7g.large"} {
		l := cloud.Launch{Type: typ, Tags: cloud.Tags{cloud.TagKind: cloud.KindHost}}
		got, err := c.RunInstance(t.Context(), l)
		if got.ID != "i-test" || err != nil {
			t.Errorf("RunInstance(%q) = %+v, %v", typ, got, err)
		}
	}
}

func TestRoute53AndSTSOperationMappings(t *testing.T) {
	// R-X95Q-QOFD
	tests := []struct {
		service, op, subject, path, method string
		call                               func(context.Context, cloud.Cloud) error
	}{
		{"route53", "ListResourceRecordSets", "name.test", "/2013-04-01/hostedzone/zone/rrset", "GET",
			func(ctx context.Context, c cloud.Cloud) error {
				_, err := c.FindRecords(ctx, "zone", "name.test")
				return err
			}},
		{"route53", "ChangeResourceRecordSets", "name.test", "/2013-04-01/hostedzone/zone/rrset", "POST",
			func(ctx context.Context, c cloud.Cloud) error {
				_, err := c.ChangeRecord(ctx, "zone", cloud.RecordChange{Action: cloud.RecordDelete, Name: "name.test"})
				return err
			}},
		{"route53", "GetChange", "changeid", "/2013-04-01/change/changeid", "GET",
			func(ctx context.Context, c cloud.Cloud) error { _, err := c.ChangeInsync(ctx, "changeid"); return err }},
		{"sts", "GetCallerIdentity", "", "/", "POST",
			func(ctx context.Context, c cloud.Cloud) error { _, err := c.Account(ctx); return err }},
		{"sts", "GetCallerIdentity", "", "/", "GET",
			func(ctx context.Context, c cloud.Cloud) error {
				_, err := c.CallerOf(ctx, "https://sts.us-east-2.amazonaws.com/?Action=GetCallerIdentity", "test-binding")
				return err
			}},
	}
	for _, test := range tests {
		t.Run(test.op+"/"+test.method, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != test.path || r.Method != test.method {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				if test.service == "sts" {
					if test.method == "POST" {
						form(t, r, test.op)
					}
					return xmlResponse(403, `<ErrorResponse><Error><Code>AccessDenied</Code><Message>private text</Message></Error></ErrorResponse>`), nil
				}
				return xmlResponse(403, `<ErrorResponse><Error><Code>AccessDenied</Code><Message>private text</Message></Error></ErrorResponse>`), nil
			})
			wantCode := "AccessDenied"
			if test.service == "sts" && test.method == "GET" {
				wantCode = "HTTP 403"
			}
			checkError(t, test.call(t.Context(), c), test.service, test.op, test.subject, wantCode, cloud.ClassRefused)
			if calls != 1 {
				t.Errorf("requests = %d, want 1", calls)
			}
		})
	}
}

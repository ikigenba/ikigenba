package awssdk_test

import (
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
)

const instanceXML = `<instanceId>i-test</instanceId><instanceState><name>pending</name></instanceState>
<instanceType>t4g.medium</instanceType><placement><availabilityZone>us-east-2a</availabilityZone></placement>
<rootDeviceName>/dev/xvda</rootDeviceName><blockDeviceMapping>
<item><deviceName>/dev/sdf</deviceName><ebs><volumeId>vol-data</volumeId></ebs></item>
<item><deviceName>/dev/xvda</deviceName><ebs><volumeId>vol-root</volumeId></ebs></item>
</blockDeviceMapping><tagSet><item><key>Kind</key><value>host</value></item><item><key>Host</key><value>host01</value></item></tagSet>`

func checkTagSpecifications(t *testing.T, params url.Values, want map[string]cloud.Tags) {
	t.Helper()
	got := make(map[string]cloud.Tags)
	for key := range params {
		prefix, ok := strings.CutSuffix(key, ".ResourceType")
		if !ok || !strings.HasPrefix(prefix, "TagSpecification.") {
			continue
		}
		resource := params.Get(key)
		params.Del(key)
		if _, duplicate := got[resource]; duplicate {
			t.Fatalf("duplicate specification for %s", resource)
		}
		tags := make(cloud.Tags)
		for tagKey := range params {
			item, ok := strings.CutSuffix(tagKey, ".Key")
			if !ok || !strings.HasPrefix(item, prefix+".Tag.") {
				continue
			}
			name := params.Get(tagKey)
			if _, duplicate := tags[name]; duplicate || !params.Has(item+".Value") {
				t.Fatalf("invalid %s tag named %s", resource, name)
			}
			tags[name] = params.Get(item + ".Value")
			params.Del(tagKey)
			params.Del(item + ".Value")
		}
		got[resource] = tags
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tag specifications = %v, want %v", got, want)
	}
}

func TestInstanceRequests(t *testing.T) {
	// R-XADN-4G62 R-XBLJ-I7WR
	want := cloud.Instance{ID: "i-test", State: cloud.InstancePending, Type: "t4g.medium", AZ: "us-east-2a",
		RootVolume: "vol-root", Tags: cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagHost: "host01"}}
	ctx := t.Context()
	c := open(t, func(r *http.Request) (*http.Response, error) {
		params := form(t, r, "RunInstances")
		checkTagSpecifications(t, params, map[string]cloud.Tags{
			"instance":          {"AccountsURL": "https://accounts.test", "Host": "instance-host", "Kind": "host"},
			"volume":            {"Host": "host01", "Kind": "host"},
			"network-interface": {"Host": "host01", "Kind": "host"},
		})
		checkForm(t, params, map[string]string{
			"MinCount": "1", "MaxCount": "1",
			"LaunchTemplate.LaunchTemplateName": "launch-template", "LaunchTemplate.Version": "$Default",
			"IamInstanceProfile.Name": "instance-profile", "InstanceType": "t4g.medium", "ClientToken": "launch-token",
			"BlockDeviceMapping.1.DeviceName":     "/dev/xvda",
			"BlockDeviceMapping.1.Ebs.VolumeType": "gp3", "BlockDeviceMapping.1.Ebs.VolumeSize": "20",
			"BlockDeviceMapping.1.Ebs.DeleteOnTermination": "true",
		})
		return xmlResponse(200, "<RunInstancesResponse><instancesSet><item>"+instanceXML+
			"</item><item><instanceId>i-other</instanceId></item></instancesSet></RunInstancesResponse>"), nil
	})
	l := cloud.Launch{
		Template: "launch-template", InstanceProfile: "instance-profile", Type: "t4g.medium", RootGB: 20, ClientToken: "launch-token",
		Tags:         cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagHost: "host01"},
		InstanceTags: cloud.Tags{cloud.TagAccountsURL: "https://accounts.test", cloud.TagHost: "instance-host"},
	}
	got, err := c.RunInstance(ctx, l)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("RunInstance = %+v, %v, want %+v", got, err, want)
	}

	c = open(t, func(r *http.Request) (*http.Response, error) {
		form(t, r, "DescribeInstances")
		return xmlResponse(200, "<DescribeInstancesResponse><reservationSet><item><instancesSet>"+
			"<item><instanceId>i-other</instanceId></item><item>"+instanceXML+"</item>"+
			"</instancesSet></item></reservationSet></DescribeInstancesResponse>"), nil
	})
	got, err = c.DescribeInstance(ctx, "i-test")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DescribeInstance = %+v, %v, want %+v", got, err, want)
	}
	rootless, err := c.DescribeInstance(ctx, "i-other")
	if err != nil || rootless.ID != "i-other" || rootless.RootVolume != "" {
		t.Fatalf("DescribeInstance without a root device = %+v, %v", rootless, err)
	}
	_, err = c.DescribeInstance(ctx, "i-missing")
	checkError(t, err, "ec2", "DescribeInstances", "i-missing", "InvalidInstanceID.NotFound", cloud.ClassNotFound)
}

func TestInstanceStateRequests(t *testing.T) {
	// R-XE1C-9RE5
	tests := []struct {
		action string
		params map[string]string
		call   func(cloud.Cloud) error
	}{
		{"StopInstances", map[string]string{"InstanceId.1": "i-test"},
			func(c cloud.Cloud) error { return c.StopInstance(t.Context(), "i-test") }},
		{"StartInstances", map[string]string{"InstanceId.1": "i-test"},
			func(c cloud.Cloud) error { return c.StartInstance(t.Context(), "i-test") }},
		{"TerminateInstances", map[string]string{"InstanceId.1": "i-test"},
			func(c cloud.Cloud) error { return c.TerminateInstance(t.Context(), "i-test") }},
		{"ModifyInstanceAttribute", map[string]string{"InstanceId": "i-test", "InstanceType.Value": "c7g.small"},
			func(c cloud.Cloud) error { return c.SetInstanceType(t.Context(), "i-test", "c7g.small") }},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			c := open(t, func(r *http.Request) (*http.Response, error) {
				checkForm(t, form(t, r, test.action), test.params)
				return xmlResponse(200, "<"+test.action+"Response/>"), nil
			})
			if err := test.call(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAddresses(t *testing.T) {
	// R-XF98-NJ4U R-XGH5-1AVJ R-K5WO-FXYP
	ctx := t.Context()
	tags := cloud.Tags{cloud.TagKind: cloud.KindHost}
	findCalls := 0
	c := open(t, func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch r.Form.Get("Action") {
		case "AllocateAddress":
			params := form(t, r, "AllocateAddress")
			checkTagSpecifications(t, params, map[string]cloud.Tags{"elastic-ip": {"Kind": "host"}})
			checkForm(t, params, map[string]string{"Domain": "vpc"})
			return xmlResponse(200, `<AllocateAddressResponse><allocationId>eip-test</allocationId><publicIp>192.0.2.10</publicIp></AllocateAddressResponse>`), nil
		case "DescribeAddresses":
			params := form(t, r, "DescribeAddresses")
			switch params.Get("AllocationId.1") {
			case "eip-test":
				checkForm(t, params, map[string]string{"AllocationId.1": "eip-test"})
			case "eip-missing":
				return xmlResponse(200, `<DescribeAddressesResponse><addressesSet/></DescribeAddressesResponse>`), nil
			default:
				findCalls++
				checkForm(t, params, map[string]string{
					"Filter.1.Name": "tag:Host", "Filter.1.Value.1": "host01", "Filter.2.Name": "tag:Kind", "Filter.2.Value.1": "host",
				})
			}
			return xmlResponse(200, `<DescribeAddressesResponse><addressesSet>
				<item><allocationId>eip-test</allocationId><publicIp>192.0.2.10</publicIp>
				<associationId>eipassoc-test</associationId><instanceId>i-test</instanceId>
				<tagSet><item><key>Kind</key><value>host</value></item></tagSet></item>
				<item><allocationId>eip-other</allocationId></item></addressesSet></DescribeAddressesResponse>`), nil
		case "AssociateAddress":
			checkForm(t, form(t, r, "AssociateAddress"), map[string]string{"AllocationId": "eip-test", "InstanceId": "i-test"})
			return xmlResponse(200, `<AssociateAddressResponse><associationId>eipassoc-test</associationId></AssociateAddressResponse>`), nil
		case "DisassociateAddress":
			checkForm(t, form(t, r, "DisassociateAddress"), map[string]string{"AssociationId": "eipassoc-test"})
			return xmlResponse(400, failureXML("InvalidAssociationID.NotFound")), nil
		case "ReleaseAddress":
			params := form(t, r, "ReleaseAddress")
			if params.Get("AllocationId") == "eip-test" {
				checkForm(t, params, map[string]string{"AllocationId": "eip-test"})
				return xmlResponse(400, failureXML("InvalidAllocationID.NotFound")), nil
			}
			return xmlResponse(400, failureXML("UnauthorizedOperation")), nil
		default:
			t.Fatalf("unexpected action %v", r.Form)
			return nil, nil
		}
	})
	want := cloud.Address{AllocationID: "eip-test", IP: "192.0.2.10", Tags: tags}
	got, err := c.AllocateAddress(ctx, tags)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("AllocateAddress = %+v, %v, want %+v", got, err, want)
	}
	want.AssociationID, want.InstanceID = "eipassoc-test", "i-test"
	got, err = c.DescribeAddress(ctx, "eip-test")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DescribeAddress = %+v, %v, want %+v", got, err, want)
	}

	_, err = c.DescribeAddress(ctx, "eip-missing")
	checkError(t, err, "ec2", "DescribeAddresses", "eip-missing", "InvalidAllocationID.NotFound", cloud.ClassNotFound)

	for _, keepGoing := range []bool{false, true} {
		var found []cloud.Address
		findCalls = 0
		callbacks := 0
		err = c.FindAddresses(ctx, cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagHost: "host01"}, func(page []cloud.Address) bool {
			callbacks++
			found = append(found, page...)
			return keepGoing
		})
		if err != nil || findCalls != 1 || callbacks != 1 || len(found) != 2 || !reflect.DeepEqual(found[0], want) ||
			found[1].AllocationID != "eip-other" || found[1].AssociationID != "" || found[1].InstanceID != "" || found[1].IP != "" || len(found[1].Tags) != 0 {
			t.Fatalf("FindAddresses = %+v, %v; requests = %d, callbacks = %d", found, err, findCalls, callbacks)
		}
	}

	association, err := c.AssociateAddress(ctx, "eip-test", "i-test")
	if association != "eipassoc-test" || err != nil {
		t.Fatalf("AssociateAddress = %q, %v", association, err)
	}
	if err := c.DisassociateAddress(ctx, "eipassoc-test"); err != nil {
		t.Fatalf("DisassociateAddress absent = %v", err)
	}
	if err := c.ReleaseAddress(ctx, "eip-test"); err != nil {
		t.Fatalf("ReleaseAddress absent = %v", err)
	}
	checkError(t, c.ReleaseAddress(ctx, "eip-denied"), "ec2", "ReleaseAddress", "eip-denied", "UnauthorizedOperation", cloud.ClassRefused)
}

const volumeXML = `<volumeId>vol-test</volumeId><status>in-use</status><availabilityZone>us-east-2a</availabilityZone><size>32</size>
<attachmentSet><item><instanceId>i-test</instanceId><device>/dev/sdf</device><status>attached</status></item>
<item><instanceId>i-other</instanceId><device>/dev/sdg</device><status>attaching</status></item></attachmentSet>
<tagSet><item><key>Kind</key><value>host-data</value></item><item><key>Host</key><value>host01</value></item></tagSet>`

func TestVolumes(t *testing.T) {
	// R-XHP1-F2M8 R-XK4U-6M3M
	ctx := t.Context()
	c := open(t, func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		action := r.Form.Get("Action")
		params := form(t, r, action)
		switch action {
		case "CreateVolume":
			checkTagSpecifications(t, params, map[string]cloud.Tags{"volume": {"Host": "host01", "Kind": "host-data"}})
			checkForm(t, params, map[string]string{
				"AvailabilityZone": "us-east-2a", "Size": "32", "VolumeType": "gp3", "ClientToken": "volume-token",
			})
			return xmlResponse(200, "<CreateVolumeResponse>"+volumeXML+"</CreateVolumeResponse>"), nil
		case "DescribeVolumes":
			return xmlResponse(200, "<DescribeVolumesResponse><volumeSet><item><volumeId>vol-root</volumeId></item><item>"+
				volumeXML+"</item></volumeSet></DescribeVolumesResponse>"), nil
		case "AttachVolume":
			checkForm(t, params, map[string]string{"VolumeId": "vol-test", "InstanceId": "i-test", "Device": "/dev/sdf"})
		case "ModifyVolume":
			checkForm(t, params, map[string]string{"VolumeId": "vol-test", "Size": "64"})
		case "CreateTags":
			checkForm(t, params, map[string]string{"ResourceId.1": "vol-test", "Tag.1.Key": "Orphaned", "Tag.1.Value": "true"})
		case "DescribeVolumesModifications":
			switch params.Get("VolumeId.1") {
			case "vol-test":
				checkForm(t, params, map[string]string{"VolumeId.1": "vol-test"})
				return xmlResponse(200, `<DescribeVolumesModificationsResponse><volumeModificationSet>
					<item><volumeId>vol-test</volumeId><startTime>2026-01-01T09:21:00Z</startTime><modificationState>optimizing</modificationState><targetSize>64</targetSize></item>
					<item><volumeId>vol-other</volumeId><startTime>2026-01-01T09:22:00Z</startTime><modificationState>completed</modificationState><targetSize>90</targetSize></item>
					<item><volumeId>vol-test</volumeId><startTime>2026-01-01T09:20:00Z</startTime><modificationState>completed</modificationState><targetSize>48</targetSize></item>
					</volumeModificationSet></DescribeVolumesModificationsResponse>`), nil
			case "vol-missing":
				return xmlResponse(400, failureXML("InvalidVolumeModification.NotFound")), nil
			}
			return xmlResponse(200, `<DescribeVolumesModificationsResponse><volumeModificationSet/></DescribeVolumesModificationsResponse>`), nil
		default:
			t.Fatalf("unexpected action %q", action)
		}
		return xmlResponse(200, "<"+action+"Response/>"), nil
	})

	tags := cloud.Tags{cloud.TagKind: cloud.KindHostData, cloud.TagHost: "host01"}
	want := cloud.Volume{ID: "vol-test", State: cloud.VolumeInUse, AZ: "us-east-2a", SizeGB: 32, Tags: tags,
		Attachment: cloud.Attachment{InstanceID: "i-test", Device: "/dev/sdf", State: cloud.AttachmentAttached}}
	got, err := c.CreateVolume(ctx, cloud.VolumeSpec{AZ: "us-east-2a", SizeGB: 32, ClientToken: "volume-token", Tags: tags})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("CreateVolume = %+v, %v, want %+v", got, err, want)
	}
	got, err = c.DescribeVolume(ctx, "vol-test")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DescribeVolume = %+v, %v, want %+v", got, err, want)
	}
	got, err = c.DescribeVolume(ctx, "vol-root")
	if err != nil || got.ID != "vol-root" || got.Attachment != (cloud.Attachment{}) {
		t.Fatalf("DescribeVolume without attachments = %+v, %v", got, err)
	}
	_, err = c.DescribeVolume(ctx, "vol-missing")
	checkError(t, err, "ec2", "DescribeVolumes", "vol-missing", "InvalidVolume.NotFound", cloud.ClassNotFound)

	if err := c.AttachVolume(ctx, "vol-test", "i-test", "/dev/sdf"); err != nil {
		t.Fatal(err)
	}
	if err := c.ModifyVolume(ctx, "vol-test", 64); err != nil {
		t.Fatal(err)
	}
	if err := c.TagResource(ctx, "vol-test", cloud.Tags{cloud.TagOrphaned: cloud.OrphanedTrue}); err != nil {
		t.Fatal(err)
	}
	mod, err := c.VolumeModification(ctx, "vol-test")
	if err != nil || mod != (cloud.Modification{State: cloud.ModificationOptimizing, TargetGB: 64}) {
		t.Fatalf("VolumeModification = %+v, %v", mod, err)
	}
	for _, id := range []string{"vol-missing", "vol-empty"} {
		mod, err := c.VolumeModification(ctx, id)
		if err != nil || mod != (cloud.Modification{}) {
			t.Errorf("VolumeModification(%q) = %+v, %v", id, mod, err)
		}
	}
}

func TestFindPages(t *testing.T) {
	// R-K5WO-FXYP
	for _, kind := range []string{"instances", "volumes"} {
		for _, test := range []struct {
			name    string
			pages   [][]string
			stop    bool
			visited int
		}{
			{
				name: "callback stops", pages: [][]string{{"z", "a"}, {"m"}, {"b"}},
				stop: true, visited: 2,
			},
			{
				name: "exhausted with an empty page", pages: [][]string{{"z", "a"}, {}, {"m"}},
				visited: 3,
			},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				ctx := t.Context()
				requests, callbacks := 0, 0
				var expectedCallbacks int
				for _, page := range test.pages[:test.visited] {
					if len(page) != 0 {
						expectedCallbacks++
					}
				}
				c := open(t, func(r *http.Request) (*http.Response, error) {
					requests++
					if requests > test.visited {
						t.Fatalf("unexpected page request %d", requests)
					}
					want := map[string]string{
						"Filter.1.Name": "tag:Kind", "Filter.1.Value.1": "host",
						"Filter.2.Name": "tag:Operation", "Filter.2.Value.1": "op-test",
					}
					if requests > 1 {
						want["NextToken"] = strconv.Itoa(requests)
					}

					var action, begin, end, idField string
					if kind == "instances" {
						action, begin, end, idField = "DescribeInstances",
							"<reservationSet><item><instancesSet>", "</instancesSet></item></reservationSet>", "instanceId"
						want["Filter.3.Name"] = "instance-state-name"
						for i, state := range []string{"pending", "running", "shutting-down", "stopping", "stopped"} {
							key := "Filter.3.Value." + string(rune('1'+i))
							want[key] = state
						}
					} else {
						action, begin, end, idField = "DescribeVolumes", "<volumeSet>", "</volumeSet>", "volumeId"
					}
					checkForm(t, form(t, r, action), want)

					response := "<" + action + "Response>" + begin
					for _, id := range test.pages[requests-1] {
						response += "<item><" + idField + ">" + id + "</" + idField + "></item>"
					}
					response += end
					if requests < len(test.pages) {
						response += "<nextToken>" + strconv.Itoa(requests+1) + "</nextToken>"
					}
					response += "</" + action + "Response>"
					return xmlResponse(200, response), nil
				})
				check := func(ids []string) bool {
					t.Helper()
					callbacks++
					if len(ids) == 0 || !reflect.DeepEqual(ids, test.pages[requests-1]) {
						t.Fatalf("callback on request %d: %v, want %v", requests, ids, test.pages[requests-1])
					}
					return !test.stop || callbacks < expectedCallbacks
				}
				tags := cloud.Tags{cloud.TagKind: cloud.KindHost, cloud.TagOperation: "op-test"}
				var err error
				if kind == "instances" {
					err = c.FindInstances(ctx, tags, func(page []cloud.Instance) bool {
						var ids []string
						for _, item := range page {
							ids = append(ids, item.ID)
						}
						return check(ids)
					})
				} else {
					err = c.FindVolumes(ctx, tags, func(page []cloud.Volume) bool {
						var ids []string
						for _, item := range page {
							ids = append(ids, item.ID)
						}
						return check(ids)
					})
				}
				if err != nil || requests != test.visited || callbacks != expectedCallbacks {
					t.Fatalf("requests = %d, callbacks = %d, err = %v", requests, callbacks, err)
				}
			})
		}
	}
}

func TestFindUsesLiteralTagValues(t *testing.T) {
	// R-K5WO-FXYP
	tags := cloud.Tags{cloud.TagHost: "*amazon?\\"}
	tests := map[string]func(*testing.T, cloud.Cloud) error{
		"DescribeInstances": func(t *testing.T, c cloud.Cloud) error {
			return c.FindInstances(t.Context(), tags, func([]cloud.Instance) bool { t.Fatal("callback on empty response"); return false })
		},
		"DescribeAddresses": func(t *testing.T, c cloud.Cloud) error {
			return c.FindAddresses(t.Context(), tags, func([]cloud.Address) bool { t.Fatal("callback on empty response"); return false })
		},
		"DescribeVolumes": func(t *testing.T, c cloud.Cloud) error {
			return c.FindVolumes(t.Context(), tags, func([]cloud.Volume) bool { t.Fatal("callback on empty response"); return false })
		},
	}
	for op, find := range tests {
		t.Run(op, func(t *testing.T) {
			calls := 0
			c := open(t, func(r *http.Request) (*http.Response, error) {
				calls++
				params := form(t, r, op)
				if params.Get("Filter.1.Name") != "tag:Host" || params.Get("Filter.1.Value.1") != "\\*amazon\\?\\\\" {
					t.Errorf("EC2 tag filter = %v", params)
				}
				return xmlResponse(200, "<"+op+"Response/>"), nil
			})
			if err := find(t, c); err != nil || calls != 1 {
				t.Errorf("find = %v, %d requests", err, calls)
			}
		})
	}
}

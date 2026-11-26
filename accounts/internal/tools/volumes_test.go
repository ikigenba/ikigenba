package tools_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

func listKept(t *testing.T, f *fixture, args any) tools.KeptVolumeList {
	t.Helper()
	return answer[tools.KeptVolumeList](t, f.call(tools.ToolKeptVolumes, args))
}

func (f *fixture) finishKeeping(host store.Host) tools.KeptVolume {
	f.t.Helper()
	_, op, err := f.st.DeleteHost(f.t.Context(), host.Name, true, f.caller)
	if err != nil {
		f.t.Fatal(err)
	}
	f.finish(op.ID, store.Update{})
	return tools.KeptVolume{
		Volume: host.DataVolume, Host: host.Name, SizeGB: host.DataGB,
		Deleted: f.now.UTC().Format(time.RFC3339), State: store.VolumeKept,
	}
}

func (f *fixture) keptOnRecord(volume string) store.KeptVolume {
	f.t.Helper()
	kept, err := f.st.KeptVolume(f.t.Context(), volume)
	if err != nil {
		f.t.Fatal(err)
	}
	return kept
}

func TestKeptVolumePaging(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	ctx := t.Context()
	noArgs := tools.KeptVolumesArgs{}
	empty := listKept(t, f, noArgs)
	if empty.KeptVolumes == nil || len(empty.KeptVolumes) != 0 || empty.Next != nil {
		t.Fatal(empty)
	}
	var hosts []store.Host
	for range 5 {
		host, _ := f.seedHost(true)
		hosts = append(hosts, f.host(host.Name))
	}
	old := f.finishKeeping(hosts[0])
	removed := f.finishKeeping(hosts[1])
	if _, err := f.st.DeleteKeptVolume(ctx, removed.Volume, f.caller); err != nil {
		t.Fatal(err)
	}
	if err := f.st.FinishVolumeDelete(ctx, removed.Volume); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	sameSecond := f.finishKeeping(hosts[2])
	latest := f.finishKeeping(hosts[3])
	const reason = "delete was refused"
	if _, err := f.st.DeleteKeptVolume(ctx, latest.Volume, f.caller); err != nil {
		t.Fatal(err)
	}
	if err := f.st.FailVolumeDelete(ctx, latest.Volume, reason); err != nil {
		t.Fatal(err)
	}
	latest.Reason = reason

	want := []tools.KeptVolume{latest, sameSecond, old}
	if got := listKept(t, f, noArgs); !slices.Equal(got.KeptVolumes, want) || got.Next != nil {
		t.Fatalf("kept volumes %+v, want %v", got, want)
	}
	one := 1
	token := paging.Token{Tool: tools.ToolKeptVolumes}
	var page *string
	for i, volume := range want {
		got := listKept(t, f, tools.KeptVolumesArgs{Page: page, Limit: &one})
		if !slices.Equal(got.KeptVolumes, []tools.KeptVolume{volume}) {
			t.Fatalf("page %d came back as %+v, want %v", i, got, volume)
		}
		if i == len(want)-1 {
			if got.Next != nil {
				t.Fatal("last page has another page", got)
			}
			break
		}
		wantToken(t, got.Next, token, f.now.Truncate(time.Second).UnixNano(), volume.Volume)
		page = got.Next
	}
	// A well-formed token need not point at an existing entry.
	fractional := token.Encode(f.now.Truncate(time.Second).Add(time.Nanosecond).UnixNano(), "")
	if got := listKept(t, f, tools.KeptVolumesArgs{Page: &fractional}); !slices.Equal(got.KeptVolumes, want) || got.Next != nil {
		t.Fatalf("kept volumes after a sub-second cursor %+v, want %v", got, want)
	}
}

// R-10TE-Y2PM R-15P0-H5OE
func TestKeptVolumeAnswers(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	ctx := t.Context()
	host, _ := f.seedHost(true)
	volume := f.finishKeeping(f.host(host.Name))
	if _, err := f.st.DeleteKeptVolume(ctx, volume.Volume, f.caller); err != nil {
		t.Fatal(err)
	}
	const reason = "provider refused deletion"
	if err := f.st.FailVolumeDelete(ctx, volume.Volume, reason); err != nil {
		t.Fatal(err)
	}
	volume.Reason = reason
	list := f.call(tools.ToolKeptVolumes, tools.KeptVolumesArgs{})
	answer[tools.KeptVolumeList](t, list)
	wantList := testEncoding(t, tools.KeptVolumeList{KeptVolumes: []tools.KeptVolume{volume}})
	if got := unpack(t, list).Structured; !bytes.Equal(got, wantList) {
		t.Fatalf("list result %s, want %s", got, wantList)
	}
	deleted := f.call(tools.ToolDeleteKeptVolume, tools.KeptVolumeArgs{Volume: volume.Volume})
	answer[tools.KeptVolume](t, deleted)
	volume.State, volume.Reason = store.VolumeDeleting, ""
	wantDeleted := testEncoding(t, volume)
	if got := unpack(t, deleted).Structured; !bytes.Equal(got, wantDeleted) {
		t.Fatalf("delete result %s, want %s", got, wantDeleted)
	}
}

// R-19CP-MGWH
func TestKeptVolumePagingArguments(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	f.seedHost(false)
	f.seedHost(false)
	one := 1
	hosts := answer[tools.HostList](t, f.call(tools.ToolHosts, tools.HostsArgs{Limit: &one}))
	if hosts.Next == nil {
		t.Fatal("hosts has no next token")
	}
	host, _ := f.seedHost(true)
	volume := f.finishKeeping(f.host(host.Name))
	for _, args := range []any{map[string]any{"page": ""}, json.RawMessage(`{"page":null,"limit":null}`)} {
		if got := listKept(t, f, args); !slices.Equal(got.KeptVolumes, []tools.KeptVolume{volume}) || got.Next != nil {
			t.Fatalf("page for %v = %+v", args, got)
		}
	}
	token := paging.Token{Tool: tools.ToolKeptVolumes}
	validPage := token.Encode(f.now.Truncate(time.Second).UnixNano(), volume.Volume)
	badPages := []string{"invalid-page", token.Encode("timestamp", "vol-id"), *hosts.Next}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, page := range badPages {
			refusal(t, f.call(tools.ToolKeptVolumes, tools.KeptVolumesArgs{Page: &page}), paging.BadPage)
		}
		for _, limit := range []int{0, paging.MaxLimit + 1} {
			_, err := paging.Window(&limit)
			refusal(t, f.call(tools.ToolKeptVolumes, tools.KeptVolumesArgs{Limit: &limit}), err.Error())
			for _, page := range append(badPages, validPage) {
				refusal(t, f.call(tools.ToolKeptVolumes, tools.KeptVolumesArgs{Page: &page, Limit: &limit}), err.Error())
			}
		}
	}
}

// R-121B-BUGB R-0NEI-QLJZ R-1D0E-RS4K R-1E8B-5JV9 R-1FG7-JBLY R-1GO3-X3CN
func TestDeleteKeptVolume(t *testing.T) {
	f := setup(t)
	f.serveLoopback()
	ctx := t.Context()
	host, _ := f.seedHost(true)
	other, _ := f.seedHost(true)
	live, _ := f.seedHost(true)
	live = f.host(live.Name)
	volume := f.finishKeeping(f.host(host.Name))
	neighbor := f.finishKeeping(f.host(other.Name))
	if _, err := f.st.DeleteKeptVolume(ctx, volume.Volume, f.caller); err != nil {
		t.Fatal(err)
	}
	if err := f.st.FailVolumeDelete(ctx, volume.Volume, "previous failure"); err != nil {
		t.Fatal(err)
	}
	neighborBefore := f.keptOnRecord(neighbor.Volume)
	var dueOnWake []store.KeptVolume
	f.now = f.now.Add(time.Minute)
	f.caller.UserID = "retrying-user"
	f.onWake = func() {
		var err error
		dueOnWake, err = f.st.DueVolumeDeletes(ctx, f.now, "", 2)
		if err != nil {
			t.Error(err)
		}
	}
	args := tools.KeptVolumeArgs{Volume: volume.Volume}
	volume.State = store.VolumeDeleting
	if got := answer[tools.KeptVolume](t, f.call(tools.ToolDeleteKeptVolume, args)); got != volume {
		t.Fatalf("delete answer %+v, want %+v", got, volume)
	}
	deleting := f.keptOnRecord(volume.Volume)
	if deleting.Caller.UserID != f.caller.UserID || deleting.State != store.VolumeDeleting || deleting.Reason != "" ||
		!deleting.NextTry.Equal(f.now.Truncate(time.Second)) || !slices.Equal(dueOnWake, []store.KeptVolume{deleting}) {
		t.Fatalf("volume after scheduling is %+v, at wake %+v", deleting, dueOnWake)
	}
	listed := listKept(t, f, tools.KeptVolumesArgs{})
	if !slices.Contains(listed.KeptVolumes, volume) {
		t.Fatal(listed)
	}
	refusal(t, f.call(tools.ToolDeleteKeptVolume, args), tools.VolumeDeleting)
	for _, unknown := range []string{"never-stored", live.DataVolume, ""} {
		refusal(t, f.call(tools.ToolDeleteKeptVolume, tools.KeptVolumeArgs{Volume: unknown}), tools.NoKeptVolume)
	}
	if after := f.keptOnRecord(volume.Volume); after != deleting {
		t.Fatalf("reads and refusals changed the record: got %+v, want %+v", after, deleting)
	}
	if err := f.st.FinishVolumeDelete(ctx, volume.Volume); err != nil {
		t.Fatal(err)
	}
	refusal(t, f.call(tools.ToolDeleteKeptVolume, args), tools.NoKeptVolume)
	if listed := listKept(t, f, tools.KeptVolumesArgs{}); !slices.Equal(listed.KeptVolumes, []tools.KeptVolume{neighbor}) {
		t.Fatal(listed)
	}
	f.d.SetFailing(true)
	refusal(t, f.call(tools.ToolDeleteKeptVolume, tools.KeptVolumeArgs{Volume: neighbor.Volume}), store.Unreachable)
	refusal(t, f.call(tools.ToolDeleteKeptVolume, tools.KeptVolumeArgs{}), store.Unreachable)
	f.d.SetFailing(false)
	if after := f.keptOnRecord(neighbor.Volume); after != neighborBefore || !reflect.DeepEqual(f.host(live.Name), live) {
		t.Fatalf("other records changed: volume %+v, host %+v", after, f.host(live.Name))
	}
	if f.wakes != 1 || len(f.cloud.lookups) != 0 {
		t.Fatalf("wakes %d, cloud lookups %v", f.wakes, f.cloud.lookups)
	}
	assertVolumeToolEvents(t, f, volume)
}

func assertVolumeToolEvents(t *testing.T, f *fixture, volume tools.KeptVolume) {
	t.Helper()
	changed := func(host, result string) telemetry.Attrs {
		return telemetry.Attrs{"tool": tools.ToolDeleteKeptVolume, "host": host, "account": "", "user": "", "ip": "", "result": result}
	}
	want := []struct {
		name  string
		attrs telemetry.Attrs
	}{
		{trail.EventVolumeChanged, telemetry.Attrs{"volume": volume.Volume, "host": volume.Host, "from": store.VolumeKept, "to": store.VolumeDeleting, "reason": ""}},
		{trail.EventChangeFinished, changed(volume.Host, trail.ResultOK)},
		{trail.EventChangeFinished, changed(volume.Host, trail.ResultRefused)},
		{trail.EventChangeFinished, changed("", trail.ResultRefused)},
		{trail.EventChangeFinished, changed("", trail.ResultRefused)},
		{trail.EventChangeFinished, changed("", trail.ResultRefused)},
		{trail.EventChangeFinished, changed("", trail.ResultRefused)},
		{trail.EventChangeFinished, changed("", trail.ResultFailed)},
		{trail.EventChangeFinished, changed("", trail.ResultFailed)},
	}
	events := slices.DeleteFunc(allEvents(t, f), func(e telemetry.Event) bool { return e.Name == "tool.called" })
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	for i, event := range events {
		expected := want[i]
		if event.Name != expected.name || !reflect.DeepEqual(event.Attrs, expected.attrs) || event.User != f.caller.UserID {
			t.Fatalf("event %d = %+v, want %+v", i, event, expected)
		}
	}
}

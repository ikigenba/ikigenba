package apex_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/apex"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

var _ func(context.Context, io.Writer, seam.Deps, cloud.Route53, cloud.Zone, string, string) error = apex.PointRoot

type recordRoute struct {
	cloud.Route53
	t                    *testing.T
	changes              []cloud.RecordChange
	polls, waits         int
	changeErr, statusErr error
}

func (f *recordRoute) ChangeRecords(_ context.Context, zone string, changes []cloud.RecordChange) (string, error) {
	if zone != "Z09565073GHK8BYWQ1A78" || f.changes != nil {
		f.t.Fatal(zone, changes)
	}
	f.changes = changes
	return "change", f.changeErr
}
func (f *recordRoute) ChangeStatus(_ context.Context, id string) (cloud.ChangeStatus, error) {
	if id != "change" {
		f.t.Fatal(id)
	}
	f.polls++
	if f.statusErr != nil {
		return "", f.statusErr
	}
	if f.polls == 1 {
		return cloud.ChangePending, nil
	}
	return cloud.ChangeInsync, nil
}
func TestPutRootRecord(t *testing.T) {
	// R-F2VH-ZFD3 R-F8YZ-WA2K
	cause := &cloud.Error{Service: "route53", Operation: "ChangeResourceRecordSets", Code: "Throttling"}
	for _, stage := range []string{"", "change", "status"} {
		f := &recordRoute{t: t}
		if stage == "change" {
			f.changeErr = cause
		}
		if stage == "status" {
			f.statusErr = errors.New("status sentinel")
		}
		deps := seam.Deps{After: func(d time.Duration) <-chan time.Time {
			if d != space.PollInterval {
				t.Fatal(d)
			}
			f.waits++
			ch := make(chan time.Time)
			close(ch)
			return ch
		}}
		var out bytes.Buffer
		err := apex.PointRoot(t.Context(), &out, deps, f, cloud.Zone{ID: "Z09565073GHK8BYWQ1A78"}, "ikigenba.dev", "18.118.7.42")
		want := []cloud.RecordChange{{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: space.RecordTTL, Values: []string{"18.118.7.42"}}}}
		if !reflect.DeepEqual(f.changes, want) {
			t.Fatal(f.changes)
		}
		switch stage {
		case "":
			if err != nil || f.polls != 2 || f.waits != 1 || out.String() != "record: ok (ikigenba.dev -> 18.118.7.42, INSYNC)\n" {
				t.Fatalf("%v %d %d", err, f.polls, f.waits)
			}
		case "change":
			if !errors.Is(err, cause) || f.polls != 0 || f.waits != 0 || out.Len() != 0 {
				t.Fatalf("%v %d", err, f.polls)
			}
		case "status":
			if !errors.Is(err, f.statusErr) || f.polls != 1 || out.Len() != 0 {
				t.Fatal(err)
			}
		}
	}
}

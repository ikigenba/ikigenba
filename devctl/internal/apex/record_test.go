package apex_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/apex"
	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seam"
	"github.com/ikigenba/ikigenba/devctl/internal/space"
)

var _ func(context.Context, seam.Deps, cloud.Route53, cloud.Zone, string, string) error = apex.PutRecord
var _ func(string, string) string = apex.RecordDetail

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
	// R-G26G-UM7C R-G3ED-8DY1 R-G4M9-M5OQ
	put := apex.PutRecord
	detail := apex.RecordDetail
	if detail("ikigenba.dev", "3.141.27.86") != "ikigenba.dev -> 3.141.27.86, INSYNC" {
		t.Fatal("detail")
	}
	var out bytes.Buffer
	space.Step(&out, "apex", detail("ikigenba.dev", "3.141.27.86"))
	if out.String() != "apex: ok (ikigenba.dev -> 3.141.27.86, INSYNC)\n" {
		t.Fatal(out.String())
	}
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
		err := put(t.Context(), deps, f, cloud.Zone{ID: "Z09565073GHK8BYWQ1A78"}, "ikigenba.dev", "3.141.27.86")
		want := []cloud.RecordChange{{Action: cloud.ChangeUpsert, Record: cloud.Record{Name: "ikigenba.dev", Type: "A", TTL: space.RecordTTL, Values: []string{"3.141.27.86"}}}}
		if !reflect.DeepEqual(f.changes, want) {
			t.Fatal(f.changes)
		}
		switch stage {
		case "":
			if err != nil || f.polls != 2 || f.waits != 1 {
				t.Fatalf("%v %d %d", err, f.polls, f.waits)
			}
		case "change":
			if reflect.ValueOf(err) != reflect.ValueOf(cause) || f.polls != 0 || f.waits != 0 {
				t.Fatalf("%v %d", err, f.polls)
			}
		case "status":
			if reflect.ValueOf(err) != reflect.ValueOf(f.statusErr) || f.polls != 1 {
				t.Fatal(err)
			}
		}
	}
}

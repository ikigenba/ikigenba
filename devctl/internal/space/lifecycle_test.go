package space

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestLifecycleAddressOnlyRefusals(t *testing.T) {
	// R-RITC-6ZXK
	for _, command := range []string{"stop", "start", "status"} {
		f := newOperationFake(t)
		f.instances = []cloud.Instance{{ID: "old", Space: testDomain, State: cloud.StateTerminated}}
		f.addresses = []cloud.Address{{AllocationID: "alloc", IP: "18.118.7.42", Space: testDomain}}
		out, err := runOperation(t, f, command, "sbx1")
		var missing *cloud.NoInstanceError
		if out != "" || !errors.As(err, &missing) || missing.Domain != testDomain || len(f.execCalls) != 1 || containsOp(f.ops, "start ") || containsOp(f.ops, "stop ") {
			t.Fatalf("%s out=%q err=%v ops=%v", command, out, err, f.ops)
		}
	}
}

func TestLifecycleDuplicateAddressRefusedBeforeEffects(t *testing.T) {
	// R-RK18-KRO9 R-XMWC-TW4J
	for _, command := range []string{"destroy", "list"} {
		f := newOperationFake(t)
		f.addresses = []cloud.Address{{AllocationID: "a", IP: "18.190.33.61", Space: testDomain}, {AllocationID: "b", IP: "18.118.7.42", Space: testDomain}}
		out, err := runOperation(t, f, append([]string{command}, map[string][]string{"destroy": {"sbx1"}}[command]...)...)
		var duplicate *cloud.DuplicateAddressError
		if out != "" || !errors.As(err, &duplicate) || len(f.execCalls) != 1 || len(f.changeCalls) != 0 || containsOp(f.ops, "terminate ") || containsOp(f.ops, "release ") {
			t.Fatalf("%s out=%q err=%v ops=%v", command, out, err, f.ops)
		}
	}
}

type pagedBackups struct {
	cloud.S3
	pages       [][]cloud.Object
	handed      int
	deletes     [][]string
	deletePages []int
	failDelete  int
	deleteErr   error
	listErr     error
}

func (f *pagedBackups) ListObjects(_ context.Context, bucket, prefix string, each func([]cloud.Object) bool) error {
	if bucket != testRoot || prefix != "sbx1/" {
		return fmt.Errorf("bad listing %s/%s", bucket, prefix)
	}
	for _, page := range f.pages {
		f.handed++
		if !each(page) {
			return nil
		}
	}
	return f.listErr
}
func (f *pagedBackups) DeleteObjects(_ context.Context, bucket string, keys []string) error {
	if bucket != testRoot || len(keys) > cloud.ObjectPageSize {
		return errors.New("bad deletion")
	}
	f.deletes = append(f.deletes, append([]string(nil), keys...))
	f.deletePages = append(f.deletePages, f.handed)
	if len(f.deletes) == f.failDelete {
		return f.deleteErr
	}
	return nil
}
func TestDestroyBackupsDeletesEachPageBeforeFetchingNext(t *testing.T) {
	// R-U77A-4SM8 R-U8F6-IKCX
	pages := make([][]cloud.Object, 3)
	for i, n := range []int{1000, 1000, 500} {
		pages[i] = make([]cloud.Object, n)
		for j := range pages[i] {
			pages[i][j] = cloud.Object{Key: fmt.Sprintf("sbx1/%d/%d", i, j)}
		}
	}
	sentinel := errors.New("listing/delete failed")
	for _, fail := range []string{"", "second-delete", "list"} {
		f := &pagedBackups{pages: pages, deleteErr: sentinel}
		if fail == "second-delete" {
			f.failDelete = 2
		}
		if fail == "list" {
			f.pages = pages[:1]
			f.listErr = sentinel
		}
		var out bytes.Buffer
		err := destroyBackups(t.Context(), &out, f, testRoot, "sbx1", true)
		wantCalls := 3
		if fail == "second-delete" {
			wantCalls = 2
		}
		if fail == "list" {
			wantCalls = 1
		}
		if len(f.deletes) != wantCalls || f.handed != wantCalls {
			t.Fatalf("%s calls=%d pages=%d", fail, len(f.deletes), f.handed)
		}
		for i, keys := range f.deletes {
			expected := make([]string, len(pages[i]))
			for j, obj := range pages[i] {
				expected[j] = obj.Key
			}
			if !reflect.DeepEqual(keys, expected) || f.deletePages[i] != i+1 {
				t.Fatal("wrong page", i)
			}
		}
		if fail == "" {
			if err != nil || out.String() != "backups: ok (2500 objects deleted)\n" {
				t.Fatalf("%q %v", out.String(), err)
			}
		} else if !errors.Is(err, sentinel) || out.Len() != 0 {
			t.Fatalf("%s out=%q err=%v", fail, out.String(), err)
		}
	}
	for _, remove := range []bool{false, true} {
		f := &pagedBackups{}
		var out bytes.Buffer
		if err := destroyBackups(t.Context(), &out, f, testRoot, "sbx1", remove); err != nil {
			t.Fatal(err)
		}
		detail := "kept"
		if remove {
			detail = "already gone"
		}
		if !strings.Contains(out.String(), detail) || len(f.deletes) != 0 {
			t.Fatal(out.String())
		}
	}
}

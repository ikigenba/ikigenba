package seed_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
	"github.com/ikigenba/ikigenba/devctl/internal/seed"
)

func TestNewestSnapshotsFiltersAndPreservesObjects(t *testing.T) {
	// R-2Y0G-MZ32 R-VQ6N-RCHK
	type snapshotFolder func(map[string]cloud.Object, []cloud.Object, string) map[string]cloud.Object
	fold := snapshotFolder(seed.NewestSnapshots)
	prefix := "sbx1/snapshots/"
	valid := cloud.Object{Key: prefix + "crm/2026-09-12T14:22:51Z.tar.zst", Size: 7, Modified: time.Unix(23, 0)}
	ignored := []cloud.Object{{Key: prefix + "crm/x/y"}, {Key: prefix + "crm/"}, {Key: prefix + "Bad_1/2026-09-20T00:00:00Z.tar.zst"}, {Key: prefix + "notes.txt"}, {Key: "sbx2/snapshots/dashboard/2026-09-20T00:00:00Z.tar.zst"}}
	want := map[string]cloud.Object{"crm": valid}
	for _, page := range [][]cloud.Object{{valid}, append([]cloud.Object{valid}, ignored...)} {
		if got := fold(nil, page, prefix); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v", got)
		}
	}
	for _, page := range [][]cloud.Object{nil, ignored} {
		if got := fold(nil, page, prefix); len(got) != 0 {
			t.Fatal(got)
		}
		if got := fold(map[string]cloud.Object{"crm": valid}, page, prefix); !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
	}
}

func TestNewestSnapshotsOrdersFilesAndKeepsTies(t *testing.T) {
	// R-30G9-EIKG
	prefix := "sbx1/snapshots/"
	older := cloud.Object{Key: prefix + "crm/2026-09-13T00:00:00Z.tar.zst", Modified: time.Unix(100, 0)}
	newer := cloud.Object{Key: prefix + "crm/2026-09-14T09:10:02Z.tar.zst", Modified: time.Unix(1, 0)}
	want := map[string]cloud.Object{"crm": newer}
	for _, pair := range [][2]cloud.Object{{older, newer}, {newer, older}} {
		if got := seed.NewestSnapshots(nil, []cloud.Object{pair[0], pair[1]}, prefix); !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
		if got := seed.NewestSnapshots(map[string]cloud.Object{"crm": pair[0]}, []cloud.Object{pair[1]}, prefix); !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
	}
	dashboard := cloud.Object{Key: prefix + "dashboard/2026-09-12T14:22:51Z.tar.zst", Size: 7}
	got := seed.NewestSnapshots(nil, []cloud.Object{{Key: prefix + "crm/2026-09-12T14:22:51Z.tar.zst"}, dashboard, newer}, prefix)
	if !reflect.DeepEqual(got, map[string]cloud.Object{"crm": newer, "dashboard": dashboard}) {
		t.Fatal(got)
	}
	duplicate := dashboard
	duplicate.Size = 9
	if got := seed.NewestSnapshots(map[string]cloud.Object{"dashboard": dashboard}, []cloud.Object{duplicate}, prefix); got["dashboard"] != dashboard {
		t.Fatal(got)
	}
}

func TestNewestSnapshotsPageSplitsAndIndependentCalls(t *testing.T) {
	// R-VREK-5489
	prefix := "sbx1/snapshots/"
	objects := []cloud.Object{{Key: prefix + "crm/2026-09-12T14:22:51Z.tar.zst"}, {Key: prefix + "crm/x/y"}, {Key: prefix + "crm/"}, {Key: prefix + "Bad_1/2026-09-20T00:00:00Z.tar.zst"}, {Key: prefix + "notes.txt"}, {Key: "sbx2/snapshots/dashboard/2026-09-20T00:00:00Z.tar.zst"}, {Key: prefix + "crm/2026-09-14T09:10:02Z.tar.zst"}, {Key: prefix + "crm/2026-09-13T00:00:00Z.tar.zst"}, {Key: prefix + "dashboard/2026-09-12T14:22:51Z.tar.zst"}}
	want := map[string]cloud.Object{"crm": objects[6], "dashboard": objects[8]}
	reverse := append([]cloud.Object(nil), objects...)
	for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}
	for _, tc := range []struct {
		objects []cloud.Object
		sizes   []int
	}{{objects, []int{9}}, {objects, []int{1, 1, 1, 1, 1, 1, 1, 1, 1}}, {reverse, []int{1, 1, 1, 1, 1, 1, 1, 1, 1}}, {objects, []int{4, 2, 3}}} {
		var got map[string]cloud.Object
		offset := 0
		for _, size := range tc.sizes {
			got = seed.NewestSnapshots(got, tc.objects[offset:offset+size], prefix)
			offset += size
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
	}
	if got := seed.NewestSnapshots(nil, objects[:1], prefix); !reflect.DeepEqual(got, map[string]cloud.Object{"crm": objects[0]}) {
		t.Fatal(got)
	}
}

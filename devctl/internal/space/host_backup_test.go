package space

import (
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/devctl/internal/cloud"
)

func TestHostBackupSignature(_ *testing.T) {
	// R-U2BO-LPNG
	accept := func(func(cloud.Object, bool, []cloud.Object, string) (cloud.Object, bool)) {}
	accept(HostBackup)
}

func TestHostBackupAcceptsUTCStamps(t *testing.T) {
	// R-U3JK-ZHE5
	stamps := []string{"2026-11-01T00:00:00Z", "2024-02-29T23:59:59Z"}
	for digits := 1; digits <= 9; digits++ {
		stamps = append(stamps, "2026-11-08T16:12:01."+strings.Repeat("1", digits)+"Z")
	}
	for _, stamp := range stamps {
		t.Run(stamp, func(t *testing.T) {
			want := cloud.Object{Key: "us/host/" + stamp + ".tar.zst", Size: 123, Modified: time.Unix(1, 0)}
			if got, ok := HostBackup(cloud.Object{}, false, []cloud.Object{want}, "us/host/"); !ok || got != want {
				t.Fatalf("HostBackup = %#v, %v; want %#v, true", got, ok, want)
			}
		})
	}
}

func TestHostBackupIgnoresOtherObjects(t *testing.T) {
	// R-U3JK-ZHE5
	want := cloud.Object{Key: "us/host/2026-11-08T16:12:01.865067548Z.tar.zst", Size: 456, Modified: time.Unix(1, 0)}
	var ignored []cloud.Object
	for _, key := range []string{
		"us/host/2026-11-08T16:12:01.865067548Z.accounts.tar.zst",
		"us/host/2026-11-08T17:43:26.222933531Z.accounts.tar.zst",
		"us/host/old/2026-11-09T00:00:00Z.tar.zst",
		"us/host/latest.tar.zst",
		"us/host/2026-11-09T00:00:00+00:00.tar.zst",
		"us/host/2026-02-30T00:00:00Z.tar.zst",
		"us/host/2026-11-09T00:00:00Z.tar.gz",
		"them/host/2026-11-09T00:00:00Z.tar.zst",
	} {
		ignored = append(ignored, cloud.Object{Key: key, Modified: want.Modified.Add(time.Hour)})
	}
	objects := append([]cloud.Object{want}, ignored...)
	if got, ok := HostBackup(cloud.Object{}, false, objects, "us/host/"); !ok || got != want {
		t.Fatalf("HostBackup = %#v, %v; want %#v, true", got, ok, want)
	}
	for _, objects := range [][]cloud.Object{nil, ignored} {
		if got, ok := HostBackup(cloud.Object{}, false, objects, "us/host/"); ok || got != (cloud.Object{}) {
			t.Fatalf("HostBackup = %#v, %v; want zero, false", got, ok)
		}
	}
}

func TestHostBackupRejectsMalformedStamps(t *testing.T) {
	// R-U3JK-ZHE5
	for _, stamp := range []string{
		"2026-11-08T16:12:01.Z", "2026-11-08T16:12:01.1234567890Z",
		"2026-11-08T16:12:01,5Z", "2026-11-08T16:12:01.5+00:00",
		"2026-11-08T16:12:01-01:00", "2026-11-08T16:12:01z",
		"2026-11-08t16:12:01Z", "2026-11-08T6:12:01Z",
		"2026-1-08T16:12:01Z", "2026-11-8T16:12:01Z",
		"2026-11-08T16:2:01Z", "2026-11-08T16:12:1Z",
		"2026-13-08T16:12:01Z", "2026-11-00T16:12:01Z",
		"2026-11-08T24:00:00Z", "2026-11-08T16:60:00Z",
		"2026-11-08T16:12:60Z", "2025-02-29T00:00:00Z",
		"2026-11-08T16:12:01Z\n", "2026-11-08T16:12:01Z.extra",
	} {
		t.Run(stamp, func(t *testing.T) {
			object := cloud.Object{Key: "us/host/" + stamp + ".tar.zst"}
			if got, ok := HostBackup(cloud.Object{}, false, []cloud.Object{object}, "us/host/"); ok || got != (cloud.Object{}) {
				t.Fatalf("HostBackup = %#v, %v; want zero, false", got, ok)
			}
		})
	}
}

func TestHostBackupOrdersByParsedTimeThenKey(t *testing.T) {
	// R-U4RH-D94U
	for _, tc := range []struct{ winner, loser string }{
		{"2026-11-08T16:12:01.5Z", "2026-11-08T16:12:01Z"},
		{"2026-11-07T17:09:48.74850415Z", "2026-11-07T17:09:48.7485041Z"},
		{"2026-11-08T17:43:26.222933531Z", "2026-11-08T16:12:01.865067548Z"},
		{"2026-11-08T16:12:01.5Z", "2026-11-08T16:12:01.50Z"},
	} {
		t.Run(tc.winner+" over "+tc.loser, func(t *testing.T) {
			want := cloud.Object{Key: "us/host/" + tc.winner + ".tar.zst", Size: 789, Modified: time.Unix(1, 0)}
			other := cloud.Object{Key: "us/host/" + tc.loser + ".tar.zst", Modified: want.Modified.Add(time.Hour)}
			for _, objects := range [][]cloud.Object{{want, other}, {other, want}} {
				if got, ok := HostBackup(cloud.Object{}, false, objects, "us/host/"); !ok || got != want {
					t.Fatalf("HostBackup(cloud.Object{}, false, %#v) = %#v, %v; want %#v, true", objects, got, ok, want)
				}
			}
		})
	}
}

func TestHostBackupFoldsPagesWithoutState(t *testing.T) {
	// R-U4RH-D94U R-U5ZD-R0VJ R-U3JK-ZHE5
	const prefix = "us/host/"
	older := cloud.Object{Key: prefix + "2026-11-01T00:00:00Z.tar.zst", Size: 1}
	winner := cloud.Object{Key: prefix + "2026-11-08T17:43:26.222933531Z.tar.zst", Size: 2}
	objects := []cloud.Object{{Key: prefix + "2026-11-08T16:12:01.865067548Z.tar.zst"},
		{Key: prefix + "2026-11-08T16:12:01.865067548Z.accounts.tar.zst"},
		{Key: prefix + "2026-11-08T17:43:26.222933531Z.accounts.tar.zst"},
		{Key: prefix + "old/2026-11-09T00:00:00Z.tar.zst"},
		{Key: prefix + "latest.tar.zst"}, {Key: prefix + "2026-11-09T00:00:00+00:00.tar.zst"},
		{Key: prefix + "2026-02-30T00:00:00Z.tar.zst"}, {Key: prefix + "2026-11-09T00:00:00Z.tar.gz"},
		{Key: "them/host/2026-11-09T00:00:00Z.tar.zst"}, winner}
	for _, reversed := range []bool{false, true} {
		page := append([]cloud.Object(nil), objects...)
		if reversed {
			for i, j := 0, len(page)-1; i < j; i, j = i+1, j-1 {
				page[i], page[j] = page[j], page[i]
			}
		}
		for _, sizes := range [][]int{{10}, {1, 1, 1, 1, 1, 1, 1, 1, 1, 1}, {3, 3, 4}} {
			var newest cloud.Object
			found := false
			offset := 0
			for _, size := range sizes {
				newest, found = HostBackup(newest, found, page[offset:offset+size], prefix)
				offset += size
			}
			if newest != winner || !found {
				t.Fatalf("fold %v: %v %v", sizes, newest, found)
			}
			newest, found = HostBackup(cloud.Object{}, false, []cloud.Object{older}, prefix)
			if newest != older || !found {
				t.Fatalf("prior fold leaked: %v %v", newest, found)
			}
		}
	}
	got, found := HostBackup(older, true, objects[1:9], prefix)
	if got != older || !found {
		t.Fatalf("ignored page replaced prior: %v %v", got, found)
	}
	for _, pair := range [][2]string{{"2026-11-08T16:12:01.5Z", "2026-11-08T16:12:01Z"}, {"2026-11-07T17:09:48.74850415Z", "2026-11-07T17:09:48.7485041Z"}, {"2026-11-08T17:43:26.222933531Z", "2026-11-08T16:12:01.865067548Z"}, {"2026-11-08T16:12:01.5Z", "2026-11-08T16:12:01.50Z"}} {
		want := cloud.Object{Key: prefix + pair[0] + ".tar.zst"}
		loser := cloud.Object{Key: prefix + pair[1] + ".tar.zst", Modified: time.Unix(9, 0)}
		for _, both := range [][2]cloud.Object{{want, loser}, {loser, want}} {
			got, found := HostBackup(both[0], true, []cloud.Object{both[1]}, prefix)
			if got != want || !found {
				t.Fatalf("fold pair: %v %v", got, found)
			}
		}
	}
}

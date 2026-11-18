package pages

import (
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
)

// R-S5OQ-Q4G0
func TestEmptyHostAndReportLists(t *testing.T) {
	at := time.Date(2026, 11, 18, 14, 5, 9, 0, time.UTC)
	host := hostView(store.Host{Name: "host01", State: store.HostRunning}, at)
	if host.Allowlist == nil || len(host.Allowlist) != 0 {
		t.Fatalf("host allowlist = %#v, want non-nil empty slice", host.Allowlist)
	}
	if got := reportView(nil, 7); got != nil {
		t.Fatalf("missing report = %#v, want nil", got)
	}
	for _, total := range []int{0, 3} {
		got := reportView(&store.Report{Received: at, Body: protocol.Report{BanTotal: total}}, 7)
		if got == nil || got.BanTotal != total ||
			got.FailedUnits == nil || len(got.FailedUnits) != 0 ||
			got.Slices == nil || len(got.Slices) != 0 ||
			got.Bans == nil || len(got.Bans) != 0 ||
			got.Allowlist == nil || len(got.Allowlist) != 0 {
			t.Fatalf("report = %#v, want non-nil empty lists and ban total %d", got, total)
		}
	}
}

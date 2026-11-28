package pages_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/appkit/page"
)

// R-WOLJ-HAFL
func TestHostShowsTemplateCommitsFromOneSnapshot(t *testing.T) {
	f := setup(t)
	st, ctx := f.cfg.Store, t.Context()
	host := backupPageHost(t, f, "")
	target := "/hosts/" + host.Name
	read := func() pages.HostData {
		t.Helper()
		return pages.CapturePageData[pages.HostData](t, f.cfg, request(http.MethodGet, target, "viewer"), "host")
	}
	check := func(seeds []pages.SeedRow, homes []pages.HomeRow) {
		t.Helper()
		got := read()
		if !reflect.DeepEqual(got.Seeds, seeds) || !reflect.DeepEqual(got.Homes, homes) {
			t.Fatalf("host template received seeds %#v and homes %#v, want %#v and %#v", got.Seeds, got.Homes, seeds, homes)
		}
	}
	check([]pages.SeedRow{}, []pages.HomeRow{})

	place := func(slug string) store.Account {
		t.Helper()
		account, op, err := st.PlaceAccount(ctx, slug, host.Name, caller)
		checkPageStore(t, err)
		checkPageStore(t, st.Finish(ctx, op.ID, store.Update{}))
		return account
	}
	zulu := place("zulu")
	seeds := []pages.SeedRow{{Account: zulu.Slug}}
	check(seeds, []pages.HomeRow{})
	alpha := place("alpha")
	add := func(account store.Account, name string) store.User {
		t.Helper()
		user, err := st.AddUser(ctx, account.Slug, name, key, caller)
		checkPageStore(t, err)
		return user
	}
	betsy := add(zulu, "betsy")
	zoe := add(alpha, "zoe")
	seeds = []pages.SeedRow{{Account: alpha.Slug}, {Account: zulu.Slug}}
	homes := []pages.HomeRow{{User: betsy.Name, Account: zulu.Slug}, {User: zoe.Name, Account: alpha.Slug}}
	check(seeds, homes)

	seedCommit, homeCommit := strings.Repeat("a1", 20), strings.Repeat("b2", 32)
	report := protocol.Report{
		Protocol: protocol.Version,
		Seeds:    []protocol.Seed{{GID: zulu.GID, Commit: seedCommit}},
		Homes:    []protocol.Home{{UID: betsy.UID, Commit: homeCommit}},
	}
	now := f.cfg.Now()
	reported := false
	f.cfg.Now = func() time.Time {
		if !reported {
			reported = true
			_, err := st.RecordReport(ctx, host.Name, report, now)
			checkPageStore(t, err)
		}
		return now
	}
	during := read()
	if !reported {
		t.Fatal("the page clock did not submit the report during the read")
	}
	before := reflect.DeepEqual(during.Seeds, seeds) && reflect.DeepEqual(during.Homes, homes)
	seeds[1].Commit, homes[0].Commit = seedCommit, homeCommit
	after := reflect.DeepEqual(during.Seeds, seeds) && reflect.DeepEqual(during.Homes, homes)
	if !before && !after {
		t.Fatalf("the report split the page's reads: seeds %#v, homes %#v", during.Seeds, during.Homes)
	}
	check(seeds, homes)
	_, err := st.RecordReport(ctx, host.Name, protocol.Report{Protocol: protocol.Version}, now.Add(time.Second))
	checkPageStore(t, err)
	check(seeds, homes)
}

// R-WJPX-Y7GT
func TestAboutShowsStoredTeamTemplate(t *testing.T) {
	f := setup(t)
	const target = "/about"
	b := banner(f, target, []page.Level{pages.AboutLevel})
	want := aboutData(b, store.DefaultBackupSettings())
	check := func() {
		t.Helper()
		same(t, answer(f, http.MethodGet, target, "viewer"), expected(f, http.MethodGet, target, "about", http.StatusOK, want))
	}
	check()

	ref := "v2"
	_, changed, err := f.cfg.Store.SetTemplateSettings(t.Context(), nil, &ref)
	checkPageStore(t, err)
	if !changed {
		t.Fatal("fixture did not change the template setting")
	}
	want.TemplateRef = ref
	check()

	url := "https://chosen.example.test/team"
	_, changed, err = f.cfg.Store.SetTemplateSettings(t.Context(), &url, nil)
	checkPageStore(t, err)
	if !changed {
		t.Fatal("fixture did not change the template repository")
	}
	want.TemplateURL = url
	check()
}

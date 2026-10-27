package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-ZHQ8-HZNS R-ZLDX-NAVV R-3GAA-W7C0
func TestMapLegacyListID(t *testing.T) {
	contractEqual(t, store.LegacyListIDPrefix, "coh_")
	for _, s := range []string{"coh_0123456789abcdef", "pl_0123456789abcdef", "coh_0123456789ABCDEF", "coh_0123456789abcde", "coh_0123456789abcdef0", "cand_0123456789abcdef", "coh_", ""} {
		want := s
		if s == "coh_0123456789abcdef" {
			want = "pl_0123456789abcdef"
		}
		contractEqual(t, store.MapListID(s), want)
		contractEqual(t, store.ValidProspectListID(s), s == "pl_0123456789abcdef")
	}
}

// R-2760-QOWB R-2ATP-W04E R-2EHF-1BCH
func TestIngestPassScopesDistinguishToolAndDelivery(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	tool := func(scraper string) store.Pass {
		return store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Scraper: scraper, UserID: "owner"}
	}
	for _, scraper := range []string{"fmcsa", "ppp", ""} {
		p, e := f.s.AddPass(ctx, tool(scraper))
		contractOK(t, e)
		contractEqual(t, p.Scraper, scraper)
	}
	_, e := f.s.AddPass(ctx, tool("fmcsa"))
	contractError(t, e, store.ErrBusy)
	for _, trigger := range []string{"fmcsa", store.TriggerIngest} {
		p, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: trigger, Delivery: "delivery-" + trigger})
		contractOK(t, e)
		contractEqual(t, p.Trigger, trigger)
	}
	_, e = f.s.AddPass(ctx, store.Pass{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudgeTool, Delivery: "bad-cron"})
	contractError(t, e, store.ErrUnreachable)
	p, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"})
	contractOK(t, e)
	contractEqual(t, p.Origin, store.OriginJudge)
}

func insertLegacyListRows(ctx context.Context, tx *sql.Tx, suffix string) error {
	stamp := "2026-10-01T00:00:00Z"
	id := "cand_" + suffix
	if e := writePriorCandidate(ctx, tx, id); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO assessments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,known_since,government_match,newest_filing,categories,fleet_verdict,power_units,drivers,website,website_url,minutes,excluded,rules,scored) VALUES (?,.6,600000,'[]',100,200,.7,'','','','[]','unknown',NULL,NULL,'unknown','',NULL,0,?,?)`, id, sources.Rules, stamp); e != nil {
		return e
	}
	base, _ := strconv.ParseUint(suffix, 16, 64)
	for j := 1; j <= 2; j++ {
		next := fmt.Sprintf("cand_%016x", base+uint64(j))
		if e := writePriorCandidate(ctx, tx, next); e != nil {
			return e
		}
		op := []float64{.8, .4}[j-1]
		if _, e := tx.ExecContext(ctx, `INSERT INTO assessments(candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,excluded,rules,scored) VALUES (?,?,?,'[]',?,?,.7,0,?,?)`, next, op, int64(op*1000000), 100*(j+1), 200*(j+1), sources.Rules, stamp); e != nil {
			return e
		}
	}
	filters := []string{`{}`, `{}`, `{"revenue_min":150}`, `{"max_minutes":15}`, `{"website":["https"]}`, `{"categories":["trucking and logistics"]}`}
	fields := []string{store.OrderRevenueLow, store.OrderOperatingConfidence, store.OrderRevenueLow, store.OrderDriveMinutes, store.OrderOperatingConfidence, store.OrderOperatingConfidence}
	directions := []string{store.DirectionAsc, store.DirectionDesc, store.DirectionDesc, store.DirectionAsc, store.DirectionDesc, store.DirectionDesc}
	for i := 1; i <= 6; i++ {
		key := fmt.Sprintf("coh_%016x", i)
		ordering := fmt.Sprintf(`[{"field":%q,"direction":%q}]`, fields[i-1], directions[i-1])
		if _, e := tx.ExecContext(ctx, `INSERT INTO cohorts(id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed) VALUES (?,?,?,?,?,'owner','mail',?,'editor','edited',?)`, key, fmt.Sprintf("list-%d", i), filters[i-1], ordering, i, stamp, stamp); e != nil {
			return e
		}
	}
	for j := 0; j < 3; j++ {
		next := fmt.Sprintf("cand_%016x", base+uint64(j))
		categories := `[]`
		if j < 2 {
			categories = `[{"Name":"trucking and logistics","Source":"fmcsa"}]`
		}
		state := "unknown"
		if j == 1 {
			state = "https"
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO facts(candidate,categories,fleet_verdict,website,minutes) VALUES (?,?,'unknown',?,?)`, next, categories, state, (j+1)*10); e != nil {
			return e
		}
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO links(scraper,source_id,candidate,run,digest,name_key,state,zip,phone_key,domain_key,reading,reason,confidence,first_seen,last_seen,gone) VALUES ('fmcsa','old-row',?,'old-run','old-digest','','','','','','{}','source_key',1,?,?,0)`, id, stamp, stamp); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,'website_check','old-input','{}','old-source',?,.25)`, id, stamp); e != nil {
		return e
	}
	for _, kind := range []string{store.KindReject, store.KindUnreject} {
		if _, e := tx.ExecContext(ctx, `INSERT INTO rejects(cohort,candidate,kind,user_id,user_email,at,why) VALUES ('coh_0000000000000001',?,?,'owner','mail',?,'reason')`, id, kind, stamp); e != nil {
			return e
		}
	}
	for _, key := range []string{"coh_0000000000000002", "coh_0000000000000099"} {
		if _, e := tx.ExecContext(ctx, `INSERT INTO rejects(cohort,candidate,kind,user_id,user_email,at,why) VALUES (?,?,'reject','owner','mail',?,'reason')`, key, id, stamp); e != nil {
			return e
		}
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO campaign_uses(candidate,campaign,cohort,added) VALUES (?,'campaign','coh_0000000000000001',?)`, id, stamp); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO campaign_uses(candidate,campaign,cohort,added) VALUES (?,'empty','',?)`, id, stamp); e != nil {
		return e
	}

	for i, kind := range []string{"rebuild", "enrich_cohort"} {
		origin, trigger, target, enrich := "rebuild", "rebuild", "", ""
		if i == 1 {
			origin, trigger, target, enrich = "enrich", "enrich", "coh_0000000000000001", enrichers.KindBrief
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery,kind,origin,target,enrich_kind) VALUES (?,?,'done','',?, ?,1,2,3,4,5,6,7,8,9,'owner','mail','request','',?,?,?,?)`, fmt.Sprintf("pass_%016x", i+1), trigger, stamp, stamp, kind, origin, target, enrich); e != nil {
			return e
		}
	}
	return nil
}

// R-KV14-69AF
func TestPreviousReleaseListsAndJudgmentsMigrate(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: earlierMigrations(t, "0016_campaign_uses.sql")})
	contractOK(t, e)
	contractOK(t, d.Write(ctx, func(tx *sql.Tx) error { return insertLegacyListRows(ctx, tx, "0000000000000001") }))
	contractOK(t, d.Close())
	d, e = db.Open(ctx, db.Config{Path: path, Migrations: candidates.Migrations()})
	contractOK(t, e)
	defer func() { contractOK(t, d.Close()) }()
	s := store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }})
	lists, e := s.ListProspectLists(ctx, "", "", 100)
	contractOK(t, e)
	contractEqual(t, len(lists), 6)
	id := "cand_0000000000000001"
	for i, k := range lists {
		stamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		contractEqual(t, k.ID, fmt.Sprintf("pl_%016x", i+1))
		contractEqual(t, k.Name, fmt.Sprintf("list-%d", i+1))
		contractEqual(t, k.Size, int64(i+1))
		contractEqual(t, k.CreatedUserID, "owner")
		contractEqual(t, k.ChangedUserID, "editor")
		contractEqual(t, k.Created, stamp)
		contractEqual(t, k.Changed, stamp)
		contractEqual(t, k.CreatedUserEmail, "mail")
		contractEqual(t, k.ChangedUserEmail, "edited")
		expectedFilters := []store.ProspectListFilters{{}, {}, {RevenueMin: ptrInt(150)}, {MaxMinutes: ptrInt(15)}, {Website: []string{sources.WebsiteHTTPS}}, {Categories: []string{sources.CategoryTrucking}}}
		contractEqual(t, k.Filters, expectedFilters[i])
		fields := []string{store.OrderRevenueLow, store.OrderOperatingConfidence, store.OrderRevenueLow, store.OrderDriveMinutes, store.OrderOperatingConfidence, store.OrderOperatingConfidence}
		dirs := []string{store.DirectionAsc, store.DirectionDesc, store.DirectionDesc, store.DirectionAsc, store.DirectionDesc, store.DirectionDesc}
		contractEqual(t, k.Order, []store.ProspectListOrder{{Field: fields[i], Direction: dirs[i]}})
		got, e := s.GetProspectList(ctx, k.ID)
		contractOK(t, e)
		contractEqual(t, got, k)
		_, e = s.GetProspectList(ctx, fmt.Sprintf("coh_%016x", i+1))
		contractError(t, e, store.ErrNotFound)
		_, e = s.ReadProspectList(ctx, fmt.Sprintf("coh_%016x", i+1))
		contractError(t, e, store.ErrNotFound)
		r, e := s.ReadProspectList(ctx, k.ID)
		contractOK(t, e)
		expectedIDs := [][]string{{id}, {"cand_0000000000000002", "cand_0000000000000003"}, {"cand_0000000000000003", "cand_0000000000000002"}, {id}, {"cand_0000000000000002"}, {"cand_0000000000000002", id}}
		contractEqual(t, prospectListIDs(r.Members), expectedIDs[i])
		matching := []int64{3, 2, 2, 1, 1, 2}
		contractEqual(t, r.Matching, matching[i])
		if i == 1 {
			contractEqual(t, len(r.Rejects), 1)
			contractEqual(t, r.Rejects[0].ProspectList, k.ID)
			contractEqual(t, r.Rejects[0].ProspectListName, k.Name)
		} else {
			contractEqual(t, len(r.Rejects), 0)
		}
		contractEqual(t, r.Classified, int64(0))
	}
	links, e := s.Links(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(links), 1)
	contractEqual(t, links[0].Scraper, "fmcsa")
	contractEqual(t, links[0].SourceID, "old-row")
	contractEqual(t, links[0].Run, "old-run")
	contractEqual(t, links[0].Digest, "old-digest")
	results, e := s.Enrichments(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(results), 1)
	contractEqual(t, results[0].Inputs, "old-input")
	contractEqual(t, results[0].Source, "old-source")
	contractEqual(t, results[0].Cost, .25)
	facts, e := s.GetFacts(ctx, id)
	contractOK(t, e)
	contractEqual(t, *facts.Minutes, int64(10))
	contractEqual(t, facts.Categories, []sources.Category{{Name: sources.CategoryTrucking, Source: "fmcsa"}})
	rejects, e := s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(rejects), 4)
	contractEqual(t, rejects[0].ProspectList, "pl_0000000000000099")
	contractEqual(t, rejects[0].ProspectListName, "")
	uses, e := s.CampaignUses(ctx, id)
	contractOK(t, e)
	contractEqual(t, len(uses), 2)
	for _, u := range uses {
		if u.Campaign == "campaign" {
			contractEqual(t, u.ProspectList, "pl_0000000000000001")
		} else {
			contractEqual(t, u.ProspectList, "")
		}
	}
	a, e := s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, a.Candidate, id)
	contractEqual(t, *a.Operating, .6)
	contractEqual(t, a.OperatingKey, int64(600000))
	contractEqual(t, *a.RevenueLow, int64(100))
	contractEqual(t, *a.RevenueHigh, int64(200))
	contractEqual(t, *a.RevenueConfidence, .7)
	contractEqual(t, a.Rules, int64(sources.Rules))
	contractEqual(t, a.Calculated, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	stamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	op, conf := .6, .7
	contractEqual(t, a, store.Judgments{HeadcountSeries: []judging.Series{}, Trend: judging.TrendUnknown, Candidate: id, Operating: &op, OperatingKey: 600000, Evidence: []sources.Evidence{}, RevenueLow: ptrInt(100), RevenueHigh: ptrInt(200), RevenueConfidence: &conf, Rules: sources.Rules, Calculated: stamp})
	contractEqual(t, rejects, []store.Reject{
		{Kind: store.KindReject, ProspectList: "pl_0000000000000099", Candidate: id, Why: "reason", UserID: "owner", UserEmail: "mail", At: stamp},
		{Kind: store.KindReject, ProspectList: "pl_0000000000000002", ProspectListName: "list-2", Candidate: id, Why: "reason", UserID: "owner", UserEmail: "mail", At: stamp},
		{Kind: store.KindUnreject, ProspectList: "pl_0000000000000001", ProspectListName: "list-1", Candidate: id, Why: "reason", UserID: "owner", UserEmail: "mail", At: stamp},
		{Kind: store.KindReject, ProspectList: "pl_0000000000000001", ProspectListName: "list-1", Candidate: id, Why: "reason", UserID: "owner", UserEmail: "mail", At: stamp}})
	contractEqual(t, uses, []store.CampaignUse{{Candidate: id, Campaign: "campaign", ProspectList: "pl_0000000000000001", Added: stamp}, {Candidate: id, Campaign: "empty", Added: stamp}})

	p, e := s.GetPass(ctx, "pass_0000000000000001")
	contractOK(t, e)
	contractEqual(t, p.Kind, store.KindIngest)
	contractEqual(t, p.Origin, store.OriginIngest)
	contractEqual(t, p.Trigger, store.TriggerIngest)
	contractEqual(t, p.Judged, int64(8))
	contractEqual(t, p, store.Pass{ID: "pass_0000000000000001", Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Status: store.StatusDone, Started: stamp, Finished: stamp, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, UserID: "owner", UserEmail: "mail", RequestID: "request"})
	p, e = s.GetPass(ctx, "pass_0000000000000002")
	contractOK(t, e)
	contractEqual(t, p.Kind, store.KindEnrichTarget)
	contractEqual(t, p.Target, "pl_0000000000000001")
	contractEqual(t, p.Judged, int64(8))
}

// R-ZSPB-XXC1
func TestPreviousReleaseWritesStaySeparateAfterUpgrade(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, e := f.s.Judge(ctx, []string{id})
	contractOK(t, e)
	before, e := f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	k := prospectListCreate(t, f, "current")
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error { return insertLegacyListRows(ctx, tx, "2222222222222222") }))
	lists, e := f.s.ListProspectLists(ctx, "", "", 100)
	contractOK(t, e)
	contractEqual(t, lists, []store.ProspectList{k})
	a, e := f.s.GetJudgments(ctx, id)
	contractOK(t, e)
	contractEqual(t, a, before)
	_, e = f.s.GetJudgments(ctx, "cand_2222222222222222")
	contractError(t, e, store.ErrNotFound)
	rs, e := f.s.Rejects(ctx, "cand_2222222222222222")
	contractOK(t, e)
	contractEqual(t, len(rs), 4)
	for _, r := range rs {
		contractEqual(t, r.ProspectList, "")
		contractEqual(t, r.ProspectListName, "")
		contractEqual(t, r.Why, "reason")
	}
	us, e := f.s.CampaignUses(ctx, "cand_2222222222222222")
	contractOK(t, e)
	contractEqual(t, len(us), 2)
	for _, u := range us {
		contractEqual(t, u.ProspectList, "")
	}
	p, e := f.s.GetPass(ctx, "pass_0000000000000001")
	contractOK(t, e)
	contractEqual(t, p.Judged, int64(0))
	contractEqual(t, p.Scraper, "")
}

// R-B9KK-F52P
func TestCurrentWritesLeaveLegacyFieldsEmpty(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	k := prospectListCreate(t, f, "new")
	_, e := f.s.Reject(ctx, store.Reject{Kind: store.KindReject, ProspectList: k.ID, Candidate: id, Why: "reason", UserID: "owner"})
	contractOK(t, e)
	p, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindJudge, Origin: store.OriginJudge, Trigger: store.TriggerJudgeTool, UserID: "owner"})
	contractOK(t, e)
	_, e = f.s.FinishPass(ctx, p.ID, store.Counts{Judged: 3})
	contractOK(t, e)
	contractOK(t, f.s.AddCampaignUse(ctx, id, "campaign", k.ID, f.now))
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`UPDATE rejects SET why = 'w' WHERE cohort = ''`, `UPDATE passes SET user_email = 'm' WHERE rescored = 0`, `UPDATE campaign_uses SET reason = 'r' WHERE cohort = ''`} {
			if _, e := tx.ExecContext(ctx, q); e != nil {
				return e
			}
		}
		return nil
	}))
	rs, e := f.s.Rejects(ctx, id)
	contractOK(t, e)
	contractEqual(t, rs[0].Why, "w")
	p, e = f.s.GetPass(ctx, p.ID)
	contractOK(t, e)
	contractEqual(t, p.UserEmail, "m")
	contractEqual(t, p.Judged, int64(3))
	us, e := f.s.CampaignUses(ctx, id)
	contractOK(t, e)
	contractEqual(t, us[0].Reason, "r")
	contractEqual(t, us[0].ProspectList, k.ID)
}

// R-ZNTQ-EUD9
func TestProspectListRenameMigrationBytes(t *testing.T) {
	got, e := fs.ReadFile(candidates.Migrations(), "0017_prospect_lists.sql")
	contractOK(t, e)
	contractEqual(t, string(got), prospectListRenameMigration)
}

const prospectListRenameMigration = `CREATE TABLE prospect_lists (id TEXT PRIMARY KEY, name TEXT NOT NULL, filters TEXT NOT NULL, ordering TEXT NOT NULL, size INTEGER NOT NULL, created_user_id TEXT NOT NULL, created_user_email TEXT NOT NULL, created TEXT NOT NULL, changed_user_id TEXT NOT NULL, changed_user_email TEXT NOT NULL, changed TEXT NOT NULL);
CREATE INDEX prospect_lists_window ON prospect_lists(name,id);
INSERT INTO prospect_lists (id,name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed) SELECT 'pl_'||substr(id,5),name,filters,ordering,size,created_user_id,created_user_email,created,changed_user_id,changed_user_email,changed FROM cohorts;
ALTER TABLE rejects ADD COLUMN prospect_list TEXT NOT NULL DEFAULT '';
UPDATE rejects SET prospect_list = 'pl_'||substr(cohort,5) WHERE substr(cohort,1,4) = 'coh_';
CREATE INDEX rejects_prospect_list ON rejects(prospect_list,candidate,seq);
ALTER TABLE campaign_uses ADD COLUMN prospect_list TEXT NOT NULL DEFAULT '';
UPDATE campaign_uses SET prospect_list = 'pl_'||substr(cohort,5) WHERE substr(cohort,1,4) = 'coh_';
CREATE TABLE judgments (candidate TEXT PRIMARY KEY, operating REAL, operating_key INTEGER NOT NULL DEFAULT -1, evidence TEXT, revenue_low INTEGER, revenue_high INTEGER, revenue_confidence REAL, excluded INTEGER NOT NULL, rules INTEGER NOT NULL, calculated TEXT NOT NULL);
INSERT INTO judgments (candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,excluded,rules,calculated) SELECT candidate,operating,operating_key,evidence,revenue_low,revenue_high,revenue_confidence,excluded,rules,scored FROM assessments;
CREATE INDEX judgments_operating ON judgments(operating_key DESC,candidate);
ALTER TABLE passes ADD COLUMN judged INTEGER NOT NULL DEFAULT 0;
ALTER TABLE passes ADD COLUMN scraper TEXT NOT NULL DEFAULT '';
UPDATE passes SET judged = rescored;
UPDATE passes SET kind = 'ingest' WHERE kind = 'rebuild';
UPDATE passes SET origin = 'ingest' WHERE origin = 'rebuild';
UPDATE passes SET "trigger" = 'ingest' WHERE "trigger" = 'rebuild';
UPDATE passes SET kind = 'enrich_target' WHERE kind = 'enrich_cohort';
UPDATE passes SET target = 'pl_'||substr(target,5) WHERE substr(target,1,4) = 'coh_';
`

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	neturl "net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

var factsColumns = columns("candidate:Candidate known_since:KnownSince government_match:GovernmentMatch newest_filing:NewestFiling categories:Categories fleet_verdict:FleetVerdict power_units:PowerUnits drivers:Drivers headcounts:Headcounts website:Website website_url:WebsiteURL website_source:WebsiteSource minutes:Minutes minutes_source:MinutesSource miles:Miles route_address:RouteAddress brief:Brief brief_labels:BriefLabels known_since_source:KnownSinceSource newest_filing_source:NewestFilingSource website_checked_url:WebsiteCheckedURL")

func unknownFacts(id string) Facts {
	return Facts{Candidate: id, Categories: []sources.Category{}, Records: []sources.Category{}, Headcounts: []sources.Headcount{}, BriefLabels: []string{}, OtherKnownSince: []sources.Sourced[time.Time]{}, OtherPowerUnits: []sources.Sourced[int64]{}, FleetVerdict: sources.FleetUnknown, Website: sources.WebsiteUnknown}
}
func factsKey(ctx context.Context, tx *sql.Tx, id string) (Facts, error) {
	f, err := storedFactsKey(ctx, tx, id)
	if err == nil {
		err = factAlternatives(ctx, tx, &f)
	}
	return f, err
}
func storedFactsKey(ctx context.Context, tx *sql.Tx, id string) (Facts, error) {
	fs, err := readRecords[Facts](ctx, tx, "facts", factsColumns, " WHERE candidate=?", id)
	if err != nil {
		return Facts{}, err
	}
	if len(fs) == 0 {
		return Facts{}, ErrNotFound
	}
	f := fs[0]
	var b, d, dr, l, st, w, ws string
	if err = scanSQL(ctx, tx, "SELECT brief_at,drive_minutes_at,drive_route_at,link_facts_at,site_text_at,website_check_at,website_search_at FROM facts WHERE candidate=?", []any{id}, &b, &d, &dr, &l, &st, &w, &ws); err != nil {
		return Facts{}, err
	}
	for i, a := range []string{b, d, dr, l, st, w, ws} {
		if a != "" {
			t, err := time.Parse(time.RFC3339, a)
			if err != nil {
				return Facts{}, err
			}
			f.Written = append(f.Written, Written{Kind: []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindDriveRoute, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindWebsiteSearch}[i], At: t.UTC()})
		}
	}
	f.Records, err = recordCategories(ctx, tx, id)
	return f, err
}
func (s *Store) projectFacts(ctx context.Context, tx *sql.Tx, id string) error {
	f, extra, err := s.projectedFacts(ctx, tx, id)
	if err != nil || f.Candidate == "" {
		return err
	}
	return insertRecord(ctx, tx, "facts", factsColumns, f, extra)
}
func (s *Store) projectedFacts(ctx context.Context, tx *sql.Tx, id string) (Facts, map[string]any, error) {
	places, err := placesURL(ctx, tx, id)
	if err != nil {
		return Facts{}, nil, err
	}
	es, err := s.factsResults(ctx, tx, id, places)
	if err != nil {
		return Facts{}, nil, err
	}
	os, err := ownerRecordsKey(ctx, tx, id)
	if err != nil {
		return Facts{}, nil, err
	}
	if len(es) == 0 && places == "" && len(os) == 0 {
		var exists int
		if err = scanSQL(ctx, tx, "SELECT EXISTS(SELECT 1 FROM facts WHERE candidate=?)", []any{id}, &exists); err != nil {
			return Facts{}, nil, err
		}
		if exists == 0 {
			return Facts{}, nil, nil
		}
	}
	f := unknownFacts(id)
	seen := map[string]bool{}
	goodBrief := false
	briefCategories := []sources.Category{}
	extra := map[string]any{"drive_minutes_at": "", "drive_route_at": "", "link_facts_at": "", "website_check_at": "", "site_text_at": "", "brief_at": "", "website_search_at": ""}
	for _, e := range es {
		if e.Kind == enrichers.KindBrief && !goodBrief && !(enrichers.Brief{}).Failed(e.Found) {
			var b enrichers.BriefFound
			if err = json.Unmarshal(e.Found, &b); err != nil {
				return Facts{}, nil, err
			}
			f.Brief = b.Summary
			f.BriefLabels = append(f.BriefLabels, b.Labels...)
			for _, c := range b.Categories {
				briefCategories = append(briefCategories, sources.Category{Name: c.Name, Source: enrichers.KindBrief})
			}
			goodBrief = true
		}
		if seen[e.Kind] {
			continue
		}
		seen[e.Kind] = true
		switch e.Kind {
		case enrichers.KindDriveMinutes:
			var v enrichers.MinutesFound
			if err = json.Unmarshal(e.Found, &v); err != nil {
				return Facts{}, nil, err
			}
			f.Minutes = v.Minutes
		case enrichers.KindLinkFacts:
			var v enrichers.LinkFound
			if err = json.Unmarshal(e.Found, &v); err != nil {
				return Facts{}, nil, err
			}
			if v.KnownSince != "" {
				f.KnownSince, err = time.Parse("2006-01-02", v.KnownSince)
				if err != nil {
					return Facts{}, nil, err
				}
			}
			if v.NewestFiling != "" {
				f.NewestFiling, err = time.Parse("2006-01-02", v.NewestFiling)
				if err != nil {
					return Facts{}, nil, err
				}
			}
			f.KnownSinceSource, f.NewestFilingSource = v.KnownSinceSource, v.NewestFilingSource
			f.GovernmentMatch = v.GovernmentMatch
			f.FleetVerdict = v.FleetVerdict
			f.PowerUnits = v.PowerUnits
			f.Drivers = v.Drivers
			for _, h := range v.Headcounts {
				asOf := time.Time{}
				if h.Date != "" {
					asOf, err = time.Parse("2006-01-02", h.Date)
					if err != nil {
						return Facts{}, nil, err
					}
				}
				f.Headcounts = append(f.Headcounts, sources.Headcount{Scraper: h.Scraper, Kind: h.Kind, Count: h.Count, AsOf: asOf, Hours: h.Hours})
			}
			for _, c := range v.Categories {
				f.Categories = append(f.Categories, sources.Category{Name: c.Name, Source: c.Source})
			}
		}
		extra[e.Kind+"_at"] = e.At.Format(time.RFC3339)
	}
	if f.Minutes != nil {
		f.MinutesSource = MinutesByZip
	}
	// The address is needed only when a recorded route could decide the drive.
	if seen[enrichers.KindDriveRoute] && s.cfg.Origin != "" {
		inputs, err := s.routeInputs(ctx, tx, id)
		if err != nil {
			return Facts{}, nil, err
		}
		for _, e := range es {
			if inputs == "" || e.Kind != enrichers.KindDriveRoute || e.Inputs != inputs {
				continue
			}
			var g enrichers.RouteFound
			if json.Unmarshal(e.Found, &g) == nil && !g.Failed() && g.Minutes != nil {
				f.Minutes, f.MinutesSource, f.Miles, f.RouteAddress = g.Minutes, MinutesByRoute, g.Miles, g.Address
				break
			}
		}
	}
	f.WebsiteSource, f.Website, f.WebsiteURL, _, _, f.WebsiteCheckedURL, _ = WebsiteDecisionOf(es, places, ownerURL(os))
	if len(briefCategories) > 0 {
		f.Categories = briefCategories
	}
	sort.Slice(f.Categories, func(i, j int) bool {
		a, b := f.Categories[i], f.Categories[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Source < b.Source
	})
	unique := f.Categories[:0]
	for _, c := range f.Categories {
		if len(unique) == 0 || unique[len(unique)-1] != c {
			unique = append(unique, c)
		}
	}
	f.Categories = unique
	if err = ownerFacts(ctx, tx, &f, es, places); err != nil {
		return Facts{}, nil, err
	}
	return f, extra, nil
}

// factsResults retains each kind's newest result and any older deciding result.
func (s *Store) factsResults(ctx context.Context, tx *sql.Tx, id, p string) ([]Enrichment, error) {
	kinds := []string{sources.WebsiteCheck, enrichers.KindDriveMinutes, enrichers.KindLinkFacts, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindWebsiteSearch, enrichers.KindDriveRoute}
	// Each of the fixed seven probes seeks one index entry; no history is grouped or read.
	rows, err := querySQL(ctx, tx, `WITH kinds(kind) AS (VALUES (?),(?),(?),(?),(?),(?),(?))
 SELECT e.kind,e.seq,e.found,e.inputs,e.at FROM kinds JOIN enrichments AS e
 ON e.seq=(SELECT h.seq FROM enrichments AS h WHERE h.candidate=? AND h.kind=kinds.kind ORDER BY h.seq DESC LIMIT 1)`, kinds[0], kinds[1], kinds[2], kinds[3], kinds[4], kinds[5], kinds[6], id)
	if err != nil {
		return nil, err
	}
	type result struct {
		seq               int64
		found, inputs, at string
	}
	newest := make(map[string]result, len(kinds))
	for rows.Next() {
		var kind string
		var r result
		if err = rows.Scan(&kind, &r.seq, &r.found, &r.inputs, &r.at); err != nil {
			_ = rows.Close()
			return nil, err
		}
		newest[kind] = r
	}
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	out := []Enrichment{}
	var routeInputs string
	if _, hasRoute := newest[enrichers.KindDriveRoute]; hasRoute && s.cfg.Origin != "" {
		routeInputs, err = s.routeInputs(ctx, tx, id)
		if err != nil {
			return nil, err
		}
	}
	var takeover []Enrichment
	if check, ok := newest[sources.WebsiteCheck]; ok {
		var w website.Result
		if err := json.Unmarshal([]byte(check.found), &w); err != nil {
			return nil, err
		}
		if w.State == sources.WebsiteHTTPS || w.State == sources.WebsiteHTTP {
			takeover, err = s.takeoverSearch(ctx, tx, id, w.URL)
			if err != nil {
				return nil, err
			}
		}
	}
	checkWorks := false
	for _, kind := range kinds {
		r, ok := newest[kind]
		if !ok {
			continue
		}
		first := true
		for {
			if !first {
				before := r.seq
				err = scanSQL(ctx, tx, "SELECT seq,found,inputs,at FROM enrichments WHERE candidate=? AND kind=? AND seq<? ORDER BY seq DESC LIMIT 1", []any{id, kind, before}, &r.seq, &r.found, &r.inputs, &r.at)
				if errors.Is(err, sql.ErrNoRows) {
					break
				}
				if err != nil {
					return nil, err
				}
			}
			e := Enrichment{Candidate: id, Kind: kind, Inputs: r.inputs, Found: json.RawMessage(r.found)}
			e.At, err = time.Parse(time.RFC3339, r.at)
			if err != nil {
				return nil, err
			}
			deciding := false
			done := true
			switch kind {
			case sources.WebsiteCheck:
				var v website.Result
				if err = json.Unmarshal(e.Found, &v); err != nil {
					return nil, err
				}
				checkWorks = v.State == sources.WebsiteHTTPS || v.State == sources.WebsiteHTTP
			case enrichers.KindBrief:
				deciding = !(enrichers.Brief{}).Failed(e.Found)
				done = deciding
			case enrichers.KindWebsiteSearch:
				var v enrichers.WebsiteSearchFound
				if err = json.Unmarshal(e.Found, &v); err != nil {
					return nil, err
				}
				deciding = !inertSearch(v) && !v.Failed() && (v.Answer.URL == "" || v.Confirmation != nil && v.Confirmation.State == sources.WebsiteNone)
				confirmed := !inertSearch(v) && !v.Failed() && v.Confirmation != nil && (v.Confirmation.State == sources.WebsiteHTTPS || v.Confirmation.State == sources.WebsiteHTTP)
				// Retain only the newest unfound search while looking for a confirmed one.
				if deciding {
					for _, prior := range out {
						if prior.Kind == kind {
							var p enrichers.WebsiteSearchFound
							_ = json.Unmarshal(prior.Found, &p)
							if !inertSearch(p) && !p.Failed() && (p.Answer.URL == "" || p.Confirmation != nil && p.Confirmation.State == sources.WebsiteNone) {
								deciding = false
								break
							}
						}
					}
				}
				deciding = deciding || confirmed
				done = checkWorks && len(takeover) == 0 || confirmed
			case enrichers.KindDriveRoute:
				var v enrichers.RouteFound
				if err = json.Unmarshal(e.Found, &v); err != nil {
					return nil, err
				}
				deciding = routeInputs != "" && r.inputs == routeInputs && !v.Failed() && v.Minutes != nil
				done = routeInputs == "" || deciding
			}
			if first || deciding {
				out = append(out, e)
			}
			first = false
			if done {
				break
			}
		}
	}
	out = append(out, takeover...)
	if p != "" {
		matched, err := matchingCheck(ctx, tx, id, p, "")
		if err != nil {
			return nil, err
		}
		if matched != nil {
			out = append(out, *matched)
		}
	}
	os, err := ownerRecordsKey(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if q := ownerURL(os); q != "" {
		checks, err := enrichmentRecords(ctx, tx, " WHERE candidate=? AND kind=? AND (json_extract(found,'$.from')=? OR EXISTS(SELECT 1 FROM json_each(found,'$.misses') AS m WHERE json_extract(m.value,'$.from')=?)) ORDER BY seq DESC LIMIT 1", id, sources.WebsiteCheck, q, q)
		if err != nil {
			return nil, err
		}
		// Keep the newest global check first for Written, but do not let an older
		// Places match decide the owner URL before its newest attempted check.
		kept := out[:0]
		firstCheck := true
		for _, e := range out {
			var w website.Result
			if e.Kind != sources.WebsiteCheck || json.Unmarshal(e.Found, &w) != nil {
				kept = append(kept, e)
				continue
			}
			preserve := firstCheck
			firstCheck = false
			matches := w.From == q
			for _, m := range w.Misses {
				matches = matches || m.From == q
			}
			if !matches || preserve {
				kept = append(kept, e)
			}
		}
		kept = append(kept, checks...)
		out = kept
	}
	return out, nil
}

// takeoverSearch folds matching searches without retaining their history.
func (s *Store) takeoverSearch(ctx context.Context, tx *sql.Tx, id, websiteURL string) ([]Enrichment, error) {
	rows, err := querySQL(ctx, tx, `SELECT seq,json_extract(found,'$.website'),coalesce(json_extract(found,'$.website_from'),''),coalesce(json_extract(found,'$.confirmation.url'),'') FROM enrichments WHERE candidate=? AND kind=? AND coalesce(json_extract(found,'$.website'),'')<>'' AND json_extract(found,'$.run.status')=? AND json_extract(found,'$.run.exit_code')=0 AND json_type(found,'$.answer')='object' AND json_extract(found,'$.confirmation.state') IN (?,?) ORDER BY seq DESC`, id, enrichers.KindWebsiteSearch, enrichers.RunExited, sources.WebsiteHTTPS, sources.WebsiteHTTP)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	websiteKey := websiteSiteKey(websiteURL)
	for rows.Next() {
		var seq int64
		var searched, from, landing string
		if err = rows.Scan(&seq, &searched, &from, &landing); err != nil {
			return nil, err
		}
		searchedKey := websiteSiteKey(searched)
		if from != WebsiteBySearch && (searchedKey == "" || searchedKey != websiteKey) {
			continue
		}
		if key := websiteSiteKey(landing); key != "" && key == searchedKey {
			continue
		}
		if err = rows.Close(); err != nil {
			return nil, err
		}
		return enrichmentRecords(ctx, tx, " WHERE candidate=? AND seq=?", id, seq)
	}
	return nil, rows.Err()
}

// GetFacts reads the materialized enrichment facts.
func (s *Store) GetFacts(ctx context.Context, candidate string) (Facts, error) {
	var out Facts
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error { var err error; out, err = factsKey(ctx, tx, candidate); return err }))
	return out, err
}

// FillFacts repairs projections and refreshes the headcount and label columns.
func (s *Store) FillFacts(ctx context.Context, after string, n int) (string, error) {
	var last string
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		last = ""
		if n < 1 {
			return ErrUnreachable
		}
		cs, err := readRecords[Candidate](ctx, tx, "candidates INDEXED BY sqlite_autoindex_candidates_1", candidateColumns, " WHERE status=? AND id>? ORDER BY id LIMIT ?", StatusActive, after, n)
		if err != nil {
			return err
		}
		t := s.now()
		for _, c := range cs {
			last = c.ID
			var results int
			if err = scanSQL(ctx, tx, "SELECT EXISTS(SELECT 1 FROM enrichments WHERE candidate=?)", []any{c.ID}, &results); err != nil {
				return err
			}
			if results == 0 {
				continue
			}
			old, err := storedFactsKey(ctx, tx, c.ID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			missing := errors.Is(err, ErrNotFound)
			var routes int64
			if err = scanSQL(ctx, tx, "SELECT EXISTS(SELECT 1 FROM enrichments WHERE candidate=? AND kind=?)", []any{c.ID, enrichers.KindDriveRoute}, &routes); err != nil {
				return err
			}
			if routes > 0 {
				if err = s.refreshKey(ctx, tx, c.ID, t); err != nil {
					return err
				}
			}
			f, extra, err := s.projectedFacts(ctx, tx, c.ID)
			if err != nil {
				return err
			}
			if f.Candidate == "" {
				continue
			}
			repair := missing || (old.Minutes == nil && old.MinutesSource != "") || (old.Minutes != nil && old.MinutesSource == "") || routes > 0
			if !repair {
				if reflect.DeepEqual(old.Headcounts, f.Headcounts) && reflect.DeepEqual(old.BriefLabels, f.BriefLabels) {
					continue
				}
				headcounts, err := encodeValue(reflect.ValueOf(f.Headcounts))
				if err != nil {
					return err
				}
				labels, err := json.Marshal(f.BriefLabels)
				if err != nil {
					return err
				}
				if err = execSQL(ctx, tx, "UPDATE facts SET headcounts=?,brief_labels=? WHERE candidate=?", headcounts, string(labels), c.ID); err != nil {
					return err
				}
				continue
			}
			projected := f
			projected.Records = old.Records
			projected.OtherKnownSince = old.OtherKnownSince
			projected.OtherPowerUnits = old.OtherPowerUnits
			for _, kind := range []string{enrichers.KindBrief, enrichers.KindDriveMinutes, enrichers.KindDriveRoute, enrichers.KindLinkFacts, enrichers.KindSiteText, sources.WebsiteCheck, enrichers.KindWebsiteSearch} {
				if at, ok := extra[kind+"_at"].(string); ok && at != "" {
					parsed, err := time.Parse(time.RFC3339, at)
					if err != nil {
						return err
					}
					projected.Written = append(projected.Written, Written{Kind: kind, At: parsed})
				}
			}
			if !missing && reflect.DeepEqual(old, projected) {
				continue
			}
			if err = insertRecord(ctx, tx, "facts", factsColumns, f, extra); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return "", err
	}
	return last, nil
}

// ClassifiedBy identifies the evidence that supplies the categories.
func ClassifiedBy(categories []sources.Category) string {
	if len(categories) == 0 {
		return ""
	}
	source := categories[0].Source
	if source != enrichers.KindBrief && source != sources.Owner {
		return enrichers.SourceLinks
	}
	for _, c := range categories {
		if c.Source != source {
			return enrichers.SourceLinks
		}
	}
	return source
}

// RecordsDiffer compares a brief classification with record category names.
func RecordsDiffer(categories, records []sources.Category) bool {
	if ClassifiedBy(categories) != enrichers.KindBrief {
		return false
	}
	names := map[string]bool{}
	for _, c := range categories {
		names[c.Name] = true
	}
	other := map[string]bool{}
	for _, c := range records {
		other[c.Name] = true
	}
	if len(names) != len(other) {
		return true
	}
	for name := range names {
		if !other[name] {
			return true
		}
	}
	return false
}

func recordCategories(ctx context.Context, tx *sql.Tx, id string) ([]sources.Category, error) {
	es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? AND kind=? ORDER BY seq DESC LIMIT 1", id, enrichers.KindLinkFacts)
	if err != nil {
		return nil, err
	}
	out := []sources.Category{}
	if len(es) == 0 {
		return out, nil
	}
	var found enrichers.LinkFound
	if err = json.Unmarshal(es[0].Found, &found); err != nil {
		return nil, err
	}
	for _, c := range found.Categories {
		out = append(out, sources.Category{Name: c.Name, Source: c.Source})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Source < out[j].Source
	})
	unique := out[:0]
	for _, c := range out {
		if len(unique) == 0 || unique[len(unique)-1] != c {
			unique = append(unique, c)
		}
	}
	return unique, nil
}

// WebsiteDecisionOf returns the website selected by the recorded evidence.
func WebsiteDecisionOf(es []Enrichment, placesURL, ownerURL string) (source, state, url, title, why, checkedURL string, at time.Time) {
	if ownerURL != "" {
		state = sources.WebsiteHTTP
		if u, err := neturl.Parse(ownerURL); err == nil && strings.EqualFold(u.Scheme, "https") {
			state = sources.WebsiteHTTPS
		}
		for _, e := range es {
			if e.Kind != sources.WebsiteCheck {
				continue
			}
			var w website.Result
			if json.Unmarshal(e.Found, &w) != nil {
				continue
			}
			if w.From == ownerURL {
				return WebsiteByOwner, w.State, ownerURL, w.Title, "", "", e.At
			}
			for _, miss := range w.Misses {
				if miss.From == ownerURL {
					return WebsiteByOwner, state, ownerURL, "", "", "", e.At
				}
			}
		}
		return WebsiteByOwner, state, ownerURL, "", "", "", time.Time{}
	}
	state = sources.WebsiteUnknown
	var check *website.Result
	var checkAt time.Time
	var confirmed, unfound *enrichers.WebsiteSearchFound
	var confirmedAt, unfoundAt time.Time
	takeovers := map[string]bool{}
	searchTakeover := false
	placeChecked := false
	for _, e := range es {
		if e.Kind == sources.WebsiteCheck {
			for _, u := range website.URLs(e.Inputs) {
				if u == placesURL {
					placeChecked = true
				}
			}
		}
		if e.Kind == sources.WebsiteCheck && check == nil {
			var w website.Result
			if json.Unmarshal(e.Found, &w) == nil {
				check = &w
				checkAt = e.At
			}
		}
		if e.Kind != enrichers.KindWebsiteSearch {
			continue
		}
		var v enrichers.WebsiteSearchFound
		if json.Unmarshal(e.Found, &v) != nil || v.Failed() {
			continue
		}
		working := v.Confirmation != nil && (v.Confirmation.State == sources.WebsiteHTTPS || v.Confirmation.State == sources.WebsiteHTTP)
		if working && !searchLandsOn(v) && v.Website != "" {
			if key := websiteSiteKey(v.Website); key != "" {
				takeovers[key] = true
			}
			searchTakeover = searchTakeover || v.WebsiteFrom == WebsiteBySearch
		}
		if inertSearch(v) {
			continue
		}
		if working && confirmed == nil {
			confirmed = &v
			confirmedAt = e.At
		}
		if unfound == nil && (v.Answer.URL == "" || v.Confirmation != nil && v.Confirmation.State == sources.WebsiteNone) {
			unfound = &v
			unfoundAt = e.At
		}
	}
	if check != nil && (check.State == sources.WebsiteHTTPS || check.State == sources.WebsiteHTTP) && !searchTakeover && !takeovers[websiteSiteKey(check.URL)] {
		return WebsiteByCheck, check.State, check.URL, check.Title, "", check.From, checkAt
	}
	if confirmed != nil {
		c := confirmed.Confirmation
		return WebsiteBySearch, c.State, c.URL, c.Title, confirmed.Answer.Why, "", confirmedAt
	}
	if placesURL != "" && !placeChecked {
		if check != nil {
			title, at = check.Title, checkAt
		}
		return WebsiteByPlaces, sources.WebsiteUnknown, "", title, "", "", at
	}
	if unfound != nil {
		return "", sources.WebsiteNone, "", "", unfound.Answer.Why, "", unfoundAt
	}
	if check != nil {
		title = check.Title
		at = checkAt
	}
	return
}

func inertSearch(v enrichers.WebsiteSearchFound) bool {
	if v.Website == "" {
		return false
	}
	if v.Confirmation == nil {
		return v.Case == enrichers.SearchCaseStands || v.Case == enrichers.SearchCaseNone
	}
	return v.Confirmation.State == sources.WebsiteNone || (v.Confirmation.State == sources.WebsiteHTTPS || v.Confirmation.State == sources.WebsiteHTTP) && searchLandsOn(v)
}

func searchLandsOn(v enrichers.WebsiteSearchFound) bool {
	if v.Confirmation == nil {
		return false
	}
	key := websiteSiteKey(v.Confirmation.URL)
	return key != "" && key == websiteSiteKey(v.Website)
}

func websiteSiteKey(address string) string {
	for _, raw := range []string{address, "http://" + address} {
		if u, err := neturl.Parse(raw); err == nil && u.Hostname() != "" {
			return strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), "www.")
		}
	}
	return ""
}

// MinutesByRoute and MinutesByZip identify the deciding drive result.
const (
	MinutesByRoute = "route"
	MinutesByZip   = "zip"
)

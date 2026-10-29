package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// OwnerRecordIDPrefix identifies an owner's immutable record.
const OwnerRecordIDPrefix = "own_"

// ValidOwnerRecordID recognizes owner record identifiers.
func ValidOwnerRecordID(s string) bool { return validID(s, OwnerRecordIDPrefix) }

// OwnerHeadcount records the count and year supplied by the owner.
type OwnerHeadcount struct {
	Count int `json:"count"`
	Year  int `json:"year"`
}

// OwnerRevenue is a range with either bound optional.
type OwnerRevenue struct {
	Low  *int64 `json:"low,omitempty"`
	High *int64 `json:"high,omitempty"`
}

// OwnerPerson is a person named by the owner.
type OwnerPerson struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// OwnerValues holds the facts supplied in an owner record.
type OwnerValues struct {
	Name       *string          `json:"name,omitempty"`
	Headcount  *OwnerHeadcount  `json:"headcount,omitempty"`
	Revenue    *OwnerRevenue    `json:"revenue,omitempty"`
	Category   *string          `json:"category,omitempty"`
	Website    *string          `json:"website,omitempty"`
	Address    *sources.Address `json:"address,omitempty"`
	Phone      *string          `json:"phone,omitempty"`
	Email      *string          `json:"email,omitempty"`
	People     []OwnerPerson    `json:"people,omitempty"`
	KnownSince *int             `json:"known_since,omitempty"`
	EIN        *string          `json:"ein,omitempty"`
	Fleet      *int64           `json:"fleet,omitempty"`
}

// OwnerRecord is one immutable statement of the owner's facts.
type OwnerRecord struct {
	ID, Candidate           string
	Founded                 bool
	Note, UserID, UserEmail string
	At                      time.Time
	Set                     OwnerValues
	Cleared                 []string
}

type ownerAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
	State  string `json:"state"`
	Zip    string `json:"zip"`
	County string `json:"county,omitempty"`
}

// MarshalJSON encodes the owner's address with the public fact member names.
func (v OwnerValues) MarshalJSON() ([]byte, error) {
	var address *ownerAddress
	if x := v.Address; x != nil {
		address = &ownerAddress{x.Street, x.City, x.State, x.Zip, x.County}
	}
	var people *[]OwnerPerson
	if v.People != nil {
		people = &v.People
	}
	return json.Marshal(struct {
		Name       *string         `json:"name,omitempty"`
		Headcount  *OwnerHeadcount `json:"headcount,omitempty"`
		Revenue    *OwnerRevenue   `json:"revenue,omitempty"`
		Category   *string         `json:"category,omitempty"`
		Website    *string         `json:"website,omitempty"`
		Address    *ownerAddress   `json:"address,omitempty"`
		Phone      *string         `json:"phone,omitempty"`
		Email      *string         `json:"email,omitempty"`
		People     *[]OwnerPerson  `json:"people,omitempty"`
		KnownSince *int            `json:"known_since,omitempty"`
		EIN        *string         `json:"ein,omitempty"`
		Fleet      *int64          `json:"fleet,omitempty"`
	}{v.Name, v.Headcount, v.Revenue, v.Category, v.Website, address, v.Phone, v.Email, people, v.KnownSince, v.EIN, v.Fleet})
}

// UnmarshalJSON decodes the owner's public fact members.
func (v *OwnerValues) UnmarshalJSON(b []byte) error {
	*v = OwnerValues{}
	type plain OwnerValues
	var wire struct {
		*plain
		Address *ownerAddress `json:"address"`
	}
	wire.plain = (*plain)(v)
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	if a := wire.Address; a != nil {
		v.Address = &sources.Address{Street: a.Street, City: a.City, State: a.State, Zip: a.Zip, County: a.County}
	} else {
		v.Address = nil
	}
	return nil
}
func digits(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
func factFields(v OwnerValues) []bool {
	return []bool{v.Name != nil, v.Headcount != nil, v.Revenue != nil, v.Category != nil, v.Website != nil, v.Address != nil, v.Phone != nil, v.Email != nil, v.People != nil, v.KnownSince != nil, v.EIN != nil, v.Fleet != nil}
}

// ValidOwnerValues validates each supplied fact independently.
func ValidOwnerValues(v OwnerValues) bool {
	if v.Name != nil && strings.TrimSpace(*v.Name) == "" {
		return false
	}
	if h := v.Headcount; h != nil && (h.Count < 0 || h.Year < 1 || h.Year > 9999) {
		return false
	}
	if r := v.Revenue; r != nil {
		if r.Low == nil && r.High == nil || r.Low != nil && *r.Low < 0 || r.High != nil && *r.High < 0 || r.Low != nil && r.High != nil && *r.Low > *r.High {
			return false
		}
	}
	if v.Category != nil && !slices.Contains(sources.Categories(), *v.Category) {
		return false
	}
	if v.Website != nil && (*v.Website != strings.TrimSpace(*v.Website) || website.Inputs(*v.Website, "") == "") {
		return false
	}
	if a := v.Address; a != nil {
		if strings.TrimSpace(a.Street+a.City+a.State+a.Zip) == "" {
			return false
		}
		if a.State != "" && (len(a.State) != 2 || a.State[0] < 'A' || a.State[0] > 'Z' || a.State[1] < 'A' || a.State[1] > 'Z') {
			return false
		}
		if a.Zip != "" && !digits(a.Zip, 5) {
			return false
		}
		if a.County != "" && (len(a.County) != len(a.State)+4 || !strings.HasPrefix(a.County, a.State+"-") || !digits(a.County[len(a.State)+1:], 3)) {
			return false
		}
	}
	if v.Phone != nil && !digits(*v.Phone, 10) {
		return false
	}
	if v.Email != nil {
		e := *v.Email
		i := strings.IndexByte(e, '@')
		if i < 1 || i == len(e)-1 || strings.Count(e, "@") != 1 {
			return false
		}
		for _, b := range []byte(e) {
			if b <= 0x20 {
				return false
			}
		}
	}
	if v.People != nil {
		if len(v.People) == 0 {
			return false
		}
		for _, p := range v.People {
			if strings.TrimSpace(p.Name) == "" {
				return false
			}
		}
	}
	if v.KnownSince != nil && (*v.KnownSince < 1 || *v.KnownSince > 9999) {
		return false
	}
	return (v.EIN == nil || digits(*v.EIN, 9)) && (v.Fleet == nil || *v.Fleet >= 0)
}
func ownerOrder(a, b OwnerRecord) bool {
	if !a.At.Equal(b.At) {
		return a.At.After(b.At)
	}
	return a.ID < b.ID
}
func sortedOwners(rs []OwnerRecord) []OwnerRecord {
	out := append([]OwnerRecord{}, rs...)
	sort.Slice(out, func(i, j int) bool { return ownerOrder(out[i], out[j]) })
	return out
}

// OwnerSet returns the deciding record of every fact still set.
func OwnerSet(rs []OwnerRecord) map[string]OwnerRecord {
	out := map[string]OwnerRecord{}
	seen := map[string]bool{}
	for _, r := range sortedOwners(rs) {
		fields := factFields(r.Set)
		for i, w := range sources.SettableFacts() {
			if seen[w] {
				continue
			}
			if slices.Contains(r.Cleared, w) {
				seen[w] = true
			} else if fields[i] {
				seen[w] = true
				out[w] = r
			}
		}
	}
	return out
}
func ownerGiven(rs []OwnerRecord) []sources.Record {
	out := []sources.Record{}
	cleared := map[string]bool{}
	people := false
	for _, r := range sortedOwners(rs) {
		v := r.Set
		rd := sources.Reading{Filed: r.At}
		if !cleared[sources.FactName] && v.Name != nil {
			rd.Name = *v.Name
		}
		if !cleared[sources.FactAddress] && v.Address != nil {
			a := v.Address
			rd.Street, rd.City, rd.State, rd.Zip, rd.County = a.Street, a.City, a.State, a.Zip, a.County
		}
		if !cleared[sources.FactPhone] && v.Phone != nil {
			rd.Phone = *v.Phone
		}
		if !cleared[sources.FactEmail] && v.Email != nil {
			rd.Email = *v.Email
		}
		if !cleared[sources.FactPeople] && !people && v.People != nil {
			people = true
			for _, p := range v.People {
				rd.People = append(rd.People, sources.Person{Name: p.Name, Role: p.Role})
			}
		}
		if !cleared[sources.FactKnownSince] && v.KnownSince != nil {
			rd.KnownSince = time.Date(*v.KnownSince, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		if !cleared[sources.FactEIN] && v.EIN != nil {
			rd.EINKey = *v.EIN
		}
		if !cleared[sources.FactFleet] && v.Fleet != nil {
			rd.PowerUnits = v.Fleet
		}
		out = append(out, sources.Record{Source: sources.Owner, SourceID: r.ID, Seen: r.At, Reading: rd})
		for _, w := range r.Cleared {
			cleared[w] = true
		}
	}
	return out
}

type ownerRow struct {
	ID, Candidate                    string
	Founded                          bool
	Note, UserID, UserEmail          string
	At                               time.Time
	Name                             *string
	Headcount, HeadcountYear         *int
	RevenueLow, RevenueHigh          *int64
	Category, Website                *string
	Street, City, State, Zip, County *string
	Phone, Email                     *string
	People                           []OwnerPerson
	KnownSince                       *int
	EIN                              *string
	Fleet                            *int64
	Cleared                          []string
}

var ownerColumns = columns("id:ID candidate:Candidate founded:Founded note:Note user_id:UserID user_email:UserEmail at:At name:Name headcount:Headcount headcount_year:HeadcountYear revenue_low:RevenueLow revenue_high:RevenueHigh category:Category website:Website street:Street city:City state:State zip:Zip county:County phone:Phone email:Email people:People known_since:KnownSince ein:EIN fleet:Fleet cleared:Cleared")

func ownerRecordRow(r OwnerRecord) ownerRow {
	v := r.Set
	x := ownerRow{ID: r.ID, Candidate: r.Candidate, Founded: r.Founded, Note: r.Note, UserID: r.UserID, UserEmail: r.UserEmail, At: r.At, Name: v.Name, Category: v.Category, Website: v.Website, Phone: v.Phone, Email: v.Email, People: v.People, KnownSince: v.KnownSince, EIN: v.EIN, Fleet: v.Fleet, Cleared: r.Cleared}
	if v.Headcount != nil {
		x.Headcount = &v.Headcount.Count
		x.HeadcountYear = &v.Headcount.Year
	}
	if v.Revenue != nil {
		x.RevenueLow = v.Revenue.Low
		x.RevenueHigh = v.Revenue.High
	}
	if a := v.Address; a != nil {
		x.Street = &a.Street
		x.City = &a.City
		x.State = &a.State
		x.Zip = &a.Zip
		x.County = &a.County
	}
	return x
}
func (x ownerRow) record() OwnerRecord {
	r := OwnerRecord{ID: x.ID, Candidate: x.Candidate, Founded: x.Founded, Note: x.Note, UserID: x.UserID, UserEmail: x.UserEmail, At: x.At, Cleared: x.Cleared, Set: OwnerValues{Name: x.Name, Category: x.Category, Website: x.Website, Phone: x.Phone, Email: x.Email, People: x.People, KnownSince: x.KnownSince, EIN: x.EIN, Fleet: x.Fleet}}
	if x.Headcount != nil {
		r.Set.Headcount = &OwnerHeadcount{*x.Headcount, *x.HeadcountYear}
	}
	if x.RevenueLow != nil || x.RevenueHigh != nil {
		r.Set.Revenue = &OwnerRevenue{x.RevenueLow, x.RevenueHigh}
	}
	if x.Street != nil {
		r.Set.Address = &sources.Address{Street: *x.Street, City: *x.City, State: *x.State, Zip: *x.Zip, County: *x.County}
	}
	if r.Cleared == nil {
		r.Cleared = []string{}
	}
	return r
}

type ownerPresenceKey struct{}
type ownerPresence struct{ checked, present, einChecked, einPresent bool }

func ownerReadContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(ownerPresenceKey{}).(*ownerPresence); ok {
		return ctx
	}
	return context.WithValue(ctx, ownerPresenceKey{}, &ownerPresence{})
}
func ownersPresent(ctx context.Context, tx *sql.Tx) (bool, error) {
	cache, _ := ctx.Value(ownerPresenceKey{}).(*ownerPresence)
	if cache != nil && cache.checked {
		return cache.present, nil
	}
	var present bool
	if err := scanSQL(ctx, tx, "SELECT EXISTS(SELECT 1 FROM owner_records)", nil, &present); err != nil {
		return false, err
	}
	if cache != nil {
		cache.checked, cache.present = true, present
	}
	return present, nil
}
func einSourcesPresent(ctx context.Context, tx *sql.Tx) (bool, error) {
	cache, _ := ctx.Value(ownerPresenceKey{}).(*ownerPresence)
	if cache != nil && cache.einChecked {
		return cache.einPresent, nil
	}
	var present bool
	if err := scanSQL(ctx, tx, "SELECT EXISTS(SELECT 1 FROM links WHERE scraper IN (?,?))", []any{"form5500", "osha_ita"}, &present); err != nil {
		return false, err
	}
	if cache != nil {
		cache.einChecked, cache.einPresent = true, present
	}
	return present, nil
}
func ownerRecordsKey(ctx context.Context, tx *sql.Tx, id string) ([]OwnerRecord, error) {
	present, err := ownersPresent(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !present {
		return []OwnerRecord{}, nil
	}
	xs, err := readRecords[ownerRow](ctx, tx, "owner_records", ownerColumns, " WHERE candidate IN (WITH RECURSIVE family(id) AS (SELECT ? UNION SELECT c.id FROM candidates c JOIN family f ON c.merged_into=f.id WHERE c.status='merged') SELECT id FROM family) ORDER BY at DESC,id", id)
	if err != nil {
		return nil, err
	}
	out := make([]OwnerRecord, 0, len(xs))
	for _, x := range xs {
		out = append(out, x.record())
	}
	return out, nil
}

// OwnerRecords reads the immutable records of a candidate and its merged family.
func (s *Store) OwnerRecords(ctx context.Context, id string) ([]OwnerRecord, error) {
	var out []OwnerRecord
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, id); err != nil {
			return err
		}
		var err error
		out, err = ownerRecordsKey(ctx, tx, id)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AddOwnerRecord synchronously records, projects and judges one owner's statement.
func (s *Store) AddOwnerRecord(ctx context.Context, r OwnerRecord) (OwnerRecord, Candidate, error) {
	var out OwnerRecord
	var c Candidate
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if r.UserID == "" {
			return ErrUnreachable
		}
		if strings.TrimSpace(r.Note) == "" || !ValidOwnerValues(r.Set) {
			return ErrInvalid
		}
		fields := factFields(r.Set)
		words := sources.SettableFacts()
		seen := map[string]bool{}
		anySet := slices.Contains(fields, true)
		for _, w := range r.Cleared {
			i := slices.Index(words, w)
			if i < 0 || seen[w] || fields[i] {
				return ErrInvalid
			}
			seen[w] = true
		}
		if !anySet && len(r.Cleared) == 0 || r.Candidate == "" && (r.Set.Name == nil || len(r.Cleared) > 0) {
			return ErrInvalid
		}
		var err error
		if r.Candidate != "" {
			c, err = candidateKey(ctx, tx, r.Candidate)
			if err != nil {
				return err
			}
			if c.Status == StatusMerged {
				return ErrInvalid
			}
		}
		id, err := s.mintKey(ctx, tx, OwnerRecordIDPrefix)
		if err != nil {
			return err
		}
		t := s.now()
		out = OwnerRecord{ID: id, Candidate: r.Candidate, Note: r.Note, UserID: r.UserID, UserEmail: r.UserEmail, At: t, Set: r.Set, Cleared: []string{}}
		for _, w := range words {
			if seen[w] {
				out.Cleared = append(out.Cleared, w)
			}
		}
		if out.Candidate == "" {
			rd := ownerGiven([]OwnerRecord{out})[0].Reading
			rd.EINKey = sources.EINKey(rd.EINKey)
			rd.NameKey = sources.NameKey(rd.Name)
			rd.PhoneKey = sources.PhoneKey(rd.Phone)
			rd.DomainKey = sources.DomainKey(rd.Email)
			out.Candidate, _, _, err = matchOwnerCandidate(ctx, tx, rd, r.Set.Website)
			if err != nil {
				return err
			}
			if out.Candidate == "" {
				out.Candidate, err = s.mintKey(ctx, tx, CandidateIDPrefix)
				if err != nil {
					return err
				}
				out.Founded = true
				c = Candidate{ID: out.Candidate, Status: StatusActive, Names: []string{}, People: []sources.Sourced[sources.Person]{}, Cargo: []string{}, Created: t, Updated: t}
				if err = putCandidate(ctx, tx, c); err != nil {
					return err
				}
			}
		}
		if err = insertRecord(ctx, tx, "owner_records", ownerColumns, ownerRecordRow(out), nil); err != nil {
			return err
		}
		if cache, _ := ctx.Value(ownerPresenceKey{}).(*ownerPresence); cache != nil {
			cache.checked, cache.present = true, true
		}
		if err = s.refreshKey(ctx, tx, out.Candidate, t); err != nil {
			return err
		}
		c, err = candidateKey(ctx, tx, out.Candidate)
		if err != nil {
			return err
		}
		for _, en := range enrichers.New(enrichers.Config{}).All() {
			if der, ok := en.(enrichers.Deriver); ok {
				if _, err = s.deriveCandidate(ctx, tx, c, der, false, t); err != nil {
					return err
				}
			}
		}
		if err = s.projectFacts(ctx, tx, c.ID); err != nil {
			return err
		}
		if _, err = judgeKeys(ctx, tx, []string{c.ID}, t); err != nil {
			return err
		}
		c, err = candidateKey(ctx, tx, c.ID)
		return err
	}))
	if err != nil {
		return OwnerRecord{}, Candidate{}, err
	}
	return out, c, nil
}

func oldestCandidate(ctx context.Context, tx *sql.Tx, ids []string) (string, error) {
	var best Candidate
	for _, id := range ids {
		if id == "" {
			continue
		}
		c, err := candidateKey(ctx, tx, id)
		if err != nil {
			return "", err
		}
		if c.Status != StatusActive {
			continue
		}
		if best.ID == "" || c.Created.Before(best.Created) || c.Created.Equal(best.Created) && c.ID < best.ID {
			best = c
		}
	}
	return best.ID, nil
}
func ownerMatches(ctx context.Context, tx *sql.Tx, kind, key, state, zip string) ([]string, error) {
	present, err := ownersPresent(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !present {
		return []string{}, nil
	}
	where := ""
	args := []any{}
	switch kind {
	case LinkEIN:
		where = " WHERE ein=?"
		args = []any{key}
	case LinkPhone:
		where = " WHERE phone=?"
		args = []any{key}
	case LinkDomain:
		where = " WHERE email IS NOT NULL OR website IS NOT NULL"
	case LinkMatchKey:
		where = " WHERE state=? AND zip=?"
		args = []any{state, zip}
	}
	selectColumns := "candidate"
	if kind == LinkDomain {
		selectColumns = "candidate,email,website"
	}
	rows, err := querySQL(ctx, tx, "SELECT DISTINCT "+selectColumns+" FROM owner_records"+where, args...)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if kind == LinkDomain {
			var email, site sql.NullString
			err = rows.Scan(&id, &email, &site)
			if err == nil && sources.DomainKey(email.String) != key && places.DomainKey(site.String) != key {
				continue
			}
		} else {
			err = rows.Scan(&id)
		}
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		for {
			var status, next string
			err = scanSQL(ctx, tx, "SELECT status,merged_into FROM candidates WHERE id=?", []any{id}, &status, &next)
			if err != nil {
				return nil, err
			}
			if status != StatusMerged {
				break
			}
			id = next
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		rs, err := ownerRecordsKey(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		given := ownerGiven(rs)
		names := []string{}
		addresses := []sources.Address{}
		matched := false
		for _, g := range given {
			rd := g.Reading
			switch kind {
			case LinkEIN:
				matched = matched || sources.EINKey(rd.EINKey) == key
			case LinkPhone:
				matched = matched || sources.PhoneKey(rd.Phone) == key
			case LinkDomain:
				matched = matched || sources.DomainKey(rd.Email) == key
			case LinkMatchKey:
				if rd.Name != "" {
					names = append(names, sources.NameKey(rd.Name))
				}
				if rd.State != "" {
					addresses = append(addresses, sources.Address{State: rd.State, Zip: rd.Zip})
				}
			}
		}
		if kind == LinkMatchKey && slices.Contains(names, key) {
			for _, a := range addresses {
				if a.State == state && a.Zip == zip {
					matched = true
				}
			}
		}
		if kind == LinkDomain {
			cleared := false
			for _, r := range sortedOwners(rs) {
				if !cleared && r.Set.Website != nil && places.DomainKey(*r.Set.Website) == key {
					matched = true
				}
				if slices.Contains(r.Cleared, sources.FactWebsite) {
					cleared = true
				}
			}
		}
		if matched {
			out = append(out, id)
		}
	}
	return out, nil
}
func matchKeyCandidates(ctx context.Context, tx *sql.Tx, kind, key, state, zip string) (string, error) {
	if key == "" {
		return "", nil
	}
	condition := ""
	args := []any{StatusActive}
	switch kind {
	case LinkEIN:
		condition = "l.ein_key=? AND json_extract(l.reading,'$.EINKey')=?"
		args = append(args, key, key)
	case LinkPhone:
		condition = "l.phone_key=?"
		args = append(args, key)
	case LinkDomain:
		condition = "l.domain_key=?"
		args = append(args, key)
	case LinkMatchKey:
		condition = "l.state=? AND l.zip=? AND l.rowid IN (SELECT rowid FROM links WHERE name_key=? UNION SELECT n.rowid FROM link_name_keys k JOIN links n ON n.scraper=k.scraper AND n.source_id=k.source_id WHERE k.name_key=?) AND json_extract(l.reading,'$.State')=? AND json_extract(l.reading,'$.Zip')=? AND (json_extract(l.reading,'$.NameKey')=? OR EXISTS(SELECT 1 FROM json_each(l.reading,'$.NameKeys') n WHERE n.value=?))"
		args = append(args, state, zip, key, key, state, zip, key, key)
	}
	var link string
	err := scanSQL(ctx, tx, "SELECT l.candidate FROM links l WHERE (SELECT status FROM candidates WHERE id=l.candidate)=? AND "+condition+" ORDER BY (SELECT created FROM candidates WHERE id=l.candidate),l.candidate LIMIT 1", args, &link)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	ids, err := ownerMatches(ctx, tx, kind, key, state, zip)
	if err != nil {
		return "", err
	}
	ids = append(ids, link)
	return oldestCandidate(ctx, tx, ids)
}
func matchOwnerCandidate(ctx context.Context, tx *sql.Tx, rd sources.Reading, site *string) (string, string, float64, error) {
	id, reason, confidence, err := matchCandidate(ctx, tx, rd)
	if err != nil || id != "" {
		return id, reason, confidence, err
	}
	keys := []struct{ kind, key string }{{LinkPhone, rd.PhoneKey}, {LinkDomain, rd.DomainKey}}
	if site != nil {
		keys = append(keys, struct{ kind, key string }{LinkDomain, places.DomainKey(*site)})
	}
	for _, k := range keys {
		id, err = matchKeyCandidates(ctx, tx, k.kind, k.key, "", "")
		if err != nil {
			return "", "", 0, err
		}
		if id != "" {
			return id, k.kind, .85, nil
		}
	}
	return "", LinkSourceKey, 1, nil
}

func withCandidateEIN(ctx context.Context, tx *sql.Tx, c *Candidate) error {
	c.EIN, c.EINSource, c.EINRecord = "", "", ""
	if c.Status != StatusActive {
		return nil
	}
	hasOwners, err := ownersPresent(ctx, tx)
	if err != nil {
		return err
	}
	hasEINSources, err := einSourcesPresent(ctx, tx)
	if err != nil {
		return err
	}
	if !hasOwners && !hasEINSources {
		return nil
	}
	var ls []Link
	if hasEINSources {
		ls, err = readRecords[Link](ctx, tx, "links", linkColumns, " WHERE candidate=? AND scraper IN (?,?) AND (gone=0 OR NOT EXISTS(SELECT 1 FROM links live WHERE live.candidate=? AND live.gone=0))", c.ID, "form5500", "osha_ita", c.ID)
		if err != nil {
			return err
		}
	}
	var os []OwnerRecord
	if hasOwners {
		os, err = ownerRecordsKey(ctx, tx, c.ID)
		if err != nil {
			return err
		}
	}
	if len(ls) == 0 && len(os) == 0 {
		return nil
	}
	r := emptyRecords()
	for _, l := range ls {
		r.links[sources.SourceKey(l.Scraper, l.SourceID)] = l
	}
	p := sources.Pick(ownerFactOrders(sources.FactEIN), sourceRecords(linksFor(r, c.ID)), ownerGiven(os))
	c.EIN, c.EINSource, c.EINRecord = p.EIN.Value, p.EIN.Source, p.EIN.Key
	return nil
}

func ownerFactOrders(facts ...string) []sources.Order {
	out := []sources.Order{}
	for _, order := range sources.Orders() {
		if slices.Contains(facts, order.Fact) {
			out = append(out, order)
		}
	}
	return out
}
func factAlternatives(ctx context.Context, tx *sql.Tx, f *Facts) error {
	ls, err := readRecords[Link](ctx, tx, "links", linkColumns, " WHERE candidate=? AND (coalesce(json_extract(reading,'$.KnownSince'),'') NOT IN ('','0001-01-01T00:00:00Z') OR json_type(reading,'$.PowerUnits')='integer') AND (gone=0 OR NOT EXISTS(SELECT 1 FROM links live WHERE live.candidate=? AND live.gone=0))", f.Candidate, f.Candidate)
	if err != nil {
		return err
	}
	os, err := ownerRecordsKey(ctx, tx, f.Candidate)
	if err != nil {
		return err
	}
	f.OtherKnownSince = []sources.Sourced[time.Time]{}
	f.OtherPowerUnits = []sources.Sourced[int64]{}
	if len(ls) == 0 && len(os) == 0 {
		return nil
	}
	r := emptyRecords()
	for _, l := range ls {
		r.links[sources.SourceKey(l.Scraper, l.SourceID)] = l
	}
	p := sources.Pick(ownerFactOrders(sources.FactKnownSince, sources.FactFleet), sourceRecords(linksFor(r, f.Candidate)), ownerGiven(os))
	f.OtherKnownSince = []sources.Sourced[time.Time]{}
	f.OtherPowerUnits = []sources.Sourced[int64]{}
	for _, v := range append([]sources.Sourced[time.Time]{p.KnownSince}, p.OtherKnownSince...) {
		if v.Source == "" || v.Value.Equal(f.KnownSince) {
			continue
		}
		duplicate := false
		for _, x := range f.OtherKnownSince {
			duplicate = duplicate || x.Value.Equal(v.Value)
		}
		if !duplicate {
			f.OtherKnownSince = append(f.OtherKnownSince, v)
		}
	}
	for _, v := range append([]sources.Sourced[int64]{p.PowerUnits}, p.OtherPowerUnits...) {
		if v.Source == "" || f.PowerUnits != nil && v.Value == *f.PowerUnits {
			continue
		}
		duplicate := false
		for _, x := range f.OtherPowerUnits {
			duplicate = duplicate || x.Value == v.Value
		}
		if !duplicate {
			f.OtherPowerUnits = append(f.OtherPowerUnits, v)
		}
	}
	return nil
}
func ownerURL(rs []OwnerRecord) string {
	if o, ok := OwnerSet(rs)[sources.FactWebsite]; ok {
		return *o.Set.Website
	}
	return ""
}
func ownerFacts(ctx context.Context, tx *sql.Tx, f *Facts, es []Enrichment, p string) error {
	os, err := ownerRecordsKey(ctx, tx, f.Candidate)
	if err != nil {
		return err
	}
	set := OwnerSet(os)
	if len(os) > 0 {
		picked := sources.Pick(ownerFactOrders(sources.FactKnownSince), nil, ownerGiven(os))
		if picked.KnownSince.Source == sources.Owner {
			f.KnownSince = picked.KnownSince.Value
			f.KnownSinceSource = sources.Owner
		}
	}
	if o, ok := set[sources.FactFleet]; ok {
		f.PowerUnits = o.Set.Fleet
	}
	if o, ok := set[sources.FactCategory]; ok {
		f.Categories = []sources.Category{{Name: *o.Set.Category, Source: sources.Owner}}
	}
	f.WebsiteSource, f.Website, f.WebsiteURL, _, _, f.WebsiteCheckedURL, _ = WebsiteDecisionOf(es, p, ownerURL(os))
	return nil
}

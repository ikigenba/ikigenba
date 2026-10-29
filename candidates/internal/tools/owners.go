package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Owner tool descriptions are public copy.
const (
	DescribeAddBusiness     string = "Add a business with the owner's facts and a note."
	DescribeSetFacts        string = "Set or clear the owner's facts on a candidate business."
	EventOwnerRecordWritten        = "owner_record.written"
)

// HeadcountArgs describes a dated headcount.
type HeadcountArgs struct {
	Count int `json:"count" mcp:"required"`
	Year  int `json:"year" mcp:"required"`
}

// RevenueArgs describes revenue bounds.
type RevenueArgs struct {
	Low  *int64 `json:"low"`
	High *int64 `json:"high"`
}

// AddressArgs describes a postal address.
type AddressArgs struct {
	Street string `json:"street"`
	City   string `json:"city"`
	State  string `json:"state"`
	Zip    string `json:"zip"`
	County string `json:"county"`
}

// PersonArgs describes a person and role.
type PersonArgs struct {
	Name string `json:"name" mcp:"required"`
	Role string `json:"role"`
}

// AddBusinessArgs supplies owner facts for a business.
type AddBusinessArgs struct {
	Name       string         `json:"name" mcp:"required"`
	Headcount  *HeadcountArgs `json:"headcount"`
	Revenue    *RevenueArgs   `json:"revenue"`
	Category   *string        `json:"category"`
	Website    *string        `json:"website"`
	Address    *AddressArgs   `json:"address"`
	Phone      *string        `json:"phone"`
	Email      *string        `json:"email"`
	People     []PersonArgs   `json:"people"`
	KnownSince *int           `json:"known_since"`
	EIN        *string        `json:"ein"`
	Fleet      *int64         `json:"fleet"`
	Note       string         `json:"note" mcp:"required"`
}

// SetFactsArgs supplies facts to set and clear.
type SetFactsArgs struct {
	Candidate  string         `json:"candidate" mcp:"required"`
	Name       *string        `json:"name"`
	Headcount  *HeadcountArgs `json:"headcount"`
	Revenue    *RevenueArgs   `json:"revenue"`
	Category   *string        `json:"category"`
	Website    *string        `json:"website"`
	Address    *AddressArgs   `json:"address"`
	Phone      *string        `json:"phone"`
	Email      *string        `json:"email"`
	People     []PersonArgs   `json:"people"`
	KnownSince *int           `json:"known_since"`
	EIN        *string        `json:"ein"`
	Fleet      *int64         `json:"fleet"`
	Clear      []string       `json:"clear"`
	Note       string         `json:"note" mcp:"required"`
}

// OwnerRecord is an owner's dated record as answered for one candidate.
type OwnerRecord struct {
	ID        string            `json:"id"`
	Candidate string            `json:"candidate"`
	Founded   bool              `json:"founded"`
	Note      string            `json:"note"`
	User      string            `json:"user"`
	Email     string            `json:"email"`
	At        string            `json:"at"`
	Set       store.OwnerValues `json:"set"`
	Cleared   []string          `json:"cleared"`
}

// MarshalJSON writes an empty cleared list as an array.
func (v OwnerRecord) MarshalJSON() ([]byte, error) {
	type plain OwnerRecord
	if v.Cleared == nil {
		v.Cleared = []string{}
	}
	return json.Marshal(plain(v))
}

// OwnerSetBy names the owner record deciding one fact.
type OwnerSetBy struct {
	Record string `json:"record"`
	Note   string `json:"note"`
	At     string `json:"at"`
}

// SourcedCount names the source of a count.
type SourcedCount struct {
	Value  int64  `json:"value"`
	Source string `json:"source"`
}

// OwnerWrite answers an owner write with the business it changed.
type OwnerWrite struct {
	Record    OwnerRecord `json:"record"`
	Candidate Detail      `json:"candidate"`
}

func ownerValues(a SetFactsArgs) store.OwnerValues {
	v := store.OwnerValues{Name: a.Name, Category: a.Category, Website: a.Website, Phone: a.Phone, Email: a.Email, KnownSince: a.KnownSince, EIN: a.EIN, Fleet: a.Fleet}
	if a.Headcount != nil {
		v.Headcount = &store.OwnerHeadcount{Count: a.Headcount.Count, Year: a.Headcount.Year}
	}
	if a.Revenue != nil {
		v.Revenue = &store.OwnerRevenue{Low: a.Revenue.Low, High: a.Revenue.High}
	}
	if a.Address != nil {
		v.Address = &sources.Address{Street: a.Address.Street, City: a.Address.City, State: a.Address.State, Zip: a.Address.Zip, County: a.Address.County}
	}
	if a.People != nil {
		v.People = []store.OwnerPerson{}
		for _, p := range a.People {
			v.People = append(v.People, store.OwnerPerson{Name: p.Name, Role: p.Role})
		}
	}
	return v
}
func ownerFields(v store.OwnerValues) map[string]json.RawMessage {
	b, _ := json.Marshal(v)
	var m map[string]json.RawMessage
	_ = json.Unmarshal(b, &m)
	return m
}
func ownerRecordOf(r store.OwnerRecord, id string) OwnerRecord {
	return OwnerRecord{ID: r.ID, Candidate: r.Candidate, Founded: r.Founded && r.Candidate == id, Note: r.Note, User: r.UserID, Email: r.UserEmail, At: stamp(r.At), Set: r.Set, Cleared: append([]string{}, r.Cleared...)}
}
func ownerURL(rs []store.OwnerRecord) string {
	if r, ok := store.OwnerSet(rs)[sources.FactWebsite]; ok && r.Set.Website != nil {
		return *r.Set.Website
	}
	return ""
}
func writeOwner(ctx context.Context, c identity.Caller, cfg Config, r store.OwnerRecord) mcp.Result {
	if c.UserID == "" {
		return refuse(passes.RefusalUnavailable)
	}
	r.UserID, r.UserEmail = c.UserID, c.Email
	r, _, err := cfg.Store.AddOwnerRecord(ctx, r)
	if err != nil {
		return failure(err)
	}
	if cfg.Telemetry != nil {
		var set []string
		fields := ownerFields(r.Set)
		for _, w := range sources.SettableFacts() {
			if _, ok := fields[w]; ok {
				set = append(set, w)
			}
		}
		cfg.Telemetry.Emit(ctx, EventOwnerRecordWritten, telemetry.Attrs{"record": r.ID, "candidate": r.Candidate, "founded": r.Founded, "set": strings.Join(set, ","), "cleared": strings.Join(r.Cleared, ",")})
	}
	d, err := detailValue(ctx, cfg, r.Candidate)
	if err != nil {
		return failure(err)
	}
	return answer(OwnerWrite{ownerRecordOf(r, r.Candidate), d})
}
func registerOwners(srv *mcp.Server, cfg Config) {
	add(srv, "add_business", DescribeAddBusiness, mcp.Additive, func(ctx context.Context, c identity.Caller, a AddBusinessArgs) mcp.Result {
		v := ownerValues(SetFactsArgs{Name: &a.Name, Headcount: a.Headcount, Revenue: a.Revenue, Category: a.Category, Website: a.Website, Address: a.Address, Phone: a.Phone, Email: a.Email, People: a.People, KnownSince: a.KnownSince, EIN: a.EIN, Fleet: a.Fleet})
		if strings.TrimSpace(a.Note) == "" || !store.ValidOwnerValues(v) {
			return refuse(passes.RefusalInvalid)
		}
		return writeOwner(ctx, c, cfg, store.OwnerRecord{Note: a.Note, Set: v})
	})
	add(srv, "set_facts", DescribeSetFacts, mcp.Additive, func(ctx context.Context, c identity.Caller, a SetFactsArgs) mcp.Result {
		v := ownerValues(a)
		fields := ownerFields(v)
		invalid := strings.TrimSpace(a.Note) == "" || !store.ValidOwnerValues(v) || len(fields) == 0 && len(a.Clear) == 0
		seen := map[string]bool{}
		for _, w := range a.Clear {
			_, set := fields[w]
			if !slices.Contains(sources.SettableFacts(), w) || seen[w] || set {
				invalid = true
			}
			seen[w] = true
		}
		if invalid {
			return refuse(passes.RefusalInvalid)
		}
		if !store.ValidCandidateID(a.Candidate) {
			return refuse(passes.RefusalNotFound)
		}
		return writeOwner(ctx, c, cfg, store.OwnerRecord{Candidate: a.Candidate, Note: a.Note, Set: v, Cleared: a.Clear})
	})
}

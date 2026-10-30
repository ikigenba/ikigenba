package store_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

const searchDefaultUint8 uint8 = store.SearchDefaultLimit
const searchMaxUint8 uint8 = store.SearchMaxLimit

// R-4GPX-QFH9 R-75VO-PCHH
func TestSearchPublicAPI(t *testing.T) {
	var _ interface {
		Search(context.Context, string, int) ([]store.Match, error)
	} = (*store.Store)(nil)
	m := store.Match{store.Candidate{ID: "example"}, "Name", 0.75}
	shape := struct {
		Candidate store.Candidate
		Matched   string
		Score     float64
	}(m)
	contractEqual(t, store.Match(shape), m)
	contractEqual(t, []uint8{searchDefaultUint8, searchMaxUint8}, []uint8{10, 50})
}
func searchAttach(t *testing.T, f *contractFixture, key, name string, alternates ...string) string {
	t.Helper()
	o := contractObservation(key, name, f.now)
	o.Reading.PhoneKey, o.Reading.DomainKey = "", ""
	o.Reading.NameKey = key
	o.Reading.Names = append([]string{name}, alternates...)
	return contractAttach(t, f, "search-run", o)[0]
}
func searchCandidates(t *testing.T, s *store.Store, ids []string) []store.Candidate {
	t.Helper()
	out := make([]store.Candidate, 0, len(ids))
	for _, id := range ids {
		c, e := s.GetCandidate(context.Background(), id)
		contractOK(t, e)
		out = append(out, c)
	}
	return out
}

// The oracle walks single-edit edges. Two edges may edit a substring again
// after transposition, independently of the implementation's distance matrix.
func referenceOneEdit(a, b []rune) bool {
	if string(a) == string(b) {
		return true
	}
	if len(a) == len(b) {
		bad := []int{}
		for i := range a {
			if a[i] != b[i] {
				bad = append(bad, i)
			}
		}
		return len(bad) == 1 || len(bad) == 2 && bad[1] == bad[0]+1 && a[bad[0]] == b[bad[1]] && a[bad[1]] == b[bad[0]]
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b) != len(a)+1 {
		return false
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	return string(a[i:]) == string(b[i+1:])
}
func referenceDistance(a, b string, budget int) int {
	ar, br := []rune(a), []rune(b)
	if a == b {
		return 0
	}
	if referenceOneEdit(ar, br) {
		return 1
	}
	if budget < 2 || len(ar)-len(br) > budget || len(br)-len(ar) > budget {
		return budget + 1
	}
	if budget > 2 {
		panic("search oracle fixture exceeds two-edit words")
	}
	alphabet := []rune{}
	for _, r := range br {
		if !slices.Contains(alphabet, r) {
			alphabet = append(alphabet, r)
		}
	}
	for i := 0; i <= len(ar); i++ {
		for _, r := range alphabet {
			v := slices.Insert(slices.Clone(ar), i, r)
			if referenceOneEdit(v, br) {
				return 2
			}
			if i < len(ar) {
				v = slices.Clone(ar)
				v[i] = r
				if referenceOneEdit(v, br) {
					return 2
				}
			}
		}
		if i < len(ar) {
			v := slices.Delete(slices.Clone(ar), i, i+1)
			if referenceOneEdit(v, br) {
				return 2
			}
		}
		if i+1 < len(ar) {
			v := slices.Clone(ar)
			v[i], v[i+1] = v[i+1], v[i]
			if referenceOneEdit(v, br) {
				return 2
			}
		}
	}
	return budget + 1
}
func referenceWords(s string) []string {
	var words []string
	var word []rune
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else if len(word) > 0 {
			words = append(words, string(word))
			word = nil
		}
	}
	if len(word) > 0 {
		words = append(words, string(word))
	}
	return words
}
func referenceName(q, name string, display bool) (int, float64) {
	n := strings.ToLower(strings.ReplaceAll(name, "\x00", " "))
	tier := 0
	edits, allowance := 0, 0
	qw, nw := referenceWords(q), referenceWords(n)
	switch {
	case q == n:
		tier = 1
	case strings.HasPrefix(n, q):
		tier = 2
	default:
		if len(qw) > 0 {
			tier = 3
			for _, u := range qw {
				found := false
				for _, v := range nw {
					found = found || strings.HasPrefix(v, u)
				}
				if !found {
					tier = 0
					break
				}
			}
		}
		if tier == 0 && strings.Contains(n, q) {
			tier = 4
		}
		if tier == 0 && len(qw) > 0 {
			tier = 5
			for _, u := range qw {
				best, allowed := math.MaxInt, -1
				for _, v := range nw {
					a := utf8.RuneCountInString(v) / 5
					d := referenceDistance(u, v, a)
					if d <= a && (d < best || d == best && a > allowed) {
						best, allowed = d, a
					}
				}
				if allowed < 0 {
					tier = 0
					break
				}
				edits += best
				allowance += allowed
			}
		}
	}
	if tier == 0 {
		return 0, 0
	}
	if tier == 1 {
		return 1, 1
	}
	bands := map[int][2]float64{2: {0.8, 1}, 3: {0.6, 0.8}, 4: {0.4, 0.6}, 5: {0.1, 0.4}}
	b := bands[tier]
	d := 0.0
	if display {
		d = 0.6
	}
	k := 1.0
	if tier == 5 {
		k = 1 - float64(edits)/float64(allowance)
	}
	score := b[0] + (b[1]-b[0])*(d+0.25*min(1, float64(utf8.RuneCountInString(q))/float64(utf8.RuneCountInString(n)))+0.15*k)
	if score >= b[1] {
		score = math.Nextafter(b[1], 0)
	}
	return tier, score
}
func referenceSearch(candidates []store.Candidate, q string, limit int) []store.Match {
	q = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(q, "\x00", " ")))
	short := utf8.RuneCountInString(q) < 3
	result := []store.Match{}
	for _, c := range candidates {
		if c.Status != store.StatusActive {
			continue
		}
		names := append([]string{c.Name}, c.Names...)
		if short {
			names = names[:1]
		}
		best := store.Match{}
		seen := map[string]bool{"": true}
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			if short && !strings.HasPrefix(strings.ToLower(strings.ReplaceAll(name, "\x00", " ")), q) {
				continue
			}
			_, score := referenceName(q, name, name == c.Name)
			if score > best.Score {
				best = store.Match{Candidate: c, Matched: name, Score: score}
			}
		}
		if best.Score > 0 {
			result = append(result, best)
		}
	}
	slices.SortFunc(result, func(a, b store.Match) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return strings.Compare(a.Candidate.ID, b.Candidate.ID)
	})
	return result[:min(limit, len(result))]
}
func assertSearchRanking(t *testing.T, s *store.Store, candidates []store.Candidate, q string, limit int) {
	t.Helper()
	got, e := s.Search(context.Background(), q, limit)
	contractOK(t, e)
	want := referenceSearch(candidates, q, limit)
	if got == nil {
		t.Fatalf("Search(%q) returned nil slice", q)
	}
	contractEqual(t, len(got), len(want))
	for i, m := range got {
		contractEqual(t, m.Candidate, want[i].Candidate)
		contractEqual(t, m.Matched, want[i].Matched)
		if math.Abs(m.Score-want[i].Score) > 1e-14 {
			t.Fatalf("Search(%q)[%d] score %.17g want %.17g", q, i, m.Score, want[i].Score)
		}
	}
}

// R-4HXU-477Y R-7GUS-5A5Q R-7ARA-8FG9 R-7BZ6-M76Y R-7D72-ZYXN R-7FMV-RIF1
func TestSearchSmallStoreAgainstEditGraphRanking(t *testing.T) {
	f := contractDB(t)
	names := []string{"Acme Trucking", "Zed Holdings", "Acme Trucking North", "Acme Delivery", "Trujillo Transport Services", "MCCUEN CONSTRUCTION INC", "Abcdefghij", "Abcdzefghi Abcdfghi", "Jone Co", "A Trucking Acme", "Smith-Jones Co.", "AT&T Industries", "Brown Bird", "Acme", "Longword Service", "Decimal123 Trucking", "Acme\x00Trucking"}
	ids := []string{}
	for i, name := range names {
		alternates := []string{}
		switch i {
		case 0:
			alternates = []string{"ACME TRUCKING LLC", "Acme Trucking", ""}
		case 1:
			alternates = []string{"Acme Trucking"}
		case 12:
			alternates = []string{"BROWN HAWK LLC"}
		case 14:
			alternates = []string{"Middle Alpha", "Middle Omega"}
		}
		ids = append(ids, searchAttach(t, f, name, name, alternates...))
	}
	cs := searchCandidates(t, f.s, ids)
	queries := []string{"  ACME Trucking \t", "acme", "trucking acme", "me truck", "brown hawk", "trujilo transport", "mcuen", "cadefghij", "abdcefghij", "xbcdefghij", "abcdefghi", "jnoe", "acme truckinf", "truck acm", "smith co", "at&t", "a trucking", "decimal123", "middle", "unfindable", "---", "truking acme", "\x00 ACME\x00Trucking \x00", "acme\x00\x00trucking"}
	for _, q := range queries {
		for _, limit := range []int{1, 10, 50} {
			t.Run(q+"/"+strconv.Itoa(limit), func(t *testing.T) { assertSearchRanking(t, f.s, cs, q, limit) })
		}
	}
	got, e := f.s.Search(context.Background(), "trucking acme", 50)
	contractOK(t, e)
	for _, m := range got {
		if m.Candidate.ID == ids[0] {
			contractEqual(t, m.Score, math.Nextafter(0.8, 0))
		}
	}
	got, e = f.s.Search(context.Background(), "jnoe", 50)
	contractOK(t, e)
	contractEqual(t, len(got), 0)
}

// R-7I2O-J1WF R-7FMV-RIF1
func TestSearchShortUnicodePrefixAndLongUnicodeNames(t *testing.T) {
	f := contractDB(t)
	ids := []string{}
	for _, name := range []string{"Ab", "Abc", "Abc", "Zed", "Ébène Co", "ÉBÈNE", "At&T", "Smith-Jones", "中文公司"} {
		ids = append(ids, searchAttach(t, f, name+strconv.Itoa(len(ids)), name, "Ab Alternate"))
	}
	cs := searchCandidates(t, f.s, ids)
	for _, q := range []string{"a", " AB ", "ÉB", "中文"} {
		for _, limit := range []int{1, 2, 50} {
			assertSearchRanking(t, f.s, cs, q, limit)
		}
	}
	got, e := f.s.Search(context.Background(), "ab", 50)
	contractOK(t, e)
	contractEqual(t, got[0].Score, 1.0)
	if math.Abs(got[1].Score-0.9833333333333333) > 1e-14 {
		t.Fatal(got[1].Score)
	}
}

// R-7EEZ-DQOC
func TestSearchValidationPrecedenceAndReadOnly(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{searchAttach(t, f, "one", "Acme Trucking"), searchAttach(t, f, "two", "Other Business")}
	before := searchCandidates(t, f.s, ids)
	listed, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 500)
	contractOK(t, e)
	prior, e := f.s.Search(ctx, "acme", 50)
	contractOK(t, e)
	for _, args := range []struct {
		q     string
		limit int
	}{{"", 10}, {" \t\n", 10}, {"name", 0}, {"name", -1}, {"name", 51}} {
		got, err := f.s.Search(ctx, args.q, args.limit)
		contractError(t, err, store.ErrInvalid)
		if got != nil {
			t.Fatal("invalid returned a slice")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		got, err = f.s.Search(canceled, args.q, args.limit)
		if got != nil || !errors.Is(err, store.ErrUnreachable) || !errors.Is(err, context.Canceled) || errors.Is(err, store.ErrInvalid) {
			t.Fatalf("canceled: %v %v", got, err)
		}
		f.d.SetFailing(true)
		got, err = f.s.Search(ctx, args.q, args.limit)
		f.d.SetFailing(false)
		if got != nil || !errors.Is(err, store.ErrUnreachable) || errors.Is(err, store.ErrInvalid) {
			t.Fatalf("failing: %v %v", got, err)
		}
	}
	for _, q := range []string{"a", "acme", "truking", "nonexistent"} {
		_, e = f.s.Search(ctx, q, 50)
		contractOK(t, e)
	}
	contractEqual(t, searchCandidates(t, f.s, ids), before)
	after, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 500)
	contractOK(t, e)
	contractEqual(t, after, listed)
	matches, e := f.s.Search(ctx, "acme", 50)
	contractOK(t, e)
	contractEqual(t, matches, prior)
	f.reopen()
	contractEqual(t, searchCandidates(t, f.s, ids), before)
}

// R-EIT4-6RL9
func TestSearchFollowsEveryCandidateWrite(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	ids := []string{}
	check := func() {
		t.Helper()
		cs := searchCandidates(t, f.s, ids)
		for _, c := range cs {
			for _, name := range append([]string{c.Name}, c.Names...) {
				if name != "" {
					assertSearchRanking(t, f.s, cs, name, 50)
				}
			}
		}
	}
	a := searchAttach(t, f, "rename", "Acme Trucking")
	ids = append(ids, a)
	check()
	o := contractObservation("rename", "Newname Hauling", f.now)
	o.Reading.PhoneKey, o.Reading.DomainKey = "", ""
	o.Reading.Names = []string{o.Reading.Name}
	o.Digest = "renamed"
	contractAttach(t, f, "new-run", o)
	check()
	got, e := f.s.Search(ctx, "Acme Trucking", 50)
	contractOK(t, e)
	contractEqual(t, len(got), 0)
	b := searchAttach(t, f, "merge", "Brown Hawk Logistics")
	ids = append(ids, b)
	check()
	_, _, e = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: a, Other: b, Why: "same business", UserID: "owner"})
	contractOK(t, e)
	check()
	got, e = f.s.Search(ctx, "Brown Hawk Logistics", 50)
	contractOK(t, e)
	contractEqual(t, len(got), 1)
	contractEqual(t, got[0].Candidate.ID, a)
	ruling, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: a, Keys: []string{sources.SourceKey("fmcsa", "merge")}, Why: "different business", UserID: "owner"})
	contractOK(t, e)
	ids = append(ids, ruling.Other)
	check()
	for _, kind := range []string{store.KindExclude, store.KindInclude, store.KindNotSame} {
		r := store.Ruling{Kind: kind, Candidate: a, Why: "owner's rule", UserID: "owner"}
		if kind == store.KindNotSame {
			r.Other = ruling.Other
		}
		_, _, e = f.s.Decide(ctx, r)
		contractOK(t, e)
		check()
	}
	_, _, e = f.s.AddOwnerRecord(ctx, store.OwnerRecord{Candidate: a, Set: store.OwnerValues{Name: ownPtr("Owner Naming Company"), EIN: ownPtr("123456789")}, UserID: "owner", Note: "correct name"})
	contractOK(t, e)
	check()
	_, _, e = f.s.MarkGone(ctx, "fmcsa", "absent", "", 500)
	contractOK(t, e)
	check()
	_, e = f.s.Rebuild(ctx, "", 500)
	contractOK(t, e)
	check()
	_, _, e = f.s.Derive(ctx, enrichers.KindLinkFacts, "", true, 500)
	contractOK(t, e)
	check()
	_, e = f.s.DeriveAmong(ctx, enrichers.KindLinkFacts, ids, true)
	contractOK(t, e)
	check()
	_, e = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: a, Kind: sources.WebsiteCheck, Inputs: "owner.test", Source: "owner.test", Found: []byte(`{"state":"https","url":"https://owner.test","status_code":200}`)}})
	contractOK(t, e)
	check()
	_, e = f.s.FillFacts(ctx, "", 500)
	contractOK(t, e)
	check()
	parks := seedParks(t, f)
	pass := walkPass(t, f)
	walkSweep(t, f, pass, parks[0], walkFound("search-place", "Walk Found Business", "", ""))
	places, e := f.s.ParkPlaces(ctx, parks[0].ID)
	contractOK(t, e)
	for _, place := range places {
		if !slices.Contains(ids, place.Candidate) {
			ids = append(ids, place.Candidate)
		}
	}
	check()
	f.reopen()
	check()
}

// R-EIT4-6RL9
func TestSearchUsesFinalNamesOfEachWriteUnit(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	var id string
	for batch, pair := range [][2]string{{"Zebra Freight", "Orchid Supply"}, {"Masonry Union", "Copper Welding"}} {
		observations := make([]store.Observation, 0, 2)
		for _, name := range pair {
			o := contractObservation("batch-source", name, f.now)
			o.Digest = name
			o.Reading.PhoneKey, o.Reading.DomainKey = "", ""
			o.Reading.Names = []string{name}
			observations = append(observations, o)
		}
		_, ids, err := f.s.Attach(ctx, "batch-run", false, observations)
		contractOK(t, err)
		contractEqual(t, len(ids), 1)
		if batch == 0 {
			id = ids[0]
		} else {
			contractEqual(t, ids[0], id)
		}
		current := searchCandidates(t, f.s, ids)
		contractEqual(t, current[0].Name, pair[1])
		assertSearchRanking(t, f.s, current, pair[1], 50)
		gone, err := f.s.Search(ctx, pair[0], 50)
		contractOK(t, err)
		contractEqual(t, len(gone), 0)
	}
	f.reopen()
	current := searchCandidates(t, f.s, []string{id})
	assertSearchRanking(t, f.s, current, "Copper Welding", 50)
}

// R-9RWG-SWE5 R-EIT4-6RL9 R-7EEZ-DQOC
func TestSearchNULNames(t *testing.T) {
	for _, name := range []string{"A\x00B", "A\x00\x00B"} {
		t.Run(name, func(t *testing.T) {
			f := contractDB(t)
			id := searchAttach(t, f, "nul", name)
			for _, q := range []string{name, strings.ReplaceAll(name, "\x00", " "), "\x00 " + name + " \x00"} {
				got, err := f.s.Search(t.Context(), q, store.SearchMaxLimit)
				contractOK(t, err)
				contractEqual(t, len(got), 1)
				contractEqual(t, got[0].Candidate.ID, id)
				contractEqual(t, got[0].Matched, name)
				contractEqual(t, got[0].Score, 1.0)
			}
			f.reopen()
			assertSearchRanking(t, f.s, searchCandidates(t, f.s, []string{id}), name, store.SearchMaxLimit)
		})
	}
	t.Run("alternate", func(t *testing.T) {
		f := contractDB(t)
		id := searchAttach(t, f, "alternate", "Other Company", "A\x00B", "A B", "A\x00B", "")
		for _, q := range []string{"A\x00B", "a b"} {
			got, err := f.s.Search(t.Context(), q, store.SearchMaxLimit)
			contractOK(t, err)
			contractEqual(t, len(got), 1)
			contractEqual(t, got[0].Candidate.ID, id)
			contractEqual(t, got[0].Matched, "A\x00B")
			contractEqual(t, got[0].Score, 1.0)
		}
	})
	f := contractDB(t)
	for _, q := range []string{"\x00", "\x00 ", "\t\x00\n"} {
		got, err := f.s.Search(t.Context(), q, store.SearchDefaultLimit)
		contractError(t, err, store.ErrInvalid)
		if got != nil {
			t.Fatal("blank NUL query returned a slice")
		}
	}
}

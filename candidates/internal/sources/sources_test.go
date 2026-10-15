package sources_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-20MD-CXLM
func TestRecordShapes(t *testing.T) {
	// Conversions use the records' complete underlying shapes, including order.
	type personShape struct{ Name, Role string }
	type exclusionShape struct{ Reason, Detail string }
	type estimateShape struct {
		Source     string
		Low        int64
		High       *int64
		Confidence float64
		Facts      map[string]json.RawMessage
		Filed      time.Time
		Formula    int64
	}
	type readingShape struct {
		NameKey, PhoneKey, DomainKey, Name             string
		Names                                          []string
		Street, City, State, Zip, County, Phone, Email string
		People                                         []sources.Person
		Class                                          string
		Cargo                                          []string
		Filed                                          time.Time
		Fleet                                          bool
		Estimate                                       *sources.Estimate
		Exclusions                                     []sources.Exclusion
	}
	if personShape(sources.Person{}) != (personShape{}) || exclusionShape(sources.Exclusion{}) != (exclusionShape{}) {
		t.Fatal("zero record")
	}
	if !reflect.DeepEqual(estimateShape(sources.Estimate{}), estimateShape{}) || !reflect.DeepEqual(readingShape(sources.Reading{}), readingShape{}) {
		t.Fatal("zero record")
	}
}

type fake struct{ name string }

func (f *fake) Name() string                   { return f.name }
func (*fake) Read(scrapes.Row) sources.Reading { return sources.Reading{} }

// R-FLWK-VBKE R-2326-4H30
func TestRegistry(t *testing.T) {
	a, b, c := &fake{"z"}, &fake{"a"}, &fake{"z"}
	r := sources.New(a, b, c)
	for range 2 {
		ns := r.Names()
		if !reflect.DeepEqual(ns, []string{"a", "z"}) {
			t.Fatal(ns)
		}
		ns[0] = "changed"
	}
	if got, ok := r.Get("z"); !ok || got != a {
		t.Fatal(got, ok)
	}
	if got, ok := r.Get("none"); ok || got != nil {
		t.Fatal(got, ok)
	}
	if ns := sources.New().Names(); ns == nil || len(ns) != 0 {
		t.Fatal(ns)
	}
}

// R-W9RP-BU4B
func TestNameKey(t *testing.T) {
	for in, want := range map[string]string{"Acme Trucking, Inc.": "ACME TRUCKING", "J.B. Hunt Transport Co Inc": "JB HUNT TRANSPORT", "  smith  &  sons l.l.c. ": "SMITH SONS", "COMPANY": "COMPANY", "INC LLC": "INC", "CO-OP SERVICES": "COOP SERVICES", "Peña Hauling LLC": "PEÑA HAULING", "": "", "...": "", "A LTD CORP COMPANY CO INC LLC": "A", "hauling\t\n42": "HAULING 42"} {
		if got := sources.NameKey(in); got != want {
			t.Errorf("%q: %q != %q", in, got, want)
		}
	}
}

// R-G2Z6-83Y4
func TestPhoneKey(t *testing.T) {
	for in, want := range map[string]string{"(320) 555-0134": "3205550134", "+1 320.555.0134": "3205550134", "555-0134": "5550134", "": "", "none": "", "１２3٤5": "35", "123456789012345": "6789012345"} {
		if got := sources.PhoneKey(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}

// R-G472-LVOT
func TestDomainKey(t *testing.T) {
	for in, want := range map[string]string{"Dispatch@AcmeTrucking.COM ": "acmetrucking.com", "a@b@acme.net": "acme.net", "nobody": "", "a@ ": "", "@X.COM": "x.com"} {
		if got := sources.DomainKey(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	for _, d := range []string{"aol.com", "arvig.net", "att.net", "bellsouth.net", "centurylink.net", "centurytel.net", "charter.net", "comcast.net", "earthlink.net", "embarqmail.com", "frontier.com", "frontiernet.net", "gmail.com", "gmx.com", "googlemail.com", "hotmail.com", "hughes.net", "icloud.com", "live.com", "mac.com", "mail.com", "mchsi.com", "me.com", "midco.net", "msn.com", "outlook.com", "protonmail.com", "q.com", "qwest.net", "rocketmail.com", "sbcglobal.net", "tds.net", "usfamily.net", "verizon.net", "windstream.net", "yahoo.com", "ymail.com"} {
		if got := sources.DomainKey("x@" + d); got != "" {
			t.Error(d, got)
		}
	}
}

// R-G5EY-ZNFI
func TestCountySourceKey(t *testing.T) {
	for _, v := range [][3]string{{" MN ", " 115 ", "MN-115"}, {"", "115", ""}, {"MN", " \t", ""}, {" mn ", " 001 ", "mn-001"}} {
		if got := sources.County(v[0], v[1]); got != v[2] {
			t.Fatal(got)
		}
	}
	if got := sources.SourceKey("a:b", "c:d"); got != "a:b:c:d" {
		t.Fatal(got)
	}
	if sources.SourceKey("", "") != ":" {
		t.Fatal("empty key")
	}
}

// R-OW1D-D01A
func TestConstants(t *testing.T) {
	const (
		private         = sources.ClassPrivate
		forHire         = sources.ClassForHire
		mixed           = sources.ClassMixed
		unknown         = sources.ClassUnknown
		governmentClass = sources.ExcludeGovernmentClass
		governmentName  = sources.ExcludeGovernmentName
		ruling          = sources.ExcludeRuling
		// A typed integer cannot initialize this float64 without conversion;
		// the remainder also requires Rules to be an integer constant.
		rules  float64 = sources.Rules
		parity         = sources.Rules % 2
	)
	if got, want := []string{private, forHire, mixed, unknown, governmentClass, governmentName, ruling}, []string{"private", "for_hire", "mixed", "unknown", "government_class", "government_name", "ruling"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("constants = %q, want %q", got, want)
	}
	if rules != 2 || parity != 0 {
		t.Fatalf("rules = %v, parity = %v", rules, parity)
	}
}

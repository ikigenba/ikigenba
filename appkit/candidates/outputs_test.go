package candidates_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/candidates"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// R-DYF4-XIH9 R-P4O8-Q4M5 R-Q2TF-FPDL R-OUX1-NYOL R-OW4Y-1QFA R-OXCU-FI5Z R-OYKQ-T9WO R-OZSN-71ND R-P10J-KTE2 R-P3GC-CCVG
func TestPublicTypes(t *testing.T) {
	w, _ := writer(t)
	constructors := []func(candidates.ClientConfig) *candidates.Client{candidates.NewClient}
	cfg := candidates.ClientConfig{services.Entry{}, w}
	var config struct {
		Service   services.Entry
		Telemetry *telemetry.Writer
	} = cfg
	c := constructors[0](cfg)
	members := []func(context.Context, string) ([]candidates.Member, error){c.Members, c.ProspectList}
	candidate := []func(context.Context, string) (candidates.Business, error){c.Candidate}
	person := candidates.Person{"person", "role"}
	var ps struct{ Name, Role string } = person
	business := candidates.Business{"id", "status", "merged", "name", []string{"name"}, "street", "city", "state", "zip", "county", "phone", "email", []candidates.Person{person}}
	var bs struct {
		ID, Status, MergedInto, Name                   string
		Names                                          []string
		Street, City, State, Zip, County, Phone, Email string
		People                                         []candidates.Person
	} = business
	member := candidates.Member{business, true}
	var ms struct {
		Business candidates.Business
		Excluded bool
	} = member
	he := candidates.HTTPError{599, "body"}
	var hs struct {
		StatusCode int
		Body       string
	} = he
	if hs.StatusCode != 599 || hs.Body != "body" || he.Error() == "" {
		t.Fatal("HTTPError fields")
	}
	ref := candidates.Refusal{"text"}
	var rs struct{ Text string } = ref
	if config.Telemetry != w || c == nil || members[0] == nil || members[1] == nil || candidate[0] == nil || candidates.Person(ps) != person || !reflect.DeepEqual(candidates.Business(bs), business) || !reflect.DeepEqual(candidates.Member(ms), member) || rs.Text != "text" || ref.Error() == "" {
		t.Fatal("public types do not preserve their fields")
	}
}

// R-P28F-YL4R R-PO6M-UGH9
func TestErrorValues(t *testing.T) {
	var sentinels = []error{candidates.ErrNotFound, candidates.ErrUnavailable, candidates.ErrStopping}
	for i, err := range sentinels {
		if err == nil || !strings.HasPrefix(err.Error(), "candidates: ") {
			t.Fatal(err)
		}
		for j, other := range sentinels {
			if errors.Is(err, other) != (i == j) {
				t.Fatalf("sentinel overlap: %v %v", err, other)
			}
		}
	}
	for _, text := range []string{"", "sample\n日本", "not_found: arbitrary"} {
		if (&candidates.Refusal{Text: text}).Error() != "candidates: "+text {
			t.Fatal(text)
		}
	}
}

// R-PFNC-62AE R-PGV8-JU13 R-PI34-XLRS R-PJB1-BDIH
func TestSuccess(t *testing.T) {
	minimal := candidates.Business{ID: "id", Status: "active", Name: "business", Names: []string{}, People: []candidates.Person{}}
	full := candidates.Business{
		ID: "id\n日本", Status: "merged", MergedInto: "survivor", Name: "primary",
		Names: []string{"alias", "alias", "", "日本"}, Street: "street", City: "city", State: "state", Zip: "zip", County: "county", Phone: "phone", Email: "email",
		People: []candidates.Person{{Name: "first", Role: "owner"}, {}, {Name: "last"}, {Role: "other"}},
	}
	fullJSON := `{"id":null,"id":"id\n日本","status":"merged","merged_into":"survivor","name":"primary","names":["alias","alias","","日本"],"street":"street","city":"city","state":"state","zip":"zip","county":"county","phone":"phone","email":"email","people":[{"name":"first","role":"owner","future":1},{},{"name":"last"},{"role":"other"}],"ID":null,"STATUS":[],"Names":false,"future":{"data":null}}`
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, op := range []string{"cohort", "candidate", "prospect_list"} {
			for _, fixture := range []struct {
				body string
				want candidates.Business
			}{
				{minimalBusiness, minimal}, {fullJSON, full},
				{`{"id":"","status":"","name":"","names":[],"people":[]}`, candidates.Business{Names: []string{}, People: []candidates.Person{}}},
			} {
				body := `{"candidate":` + fixture.body + `,"future":null}`
				var want any = fixture.want
				if op != "candidate" {
					body = `{"members":[{"candidate":` + fixture.body + `,"excluded":true,"future":7},{"candidate":` + minimalBusiness + `},{"candidate":` + fixture.body + `,"excluded":false}],"rejects":null,"matching":"ignored","classified":false}`
					want = []candidates.Member{{Business: fixture.want, Excluded: true}, {Business: minimal}, {Business: fixture.want}}
				}
				// Last duplicate output member counts and non-true isError values are successes.
				for _, isError := range []string{"false", "null", "1", `"true"`} {
					bodyWithDuplicate := `{"candidate":null,"members":null,` + strings.TrimPrefix(body, "{")
					result := `{"resultType":"complete","isError":` + isError + `,"structuredContent":` + bodyWithDuplicate + `,"content":[{"type":"text","text":"invalid fallback"}],"future":true}`
					got, err := transportCall(context.Background(), t, answer(t, 200, media, result), op, "id")
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("%s %s: got %#v want %#v: %v", media, op, got, want, err)
					}
				}
			}
		}
	}
	got, err := answer(t, 200, "application/json", `{"structuredContent":{"members":[]}}`).Members(context.Background(), "")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty members: %#v %v", got, err)
	}
}

func mutate(t *testing.T, body, key string, value json.RawMessage, remove bool) string {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &fields); err != nil {
		t.Fatal(err)
	}
	if remove {
		delete(fields, key)
	} else {
		fields[key] = value
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// R-PPEJ-887Y R-PFNC-62AE R-PGV8-JU13
func TestMalformedOutputs(t *testing.T) {
	invalidCandidates := []string{"null", "[]", "true", "1", `"text"`, "{}"}
	required := []string{"id", "status", "name"}
	optional := []string{"merged_into", "street", "city", "state", "zip", "county", "phone", "email"}
	for _, key := range required {
		missing := mutate(t, minimalBusiness, key, nil, true)
		invalidCandidates = append(invalidCandidates, missing, mutate(t, missing, strings.ToUpper(key), json.RawMessage(`"wrong case"`), false))
	}
	for _, key := range append(required, optional...) {
		for _, value := range []string{"null", "true", "[]", "{}", "1"} {
			invalidCandidates = append(invalidCandidates, mutate(t, minimalBusiness, key, json.RawMessage(value), false))
		}
	}
	for _, key := range []string{"names", "people"} {
		for _, value := range []string{"null", "true", "{}", "1", `"text"`, "[null]", "[1]", "[true]", "[[]]"} {
			invalidCandidates = append(invalidCandidates, mutate(t, minimalBusiness, key, json.RawMessage(value), false))
		}
	}
	invalidCandidates = append(invalidCandidates,
		mutate(t, minimalBusiness, "names", json.RawMessage(`["ok",{}]`), false),
		mutate(t, minimalBusiness, "people", json.RawMessage(`[{"name":null}]`), false),
		mutate(t, minimalBusiness, "people", json.RawMessage(`[{"role":1}]`), false),
		strings.TrimSuffix(minimalBusiness, "}")+`,"id":null}`,
	)
	for _, op := range []string{"cohort", "candidate", "prospect_list"} {
		results := []string{`{}`, `{"structuredContent":null}`, `{"structuredContent":[]}`, `{"structuredContent":1}`, `{"structuredContent":true}`, `{"structuredContent":"text"}`,
			`{"structuredContent":{},"content":[{"type":"text","text":` + fmt.Sprintf("%q", output(op)) + `}]}`,
			`{"isError":true,"isError":true}`,
			`{"structuredContent":` + output(op) + `,"structuredContent":` + output(op) + `}`,
		}
		for _, candidate := range invalidCandidates {
			body := `{"candidate":` + candidate + `}`
			if op != "candidate" {
				// A late invalid member must not leak the earlier partial result.
				body = `{"members":[{"candidate":` + minimalBusiness + `},{"candidate":` + candidate + `}]}`
			}
			results = append(results, `{"structuredContent":`+body+`}`)
		}
		if op != "candidate" {
			for _, bad := range []string{"null", "{}", "true", "1", `"text"`, "[null]", "[[]]", "[1]", "[{}]", `[{"Candidate":` + minimalBusiness + `}]`} {
				results = append(results, `{"structuredContent":{"members":`+bad+`}}`)
			}
			for _, bad := range []string{"null", "1", `"false"`, "[]", "{}"} {
				results = append(results, `{"structuredContent":{"members":[{"candidate":`+minimalBusiness+`,"excluded":`+bad+`}]}}`)
			}
		} else {
			results = append(results, `{"structuredContent":{"Candidate":`+minimalBusiness+`}}`)
		}
		for i, result := range results {
			t.Run(fmt.Sprintf("%s/%d", op, i), func(t *testing.T) {
				_, err := transportCall(context.Background(), t, answer(t, 200, "application/json", result), op, "id")
				malformed(t, err)
			})
		}
	}
}

// R-PKIX-P596 R-PLQU-2WZV R-PMYQ-GOQK
func TestRefusals(t *testing.T) {
	fixtures := []struct{ result, text string }{
		{`{"isError":true}`, ""},
		{`{"isError":true,"content":null}`, ""},
		{`{"isError":true,"content":{}}`, ""},
		{`{"isError":true,"content":[{"type":"text","text":"first"},{"type":"image","text":"ignore"},{"type":"text","text":null},null,1,[],{"Type":"text","text":"ignore"},{"type":"text","Text":"ignore"},{"type":"text","text":""},{"type":"text","text":"last\nline"}],"structuredContent":{}}`, "first\n\nlast\nline"},
		{`{"isError":true,"content":[{"type":"text","text":"old","text":"new"},{"type":"image","type":"text","text":"second"}]}`, "new\nsecond"},
	}
	for _, text := range []string{"not_found: missing", "unavailable: retry", "stopping: stop", "invalid: bad", "busy: later", "unknown: future", "not_found: ", "not_found", "not_found:", " not_found: bad", "NOT_FOUND: bad", "prefix\nnot_found: bad", "stopping: stop\nextra"} {
		raw, _ := json.Marshal(text)
		fixtures = append(fixtures, struct{ result, text string }{`{"isError":true,"content":[{"type":"text","text":` + string(raw) + `}],"structuredContent":null}`, text})
	}
	for _, media := range []string{"application/json", "text/event-stream"} {
		for _, op := range []string{"cohort", "candidate", "prospect_list"} {
			for _, f := range fixtures {
				_, err := transportCall(context.Background(), t, answer(t, 200, media, f.result), op, "id")
				var want error
				for prefix, sentinel := range map[string]error{"not_found: ": candidates.ErrNotFound, "unavailable: ": candidates.ErrUnavailable, "stopping: ": candidates.ErrStopping} {
					if strings.HasPrefix(f.text, prefix) {
						want = sentinel
					}
				}
				var ref *candidates.Refusal
				var he *candidates.HTTPError
				if err == nil || errors.As(err, &he) {
					t.Fatal(err)
				}
				if want == nil {
					if sentinel(err) || !errors.As(err, &ref) || ref.Text != f.text {
						t.Fatalf("refusal %q: %v", f.text, err)
					}
				} else {
					if errors.As(err, &ref) || !errors.Is(err, want) {
						t.Fatalf("sentinel %q: %v", f.text, err)
					}
					for _, other := range []error{candidates.ErrNotFound, candidates.ErrUnavailable, candidates.ErrStopping} {
						if errors.Is(err, other) != errors.Is(other, want) {
							t.Fatal(err, other)
						}
					}
				}
			}
		}
	}
}

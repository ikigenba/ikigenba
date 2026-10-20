package source_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

type fake struct{}

func (fake) Name() string { return "test" }
func (fake) Scrape(_ context.Context, batch func([]source.Row) error) error {
	return batch([]source.Row{})
}

// R-PVE4-VETH R-0OXS-TK1T R-P0WN-B5A4
func TestRowJSON(t *testing.T) {
	var sc source.Scraper = fake{}
	if sc.Name() != "test" || sc.Scrape(context.Background(), func([]source.Row) error { return nil }) != nil || source.Called != "source.called" {
		t.Fatal("interface")
	}
	lat, lon := 1.25, -2.5
	r := source.Row{Scraper: "test", Run: "run_test", SourceID: "17", Scraped: time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("east", 3600)), Digest: "digest", Name: "Test", Names: []string{"Test", "Other"}, Address: source.Address{Street: "Street", City: "City", State: "ST", Zip: "12345", County: "001"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "Test LLC", Role: "party"}}, Facts: json.RawMessage(`{"value":"001"}`), Raw: json.RawMessage(`{"dot_number":"17"}`)}
	got, err := r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"scraper":"test","run":"run_test","source_id":"17","scraped":"2024-01-02T02:04:05Z","digest":"digest","name":"Test","names":["Test","Other"],"address":{"street":"Street","city":"City","state":"ST","zip":"12345","county":"001"},"lat":1.25,"lon":-2.5,"people":[{"name":"Test LLC","role":"party"}],"facts":{"value":"001"},"raw":{"dot_number":"17"}}`
	if string(got) != want {
		t.Fatalf("%s", got)
	}
	got, err = json.Marshal(source.Row{})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"scraper":"","run":"","source_id":"","scraped":"0001-01-01T00:00:00Z","digest":"","name":"","names":[],"address":{},"people":[],"facts":{}}`
	if string(got) != want {
		t.Fatalf("empty: %s", got)
	}
	r = source.Row{Names: []string{}, People: []source.Person{}, Facts: json.RawMessage{}, Address: source.Address{City: "City"}, Lon: &lon}
	got, err = r.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err = json.Unmarshal(got, &obj); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(obj["address"], map[string]any{"city": "City"}) || obj["lon"] != lon || obj["lat"] != nil {
		t.Fatal(obj)
	}
}

// R-0RDL-L3J7
func TestNames(t *testing.T) {
	for _, tc := range []struct{ in, out []string }{{[]string{" \tACME INC\n", "", "ACME INC", "ACME", "acme"}, []string{"ACME INC", "ACME", "acme"}}, {[]string{"", " \t"}, []string{}}, {nil, []string{}}} {
		got := source.Names(tc.in...)
		if !reflect.DeepEqual(got, tc.out) {
			t.Fatal(got)
		}
	}
}

// R-0SLH-YV9W
func TestTitleCase(t *testing.T) {
	for in, want := range map[string]string{"NORTH ST. PAUL": "North St. Paul", "o'neill": "O'Neill", "3RD AVE": "3Rd Ave", "MC-GREGOR": "Mc-Gregor", "éCOLE ΩMEGA": "École Ωmega", "a\u0301B": "A\u0301B", "": ""} {
		if got := source.TitleCase(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}

// R-0TTE-CN0L
func TestAddress(t *testing.T) {
	if got := source.NewAddress(" Test Road ", "TEST CITY", " mn ", " 12345-6789 ", " 027 "); got != (source.Address{Street: "Test Road", City: "Test City", State: "MN", Zip: "12345", County: "027"}) {
		t.Fatal(got)
	}
	if got := source.NewAddress(" ", "\t", "", " ", "\n"); got != (source.Address{}) {
		t.Fatal(got)
	}
	if got := source.NewAddress("", "", "", " é一二三四五 ", ""); got.Zip != "é一二三四" {
		t.Fatal(got)
	}
	if source.NewAddress("", "", "", "abc", "").Zip != "abc" {
		t.Fatal("short zip")
	}
}

// R-0W97-46HZ
func TestDigest(t *testing.T) {
	if source.Digest([]byte("{}")) != "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatal("digest")
	}
	if source.Digest(nil) != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" || source.Digest([]byte("{ }")) == source.Digest([]byte("{}")) {
		t.Fatal("raw digest")
	}
}

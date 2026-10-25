package sources_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-0S77-3ZKD R-0TF3-HRB2
func TestReadingNameKeys(t *testing.T) {
	for _, s := range []sources.Source{fmcsa.New(), ppp.New()} {
		for _, names := range [][]string{nil, {"Acme Trucking LLC", "Acme Freight"}, {"Acme Trucking LLC", "Acme Trucking, LLC"}} {
			r := scrapes.Row{Names: names, Facts: map[string]json.RawMessage{"ein": json.RawMessage(`"411234567"`)}}
			got := s.Read(r)
			if got.EINKey != "" || !reflect.DeepEqual(got.NameKeys, sources.NameKeys(names)) {
				t.Fatal(got)
			}
			if len(got.NameKeys) > 0 {
				got.NameKeys[0] = "changed"
				if reflect.DeepEqual(got.NameKeys, s.Read(r).NameKeys) {
					t.Fatal("shared keys")
				}
			}
		}
	}
}

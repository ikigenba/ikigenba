package pages

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-F2ZL-GQNA R-IYP1-PJL4
func TestLinkEINAndNames(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	links := []LinkView{}
	for _, reading := range []sources.Reading{
		{EINKey: "410123456", Names: []string{"chosen-first-business", "chosen-second-business"}},
		{Names: []string{"chosen-only-business"}},
		{},
	} {
		view := linkView(store.Link{Reading: reading}, "")
		if view.EIN != reading.EINKey || view.Names == nil || !reflect.DeepEqual(view.Names, append([]string{}, reading.Names...)) {
			t.Fatal(view)
		}
		links = append(links, view)
	}
	var out bytes.Buffer
	if err = set.templates.ExecuteTemplate(&out, "candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Links: links}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"410123456", "chosen-first-business", "chosen-second-business", "chosen-only-business"} {
		if !strings.Contains(out.String(), value) {
			t.Fatalf("missing supplied value %q", value)
		}
	}
}

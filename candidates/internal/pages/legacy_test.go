package pages

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-0M4U-ZMZ1 R-693O-ZJ4P
func TestLegacyListPathRedirectsWithoutStoreRead(t *testing.T) {
	cfg, db := pageFixture(t)
	list, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "redirect-list", Size: store.ProspectListSizeDefault, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	held := strings.Replace(list.ID, store.ProspectListIDPrefix, store.LegacyListIDPrefix, 1)
	for _, failing := range []bool{false, true} {
		db.SetFailing(failing)
		for _, id := range []string{held, "coh_0000000000000000"} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := requestPage(cfg, method, "/cohorts/"+id+"?ignored=yes", "reader")
				if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/prospect-lists/"+store.MapListID(id) || w.Body.Len() != 0 {
					t.Fatal(w)
				}
			}
		}
		for _, id := range []string{list.ID, "coh_0123456789ABCDEF", "coh_000000000000000", "coh_00000000000000000", "coh_0000000000000000/"} {
			w := requestPage(cfg, http.MethodGet, "/cohorts/"+id, "reader")
			if w.Code != http.StatusNotFound {
				t.Fatal(id, w)
			}
		}
	}
}

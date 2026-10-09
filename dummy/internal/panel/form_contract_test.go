package panel_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-K5GV-9F5J R-HTHJ-BDCY
func TestFormMediaClassificationAndValidationDecision(t *testing.T) {
	for _, media := range []string{"application/x-www-form-urlencoded", " APPLICATION/X-WWW-FORM-URLENCODED \t; charset=UTF-8", "\u2003application/x-www-form-urlencoded\u2003;ignored;more", "application/x-www-form-urlencoded; invalid parameter", "", "application/json", "application/x-www-form-urlencoded-extra", ";application/x-www-form-urlencoded", "application/x-www-form-urlencoded, text/plain"} {
		for _, sub := range []widget.Submission{{Name: "fresh", Count: "1", Status: "active"}, {Name: "", Count: "bad", Status: "archived"}, {Name: "fresh", Count: "bad", Status: "active"}} {
			t.Run(media+formBody(sub), func(t *testing.T) {
				mediaPrefix, _, _ := strings.Cut(media, ";")
				want := http.StatusUnsupportedMediaType
				if strings.EqualFold(strings.TrimSpace(mediaPrefix), "application/x-www-form-urlencoded") {
					_, errs := formTestCreate(t, panelTestStore(t), sub)
					want = http.StatusSeeOther
					if errs.Any() {
						want = http.StatusUnprocessableEntity
					}
				}
				w := formRequest(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), http.MethodPost, "/widgets", media, formBody(sub))
				if w.Code != want {
					t.Errorf("status=%d, want %d", w.Code, want)
				}
			})
		}
	}
}

// R-HOLX-SAE6
func TestFormRefusalsPreserveWholeStoreSequence(t *testing.T) {
	for _, media := range []string{"application/json", "application/x-www-form-urlencoded"} {
		for _, seeded := range []bool{false, true} {
			t.Run(fmt.Sprint(media, seeded), func(t *testing.T) {
				store := panelTestStore(t)
				if seeded {
					for _, sub := range []widget.Submission{{Name: "z-last", Count: "6", Status: "retired"}, {Name: "a-first", Count: "0", Status: "paused"}} {
						if _, errs := formTestCreate(t, store, sub); errs.Any() {
							t.Fatal(errs)
						}
					}
				}
				before := panelStoreAll(t, store)
				w := formRequest(coreHandler(t, store, pageTestBanner, io.Discard), http.MethodPost, "/widgets", media, formBody(widget.Submission{Name: "z-last", Count: "bad", Status: "archived"}))
				if w.Code != http.StatusUnsupportedMediaType && w.Code != http.StatusUnprocessableEntity {
					t.Fatalf("fixture did not produce refusal: %d", w.Code)
				}
				if got := panelStoreAll(t, store); !slices.Equal(got, before) {
					t.Errorf("store=%v, want unchanged %v", got, before)
				}
			})
		}
	}
}

// R-KNRC-ZZ9Y R-UBKY-GUDK
func TestFormUnsupportedMediaFailureContract(t *testing.T) {
	for _, media := range []string{"", "application/json", "multipart/form-data; boundary=a", "text/plain", "application/x-www-form-urlencoded-extra", ";application/x-www-form-urlencoded"} {
		t.Run(media, func(t *testing.T) {
			body := &formObservedBody{reader: strings.NewReader("name=valid&count=1&status=active")}
			r := httptest.NewRequest(http.MethodPost, "/widgets", body)
			r.Header.Set("X-User-Id", "form-user")
			r.Header.Set("X-User-Email", "form-user@example.test")
			if media != "" {
				r.Header.Set("Content-Type", media)
			}
			w := pageTestResponse(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), r)
			if w.Code != http.StatusUnsupportedMediaType || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("unsupported response=%d %v", w.Code, w.Header())
			}
			if body.reads != 0 {
				t.Errorf("unsupported body read %d times", body.reads)
			}
			pageTestFailure(t, w, r, panel.UnsupportedMediaTypeMessage)
		})
	}
}

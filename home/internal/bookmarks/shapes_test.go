package bookmarks

import (
	"bytes"
	"testing"
	"time"
)

// R-BG98-IMV7 R-BIP1-A6CL R-BJWX-NY3A R-BMCQ-FHKO R-BNKM-T9BD R-BOSJ-7122
func TestPositionalValues(t *testing.T) {
	now := func() time.Time { return time.Time{} }
	cfg := Config{now, bytes.NewReader(nil)}
	ic := Icon{"test/type", []byte{3}}
	draft := Draft{"name", "https://x/", "user", ic}
	submission := Submission{draft.Name, draft.URL}
	fields := FieldErrors{"name-error", "url-error"}
	b := Bookmark{"id", draft.Name, draft.URL, draft.CreatedBy, len(draft.Icon.Data) > 0, cfg.Now()}
	if submission.Name != b.Name || submission.URL != b.URL || ic.Type != draft.Icon.Type || cfg.Rand == nil || fields.Name != "name-error" || fields.URL != "url-error" || !b.HasIcon || b.ID != "id" || b.CreatedBy != "user" || !b.Created.IsZero() {
		t.Fatal("positional value mismatch")
	}
}

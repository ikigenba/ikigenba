package tools_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	root "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
)

type typedRandom struct{ n byte }

func (r *typedRandom) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = r.n
		r.n++
	}
	return len(b), nil
}
func typedRecords(t *testing.T) (*store.Store, *db.DB, *mcp.Client, *time.Time) {
	t.Helper()
	t.Setenv(services.Variable, "")
	at := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state", "gmail.db"), Migrations: root.Migrations(), Now: func() time.Time { return at }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	st := store.New(d, store.Config{Now: func() time.Time { return at }, Rand: &typedRandom{}})
	s := typedServer(t)
	tools.Register(s, tools.Config{Store: st})
	return st, d, typedClient(t, s), &at
}
func typedDecode(t *testing.T, b json.RawMessage, v any) {
	t.Helper()
	if e := json.Unmarshal(b, v); e != nil {
		t.Fatal(e)
	}
}
func typedRefusal(t *testing.T, c *mcp.Client, name string, a any, want string) {
	t.Helper()
	b, e := json.Marshal(a)
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.CallTool(context.Background(), identity.Caller{UserID: "owner"}, name, b)
	if e != nil {
		t.Fatal(e)
	}
	if !r.IsError() {
		t.Fatal("not refused")
	}
	wire, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Content    []struct{ Type, Text string }
		Structured json.RawMessage `json:"structuredContent"`
	}
	typedDecode(t, wire, &v)
	if len(v.Content) != 1 || v.Content[0].Type != "text" || v.Content[0].Text != want || v.Structured != nil {
		t.Fatalf("refusal %s", wire)
	}
}

// R-OQ70-NLLW R-N1FE-FY8Z
func TestTypedStoredDrafts(t *testing.T) {
	st, d, c, at := typedRecords(t)
	ctx := context.Background()
	var empty tools.DraftList
	typedDecode(t, typedCall(t, c, "drafts", tools.NoArgs{}), &empty)
	typedEqual(t, empty.Drafts, []tools.Draft{})
	first, e := st.AddDraft(ctx, store.Draft{GmailDraft: "remote", Message: "message", Thread: "thread", To: []string{"to"}, Body: "private", Subject: "subject", Link: "link", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	*at = at.Add(time.Minute)
	second, e := st.AddDraft(ctx, store.Draft{GmailDraft: "second", Message: "second-message", Body: "second-private", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = st.AddDraft(ctx, store.Draft{GmailDraft: "other", Message: "other-message", UserID: "other"}); e != nil {
		t.Fatal(e)
	}
	var v tools.DraftList
	typedDecode(t, typedCall(t, c, "drafts", tools.NoArgs{}), &v)
	typedEqual(t, v.Drafts, []tools.Draft{tools.DraftOf(second, false), tools.DraftOf(first, false)})
	if v.Drafts[1].Status != store.DraftOpen || v.Drafts[1].Draft == nil || v.Drafts[1].Link == nil {
		t.Fatal(v)
	}
	d.SetFailing(true)
	typedRefusal(t, c, "drafts", tools.NoArgs{}, "unavailable: "+tools.RefuseUnavailable)
}

// R-ZOWT-XM78 R-ZQ4Q-BDXX R-ZRCM-P5OM
func TestTypedStoredTransfers(t *testing.T) {
	st, d, c, at := typedRecords(t)
	ctx := context.Background()
	var empty tools.TransferList
	typedDecode(t, typedCall(t, c, "transfers", tools.NoArgs{}), &empty)
	typedEqual(t, empty.Transfers, []tools.Record{})
	first, e := st.AddTransfer(ctx, store.Transfer{Direction: store.DirectionPull, Status: store.StatusQueued, Message: "message", Path: "path", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	*at = at.Add(time.Minute)
	second, e := st.AddTransfer(ctx, store.Transfer{Direction: store.DirectionPush, Status: store.StatusQueued, Draft: "draft", Path: "second-path", Message: "second-message", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	other, e := st.AddTransfer(ctx, store.Transfer{Direction: store.DirectionPull, Status: store.StatusQueued, Path: "other-path", Message: "other-message", UserID: "other"})
	if e != nil {
		t.Fatal(e)
	}
	var v tools.TransferList
	typedDecode(t, typedCall(t, c, "transfers", tools.NoArgs{}), &v)
	typedEqual(t, v.Transfers, []tools.Record{tools.RecordOf(second), tools.RecordOf(first)})
	var single tools.Record
	typedDecode(t, typedCall(t, c, "transfer", tools.TransferArgs{Transfer: first.ID}), &single)
	typedEqual(t, single, tools.RecordOf(first))
	unknown := store.TransferIDPrefix + "ffffffffffffffff"
	if !store.ValidTransferID(unknown) {
		t.Fatal("fixture id")
	}
	for _, id := range []string{"malformed", unknown, other.ID} {
		typedRefusal(t, c, "transfer", tools.TransferArgs{Transfer: id}, "not_found: "+tools.RefuseNotFound)
	}
	if _, e = st.StartTransfer(ctx, first.ID); e != nil {
		t.Fatal(e)
	}
	ended, e := st.FinishTransfer(ctx, first.ID, store.Ending{Status: store.StatusDone})
	if e != nil {
		t.Fatal(e)
	}
	*at = at.Add(48 * time.Hour)
	if _, e = st.PruneTransfers(ctx, *at, 1, 1); e != nil {
		t.Fatal(e)
	}
	typedRefusal(t, c, "transfer", tools.TransferArgs{Transfer: ended.ID}, "not_found: "+tools.RefuseNotFound)
	d.SetFailing(true)
	typedRefusal(t, c, "transfer", tools.TransferArgs{Transfer: "malformed"}, "not_found: "+tools.RefuseNotFound)
	typedRefusal(t, c, "transfers", tools.NoArgs{}, "unavailable: "+tools.RefuseUnavailable)
	typedRefusal(t, c, "transfer", tools.TransferArgs{Transfer: second.ID}, "unavailable: "+tools.RefuseUnavailable)
}

package cli_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
)

func (h *runHarness) campaignDelivery(name, campaign, candidate, prospectList, reason string, at time.Time, request string) []byte {
	h.t.Helper()
	e := events.Event{ID: "evt_1212121212121212", Time: at, Service: "campaigns", Name: name, Attrs: events.Attrs{"campaign": campaign, "candidate": candidate, "prospect_list": prospectList, "reason": reason, "piece": "piece_fixture", "channel": "mail", "how": "hand", "at": at.Add(-24 * time.Hour).UTC().Format(time.RFC3339)}, Seq: 1, Received: h.p.Now()}
	b, err := json.Marshal(e)
	if err != nil {
		h.t.Fatal(err)
	}
	var env map[string]any
	if err = json.Unmarshal(b, &env); err != nil {
		h.t.Fatal(err)
	}
	env["attempt"] = 1
	b, err = json.Marshal(env)
	if err != nil {
		h.t.Fatal(err)
	}
	code, answer := h.request("POST", "/events", request, b)
	var out map[string]string
	want := events.OutcomeOK
	if candidate == "unknown_campaign_candidate" {
		want = events.OutcomeSkip
	}
	if code != 200 || json.Unmarshal(answer, &out) != nil || len(out) != 1 || out["outcome"] != want {
		h.t.Fatal(code, string(answer))
	}
	return answer
}

// R-4290-EY6Y R-0FWV-JLS0
func TestRunCampaignHistoryIsLocal(t *testing.T) {
	var requests atomic.Int64
	socketDir, err := os.MkdirTemp("", "cand-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "campaign")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }), ConnState: func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			requests.Add(1)
		}
	}}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	dir := t.TempDir()
	h := newRunHarness(t, dir, runServices(t, map[string]string{"campaigns": socket}))
	d, st := runStore(t, dir, h.p.Now)
	_, ids, err := st.Attach(t.Context(), "campaign_seed", false, []store.Observation{{Scraper: "fmcsa", SourceID: "business", Digest: "business", Reading: sources.Reading{Name: "Supplied Company", NameKey: "supplied company"}}})
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	h.start()
	stamp := h.p.Now().UTC().Truncate(time.Second)
	names := []string{"campaign.member.added", "campaign.member.rejected", "campaign.piece.delivered", "campaign.member.opened"}
	for i, name := range names {
		h.campaignDelivery(name, "campaign_fixture", ids[0], "prospect_list_fixture", "supplied private reason", stamp.Add(time.Duration(i)*time.Hour), fmt.Sprintf("campaign-request-%d", i))
	}
	r, v := h.call(identity.Caller{UserID: "owner", RequestID: "campaign-read"}, "candidate", tools.CandidateArgs{Candidate: ids[0]})
	b, err := json.Marshal(v)
	if err != nil || r.IsError() {
		t.Fatal(r, err)
	}
	var detail tools.Detail
	if err = json.Unmarshal(b, &detail); err != nil || len(detail.Campaigns) != 1 || detail.Campaigns[0].Campaign != "campaign_fixture" {
		t.Fatal(detail, err)
	}
	code, body := h.request("GET", "/candidates/"+ids[0], "campaign-page", nil)
	if code != 200 || !strings.Contains(string(body), urls.CampaignPage(urls.Base("https://campaigns.example.test"), "campaign_fixture")) {
		t.Fatal(code, string(body))
	}
	h.stop()
	if requests.Load() != 0 || len(h.bus.Events()) != 0 {
		t.Fatal("unexpected sibling call or event")
	}
	_, st = runStore(t, dir, h.p.Now)
	uses, err := st.CampaignUses(t.Context(), ids[0])
	if err != nil || len(uses) != 1 {
		t.Fatal(uses, err)
	}
	u := uses[0]
	if u.Campaign != "campaign_fixture" || u.ProspectList != "prospect_list_fixture" || u.Reason != "supplied private reason" || !u.Added.Equal(stamp) || !u.Rejected.Equal(stamp.Add(time.Hour)) || !u.Delivered.Equal(stamp.Add(2*time.Hour)) || !u.Opened.Equal(stamp.Add(-21*time.Hour)) {
		t.Fatal(u)
	}
	for i := range names {
		counts := map[string]int{}
		for _, e := range h.telemetry.Events() {
			if e.RequestID == fmt.Sprintf("campaign-request-%d", i) {
				counts[e.Name]++
			}
		}
		if len(counts) != 2 || counts["request.started"] != 1 || counts["request.finished"] != 1 {
			t.Fatal(counts)
		}
	}
}

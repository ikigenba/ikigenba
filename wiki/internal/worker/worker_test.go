package worker_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	agentkit "github.com/ikigenba/agentkit"

	"wiki/internal/compile"
	"wiki/internal/db"
	"wiki/internal/extract"
	"wiki/internal/llm"
	wikidomain "wiki/internal/wiki"
	"wiki/internal/worker"
)

func TestRunIntegratesPendingJobWithRealDBAndMockProvider(t *testing.T) {
	// R-M9ZJ-LZNK
	// R-MB7F-ZRE9
	// R-MCFC-DJ4Y
	ctx := context.Background()
	conn := migratedDB(t, ctx)
	defer conn.Close()

	prov := &scriptedProvider{responses: []string{
		`{"subjects":[{
			"type":"entity",
			"kind":"company",
			"name":"Acme Robotics",
			"occurred_at":"",
			"claims":["Acme Robotics opened a research lab in Tulsa."]
		}]}`,
		`{"title":"Acme Robotics","body":"Acme Robotics opened a research lab in Tulsa. [job]"}`,
	}}
	client := llm.New(prov, nil)
	svc := wikidomain.NewService(
		conn,
		extract.New(client, llm.CallSite{Model: "extract-model"}),
		compile.New(client, llm.CallSite{Model: "compile-model"}, nil),
		clockAt(time.Date(2026, 6, 20, 20, 33, 0, 0, time.UTC)),
	)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Run(runCtx, svc) }()

	jobID, err := svc.Ingest(ctx, "owner@example.com", "Acme Robotics opened a research lab in Tulsa.", "Tulsa lab", []string{"robotics"})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	status := waitStatus(t, ctx, svc, jobID, wikidomain.JobDone)
	if status.StartedAt == nil || status.FinishedAt == nil {
		t.Fatalf("status = %+v, want started and finished times", status)
	}
	if len(status.Subjects) != 1 {
		t.Fatalf("subjects = %#v, want one produced subject", status.Subjects)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want extract plus compile", len(prov.requests))
	}

	page, err := wikidomain.NewPageStore(conn).Get(ctx, status.Subjects[0])
	if err != nil {
		t.Fatalf("Get page: %v", err)
	}
	if page.Title != "Acme Robotics" || !strings.Contains(page.Body, "Tulsa") {
		t.Fatalf("page = %+v, want compiled page from extracted claim", page)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func migratedDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	conn, err := db.Open(t.TempDir() + "/wiki.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Migrate(ctx, conn); err != nil {
		conn.Close()
		t.Fatalf("Migrate: %v", err)
	}
	return conn
}

func waitStatus(t *testing.T, ctx context.Context, svc *wikidomain.Service, jobID, want string) wikidomain.JobStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last wikidomain.JobStatus
	for time.Now().Before(deadline) {
		status, err := svc.JobStatus(ctx, jobID)
		if err != nil {
			t.Fatalf("JobStatus: %v", err)
		}
		last = status
		if status.Status == want {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job status = %+v, want %s", last, want)
	return wikidomain.JobStatus{}
}

func clockAt(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

type scriptedProvider struct {
	responses []string
	requests  []agentkit.Request
}

func (p *scriptedProvider) RoundTrip(_ context.Context, req *agentkit.Request) *agentkit.RoundTrip {
	p.requests = append(p.requests, cloneRequest(req))
	text := `{"subjects":[]}`
	if len(p.responses) > 0 {
		text = p.responses[0]
		p.responses = p.responses[1:]
	}
	return agentkit.NewRoundTrip(
		agentkit.Message{Role: agentkit.RoleAssistant, Blocks: []agentkit.Block{agentkit.TextBlock{Text: text}}},
		agentkit.FinishStop,
		agentkit.Usage{InputUncached: 1, Output: 1, Total: 2},
		nil,
		nil,
	)
}

func (p *scriptedProvider) Name() string {
	return "scripted"
}

func (p *scriptedProvider) Pricing(string) (agentkit.Pricing, bool) {
	return agentkit.Pricing{Tiers: []agentkit.RateTier{{MinInputTokens: 0}}}, true
}

func cloneRequest(req *agentkit.Request) agentkit.Request {
	if req == nil {
		return agentkit.Request{}
	}
	return agentkit.Request{
		Model:    req.Model,
		System:   req.System,
		Messages: append([]agentkit.Message(nil), req.Messages...),
		Tools:    append([]agentkit.Tool(nil), req.Tools...),
		Gen:      req.Gen,
	}
}

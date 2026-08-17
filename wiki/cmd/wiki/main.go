// Command wiki is the loopback-only wiki MCP service behind nginx.
package main

import (
	"appkit"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"registry"
	"strconv"
	"strings"
	"time"
	"wiki/internal/ask"
	"wiki/internal/compile"
	"wiki/internal/db"
	"wiki/internal/extract"
	"wiki/internal/llm"
	"wiki/internal/match"
	"wiki/internal/mcp"
	"wiki/internal/page"
	"wiki/internal/retrieve"
	"wiki/internal/web"
	"wiki/internal/wiki"
	"wiki/internal/worker"
)

func main() {
	appkit.Main(newSpec(wiki.NewConfig))
}

type configLoader func(func(string) string) (wiki.Config, error)

//nolint:funlen // Keeping lifecycle hooks in one specification makes the shipped wiring auditable as a unit.
func newSpec(loadConfig configLoader) appkit.Spec {
	var svc *wiki.Service

	return appkit.Spec{
		App:   wiki.App,
		Mount: wiki.Mount,
		Port:  registry.MustPort("wiki"),
		MCP:   true,
		WWW:   true,
		ManifestExtras: []appkit.ManifestKV{
			{Key: "MODEL_ID", Value: wiki.ModelID},
			{Key: "WORKER_CONCURRENCY", Value: strconv.Itoa(wiki.WorkerConcurrency)},
			{Key: "SEARCH_DEFAULT", Value: strconv.Itoa(wiki.SearchDefault)},
			{Key: "SEARCH_CAP", Value: strconv.Itoa(wiki.SearchCap)},
			{Key: "ASK_BODY_BUDGET", Value: strconv.Itoa(wiki.AskBodyBudget)},
			{Key: "ASK_CACHE_CAP", Value: strconv.Itoa(wiki.AskCacheCapDefault)},
		},
		Migrations: db.FS,
		Health: func(ctx context.Context) (map[string]any, error) {
			if svc == nil {
				return nil, fmt.Errorf("wiki: Health reporter ran before Handlers built the Service")
			}
			return svc.Health(ctx)
		},
		Config: func(getenv func(string) string) (any, error) {
			return loadConfig(getenv)
		},
		Handlers: func(rt *appkit.Router) error {
			cfg, err := loadConfig(os.Getenv)
			if err != nil {
				return fmt.Errorf("wiki: %w", err)
			}
			if rt.DB() == nil {
				return fmt.Errorf("wiki: no DB handle on router")
			}
			write := rt.DB()
			dbPath, err := db.Path(context.Background(), write)
			if err != nil {
				return err
			}
			read, err := db.OpenRead(dbPath)
			if err != nil {
				return err
			}
			conns := wiki.Conns{Read: read, Write: write}
			llmClient := cfg.LLM
			queueEnabled := llmClient == nil
			if llmClient == nil {
				llmClient = llm.New(registry.BaseURL("prompts"))
			}
			vectorCache := retrieve.NewVectorCache()
			cacheEntries, err := wiki.LoadVectorCacheEntries(context.Background(), conns)
			if err != nil {
				return err
			}
			vectorCache.Replace(retrieveCacheEntries(cacheEntries))
			pageEmbedder, queryEmbedding := embeddingPaths(llmClient, cfg.EmbedSite)
			extractor := extract.New(llmClient, cfg.CallSites.Extract)
			matcher := buildMatcher(cfg, llmClient)
			compiler := buildCompiler(cfg, llmClient)
			serviceOptions := []wiki.ServiceOption{
				wiki.WithMatcher(matcher),
				wiki.WithTelemetryRecorder(rt.Recorder()),
				wiki.WithLogger(rt.Logger()),
				wiki.WithPageEmbedder(cfg.EmbedSite.Model, pageEmbedder),
				wiki.WithVectorCacheUpdater(func(scope, subjectID, title string, vec []float32) {
					vectorCache.Upsert(retrieve.VectorEntry{Scope: scope, SubjectID: subjectID, Title: title, Vec: vec})
				}),
				wiki.WithVectorCacheRemover(vectorCache.Remove),
			}
			if queueEnabled {
				serviceOptions = append(serviceOptions, wiki.WithCompletionQueue(llmClient, cfg.CallSites.Extract, cfg.CallSites.Match, cfg.CallSites.Compile))
			}
			svc = wiki.NewService(conns, extractor, compiler, time.Now, serviceOptions...)
			search := retrieve.NewHybridRetriever(
				retrieve.NewKeywordRetriever(read),
				retrieve.NewVectorRetriever(queryEmbedder(queryEmbedding), vectorCache),
				wiki.NewResolver(read),
				wiki.NewPageStore(read),
				retrieve.FusionConfig{FinalK: cfg.SearchDefault},
			)
			asker := ask.New(
				search,
				wiki.NewSubjectStore(read),
				wiki.NewPageStore(read),
				llmClient,
				cfg.CallSites.AskSubject,
				cfg.CallSites.AskSynthesis,
				ask.WithBodyBudget(cfg.AskBodyBudget),
			)
			askCache := ask.NewCache(asker, cfg.AskCacheCap)
			svc.AskInvalidate = askCache.Invalidate
			webBase := strings.TrimRight(rt.AuthServer(), "/") + wiki.Mount + "subject/"
			webPageService := pathPageService{
				resolver: wiki.NewResolver(read),
				service:  svc,
				webBase:  webBase,
			}
			pageService := pathPageService{
				resolver:     wiki.NewResolver(read),
				service:      svc,
				renderFooter: true,
				notFound:     sql.ErrNoRows,
			}
			subjectService := publicSubjectService{
				subjects: wiki.NewSubjectStore(read),
				pages:    wiki.NewPageStore(read),
			}
			claimService := pathClaimService{
				resolver:     wiki.NewResolver(read),
				claims:       wiki.NewClaimStore(read),
				suppressions: wiki.NewSuppressionStore(read),
			}
			mergeResolver := mergePathResolver{subjects: wiki.NewSubjectStore(read)}
			jobs := wiki.NewJobStore(conns)
			aliases := wiki.NewAliasStore(read)
			scopes := wiki.NewScopeStore(conns)
			scopes.AskInvalidate = askCache.Invalidate
			scopes.VectorInvalidate = vectorCache.RemoveScope
			statusService := publicStatusService{service: svc}
			rt.Handle("/", web.NewHandler(rt.Service(), rt.Version(), wiki.Mount, rt.WWW(),
				web.WithLogger(rt.Logger()),
				web.WithScopeStore(scopes),
				web.WithRecentLister(recentAdapter{svc: svc}),
				web.WithKeywordRetriever(retrieve.NewKeywordRetriever(read)),
				web.WithAsker(askCache),
				web.WithMentioner(mentionAdapter{svc: svc, webBase: webBase}),
				web.WithLinkifier(svc, webBase),
				web.WithPageFinder(webPageService),
			))
			mcpHandler, err := mcp.NewHandler(rt,
				mcp.WithIngestService(svc),
				mcp.WithJobStatusService(statusService),
				mcp.WithJobAbortService(svc),
				mcp.WithJobRerunService(svc),
				mcp.WithJobListService(jobListService{jobs: jobs}),
				mcp.WithJobsCountService(jobCountService{jobs: jobs}),
				mcp.WithMergeService(mergeResolver, svc),
				mcp.WithMergeListService(aliases),
				mcp.WithSubjectListService(subjectService),
				mcp.WithClaimListService(claimService),
				mcp.WithPagePathService(pageService),
				mcp.WithMentionLinkifier(svc),
				mcp.WithAskFunc(askCache.Ask),
				mcp.WithScopeService(scopes),
			)
			if err != nil {
				return err
			}
			rt.Handle("POST /mcp", clearWriteDeadline(rt.RequireIdentity(mcpHandler)))
			return nil
		},
		Workers: []func(ctx context.Context) error{
			func(ctx context.Context) error { return worker.Run(ctx, svc) },
			func(ctx context.Context) error { return worker.RunEmbeddingCatchUp(ctx, svc) },
		},
	}
}

func clearWriteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		next.ServeHTTP(w, r)
	})
}

type promptsEmbedder struct {
	client *llm.Client
	site   llm.EmbedSite
	role   string
}

func embeddingPaths(client *llm.Client, site wiki.EmbedSite) (pageEmbedder, query wiki.PageEmbedder) {
	base := llm.EmbedSite{Model: site.Model, Dims: site.Dims}
	pageEmbedder = promptsEmbedder{client: client, site: llm.EmbedSite{Name: "wiki.embed-page", Model: base.Model, Dims: base.Dims}, role: "document"}
	query = promptsEmbedder{client: client, site: llm.EmbedSite{Name: "wiki.embed-query", Model: base.Model, Dims: base.Dims}, role: "query"}
	return pageEmbedder, query
}

func (e promptsEmbedder) Embed(ctx context.Context, attr llm.Attribution, inputs []string, _ wiki.EmbedRole) (*wiki.EmbedResult, error) {
	vectors, err := e.client.Embed(ctx, e.site, attr, e.role, inputs)
	if err != nil {
		return nil, err
	}
	return &wiki.EmbedResult{Vectors: vectors}, nil
}

func retrieveCacheEntries(entries []wiki.VectorCacheEntry) []retrieve.VectorEntry {
	out := make([]retrieve.VectorEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, retrieve.VectorEntry{
			Scope:     entry.Scope,
			SubjectID: entry.SubjectID,
			Title:     entry.Title,
			Vec:       entry.Vec,
		})
	}
	return out
}

func queryEmbedder(embedder wiki.PageEmbedder) func(context.Context, llm.Attribution, string) ([]float32, error) {
	return func(ctx context.Context, attr llm.Attribution, text string) ([]float32, error) {
		result, err := embedder.Embed(ctx, attr, []string{text}, wiki.EmbedQuery)
		if err != nil {
			return nil, err
		}
		if result == nil || len(result.Vectors) != 1 {
			return nil, fmt.Errorf("wiki: query embedder returned %d vectors, want 1", vectorCount(result))
		}
		return append([]float32(nil), result.Vectors[0]...), nil
	}
}

func vectorCount(result *wiki.EmbedResult) int {
	if result == nil {
		return 0
	}
	return len(result.Vectors)
}

type publicSubject struct {
	Path    string
	Type    string
	Name    string
	HasPage bool
}

type publicClaim struct {
	ID           string
	Kind         string
	Text         string
	Job          string
	Suppressed   bool
	SuppressedBy []string
}

type publicPage struct {
	Path  string
	Title string
	Body  string
}

type publicJobStatus struct {
	ID            string
	CorrelationID string
	Status        string
	ReceivedAt    time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Error         string
	Subjects      []string
	UnitsComplete int
	UnitsTotal    int
}

type jobListService struct {
	jobs *wiki.JobStore
}

func (s jobListService) ListJobsInScope(ctx context.Context, scope string, f mcp.JobFilter, p page.Params) ([]wiki.Job, string, error) {
	return s.jobs.ListJobs(ctx, scope, wiki.JobFilter{
		Statuses: f.Statuses,
		Kinds:    f.Kinds,
		Since:    f.Since,
		Until:    f.Until,
	}, p)
}

type jobCountService struct {
	jobs *wiki.JobStore
}

func (s jobCountService) CountJobsInScope(ctx context.Context, scope string, f mcp.JobFilter) (int, error) {
	return s.jobs.CountJobs(ctx, scope, wiki.JobFilter{
		Statuses: f.Statuses,
		Kinds:    f.Kinds,
		Since:    f.Since,
		Until:    f.Until,
	})
}

type mergePathResolver struct {
	subjects *wiki.SubjectStore
}

func (r mergePathResolver) GetByPath(ctx context.Context, scope, path string) (wiki.Subject, error) {
	subject, err := r.subjects.GetByPath(ctx, scope, path)
	if errors.Is(err, wiki.ErrSubjectNotFound) {
		return wiki.Subject{}, sql.ErrNoRows
	}
	return subject, err
}

type recentAdapter struct {
	svc *wiki.Service
}

func (a recentAdapter) Recent(ctx context.Context, scope string, limit int) ([]web.SubjectRef, error) {
	subjects, err := a.svc.Recent(ctx, scope, limit)
	if err != nil {
		return nil, err
	}
	refs := make([]web.SubjectRef, 0, len(subjects))
	for _, subject := range subjects {
		refs = append(refs, web.SubjectRef{
			Href: "subject/" + wiki.Path(subject),
			Name: subject.Name,
			Type: subject.Type,
		})
	}
	return refs, nil
}

type mentionAdapter struct {
	svc     *wiki.Service
	webBase string
}

func (a mentionAdapter) MentionsIn(ctx context.Context, scope, text string) ([]web.Ref, error) {
	mentions, err := a.svc.MentionsIn(ctx, scope, text)
	if err != nil {
		return nil, err
	}
	refs := make([]web.Ref, 0, len(mentions))
	for _, mention := range mentions {
		refs = append(refs, web.Ref{
			Href: a.webBase + mention.Path,
			Name: mention.Name,
		})
	}
	return refs, nil
}

type publicSubjectService struct {
	subjects *wiki.SubjectStore
	pages    *wiki.PageStore
}

func (s publicSubjectService) ListInScope(ctx context.Context, scope, typ, nameContains string, p page.Params) ([]publicSubject, string, error) {
	subjects, next, err := s.subjects.ListInScope(ctx, scope, typ, nameContains, p)
	if err != nil {
		return nil, "", err
	}
	out := make([]publicSubject, 0, len(subjects))
	for _, subject := range subjects {
		_, err := s.pages.GetBySubject(ctx, subject.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, "", err
		}
		out = append(out, publicSubject{
			Path:    wiki.Path(subject),
			Type:    subject.Type,
			Name:    subject.Name,
			HasPage: err == nil,
		})
	}
	return out, next, nil
}

type pathClaimService struct {
	resolver     *wiki.Resolver
	claims       *wiki.ClaimStore
	suppressions *wiki.SuppressionStore
}

func (s pathClaimService) ListBySubject(ctx context.Context, path string, p page.Params) ([]publicClaim, string, error) {
	return s.ListBySubjectInScope(ctx, "default", path, p)
}

func (s pathClaimService) ListBySubjectInScope(ctx context.Context, scope, path string, p page.Params) ([]publicClaim, string, error) {
	subject, err := s.resolver.ResolveByPath(ctx, scope, path)
	if errors.Is(err, wiki.ErrSubjectNotFound) {
		return nil, "", sql.ErrNoRows
	}
	if err != nil {
		return nil, "", err
	}
	claims, next, err := s.claims.ListBySubject(ctx, subject.ID, p)
	if err != nil {
		return nil, "", err
	}
	edges, err := s.suppressions.ListBySubject(ctx, subject.ID)
	if err != nil {
		return nil, "", err
	}
	allClaims, err := s.listAllClaims(ctx, subject.ID)
	if err != nil {
		return nil, "", err
	}
	_, suppressedBy := wiki.Effective(allClaims, edges)
	out := make([]publicClaim, 0, len(claims))
	for _, claim := range claims {
		suppressors := suppressedBy[claim.ID]
		out = append(out, publicClaim{
			ID:           claim.ID,
			Kind:         claim.Kind,
			Text:         claim.Body,
			Job:          claim.JobID,
			Suppressed:   len(suppressors) > 0,
			SuppressedBy: suppressors,
		})
	}
	return out, next, nil
}

func (s pathClaimService) listAllClaims(ctx context.Context, subjectID string) ([]wiki.Claim, error) {
	var out []wiki.Claim
	p := page.Params{}
	for {
		claims, next, err := s.claims.ListBySubject(ctx, subjectID, p)
		if err != nil {
			return nil, err
		}
		out = append(out, claims...)
		if next == "" {
			return out, nil
		}
		p.Cursor = next
	}
}

type publicStatusService struct {
	service *wiki.Service
}

func (s publicStatusService) JobStatus(ctx context.Context, jobID string) (publicJobStatus, error) {
	status, err := s.service.JobStatus(ctx, jobID)
	if err != nil {
		return publicJobStatus{}, err
	}
	subjects, err := s.service.Subjects(ctx, "", "")
	if err != nil {
		return publicJobStatus{}, err
	}
	paths := make(map[string]string, len(subjects))
	for _, subject := range subjects {
		paths[subject.ID] = wiki.Path(subject)
	}
	publicSubjects := make([]string, 0, len(status.Subjects))
	for _, subjectID := range status.Subjects {
		if path, ok := paths[subjectID]; ok {
			publicSubjects = append(publicSubjects, path)
		}
	}
	return publicJobStatus{
		ID:            status.ID,
		CorrelationID: status.CorrelationID,
		Status:        status.Status,
		ReceivedAt:    status.ReceivedAt,
		StartedAt:     status.StartedAt,
		FinishedAt:    status.FinishedAt,
		Error:         status.Error,
		Subjects:      publicSubjects,
		UnitsComplete: status.UnitsComplete,
		UnitsTotal:    status.UnitsTotal,
	}, nil
}

type pathPageService struct {
	resolver     *wiki.Resolver
	service      *wiki.Service
	webBase      string
	renderFooter bool
	notFound     error
}

func (s pathPageService) PageByPath(ctx context.Context, path string) (web.SubjectView, error) {
	return s.PageByPathInScope(ctx, "default", path)
}

func (s pathPageService) PageByPathInScope(ctx context.Context, scope, path string) (web.SubjectView, error) {
	subject, err := s.resolver.ResolveByPath(ctx, scope, path)
	if errors.Is(err, wiki.ErrSubjectNotFound) {
		return web.SubjectView{}, s.notFoundErr()
	}
	if err != nil {
		return web.SubjectView{}, err
	}
	pageView, err := s.service.PageWithLinks(ctx, scope, subject.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return web.SubjectView{}, s.notFoundErr()
	}
	if err != nil {
		return web.SubjectView{}, err
	}
	body, err := s.service.LinkifyMentions(ctx, scope, pageView.Body, s.webBase, subject.ID)
	if err != nil {
		return web.SubjectView{}, err
	}
	footer := ""
	if s.renderFooter {
		footer = strings.TrimPrefix(wiki.RenderFooter(body, pageView.Mentions, pageView.MentionedBy), body)
	}
	return web.SubjectView{
		SubjectID: subject.ID,
		Path:      wiki.Path(subject),
		Title:     pageView.Title,
		Body:      body,
		Footer:    footer,
		Outbound:  webRefs(s.webBase, pageView.Mentions),
		Inbound:   webRefs(s.webBase, pageView.MentionedBy),
	}, nil
}

func (s pathPageService) notFoundErr() error {
	if s.notFound != nil {
		return s.notFound
	}
	return web.ErrNotFound
}

func webRefs(webBase string, refs []wiki.Ref) []web.Ref {
	out := make([]web.Ref, 0, len(refs))
	for _, ref := range refs {
		out = append(out, web.Ref{
			Href: webBase + ref.Path,
			Name: ref.Name,
		})
	}
	return out
}

func buildCompiler(cfg wiki.Config, c *llm.Client) *compile.Compiler {
	return compile.New(c, cfg.CallSites.Compile, nil)
}

func buildMatcher(cfg wiki.Config, c *llm.Client) *match.Matcher {
	return match.New(c, cfg.CallSites.Match)
}

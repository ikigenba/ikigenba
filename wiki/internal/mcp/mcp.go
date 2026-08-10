// Package mcp implements wiki's domain MCP tools over the appkit transport.
package mcp

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"appkit"
	appkitmcp "appkit/mcp"
	"appkit/server"

	paging "wiki/internal/page"
	"wiki/internal/wiki"
)

const Instructions = "wiki is a scoped knowledge base (notes, a second brain) built from source text you ingest. Choose or create a scope first; every content call requires it. Ingest queues text; a background pipeline distills entities, events, concepts, and their claims into cited pages. Ask for grounded answers; read knowledge by type/slug path; track jobs; merge duplicates; and steer work with abort and rerun. Call guide for scope management, field catalogs, paths, and worked examples before your first ingest or merge."

const (
	errScopeNotFound appkitmcp.ErrorCode = "scope_not_found"
	errScopeExists   appkitmcp.ErrorCode = "scope_exists"
	errScopeDefault  appkitmcp.ErrorCode = "scope_is_default"
)

// Handler holds configured wiki domain tool dependencies.
type Handler struct {
	pageBase  string
	linkify   func(context.Context, string, string, string, string) (string, error)
	ingest    func(context.Context, string, string, string, string, string, []string) (string, error)
	status    func(context.Context, string) (any, error)
	abort     func(context.Context, string) (any, error)
	rerun     func(context.Context, string) (any, error)
	jobs      func(context.Context, string, JobFilter, paging.Params) (any, string, error)
	jobsCount func(context.Context, string, JobFilter) (int, error)
	resolve   func(context.Context, string, string) (any, error)
	merge     func(context.Context, string, string, string) (string, error)
	merges    func(context.Context, string, paging.Params) (any, string, error)
	ask       func(context.Context, string, string, string) (any, error)
	subjects  func(context.Context, string, string, string, paging.Params) (any, string, error)
	claims    func(context.Context, string, string, paging.Params) (any, string, error)
	page      func(context.Context, string, string) (any, error)
	scopes    scopeService
}

// JobFilter is a paginated MCP job-list filter.
type JobFilter struct {
	Statuses     []string
	Kinds        []string
	Since, Until time.Time
}

type ingestService interface {
	Ingest(ctx context.Context, scope, ownerID, ownerEmail, text, title string, tags []string) (string, error)
}

type jobStatusFunc[T any] interface {
	JobStatus(ctx context.Context, jobID string) (T, error)
}

type jobAbortFunc[T any] interface {
	Abort(ctx context.Context, jobID string) (T, error)
}

type jobRerunFunc[T any] interface {
	Rerun(ctx context.Context, jobID string) (T, error)
}

type jobListFunc[T any] interface {
	ListJobsInScope(ctx context.Context, scope string, f JobFilter, p paging.Params) (T, string, error)
}

type jobsCountFunc interface {
	CountJobsInScope(ctx context.Context, scope string, f JobFilter) (int, error)
}

type subjectPathFunc[T any] interface {
	GetByPath(ctx context.Context, scope, path string) (T, error)
}

type mergeFunc interface {
	MergeSubjects(ctx context.Context, scope, fromSubjectID, toSubjectID string) (string, error)
}

type mergeListFunc[T any] interface {
	ListMergesInScope(ctx context.Context, scope string, p paging.Params) (T, string, error)
}

type subjectsFunc[T any] interface {
	Subjects(ctx context.Context, typ, nameContains string) (T, error)
}

type subjectListFunc[T any] interface {
	ListInScope(ctx context.Context, scope, typ, nameContains string, p paging.Params) (T, string, error)
}

type claimsFunc[T any] interface {
	ClaimsBySubject(ctx context.Context, subjectID string) (T, error)
}

type claimListFunc[T any] interface {
	ListBySubjectInScope(ctx context.Context, scope, subjectID string, p paging.Params) (T, string, error)
}

type pageFunc[T any] interface {
	PageBySubject(ctx context.Context, subjectID string) (T, error)
}

type pageByPathFunc[T any] interface {
	PageByPathInScope(ctx context.Context, scope, path string) (T, error)
}

type mentionLinkifier interface {
	LinkifyMentions(ctx context.Context, scope, text, base, excludeID string) (string, error)
}

type scopeService interface {
	Create(context.Context, string) (wiki.Scope, error)
	Get(context.Context, string) (wiki.Scope, error)
	List(context.Context) ([]wiki.Scope, error)
	SetVisibility(context.Context, string, string) error
	Delete(context.Context, string) error
}

// Option configures optional MCP tools backed by wiki domain services.
type Option func(*Handler)

// WithIngestService enables the ingest tool.
func WithIngestService(s ingestService) Option {
	return func(h *Handler) {
		if s != nil {
			h.ingest = func(ctx context.Context, scope, ownerID, ownerEmail, text, title string, tags []string) (string, error) {
				return s.Ingest(ctx, scope, ownerID, ownerEmail, text, title, tags)
			}
		}
	}
}

// WithJobStatusService enables the job-status tool.
func WithJobStatusService[T any](s jobStatusFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.status = func(ctx context.Context, jobID string) (any, error) {
				return s.JobStatus(ctx, jobID)
			}
		}
	}
}

// WithJobAbortService enables the abort tool.
func WithJobAbortService[T any](s jobAbortFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.abort = func(ctx context.Context, jobID string) (any, error) {
				return s.Abort(ctx, jobID)
			}
		}
	}
}

// WithJobRerunService enables the rerun tool.
func WithJobRerunService[T any](s jobRerunFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.rerun = func(ctx context.Context, jobID string) (any, error) {
				return s.Rerun(ctx, jobID)
			}
		}
	}
}

// WithJobListService enables the paginated jobs tool.
func WithJobListService[T any](s jobListFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.jobs = func(ctx context.Context, scope string, f JobFilter, p paging.Params) (any, string, error) {
				return s.ListJobsInScope(ctx, scope, f, p)
			}
		}
	}
}

// WithJobsService enables the paginated jobs tool.
func WithJobsService[T any](s jobListFunc[T]) Option {
	return WithJobListService(s)
}

// WithJobsCountService enables the jobs_count tool.
func WithJobsCountService(s jobsCountFunc) Option {
	return func(h *Handler) {
		if s != nil {
			h.jobsCount = s.CountJobsInScope
		}
	}
}

// WithMergeService enables the merge tool.
func WithMergeService[T any](resolver subjectPathFunc[T], s mergeFunc) Option {
	return func(h *Handler) {
		if resolver != nil {
			h.resolve = func(ctx context.Context, scope, path string) (any, error) {
				return resolver.GetByPath(ctx, scope, path)
			}
		}
		if s != nil {
			h.merge = func(ctx context.Context, scope, from, to string) (string, error) {
				return s.MergeSubjects(ctx, scope, from, to)
			}
		}
	}
}

// WithMergeListService enables the paginated merges audit tool.
func WithMergeListService[T any](s mergeListFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.merges = func(ctx context.Context, scope string, p paging.Params) (any, string, error) {
				return s.ListMergesInScope(ctx, scope, p)
			}
		}
	}
}

// WithAbortService enables the abort tool.
func WithAbortService[T any](s jobAbortFunc[T]) Option {
	return WithJobAbortService(s)
}

// WithRerunService enables the rerun tool.
func WithRerunService[T any](s jobRerunFunc[T]) Option {
	return WithJobRerunService(s)
}

// WithSubjectsService enables the registry-list subjects tool.
func WithSubjectsService[T any](s subjectsFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.subjects = func(ctx context.Context, _, typ, nameContains string, _ paging.Params) (any, string, error) {
				subjects, err := s.Subjects(ctx, typ, nameContains)
				return subjects, "", err
			}
		}
	}
}

// WithSubjectListService enables the paginated registry-list subjects tool.
func WithSubjectListService[T any](s subjectListFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.subjects = func(ctx context.Context, scope, typ, nameContains string, p paging.Params) (any, string, error) {
				return s.ListInScope(ctx, scope, typ, nameContains, p)
			}
		}
	}
}

// WithClaimsService enables the claims-by-subject tool.
func WithClaimsService[T any](s claimsFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.claims = func(ctx context.Context, _, subjectID string, _ paging.Params) (any, string, error) {
				claims, err := s.ClaimsBySubject(ctx, subjectID)
				return claims, "", err
			}
		}
	}
}

// WithClaimListService enables the paginated claims-by-subject tool.
func WithClaimListService[T any](s claimListFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.claims = func(ctx context.Context, scope, subjectID string, p paging.Params) (any, string, error) {
				return s.ListBySubjectInScope(ctx, scope, subjectID, p)
			}
		}
	}
}

// WithPageService enables the page tool from the legacy subject-id service.
func WithPageService[T any](s pageFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.page = func(ctx context.Context, _, subjectID string) (any, error) {
				return s.PageBySubject(ctx, subjectID)
			}
		}
	}
}

// WithPagePathService enables the page tool from a public type/norm_name path service.
func WithPagePathService[T any](s pageByPathFunc[T]) Option {
	return func(h *Handler) {
		if s != nil {
			h.page = func(ctx context.Context, scope, path string) (any, error) {
				return s.PageByPathInScope(ctx, scope, path)
			}
		}
	}
}

// WithMentionLinkifier enables inline first-occurrence subject links in prose
// returned by the ask and page tools.
func WithMentionLinkifier(s mentionLinkifier) Option {
	return func(h *Handler) {
		if s != nil {
			h.linkify = func(ctx context.Context, scope, text, base, excludeID string) (string, error) {
				return s.LinkifyMentions(ctx, scope, text, base, excludeID)
			}
		}
	}
}

// WithAskFunc enables the grounded ask tool.
func WithAskFunc[T any](fn func(context.Context, string, string, string) (T, error)) Option {
	return func(h *Handler) {
		if fn != nil {
			h.ask = func(ctx context.Context, scope, owner, question string) (any, error) {
				return fn(ctx, scope, owner, question)
			}
		}
	}
}

// WithScopeService enables scope validation and the four scope-management tools.
func WithScopeService(s scopeService) Option {
	return func(h *Handler) { h.scopes = s }
}

// Tools returns wiki's configured domain MCP tools. Chassis health and
// reflection are supplied by appkit/mcp and are not declared here.
func Tools(opts ...Option) []appkitmcp.Tool {
	h := &Handler{}
	for _, opt := range opts {
		opt(h)
	}
	tools := []appkitmcp.Tool{}
	if h.ingest != nil {
		tools = append(tools, domainTool(ingestTool(), h.handleIngestCall))
	}
	if h.status != nil {
		tools = append(tools, domainTool(jobStatusTool(), h.handleJobStatusCall))
	}
	if h.abort != nil {
		tools = append(tools, domainTool(jobAbortTool(), h.handleJobAbortCall))
	}
	if h.rerun != nil {
		tools = append(tools, domainTool(jobRerunTool(), h.handleJobRerunCall))
	}
	if h.jobs != nil {
		tools = append(tools, domainTool(jobsTool(), h.handleJobsCall))
	}
	if h.jobsCount != nil {
		tools = append(tools, domainTool(jobsCountTool(), h.handleJobsCountCall))
	}
	if h.resolve != nil && h.merge != nil {
		tools = append(tools, domainTool(mergeTool(), h.handleMergeCall))
	}
	if h.merges != nil {
		tools = append(tools, domainTool(mergesTool(), h.handleMergesCall))
	}
	if h.ask != nil {
		tools = append(tools, domainTool(askTool(), h.handleAskCall))
	}
	if h.subjects != nil {
		tools = append(tools, domainTool(subjectsTool(), h.handleSubjectsCall))
	}
	if h.claims != nil {
		tools = append(tools, domainTool(claimsTool(), h.handleClaimsCall))
	}
	if h.page != nil {
		tools = append(tools, domainTool(pageTool(), h.handlePageCall))
	}
	if h.scopes != nil {
		tools = append(tools,
			domainTool(scopesTool(), h.handleScopesCall),
			domainTool(scopeCreateTool(), h.handleScopeCreateCall),
			domainTool(scopeDeleteTool(), h.handleScopeDeleteCall),
			domainTool(scopeSetVisibilityTool(), h.handleScopeSetVisibilityCall),
		)
	}
	tools = append(tools, domainTool(guideTool(), handleGuideCall))
	return tools
}

// NewHandler builds the MCP handler from appkit's route-time service metadata.
func NewHandler(rt *appkit.Router, opts ...Option) (http.Handler, error) {
	pageBase := strings.TrimRight(rt.AuthServer(), "/") + wiki.Mount + "subject/"
	handlerOpts := append([]Option{}, opts...)
	handlerOpts = append(handlerOpts, func(h *Handler) {
		h.pageBase = pageBase
	})
	return appkitmcp.New(appkitmcp.Options{
		Service:       rt.Service(),
		Version:       rt.Version(),
		Instructions:  Instructions,
		Tools:         Tools(handlerOpts...),
		Health:        rt.Health(),
		Events:        rt.Events(),
		Publishes:     rt.Publishes(),
		Subscriptions: rt.Subscriptions(),
	})
}

func domainTool(desc map[string]any, handler func(context.Context, json.RawMessage, server.Identity) (map[string]any, error)) appkitmcp.Tool {
	tool := appkitmcp.Tool{
		Name:        desc["name"].(string),
		Description: desc["description"].(string),
		InputSchema: desc["inputSchema"].(map[string]any),
		Handler:     handler,
	}
	if schema, ok := desc["outputSchema"].(map[string]any); ok {
		tool.OutputSchema = schema
	}
	return tool
}

func (h *Handler) handleIngestCall(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	if h.ingest == nil {
		return internalError("ingest tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		Text  string   `json:"text"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	if strings.TrimSpace(args.Text) == "" {
		return validationError("text is required"), nil
	}
	jobID, err := h.ingest(ctx, scope, id.OwnerID, id.OwnerEmail, args.Text, args.Title, args.Tags)
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]string{"job_id": jobID})
}

func (h *Handler) handleJobStatusCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.status == nil {
		return internalError("job_status tool is not configured"), nil
	}
	var args struct {
		JobID string `json:"job_id"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	if strings.TrimSpace(args.JobID) == "" {
		return validationError("job_id is required"), nil
	}
	status, err := h.status(ctx, args.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("job", args.JobID), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(publicStatusResult(status))
}

func (h *Handler) handleJobAbortCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.abort == nil {
		return internalError("abort tool is not configured"), nil
	}
	args, err := decodeJobIDArgs(raw)
	if err != nil {
		return validationError(err.Error()), nil
	}
	result, err := h.abort(ctx, args.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("job", args.JobID), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(publicAbortResult(result))
}

func (h *Handler) handleJobRerunCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.rerun == nil {
		return internalError("rerun tool is not configured"), nil
	}
	args, err := decodeJobIDArgs(raw)
	if err != nil {
		return validationError(err.Error()), nil
	}
	result, err := h.rerun(ctx, args.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("job", args.JobID), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(publicRerunResult(result))
}

func (h *Handler) handleJobsCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.jobs == nil {
		return internalError("jobs tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	filter, limit, cursor, err := decodeJobsArgs(raw, true)
	if err != nil {
		return validationError(err.Error()), nil
	}
	jobs, next, err := h.jobs(ctx, scope, filter, paging.Params{Limit: limit, Cursor: cursor})
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(pagedResult("jobs", publicJobsResult(jobs), next))
}

func (h *Handler) handleJobsCountCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.jobsCount == nil {
		return internalError("jobs_count tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	filter, _, _, err := decodeJobsArgs(raw, false)
	if err != nil {
		return validationError(err.Error()), nil
	}
	count, err := h.jobsCount(ctx, scope, filter)
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]int{"count": count})
}

func (h *Handler) handleMergeCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.resolve == nil || h.merge == nil {
		return internalError("merge tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	fromPath := strings.TrimSpace(args.From)
	toPath := strings.TrimSpace(args.To)
	if fromPath == "" {
		return validationError("from is required"), nil
	}
	if toPath == "" {
		return validationError("to is required"), nil
	}
	from, err := h.resolve(ctx, scope, fromPath)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("subject", fromPath), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	to, err := h.resolve(ctx, scope, toPath)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("subject", toPath), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	fromID := stringField(indirect(reflect.ValueOf(from)), "ID")
	toID := stringField(indirect(reflect.ValueOf(to)), "ID")
	if fromID == toID {
		return validationError("from and to resolve to the same subject"), nil
	}
	jobID, err := h.merge(ctx, scope, fromID, toID)
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]string{"job_id": jobID})
}

func (h *Handler) handleMergesCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.merges == nil {
		return internalError("merges tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	if strings.TrimSpace(args.Cursor) != "" {
		if _, ok := paging.DecodeCursor(args.Cursor); !ok {
			return validationError("cursor is invalid"), nil
		}
	}
	merges, next, err := h.merges(ctx, scope, paging.Params{Limit: args.Limit, Cursor: args.Cursor})
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(pagedResult("merges", publicMergesResult(merges), next))
}

func (h *Handler) handleAskCall(ctx context.Context, raw json.RawMessage, id server.Identity) (map[string]any, error) {
	if h.ask == nil {
		return internalError("ask tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		Question string `json:"question"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	if strings.TrimSpace(args.Question) == "" {
		return validationError("question is required"), nil
	}
	answer, err := h.ask(ctx, scope, id.OwnerEmail, args.Question)
	if err != nil {
		return internalError(err.Error()), nil
	}
	result := askToolResult(answer, h.pageBase)
	if h.linkify != nil {
		text, err := h.linkify(ctx, scope, result["answer"].(string), h.pageBase, "")
		if err != nil {
			return internalError(err.Error()), nil
		}
		result["answer"] = text
	}
	return appkitmcp.StructuredResult(result)
}

func (h *Handler) handleSubjectsCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.subjects == nil {
		return internalError("subjects tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	subjects, next, err := h.subjects(ctx, scope, args.Type, args.Name, paging.Params{Limit: args.Limit, Cursor: args.Cursor})
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(pagedResult("subjects", publicSubjectsResult(subjects), next))
}

func (h *Handler) handleClaimsCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.claims == nil {
		return internalError("claims tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		Subject string `json:"subject"`
		Path    string `json:"path"`
		Limit   int    `json:"limit"`
		Cursor  string `json:"cursor"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	subject := strings.TrimSpace(args.Subject)
	if subject == "" {
		subject = strings.TrimSpace(args.Path)
	}
	if subject == "" {
		return validationError("subject is required"), nil
	}
	claims, next, err := h.claims(ctx, scope, subject, paging.Params{Limit: args.Limit, Cursor: args.Cursor})
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("subject", subject), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(pagedResult("claims", publicClaimsResult(claims), next))
}

func (h *Handler) handlePageCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	if h.page == nil {
		return internalError("page tool is not configured"), nil
	}
	scope, scopeErr := h.requireScope(ctx, raw)
	if scopeErr != nil {
		return scopeErr, nil
	}
	var args struct {
		Subject string `json:"subject"`
		Path    string `json:"path"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	subject := strings.TrimSpace(args.Subject)
	if subject == "" {
		subject = strings.TrimSpace(args.Path)
	}
	if subject == "" {
		return validationError("subject is required"), nil
	}
	page, err := h.page(ctx, scope, subject)
	if errors.Is(err, sql.ErrNoRows) {
		return notFoundError("subject", subject), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	result := publicPageResult(page, subject)
	footer := stringField(indirect(reflect.ValueOf(page)), "Footer")
	if h.linkify != nil {
		body, err := h.linkify(ctx, scope, result["body"], h.pageBase, stringField(indirect(reflect.ValueOf(page)), "SubjectID"))
		if err != nil {
			return internalError(err.Error()), nil
		}
		result["body"] = body
	}
	result["body"] += footer
	return appkitmcp.StructuredResult(result)
}

func handleGuideCall(context.Context, json.RawMessage, server.Identity) (map[string]any, error) {
	return appkitmcp.TextResult(guideDoc), nil
}

func (h *Handler) requireScope(ctx context.Context, raw json.RawMessage) (string, map[string]any) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", validationError(err.Error())
	}
	scope := args.Scope
	if strings.TrimSpace(scope) == "" {
		return "", validationError("scope is required; use scopes to list available scopes")
	}
	if h.scopes == nil {
		return scope, nil
	}
	if _, err := h.scopes.Get(ctx, scope); errors.Is(err, wiki.ErrScopeNotFound) {
		return "", appkitmcp.ErrorResult(errScopeNotFound, fmt.Sprintf("scope %q not found; use scopes to list available scopes", scope))
	} else if err != nil {
		return "", internalError(err.Error())
	}
	return scope, nil
}

func (h *Handler) handleScopesCall(ctx context.Context, _ json.RawMessage, _ server.Identity) (map[string]any, error) {
	scopes, err := h.scopes.List(ctx)
	if err != nil {
		return internalError(err.Error()), nil
	}
	items := make([]map[string]any, 0, len(scopes))
	for _, scope := range scopes {
		items = append(items, map[string]any{"name": scope.Name, "visibility": scope.Visibility, "created_at": scope.CreatedAt})
	}
	return appkitmcp.StructuredResult(map[string]any{"scopes": items})
}

func (h *Handler) handleScopeCreateCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	name, result := decodeScopeName(raw)
	if result != nil {
		return result, nil
	}
	scope, err := h.scopes.Create(ctx, name)
	switch {
	case errors.Is(err, wiki.ErrInvalidScopeName):
		return validationError("name must be a lowercase slug of letters, digits, and single hyphens, 1-64 characters, with no leading or trailing hyphen"), nil
	case errors.Is(err, wiki.ErrScopeExists):
		return appkitmcp.ErrorResult(errScopeExists, fmt.Sprintf("scope %q already exists", name)), nil
	case err != nil:
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]string{"name": scope.Name, "visibility": scope.Visibility})
}

func (h *Handler) handleScopeDeleteCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	name, result := decodeScopeName(raw)
	if result != nil {
		return result, nil
	}
	err := h.scopes.Delete(ctx, name)
	switch {
	case errors.Is(err, wiki.ErrScopeIsDefault):
		return appkitmcp.ErrorResult(errScopeDefault, "the default scope cannot be deleted"), nil
	case errors.Is(err, wiki.ErrScopeNotFound):
		return notFoundError("scope", name), nil
	case err != nil:
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]any{"name": name, "deleted": true})
}

func (h *Handler) handleScopeSetVisibilityCall(ctx context.Context, raw json.RawMessage, _ server.Identity) (map[string]any, error) {
	var args struct {
		Name       string `json:"name"`
		Visibility string `json:"visibility"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return validationError(err.Error()), nil
	}
	args.Name = strings.TrimSpace(args.Name)
	args.Visibility = strings.TrimSpace(args.Visibility)
	if args.Name == "" {
		return validationError("name is required"), nil
	}
	if args.Visibility != "private" && args.Visibility != "public" {
		return validationError("visibility must be one of {private, public}"), nil
	}
	err := h.scopes.SetVisibility(ctx, args.Name, args.Visibility)
	if errors.Is(err, wiki.ErrScopeNotFound) {
		return notFoundError("scope", args.Name), nil
	}
	if err != nil {
		return internalError(err.Error()), nil
	}
	return appkitmcp.StructuredResult(map[string]string{"name": args.Name, "visibility": args.Visibility})
}

func decodeScopeName(raw json.RawMessage) (string, map[string]any) {
	var args struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", validationError(err.Error())
	}
	name := args.Name
	if strings.TrimSpace(name) == "" {
		return "", validationError("name is required; names must follow the lowercase slug rule")
	}
	return name, nil
}

func ingestTool() map[string]any {
	return map[string]any{
		"name":        "ingest",
		"description": "Ingest source text when adding knowledge. Provide text plus optional title and tags; queues background processing and returns a job_id immediately.",
		"inputSchema": scopedSchema(map[string]any{
			"text":  map[string]any{"type": "string"},
			"title": map[string]any{"type": "string"},
			"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, []string{"text"}),
		"outputSchema": objectSchema(map[string]any{"job_id": stringSchema()}, []string{"job_id"}),
	}
}

func jobStatusTool() map[string]any {
	return map[string]any{
		"name":        "status",
		"description": "Status check for an ingest or merge job. Provide its job_id; returns lifecycle timestamps, errors, and compiled subject paths.",
		"inputSchema": objectSchema(map[string]any{
			"job_id": map[string]any{"type": "string"},
		}, []string{"job_id"}),
		"outputSchema": objectSchema(map[string]any{
			"status": stringSchema(), "received_at": nullableStringSchema(), "started_at": nullableStringSchema(),
			"finished_at": nullableStringSchema(), "error": stringSchema(), "subjects": stringArraySchema(),
		}, []string{"status"}),
	}
}

func jobAbortTool() map[string]any {
	return map[string]any{
		"name":        "abort",
		"description": "Abort a pending or working job by job_id. This terminal action cannot abort a finished job; returns whether it was aborted and its resulting status.",
		"inputSchema": objectSchema(map[string]any{
			"job_id": map[string]any{"type": "string"},
		}, []string{"job_id"}),
		"outputSchema": objectSchema(map[string]any{"aborted": boolSchema(), "status": stringSchema()}, []string{"aborted", "status"}),
	}
}

func jobRerunTool() map[string]any {
	return map[string]any{
		"name":        "rerun",
		"description": "Rerun a done, failed, or aborted job by job_id. This requeues its work and returns whether it was requeued and its resulting status.",
		"inputSchema": objectSchema(map[string]any{
			"job_id": map[string]any{"type": "string"},
		}, []string{"job_id"}),
		"outputSchema": objectSchema(map[string]any{"requeued": boolSchema(), "status": stringSchema()}, []string{"requeued", "status"}),
	}
}

func jobsTool() map[string]any {
	return map[string]any{
		"name":        "jobs",
		"description": "Jobs history for ingestion and merges. Filter by status, kind, or RFC3339 time range and paginate with limit/cursor; returns jobs and next_cursor.",
		"inputSchema": scopedListSchema(map[string]any{
			"status": jobStatusArraySchema(),
			"kind":   jobKindArraySchema(),
			"since":  map[string]any{"type": "string"},
			"until":  map[string]any{"type": "string"},
		}),
		"outputSchema": pagedOutputSchema("jobs", objectArraySchema(map[string]any{
			"id": stringSchema(), "owner_email": stringSchema(), "owner_id": stringSchema(), "title": stringSchema(), "tags": stringArraySchema(),
			"status": stringSchema(), "received_at": nullableStringSchema(), "started_at": nullableStringSchema(),
			"finished_at": nullableStringSchema(), "error": stringSchema(),
		})),
	}
}

func jobsCountTool() map[string]any {
	return map[string]any{
		"name":        "jobs_count",
		"description": "Jobs count for ingestion and merges. Apply the same status, kind, and RFC3339 time filters as jobs; returns the matching count without pagination.",
		"inputSchema": scopedSchema(map[string]any{
			"status": jobStatusArraySchema(),
			"kind":   jobKindArraySchema(),
			"since":  map[string]any{"type": "string"},
			"until":  map[string]any{"type": "string"},
		}, nil),
		"outputSchema": objectSchema(map[string]any{"count": map[string]any{"type": "integer"}}, []string{"count"}),
	}
}

func mergeTool() map[string]any {
	return map[string]any{
		"name":        "merge",
		"description": "Merge a duplicate subject into its survivor. Provide from and to as type/slug paths; queues an irreversible fold and returns its job_id.",
		"inputSchema": scopedSchema(map[string]any{
			"from": map[string]any{"type": "string"},
			"to":   map[string]any{"type": "string"},
		}, []string{"from", "to"}),
		"outputSchema": objectSchema(map[string]any{"job_id": stringSchema()}, []string{"job_id"}),
	}
}

func mergesTool() map[string]any {
	return map[string]any{
		"name":        "merges",
		"description": "Merge alias audit history. Paginate with limit/cursor; returns folded names, survivor subject IDs, and next_cursor.",
		"inputSchema": scopedListSchema(map[string]any{}),
		"outputSchema": pagedOutputSchema("merges", objectArraySchema(map[string]any{
			"norm_name": stringSchema(), "subject_id": stringSchema(), "name": stringSchema(),
			"owner_email": stringSchema(), "owner_id": stringSchema(), "created_at": stringSchema(),
		})),
	}
}

func jobStatusArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "string",
			"enum": validJobStatuses,
		},
	}
}

func askTool() map[string]any {
	return map[string]any{
		"name":        "ask",
		"description": "Ask a question over your own wiki when you need a grounded answer. Provide question; returns found, answer, and cited page URLs with titles.",
		"inputSchema": scopedSchema(map[string]any{
			"question": map[string]any{"type": "string"},
		}, []string{"question"}),
		"outputSchema": objectSchema(map[string]any{
			"found": boolSchema(), "answer": stringSchema(),
			"citations": objectArraySchema(map[string]any{"url": stringSchema(), "title": stringSchema()}),
		}, []string{"found", "answer", "citations"}),
	}
}

func askToolResult(answer any, pageBase string) map[string]any {
	found, text, sourceCitations := answerFields(answer)
	citations := make([]map[string]string, 0, sourceCitations.Len())
	for i := 0; i < sourceCitations.Len(); i++ {
		citation := indirect(sourceCitations.Index(i))
		citations = append(citations, map[string]string{
			"url":   pageBase + stringField(citation, "Path"),
			"title": stringField(citation, "Title"),
		})
	}
	return map[string]any{
		"found":     found,
		"answer":    text,
		"citations": citations,
	}
}

func answerFields(answer any) (bool, string, reflect.Value) {
	v := indirect(reflect.ValueOf(answer))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return false, "", reflect.ValueOf([]any{})
	}
	return boolField(v, "Found"), stringField(v, "Text"), sliceField(v, "Citations")
}

func indirect(v reflect.Value) reflect.Value {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func boolField(v reflect.Value, name string) bool {
	field := v.FieldByName(name)
	return field.IsValid() && field.Kind() == reflect.Bool && field.Bool()
}

func stringField(v reflect.Value, name string) string {
	field := v.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

func intField(v reflect.Value, name string) int64 {
	field := v.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int()
	default:
		return 0
	}
}

func sliceField(v reflect.Value, name string) reflect.Value {
	field := v.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return reflect.ValueOf([]any{})
	}
	return field
}

func subjectsTool() map[string]any {
	return map[string]any{
		"name":        "subjects",
		"description": "Subjects registry lookup. Optionally filter by type and name substring and paginate with limit/cursor; returns subject paths, names, page availability, and next_cursor.",
		"inputSchema": scopedListSchema(map[string]any{
			"type": map[string]any{"type": "string"},
			"name": map[string]any{"type": "string"},
		}),
		"outputSchema": pagedOutputSchema("subjects", objectArraySchema(map[string]any{
			"path": stringSchema(), "type": stringSchema(), "name": stringSchema(), "has_page": boolSchema(),
		})),
	}
}

func claimsTool() map[string]any {
	return map[string]any{
		"name":        "claims",
		"description": "Claims lookup for one subject. Provide subject as a type/slug path and paginate with limit/cursor; returns cited claims and next_cursor.",
		"inputSchema": scopedSchema(map[string]any{
			"subject": map[string]any{"type": "string"},
			"limit":   map[string]any{"type": "integer"},
			"cursor":  map[string]any{"type": "string"},
		}, []string{"subject"}),
		"outputSchema": pagedOutputSchema("claims", objectArraySchema(map[string]any{
			"id": stringSchema(), "text": stringSchema(), "job": stringSchema(),
		})),
	}
}

func pageTool() map[string]any {
	return map[string]any{
		"name":        "page",
		"description": "Page lookup for compiled knowledge about one subject. Provide subject as a type/slug path; returns its title and cited Markdown body.",
		"inputSchema": scopedSchema(map[string]any{
			"subject": map[string]any{"type": "string"},
		}, []string{"subject"}),
		"outputSchema": objectSchema(map[string]any{
			"subject": stringSchema(), "title": stringSchema(), "body": stringSchema(),
		}, []string{"subject", "title", "body"}),
	}
}

func scopesTool() map[string]any {
	return map[string]any{
		"name":        "scopes",
		"description": "List explicit wiki scopes. Returns every scope name, visibility, and creation time in name order; default is always present.",
		"inputSchema": objectSchema(map[string]any{}, nil),
		"outputSchema": objectSchema(map[string]any{
			"scopes": objectArraySchema(map[string]any{"name": stringSchema(), "visibility": stringSchema(), "created_at": map[string]any{"type": "integer"}}),
		}, []string{"scopes"}),
	}
}

func scopeCreateTool() map[string]any {
	return map[string]any{
		"name":         "scope_create",
		"description":  "Create an explicit private scope for isolated wiki content. Provide a lowercase slug name; returns its name and private visibility.",
		"inputSchema":  objectSchema(map[string]any{"name": stringSchema()}, []string{"name"}),
		"outputSchema": objectSchema(map[string]any{"name": stringSchema(), "visibility": stringSchema()}, []string{"name", "visibility"}),
	}
}

func scopeDeleteTool() map[string]any {
	return map[string]any{
		"name":         "scope_delete",
		"description":  "Irreversibly delete a non-default scope and all of its wiki content. Provide its exact name; returns deleted true.",
		"inputSchema":  objectSchema(map[string]any{"name": stringSchema()}, []string{"name"}),
		"outputSchema": objectSchema(map[string]any{"name": stringSchema(), "deleted": boolSchema()}, []string{"name", "deleted"}),
	}
}

func scopeSetVisibilityTool() map[string]any {
	return map[string]any{
		"name":        "scope_set_visibility",
		"description": "Set a scope's human-web visibility. Provide its name and private or public; MCP access remains bearer-gated.",
		"inputSchema": objectSchema(map[string]any{
			"name":       stringSchema(),
			"visibility": map[string]any{"type": "string", "enum": []string{"private", "public"}},
		}, []string{"name", "visibility"}),
		"outputSchema": objectSchema(map[string]any{"name": stringSchema(), "visibility": stringSchema()}, []string{"name", "visibility"}),
	}
}

func guideTool() map[string]any {
	return map[string]any{
		"name":        "guide",
		"description": "Guide to wiki fields, subject paths, lifecycle states, pagination, and worked usage examples. Call before your first ingest or merge.",
		"inputSchema": objectSchema(map[string]any{}, nil),
	}
}

func listSchema(properties map[string]any) map[string]any {
	properties["limit"] = map[string]any{"type": "integer"}
	properties["cursor"] = map[string]any{"type": "string"}
	return objectSchema(properties, nil)
}

func scopedListSchema(properties map[string]any) map[string]any {
	properties["limit"] = map[string]any{"type": "integer"}
	properties["cursor"] = map[string]any{"type": "string"}
	return scopedSchema(properties, nil)
}

func scopedSchema(properties map[string]any, required []string) map[string]any {
	properties["scope"] = map[string]any{
		"type":        "string",
		"description": "Explicit wiki scope; content and lists are isolated per scope. Use scopes to discover names.",
	}
	required = append([]string{"scope"}, required...)
	return objectSchema(properties, required)
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if len(properties) == 0 {
		return map[string]any{"type": "object"}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema() map[string]any { return map[string]any{"type": "string"} }

func boolSchema() map[string]any { return map[string]any{"type": "boolean"} }

func nullableStringSchema() map[string]any {
	return map[string]any{"type": []string{"string", "null"}}
}

func stringArraySchema() map[string]any {
	return map[string]any{"type": "array", "items": stringSchema()}
}

func objectArraySchema(properties map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": objectSchema(properties, nil)}
}

func pagedOutputSchema(name string, values map[string]any) map[string]any {
	return objectSchema(map[string]any{name: values, "next_cursor": stringSchema()}, nil)
}

//go:embed guide.md
var guideDoc string

type jobIDArgs struct {
	JobID string `json:"job_id"`
}

func decodeJobIDArgs(raw json.RawMessage) (jobIDArgs, error) {
	var args jobIDArgs
	if err := decodeArgs(raw, &args); err != nil {
		return args, err
	}
	if strings.TrimSpace(args.JobID) == "" {
		return args, fmt.Errorf("job_id is required")
	}
	return args, nil
}

var validJobStatuses = []string{"pending", "working", "done", "failed", "aborted"}

type jobStatusArgs []string

func (s *jobStatusArgs) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*s = nil
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("status must be an array of strings")
		}
		values = []string{value}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !validJobStatus(value) {
			return fmt.Errorf("status must be one of %s", strings.Join(validJobStatuses, ", "))
		}
		out = append(out, value)
	}
	*s = out
	return nil
}

func validJobStatus(value string) bool {
	for _, valid := range validJobStatuses {
		if value == valid {
			return true
		}
	}
	return false
}

var validJobKinds = []string{"ingest", "merge"}

type jobKindArgs []string

func (k *jobKindArgs) UnmarshalJSON(raw []byte) error {
	if string(raw) == "null" {
		*k = nil
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("kind must be an array of strings")
		}
		values = []string{value}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !validJobKind(value) {
			return fmt.Errorf("kind must be one of %s", strings.Join(validJobKinds, ", "))
		}
		out = append(out, value)
	}
	*k = out
	return nil
}

func validJobKind(value string) bool {
	for _, valid := range validJobKinds {
		if value == valid {
			return true
		}
	}
	return false
}

func jobKindArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "string",
			"enum": validJobKinds,
		},
	}
}

func decodeJobsArgs(raw json.RawMessage, withPaging bool) (JobFilter, int, string, error) {
	var args struct {
		Status jobStatusArgs `json:"status"`
		Kind   jobKindArgs   `json:"kind"`
		Since  string        `json:"since"`
		Until  string        `json:"until"`
		Limit  int           `json:"limit"`
		Cursor string        `json:"cursor"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return JobFilter{}, 0, "", err
	}
	since, err := parseOptionalTime(args.Since)
	if err != nil {
		return JobFilter{}, 0, "", fmt.Errorf("since must be RFC3339")
	}
	until, err := parseOptionalTime(args.Until)
	if err != nil {
		return JobFilter{}, 0, "", fmt.Errorf("until must be RFC3339")
	}
	cursor := strings.TrimSpace(args.Cursor)
	if withPaging && cursor != "" {
		if _, ok := paging.DecodeCursor(cursor); !ok {
			return JobFilter{}, 0, "", fmt.Errorf("cursor is invalid")
		}
	}
	kinds := []string(args.Kind)
	if len(kinds) == 0 {
		kinds = []string{"ingest"}
	}
	return JobFilter{Statuses: []string(args.Status), Kinds: kinds, Since: since, Until: until}, args.Limit, cursor, nil
}

func parseOptionalTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func pagedResult(name string, values any, next string) map[string]any {
	return map[string]any{
		name:          values,
		"next_cursor": next,
	}
}

func publicStatusResult(status any) map[string]any {
	v := indirect(reflect.ValueOf(status))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return map[string]any{}
	}
	return map[string]any{
		"status":      stringField(v, "Status"),
		"received_at": interfaceField(v, "ReceivedAt"),
		"started_at":  interfaceField(v, "StartedAt"),
		"finished_at": interfaceField(v, "FinishedAt"),
		"error":       stringField(v, "Error"),
		"subjects":    stringSliceField(v, "Subjects"),
	}
}

func publicAbortResult(result any) map[string]any {
	v := indirect(reflect.ValueOf(result))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return map[string]any{}
	}
	return map[string]any{
		"aborted": boolField(v, "Aborted"),
		"status":  stringField(v, "Status"),
	}
}

func publicRerunResult(result any) map[string]any {
	v := indirect(reflect.ValueOf(result))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return map[string]any{}
	}
	return map[string]any{
		"requeued": boolField(v, "Requeued"),
		"status":   stringField(v, "Status"),
	}
}

func publicJobsResult(jobs any) []map[string]any {
	values := sliceValue(reflect.ValueOf(jobs))
	out := make([]map[string]any, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		job := indirect(values.Index(i))
		if !job.IsValid() || job.Kind() != reflect.Struct {
			continue
		}
		out = append(out, map[string]any{
			"id":          stringField(job, "ID"),
			"owner_email": stringField(job, "OwnerEmail"),
			"owner_id":    stringField(job, "OwnerID"),
			"title":       stringField(job, "Title"),
			"tags":        stringSliceField(job, "Tags"),
			"status":      stringField(job, "Status"),
			"received_at": interfaceField(job, "ReceivedAt"),
			"started_at":  interfaceField(job, "StartedAt"),
			"finished_at": interfaceField(job, "FinishedAt"),
			"error":       stringField(job, "Error"),
		})
	}
	return out
}

func publicMergesResult(merges any) []map[string]string {
	values := sliceValue(reflect.ValueOf(merges))
	out := make([]map[string]string, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		merge := indirect(values.Index(i))
		if !merge.IsValid() || merge.Kind() != reflect.Struct {
			continue
		}
		out = append(out, map[string]string{
			"norm_name":   stringField(merge, "NormName"),
			"subject_id":  stringField(merge, "SubjectID"),
			"name":        stringField(merge, "Name"),
			"owner_email": stringField(merge, "OwnerEmail"),
			"owner_id":    stringField(merge, "OwnerID"),
			"created_at":  stringField(merge, "CreatedAt"),
		})
	}
	return out
}

func publicSubjectsResult(subjects any) []map[string]any {
	values := sliceValue(reflect.ValueOf(subjects))
	out := make([]map[string]any, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		subject := indirect(values.Index(i))
		if !subject.IsValid() || subject.Kind() != reflect.Struct {
			continue
		}
		out = append(out, map[string]any{
			"path":     pathField(subject),
			"type":     stringField(subject, "Type"),
			"name":     stringField(subject, "Name"),
			"has_page": boolField(subject, "HasPage"),
		})
	}
	return out
}

func publicClaimsResult(claims any) []map[string]string {
	values := sliceValue(reflect.ValueOf(claims))
	out := make([]map[string]string, 0, values.Len())
	for i := 0; i < values.Len(); i++ {
		claim := indirect(values.Index(i))
		if !claim.IsValid() || claim.Kind() != reflect.Struct {
			continue
		}
		text := stringField(claim, "Text")
		if text == "" {
			text = stringField(claim, "Body")
		}
		job := stringField(claim, "Job")
		if job == "" {
			job = stringField(claim, "JobID")
		}
		out = append(out, map[string]string{
			"id":   stringField(claim, "ID"),
			"text": text,
			"job":  job,
		})
	}
	return out
}

func publicPageResult(page any, path string) map[string]string {
	v := indirect(reflect.ValueOf(page))
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return map[string]string{}
	}
	pagePath := stringField(v, "Path")
	if pagePath == "" {
		pagePath = strings.TrimSpace(path)
	}
	return map[string]string{
		"subject": pagePath,
		"title":   stringField(v, "Title"),
		"body":    stringField(v, "Body"),
	}
}

func pathField(v reflect.Value) string {
	if path := stringField(v, "Path"); path != "" {
		return path
	}
	typ := stringField(v, "Type")
	normName := stringField(v, "NormName")
	if typ == "" || normName == "" {
		return ""
	}
	return typ + "/" + normName
}

func interfaceField(v reflect.Value, name string) any {
	field := v.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	if (field.Kind() == reflect.Pointer || field.Kind() == reflect.Interface) && field.IsNil() {
		return nil
	}
	return field.Interface()
}

func stringSliceField(v reflect.Value, name string) []string {
	field := v.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return nil
	}
	out := make([]string, 0, field.Len())
	for i := 0; i < field.Len(); i++ {
		item := field.Index(i)
		if item.Kind() == reflect.String {
			out = append(out, item.String())
		}
	}
	return out
}

func sliceValue(v reflect.Value) reflect.Value {
	v = indirect(v)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return reflect.ValueOf([]any{})
	}
	return v
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func validationError(text string) map[string]any {
	return appkitmcp.ErrorResult(appkitmcp.ErrValidation, text)
}

func notFoundError(kind, id string) map[string]any {
	return appkitmcp.ErrorResult(appkitmcp.ErrNotFound, fmt.Sprintf("%s %s not found", kind, id))
}

func internalError(text string) map[string]any {
	return appkitmcp.ErrorResult(appkitmcp.ErrInternal, text)
}

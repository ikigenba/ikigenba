// Package web serves the unauthenticated wiki read surface.
package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strings"

	appkitweb "appkit/web"

	"wiki/internal/ask"
	"wiki/internal/markdown"
	"wiki/internal/page"
	"wiki/internal/retrieve"
	"wiki/internal/wiki"
)

var ErrNotFound = errors.New("web: subject not found")

// Asker answers a question for the owner supplied by the front door.
type Asker interface {
	Ask(ctx context.Context, scope, owner, question string) (ask.Answer, error)
}

// PageFinder resolves a public type/slug path to a rendered subject view.
type PageFinder interface {
	PageByPath(ctx context.Context, path string) (SubjectView, error)
}

// OrphanLister lists subjects with zero inbound mentions for the home index.
type OrphanLister interface {
	Orphans(ctx context.Context, scope string) ([]Ref, error)
}

// SubjectLister lists the subjects available in one scope for public browsing.
type SubjectLister interface {
	ListInScope(context.Context, string, string, string, page.Params) ([]wiki.Subject, string, error)
}

// Mentioner lists wiki subjects mentioned by rendered answer text.
type Mentioner interface {
	MentionsIn(ctx context.Context, scope, text string) ([]Ref, error)
}

// Linkifier projects named subject mentions into markdown links.
type Linkifier interface {
	LinkifyMentions(ctx context.Context, scope, text, base, excludeID string) (string, error)
}

type scopeRegistry interface {
	Get(context.Context, string) (wiki.Scope, error)
	List(context.Context) ([]wiki.Scope, error)
}

type scopedPageFinder interface {
	PageByPathInScope(context.Context, string, string) (SubjectView, error)
}

// Ref is a mount-relative link target for the read surface.
type Ref struct {
	Href string
	Name string
}

// SubjectView is the rendered public page shape consumed by subject routes.
type SubjectView struct {
	SubjectID string
	Path      string
	Title     string
	Body      string
	Footer    string
	Outbound  []Ref
	Inbound   []Ref
}

// Option injects an external dependency seam.
type Option func(*handler)

// WithAsker injects the question-answering seam.
func WithAsker(a Asker) Option {
	return func(h *handler) {
		h.asker = a
	}
}

// WithPageFinder injects the subject-page lookup seam.
func WithPageFinder(p PageFinder) Option {
	return func(h *handler) {
		h.pages = p
	}
}

// WithOrphanLister injects the home-page orphan index seam.
func WithOrphanLister(o OrphanLister) Option {
	return func(h *handler) {
		h.orphans = o
	}
}

// WithSubjectLister injects the public browse-index seam.
func WithSubjectLister(s SubjectLister) Option {
	return func(h *handler) {
		h.subjects = s
	}
}

// WithKeywordRetriever injects the inference-free public search seam.
func WithKeywordRetriever(r retrieve.Retriever) Option {
	return func(h *handler) {
		h.keywords = r
	}
}

// WithMentioner injects the answer mention lookup seam.
func WithMentioner(m Mentioner) Option {
	return func(h *handler) {
		h.mentions = m
	}
}

// WithLinkifier injects the inline mention-link projection seam.
func WithLinkifier(l Linkifier, base string) Option {
	return func(h *handler) {
		h.linkifier = l
		h.linkBase = base
	}
}

// WithScopeStore injects the scope registry used by scoped routes and selectors.
func WithScopeStore(scopes *wiki.ScopeStore) Option {
	return func(h *handler) { h.scopes = scopes }
}

type handler struct {
	service string
	version string
	mount   string
	site    *appkitweb.Site

	asker     Asker
	pages     PageFinder
	orphans   OrphanLister
	subjects  SubjectLister
	keywords  retrieve.Retriever
	mentions  Mentioner
	linkifier Linkifier
	linkBase  string
	scopes    scopeRegistry
}

type pageData struct {
	Service string
	Version string
	Mount   string
	Base    string
	Tier    string
	Scope   string
	Scopes  []wiki.Scope

	Query       string
	Asked       bool
	Answer      ask.Answer
	AnswerHTML  template.HTML
	Cites       []Ref
	Mentions    []Ref
	Orphans     []Ref
	Browse      []Ref
	Results     []Ref
	Searched    bool
	Subject     SubjectView
	SubjectHTML template.HTML
}

// NewHandler builds the read-surface mux.
func NewHandler(service, version, mount string, site *appkitweb.Site, opts ...Option) http.Handler {
	h := &handler{service: service, version: version, mount: normalizeMount(mount), site: site}
	for _, opt := range opts {
		opt(h)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.landing)
	mux.HandleFunc("GET /private/{scope}/{$}", h.privateHome)
	mux.HandleFunc("GET /private/{scope}/subject/{type}/{slug}", h.privateSubject)
	mux.HandleFunc("GET /private/{scope}/select", h.selectScope)
	mux.HandleFunc("GET /public/{scope}/{$}", h.publicHome)
	mux.HandleFunc("GET /public/{scope}/subject/{type}/{slug}", h.publicSubject)
	mux.HandleFunc("GET /public/{scope}/select", h.selectScope)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		h.renderNotFound(w, r, routeTier(r))
	})
	return mux
}

func (h *handler) landing(w http.ResponseWriter, r *http.Request) {
	scope := "default"
	if cookie, err := r.Cookie("wiki_scope"); err == nil {
		if _, ok := h.resolveScope(r.Context(), cookie.Value); ok {
			scope = cookie.Value
		}
	}
	http.Redirect(w, r, h.mount+"private/"+scope+"/", http.StatusSeeOther)
}

func (h *handler) selectScope(w http.ResponseWriter, r *http.Request) {
	tier := routeTier(r)
	if _, ok := h.resolvePageScope(w, r, tier); !ok {
		return
	}
	target := strings.TrimSpace(r.URL.Query().Get("to"))
	scope, ok := h.resolveScope(r.Context(), target)
	if !ok || (tier == "public" && scope.Visibility != "public") {
		h.renderNotFound(w, r, tier)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "wiki_scope", Value: target, Path: h.mount, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, h.mount+tier+"/"+target+"/", http.StatusSeeOther)
}

func (h *handler) privateHome(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePageScope(w, r, "private")
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query != "" {
		h.ask(w, r, scope, query)
		return
	}

	var orphans []Ref
	if h.orphans != nil {
		refs, err := h.orphans.Orphans(r.Context(), scope.Name)
		if err != nil {
			http.Error(w, "list orphan pages", http.StatusInternalServerError)
			return
		}
		orphans = refs
		orphans = h.scopeRefs(orphans, "private", scope.Name)
	}

	if err := h.site.Render(w, "home", h.pageData(r.Context(), "private", scope.Name, pageData{
		Orphans: orphans,
	})); err != nil {
		http.Error(w, "render home page", http.StatusInternalServerError)
	}
}

func (h *handler) publicHome(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePageScope(w, r, "public")
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := pageData{Query: query}
	if query != "" {
		data.Searched = true
		if h.keywords == nil {
			http.Error(w, "search wiki", http.StatusNotImplemented)
			return
		}
		result, err := h.keywords.Search(r.Context(), scope.Name, query, retrieve.SearchLimits{})
		if err != nil {
			http.Error(w, "search wiki", http.StatusInternalServerError)
			return
		}
		for _, hit := range result.Hits {
			data.Results = append(data.Results, Ref{Href: h.subjectHref("public", scope.Name, hit.Path), Name: hit.Title})
		}
	} else if h.subjects != nil {
		cursor := ""
		for {
			subjects, next, err := h.subjects.ListInScope(r.Context(), scope.Name, "", "", page.Params{Limit: page.MaxLimit, Cursor: cursor})
			if err != nil {
				http.Error(w, "list subjects", http.StatusInternalServerError)
				return
			}
			for _, subject := range subjects {
				data.Browse = append(data.Browse, Ref{Href: h.subjectHref("public", scope.Name, wiki.Path(subject)), Name: subject.Name})
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	if err := h.site.Render(w, "home", h.pageData(r.Context(), "public", scope.Name, data)); err != nil {
		http.Error(w, "render home page", http.StatusInternalServerError)
	}
}

func (h *handler) ask(w http.ResponseWriter, r *http.Request, scope wiki.Scope, question string) {
	if h.asker == nil {
		http.Error(w, "ask wiki", http.StatusNotImplemented)
		return
	}
	answer, err := h.asker.Ask(r.Context(), scope.Name, r.Header.Get("X-Owner-Email"), question)
	if err != nil {
		http.Error(w, "ask wiki", http.StatusInternalServerError)
		return
	}

	cites := make([]Ref, 0, len(answer.Citations))
	for _, citation := range answer.Citations {
		if citation.Path == "" || citation.Title == "" {
			continue
		}
		cites = append(cites, Ref{
			Href: "subject/" + citation.Path,
			Name: citation.Title,
		})
	}
	var mentions []Ref
	if h.mentions != nil {
		refs, err := h.mentions.MentionsIn(r.Context(), scope.Name, answer.Text)
		if err != nil {
			http.Error(w, "link answer mentions", http.StatusInternalServerError)
			return
		}
		mentions = refs
		mentions = h.scopeRefs(mentions, "private", scope.Name)
	}
	answerHTML := markdown.Render(answer.Text)
	if h.linkifier != nil {
		base := h.absoluteSubjectBase("private", scope.Name)
		text, err := h.linkifier.LinkifyMentions(r.Context(), scope.Name, answer.Text, base, "")
		if err != nil {
			http.Error(w, "link answer mentions", http.StatusInternalServerError)
			return
		}
		answerHTML = markdown.Render(text)
	}

	if err := h.site.Render(w, "home", h.pageData(r.Context(), "private", scope.Name, pageData{
		Query:      question,
		Asked:      true,
		Answer:     answer,
		AnswerHTML: answerHTML,
		Cites:      cites,
		Mentions:   mentions,
	})); err != nil {
		http.Error(w, "render ask page", http.StatusInternalServerError)
	}
}

func (h *handler) privateSubject(w http.ResponseWriter, r *http.Request) {
	h.subject(w, r, "private")
}

func (h *handler) publicSubject(w http.ResponseWriter, r *http.Request) {
	h.subject(w, r, "public")
}

func (h *handler) subject(w http.ResponseWriter, r *http.Request, tier string) {
	scope, ok := h.resolvePageScope(w, r, tier)
	if !ok {
		return
	}
	if h.pages == nil {
		http.Error(w, "find subject page", http.StatusNotImplemented)
		return
	}
	path := r.PathValue("type") + "/" + r.PathValue("slug")
	var subject SubjectView
	var err error
	if pages, ok := h.pages.(scopedPageFinder); ok {
		subject, err = pages.PageByPathInScope(r.Context(), scope.Name, path)
	} else {
		subject, err = h.pages.PageByPath(r.Context(), path)
	}
	if errors.Is(err, ErrNotFound) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if renderErr := h.site.Render(w, "subject", h.pageData(r.Context(), tier, scope.Name, pageData{
			Subject: SubjectView{
				Title: "Subject not found",
				Body:  "No page exists for this subject.",
			},
			SubjectHTML: markdown.Render("No page exists for this subject."),
		})); renderErr != nil {
			http.Error(w, "render subject page", http.StatusInternalServerError)
		}
		return
	}
	if err != nil {
		http.Error(w, "find subject page", http.StatusInternalServerError)
		return
	}
	subject = h.scopeSubjectLinks(subject, tier, scope.Name)

	if err := h.site.Render(w, "subject", h.pageData(r.Context(), tier, scope.Name, pageData{
		Subject:     subject,
		SubjectHTML: markdown.Render(subject.Body),
	})); err != nil {
		http.Error(w, "render subject page", http.StatusInternalServerError)
	}
}

func routeTier(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/public/") {
		return "public"
	}
	return "private"
}

func (h *handler) resolveScope(ctx context.Context, name string) (wiki.Scope, bool) {
	if h.scopes == nil {
		if name == "default" {
			return wiki.Scope{Name: "default", Visibility: "private"}, true
		}
		return wiki.Scope{}, false
	}
	scope, err := h.scopes.Get(ctx, name)
	return scope, err == nil
}

func (h *handler) resolvePageScope(w http.ResponseWriter, r *http.Request, tier string) (wiki.Scope, bool) {
	name := r.PathValue("scope")
	scope, ok := h.resolveScope(r.Context(), name)
	if !ok || (tier == "public" && scope.Visibility != "public") {
		h.renderNotFound(w, r, tier)
		return wiki.Scope{}, false
	}
	return scope, true
}

func (h *handler) pageData(ctx context.Context, tier, scope string, data pageData) pageData {
	data.Service, data.Version, data.Mount = h.service, h.version, h.mount
	data.Tier, data.Scope = tier, scope
	data.Base = h.mount + tier + "/" + scope + "/"
	if h.scopes == nil {
		data.Scopes = []wiki.Scope{{Name: "default", Visibility: "private"}}
		return data
	}
	all, err := h.scopes.List(ctx)
	if err != nil {
		return data
	}
	for _, candidate := range all {
		if tier == "private" || candidate.Visibility == "public" {
			data.Scopes = append(data.Scopes, candidate)
		}
	}
	return data
}

func (h *handler) renderNotFound(w http.ResponseWriter, r *http.Request, tier string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = h.site.Render(w, "subject", h.pageData(r.Context(), tier, "default", pageData{
		Subject:     SubjectView{Title: "Subject not found", Body: "No page exists for this subject."},
		SubjectHTML: markdown.Render("No page exists for this subject."),
	}))
}

func (h *handler) scopeRefs(refs []Ref, tier, scope string) []Ref {
	if h.linkBase == "" {
		return refs
	}
	for i := range refs {
		refs[i].Href = strings.ReplaceAll(refs[i].Href, h.linkBase, h.absoluteSubjectBase(tier, scope))
	}
	return refs
}

func (h *handler) scopeSubjectLinks(subject SubjectView, tier, scope string) SubjectView {
	if h.linkBase == "" {
		return subject
	}
	base := h.absoluteSubjectBase(tier, scope)
	subject.Body = strings.ReplaceAll(subject.Body, h.linkBase, base)
	subject.Footer = strings.ReplaceAll(subject.Footer, h.linkBase, base)
	subject.Outbound = h.scopeRefs(subject.Outbound, tier, scope)
	subject.Inbound = h.scopeRefs(subject.Inbound, tier, scope)
	return subject
}

func (h *handler) absoluteSubjectBase(tier, scope string) string {
	base := strings.TrimSuffix(h.linkBase, "subject/")
	if base == h.linkBase {
		return h.linkBase
	}
	return base + tier + "/" + scope + "/subject/"
}

func (h *handler) subjectHref(tier, scope, path string) string {
	return h.mount + tier + "/" + scope + "/subject/" + strings.TrimPrefix(path, "/")
}

func normalizeMount(mount string) string {
	if mount == "" {
		return "/"
	}
	if mount[len(mount)-1] != '/' {
		return mount + "/"
	}
	return mount
}

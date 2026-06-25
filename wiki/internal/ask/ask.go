// Package ask answers questions from retrieved wiki pages.
package ask

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	agentkit "github.com/ikigenba/agentkit"

	"wiki/internal/llm"
	"wiki/internal/retrieve"
	"wiki/internal/wiki"
)

const honestEmptyText = "The wiki holds nothing on that question."

const (
	defaultMaxTokens      = 16384
	defaultRelevanceFloor = 0.30
	defaultFinalK         = 8
)

// Answer is a generated answer and the wiki pages it cites.
type Answer struct {
	Found     bool
	Text      string
	Citations []Citation
}

// Citation identifies a wiki page the answer drew on.
type Citation struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// Retriever is the analyzed-query retrieval seam Ask consumes.
type Retriever interface {
	SearchAnalyzed(ctx context.Context, qa any, limits retrieve.SearchLimits) (retrieve.Result, error)
}

// Asker is the read-only question-answering service.
type Asker struct {
	search      Retriever
	subjects    *wiki.SubjectStore
	pages       *wiki.PageStore
	c           *llm.Client
	analyzeSite llm.CallSite
	synthSite   llm.CallSite
	floor       float64
	finalK      int
}

// Option configures an Asker.
type Option func(*Asker)

// WithRelevanceFloor sets the cosine floor used by the honest-empty gate.
func WithRelevanceFloor(floor float64) Option {
	return func(a *Asker) {
		a.floor = floor
	}
}

// WithFinalK sets the number of retrieved pages Ask requests for synthesis.
func WithFinalK(k int) Option {
	return func(a *Asker) {
		a.finalK = k
	}
}

// New creates an Asker from the injected retrieval, page, and LLM seams.
func New(search Retriever, subjects *wiki.SubjectStore, pages *wiki.PageStore, c *llm.Client, analyzeSite, synthSite llm.CallSite, opts ...Option) *Asker {
	a := &Asker{
		search:      search,
		subjects:    subjects,
		pages:       pages,
		c:           c,
		analyzeSite: analyzeSite,
		synthSite:   synthSite,
		floor:       defaultRelevanceFloor,
		finalK:      defaultFinalK,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.finalK <= 0 {
		a.finalK = defaultFinalK
	}
	return a
}

// DefaultSubjectCallSite returns the production ask subject-analysis settings.
func DefaultSubjectCallSite() llm.CallSite {
	return llm.CallSite{
		Stage:     "ask-subject",
		Reasoning: agentkit.Level("low"),
		MaxTokens: defaultMaxTokens,
	}
}

// DefaultSynthesisCallSite returns the production ask answer-synthesis settings.
func DefaultSynthesisCallSite() llm.CallSite {
	return llm.CallSite{
		Stage:     "ask-synthesis",
		Reasoning: agentkit.Level("low"),
		MaxTokens: defaultMaxTokens,
	}
}

// Ask answers a question by analyzing it, retrieving relevant pages, reading
// only those page bodies, and synthesizing an answer grounded in that set.
func (a *Asker) Ask(ctx context.Context, owner, question string) (Answer, error) {
	_ = owner
	if a == nil || a.search == nil || a.subjects == nil || a.pages == nil {
		return Answer{}, fmt.Errorf("ask: nil stores")
	}
	if a.c == nil {
		return Answer{}, fmt.Errorf("ask: nil llm client")
	}

	analysis, err := Analyze(ctx, a.c, a.analyzeSite, question)
	if err != nil {
		return Answer{}, err
	}

	retrieved, err := a.search.SearchAnalyzed(ctx, analysis, retrieve.SearchLimits{Limit: a.finalK})
	if err != nil {
		return Answer{}, err
	}
	if !retrieved.Pinned && retrieved.TopDense < a.floor {
		return honestEmpty(), nil
	}

	pages, err := a.gatherPages(ctx, retrieved.Hits)
	if err != nil {
		return Answer{}, err
	}
	if len(pages) == 0 {
		return honestEmpty(), nil
	}

	result, err := llm.JSON[answerResult](ctx, a.c, a.synthSite, synthPrompt(question, pages), func(out *answerResult) error {
		normalizeAnswer(out)
		return nil
	})
	if err != nil {
		return Answer{}, err
	}
	normalizeAnswer(&result)
	if !result.Found || result.Text == "" {
		return honestEmpty(), nil
	}
	citations := filterCitations(result.Citations, pages)
	if len(citations) == 0 {
		return honestEmpty(), nil
	}
	return Answer{Found: true, Text: result.Text, Citations: citations}, nil
}

func honestEmpty() Answer {
	return Answer{Found: false, Text: honestEmptyText}
}

type answerResult struct {
	Found     bool       `json:"found"`
	Text      string     `json:"text"`
	Citations []Citation `json:"citations"`
}

type pageContext struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (a *Asker) gatherPages(ctx context.Context, hits []retrieve.Hit) ([]pageContext, error) {
	seenSubjects := map[string]struct{}{}
	out := make([]pageContext, 0, len(hits))
	for _, hit := range hits {
		subjectID := strings.TrimSpace(hit.PageID)
		if subjectID == "" {
			continue
		}
		if _, ok := seenSubjects[subjectID]; ok {
			continue
		}
		subject, err := a.subjects.Get(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		page, err := a.pages.GetBySubject(ctx, subjectID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		seenSubjects[subjectID] = struct{}{}
		out = append(out, pageContext{
			Path:  wiki.Path(subject),
			Title: page.Title,
			Body:  page.Body,
		})
	}
	return out, nil
}

func synthPrompt(question string, pages []pageContext) string {
	raw, _ := json.Marshal(pages)
	return "Answer the question using only the supplied wiki pages. " +
		"Return only JSON with found, text, and citations. " +
		"Each citation must use an exact path and title from the pages. " +
		"If the pages do not answer the question, return found=false.\n\n" +
		"Question: " + question + "\n\nPages: " + string(raw)
}

func normalizeAnswer(out *answerResult) {
	if out == nil {
		return
	}
	out.Text = strings.TrimSpace(out.Text)
	for i := range out.Citations {
		out.Citations[i].Path = strings.TrimSpace(out.Citations[i].Path)
		out.Citations[i].Title = strings.TrimSpace(out.Citations[i].Title)
	}
}

func filterCitations(citations []Citation, pages []pageContext) []Citation {
	allowed := make(map[Citation]struct{}, len(pages))
	for _, page := range pages {
		allowed[Citation{Path: page.Path, Title: page.Title}] = struct{}{}
	}
	out := make([]Citation, 0, len(citations))
	for _, citation := range citations {
		if _, ok := allowed[citation]; !ok {
			continue
		}
		out = append(out, citation)
	}
	return out
}

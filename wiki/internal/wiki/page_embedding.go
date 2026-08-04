package wiki

import (
	"context"
	"fmt"
	"strings"

	"appkit/telemetry"

	"wiki/internal/llm"
)

// EmbedRole identifies how prompts should interpret embedding input.
type EmbedRole string

const (
	EmbedDocument EmbedRole = "document"
	EmbedQuery    EmbedRole = "query"
)

// EmbedResult is the wiki-owned embedding result shape.
type EmbedResult struct {
	Vectors [][]float32
}

// PageEmbedder embeds compiled wiki page bodies.
type PageEmbedder interface {
	Embed(ctx context.Context, attr llm.Attribution, inputs []string, role EmbedRole) (*EmbedResult, error)
}

// VectorCache updates the in-memory vector search cache for one subject.
type VectorCache func(subjectID, title string, vec []float32)

// ServiceOption configures optional service dependencies.
type ServiceOption func(*Service)

// WithTelemetryRecorder installs the recorder that starts self-originated job roots.
func WithTelemetryRecorder(recorder *telemetry.Recorder) ServiceOption {
	return func(s *Service) {
		if s != nil && recorder != nil {
			s.recorder = recorder
		}
	}
}

// WithPageEmbedder installs the embedder used to keep page vectors current.
func WithPageEmbedder(model string, embedder PageEmbedder) ServiceOption {
	return func(s *Service) {
		if s == nil {
			return
		}
		s.embedModel = strings.TrimSpace(model)
		s.pageEmbedder = embedder
	}
}

// WithVectorCacheUpdater installs the in-memory vector cache update hook.
func WithVectorCacheUpdater(update func(subjectID, title string, vec []float32)) ServiceOption {
	return func(s *Service) {
		if s == nil {
			return
		}
		s.vectorCache = update
	}
}

// WithVectorCacheRemover installs the in-memory vector cache removal hook.
func WithVectorCacheRemover(remove func(subjectID string)) ServiceOption {
	return func(s *Service) {
		if s == nil {
			return
		}
		s.vectorCacheRemove = remove
	}
}

// embedAndStore computes and stores the current vector for a compiled page.
func (s *Service) embedAndStore(ctx context.Context, attr llm.Attribution, p Page) error {
	if s == nil {
		return fmt.Errorf("wiki: nil service")
	}
	if s.pageEmbedder == nil {
		return nil
	}
	result, err := s.pageEmbedder.Embed(ctx, attr, []string{p.Body}, EmbedDocument)
	if err != nil {
		return err
	}
	if result == nil || len(result.Vectors) != 1 {
		return fmt.Errorf("wiki: page embedder returned %d vectors, want 1", vectorCount(result))
	}
	vec := append([]float32(nil), result.Vectors[0]...)
	embedding := Embedding{
		SubjectID:   p.SubjectID,
		Model:       s.embedModel,
		Dims:        len(vec),
		Vec:         vec,
		ContentHash: pageFingerprint(p),
		UpdatedAt:   s.now().Unix(),
	}
	if err := s.embeddings.Upsert(ctx, embedding); err != nil {
		return err
	}
	if s.vectorCache != nil {
		s.vectorCache(p.SubjectID, p.Title, vec)
	}
	return nil
}

// DrainEmbeddingCatchUp embeds every page missing a current vector under one
// self-originated correlation root.
func (s *Service) DrainEmbeddingCatchUp(ctx context.Context) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("wiki: nil service")
	}
	if s.pageEmbedder == nil {
		return 0, nil
	}
	return s.drainEmbeddingCandidates(ctx)
}

func (s *Service) drainEmbeddingCandidates(ctx context.Context) (int, error) {
	rows, err := s.pages.db.QueryContext(ctx, `
		SELECT p.id, p.subject_id, p.title, p.body, e.model, e.content_hash
		FROM pages p
		LEFT JOIN page_embeddings e ON e.subject_id = p.subject_id
		ORDER BY p.id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type candidate struct {
		page        Page
		model, hash *string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.page.ID, &c.page.SubjectID, &c.page.Title, &c.page.Body, &c.model, &c.hash); err != nil {
			return 0, err
		}
		if c.model == nil || *c.model != s.embedModel || c.hash == nil || *c.hash != pageFingerprint(c.page) {
			candidates = append(candidates, c)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	rootCtx, groupID := s.recorder.StartRoot(ctx, "wiki.embedding-catch-up", map[string]any{"pages": len(candidates)})
	attr := llm.Attribution{Origin: "service:wiki", GroupID: groupID}
	for i, candidate := range candidates {
		if err := s.embedAndStore(rootCtx, attr, candidate.page); err != nil {
			return i, err
		}
	}
	return len(candidates), nil
}

func vectorCount(result *EmbedResult) int {
	if result == nil {
		return 0
	}
	return len(result.Vectors)
}

func pageFingerprint(p Page) string {
	return hashText(p.Title + "\n\n" + p.Body)
}

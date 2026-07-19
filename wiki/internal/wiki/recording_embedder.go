package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"appkit/logging"
	agentkit "github.com/ikigenba/agentkit"

	"wiki/internal/llm"
)

type recordingEmbedder struct {
	inner    agentkitEmbedder
	recorder llm.Recorder
	stage    string
	provider string
	model    string
	dims     int
	now      func() time.Time
	newID    func() string
}

type agentkitEmbedder interface {
	Embed(context.Context, []string, agentkit.InputType) (*agentkit.EmbedResult, error)
}

func NewRecordingEmbedder(inner agentkitEmbedder, recorder llm.Recorder, stage string, provider agentkit.EmbeddingProvider, model string, dims int) agentkitEmbedder {
	providerName := ""
	if provider != nil {
		providerName = provider.Name()
	}
	return &recordingEmbedder{inner: inner, recorder: recorder, stage: stage, provider: providerName, model: model, dims: dims}
}

func (e *recordingEmbedder) Embed(ctx context.Context, texts []string, role agentkit.InputType) (*agentkit.EmbedResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if e.now == nil {
		e.now = time.Now
	}
	if e.newID == nil {
		e.newID = logging.NewULID
	}
	startedAt := e.now()
	result, err := e.inner.Embed(ctx, texts, role)
	endedAt := e.now()
	if e.recorder != nil {
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		rec := llm.CallRecord{
			ID: e.newID(), Stage: e.stage, JobID: llm.JobID(ctx), Attempt: 1,
			Provider: e.provider, Model: e.model,
			Params:   mustMarshal(map[string]any{"dimensions": e.dims}),
			Request:  mustMarshal(map[string]any{"inputs": append([]string(nil), texts...), "role": embeddingRole(role)}),
			Response: embeddingResponse(result), Usage: embeddingUsage(result), Err: errText,
			StartedAt: startedAt, EndedAt: endedAt,
		}
		if recErr := e.recorder.Record(ctx, rec); recErr != nil {
			return nil, recErr
		}
	}
	return result, err
}

func embeddingRole(role agentkit.InputType) string {
	switch role {
	case agentkit.InputQuery:
		return "query"
	case agentkit.InputDocument:
		return "document"
	case agentkit.InputUnspecified:
		return "unspecified"
	default:
		return fmt.Sprintf("unknown:%d", role)
	}
}

func embeddingResponse(result *agentkit.EmbedResult) string {
	if result == nil {
		return ""
	}
	dims := 0
	if len(result.Vectors) > 0 {
		dims = len(result.Vectors[0])
	}
	return mustMarshal(map[string]any{"vectors": len(result.Vectors), "dims": dims, "warnings": len(result.Warnings)})
}

func embeddingUsage(result *agentkit.EmbedResult) string {
	if result == nil || result.Usage() == (agentkit.EmbeddingUsage{}) {
		return ""
	}
	return mustMarshal(result.Usage())
}

func mustMarshal(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

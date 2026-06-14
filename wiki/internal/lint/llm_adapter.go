package lint

import (
	"context"
	"encoding/json"

	"agentkit/provider"

	"wiki/internal/config"
	"wiki/internal/llm"
)

// wrapperCaller adapts *llm.Wrapper to the lint caller interface (the judge + fold
// stages): it unwraps the wrapper's StructuredResult to the raw assistant text the
// parsers consume. Keeping the stages behind caller (rather than *llm.Wrapper
// directly) is what lets the unit gate mock the LLM while production goes through
// the real config-injected wrapper (obligation 1).
type wrapperCaller struct {
	w *llm.Wrapper
}

// NewWrapperCaller adapts a *llm.Wrapper into the caller the lint job takes. The
// composition root builds the wrapper once (client factory + accounting logger)
// and hands it here.
func NewWrapperCaller(w *llm.Wrapper) caller {
	return wrapperCaller{w: w}
}

func (c wrapperCaller) Structured(ctx context.Context, site config.CallSite, schema json.RawMessage, msgs []provider.Message) (string, error) {
	res, err := c.w.Structured(ctx, site, schema, msgs)
	if err != nil {
		return "", err
	}
	return res.Raw, nil
}

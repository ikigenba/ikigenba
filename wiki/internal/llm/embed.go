package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// EmbedSite configures one prompts embedding call site.
type EmbedSite struct {
	Name  string
	Model string
	Dims  int
}

type embedRequest struct {
	Origin     string   `json:"origin"`
	Name       string   `json:"name"`
	GroupID    string   `json:"group_id,omitempty"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions"`
	Role       string   `json:"role"`
	Inputs     []string `json:"inputs"`
}

type embedResponse struct {
	Vectors [][]float32 `json:"vectors"`
}

// Embed posts one batch to prompts /embed and returns vectors in input order.
func (c *Client) Embed(ctx context.Context, site EmbedSite, attr Attribution, role string, inputs []string) ([][]float32, error) {
	if c == nil || c.http == nil || c.baseURL == "" {
		return nil, fmt.Errorf("llm Embed: invalid prompts client")
	}
	body, err := json.Marshal(embedRequest{
		Origin: attr.Origin, Name: site.Name, GroupID: attr.GroupID,
		Model: site.Model, Dimensions: site.Dims, Role: role,
		Inputs: append([]string(nil), inputs...),
	})
	if err != nil {
		return nil, fmt.Errorf("llm: encode /embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: build /embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm: prompts /embed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read prompts /embed response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, promptsEndpointStatusError("/embed", resp.StatusCode, raw)
	}
	var out embedResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("llm: decode prompts /embed response: %w", err)
	}
	return out.Vectors, nil
}

package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func apexResult(s *Site) (mcp.Result, error) {
	var wire any
	if s != nil {
		wire = struct {
			ID         string  `json:"id"`
			Name       string  `json:"name"`
			Slug       string  `json:"slug"`
			URL        string  `json:"url"`
			Repo       string  `json:"repo"`
			Ref        string  `json:"ref"`
			Visibility string  `json:"visibility"`
			Listed     bool    `json:"listed"`
			Commit     *string `json:"commit,omitempty"`
			Created    string  `json:"created"`
			Published  *string `json:"published,omitempty"`
		}{s.ID, s.Name, s.Slug, s.URL, s.Repo, s.Ref, s.Visibility, s.Listed, s.Commit, s.Created, s.Published}
	}
	object, err := json.Marshal(struct {
		Apex any `json:"apex"`
	}{wire})
	if err != nil {
		return mcp.Result{}, err
	}
	content, err := json.Marshal([]struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{"text", string(object)}})
	if err != nil {
		return mcp.Result{}, err
	}
	data := append([]byte(`{"content":`), content...)
	data = append(data, []byte(`,"structuredContent":`)...)
	data = append(data, object...)
	data = append(data, '}')
	var result mcp.Result
	err = json.Unmarshal(data, &result)
	return result, err
}
func (cfg Config) apex(ctx context.Context, u identity.Caller, a ApexArgs) (mcp.Result, error) {
	if a.Name != nil && a.Clear != nil && *a.Clear {
		return mcp.Result{}, errors.New(NameOrClear)
	}
	if a.Name != nil {
		s, err := cfg.find(ctx, u.UserID, *a.Name)
		if err != nil {
			return mcp.Result{}, err
		}
		if s.Visibility != store.Public {
			return mcp.Result{}, errors.New(ApexNotPublic)
		}
		s, err = cfg.Store.SetApex(ctx, s.ID)
		if errors.Is(err, store.ErrNotPublic) {
			return mcp.Result{}, errors.New(ApexNotPublic)
		}
		if err != nil {
			return mcp.Result{}, catalogError(err)
		}
		cfg.emit(ctx, "site.apex", telemetry.Attrs{"site": s.ID})
		o := siteObject(ctx, s)
		return apexResult(&o)
	}
	if a.Clear != nil && *a.Clear {
		if err := cfg.Store.ClearApex(ctx); err != nil {
			return mcp.Result{}, catalogError(err)
		}
		cfg.emit(ctx, "site.apex", telemetry.Attrs{"site": ""})
		return apexResult(nil)
	}
	s, ok, err := cfg.Store.Apex(ctx)
	if err != nil {
		return mcp.Result{}, catalogError(err)
	}
	if !ok {
		return apexResult(nil)
	}
	o := siteObject(ctx, s)
	return apexResult(&o)
}

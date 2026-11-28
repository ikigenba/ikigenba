package tools

import (
	"context"
	"errors"

	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
)

// TeamTemplateArgs takes no arguments.
type TeamTemplateArgs struct{}

// SetTeamTemplateArgs changes either or both members of the setting.
type SetTeamTemplateArgs struct {
	URL *string `json:"url" description:"HTTPS URL of the team template repository."`
	Ref *string `json:"ref" description:"Branch or tag of the team template repository."`
}

// TeamTemplate is the repository and ref used for new accounts and users.
type TeamTemplate struct {
	URL string `json:"url" mcp:"required"`
	Ref string `json:"ref" mcp:"required"`
}

func (cfg Config) teamTemplate(ctx context.Context, _ TeamTemplateArgs) (TeamTemplate, error) {
	setting, err := cfg.Store.TemplateSettings(ctx)
	if err != nil {
		return TeamTemplate{}, errors.New(store.Unreachable)
	}
	return TeamTemplate(setting), nil
}

func (cfg Config) setTeamTemplate(ctx context.Context, _ identity.Caller, a SetTeamTemplateArgs, _ *changeRecord) (TeamTemplate, error) {
	if a.URL == nil && a.Ref == nil {
		return TeamTemplate{}, errors.New(NothingToChange)
	}
	if a.URL != nil && valid.TemplateURL(*a.URL) != nil {
		return TeamTemplate{}, errors.New(InvalidURL)
	}
	if a.Ref != nil && valid.TemplateRef(*a.Ref) != nil {
		return TeamTemplate{}, errors.New(InvalidRef)
	}
	setting, changed, err := cfg.Store.SetTemplateSettings(ctx, a.URL, a.Ref)
	if err != nil {
		return TeamTemplate{}, errors.New(store.Unreachable)
	}
	if changed {
		trail.TemplateChanged(ctx, cfg.Telemetry, setting.URL, setting.Ref)
	}
	return TeamTemplate(setting), nil
}

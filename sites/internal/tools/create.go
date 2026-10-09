package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func createRules(a CreateArgs) error {
	if !store.ValidName(a.Name) {
		return fmt.Errorf(InvalidName, a.Name)
	}
	if a.Ref != nil && !cache.ValidRef(*a.Ref) {
		return fmt.Errorf(InvalidRef, *a.Ref)
	}
	if a.Visibility != nil && *a.Visibility != store.Public && *a.Visibility != store.Private {
		return errors.New(BadVisibility)
	}
	return nil
}
func (cfg Config) create(ctx context.Context, u identity.Caller, a CreateArgs) (Site, error) {
	if !store.ValidName(a.Name) {
		return Site{}, fmt.Errorf(InvalidName, a.Name)
	}
	taken, err := cfg.Store.Taken(ctx, a.Name)
	if err != nil {
		if rule := createRules(a); rule != nil {
			return Site{}, rule
		}
		return Site{}, catalogError(err)
	}
	if taken {
		return Site{}, fmt.Errorf(NameTaken, a.Name)
	}
	if !cache.ValidRepo(a.Repo) {
		return Site{}, fmt.Errorf(NoRepository, a.Repo)
	}
	owner, err := cfg.Cache.Owner(ctx, a.Repo)
	if errors.Is(err, cache.ErrRepositoryMissing) || err == nil && owner != u.UserID {
		return Site{}, fmt.Errorf(NoRepository, a.Repo)
	}
	if err != nil {
		return Site{}, cfg.gitError(ctx, err)
	}
	if rule := createRules(a); rule != nil {
		return Site{}, rule
	}
	d := store.Draft{Owner: u.UserID, Name: a.Name, Repo: a.Repo, Ref: "main", Visibility: store.Public, Listed: true}
	if a.Ref != nil {
		d.Ref = *a.Ref
	}
	if a.Visibility != nil {
		d.Visibility = *a.Visibility
	}
	if a.Listed != nil {
		d.Listed = *a.Listed
	}
	s, err := cfg.Store.Create(ctx, d)
	if errors.Is(err, store.ErrNameTaken) {
		return Site{}, fmt.Errorf(NameTaken, a.Name)
	}
	if err != nil {
		return Site{}, catalogError(err)
	}
	cfg.emit(ctx, "site.created", telemetry.Attrs{"site": s.ID, "repo": s.Repo, "visibility": s.Visibility, "listed": s.Listed})
	return siteObject(ctx, s), nil
}

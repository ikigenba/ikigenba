package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/cache"
	"github.com/ikigenba/ikigenba/sites/internal/store"
)

func updateRules(a UpdateArgs) error {
	if !store.ValidName(a.Name) {
		return fmt.Errorf("no site named '%s'", a.Name)
	}
	if a.Visibility == nil && a.Listed == nil && a.Ref == nil {
		return errors.New("update needs at least one of visibility, listed, ref")
	}
	if a.Visibility != nil && *a.Visibility != store.Public && *a.Visibility != store.Private {
		return errors.New("visibility must be public or private")
	}
	if a.Ref != nil && !cache.ValidRef(*a.Ref) {
		return fmt.Errorf("invalid ref '%s'", *a.Ref)
	}
	return nil
}
func (cfg Config) update(ctx context.Context, u identity.Caller, a UpdateArgs) (Site, error) {
	s, err := cfg.find(ctx, u.UserID, a.Name)
	if err != nil {
		if err.Error() == store.Unreachable {
			if rule := updateRules(a); rule != nil {
				return Site{}, rule
			}
		}
		return Site{}, err
	}
	if rule := updateRules(a); rule != nil {
		return Site{}, rule
	}
	s, changed, err := cfg.Store.Update(ctx, s.ID, store.Change{Visibility: a.Visibility, Listed: a.Listed, Ref: a.Ref})
	if errors.Is(err, store.ErrNotPublic) {
		return Site{}, errors.New("apex site must be public")
	}
	if err != nil {
		return Site{}, catalogError(err)
	}
	for _, field := range changed {
		value := s.Ref
		switch field {
		case "visibility":
			value = s.Visibility
		case "listed":
			value = strconv.FormatBool(s.Listed)
		}
		cfg.emit(ctx, "site.updated", telemetry.Attrs{"site": s.ID, "field": field, "value": value})
	}
	return siteObject(ctx, s), nil
}
